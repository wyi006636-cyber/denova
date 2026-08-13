package yanzhouadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
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
	TerminationCauseToolError           TerminationCause = "tool_error"
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
		TerminationCauseToolError,
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
	var state TerminalRunState
	var reason string
	switch input.Cause {
	case TerminationCauseProviderIdleTimeout:
		state, reason = TerminalRunStateInterrupted, "provider_idle_timeout"
	case TerminationCauseUserCancelled:
		state, reason = TerminalRunStateAborted, "cancelled"
	case TerminationCauseProviderError:
		state, reason = TerminalRunStateFailed, "provider_error"
	case TerminationCauseToolError:
		state, reason = TerminalRunStateFailed, "tool_error"
	case TerminationCausePanic:
		state, reason = TerminalRunStateFailed, "panic"
	case TerminationCauseBudgetExhausted:
		state, reason = TerminalRunStateBudgetExhausted, "budget_exhausted"
	case TerminationCauseRunWallTimeout:
		state, reason = TerminalRunStateBudgetExhausted, "run_wall_timeout"
	default:
		return invalidTermination()
	}
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
	case TerminationCauseProviderError, TerminationCauseToolError, TerminationCausePanic:
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
	if input.Resumable != nil && input.Cause != TerminationCauseProviderError && input.Cause != TerminationCauseToolError && input.Cause != TerminationCausePanic && *input.Resumable != resumable {
		return invalidTermination()
	}

	return TerminationDecision{
		EventType:           eventType,
		State:               state,
		Reason:              reason,
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

func validResumeIdentityValue(value string) bool {
	return value != "" &&
		value == strings.TrimSpace(value) &&
		len(value) <= runtimeEventMaxRunIDBytes &&
		runtimeEventRunIDPattern.MatchString(value) &&
		!containsSensitiveString(value)
}

func containsSensitiveString(value string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "_", ""), "-", ""))
	for _, marker := range []string{"apikey", "runtimeauth", "authorization", "bearer", "password", "secret"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
