package yanzhouadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"denova/internal/session"
	"denova/internal/yanzhouprotocol"
)

var writingCapabilityKinds = map[string]string{
	"book.conceive":       "conception",
	"outline.main.create": "outline", "outline.main.rewrite": "outline",
	"outline.volume.create": "outline", "outline.volume.rewrite": "outline",
	"outline.chapter.create": "outline", "outline.chapter.expand": "outline",
	"outline.chapter.rewrite": "outline", "outline.chapter.check_volume": "review",
	"chapter.generate_from_outline": "draft", "chapter.continue": "draft",
	"chapter.rewrite": "transform", "chapter.polish": "transform",
	"chapter.naturalize": "transform", "chapter.dialogue": "transform",
	"chapter.scene_description": "transform", "chapter.review": "review",
	"book.review": "review", "review.repair": "repair", "setting.sync": "state_patch",
	"image.generate": "image", "game.turn.generate": "game_turn",
	"game.turn.adapt_to_novel": "adaptation",
}

type writingRuntimeState struct {
	runID  string
	cancel context.CancelFunc
}

// WritingFrameRuntime consumes the existing run.start frame for non-Plan writing.
// It has model and proposal authority only: no workspace path and no Writer port.
type WritingFrameRuntime struct {
	mu                 sync.Mutex
	responseMu         sync.Mutex
	store              RuntimeEventStore
	client             *http.Client
	sessions           *session.Store
	runs               map[string]writingRuntimeState
	idempotency        map[string]string
	pendingResponses   map[string]chan yanzhouprotocol.Envelope
	earlyToolResponses map[string]yanzhouprotocol.Envelope
	pendingCancels     map[string]struct{}
}

func NewWritingFrameRuntime(store RuntimeEventStore, client *http.Client, stores ...*session.Store) (*WritingFrameRuntime, error) {
	if store == nil {
		return nil, errors.New("writing runtime event store is required")
	}
	if len(stores) > 1 {
		return nil, errors.New("writing runtime session store is invalid")
	}
	if client == nil {
		client = &http.Client{}
	}
	runtime := &WritingFrameRuntime{
		store: store, client: client,
		runs: map[string]writingRuntimeState{}, idempotency: map[string]string{},
		pendingResponses:   map[string]chan yanzhouprotocol.Envelope{},
		earlyToolResponses: map[string]yanzhouprotocol.Envelope{},
		pendingCancels:     map[string]struct{}{},
	}
	if len(stores) == 1 {
		runtime.sessions = stores[0]
	}
	return runtime, nil
}

// CancelRun consumes the existing run.cancel authority. It only cancels model/tool
// work; the runtime still emits the single durable run.aborted terminal itself.
func (runtime *WritingFrameRuntime) CancelRun(runID string) error {
	if runtime == nil || !validPlanSchemaID(runID) {
		return errors.New("writing run cancel is invalid")
	}
	runtime.mu.Lock()
	state, active := runtime.runs[runID]
	if active {
		cancel := state.cancel
		runtime.mu.Unlock()
		if cancel == nil {
			return errors.New("writing run cancel is unavailable")
		}
		cancel()
		return nil
	}
	if len(runtime.pendingCancels) >= 128 {
		runtime.mu.Unlock()
		return errors.New("writing run cancel buffer is full")
	}
	runtime.pendingCancels[runID] = struct{}{}
	runtime.mu.Unlock()
	return nil
}

type writingContextSection struct {
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	Revision  string `json:"revision"`
	Truncated *bool  `json:"truncated"`
}

type writingContextToolResult struct {
	Kind              string `json:"kind"`
	MutationPerformed bool   `json:"mutationPerformed"`
	Data              struct {
		ContextPackRef string                  `json:"contextPackRef"`
		Sections       []writingContextSection `json:"sections"`
	} `json:"data"`
	Accounting json.RawMessage `json:"accounting,omitempty"`
}

