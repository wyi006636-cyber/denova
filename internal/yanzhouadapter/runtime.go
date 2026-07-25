package yanzhouadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"denova/internal/agent"
)

// RuntimeTimeoutType is the exact closed timeout taxonomy from Product Spec 1.0.
type RuntimeTimeoutType string

const (
	RuntimeTimeoutStartup         RuntimeTimeoutType = "startup_timeout"
	RuntimeTimeoutHandshake       RuntimeTimeoutType = "handshake_timeout"
	RuntimeTimeoutProviderConnect RuntimeTimeoutType = "provider_connect_timeout"
	RuntimeTimeoutProviderIdle    RuntimeTimeoutType = "provider_idle_timeout"
	RuntimeTimeoutTool            RuntimeTimeoutType = "tool_timeout"
	RuntimeTimeoutRunWall         RuntimeTimeoutType = "run_wall_timeout"
	RuntimeTimeoutCancelGrace     RuntimeTimeoutType = "cancel_grace_timeout"
	RuntimeTimeoutDisplayConsumer RuntimeTimeoutType = "display_consumer_timeout"
)

// TerminationCause is the closed set of lifecycle causes handled by WP3.
type TerminationCause string

const (
	TerminationCauseProviderIdleTimeout TerminationCause = "provider_idle_timeout"
	TerminationCauseUserCancelled       TerminationCause = "user_cancelled"
	TerminationCauseProviderError       TerminationCause = "provider_error"
	TerminationCausePanic               TerminationCause = "panic"
	TerminationCauseBudgetExhausted     TerminationCause = "budget_exhausted"
	TerminationCauseRunWallTimeout      TerminationCause = "run_wall_timeout"
)

// TerminalRunState uses only existing Product Spec terminal states.
type TerminalRunState string

const (
	TerminalRunStateInterrupted     TerminalRunState = "interrupted"
	TerminalRunStateBudgetExhausted TerminalRunState = "budget_exhausted"
	TerminalRunStateCompleted       TerminalRunState = "completed"
	TerminalRunStateFailed          TerminalRunState = "failed"
	TerminalRunStateAborted         TerminalRunState = "aborted"
)

var (
	runtimeTimeoutTypes = []RuntimeTimeoutType{
		RuntimeTimeoutStartup,
		RuntimeTimeoutHandshake,
		RuntimeTimeoutProviderConnect,
		RuntimeTimeoutProviderIdle,
		RuntimeTimeoutTool,
		RuntimeTimeoutRunWall,
		RuntimeTimeoutCancelGrace,
		RuntimeTimeoutDisplayConsumer,
	}
	terminationCauses = []TerminationCause{
		TerminationCauseProviderIdleTimeout,
		TerminationCauseUserCancelled,
		TerminationCauseProviderError,
		TerminationCausePanic,
		TerminationCauseBudgetExhausted,
		TerminationCauseRunWallTimeout,
	}
	terminalRunStates = []TerminalRunState{
		TerminalRunStateInterrupted,
		TerminalRunStateBudgetExhausted,
		TerminalRunStateCompleted,
		TerminalRunStateFailed,
		TerminalRunStateAborted,
	}
)

// TerminationInput contains only bounded refs and explicit lifecycle facts.
type TerminationInput struct {
	Cause               TerminationCause   `json:"cause"`
	Resumable           *bool              `json:"resumable,omitempty"`
	PartialArtifactRefs []string           `json:"partialArtifactRefs,omitempty"`
	CheckpointID        string             `json:"checkpointId,omitempty"`
	TimeoutType         RuntimeTimeoutType `json:"timeoutType,omitempty"`
}

// TerminationDecision is safe for a run.event payload. It deliberately has no
// raw provider error, path, credential, profile, request, response, or stderr.
type TerminationDecision struct {
	EventType           RunEventType       `json:"eventType"`
	State               TerminalRunState   `json:"state"`
	Reason              string             `json:"reason"`
	Resumable           bool               `json:"resumable"`
	PartialArtifactRefs []string           `json:"partialArtifactRefs"`
	CheckpointID        string             `json:"checkpointId,omitempty"`
	TimeoutType         RuntimeTimeoutType `json:"timeoutType,omitempty"`
}

func RuntimeTimeoutTypes() []RuntimeTimeoutType {
	return append([]RuntimeTimeoutType(nil), runtimeTimeoutTypes...)
}

func TerminationCauses() []TerminationCause {
	return append([]TerminationCause(nil), terminationCauses...)
}

func TerminalRunStates() []TerminalRunState {
	return append([]TerminalRunState(nil), terminalRunStates...)
}

func validRuntimeTimeoutType(value RuntimeTimeoutType) bool {
	for _, candidate := range runtimeTimeoutTypes {
		if value == candidate {
			return true
		}
	}
	return false
}

func terminationEventType(state TerminalRunState) (RunEventType, bool) {
	switch state {
	case TerminalRunStateInterrupted:
		return RunEventTypeRunInterrupted, true
	case TerminalRunStateBudgetExhausted:
		return RunEventTypeRunBudgetExhausted, true
	case TerminalRunStateFailed:
		return RunEventTypeRunFailed, true
	case TerminalRunStateAborted:
		return RunEventTypeRunAborted, true
	default:
		return "", false
	}
}

