package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"

	"denova/config"
	agentcontext "denova/internal/agent/context"
	"denova/internal/session"
)

const contextReceiptMaxNumber = 1_000_000_000

// Conversation 抽象 Agent 对话的上下文读取与结果写入。
// 写作模式写入普通 session，游戏模式可写入 interactive/story。
type Conversation interface {
	PrepareMessages(originalMessage, agentMessage string) ([]*schema.Message, error)
	AppendAssistant(content string) error
	MarkInterrupted(userMessage, assistantContent, reason string) error
	PendingInterruption() *session.Interruption
	ResolveInterruption(id string) error
}

// ResumeAttemptStatus is the durable state of a newly executed resume
// attempt. Resume starts a new execution; it never represents a suspended Go
// routine continuing in memory.
type ResumeAttemptStatus string

const (
	ResumeAttemptStatusRunning   ResumeAttemptStatus = "running"
	ResumeAttemptStatusSucceeded ResumeAttemptStatus = "succeeded"
	ResumeAttemptStatusFailed    ResumeAttemptStatus = "failed"
)

// ResumeAttemptFinishOutcome is the closed terminal reason recorded for a
// durable resume attempt.
type ResumeAttemptFinishOutcome string

const (
	ResumeAttemptOutcomeSucceeded              ResumeAttemptFinishOutcome = "succeeded"
	ResumeAttemptOutcomePrepareFailed          ResumeAttemptFinishOutcome = "prepare_failed"
	ResumeAttemptOutcomeUserCommitFailed       ResumeAttemptFinishOutcome = "user_commit_failed"
	ResumeAttemptOutcomeCompactionFailed       ResumeAttemptFinishOutcome = "compaction_failed"
	ResumeAttemptOutcomeRunnerFailed           ResumeAttemptFinishOutcome = "runner_failed"
	ResumeAttemptOutcomeCancelled              ResumeAttemptFinishOutcome = "cancelled"
	ResumeAttemptOutcomeProviderFailed         ResumeAttemptFinishOutcome = "provider_failed"
	ResumeAttemptOutcomeProviderIdleTimeout    ResumeAttemptFinishOutcome = "provider_idle_timeout"
	ResumeAttemptOutcomePanicked               ResumeAttemptFinishOutcome = "panicked"
	ResumeAttemptOutcomeBudgetExhausted        ResumeAttemptFinishOutcome = "budget_exhausted"
	ResumeAttemptOutcomeRunWallTimeout         ResumeAttemptFinishOutcome = "run_wall_timeout"
	ResumeAttemptOutcomeAssistantPersistFailed ResumeAttemptFinishOutcome = "assistant_persist_failed"
	ResumeAttemptOutcomeLifecycleFinishFailed  ResumeAttemptFinishOutcome = "lifecycle_finish_failed"
)

// RunTerminationCause is the closed internal cause shared by the Agent loop
// and the Yanzhou adapter. It contains no provider error text or runtime data.
type RunTerminationCause string

const (
	RunTerminationProviderIdleTimeout RunTerminationCause = "provider_idle_timeout"
	RunTerminationUserCancelled       RunTerminationCause = "user_cancelled"
	RunTerminationProviderError       RunTerminationCause = "provider_error"
	RunTerminationPanic               RunTerminationCause = "panic"
	RunTerminationBudgetExhausted     RunTerminationCause = "budget_exhausted"
	RunTerminationWallTimeout         RunTerminationCause = "run_wall_timeout"
)

// RunTerminationDecision is the stable ledger/resume projection for a closed
// cause. Raw errors and provider data must never be used as Status or Reason.
type RunTerminationDecision struct {
	Status        string
	Reason        string
	ResumeOutcome ResumeAttemptFinishOutcome
}