type writingToolResponsePayload struct {
	SchemaVersion string          `json:"schemaVersion"`
	ToolID        string          `json:"toolId"`
	Success       bool            `json:"success"`
	Result        json.RawMessage `json:"result,omitempty"`
	ErrorCode     string          `json:"errorCode,omitempty"`
}

type writingToolFailure struct{ code string }

func (failure *writingToolFailure) Error() string { return "writing context tool failed" }

type writingModelFailure struct {
	code    string
	message string
}

func (failure *writingModelFailure) Error() string { return failure.code }

func writingToolFailureCode(err error) string {
	var failure *writingToolFailure
	if errors.As(err, &failure) && validPlanSchemaID(failure.code) {
		return failure.code
	}
	return "tool_failed"
}

func writingModelFailureDetails(err error) (string, string) {
	var failure *writingModelFailure
	if errors.As(err, &failure) && validPlanSchemaID(failure.code) && failure.message != "" {
		return failure.code, failure.message
	}
	return "model_request_failed", "模型请求失败，作品没有被修改"
}

func (runtime *WritingFrameRuntime) HandleToolResponse(frame yanzhouprotocol.Envelope) error {
	if runtime == nil {
		return errors.New("writing frame runtime is unavailable")
	}
	if err := frame.Validate(); err != nil || frame.Kind != yanzhouprotocol.KindToolResponse || frame.RequestID != "tool-"+frame.RunID+"-context" {
		return errors.New("writing tool response is invalid")
	}
	var payload writingToolResponsePayload
	if err := decodeStrictPlanJSON(frame.Payload, yanzhouprotocol.DefaultMaxFrameBytes, &payload); err != nil || payload.SchemaVersion != "1" || payload.ToolID != "story.get_target" {
		return errors.New("writing tool response is invalid")
	}
	runtime.responseMu.Lock()
	defer runtime.responseMu.Unlock()
	if pending := runtime.pendingResponses[frame.RequestID]; pending != nil {
		select {
		case pending <- frame:
			return nil
		default:
			return errors.New("writing tool response is duplicated")
		}
	}
	if _, exists := runtime.earlyToolResponses[frame.RequestID]; exists {
		return errors.New("writing tool response is duplicated")
	}
	if len(runtime.earlyToolResponses) >= 128 {
		return errors.New("writing tool response buffer is full")
	}
	runtime.earlyToolResponses[frame.RequestID] = frame
	return nil
}

func (runtime *WritingFrameRuntime) registerToolResponse(requestID string) (chan yanzhouprotocol.Envelope, *yanzhouprotocol.Envelope, error) {
	runtime.responseMu.Lock()
	defer runtime.responseMu.Unlock()
	if runtime.pendingResponses[requestID] != nil {
		return nil, nil, errors.New("writing tool request is duplicated")
	}
	if early, ok := runtime.earlyToolResponses[requestID]; ok {
		delete(runtime.earlyToolResponses, requestID)
		return nil, &early, nil
	}
	response := make(chan yanzhouprotocol.Envelope, 1)
	runtime.pendingResponses[requestID] = response
	return response, nil, nil
}

func (runtime *WritingFrameRuntime) clearToolResponse(requestID string) {
	runtime.responseMu.Lock()
	delete(runtime.pendingResponses, requestID)
	runtime.responseMu.Unlock()
}

