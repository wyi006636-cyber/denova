package writingcomparison

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"denova/config"
	"denova/internal/agents/modelio"
	"denova/internal/agents/prompts"
	agentrun "denova/internal/agents/run"
	projectdomain "denova/internal/project"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// Run freezes the input once and queues one detached candidate execution. A
// partial or incomplete rerun reuses the existing snapshot and never emits a
// second snapshot.frozen record.
func (service *Service) Run(ctx context.Context, layout projectdomain.Layout, id string, expectedRevision uint64) (Task, error) {
	if err := service.recoverTask(ctx, layout, id); err != nil {
		return Task{}, err
	}
	store := NewStore(layout)
	handle, err := store.Open(ctx, id)
	if err != nil {
		return Task{}, err
	}
	defer handle.Close()
	task := handle.Current()
	if task.Revision != expectedRevision {
		return Task{}, &RevisionConflictError{Expected: expectedRevision, Actual: task.Revision}
	}
	if task.Execution == ExecutionQueued || task.Execution == ExecutionRunning {
		return Task{}, domainError(CodeStateConflict, "comparison run is already active", ErrStateConflict, nil)
	}
	if task.Phase != PhaseConfiguring && task.Phase != PhaseGenerating {
		if task.Execution != ExecutionIncomplete {
			return Task{}, domainError(CodeStateConflict, "comparison task cannot run in its current state", ErrStateConflict, nil)
		}
	}
	if task.Snapshot == nil {
		cfg, cfgErr := service.config(ctx, layout)
		if cfgErr != nil {
			return Task{}, cfgErr
		}
		snapshot, freezeErr := freezeWritingSnapshot(ctx, cfg, layout, task, store)
		if freezeErr != nil {
			return Task{}, freezeErr
		}
		task, err = handle.Append(ctx, task.Revision, Event{Kind: EventSnapshotFrozen, Snapshot: snapshot})
		if err != nil {
			return Task{}, err
		}
	}
	if task.Phase != PhaseGenerating {
		from := task.Phase
		task, err = handle.Append(ctx, task.Revision, Event{Kind: EventPhaseChanged, FromPhase: from, ToPhase: PhaseGenerating})
		if err != nil {
			return Task{}, err
		}
	}
	if task.Execution != ExecutionQueued {
		from := task.Execution
		task, err = handle.Append(ctx, task.Revision, Event{Kind: EventExecutionChanged, FromExecution: from, ToExecution: ExecutionQueued})
		if err != nil {
			return Task{}, err
		}
	}
	service.markRunActive(layout, task.ID)
	return task, nil
}