func ClassifyRunTermination(cause RunTerminationCause) (RunTerminationDecision, bool) {
	switch cause {
	case RunTerminationProviderIdleTimeout:
		return RunTerminationDecision{
			Status:        "interrupted",
			Reason:        "provider_idle_timeout",
			ResumeOutcome: ResumeAttemptOutcomeProviderIdleTimeout,
		}, true
	case RunTerminationUserCancelled:
		return RunTerminationDecision{
			Status:        "aborted",
			Reason:        "cancelled",
			ResumeOutcome: ResumeAttemptOutcomeCancelled,
		}, true
	case RunTerminationProviderError:
		return RunTerminationDecision{
			Status:        "failed",
			Reason:        "provider_error",
			ResumeOutcome: ResumeAttemptOutcomeProviderFailed,
		}, true
	case RunTerminationPanic:
		return RunTerminationDecision{
			Status:        "failed",
			Reason:        "panic",
			ResumeOutcome: ResumeAttemptOutcomePanicked,
		}, true
	case RunTerminationBudgetExhausted:
		return RunTerminationDecision{
			Status:        "budget_exhausted",
			Reason:        "budget_exhausted",
			ResumeOutcome: ResumeAttemptOutcomeBudgetExhausted,
		}, true
	case RunTerminationWallTimeout:
		return RunTerminationDecision{
			Status:        "budget_exhausted",
			Reason:        "run_wall_timeout",
			ResumeOutcome: ResumeAttemptOutcomeRunWallTimeout,
		}, true
	default:
		return RunTerminationDecision{}, false
	}
}

// ResumeAttemptBasis is the bounded request used by the main-side authority to
// validate that an interruption is still pending before allocating an attempt.
type ResumeAttemptBasis struct {
	OperationID    string `json:"operation_id"`
	InterruptionID string `json:"interruption_id"`
}

// ResumeValidationReceipt records only the durable identities established by
// the main-side authority while validating the pending interruption.
type ResumeValidationReceipt struct {
	OperationID    string `json:"operation_id"`
	InterruptionID string `json:"interruption_id"`
	OriginRunID    string `json:"origin_run_id"`
}

// ResumeAttemptIdentity identifies a new execution derived from a durable
// interruption. OriginRunID is lineage only and must not be used as the new
// execution identity.
type ResumeAttemptIdentity struct {
	AttemptID       string                  `json:"attempt_id"`
	ExecutionRunID  string                  `json:"execution_run_id"`
	InterruptionID  string                  `json:"interruption_id"`
	OriginRunID     string                  `json:"origin_run_id"`
	ParentAttemptID string                  `json:"parent_attempt_id,omitempty"`
	AttemptNumber   int                     `json:"attempt_number"`
	Status          ResumeAttemptStatus     `json:"status"`
	Validation      ResumeValidationReceipt `json:"validation"`
}

// ResumeAttemptFinish is the canonical terminal operation sent to the
// main-side authority.
type ResumeAttemptFinish struct {
	Attempt ResumeAttemptIdentity      `json:"attempt"`
	Outcome ResumeAttemptFinishOutcome `json:"outcome"`
}

// ResumeAttemptFinishReceipt is the bounded durable result of a finish
// operation. InterruptionResolved is true only for a successful attempt.
type ResumeAttemptFinishReceipt struct {
	Attempt              ResumeAttemptIdentity      `json:"attempt"`
	Outcome              ResumeAttemptFinishOutcome `json:"outcome"`
	InterruptionResolved bool                       `json:"interruption_resolved"`
}

// ResumeAttemptLifecycle is optional. Legacy Conversation implementations do
// not implement it and retain their existing interruption behavior.
type ResumeAttemptLifecycle interface {
	BeginResumeAttempt(context.Context, ResumeAttemptBasis) (ResumeAttemptIdentity, error)
	FinishResumeAttempt(context.Context, ResumeAttemptFinish) (ResumeAttemptFinishReceipt, error)
}

// UserMessageReferencesSetter lets a durable conversation attach bounded,
// display-only references to the next persisted user message.
type UserMessageReferencesSetter interface {
	SetUserMessageReferences([]session.UserMessageReference)
}

// ContextSourceReporter 可由 Conversation 提供本轮已拼装的业务上下文来源。
// ChatService 会在 PrepareMessages 后追加打印，便于排查非通用注入内容。
type ContextSourceReporter interface {
	ContextSourceSummary() string
}

// ContextLedgerReporter exposes bounded metadata for the actual domain context
// fragments assembled by a Conversation. Full fragment content is never
// persisted by the runtime.
type ContextLedgerReporter interface {
	ContextLedgerParts() []ContextLedgerPart
}

