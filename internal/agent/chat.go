package agent

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"denova/internal/book"
	"denova/internal/observability"
	"denova/internal/prompts"
	"denova/internal/session"
)

const (
	maxReferenceFileBytes  = 128 * 1024
	maxReferenceTotalBytes = 200 * 1024
)

// Event 表示 Agent 输出的传输无关事件。
type Event struct {
	Type string
	Data interface{}
}

var contextCompactionEventStringFields = []string{
	"phase", "status", "strategy", "skipped_reason",
}

var contextCompactionEventNumberFields = []string{
	"attempt", "tokens_before", "projected_tokens_before", "reserved_completion_tokens",
	"reserved_tool_result_tokens", "tokens_after", "projected_tokens_after",
	"context_window_tokens", "threshold", "target_ratio", "epoch",
	"source_message_count", "message_count_before", "message_count_after",
}

func boundedContextCompactionNumber(value any) (any, bool) {
	switch typed := value.(type) {
	case int:
		return typed, typed >= 0 && typed <= contextReceiptMaxNumber
	case int8:
		return typed, typed >= 0
	case int16:
		return typed, typed >= 0
	case int32:
		return typed, typed >= 0 && typed <= contextReceiptMaxNumber
	case int64:
		return typed, typed >= 0 && typed <= contextReceiptMaxNumber
	case uint:
		return typed, typed <= contextReceiptMaxNumber
	case uint8:
		return typed, true
	case uint16:
		return typed, true
	case uint32:
		return typed, uint64(typed) <= contextReceiptMaxNumber
	case uint64:
		return typed, typed <= contextReceiptMaxNumber
	case float32:
		value64 := float64(typed)
		return typed, !math.IsNaN(value64) && !math.IsInf(value64, 0) && value64 >= 0 && value64 <= contextReceiptMaxNumber
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0) && typed >= 0 && typed <= contextReceiptMaxNumber
	default:
		return nil, false
	}
}

func boundedContextCompactionEvent(event Event) Event {
	if event.Type != "context_compaction" {
		return event
	}
	var raw map[string]any
	switch data := event.Data.(type) {
	case map[string]any:
		raw = make(map[string]any, len(data))
		for key, value := range data {
			raw[key] = value
		}
	case map[string]string:
		raw = make(map[string]any, len(data))
		for key, value := range data {
			raw[key] = value
		}
	default:
		return Event{Type: event.Type, Data: map[string]any{"status": "invalid_projection"}}
	}
	values := make(map[string]any, len(contextCompactionEventStringFields)+len(contextCompactionEventNumberFields)+4)
	for _, field := range contextCompactionEventStringFields {
		if value, ok := raw[field].(string); ok {
			if bounded := boundedContextReceiptLabel(value); bounded != "" {
				values[field] = bounded
			}
		}
	}
	for _, field := range contextCompactionEventNumberFields {
		if value, ok := boundedContextCompactionNumber(raw[field]); ok {
			values[field] = value
		}
	}
	if summary, ok := raw["summary"].(string); ok {
		if summary != "" {
			values["summary_artifact_ref"] = contextReceiptSHA256([]byte(summary))
			values["summary_chars"] = len([]rune(summary))
		}
	}
	if delta, ok := raw["delta"].(string); ok {
		if delta != "" {
			values["delta_hash"] = contextReceiptSHA256([]byte(delta))
			values["delta_chars"] = len([]rune(delta))
		}
	}
	return Event{Type: event.Type, Data: values}
}

// ChatRequest 表示一次聊天请求的传输无关参数。
type ChatRequest struct {
	Message        string             `json:"message"`
	References     []string           `json:"references"`
	LoreReferences []string           `json:"lore_references"`
	StyleScenes    []string           `json:"style_scenes"`
	Selections     []TextSelectionRef `json:"selections"`
	IDEContext     IDEContextRef      `json:"ide_context,omitempty"`
	ReviewFeedback ReviewFeedbackRefs `json:"review_feedback,omitempty"`
	PlanMode       bool               `json:"plan_mode"`
	WritingSkill   string             `json:"writing_skill"`
	ImagePresetID  string             `json:"image_preset_id"`
	TellerID       string             `json:"teller_id"`
	Locale         string             `json:"-"`

	// StyleRules 由后端按当前导演配置注入（场景 → 共享文风参考索引）。
	// StyleScenes 非空时只注入用户本轮通过 # 指定的场景；为空时作为场景化建议参与本轮上下文。
	StyleRules []StyleRule `json:"-"`

	// ImagePreset is resolved by the app layer from ImagePresetID or workspace settings.
	ImagePreset ImagePresetContext `json:"-"`

	// ResolvedReviewFeedback is populated by the app layer from a canonical
	// workspace review ledger. Clients may submit IDs only, never comment text.
	ResolvedReviewFeedback ReviewFeedbackContexts `json:"-"`
}

// StyleRule 是 prompts.StyleRule 的镜像，避免调用方直接依赖 prompts 包。
type StyleRule = prompts.StyleRule

// StyleReference 是 prompts.StyleReference 的镜像，避免调用方直接依赖 prompts 包。
type StyleReference = prompts.StyleReference

// IDEContextRef carries lightweight, model-visible IDE state for one turn.
// It must describe UI focus only and must not include editor file content.
type IDEContextRef struct {
	CurrentFile string   `json:"current_file,omitempty"`
	OpenFiles   []string `json:"open_files,omitempty"`
}

// ImagePresetContext is a bounded visual style preset for image generation only.
type ImagePresetContext struct {
	ID                string
	Name              string
	AgentSystemPrompt string
	ToolRequestPrompt string
}

