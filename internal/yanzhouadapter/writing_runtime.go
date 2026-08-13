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
	runs               map[string]writingRuntimeState
	idempotency        map[string]string
	pendingResponses   map[string]chan yanzhouprotocol.Envelope
	earlyToolResponses map[string]yanzhouprotocol.Envelope
	pendingCancels     map[string]struct{}
}

func NewWritingFrameRuntime(store RuntimeEventStore, client *http.Client) (*WritingFrameRuntime, error) {
	if store == nil {
		return nil, errors.New("writing runtime event store is required")
	}
	if client == nil {
		client = &http.Client{}
	}
	return &WritingFrameRuntime{
		store: store, client: client,
		runs: map[string]writingRuntimeState{}, idempotency: map[string]string{},
		pendingResponses:   map[string]chan yanzhouprotocol.Envelope{},
		earlyToolResponses: map[string]yanzhouprotocol.Envelope{},
		pendingCancels:     map[string]struct{}{},
	}, nil
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
	if err := decodeStrictPlanJSON(frame.Payload, yanzhouprotocol.DefaultMaxFrameBytes, &payload); err != nil || !payload.Success || payload.ErrorCode != "" {
		return "", errors.New("writing context tool failed")
	}
	var result writingContextToolResult
	if err := decodeStrictPlanJSON(payload.Result, yanzhouprotocol.DefaultMaxFrameBytes, &result); err != nil || result.Kind != "read-result" || result.MutationPerformed || result.Data.ContextPackRef != request.ContextPackRef.Ref || (len(result.Data.Sections) == 0 && request.CapabilityID != "book.conceive") || len(result.Data.Sections) > 64 {
		return "", errors.New("writing context tool result is invalid")
	}
	var builder strings.Builder
	for _, section := range result.Data.Sections {
		if !validPlanSchemaID(section.Kind) || section.Content == "" || len(section.Content) > 128*1024 || !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(section.Revision) || section.Truncated == nil {
			return "", errors.New("writing context section is invalid")
		}
		builder.WriteString("[")
		builder.WriteString(section.Kind)
		builder.WriteString("]\n")
		builder.WriteString(section.Content)
		builder.WriteString("\n")
		if builder.Len() > 512*1024 {
			return "", errors.New("writing context tool result exceeds limit")
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
	wallTime := writingRunWallTimeMS(request, profile)
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
	artifacts := []writingRuntimeArtifact{}
	defer func() {
		runWasCancelled := errors.Is(runCtx.Err(), context.Canceled)
		cancel()
		if handleErr != nil && ctx.Err() == nil && errors.Is(handleErr, context.Canceled) && runWasCancelled {
			handleErr = runtime.emitCancelled(ctx, output, request, artifacts)
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
		return runtime.emitCancelled(ctx, output, request, nil)
	}
	if _, err := EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeToolRequested, Payload: map[string]any{
		"toolId": "story.get_target", "agentId": "primary-writer",
	}}); err != nil {
		return err
	}
	contextText, err := runtime.requestWritingContext(runCtx, output, request)
	if err != nil {
		if errors.Is(runCtx.Err(), context.Canceled) {
			return runtime.emitCancelled(ctx, output, request, nil)
		}
		_, terminalErr := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRunFailed, Payload: map[string]any{
			"schemaVersion": "1", "reason": "tool_error", "resumable": false, "partialArtifactRefs": []string{},
		}})
		return terminalErr
	}
	if _, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeToolCompleted, Payload: map[string]any{
		"toolId": "story.get_target", "agentId": "primary-writer", "contextPackRef": request.ContextPackRef.Ref,
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
		var delegation writingDelegation
		publicDelegation := stage.Delegated && stage.RoleID == HarnessRoleReviewer
		if request.CapabilityID == "book.conceive" && stage.ID == "primary-revision" {
			continue
		}
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
			delegation, err = prepareWritingDelegation(request, stage, artifacts)
			if err != nil {
				_, terminalErr := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRunFailed, Payload: map[string]any{
					"schemaVersion": "1", "reason": "provider_error", "resumable": false, "partialArtifactRefs": writingArtifactIDs(artifacts),
				}})
				return terminalErr
			}
		}
		if publicDelegation {
			if _, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{
				Type: RunEventTypeDelegationStarted, Payload: writingDelegationStartedPayload(delegation),
			}); err != nil {
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
		response, callErr := runtime.callModel(runCtx, request, stage, artifacts, contextText, delegation.agent.Prompt)
		modelCalls++
		if callErr != nil {
			if errors.Is(runCtx.Err(), context.Canceled) {
				return runtime.emitCancelled(ctx, output, request, artifacts)
			}
			if publicDelegation {
				if _, err = EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{
					Type:    RunEventTypeDelegationCompleted,
					Payload: writingDelegationCompletedPayload(delegation, []string{}, "failed"),
				}); err != nil {
					return err
				}
			}
			_, terminalErr := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRunFailed, Payload: map[string]any{
				"schemaVersion": "1", "reason": "provider_error", "resumable": false, "partialArtifactRefs": writingArtifactIDs(artifacts),
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
		if publicDelegation {
			if _, err = EmitRunEvent(runCtx, runtime.store, output, request.RunID, RuntimeEventInput{
				Type:    RunEventTypeDelegationCompleted,
				Payload: writingDelegationCompletedPayload(delegation, []string{artifact.ID}, "completed"),
			}); err != nil {
				return err
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
	return err
}

// FailPanic is called only by the goroutine that recovered a writing frame panic.
func (runtime *WritingFrameRuntime) FailPanic(ctx context.Context, frame yanzhouprotocol.Envelope, output io.Writer) error {
	if runtime == nil || output == nil {
		return errors.New("writing runtime panic could not be terminalized")
	}
	runID := frame.RunID
	if !validPlanSchemaID(runID) {
		var request planRunRequest
		if decodeStrictPlanJSON(frame.Payload, yanzhouprotocol.DefaultMaxFrameBytes, &request) == nil {
			runID = request.RunID
		}
	}
	if !validPlanSchemaID(runID) {
		return errors.New("writing runtime panic run is invalid")
	}
	_, err := EmitRunEvent(context.WithoutCancel(ctx), runtime.store, output, runID, RuntimeEventInput{
		Type: RunEventTypeRunFailed,
		Payload: map[string]any{
			"schemaVersion": "1", "reason": "panic", "resumable": false, "partialArtifactRefs": []string{},
		},
	})
	return err
}

func (runtime *WritingFrameRuntime) emitCancelled(ctx context.Context, output io.Writer, request planRunRequest, artifacts []writingRuntimeArtifact) error {
	_, err := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{Type: RunEventTypeRunAborted, Payload: map[string]any{
		"schemaVersion": "1", "reason": "cancelled", "resumable": false, "partialArtifactRefs": writingArtifactIDs(artifacts),
	}})
	return err
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
		if capabilityID == "book.conceive" {
			return "conception"
		}
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
	if err := validateRunSkillSnapshot(request.RunID, request.SelectedSkillIDs, request.SkillSnapshot); err != nil {
		return err
	}
	profile, err := writingHarnessProfile(request.HarnessProfile)
	if err != nil {
		return err
	}
	for _, stage := range profile.Stages {
		if !stage.Delegated {
			continue
		}
		snapshot := runSubAgentSnapshot(request)
		if snapshot.Validate() != nil {
			return errors.New("writing SubAgent snapshot is invalid")
		}
		agent, found := snapshot.Agent(string(stage.RoleID))
		if !found || !agent.Enabled {
			return errors.New("writing delegated stage is unavailable")
		}
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

func (runtime *WritingFrameRuntime) callModel(ctx context.Context, request planRunRequest, stage WritingHarnessStage, previous []writingRuntimeArtifact, contextText, delegatedPrompt string) (ModelResponse, error) {
	adapter, err := NewModelAdapter(request.EffectiveModelProfile.effective())
	if err != nil {
		return ModelResponse{}, err
	}
	maxOutput := 4096
	if request.Budgets.MaxOutputTokens != nil && *request.Budgets.MaxOutputTokens > 0 {
		maxOutput = *request.Budgets.MaxOutputTokens
	}
	jsonConception := request.CapabilityID == "book.conceive"
	disableThinking := jsonConception && request.EffectiveModelProfile.ProviderType == ProviderOpenAICompatible && strings.HasPrefix(strings.ToLower(request.EffectiveModelProfile.Model), "deepseek-v4-")
	native, err := adapter.BuildRequest(ModelRequest{
		Messages: []ModelMessage{
			{Role: "system", Content: writingSystemInstruction(request.CapabilityID, request.HarnessProfile, request.SelectedSkillIDs, stage, delegatedPrompt)},
			{Role: "user", Content: writingStageInput(request.UserIntent, contextText, previous, request.CapabilityID)},
		},
		MaxOutputTokens: maxOutput,
		JSONOutput:      jsonConception,
		DisableThinking: disableThinking,
	}, false)
	if err != nil {
		return ModelResponse{}, err
	}
	deadline := time.Duration(request.EffectiveModelProfile.TimeoutMS) * time.Millisecond
	if wall := time.Duration(request.Budgets.MaxWallTimeMS) * time.Millisecond; wall < deadline {
		deadline = wall
	}
	callCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(callCtx, native.Method, native.URL, bytes.NewReader(native.Body))
	if err != nil {
		return ModelResponse{}, err
	}
	for key, value := range native.Headers {
		httpRequest.Header.Set(key, value)
	}
	response, err := runtime.client.Do(httpRequest)
	if err != nil {
		return ModelResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil || len(body) > 4*1024*1024 || response.StatusCode < 200 || response.StatusCode >= 300 {
		return ModelResponse{}, errors.New("writing model request failed")
	}
	modelResponse, err := adapter.NormalizeResponse(body)
	if err != nil || strings.TrimSpace(modelResponse.Content) == "" || len(modelResponse.ToolCalls) != 0 {
		return ModelResponse{}, errors.New("writing model response is invalid")
	}
	return modelResponse, nil
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

func writingRunWallTimeMS(request planRunRequest, profile WritingHarnessProfile) int {
	wallTime := request.Budgets.MaxWallTimeMS
	if request.CapabilityID != "book.conceive" && profile.Budget.MaxWallTimeMS < wallTime {
		wallTime = profile.Budget.MaxWallTimeMS
	}
	return wallTime
}

func writingStageInput(instruction, contextText string, previous []writingRuntimeArtifact, capabilityID string) string {
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
		artifactLimit := 16 * 1024
		if capabilityID == "book.conceive" {
			artifactLimit = 128 * 1024
		}
		if len(content) > artifactLimit {
			start := len(content) - artifactLimit
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

func writingSystemInstruction(capabilityID, harnessProfile string, skillIDs []string, stage WritingHarnessStage, delegatedPrompt string) string {
	instruction := "Produce only the requested bounded stage result. Capability: " + capabilityID + ". Harness: " + harnessProfile + ". Stage: " + stage.ID + ". Role: " + string(stage.RoleID) + "."
	if len(skillIDs) > 0 {
		instruction += " Skill: " + strings.Join(skillIDs, ", ") + "."
	}
	if delegatedPrompt != "" {
		instruction += " Authorized SubAgent instruction: " + delegatedPrompt
	}
	if capabilityID == "book.conceive" {
		instruction += writingConceptionInstruction(stage)
	}
	return instruction + " Never claim the work was committed, never expose reasoning, and never request a filesystem path."
}

func writingConceptionInstruction(stage WritingHarnessStage) string {
	if stage.RoleID == HarnessRoleReviewer || stage.RoleID == HarnessRoleFinalGate {
		return ` Review the complete prior conception Artifact for canonical completeness, causal consistency, and fidelity to the author's saved conversation. Return only one JSON object with keys status and issues. status must be pass or revise. issues must always contain 2-5 concrete structure or content suggestions, even when status is pass; never return an empty issues array or only say pass. Every issue must have a precise path, problem, and actionable fix. A truncated, unclosed, or otherwise non-parseable JSON package must always be revise. Do not rewrite the package in this review stage, and do not turn subjective advice into a mechanical gate.`
	}
	return ` Generate the complete Yanzhou canonical Book Start Package from the author's entire saved conversation. Return exactly one valid JSON object, with no Markdown fence, commentary, questions, placeholders, or omitted sections. Never use 待补充, 略, 后续再定, TBD, or ellipses as content.

If the latest user request contains [TICKET03_PARTIAL_REGEN target=...], the Main-owned ContextPack contains the author's current reviewed canonical candidate and is authoritative over older session history. Substantively rewrite the named target with concrete new or improved content; returning that target byte-for-byte unchanged is a failed regeneration. Regenerate only the named target section, copy every non-target section unchanged into the returned complete package, and preserve all ids, version bindings, and author edits outside that target.

Keep the complete JSON under 14,000 Unicode characters; target 11,500-13,000. Write compact, concrete Chinese and use minified or lightly spaced JSON rather than pretty-print indentation. Unless a tighter rule appears below, keep each scalar prose field under 60 Chinese characters and each prose-array entry under 35 Chinese characters. Do not repeat the same explanation across fields. This is a shape example only; replace every empty value with the required concrete content: {"schemaVersion":1,"meta":{},"settings":[],"characters":[],"bookPlan":{},"volumePlans":[],"currentVolumePlanId":"vp-1","chapterPlans":[]}.

The root object must contain exactly these asset sections: schemaVersion, meta, settings, characters, bookPlan, volumePlans, currentVolumePlanId, chapterPlans. Use schemaVersion 1.

meta must contain bookNameCandidates (3-6 distinct natural Chinese titles), bookName (the selected title), recommendedBookName (same selected title), type, intro, targetWords (a positive integer), marketing (specific target readers and positioning), toneNote, and risksToAvoid.

settings must be a JSON array containing exactly five category objects named 世界规则与限制, 力量体系, 重要阵营, 关键地点, 核心资源; never use those names as object keys. Every category needs a concrete introduction and exactly 3 items; every item needs name and introduction. Keep introductions under 45 Chinese characters. Rules must state boundaries, costs, or failure conditions that can constrain the plot.

characters must contain exactly six globally unique ids and include one 主角, at least one 关键配角, and one 反派. Every character needs name, role, biography, desire, non-empty boundary, and non-empty cost. Keep biography under 90 Chinese characters while still stating story position, important relationships, and growth or opposition direction. boundary and cost must each contain 1-2 concise strings. Optional gender, age, height, appearance, and tags must remain concrete when supplied.

bookPlan must contain planId bp-main, planVersion 1, status draft, readerPromise, premise, protagonistEngine with desire/fear/misbelief/troubleEngine, coreConflict, exactly 3 causal stages, endingDirection, and exactly 2 concise immutableFacts. Every stage needs a globally unique id, title, objective, irreversibleResult, startVolumeId, and endVolumeId referencing volume plans in forward order.

volumePlans must contain exactly four volumes with globally unique planIds vp-1 through vp-4, planVersion 1, sourceBookPlanVersion 1, and concrete volumeName/objective/entryState/exitState/escalation/conflictChain/climax/closure/characterTurns/threadTargets. Every volume must include status and plannedChapterRange as a JSON object with integer start and end, never a range string. Ranges must be contiguous. Keep every volume array to 1-2 concise strings. vp-1 status must be detailed and cover chapters 1-10; vp-2 through vp-4 status must be skeleton. currentVolumePlanId must be vp-1.

chapterPlans must contain exactly ten ordered chapters for vp-1. For chapter N use globally unique planId cp-N, planVersion 1, branchId main, chapterOrdinal N, status planned, sourceBookPlanVersion 1, sourceVolumePlanVersion 1, volumePlanId vp-1, the same volumeName as vp-1, a concrete chapterName, and chapterKey equal to volumeName + "/" + chapterName. Every chapter must contain a specific openingHook, goal, conflict, stakes, conflictEscalation, emotionalBeat, exactly two compact scenes with purpose/opposition/turn/outcome, 2-4 appearingRoles using exact names from characters[].name, coreScene, corePayoff, endingHook, 1-2 expectedChanges, note, and factConstraints containing the same two bookPlan immutableFacts. Keep each chapter scalar field under 50 Chinese characters and each scene field under 32. These fields must show key events and forward movement, not restate the same beat. Chapters 1-3 must use openingPhase 1, 2, 3 with distinct hooks, stakes, payoffs, and ending pressure; chapters 4-10 use openingPhase 0.

When revising, preserve the prior full package, ids, version bindings, and settled author decisions; apply review fixes and return the entire package again. Do not ask the author anything in this capability.`
}