// FinalContextLedgerReporter rebuilds domain context audit metadata from the
// exact message list sent to the model after context compaction. Implementers
// must not retain full message bodies in the returned durable records.
type FinalContextLedgerReporter interface {
	ContextLedgerPartsForMessages(messages []*schema.Message) []ContextLedgerPart
}

// ContextCompactionMessageRange identifies the durable product-transcript
// interval represented by one compaction epoch without persisting its text.
type ContextCompactionMessageRange struct {
	StartIndex int    `json:"startIndex"`
	EndIndex   int    `json:"endIndex"`
	Count      int    `json:"count"`
	Hash       string `json:"hash"`
}

// ContextCompactionReceipt is the bounded audit projection for a persisted
// compaction epoch. Summary and transcript bodies remain in their owning
// stores; only content-addressed refs and counts cross the runtime boundary.
type ContextCompactionReceipt struct {
	SchemaVersion          string                        `json:"schemaVersion"`
	Epoch                  int                           `json:"epoch"`
	Phase                  string                        `json:"phase"`
	StableContextHash      string                        `json:"stableContextHash"`
	CompressedMessageRange ContextCompactionMessageRange `json:"compressedMessageRange"`
	RetainedRecentTurns    int                           `json:"retainedRecentTurns"`
	SummaryArtifactRef     string                        `json:"summaryArtifactRef"`
	TokensBefore           int                           `json:"tokensBefore"`
	TokensAfter            int                           `json:"tokensAfter"`
	Hash                   string                        `json:"hash"`
}

// ContextCompactionReceiptReporter lets the runtime record the current epoch
// before and after a run without depending on SessionConversation internals.
type ContextCompactionReceiptReporter interface {
	LatestContextCompactionReceipt() (ContextCompactionReceipt, bool)
}

type contextCompactionRangeHashIdentity struct {
	Count      int    `json:"count"`
	EndIndex   int    `json:"endIndex"`
	Hash       string `json:"hash"`
	StartIndex int    `json:"startIndex"`
}

// Field order is lexicographic so encoding/json matches the shared
// JavaScript canonical serializer used by the main-side receipt store.
type contextCompactionHashIdentity struct {
	CompressedMessageRange contextCompactionRangeHashIdentity `json:"compressedMessageRange"`
	Epoch                  int                                `json:"epoch"`
	Phase                  string                             `json:"phase"`
	RetainedRecentTurns    int                                `json:"retainedRecentTurns"`
	SchemaVersion          string                             `json:"schemaVersion"`
	StableContextHash      string                             `json:"stableContextHash"`
	SummaryArtifactRef     string                             `json:"summaryArtifactRef"`
	TokensAfter            int                                `json:"tokensAfter"`
	TokensBefore           int                                `json:"tokensBefore"`
}

type stableContextHashIdentity struct {
	Content string `json:"content"`
	Title   string `json:"title"`
}

func contextReceiptSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("sha256:%x", digest[:])
}

func contextReceiptJSONHash(value any) (string, bool) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return contextReceiptSHA256(encoded), true
}

func contextCompactionReceiptHash(receipt ContextCompactionReceipt) (string, bool) {
	return contextReceiptJSONHash(contextCompactionHashIdentity{
		CompressedMessageRange: contextCompactionRangeHashIdentity{
			Count:      receipt.CompressedMessageRange.Count,
			EndIndex:   receipt.CompressedMessageRange.EndIndex,
			Hash:       receipt.CompressedMessageRange.Hash,
			StartIndex: receipt.CompressedMessageRange.StartIndex,
		},
		Epoch:               receipt.Epoch,
		Phase:               receipt.Phase,
		RetainedRecentTurns: receipt.RetainedRecentTurns,
		SchemaVersion:       receipt.SchemaVersion,
		StableContextHash:   receipt.StableContextHash,
		SummaryArtifactRef:  receipt.SummaryArtifactRef,
		TokensAfter:         receipt.TokensAfter,
		TokensBefore:        receipt.TokensBefore,
	})
}