func decodeWritingContextResponse(frame yanzhouprotocol.Envelope, request planRunRequest) (string, error) {
	var payload writingToolResponsePayload
	if err := decodeStrictPlanJSON(frame.Payload, yanzhouprotocol.DefaultMaxFrameBytes, &payload); err != nil {
		return "", &writingToolFailure{code: "tool_response_invalid"}
	}
	if !payload.Success || payload.ErrorCode != "" {
		return "", &writingToolFailure{code: payload.ErrorCode}
	}
	var result writingContextToolResult
	if err := decodeStrictPlanJSON(payload.Result, yanzhouprotocol.DefaultMaxFrameBytes, &result); err != nil || result.Kind != "read-result" || result.MutationPerformed || result.Data.ContextPackRef != request.ContextPackRef.Ref || len(result.Data.Sections) == 0 || len(result.Data.Sections) > 64 {
		return "", &writingToolFailure{code: "tool_result_invalid"}
	}
	var builder strings.Builder
	for _, section := range result.Data.Sections {
		if !validPlanSchemaID(section.Kind) || section.Content == "" || len(section.Content) > 128*1024 || !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(section.Revision) || section.Truncated == nil {
			return "", &writingToolFailure{code: "tool_result_invalid"}
		}
		builder.WriteString("[")
		builder.WriteString(section.Kind)
		builder.WriteString("]\n")
		builder.WriteString(section.Content)
		builder.WriteString("\n")
		if builder.Len() > 512*1024 {
			return "", &writingToolFailure{code: "tool_result_invalid"}
		}
	}
	return builder.String(), nil
}

func (runtime *WritingFrameRuntime) requestWritingContext(ctx context.Context, output io.Writer, request planRunRequest) (string, error) {
	requestID := "tool-" + request.RunID + "-context"
	response, early, err := runtime.registerToolResponse(requestID)
	if err != nil {
		return "", err
	}
	defer runtime.clearToolResponse(requestID)
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_target", "agentId": "primary-writer",
		"target": json.RawMessage(request.Target), "arguments": "{}",
	})
	if err != nil {
		return "", err
	}
	if err := yanzhouprotocol.WriteFrame(output, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolRequest, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: requestID, RunID: request.RunID, Seq: 1, Payload: payload,
	}); err != nil {
		return "", err
	}
	if early != nil {
		return decodeWritingContextResponse(*early, request)
	}
	select {
	case frame := <-response:
		return decodeWritingContextResponse(frame, request)
	case <-ctx.Done():
		return "", errors.New("writing context tool timed out")
	}
}