func expectedTerminationTimeout(cause TerminationCause) RuntimeTimeoutType {
	switch cause {
	case TerminationCauseProviderIdleTimeout:
		return RuntimeTimeoutProviderIdle
	case TerminationCauseRunWallTimeout:
		return RuntimeTimeoutRunWall
	default:
		return ""
	}
}

func invalidTermination() (TerminationDecision, error) {
	return TerminationDecision{}, errors.New("termination input is invalid")
}

// ClassifyTermination is pure and fails closed for unknown or contradictory input.
func ClassifyTermination(input TerminationInput) (TerminationDecision, error) {
	finish, ok := agent.ClassifyRunTermination(agent.RunTerminationCause(input.Cause))
	if !ok {
		return invalidTermination()
	}
	state := TerminalRunState(finish.Status)
	eventType, ok := terminationEventType(state)
	if !ok {
		return invalidTermination()
	}
	expectedTimeout := expectedTerminationTimeout(input.Cause)
	if input.TimeoutType != "" && (!validRuntimeTimeoutType(input.TimeoutType) || input.TimeoutType != expectedTimeout) {
		return invalidTermination()
	}
	refs := append([]string{}, input.PartialArtifactRefs...)
	if len(refs) > 32 {
		return invalidTermination()
	}
	seenRefs := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if !validResumeIdentityValue(ref) {
			return invalidTermination()
		}
		if _, exists := seenRefs[ref]; exists {
			return invalidTermination()
		}
		seenRefs[ref] = struct{}{}
	}
	if input.CheckpointID != "" && !validResumeIdentityValue(input.CheckpointID) {
		return invalidTermination()
	}

	var resumable bool
	switch input.Cause {
	case TerminationCauseProviderIdleTimeout,
		TerminationCauseBudgetExhausted,
		TerminationCauseRunWallTimeout:
		resumable = true
	case TerminationCauseUserCancelled:
		resumable = false
	case TerminationCauseProviderError, TerminationCausePanic:
		if input.Resumable == nil {
			return invalidTermination()
		}
		resumable = *input.Resumable
		if resumable && input.CheckpointID == "" && len(refs) == 0 {
			return invalidTermination()
		}
	default:
		return invalidTermination()
	}
	if input.Resumable != nil && input.Cause != TerminationCauseProviderError && input.Cause != TerminationCausePanic && *input.Resumable != resumable {
		return invalidTermination()
	}

	return TerminationDecision{
		EventType:           eventType,
		State:               state,
		Reason:              finish.Reason,
		Resumable:           resumable,
		PartialArtifactRefs: refs,
		CheckpointID:        input.CheckpointID,
		TimeoutType:         expectedTimeout,
	}, nil
}