func validContextReceiptSHA256(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validContextCompactionReceipt(receipt ContextCompactionReceipt) bool {
	if receipt.SchemaVersion != "1" || receipt.Epoch <= 0 || receipt.Epoch > contextReceiptMaxNumber || (receipt.Phase != contextCompactionPhasePreRun && receipt.Phase != contextCompactionPhaseMidRun) {
		return false
	}
	rangeValue := receipt.CompressedMessageRange
	if rangeValue.StartIndex < 0 || rangeValue.EndIndex < rangeValue.StartIndex || rangeValue.EndIndex > contextReceiptMaxNumber || rangeValue.Count != rangeValue.EndIndex-rangeValue.StartIndex {
		return false
	}
	if receipt.RetainedRecentTurns < 0 || receipt.RetainedRecentTurns > contextReceiptMaxNumber || receipt.TokensBefore < 0 || receipt.TokensBefore > contextReceiptMaxNumber || receipt.TokensAfter < 0 || receipt.TokensAfter > contextReceiptMaxNumber || receipt.TokensAfter > receipt.TokensBefore {
		return false
	}
	for _, value := range []string{receipt.StableContextHash, rangeValue.Hash, receipt.SummaryArtifactRef, receipt.Hash} {
		if !validContextReceiptSHA256(value) {
			return false
		}
	}
	want, ok := contextCompactionReceiptHash(receipt)
	return ok && want == receipt.Hash
}

// RunTraceMetadata is the bounded interactive identity attached to one run.
// A Conversation may fill fields such as TurnID only after its final output is
// committed, so the runtime resolves it again during finish.
type RunTraceMetadata struct {
	StoryID         string `json:"story_id,omitempty"`
	BranchID        string `json:"branch_id,omitempty"`
	TurnID          string `json:"turn_id,omitempty"`
	MaintenanceTask string `json:"maintenance_task,omitempty"`
}

type RunTraceMetadataReporter interface {
	RunTraceMetadata() RunTraceMetadata
}

// InteractiveNarrativeReadinessReporter marks the protocol boundary after a
// Game Agent has successfully staged its hidden TurnResult and may emit prose.
type InteractiveNarrativeReadinessReporter interface {
	InteractiveNarrativeReady() bool
}

type SessionConversation struct {
	session               *session.Session
	cfg                   *config.Config
	agentKind             string
	stableContextTitle    string
	stableContext         string
	dynamicContextTitle   string
	dynamicContext        string
	userMessageReferences []session.UserMessageReference
}

func (c *SessionConversation) SetUserMessageReferences(references []session.UserMessageReference) {
	if c == nil {
		return
	}
	c.userMessageReferences = append([]session.UserMessageReference(nil), references...)
}

func NewSessionConversation(sess *session.Session, options ...SessionConversationOption) *SessionConversation {
	c := &SessionConversation{session: sess}
	for _, option := range options {
		if option != nil {
			option(c)
		}
	}
	return c
}

func NewSessionConversationForAgent(sess *session.Session, cfg *config.Config, agentKind string) *SessionConversation {
	return NewSessionConversation(
		sess,
		WithSessionContextConfig(cfg, agentKind),
	)
}

func NewSessionConversationForAgentWithRuntimeContext(sess *session.Session, cfg *config.Config, agentKind, title, content string) *SessionConversation {
	return NewSessionConversation(
		sess,
		WithSessionContextConfig(cfg, agentKind),
		WithSessionRuntimeContext(title, content),
	)
}

func NewSessionConversationForAgentWithRuntimeContexts(sess *session.Session, cfg *config.Config, agentKind, stableTitle, stableContent, dynamicTitle, dynamicContent string) *SessionConversation {
	return NewSessionConversation(
		sess,
		WithSessionContextConfig(cfg, agentKind),
		WithSessionStableRuntimeContext(stableTitle, stableContent),
		WithSessionRuntimeContext(dynamicTitle, dynamicContent),
	)
}

type SessionConversationOption func(*SessionConversation)

func WithSessionContextConfig(cfg *config.Config, agentKind string) SessionConversationOption {
	return func(c *SessionConversation) {
		c.cfg = cfg
		c.agentKind = agentKind
	}
}

func WithSessionRuntimeContext(title, content string) SessionConversationOption {
	return func(c *SessionConversation) {
		c.dynamicContextTitle = title
		c.dynamicContext = content
	}
}

func WithSessionStableRuntimeContext(title, content string) SessionConversationOption {
	return func(c *SessionConversation) {
		c.stableContextTitle = title
		c.stableContext = content
	}
}

func (c *SessionConversation) PrepareMessages(originalMessage, agentMessage string) ([]*schema.Message, error) {
	if c == nil || c.session == nil {
		return nil, fmt.Errorf("会话不存在")
	}
	if err := c.session.AppendWithMetadata(schema.UserMessage(originalMessage), session.MessageMetadata{UserReferences: c.userMessageReferences}); err != nil {
		return nil, err
	}
	result, err := agentcontext.Build(context.Background(), agentcontext.Request{
		Messages: c.modelMessages(agentMessage),
		Sources:  c.runtimeContextSources(),
	})
	if err != nil {
		return nil, err
	}
	return result.Messages, nil
}

func (c *SessionConversation) ContextSourceSummary() string {
	if c == nil || (strings.TrimSpace(c.stableContext) == "" && strings.TrimSpace(c.dynamicContext) == "") {
		return ""
	}
	return agentcontext.SourceSummary(c.runtimeContextSources(), defaultContextLedgerPreviewChars)
}

func (c *SessionConversation) CompactContextIfNeeded(ctx context.Context, input ContextCompactionInput) ([]*schema.Message, ContextCompactionResult, error) {
	policy := c.compactionPolicy()
	input = withDefaultContextProjectionReserves(c.cfg, c.agentKind, input, 0)
	phase := strings.TrimSpace(input.Phase)
	if phase == "" {
		phase = contextCompactionPhasePreRun
	}
	tokensBefore := EstimateContextTokens(input.Messages, input.Tools)
	projectedTokensBefore := projectedContextTokens(tokensBefore, input)
	result := ContextCompactionResult{
		Phase:                    phase,
		TokensBefore:             tokensBefore,
		ProjectedTokensBefore:    projectedTokensBefore,
		ReservedCompletionTokens: input.ReservedCompletionTokens,
		ReservedToolResultTokens: input.ReservedToolResultTokens,
		ContextWindowTokens:      policy.ContextWindowTokens,
		Strategy:                 policy.Strategy,
		Threshold:                policy.Threshold,
		MessageCountBefore:       len(input.Messages),
		RetainedTurns:            policy.RetainedTurns,
	}
	shouldCompact, skipped := policy.shouldCompact(projectedTokensBefore, input.Force)
	if !shouldCompact {
		result.SkippedReason = skipped
		return input.Messages, result, nil
	}
	source, existingCheckpoint, sourceStart, sourceEnd := c.compactionIncrementalSource(input.KeepLatestUser)
	if strings.TrimSpace(input.ExistingCheckpoint) != "" {
		existingCheckpoint = input.ExistingCheckpoint
	}
	if len(source) == 0 && strings.TrimSpace(existingCheckpoint) == "" && strings.TrimSpace(input.ReferenceContext) == "" {
		result.SkippedReason = "empty_source"
		return input.Messages, result, nil
	}
	if !input.Force {
		if removal, ok := c.session.LatestContextCompactionRemoval(c.agentKind); ok && removal.SourceStartIndex == sourceStart && removal.SourceEndIndex >= sourceEnd {
			result.SkippedReason = "removed_same_source"
			return input.Messages, result, nil
		}
	}
	sourceTokens := EstimateContextTokens(source, nil)
	emitContextCompactionEvent(input.Emit, phase, "started", result)
	summary, inputChars, err := summarizeContextForCompaction(ctx, c.cfg, c.agentKind, existingCheckpoint, source, input.ReferenceContext, sourceTokens, policy, func(attempt int, delta string) {
		emitContextCompactionDeltaEvent(input.Emit, phase, result, attempt, delta)
	})
	if err != nil {
		emitContextCompactionEvent(input.Emit, phase, "failed", result)
		return input.Messages, result, err
	}
	epoch := c.nextCompactionEpoch()
	leading, compactableMessages := c.splitLeadingRuntimeMessages(input.Messages)
	newMessages := compactMessagesForModel(compactableMessages, summary, epoch, policy.RetainedTurns)
	if len(leading) > 0 {
		newMessages = append(append([]*schema.Message(nil), leading...), newMessages...)
	}
	result.Triggered = true
	result.Epoch = epoch
	result.Summary = summary
	result.TokensAfter = EstimateContextTokens(newMessages, input.Tools)
	result.ProjectedTokensAfter = projectedContextTokens(result.TokensAfter, input)
	result.TargetRatio = contextCompactionRatio(countRunes(summary), inputChars)
	result.SourceMessageCount = len(source)
	result.MessageCountAfter = len(newMessages)
	record := contextCompactionRecordFromResult(result, c.agentKind, sourceStart, sourceEnd, policy.RetainedTurns, summary)
	record, err = c.session.AppendContextCompaction(record)
	if err != nil {
		emitContextCompactionEvent(input.Emit, phase, "failed", result)
		return input.Messages, result, err
	}
	if record.Epoch != epoch {
		result.Epoch = record.Epoch
		newMessages = compactMessagesForModel(compactableMessages, summary, record.Epoch, policy.RetainedTurns)
		if len(leading) > 0 {
			newMessages = append(append([]*schema.Message(nil), leading...), newMessages...)
		}
		result.TokensAfter = EstimateContextTokens(newMessages, input.Tools)
		result.ProjectedTokensAfter = projectedContextTokens(result.TokensAfter, input)
		result.MessageCountAfter = len(newMessages)
	}
	emitContextCompactionEvent(input.Emit, phase, "completed", result)
	return newMessages, result, nil
}

func (c *SessionConversation) modelMessages(agentMessage string) []*schema.Message {
	history := append([]*schema.Message(nil), c.session.GetEffectiveMessages()...)
	policy := c.compactionPolicy()
	if compaction, ok := c.session.LatestContextCompaction(c.agentKind); ok && strings.TrimSpace(compaction.Summary) != "" {
		total := c.session.MessageCountTotal()
		effectiveStart := total - len(history)
		retainedTurns := compaction.RetainedTurns
		if retainedTurns <= 0 {
			retainedTurns = policy.RetainedTurns
		}
		tail := compactedMessagesAfterSource(history, effectiveStart, compaction.SourceEndIndex, retainedTurns)
		history = make([]*schema.Message, 0, 1+len(tail))
		history = append(history, NewContextCompactionSummaryMessage(compaction.Epoch, compaction.Summary))
		history = append(history, tail...)
	}
	history = applyToolResultContextPolicy(history, c.ToolResultContextPolicy())
	if len(history) > 0 {
		history[len(history)-1] = schema.UserMessage(agentMessage)
	}
	return history
}

func standaloneRuntimeContextMessage(title, content, note string) string {
	return agentcontext.StandaloneMessage(title, content, note)
}

func (c *SessionConversation) leadingRuntimeMessages() []*schema.Message {
	if c == nil || strings.TrimSpace(c.stableContext) == "" {
		return nil
	}
	content := standaloneRuntimeContextMessage(c.stableContextTitle, c.stableContext, "")
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return []*schema.Message{schema.UserMessage(content)}
}

func (c *SessionConversation) runtimeContextSources() []agentcontext.Source {
	if c == nil {
		return nil
	}
	var sources []agentcontext.Source
	if strings.TrimSpace(c.stableContext) != "" {
		title := strings.TrimSpace(c.stableContextTitle)
		if title == "" {
			title = "稳定上下文"
		}
		sources = append(sources, agentcontext.Source{
			Source:    "稳定上下文",
			Title:     title,
			Content:   c.stableContext,
			Placement: agentcontext.PlacementLeadingMessage,
			Included:  true,
			Note:      "prepended_to_model_messages",
		})
	}
	if strings.TrimSpace(c.dynamicContext) != "" {
		title := strings.TrimSpace(c.dynamicContextTitle)
		if title == "" {
			title = "本轮动态上下文"
		}
		sources = append(sources, agentcontext.Source{
			Source:    "本轮动态上下文",
			Title:     title,
			Content:   c.dynamicContext,
			Placement: agentcontext.PlacementFinalUserPrefix,
			Included:  true,
			Note:      "prepended_to_final_user_message",
		})
	}
	return sources
}

func (c *SessionConversation) splitLeadingRuntimeMessages(messages []*schema.Message) ([]*schema.Message, []*schema.Message) {
	leading := c.leadingRuntimeMessages()
	if len(leading) == 0 || len(messages) < len(leading) {
		return nil, messages
	}
	for i := range leading {
		if messages[i] == nil || leading[i] == nil || messages[i].Role != leading[i].Role || messages[i].Content != leading[i].Content {
			return nil, messages
		}
	}
	return messages[:len(leading)], messages[len(leading):]
}

func (c *SessionConversation) compactionPolicy() contextCompactionPolicy {
	if c == nil {
		return contextCompactionPolicy{}
	}
	agentKind := c.agentKind
	if strings.TrimSpace(agentKind) == "" {
		agentKind = config.AgentKindIDE
	}
	policy := resolveContextCompactionPolicy(c.cfg, agentKind)
	return policy
}

func (c *SessionConversation) nextCompactionEpoch() int {
	return c.session.NextContextCompactionEpoch(c.agentKind)
}

// LatestContextCompactionReceipt derives a content-free receipt from the
// persisted compaction record and its exact source transcript interval. The
// product transcript is read only to calculate a hash and is never copied into
// the returned value.
func (c *SessionConversation) LatestContextCompactionReceipt() (ContextCompactionReceipt, bool) {
	if c == nil || c.session == nil {
		return ContextCompactionReceipt{}, false
	}
	record, ok := c.session.LatestContextCompaction(c.agentKind)
	if !ok || record.Epoch <= 0 || record.Epoch > contextReceiptMaxNumber || strings.TrimSpace(record.Summary) == "" {
		return ContextCompactionReceipt{}, false
	}
	if record.Phase != contextCompactionPhasePreRun && record.Phase != contextCompactionPhaseMidRun {
		return ContextCompactionReceipt{}, false
	}
	messages := c.session.GetMessages()
	if record.SourceStartIndex < 0 || record.SourceEndIndex < record.SourceStartIndex || record.SourceEndIndex > len(messages) {
		return ContextCompactionReceipt{}, false
	}
	count := record.SourceEndIndex - record.SourceStartIndex
	if record.SourceMessageCount != count || record.RetainedTurns < 0 || record.RetainedTurns > contextReceiptMaxNumber || record.TokensBefore < 0 || record.TokensBefore > contextReceiptMaxNumber || record.TokensAfter < 0 || record.TokensAfter > contextReceiptMaxNumber || record.TokensAfter > record.TokensBefore {
		return ContextCompactionReceipt{}, false
	}
	stableContextHash, stableOK := contextReceiptJSONHash(stableContextHashIdentity{
		Content: c.stableContext,
		Title:   c.stableContextTitle,
	})
	rangeHash, rangeOK := contextReceiptJSONHash(messages[record.SourceStartIndex:record.SourceEndIndex])
	if !stableOK || !rangeOK {
		return ContextCompactionReceipt{}, false
	}
	receipt := ContextCompactionReceipt{
		SchemaVersion:     "1",
		Epoch:             record.Epoch,
		Phase:             record.Phase,
		StableContextHash: stableContextHash,
		CompressedMessageRange: ContextCompactionMessageRange{
			StartIndex: record.SourceStartIndex,
			EndIndex:   record.SourceEndIndex,
			Count:      count,
			Hash:       rangeHash,
		},
		RetainedRecentTurns: record.RetainedTurns,
		SummaryArtifactRef:  contextReceiptSHA256([]byte(record.Summary)),
		TokensBefore:        record.TokensBefore,
		TokensAfter:         record.TokensAfter,
	}
	hash, hashOK := contextCompactionReceiptHash(receipt)
	if !hashOK {
		return ContextCompactionReceipt{}, false
	}
	receipt.Hash = hash
	return receipt, true
}

func (c *SessionConversation) compactionIncrementalSource(keepLatestUser bool) ([]*schema.Message, string, int, int) {
	if c == nil || c.session == nil {
		return nil, "", 0, 0
	}
	messages := c.session.GetMessages()
	total := len(messages)
	sourceStart := total - c.session.MessageCountSinceClear()
	if sourceStart < 0 {
		sourceStart = 0
	}
	existingCheckpoint := ""
	if compaction, ok := c.session.LatestContextCompaction(c.agentKind); ok {
		existingCheckpoint = compaction.Summary
		if compaction.SourceEndIndex > sourceStart {
			sourceStart = compaction.SourceEndIndex
		}
	}
	if sourceStart > total {
		sourceStart = total
	}
	sourceEnd := total
	if !keepLatestUser && sourceEnd > sourceStart {
		sourceEnd--
	}
	if sourceEnd < sourceStart {
		sourceEnd = sourceStart
	}
	source := compactionSourceMessages(applyToolResultContextPolicy(messages[sourceStart:sourceEnd], c.ToolResultContextPolicy()), true)
	return source, existingCheckpoint, sourceStart, sourceEnd
}

func compactionSourceMessages(messages []*schema.Message, keepLatestUser bool) []*schema.Message {
	source := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		if isContextCompactionMessage(msg) {
			continue
		}
		source = append(source, sanitizeCompactionSourceMessage(msg))
	}
	if !keepLatestUser && len(source) > 0 && source[len(source)-1].Role == schema.User {
		source = source[:len(source)-1]
	}
	return source
}