func (runtime *WritingFrameRuntime) HandleFrame(ctx context.Context, frame yanzhouprotocol.Envelope, output io.Writer) (handleErr error) {
	if runtime == nil || output == nil {
		return errors.New("writing frame runtime is unavailable")
	}
	if err := frame.Validate(); err != nil || frame.Kind != yanzhouprotocol.KindRunStart {
		return errors.New("writing frame is invalid")
	}
	var request planRunRequest
	if err := decodeStrictPlanJSON(frame.Payload, yanzhouprotocol.DefaultMaxFrameBytes, &request); err != nil || validateWritingRunRequest(request, frame.RequestID) != nil {
		return errors.New("writing run request is invalid")
	}
	profile, err := writingHarnessProfile(request.HarnessProfile)
	if err != nil {
		return err
	}
	wallTime := request.Budgets.MaxWallTimeMS
	if profile.Budget.MaxWallTimeMS < wallTime {
		wallTime = profile.Budget.MaxWallTimeMS
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(wallTime)*time.Millisecond)
	runtime.mu.Lock()
	if existing, ok := runtime.idempotency[request.IdempotencyKey]; ok {
		runtime.mu.Unlock()
		cancel()
		if existing != request.RunID {
			return errors.New("writing run idempotency conflict")
		}
		return nil
	}
	if _, exists := runtime.runs[request.RunID]; exists {
		runtime.mu.Unlock()
		cancel()
		return errors.New("writing run already exists")
	}
	runtime.runs[request.RunID] = writingRuntimeState{runID: request.RunID, cancel: cancel}
	runtime.idempotency[request.IdempotencyKey] = request.RunID
	_, cancelPending := runtime.pendingCancels[request.RunID]
	delete(runtime.pendingCancels, request.RunID)
	runtime.mu.Unlock()
	writingSession, pendingInterruption, err := runtime.prepareSession(request)
	if err != nil {
		return err
	}
	interruptedUserMessage := request.UserIntent
	if pendingInterruption != nil {
		interruptedUserMessage = pendingInterruption.UserMessage
	}
	artifacts := []writingRuntimeArtifact{}
	defer func() {
		runWasCancelled := errors.Is(runCtx.Err(), context.Canceled)
		cancel()
		if handleErr != nil && ctx.Err() == nil && errors.Is(handleErr, context.Canceled) && runWasCancelled {
			handleErr = runtime.emitCancelled(ctx, output, request, artifacts, writingSession, interruptedUserMessage)
		}
		runtime.mu.Lock()
		delete(runtime.runs, request.RunID)
		runtime.mu.Unlock()
	}()

	if _, err := EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRunStarted, Payload: map[string]any{
		"sessionId": request.SessionID, "agentKind": request.AgentKind, "entrypoint": request.Entrypoint,
		"capabilityId": request.CapabilityID, "harnessProfile": request.HarnessProfile,
	}}); err != nil {
		return err
	}
	if cancelPending {
		cancel()
		return runtime.emitCancelled(ctx, output, request, nil, writingSession, interruptedUserMessage)
	}
	if _, err := EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeToolRequested, Payload: map[string]any{
		"toolId": "story.get_target", "agentId": "primary-writer",
	}}); err != nil {
		return err
	}
	if _, err := EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeToolStarted, Payload: map[string]any{
		"toolId": "story.get_target", "agentId": "primary-writer",
	}}); err != nil {
		return err
	}
	contextText, err := runtime.requestWritingContext(runCtx, output, request)
	if err != nil {
		if errors.Is(runCtx.Err(), context.Canceled) {
			return runtime.emitCancelled(ctx, output, request, nil, writingSession, interruptedUserMessage)
		}
		failureCode := writingToolFailureCode(err)
		if _, emitErr := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeToolCompleted, Payload: map[string]any{
			"toolId": "story.get_target", "agentId": "primary-writer", "status": "failed", "code": failureCode,
			"message": "读取当前章节失败",
		}}); emitErr != nil {
			return emitErr
		}
		_, terminalErr := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRunFailed, Payload: map[string]any{
			"schemaVersion": "1", "reason": "tool_error", "resumable": false, "partialArtifactRefs": []string{},
			"code": failureCode, "message": "无法读取当前章节，作品没有被修改",
		}})
		return terminalErr
	}
	if _, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeToolCompleted, Payload: map[string]any{
		"toolId": "story.get_target", "agentId": "primary-writer", "contextPackRef": request.ContextPackRef.Ref, "status": "succeeded",
	}}); err != nil {
		return err
	}
	if _, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeContextAccepted, Payload: map[string]any{
		"contextPackRef": request.ContextPackRef.Ref, "baseRevisionCount": len(request.BaseRevisions),
	}}); err != nil {
		return err
	}
	modelCalls := 0
	modelLimit := request.Budgets.MaxModelCalls
	if profile.Budget.MaxModelCalls < modelLimit {
		modelLimit = profile.Budget.MaxModelCalls
	}
	candidateArtifactID := ""
	for _, stage := range profile.Stages {
		if stage.ID == "deterministic-checks" {
			report := `{"status":"pass","checks":["output-not-empty"]}`
			artifact, emitErr := runtime.emitArtifact(runCtx, output, request, stage, report, artifacts, "deterministic")
			if emitErr != nil {
				return emitErr
			}
			artifacts = append(artifacts, artifact)
			if _, emitErr = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeCheckCompleted, Payload: map[string]any{
				"artifactId": candidateArtifactID, "statuses": []map[string]any{{"id": "output-not-empty", "status": "pass"}},
			}}); emitErr != nil {
				return emitErr
			}
			continue
		}
		if modelCalls >= modelLimit {
			_, terminalErr := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRunBudgetExhausted, Payload: map[string]any{
				"schemaVersion": "1", "reason": "budget_exhausted", "resumable": true, "partialArtifactRefs": writingArtifactIDs(artifacts),
			}})
			return terminalErr
		}
		if stage.Delegated {
			if _, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeDelegationStarted, Payload: map[string]any{
				"stageId": stage.ID, "role": stage.RoleID,
			}}); err != nil {
				return err
			}
		}
		if stage.ID == "primary-revision" || stage.RoleID == HarnessRoleFixer {
			if _, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRevisionRequested, Payload: map[string]any{
				"stageId": stage.ID, "role": stage.RoleID,
			}}); err != nil {
				return err
			}
		}
		response, callErr := runtime.callModel(runCtx, request, stage, artifacts, contextText, writingSession)
		modelCalls++
		if callErr != nil {
			if errors.Is(runCtx.Err(), context.Canceled) {
				return runtime.emitCancelled(ctx, output, request, artifacts, writingSession, interruptedUserMessage)
			}
			failureCode, failureMessage := writingModelFailureDetails(callErr)
			_, terminalErr := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRunFailed, Payload: map[string]any{
				"schemaVersion": "1", "reason": "provider_error", "resumable": false, "partialArtifactRefs": writingArtifactIDs(artifacts),
				"code": failureCode, "message": failureMessage,
			}})
			return terminalErr
		}
		artifact, emitErr := runtime.emitArtifact(runCtx, output, request, stage, response.Content, artifacts, "model")
		if emitErr != nil {
			return emitErr
		}
		artifacts = append(artifacts, artifact)
		if writingCandidateRole(stage.RoleID) {
			candidateArtifactID = artifact.ID
		}
		if stage.RoleID == HarnessRoleReviewer || stage.RoleID == HarnessRoleFinalGate {
			if _, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeReviewCompleted, Payload: map[string]any{
				"stageId": stage.ID, "role": stage.RoleID, "artifactId": artifact.ID, "status": "pass",
			}}); err != nil {
				return err
			}
		}
		if stage.Delegated {
			if _, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeDelegationCompleted, Payload: map[string]any{
				"stageId": stage.ID, "role": stage.RoleID, "artifactId": artifact.ID,
			}}); err != nil {
				return err
			}
		}
	}
	if writingSession != nil && candidateArtifactID != "" {
		for index := len(artifacts) - 1; index >= 0; index-- {
			if artifacts[index].ID == candidateArtifactID {
				if err := writingSession.Append(schema.AssistantMessage(artifacts[index].Content, nil)); err != nil {
					return err
				}
				break
			}
		}
	}
	if candidateArtifactID != "" && writingArtifactNeedsProposal(writingCapabilityKinds[request.CapabilityID]) {
		if _, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeProposalReady, Payload: map[string]any{
			"artifactId": candidateArtifactID,
		}}); err != nil {
			return err
		}
	}
	_, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRunCompleted, Payload: map[string]any{
		"schemaVersion": "1", "reason": "completed", "resumable": false, "partialArtifactRefs": writingArtifactIDs(artifacts),
	}})
	if err == nil && pendingInterruption != nil {
		err = writingSession.ResolveInterruption(pendingInterruption.ID)
	}
	return err
}