// TextSelectionRef 表示用户在编辑器中选中的一段文本引用。
type TextSelectionRef struct {
	FileName  string `json:"file_name"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Content   string `json:"content"`
}

// ChatService 编排会话历史、文件引用和 Agent 流式响应。
type ChatService struct {
	policy  LoopPolicy
	runtime *Runtime
}

// Runtime owns the task-level Agent loop: context assembly, tool observation,
// durable run state, post-run verification, and final lifecycle events.
type Runtime struct {
	policy LoopPolicy
}

// NewChatService 创建聊天服务。
func NewChatService() *ChatService {
	return NewChatServiceWithPolicy(DefaultLoopPolicy())
}

// NewChatServiceWithPolicy 创建带显式 loop policy 的聊天服务，主要用于测试和后续分 Agent 配置。
func NewChatServiceWithPolicy(policy LoopPolicy) *ChatService {
	policy = policy.normalized()
	return &ChatService{policy: policy, runtime: NewRuntime(policy)}
}

func NewRuntime(policy LoopPolicy) *Runtime {
	return &Runtime{policy: policy.normalized()}
}

func (s *ChatService) RunWithOptions(
	ctx context.Context,
	runner *adk.Runner,
	conversation Conversation,
	bookService *book.Service,
	req ChatRequest,
	options RunOptions,
	emit func(Event),
) {
	runtime := NewRuntime(DefaultLoopPolicy())
	if s != nil {
		if s.runtime != nil {
			runtime = s.runtime
		} else {
			runtime = NewRuntime(s.policy)
		}
	}
	runtime.Run(ctx, runner, conversation, bookService, req, options, emit)
}

func (r *Runtime) Run(
	ctx context.Context,
	runner *adk.Runner,
	conversation Conversation,
	bookService *book.Service,
	req ChatRequest,
	options RunOptions,
	emit func(Event),
) {
	if emit == nil {
		emit = func(Event) {}
	}
	runLogger := observability.Logger("agent-run")
	policy := DefaultLoopPolicy()
	if r != nil {
		policy = r.policy.normalized()
	}
	workspace := ""
	if bookService != nil {
		workspace = bookService.Workspace()
	}
	options = options.normalized(workspace)
	originalMessage := req.Message
	var pendingInterruption *session.Interruption
	if shouldResumeInterruptedRequest(req.Message) {
		pendingInterruption = conversation.PendingInterruption()
	}
	contextBuildStarted := time.Now()
	composition := composeAgentInput(req, pendingInterruption, bookService, policy)
	req = composition.Request
	originalMessage = composition.OriginalMessage
	resumeInterruption := composition.ResumeInterruption
	var resumeLifecycle ResumeAttemptLifecycle
	var resumeAttempt ResumeAttemptIdentity
	if resumeInterruption != nil {
		if lifecycle, ok := conversation.(ResumeAttemptLifecycle); ok {
			attempt, err := lifecycle.BeginResumeAttempt(ctx, ResumeAttemptBasis{
				OperationID:    options.TaskID,
				InterruptionID: resumeInterruption.ID,
			})
			if err != nil {
				emit(Event{Type: "error", Data: map[string]string{"message": "无法开始恢复任务"}})
				return
			}
			resumeLifecycle = lifecycle
			resumeAttempt = attempt
		}
	}
	options.SystemPromptLog.logForRun(options)
	var authoritativeRunIDs []string
	if resumeLifecycle != nil {
		authoritativeRunIDs = append(authoritativeRunIDs, resumeAttempt.ExecutionRunID)
	}
	runLedger, ledgerErr := newRunLedgerWithOptions(workspace, policy.RunLedger, options, authoritativeRunIDs...)
	if ledgerErr != nil {
		runLogger.Warn("run_ledger_unavailable", slog.String("reason", "ledger_open_failed"))
	}
	rootSpan := StartRootTraceSpan(runLedger, map[string]any{
		"task_id":          options.TaskID,
		"agent_kind":       options.AgentKind,
		"session_id":       options.SessionID,
		"review_thread_id": options.ReviewThreadID,
		"story_id":         options.StoryID,
		"branch_id":        options.BranchID,
		"turn_id":          options.TurnID,
		"maintenance_task": options.MaintenanceTask,
		"mode":             options.Mode,
	})
	rootSpanID := ""
	if rootSpan != nil {
		rootSpanID = rootSpan.SpanID()
	}
	runID := strings.TrimSpace(resumeAttempt.ExecutionRunID)
	if runID == "" && runLedger != nil {
		runID = runLedger.ID()
	}
	if runID == "" {
		runID = options.TaskID
	}
	traceCtx := ContextWithRunTrace(ctx, runID, runLedger, rootSpanID)
	checkpointID := options.checkpointID(runID)
	observer := newRunObserverWithIdentity(runLedger, rootSpanID, runID, options.SessionID, options.ReviewThreadID)
	usageCollector := newRunTokenUsageCollector(runID, options.AgentKind)
	if runLedger != nil {
		defer func() {
			if err := runLedger.Close(); err != nil {
				runLogger.Warn("run_ledger_close_failed", slog.String("run_id", runID))
			}
		}()
	}
	var runContextReceiptParts []ContextLedgerPart
	contextReceiptStarted := false
	recordContextReceipt := func(phase string) {
		var compactionReceipt ContextCompactionReceipt
		hasCompaction := false
		if reporter, ok := conversation.(ContextCompactionReceiptReporter); ok {
			compactionReceipt, hasCompaction = reporter.LatestContextCompactionReceipt()
		}
		if err := runLedger.RecordContextReceipt(phase, runContextReceiptParts, compactionReceipt, hasCompaction); err != nil {
			runLogger.Warn("run_ledger_context_receipt_failed", slog.String("run_id", runID), slog.String("phase", phase))
		}
	}
	finished := false
	finishRun := func(status, reason string, generatedBytes int) {
		if finished {
			return
		}
		finished = true
		if contextReceiptStarted {
			recordContextReceipt("after_run")
		}
		usageCollector.EmitIfAny(emit, generatedBytes)
		traceMetadata := runTraceMetadataForConversation(options, conversation)
		if !traceMetadata.empty() {
			if err := runLedger.Record("run_context", traceMetadata.record()); err != nil {
				runLogger.Warn("run_ledger_context_metadata_failed", slog.String("run_id", runID))
			}
		}
		if rootSpan != nil {
			rootSpan.Finish(status, map[string]any{
				"reason":           strings.TrimSpace(reason),
				"generated_bytes":  generatedBytes,
				"story_id":         traceMetadata.StoryID,
				"branch_id":        traceMetadata.BranchID,
				"turn_id":          traceMetadata.TurnID,
				"maintenance_task": traceMetadata.MaintenanceTask,
			})
		}
		if err := runLedger.RecordFinish(status, reason, generatedBytes); err != nil {
			runLogger.Warn("run_ledger_finish_failed", slog.String("run_id", runID))
		}
	}

	assistantMetadata := session.MessageMetadata{
		RunID:         runID,
		AgentKind:     options.AgentKind,
		AgentName:     options.RootAgentName,
		RootAgentName: options.RootAgentName,
	}
	if options.RootAgentName != "" {
		assistantMetadata.RunPath = []string{options.RootAgentName}
	}
	subAgentSessions := newSubAgentSessionTracker(runID)
	recorder := newDisplayEventRecorder(conversation)
	toolContextRecorder := newToolResultContextRecorder(conversation)
	mutations := newMutationTracker()
	rawEmit := emit
	emit = func(ev Event) {
		ev = boundedContextCompactionEvent(ev)
		mutations.Observe(ev)
		if displayEvent := boundedDisplayProjectionEvent(ev); displayEvent.Type != "" {
			recorder.Record(displayEvent)
		}
		if err := runLedger.RecordEvent(ev); err != nil {
			runLogger.Warn("run_ledger_event_failed", slog.String("run_id", runID), slog.String("event_type", ev.Type))
		}
		rawEmit(ev)
	}
	emit(Event{Type: "run_state", Data: map[string]string{
		"run_id":           runID,
		"task_id":          options.TaskID,
		"agent_kind":       options.AgentKind,
		"session_id":       options.SessionID,
		"review_thread_id": options.ReviewThreadID,
		"story_id":         options.StoryID,
		"branch_id":        options.BranchID,
		"turn_id":          options.TurnID,
		"maintenance_task": options.MaintenanceTask,
		"root_agent_name":  options.RootAgentName,
		"phase":            "started",
	}})
	if err := runLedger.Record("run_started", map[string]any{
		"task_id":          options.TaskID,
		"agent_kind":       options.AgentKind,
		"session_id":       options.SessionID,
		"review_thread_id": options.ReviewThreadID,
		"story_id":         options.StoryID,
		"branch_id":        options.BranchID,
		"turn_id":          options.TurnID,
		"maintenance_task": options.MaintenanceTask,
		"mode":             options.Mode,
		"message":          runLedger.summarizeText(originalMessage),
		"references":       len(req.References),
		"lore_references":  len(req.LoreReferences),
		"style_scenes":     len(req.StyleScenes),
		"selections":       len(req.Selections),
		"plan_mode":        req.PlanMode,
		"writing_skill":    req.WritingSkill,
		"checkpoint_id":    checkpointID,
	}); err != nil {
		runLogger.Warn("run_ledger_start_failed", slog.String("run_id", runID))
	}
	var resumeFinishGuard *resumeAttemptFinishGuard
	if resumeLifecycle != nil {
		resumeFinishGuard = newResumeAttemptFinishGuard(ctx, resumeLifecycle, resumeAttempt)
	}
	finishResumeAttempt := func(outcome ResumeAttemptFinishOutcome) (ResumeAttemptFinishReceipt, error) {
		if resumeFinishGuard == nil {
			return ResumeAttemptFinishReceipt{}, nil
		}
		return resumeFinishGuard.Finish(outcome)
	}
	finishTermination := func(cause RunTerminationCause, generatedBytes int) error {
		decision, ok := ClassifyRunTermination(cause)
		if !ok {
			return errors.New("run termination cause is invalid")
		}
		_, finishErr := finishResumeAttempt(decision.ResumeOutcome)
		finishRun(decision.Status, decision.Reason, generatedBytes)
		return finishErr
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			runLogger.Error("panic_recovered", slog.String("reason", "panic"))
			if finishErr := finishTermination(RunTerminationPanic, 0); finishErr != nil {
				runLogger.Error("resume_attempt_finish_failed")
			}
			markInterruptionIfNeeded(conversation, resumeInterruption, originalMessage, "", "panic")
			emit(Event{Type: "error", Data: map[string]string{"message": "Agent 异常中断"}})
			return
		}
		if resumeFinishGuard != nil && !resumeFinishGuard.Finished() {
			outcome := ResumeAttemptOutcomeRunnerFailed
			if ctx.Err() != nil {
				outcome = ResumeAttemptOutcomeCancelled
			}
			if _, finishErr := finishResumeAttempt(outcome); finishErr != nil {
				runLogger.Error("resume_attempt_finish_failed")
			}
		}
	}()

	agentMessage := composition.AgentMessage
	contextLog := composition.ContextLog
	if setter, ok := conversation.(UserMessageReferencesSetter); ok {
		setter.SetUserMessageReferences(userMessageReferencesForRequest(req))
	}

	history, err := conversation.PrepareMessages(originalMessage, agentMessage)
	if err != nil {
		runLogger.Error("prepare_messages_failed")
		if _, finishErr := finishResumeAttempt(ResumeAttemptOutcomePrepareFailed); finishErr != nil {
			runLogger.Error("resume_attempt_finish_failed")
		}
		finishRun("error", "prepare_messages_failed", 0)
		emit(Event{Type: "error", Data: map[string]string{"message": "无法准备模型上下文"}})
		return
	}
	runContextReceiptParts = contextLedgerPartsForConversation(contextLog, conversation, history)
	contextReceiptStarted = true
	recordContextReceipt("before_run")
	if options.OnUserMessageCommitted != nil {
		if err := options.OnUserMessageCommitted(traceCtx); err != nil {
			runLogger.Error("commit_user_message_side_effect_failed")
			if _, finishErr := finishResumeAttempt(ResumeAttemptOutcomeUserCommitFailed); finishErr != nil {
				runLogger.Error("resume_attempt_finish_failed")
			}
			finishRun("error", "user_message_commit_failed", 0)
			emit(Event{Type: "error", Data: map[string]string{"message": "用户消息提交后处理失败"}})
			return
		}
		emit(Event{Type: "workspace_change", Data: map[string]interface{}{
			"workspace":        options.Workspace,
			"review_thread_id": options.ReviewThreadID,
			"action":           "review_feedback_consumed",
		}})
	}
	if compactor, ok := conversation.(ContextCompactionConversation); ok {
		compactionStarted := time.Now()
		compactedHistory, compactionResult, compactErr := compactor.CompactContextIfNeeded(ContextWithRunObserver(traceCtx, observer), ContextCompactionInput{
			Messages:     history,
			AgentMessage: agentMessage,
			Phase:        contextCompactionPhasePreRun,
			Emit:         emit,
		})
		if compactErr != nil {
			runLogger.Error("context_compaction_failed", slog.Int("tokens_before", compactionResult.TokensBefore), slog.Int("context_window_tokens", compactionResult.ContextWindowTokens))
			if _, finishErr := finishResumeAttempt(ResumeAttemptOutcomeCompactionFailed); finishErr != nil {
				runLogger.Error("resume_attempt_finish_failed")
			}
			finishRun("error", "context_compaction_failed", 0)
			emit(Event{Type: "error", Data: map[string]string{"message": "上下文压缩失败"}})
			return
		}
		history = compactedHistory
		if compactionResult.Triggered {
			runLogger.Info("context_compacted", slog.String("phase", compactionResult.Phase), slog.Int("epoch", compactionResult.Epoch), slog.Int("tokens_before", compactionResult.TokensBefore), slog.Int("projected_tokens_before", compactionResult.ProjectedTokensBefore), slog.Int("tokens_after", compactionResult.TokensAfter), slog.Int("projected_tokens_after", compactionResult.ProjectedTokensAfter), slog.Int("context_window_tokens", compactionResult.ContextWindowTokens))
			if err := runLedger.Record("context_compaction", map[string]any{
				"phase":                       compactionResult.Phase,
				"epoch":                       compactionResult.Epoch,
				"tokens_before":               compactionResult.TokensBefore,
				"projected_tokens_before":     compactionResult.ProjectedTokensBefore,
				"reserved_completion_tokens":  compactionResult.ReservedCompletionTokens,
				"reserved_tool_result_tokens": compactionResult.ReservedToolResultTokens,
				"tokens_after":                compactionResult.TokensAfter,
				"projected_tokens_after":      compactionResult.ProjectedTokensAfter,
				"context_window_tokens":       compactionResult.ContextWindowTokens,
				"threshold":                   compactionResult.Threshold,
			}); err != nil {
				runLogger.Warn("run_ledger_context_compaction_failed", slog.String("run_id", runID))
			}
			RecordCompletedTraceSpan(traceCtx, "context_compaction", compactionStarted, "success", map[string]any{
				"phase":                   compactionResult.Phase,
				"epoch":                   compactionResult.Epoch,
				"tokens_before":           compactionResult.TokensBefore,
				"projected_tokens_before": compactionResult.ProjectedTokensBefore,
				"tokens_after":            compactionResult.TokensAfter,
				"projected_tokens_after":  compactionResult.ProjectedTokensAfter,
				"context_window_tokens":   compactionResult.ContextWindowTokens,
				"threshold":               compactionResult.Threshold,
			})
		}
	}
	runContextReceiptParts = contextLedgerPartsForConversation(contextLog, conversation, history)
	if err := runLedger.RecordContext(runContextReceiptParts); err != nil {
		runLogger.Warn("run_ledger_context_failed", slog.String("run_id", runID))
	}
	RecordCompletedTraceSpan(traceCtx, "context_build", contextBuildStarted, "success", map[string]any{
		"history_messages":    len(history),
		"context_parts":       len(runContextReceiptParts),
		"message_chars":       len([]rune(originalMessage)),
		"agent_message_chars": len([]rune(agentMessage)),
		"plan_mode":           req.PlanMode,
		"writing_skill":       req.WritingSkill,
	})
	contextBytes := 0
	contextChars := 0
	for _, part := range runContextReceiptParts {
		contextBytes += part.Bytes
		contextChars += part.Chars
	}
	runLogger.Info(
		"context_composition",
		slog.Int("history_messages", len(history)),
		slog.Int("original_bytes", len(originalMessage)),
		slog.Int("original_chars", len([]rune(originalMessage))),
		slog.Int("agent_message_bytes", len(agentMessage)),
		slog.Int("agent_message_chars", len([]rune(agentMessage))),
		slog.Int("references", len(req.References)),
		slog.Int("lore_references", len(req.LoreReferences)),
		slog.Int("style_scenes", len(req.StyleScenes)),
		slog.Int("style_rules", len(req.StyleRules)),
		slog.Int("selections", len(req.Selections)),
		slog.Int("context_parts", len(runContextReceiptParts)),
		slog.Int("context_bytes", contextBytes),
		slog.Int("context_chars", contextChars),
		slog.Bool("plan_mode", req.PlanMode),
		slog.Bool("writing_skill_selected", strings.TrimSpace(req.WritingSkill) != ""),
		slog.Bool("resumed", resumeInterruption != nil),
	)

	runCtx, cancelRun := context.WithCancel(contextWithCompactionController(ContextWithRunObserver(traceCtx, observer), conversation))
	defer cancelRun()
	runOptions := []adk.AgentRunOption{}
	if options.AgentKind == AgentKindInteractiveStory {
		cancelOption, cancelAgent := adk.WithCancel()
		runCtx = withInteractiveTurnCancel(runCtx, cancelAgent)
		runOptions = append(runOptions, cancelOption)
	} else if isInteractiveDirectorPlanRun(options.AgentKind, options.MaintenanceTask) {
		cancelOption, cancelAgent := adk.WithCancel()
		runCtx = withInteractiveDirectorPlanCancel(runCtx, cancelAgent)
		runOptions = append(runOptions, cancelOption)
	}
	if checkpointID != "" {
		runOptions = append(runOptions, adk.WithCheckPointID(checkpointID))
	}
	events := runner.Run(runCtx, history, runOptions...)
	var fullContent strings.Builder
	var fullThinking strings.Builder
	var planParser *planProtocolParser
	if req.PlanMode {
		planMeta := agentEventMetadata{
			AgentKind:     options.AgentKind,
			RunID:         runID,
			AgentName:     options.RootAgentName,
			RootAgentName: options.RootAgentName,
		}
		if options.RootAgentName != "" {
			planMeta.RunPath = []string{options.RootAgentName}
		}
		planParser = newPlanProtocolParser(planMeta, emit)
	}
	runLogger.Info("run_started", slog.Int("history", len(history)), slog.Int("message_len", len(req.Message)), slog.Int("agent_message_len", len(agentMessage)), slog.Bool("plan_mode", req.PlanMode), slog.Bool("writing_skill_selected", strings.TrimSpace(req.WritingSkill) != ""), slog.Int("style_scenes", len(req.StyleScenes)), slog.Int("style_rules", len(req.StyleRules)))

	for {
		if err := ctx.Err(); err != nil {
			runLogger.Warn("run_interrupted", slog.String("reason", "cancelled"), slog.Int("generated_bytes", fullContent.Len()))
			flushPlanProtocolParser(planParser, &fullContent, emit)
			discardPlanAssistantContentIfNeeded(req.PlanMode, planParser, &fullContent, &fullThinking)
			generatedBytes := fullContent.Len()
			if _, persistErr := appendAssistantIfAny(conversation, &fullContent, &fullThinking, assistantMetadata); persistErr != nil {
				runLogger.Error("persist_interrupted_assistant_failed")
			}
			if finishErr := finishTermination(RunTerminationUserCancelled, generatedBytes); finishErr != nil {
				runLogger.Error("resume_attempt_finish_failed")
			}
			emit(Event{Type: "aborted", Data: map[string]string{}})
			return
		}
		event, ok, waitErr := waitForRunnerEvent(runCtx, events, options.IdleTimeout)
		if waitErr != nil {
			flushPlanProtocolParser(planParser, &fullContent, emit)
			discardPlanAssistantContentIfNeeded(req.PlanMode, planParser, &fullContent, &fullThinking)
			generated, persistErr := appendAssistantIfAny(conversation, &fullContent, &fullThinking, assistantMetadata)
			if persistErr != nil {
				runLogger.Error("persist_interrupted_assistant_failed")
			}
			if ctx.Err() != nil {
				runLogger.Warn("run_interrupted", slog.String("reason", "cancelled"), slog.Int("generated_bytes", len(generated)))
				if finishErr := finishTermination(RunTerminationUserCancelled, len(generated)); finishErr != nil {
					runLogger.Error("resume_attempt_finish_failed")
				}
				emit(Event{Type: "aborted", Data: map[string]string{}})
				return
			}
			cancelRun()
			runLogger.Error("run_interrupted", slog.String("reason", "provider_idle_timeout"), slog.Int("generated_bytes", len(generated)))
			markInterruptionIfNeeded(conversation, resumeInterruption, originalMessage, generated, "provider_idle_timeout")
			if finishErr := finishTermination(RunTerminationProviderIdleTimeout, len(generated)); finishErr != nil {
				runLogger.Error("resume_attempt_finish_failed")
			}
			emit(Event{Type: "error", Data: map[string]string{"message": "模型服务长时间没有响应"}})
			return
		}
		if !ok {
			break
		}
		if event.Err != nil {
			if interactiveTurnCompletedByCancel(event.Err, options.AgentKind, conversation, fullContent.Len()) {
				if err := removeCheckpoint(options.Workspace, options.AgentKind, checkpointID); err != nil {
					runLogger.Warn("interactive_completion_checkpoint_cleanup_failed", slog.String("checkpoint_id", checkpointID))
				}
				runLogger.Info("interactive_turn_completed_after_submission", slog.Int("generated_bytes", fullContent.Len()))
				break
			}
			if interactiveDirectorPlanCompletedByCancel(event.Err, options.AgentKind, options.MaintenanceTask) {
				if err := removeCheckpoint(options.Workspace, options.AgentKind, checkpointID); err != nil {
					runLogger.Warn("interactive_director_completion_checkpoint_cleanup_failed", slog.String("checkpoint_id", checkpointID))
				}
				runLogger.Info("interactive_director_plan_completed_after_submission")
				break
			}
			if reason, retrying := interactiveCompletionRetryFromError(event.Err); retrying {
				runLogger.Info("interactive_completion_retry", slog.String("code", reason.Code), slog.Int("generated_bytes", fullContent.Len()))
				continue
			}
			runLogger.Error("run_interrupted", slog.String("reason", "provider_error"), slog.Int("generated_bytes", fullContent.Len()))
			flushPlanProtocolParser(planParser, &fullContent, emit)
			discardPlanAssistantContentIfNeeded(req.PlanMode, planParser, &fullContent, &fullThinking)
			generated, persistErr := appendAssistantIfAny(conversation, &fullContent, &fullThinking, assistantMetadata)
			if persistErr != nil {
				runLogger.Error("persist_interrupted_assistant_failed")
			}
			markInterruptionIfNeeded(conversation, resumeInterruption, originalMessage, generated, "provider_error")
			if finishErr := finishTermination(RunTerminationProviderError, len(generated)); finishErr != nil {
				runLogger.Error("resume_attempt_finish_failed")
			}
			emit(Event{Type: "error", Data: map[string]string{"message": "模型服务调用失败"}})
			return
		}

		if event.Output == nil || event.Output.MessageOutput == nil {
			runLogger.Warn("invalid_output_skipped", slog.Bool("output_nil", event.Output == nil), slog.Bool("message_output_nil", event.Output != nil && event.Output.MessageOutput == nil))
			continue
		}

		eventMeta := subAgentSessions.decorate(metadataForAgentEvent(event, options.RootAgentName))
		eventMeta.AgentKind = options.AgentKind
		mv := event.Output.MessageOutput
		if mv.Role == schema.Tool {
			if mv.Message == nil {
				continue
			}
			content, drainErr := drainContent(runCtx, mv, options.IdleTimeout)
			if drainErr != nil {
				discardPlanAssistantContentIfNeeded(req.PlanMode, planParser, &fullContent, &fullThinking)
				generated, persistErr := appendAssistantIfAny(conversation, &fullContent, &fullThinking, assistantMetadata)
				if persistErr != nil {
					runLogger.Error("persist_interrupted_assistant_failed")
				}
				if ctx.Err() != nil {
					runLogger.Warn("run_interrupted", slog.String("reason", "cancelled"), slog.Int("generated_bytes", len(generated)))
					if finishErr := finishTermination(RunTerminationUserCancelled, len(generated)); finishErr != nil {
						runLogger.Error("resume_attempt_finish_failed")
					}
					emit(Event{Type: "aborted", Data: map[string]string{}})
					return
				}
				cancelRun()
				runLogger.Error("run_interrupted", slog.String("reason", "provider_idle_timeout"), slog.Int("generated_bytes", len(generated)))
				markInterruptionIfNeeded(conversation, resumeInterruption, originalMessage, generated, "provider_idle_timeout")
				if finishErr := finishTermination(RunTerminationProviderIdleTimeout, len(generated)); finishErr != nil {
					runLogger.Error("resume_attempt_finish_failed")
				}
				emit(Event{Type: "error", Data: map[string]string{"message": "模型服务长时间没有响应"}})
				return
			}
			fullToolContent := content
			if content == "" {
				content = "(无返回内容)"
			}
			runLogger.Info(
				"tool_result",
				slog.String("tool", boundedContextReceiptLabel(mv.Message.ToolName)),
				slog.Int("tool_call_id_bytes", len(mv.Message.ToolCallID)),
				slog.Int("bytes", len(content)),
				slog.Int("chars", len([]rune(content))),
				slog.Bool("suspected_failure", looksLikeToolFailure(content)),
			)
			usageCollector.NoteToolResult(mv.Message.ToolName)
			data := eventMeta.appendTo(map[string]interface{}{
				"id":      mv.Message.ToolCallID,
				"name":    mv.Message.ToolName,
				"content": content,
			})
			if itemIDs, deletedIDs := parseWriteLoreItemsToolResult(mv.Message.ToolName, fullToolContent); len(itemIDs) > 0 || len(deletedIDs) > 0 {
				data["item_ids"] = itemIDs
				data["deleted_ids"] = deletedIDs
			}
			if illustrationResult, parseErr := parseChapterIllustrationToolResult(mv.Message.ToolName, fullToolContent); parseErr != nil {
				runLogger.Warn("parse_chapter_illustration_result_failed", slog.String("tool", boundedContextReceiptLabel(mv.Message.ToolName)))
			} else if illustrationResult != nil {
				data["illustration"] = illustrationResult
				data["target"] = illustrationResult.MetaPath
			} else if interactiveImageResult, parseErr := parseInteractiveImageToolResult(mv.Message.ToolName, fullToolContent); parseErr != nil {
				runLogger.Warn("parse_interactive_image_result_failed", slog.String("tool", boundedContextReceiptLabel(mv.Message.ToolName)))
			} else if interactiveImageResult != nil {
				data["interactive_image"] = interactiveImageResult
				data["target"] = interactiveImageResult.MetaPath
			} else if target := parseGeneratedImageToolTarget(mv.Message.ToolName, fullToolContent); target != "" {
				data["target"] = target
			}
			if receipt, ok := parseWorkspaceChangeToolReceipt(mv.Message.ToolName, fullToolContent); ok {
				data["workspace_change"] = receipt
				workspaceChangeData := eventMeta.appendTo(map[string]interface{}{
					"id":               receipt.ChangeSetID,
					"workspace":        receipt.Workspace,
					"change_group_id":  receipt.ChangeGroupID,
					"review_thread_id": receipt.ReviewThreadID,
					"change_set_id":    receipt.ChangeSetID,
					"path":             receipt.Path,
					"affected_paths":   []string{receipt.Path},
					"base_revision":    receipt.BaseRevision,
					"revision":         receipt.Revision,
					"review_status":    receipt.ReviewStatus,
					"apply_state":      receipt.ApplyState,
					"workspace_change": receipt,
				})
				emit(Event{Type: "workspace_change", Data: workspaceChangeData})
			}
			toolContextRecorder.RecordToolResult(mv.Message.ToolName, mv.Message.ToolCallID, content, eventMeta)
			emit(Event{Type: "tool_result", Data: data})
			continue
		}

		if mv.Role != schema.Assistant && mv.Role != "" {
			continue
		}
		if mv.IsStreaming && mv.MessageStream != nil {
			msg, streamErr := processStreamingEvent(runCtx, mv, &fullContent, &fullThinking, options.IdleTimeout, options.ToolResultMaxBytes, eventMeta, interactiveNarrativeReady(conversation, eventMeta), planParser, emit)
			if streamErr != nil {
				// A completion-guard retry arrives after all response frames. Preserve
				// the rejected call's provider usage even though its prose is discarded.
				usageCollector.AddMessage(msg)
				if reason, retrying := interactiveCompletionRetryFromError(streamErr); retrying {
					runLogger.Info("interactive_completion_retry", slog.String("code", reason.Code), slog.Int("generated_bytes", fullContent.Len()))
					continue
				}
				flushPlanProtocolParser(planParser, &fullContent, emit)
				discardPlanAssistantContentIfNeeded(req.PlanMode, planParser, &fullContent, &fullThinking)
				generated, persistErr := appendAssistantIfAny(conversation, &fullContent, &fullThinking, assistantMetadata)
				if persistErr != nil {
					runLogger.Error("persist_interrupted_assistant_failed")
				}
				if ctx.Err() != nil {
					runLogger.Warn("run_interrupted", slog.String("reason", "cancelled"), slog.Int("generated_bytes", len(generated)))
					if finishErr := finishTermination(RunTerminationUserCancelled, len(generated)); finishErr != nil {
						runLogger.Error("resume_attempt_finish_failed")
					}
					emit(Event{Type: "aborted", Data: map[string]string{}})
					return
				}
				cancelRun()
				cause := RunTerminationProviderError
				message := "模型服务调用失败"
				if strings.Contains(streamErr.Error(), "没有收到任何输出") {
					cause = RunTerminationProviderIdleTimeout
					message = "模型服务长时间没有响应"
				}
				decision, _ := ClassifyRunTermination(cause)
				markInterruptionIfNeeded(conversation, resumeInterruption, originalMessage, generated, decision.Reason)
				if finishErr := finishTermination(cause, len(generated)); finishErr != nil {
					runLogger.Error("resume_attempt_finish_failed")
				}
				emit(Event{Type: "error", Data: map[string]string{"message": message}})
				return
			}
			toolContextRecorder.RecordAssistantToolCalls(msg, eventMeta)
			usageCollector.AddMessage(msg)
			if req.PlanMode && planParser != nil && planParser.HasSuccessfulBlock() {
				cancelRun()
				break
			}
			continue
		}
		if mv.Message != nil {
			processNonStreamingEvent(mv, &fullContent, &fullThinking, options.ToolResultMaxBytes, eventMeta, interactiveNarrativeReady(conversation, eventMeta), planParser, emit)
			toolContextRecorder.RecordAssistantToolCalls(mv.Message, eventMeta)
			usageCollector.AddMessage(mv.Message)
			if req.PlanMode && planParser != nil && planParser.HasSuccessfulBlock() {
				cancelRun()
				break
			}
		}
	}

	flushPlanProtocolParser(planParser, &fullContent, emit)
	discardPlanAssistantContentIfNeeded(req.PlanMode, planParser, &fullContent, &fullThinking)
	generatedBytes := fullContent.Len()
	if _, persistErr := appendAssistantIfAny(conversation, &fullContent, &fullThinking, assistantMetadata); persistErr != nil {
		runLogger.Error("persist_assistant_failed", slog.Int("generated_bytes", generatedBytes))
		if _, finishErr := finishResumeAttempt(ResumeAttemptOutcomeAssistantPersistFailed); finishErr != nil {
			runLogger.Error("resume_attempt_finish_failed")
		}
		finishRun("error", "assistant_persistence_failed", generatedBytes)
		emit(Event{Type: "run_state", Data: map[string]string{
			"run_id":           runID,
			"task_id":          options.TaskID,
			"agent_kind":       options.AgentKind,
			"session_id":       options.SessionID,
			"review_thread_id": options.ReviewThreadID,
			"root_agent_name":  options.RootAgentName,
			"phase":            "finished",
			"status":           "error",
		}})
		emit(Event{Type: "error", Data: map[string]string{"message": "生成结果持久化失败"}})
		return
	}
	if resumeLifecycle != nil {
		receipt, err := finishResumeAttempt(ResumeAttemptOutcomeSucceeded)
		if err == nil && !validSuccessfulResumeFinishReceipt(resumeAttempt, receipt) {
			err = errors.New("resume attempt finish receipt invalid")
		}
		if err != nil {
			runLogger.Error("resume_attempt_finish_failed")
			finishRun("error", "resume attempt finish failed", generatedBytes)
			emit(Event{Type: "run_state", Data: map[string]string{
				"run_id":           runID,
				"task_id":          options.TaskID,
				"agent_kind":       options.AgentKind,
				"session_id":       options.SessionID,
				"review_thread_id": options.ReviewThreadID,
				"root_agent_name":  options.RootAgentName,
				"phase":            "finished",
				"status":           "error",
			}})
			emit(Event{Type: "error", Data: map[string]string{"message": "恢复任务状态保存失败"}})
			return
		}
	} else if resumeInterruption != nil {
		if err := conversation.ResolveInterruption(resumeInterruption.ID); err != nil {
			runLogger.Error("resolve_interruption_failed", slog.String("interruption_id", resumeInterruption.ID))
		}
	}
	observedMutations := mutations.Mutations()
	observer.RecordMutations(observedMutations)
	verification := VerifyPostRunMutations(bookService, observedMutations)
	observer.RecordVerification(verification)
	if options.OnMutationsVerified != nil && len(observedMutations) > 0 {
		options.OnMutationsVerified(ctx, observedMutations, verification)
	}
	if verification.Mutations > 0 {
		runLogger.Info("post_run_verification", slog.String("status", verification.Status), slog.Int("mutations", verification.Mutations), slog.Int("checks", len(verification.Checks)), slog.Int("warnings", len(verification.Warnings)))
		emit(Event{Type: "post_run_verification", Data: verification})
		emit(Event{Type: "verification", Data: verification})
	}
	runLogger.Info("run_completed")
	finishRun("completed", "completed", generatedBytes)
	emit(Event{Type: "run_state", Data: map[string]string{
		"run_id":           runID,
		"task_id":          options.TaskID,
		"agent_kind":       options.AgentKind,
		"session_id":       options.SessionID,
		"review_thread_id": options.ReviewThreadID,
		"root_agent_name":  options.RootAgentName,
		"phase":            "finished",
		"status":           "success",
	}})
	emit(Event{Type: "done", Data: map[string]string{}})
}

func validSuccessfulResumeFinishReceipt(attempt ResumeAttemptIdentity, receipt ResumeAttemptFinishReceipt) bool {
	return receipt.Outcome == ResumeAttemptOutcomeSucceeded &&
		receipt.InterruptionResolved &&
		receipt.Attempt.Status == ResumeAttemptStatusSucceeded &&
		receipt.Attempt.AttemptID == attempt.AttemptID &&
		receipt.Attempt.ExecutionRunID == attempt.ExecutionRunID &&
		receipt.Attempt.InterruptionID == attempt.InterruptionID &&
		receipt.Attempt.OriginRunID == attempt.OriginRunID &&
		receipt.Attempt.ParentAttemptID == attempt.ParentAttemptID &&
		receipt.Attempt.AttemptNumber == attempt.AttemptNumber
}

type resumeAttemptFinishState uint8

const (
	resumeAttemptFinishIdle resumeAttemptFinishState = iota
	resumeAttemptFinishInvoking
	resumeAttemptFinishUncertain
	resumeAttemptFinishComplete
)

type resumeAttemptFinishGuard struct {
	mu           sync.Mutex
	cond         *sync.Cond
	lifecycle    ResumeAttemptLifecycle
	attempt      ResumeAttemptIdentity
	ctx          context.Context
	state        resumeAttemptFinishState
	fallbackUsed bool
	receipt      ResumeAttemptFinishReceipt
	err          error
}

func newResumeAttemptFinishGuard(ctx context.Context, lifecycle ResumeAttemptLifecycle, attempt ResumeAttemptIdentity) *resumeAttemptFinishGuard {
	guard := &resumeAttemptFinishGuard{
		lifecycle: lifecycle,
		attempt:   attempt,
		ctx:       context.WithoutCancel(ctx),
	}
	guard.cond = sync.NewCond(&guard.mu)
	return guard
}

func (g *resumeAttemptFinishGuard) Finish(outcome ResumeAttemptFinishOutcome) (ResumeAttemptFinishReceipt, error) {
	if g == nil || g.lifecycle == nil {
		return ResumeAttemptFinishReceipt{}, nil
	}

	g.mu.Lock()
	fallback := false
	for {
		switch g.state {
		case resumeAttemptFinishComplete:
			receipt, err := g.receipt, g.err
			g.mu.Unlock()
			return receipt, err
		case resumeAttemptFinishInvoking:
			g.cond.Wait()
			continue
		case resumeAttemptFinishUncertain:
			if outcome != ResumeAttemptOutcomePanicked || g.fallbackUsed {
				g.cond.Wait()
				continue
			}
			fallback = true
			g.fallbackUsed = true
			g.state = resumeAttemptFinishInvoking
		case resumeAttemptFinishIdle:
			fallback = outcome == ResumeAttemptOutcomePanicked
			g.fallbackUsed = fallback
			g.state = resumeAttemptFinishInvoking
		}
		break
	}
	g.mu.Unlock()

	receipt, finishErr, panicked := invokeResumeAttemptFinish(g.ctx, g.lifecycle, ResumeAttemptFinish{
		Attempt: g.attempt,
		Outcome: outcome,
	})

	g.mu.Lock()
	if panicked {
		if fallback {
			g.state = resumeAttemptFinishComplete
			g.receipt = ResumeAttemptFinishReceipt{}
			g.err = errors.New("resume attempt finish failed")
			g.cond.Broadcast()
			receipt, err := g.receipt, g.err
			g.mu.Unlock()
			return receipt, err
		}
		g.state = resumeAttemptFinishUncertain
		g.cond.Broadcast()
		g.mu.Unlock()
		panic(errors.New("resume attempt finish panicked"))
	}
	g.state = resumeAttemptFinishComplete
	g.receipt = receipt
	if finishErr != nil {
		g.err = errors.New("resume attempt finish failed")
	} else {
		g.err = nil
	}
	g.cond.Broadcast()
	receipt, finishErr = g.receipt, g.err
	g.mu.Unlock()
	return receipt, finishErr
}

func (g *resumeAttemptFinishGuard) Finished() bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state == resumeAttemptFinishComplete
}

func invokeResumeAttemptFinish(ctx context.Context, lifecycle ResumeAttemptLifecycle, input ResumeAttemptFinish) (receipt ResumeAttemptFinishReceipt, err error, panicked bool) {
	defer func() {
		if recover() != nil {
			receipt = ResumeAttemptFinishReceipt{}
			err = nil
			panicked = true
		}
	}()
	receipt, err = lifecycle.FinishResumeAttempt(ctx, input)
	return receipt, err, false
}

func interactiveTurnCompletedByCancel(err error, agentKind string, conversation Conversation, generatedBytes int) bool {
	if err == nil || agentKind != AgentKindInteractiveStory || generatedBytes == 0 {
		return false
	}
	reporter, ok := conversation.(InteractiveNarrativeReadinessReporter)
	if !ok || !reporter.InteractiveNarrativeReady() {
		return false
	}
	var cancelErr *adk.CancelError
	return errors.As(err, &cancelErr) && cancelErr.Info != nil && cancelErr.Info.Mode&adk.CancelAfterToolCalls != 0
}