func sanitizeCompactionSourceMessage(msg *schema.Message) *schema.Message {
	if msg == nil {
		return nil
	}
	copied := *msg
	copied.ReasoningContent = ""
	return &copied
}

func retainTailByUserTurns(messages []*schema.Message, retainedTurns int) []*schema.Message {
	if retainedTurns <= 0 {
		retainedTurns = config.DefaultContextCompactionRetainedTurns
	}
	if retainedTurns > config.MaxContextCompactionRetainedTurns {
		retainedTurns = config.MaxContextCompactionRetainedTurns
	}
	userCount := 0
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] == nil || messages[i].Role != schema.User {
			continue
		}
		userCount++
		if userCount == retainedTurns {
			start = i
			break
		}
	}
	if userCount < retainedTurns {
		return messages
	}
	return append([]*schema.Message(nil), messages[start:]...)
}

func (c *SessionConversation) AppendAssistant(content string) error {
	return c.AppendAssistantWithMetadata(content, "", session.MessageMetadata{})
}

func (c *SessionConversation) AppendAssistantWithMetadata(content, _ string, metadata session.MessageMetadata) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("会话不存在")
	}
	return c.session.AppendWithMetadata(schema.AssistantMessage(content, nil), metadata)
}

func (c *SessionConversation) AppendContextMessage(msg *schema.Message) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("会话不存在")
	}
	return c.session.AppendContextMessage(msg)
}