// DecodeTerminationInput admits one strict bounded JSON object and suppresses
// parser details so rejected fields or values never enter public errors.
func DecodeTerminationInput(raw []byte) (TerminationInput, error) {
	if len(raw) == 0 || len(raw) > 8*1024 {
		return TerminationInput{}, errors.New("termination input is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input TerminationInput
	if err := decoder.Decode(&input); err != nil {
		return TerminationInput{}, errors.New("termination input is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return TerminationInput{}, errors.New("termination input is invalid")
	}
	if _, err := ClassifyTermination(input); err != nil {
		return TerminationInput{}, errors.New("termination input is invalid")
	}
	return input, nil
}

// DurableRuntimePort is the narrow lifecycle authority supplied by the
// Electron/main side. The sidecar receives durable identities and receipts;
// it does not discover a workspace or scan filesystem paths.
type DurableRuntimePort interface {
	BeginResumeAttempt(context.Context, agent.ResumeAttemptBasis) (agent.ResumeAttemptIdentity, error)
	FinishResumeAttempt(context.Context, agent.ResumeAttemptFinish) (agent.ResumeAttemptFinishReceipt, error)
}

// DurableRuntimeConversation adds the optional durable resume lifecycle to an
// existing Conversation without changing the legacy Conversation contract.
type DurableRuntimeConversation struct {
	agent.Conversation
	port DurableRuntimePort
}

func NewDurableRuntimeConversation(conversation agent.Conversation, port DurableRuntimePort) (*DurableRuntimeConversation, error) {
	if conversation == nil {
		return nil, errors.New("durable runtime conversation is required")
	}
	if port == nil {
		return nil, errors.New("durable runtime port is required")
	}
	return &DurableRuntimeConversation{Conversation: conversation, port: port}, nil
}

func (c *DurableRuntimeConversation) BeginResumeAttempt(ctx context.Context, basis agent.ResumeAttemptBasis) (agent.ResumeAttemptIdentity, error) {
	if !validResumeIdentityValue(basis.OperationID) || !validResumeIdentityValue(basis.InterruptionID) {
		return agent.ResumeAttemptIdentity{}, errors.New("resume attempt basis is invalid")
	}
	attempt, err := c.port.BeginResumeAttempt(ctx, basis)
	if err != nil {
		return agent.ResumeAttemptIdentity{}, errors.New("resume attempt begin failed")
	}
	if !validResumeAttemptForBasis(basis, attempt) {
		return agent.ResumeAttemptIdentity{}, errors.New("resume attempt identity is invalid")
	}
	return attempt, nil
}

func (c *DurableRuntimeConversation) FinishResumeAttempt(ctx context.Context, input agent.ResumeAttemptFinish) (agent.ResumeAttemptFinishReceipt, error) {
	if !validResumeAttemptIdentity(input.Attempt) || !validResumeFinishOutcome(input.Outcome) {
		return agent.ResumeAttemptFinishReceipt{}, errors.New("resume attempt finish is invalid")
	}
	receipt, err := c.port.FinishResumeAttempt(ctx, input)
	if err != nil {
		return agent.ResumeAttemptFinishReceipt{}, errors.New("resume attempt finish failed")
	}
	if !validResumeFinishReceipt(input, receipt) {
		return agent.ResumeAttemptFinishReceipt{}, errors.New("resume attempt finish receipt is invalid")
	}
	return receipt, nil
}

func validResumeIdentityValue(value string) bool {
	return value != "" &&
		value == strings.TrimSpace(value) &&
		len(value) <= checkpointMaxIdentityBytes &&
		checkpointIdentityPattern.MatchString(value) &&
		!containsSensitiveString(value)
}

func validResumeAttemptForBasis(basis agent.ResumeAttemptBasis, attempt agent.ResumeAttemptIdentity) bool {
	if !validResumeIdentityValue(attempt.AttemptID) ||
		!validResumeIdentityValue(attempt.ExecutionRunID) ||
		!validResumeIdentityValue(attempt.InterruptionID) ||
		!validResumeIdentityValue(attempt.OriginRunID) {
		return false
	}
	if attempt.ParentAttemptID != "" && !validResumeIdentityValue(attempt.ParentAttemptID) {
		return false
	}
	return attempt.ExecutionRunID != attempt.OriginRunID &&
		attempt.AttemptID != attempt.ParentAttemptID &&
		attempt.AttemptNumber > 0 && attempt.AttemptNumber <= 1_000_000 &&
		attempt.Status == agent.ResumeAttemptStatusRunning &&
		attempt.InterruptionID == basis.InterruptionID &&
		attempt.Validation.OperationID == basis.OperationID &&
		attempt.Validation.InterruptionID == basis.InterruptionID &&
		attempt.Validation.OriginRunID == attempt.OriginRunID
}

func validResumeAttemptIdentity(attempt agent.ResumeAttemptIdentity) bool {
	basis := agent.ResumeAttemptBasis{
		OperationID:    attempt.Validation.OperationID,
		InterruptionID: attempt.Validation.InterruptionID,
	}
	return validResumeIdentityValue(basis.OperationID) &&
		validResumeIdentityValue(basis.InterruptionID) &&
		validResumeAttemptForBasis(basis, attempt)
}

func validResumeFinishOutcome(outcome agent.ResumeAttemptFinishOutcome) bool {
	switch outcome {
	case agent.ResumeAttemptOutcomeSucceeded,
		agent.ResumeAttemptOutcomePrepareFailed,
		agent.ResumeAttemptOutcomeUserCommitFailed,
		agent.ResumeAttemptOutcomeCompactionFailed,
		agent.ResumeAttemptOutcomeRunnerFailed,
		agent.ResumeAttemptOutcomeCancelled,
		agent.ResumeAttemptOutcomeProviderFailed,
		agent.ResumeAttemptOutcomeProviderIdleTimeout,
		agent.ResumeAttemptOutcomePanicked,
		agent.ResumeAttemptOutcomeBudgetExhausted,
		agent.ResumeAttemptOutcomeRunWallTimeout,
		agent.ResumeAttemptOutcomeAssistantPersistFailed,
		agent.ResumeAttemptOutcomeLifecycleFinishFailed:
		return true
	default:
		return false
	}
}

func validResumeFinishReceipt(input agent.ResumeAttemptFinish, receipt agent.ResumeAttemptFinishReceipt) bool {
	expected := input.Attempt
	if input.Outcome == agent.ResumeAttemptOutcomeSucceeded && receipt.Outcome == agent.ResumeAttemptOutcomeLifecycleFinishFailed {
		expected.Status = agent.ResumeAttemptStatusFailed
		return !receipt.InterruptionResolved && receipt.Attempt == expected
	}
	if input.Outcome == agent.ResumeAttemptOutcomeSucceeded {
		expected.Status = agent.ResumeAttemptStatusSucceeded
		if !receipt.InterruptionResolved {
			return false
		}
	} else {
		expected.Status = agent.ResumeAttemptStatusFailed
		if receipt.InterruptionResolved {
			return false
		}
	}
	return receipt.Outcome == input.Outcome && receipt.Attempt == expected
}