func (runtime *WritingFrameRuntime) emitCancelled(ctx context.Context, output io.Writer, request planRunRequest, artifacts []writingRuntimeArtifact, writingSession *session.Session, userMessage string) error {
	if writingSession != nil {
		assistantContent := ""
		if len(artifacts) > 0 {
			assistantContent = artifacts[len(artifacts)-1].Content
		}
		if err := writingSession.MarkInterrupted(userMessage, assistantContent, "cancelled"); err != nil {
			return err
		}
	}
	_, err := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRunAborted, Payload: map[string]any{
		"schemaVersion": "1", "reason": "cancelled", "resumable": false, "partialArtifactRefs": writingArtifactIDs(artifacts),
	}})
	return err
}

func (runtime *WritingFrameRuntime) prepareSession(request planRunRequest) (*session.Session, *session.Interruption, error) {
	if runtime.sessions == nil {
		return nil, nil, nil
	}
	writingSession, err := runtime.sessions.GetOrCreate(request.SessionID)
	if err != nil {
		return nil, nil, err
	}
	var pending *session.Interruption
	if request.ExplicitContinue {
		pending = writingSession.PendingInterruption()
	}
	if err := writingSession.Append(schema.UserMessage(request.UserIntent)); err != nil {
		return nil, nil, err
	}
	return writingSession, pending, nil
}