func (c *SessionConversation) ToolResultContextPolicy() ToolResultContextPolicy {
	if c == nil {
		return ToolResultContextPolicy{}
	}
	agentKind := c.agentKind
	if strings.TrimSpace(agentKind) == "" {
		agentKind = config.AgentKindIDE
	}
	return resolveToolResultContextPolicy(c.cfg, agentKind)
}

func (c *SessionConversation) AppendDisplayEvent(event session.DisplayEvent) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("会话不存在")
	}
	return c.session.AppendDisplayEvent(event)
}

func (c *SessionConversation) UpdateDisplayToolStatus(id, name, status string) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("会话不存在")
	}
	return c.session.UpdateDisplayToolStatus(id, name, status)
}

func (c *SessionConversation) AppendDisplayToolArgs(id, name, delta string) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("会话不存在")
	}
	return c.session.AppendDisplayToolArgs(id, name, delta)
}

func (c *SessionConversation) UpdateDisplayToolResult(id, name, status, result string) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("会话不存在")
	}
	return c.session.UpdateDisplayToolResult(id, name, status, result)
}

func (c *SessionConversation) UpdateDisplayToolIllustration(id, name string, illustration *session.ChapterIllustration) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("会话不存在")
	}
	return c.session.UpdateDisplayToolIllustration(id, name, illustration)
}

func (c *SessionConversation) MarkInterrupted(userMessage, assistantContent, reason string) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("会话不存在")
	}
	return c.session.MarkInterrupted(userMessage, assistantContent, reason)
}

func (c *SessionConversation) PendingInterruption() *session.Interruption {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.PendingInterruption()
}

func (c *SessionConversation) ResolveInterruption(id string) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("会话不存在")
	}
	return c.session.ResolveInterruption(id)
}