// Execute performs the queued run. It is intentionally callback-driven so the
// App owns the detached task lifecycle while this package owns durable state.
func (service *Service) Execute(ctx context.Context, layout projectdomain.Layout, id string, emit func(Event)) error {
	store := NewStore(layout)
	handle, err := store.Open(ctx, id)
	if err != nil {
		return err
	}
	defer handle.Close()
	defer service.clearRunActive(layout, id)
	task := handle.Current()
	slog.InfoContext(ctx, "writing comparison execution starting", "task_id", id, "phase", task.Phase, "execution", task.Execution, "revision", task.Revision)
	if task.Execution != ExecutionQueued {
		return domainError(CodeStateConflict, "comparison run is not queued", ErrStateConflict, nil)
	}
	task, err = handle.Append(ctx, task.Revision, Event{Kind: EventExecutionChanged, FromExecution: ExecutionQueued, ToExecution: ExecutionRunning})
	if err != nil {
		return err
	}
	if emit != nil {
		emit(Event{Kind: EventExecutionChanged, FromExecution: ExecutionQueued, ToExecution: ExecutionRunning, Task: &task})
	}
	cfg, err := service.config(ctx, layout)
	if err != nil {
		return service.finishRunError(ctx, handle, task, emit, err)
	}
	userMessage, err := snapshotUserMessage(ctx, store, task)
	if err != nil {
		return service.finishRunError(ctx, handle, task, emit, err)
	}
	for index := range task.Candidates {
		candidate := task.Candidates[index]
		if candidate.Status == CandidateCompleted || candidate.Status == CandidateFailed || candidate.Status == CandidateRunning {
			continue
		}
		started := Event{Kind: EventCandidateStarted, CandidateID: candidate.ID}
		task, err = handle.Append(ctx, task.Revision, started)
		if err != nil {
			return err
		}
		if emit != nil {
			emit(Event{Kind: EventCandidateStarted, CandidateID: candidate.ID, Task: &task})
		}
		candidate, sourceErr := loadCandidateSource(ctx, store, task, candidate)
		if sourceErr != nil {
			taskError := taskErrorFrom(sourceErr)
			task, err = handle.Append(ctx, task.Revision, Event{Kind: EventCandidateFailed, CandidateID: candidate.ID, Error: taskError})
			if err != nil {
				return err
			}
			if emit != nil {
				emit(Event{Kind: EventCandidateFailed, CandidateID: candidate.ID, Error: taskError, Task: &task})
			}
			continue
		}
		content, usage, durationMs, callErr := service.generateCandidate(ctx, &cfg, task, candidate, userMessage)
		if callErr != nil {
			taskError := taskErrorFrom(callErr)
			failed := Event{Kind: EventCandidateFailed, CandidateID: candidate.ID, Error: taskError}
			task, err = handle.Append(ctx, task.Revision, failed)
			if err != nil {
				return err
			}
			if emit != nil {
				emit(Event{Kind: EventCandidateFailed, CandidateID: candidate.ID, Error: taskError, Task: &task})
			}
			continue
		}
		relative := fmt.Sprintf("candidates/%s/1.md", candidate.ID)
		assetPath, pathErr := store.AssetPath(task.ID, relative)
		if pathErr != nil {
			return pathErr
		}
		if err := WriteImmutableAsset(assetPath, []byte(content)); err != nil {
			taskError := taskErrorFrom(err)
			task, appendErr := handle.Append(ctx, task.Revision, Event{Kind: EventCandidateFailed, CandidateID: candidate.ID, Error: taskError})
			if appendErr != nil {
				return appendErr
			}
			if emit != nil {
				emit(Event{Kind: EventCandidateFailed, CandidateID: candidate.ID, Error: taskError, Task: &task})
			}
			continue
		}
		system := prompts.ComposeCandidateDraftInstruction(candidate.Source.Body, prompts.TargetHintText)
		inputHash := AssetSHA256([]byte(task.Snapshot.SHA256 + system + candidate.Requirement))
		revision := &CandidateRevision{Number: 1, Path: relative, SHA256: AssetSHA256([]byte(content)), Bytes: len(content), InputSHA256: inputHash, Model: candidate.Profile, CreatedAt: time.Now().UTC(), Usage: usage, DurationMs: durationMs}
		task, err = handle.Append(ctx, task.Revision, Event{Kind: EventCandidateCompleted, CandidateID: candidate.ID, Revision: revision})
		if err != nil {
			return err
		}
		if emit != nil {
			emit(Event{Kind: EventCandidateCompleted, CandidateID: candidate.ID, Revision: revision, Task: &task})
		}
	}
	toPhase := PhasePartial
	if allCandidatesCompleted(task) {
		toPhase = PhaseGenerated
	}
	if task.Execution == ExecutionRunning {
		task, err = handle.Append(ctx, task.Revision, Event{Kind: EventExecutionChanged, FromExecution: ExecutionRunning, ToExecution: ExecutionDone})
		if err != nil {
			return err
		}
		if emit != nil {
			emit(Event{Kind: EventExecutionChanged, FromExecution: ExecutionRunning, ToExecution: ExecutionDone, Task: &task})
		}
	}
	if task.Phase == PhaseGenerating {
		task, err = handle.Append(ctx, task.Revision, Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: toPhase})
		if err != nil {
			return err
		}
		if emit != nil {
			emit(Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: toPhase, Task: &task})
		}
	}
	slog.InfoContext(ctx, "writing comparison run completed", "task_id", id, "phase", task.Phase)
	return nil
}

func (service *Service) finishRunError(ctx context.Context, handle *Handle, task Task, emit func(Event), cause error) error {
	if task.Execution == ExecutionRunning {
		if _, err := handle.Append(ctx, task.Revision, Event{Kind: EventExecutionChanged, FromExecution: ExecutionRunning, ToExecution: ExecutionDone}); err != nil {
			return errors.Join(cause, err)
		}
	}
	return cause
}