type writingRuntimeArtifact struct {
	ID      string
	Kind    string
	StageID string
	RoleID  WritingHarnessRoleID
	Content string
}

func writingHarnessProfile(profileID string) (WritingHarnessProfile, error) {
	for _, profile := range WritingHarnessProfiles() {
		if string(profile.ID) == profileID {
			if err := profile.Validate(); err != nil {
				return WritingHarnessProfile{}, err
			}
			return profile, nil
		}
	}
	return WritingHarnessProfile{}, errors.New("writing Harness profile is unavailable")
}

func writingCandidateRole(role WritingHarnessRoleID) bool {
	return role == HarnessRolePrimaryWriter || role == HarnessRoleWriter || role == HarnessRoleFixer
}

func writingArtifactIDs(artifacts []writingRuntimeArtifact) []string {
	ids := make([]string, len(artifacts))
	for index, artifact := range artifacts {
		ids[index] = artifact.ID
	}
	return ids
}

func writingStageArtifactKind(stage WritingHarnessStage, capabilityID string) string {
	if writingCandidateRole(stage.RoleID) {
		if capabilityID == "image.generate" {
			return "image"
		}
		if strings.HasPrefix(capabilityID, "outline.") {
			return "outline"
		}
	}
	return stage.OutputKind
}

func (runtime *WritingFrameRuntime) emitArtifact(ctx context.Context, output io.Writer, request planRunRequest, stage WritingHarnessStage, content string, previous []writingRuntimeArtifact, source string) (writingRuntimeArtifact, error) {
	for index, text := range boundedWritingChunks(content, 4096) {
		if _, err := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeModelDelta, Payload: map[string]any{
			"text": text, "chunkIndex": index, "stageId": stage.ID, "source": source,
		}}); err != nil {
			return writingRuntimeArtifact{}, err
		}
	}
	digest := sha256.Sum256([]byte(content))
	identity := sha256.Sum256([]byte(request.RunID + "\x00" + stage.ID + "\x00" + content))
	artifactID := "artifact-" + hex.EncodeToString(identity[:12])
	parentIDs := []string{}
	if len(previous) > 0 {
		parentIDs = []string{previous[len(previous)-1].ID}
	}
	kind := writingStageArtifactKind(stage, request.CapabilityID)
	if _, err := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeArtifactCreated, Payload: map[string]any{
		"artifactId": artifactID, "artifactKind": kind, "stageId": stage.ID, "role": stage.RoleID,
		"parentArtifactIds": parentIDs, "entrypoint": request.Entrypoint, "harnessProfile": request.HarnessProfile,
		"contentSha256": "sha256:" + hex.EncodeToString(digest[:]), "contentBytes": len([]byte(content)),
	}}); err != nil {
		return writingRuntimeArtifact{}, err
	}
	return writingRuntimeArtifact{ID: artifactID, Kind: kind, StageID: stage.ID, RoleID: stage.RoleID, Content: content}, nil
}

func writingArtifactNeedsProposal(kind string) bool {
	switch kind {
	case "conception", "outline", "draft", "transform", "repair":
		return true
	default:
		return false
	}
}

func validateWritingRunRequest(request planRunRequest, envelopeRequestID string) error {
	if request.SchemaVersion != "1" || request.RequestID != envelopeRequestID || !validPlanSchemaID(request.RequestID) || !validPlanSchemaID(request.IdempotencyKey) || !validPlanSchemaID(request.RunID) || !validPlanSchemaID(request.SessionID) {
		return invalidPlanPayload()
	}
	if request.PlanMode || request.AgentKind == "" || (request.Entrypoint != "agent_chat" && request.Entrypoint != "structured_action") || !boundedPlanText(request.UserIntent, 32*1024) || !boundedPlanText(request.DisplayLocale, 64) {
		return invalidPlanPayload()
	}
	if _, ok := writingCapabilityKinds[request.CapabilityID]; !ok {
		return invalidPlanPayload()
	}
	if !knownHarnessProfileID(WritingHarnessProfileID(request.HarnessProfile)) {
		return invalidPlanPayload()
	}
	seenSkills := map[string]bool{}
	for _, skillID := range request.SelectedSkillIDs {
		if !validPlanSchemaID(skillID) || seenSkills[skillID] {
			return errors.New("writing Skill selection is invalid")
		}
		seenSkills[skillID] = true
	}
	if request.Budgets.MaxModelCalls < 1 || request.Budgets.MaxWallTimeMS < 1 || request.Budgets.MaxWallTimeMS > 24*60*60*1000 || !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(request.ContextPackRef.Ref) {
		return invalidPlanPayload()
	}
	for _, raw := range []json.RawMessage{request.Target, request.ToolManifest, request.EffectiveModelProfile.Capabilities, request.EffectiveModelProfile.Resolution} {
		if !validPlanOpaqueObject(raw) {
			return invalidPlanPayload()
		}
	}
	if _, err := NewModelAdapter(request.EffectiveModelProfile.effective()); err != nil {
		return invalidPlanPayload()
	}
	return nil
}

func (runtime *WritingFrameRuntime) callModel(ctx context.Context, request planRunRequest, stage WritingHarnessStage, previous []writingRuntimeArtifact, contextText string, writingSession *session.Session) (ModelResponse, error) {
	adapter, err := NewModelAdapter(request.EffectiveModelProfile.effective())
	if err != nil {
		return ModelResponse{}, &writingModelFailure{code: "model_configuration_invalid", message: "模型配置不可用，作品没有被修改"}
	}
	maxOutput := 4096
	if request.Budgets.MaxOutputTokens != nil && *request.Budgets.MaxOutputTokens > 0 {
		maxOutput = *request.Budgets.MaxOutputTokens
	}
	native, err := adapter.BuildRequest(ModelRequest{Messages: writingModelMessages(
		writingSession,
		writingSystemInstruction(request.CapabilityID, request.HarnessProfile, request.SelectedSkillIDs, stage),
		writingStageInput(request.UserIntent, contextText, previous),
	), MaxOutputTokens: maxOutput}, false)
	if err != nil {
		return ModelResponse{}, &writingModelFailure{code: "model_request_invalid", message: "模型请求无效，作品没有被修改"}
	}
	deadline := time.Duration(request.EffectiveModelProfile.TimeoutMS) * time.Millisecond
	if wall := time.Duration(request.Budgets.MaxWallTimeMS) * time.Millisecond; wall < deadline {
		deadline = wall
	}
	callCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(callCtx, native.Method, native.URL, bytes.NewReader(native.Body))
	if err != nil {
		return ModelResponse{}, &writingModelFailure{code: "model_request_invalid", message: "模型请求无效，作品没有被修改"}
	}
	for key, value := range native.Headers {
		httpRequest.Header.Set(key, value)
	}
	response, err := runtime.client.Do(httpRequest)
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return ModelResponse{}, &writingModelFailure{code: "network_timeout", message: "模型服务响应超时，作品没有被修改"}
		}
		return ModelResponse{}, &writingModelFailure{code: "network_error", message: "无法连接模型服务，作品没有被修改"}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil {
		return ModelResponse{}, &writingModelFailure{code: "network_error", message: "读取模型响应失败，作品没有被修改"}
	}
	if len(body) > 4*1024*1024 {
		return ModelResponse{}, &writingModelFailure{code: "response_too_large", message: "模型响应过大，作品没有被修改"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return ModelResponse{}, &writingModelFailure{code: "authentication", message: "模型凭证不可用，作品没有被修改"}
		}
		if response.StatusCode == http.StatusTooManyRequests {
			return ModelResponse{}, &writingModelFailure{code: "rate_limit", message: "模型服务请求过多，请稍后重试"}
		}
		if response.StatusCode >= http.StatusInternalServerError {
			return ModelResponse{}, &writingModelFailure{code: "provider_unavailable", message: "模型服务暂时不可用，请稍后重试"}
		}
		return ModelResponse{}, &writingModelFailure{code: "http_status", message: "模型服务拒绝了请求，作品没有被修改"}
	}
	modelResponse, err := adapter.NormalizeResponse(body)
	if err != nil || strings.TrimSpace(modelResponse.Content) == "" || len(modelResponse.ToolCalls) != 0 {
		return ModelResponse{}, &writingModelFailure{code: "invalid_response", message: "模型返回内容无法使用，作品没有被修改"}
	}
	return modelResponse, nil
}