func (service *Service) generateCandidate(ctx context.Context, cfg *config.Config, task Task, candidate Candidate, userMessage string) (string, *Usage, int64, error) {
	resolved, err := config.ResolveModelProfile(cfg, config.ModelProfileSettings{ID: candidate.Profile.ProfileID})
	if err != nil {
		return "", nil, 0, err
	}
	modelConfig, err := modelio.ConfigFromResolved(resolved)
	if err != nil {
		return "", nil, 0, err
	}
	traceCtx, finishTrace := agentrun.WithStandaloneTrace(ctx, cfg, config.AgentKindIDE, "writing_comparison_candidate", "generate", map[string]any{"task_id": task.ID, "candidate_id": candidate.ID})
	var traceErr error
	defer func() { finishTrace(traceErr) }()
	model, err := modelio.NewChatModel(traceCtx, modelConfig)
	if err != nil {
		traceErr = err
		return "", nil, 0, domainError(CodeModelError, "candidate model could not be created", err, nil)
	}
	system := prompts.ComposeCandidateDraftInstruction(candidate.Source.Body, prompts.TargetHintText)
	messages := []*agentschema.Message{agentschema.SystemMessage(system), agentschema.UserMessage(userMessage)}
	if err := modelio.ValidateConfiguredInput(cfg, config.AgentKindIDE, messages, nil); err != nil {
		traceErr = err
		return "", nil, 0, err
	}
	span, callID, llmTraceCtx := agentrun.BeginLLMCallTrace(traceCtx, config.AgentKindIDE, "writing_comparison_candidate", "generate", modelConfig, messages, nil, false)
	startedAt := time.Now()
	response, err := model.Generate(llmTraceCtx, messages)
	durationMs := time.Since(startedAt).Milliseconds()
	if err != nil {
		traceErr = err
		agentrun.FinishLLMCallTrace(span, callID, config.AgentKindIDE, "writing_comparison_candidate", "generate", modelConfig.Model, 0, nil, err, nil)
		return "", nil, durationMs, domainError(CodeModelError, "candidate model generation failed", err, nil)
	}
	agentrun.FinishLLMCallTrace(span, callID, config.AgentKindIDE, "writing_comparison_candidate", "generate", modelConfig.Model, 0, response, nil, nil)
	if response == nil || strings.TrimSpace(response.Content) == "" {
		err := domainError(CodeEmptyOutput, "candidate model returned empty content", nil, nil)
		traceErr = err
		return "", nil, durationMs, err
	}
	var usage *Usage
	if response.ResponseMeta != nil && response.ResponseMeta.Usage != nil {
		providerUsage := response.ResponseMeta.Usage
		usage = &Usage{InputTokens: providerUsage.PromptTokens, OutputTokens: providerUsage.CompletionTokens, TotalTokens: providerUsage.TotalTokens}
	}
	return strings.TrimSpace(response.Content), usage, durationMs, nil
}

func loadCandidateSource(ctx context.Context, store *Store, task Task, candidate Candidate) (Candidate, error) {
	if strings.TrimSpace(candidate.Source.Body) != "" {
		return candidate, nil
	}
	if strings.TrimSpace(candidate.Source.BodyAssetPath) == "" {
		return candidate, domainError(CodeAssetCorrupt, "candidate source asset is missing", ErrAssetCorrupt, nil)
	}
	path, err := store.AssetPath(task.ID, candidate.Source.BodyAssetPath)
	if err != nil {
		return candidate, err
	}
	data, err := ReadVerifiedAsset(path, candidate.Source.ContentSHA256)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrAssetCorrupt) {
			return candidate, domainError(CodeAssetCorrupt, "candidate source asset is unavailable", ErrAssetCorrupt, nil)
		}
		return candidate, err
	}
	candidate.Source.Body = string(data)
	return candidate, nil
}

func snapshotUserMessage(ctx context.Context, store *Store, task Task) (string, error) {
	if task.Snapshot == nil {
		return "", fmt.Errorf("writing snapshot is missing")
	}
	var builder strings.Builder
	builder.WriteString("Chapter task:\n")
	builder.WriteString(task.ChapterTask.Text)
	for _, fragment := range task.Snapshot.Fragments {
		data, err := ReadVerifiedAsset(func() string { p, _ := store.AssetPath(task.ID, fragment.AssetPath); return p }(), fragment.SHA256)
		if err != nil {
			return "", err
		}
		builder.WriteString("\n\n---\n\n# ")
		builder.WriteString(fragment.Title)
		builder.WriteString("\n\n")
		builder.Write(data)
	}
	return builder.String(), nil
}

func taskErrorFrom(err error) *TaskError {
	if err == nil {
		return nil
	}
	code := ErrorCode(err)
	return &TaskError{Code: code, Message: err.Error()}
}

func (service *Service) Recover(ctx context.Context, layout projectdomain.Layout) error {
	entries, err := os.ReadDir(layout.ComparisonsDir())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	store := NewStore(layout)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if service.isRunActive(layout, entry.Name()) {
			continue
		}
		handle, openErr := store.Open(ctx, entry.Name())
		if openErr != nil {
			slog.WarnContext(ctx, "writing comparison task recovery open failed", "task_id", entry.Name(), "error", openErr)
			continue
		}
		task := handle.Current()
		if task.Execution == ExecutionRunning || task.Execution == ExecutionQueued {
			if _, appendErr := handle.Append(ctx, task.Revision, Event{Kind: EventExecutionChanged, FromExecution: task.Execution, ToExecution: ExecutionIncomplete, Reason: "recovered at startup"}); appendErr != nil {
				slog.WarnContext(ctx, "writing comparison task recovery append failed", "task_id", entry.Name(), "error", appendErr)
			}
		}
		if closeErr := handle.Close(); closeErr != nil {
			slog.WarnContext(ctx, "writing comparison task recovery close failed", "task_id", entry.Name(), "error", closeErr)
		}
	}
	return nil
}