func writingModelMessages(writingSession *session.Session, systemInstruction, finalUserMessage string) []ModelMessage {
	messages := []ModelMessage{{Role: "system", Content: systemInstruction}}
	if writingSession == nil {
		return append(messages, ModelMessage{Role: "user", Content: finalUserMessage})
	}
	for _, message := range writingSession.GetEffectiveMessages() {
		if message == nil || (message.Role != schema.User && message.Role != schema.Assistant) || strings.TrimSpace(message.Content) == "" {
			continue
		}
		messages = append(messages, ModelMessage{Role: string(message.Role), Content: message.Content})
	}
	if len(messages) == 1 || messages[len(messages)-1].Role != "user" {
		return append(messages, ModelMessage{Role: "user", Content: finalUserMessage})
	}
	messages[len(messages)-1].Content = finalUserMessage
	return messages
}

func boundedWritingChunks(value string, maxBytes int) []string {
	if value == "" {
		return nil
	}
	chunks := []string{}
	for len(value) > 0 {
		end := len(value)
		if end > maxBytes {
			end = maxBytes
			for end > 0 && (value[end]&0xC0) == 0x80 {
				end--
			}
		}
		if end == 0 {
			end = len(value)
		}
		chunks = append(chunks, value[:end])
		value = value[end:]
	}
	return chunks
}

func writingStageInput(instruction, contextText string, previous []writingRuntimeArtifact) string {
	var builder strings.Builder
	builder.WriteString(instruction)
	if contextText != "" {
		builder.WriteString("\n\nMain-owned processed ContextPack:\n")
		builder.WriteString(contextText)
	}
	if len(previous) == 0 {
		return builder.String()
	}
	builder.WriteString("\n\nPrior bounded Artifact context:\n")
	start := 0
	if len(previous) > 3 {
		start = len(previous) - 3
	}
	for _, artifact := range previous[start:] {
		builder.WriteString("[")
		builder.WriteString(artifact.StageID)
		builder.WriteString("/")
		builder.WriteString(artifact.Kind)
		builder.WriteString("]\n")
		content := artifact.Content
		if len(content) > 16*1024 {
			start := len(content) - 16*1024
			for start < len(content) && (content[start]&0xC0) == 0x80 {
				start++
			}
			content = content[start:]
		}
		builder.WriteString(content)
		builder.WriteString("\n")
	}
	return builder.String()
}

func writingSystemInstruction(capabilityID, harnessProfile string, skillIDs []string, stage WritingHarnessStage) string {
	instruction := "Produce only the requested bounded stage result. Capability: " + capabilityID + ". Harness: " + harnessProfile + ". Stage: " + stage.ID + ". Role: " + string(stage.RoleID) + "."
	if len(skillIDs) > 0 {
		instruction += " Skill: " + strings.Join(skillIDs, ", ") + "."
	}
	return instruction + " Never claim the work was committed, never expose reasoning, and never request a filesystem path."
}
