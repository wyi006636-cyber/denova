package yanzhouadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"denova/internal/agent"
	"denova/internal/yanzhouprotocol"
)

type PlanBlockKind = agent.ProtocolPlanBlockKind

const (
	PlanBlockQuestions PlanBlockKind = agent.ProtocolPlanBlockQuestions
	PlanBlockProposal  PlanBlockKind = agent.ProtocolPlanBlockProposal
)

type PlanBlock = agent.ProtocolPlanBlock
type PlanParseResult = agent.ProtocolPlanParseResult

type PlanStreamParser struct {
	parser *agent.ProtocolPlanStreamParser
}

func NewPlanStreamParser() *PlanStreamParser {
	return &PlanStreamParser{parser: agent.NewProtocolPlanStreamParser()}
}

func (p *PlanStreamParser) Push(content string) (PlanParseResult, error) {
	return p.parser.Push(content)
}

func (p *PlanStreamParser) Flush() (PlanParseResult, error) {
	return p.parser.Flush()
}

func (p *PlanStreamParser) Stopped() bool {
	return p != nil && p.parser.Stopped()
}

func ParsePlanToolCall(name, args string) (PlanBlock, bool, error) {
	return agent.ParseProtocolPlanToolCall(name, args)
}

type PlanQuestionMode string

const (
	PlanQuestionSingle   PlanQuestionMode = "single"
	PlanQuestionMulti    PlanQuestionMode = "multi"
	PlanQuestionFreeform PlanQuestionMode = "freeform"
	PlanQuestionRank     PlanQuestionMode = "rank"
	PlanQuestionScale    PlanQuestionMode = "scale"
)

var planQuestionTopics = []string{
	"genre", "reader_promise", "protagonist", "desire", "conflict", "stakes",
	"world_rule", "relationship", "tone", "structure", "taboo", "reference", "publishing",
}

var planQuestionModes = []PlanQuestionMode{
	PlanQuestionSingle, PlanQuestionMulti, PlanQuestionFreeform, PlanQuestionRank, PlanQuestionScale,
}

var planSchemaIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type PlanQuestionOption struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Rationale string `json:"rationale,omitempty"`
}

type PlanQuestionDependency struct {
	QuestionID string `json:"questionId"`
	Answer     any    `json:"answer"`
}

type PlanScale struct {
	Min  int `json:"min"`
	Max  int `json:"max"`
	Step int `json:"step"`
}

type PlanQuestion struct {
	ID                   string                   `json:"id"`
	Topic                string                   `json:"topic"`
	Prompt               string                   `json:"prompt"`
	Mode                 PlanQuestionMode         `json:"mode"`
	Options              []PlanQuestionOption     `json:"options,omitempty"`
	RecommendedOptionIDs []string                 `json:"recommendedOptionIds,omitempty"`
	AllowCustom          bool                     `json:"allowCustom"`
	Required             bool                     `json:"required"`
	DependsOn            []PlanQuestionDependency `json:"dependsOn,omitempty"`
	Scale                *PlanScale               `json:"scale,omitempty"`
}

type PlanQuestionGroup struct {
	SchemaVersion          string         `json:"schemaVersion"`
	ID                     string         `json:"id"`
	Round                  int            `json:"round"`
	Goal                   string         `json:"goal"`
	Questions              []PlanQuestion `json:"questions"`
	RemainingUncertainties []string       `json:"remainingUncertainties"`
}

type PlanSection struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Objective string `json:"objective"`
}

type PlanApprovals struct {
	PlanApproved      bool `json:"planApproved"`
	ExecutionApproved bool `json:"executionApproved"`
	WriteApproved     bool `json:"writeApproved"`
}

type ProposedPlan struct {
	SchemaVersion string        `json:"schemaVersion"`
	ID            string        `json:"id"`
	Revision      int           `json:"revision"`
	Status        string        `json:"status"`
	Summary       string        `json:"summary"`
	Sections      []PlanSection `json:"sections"`
	Approvals     PlanApprovals `json:"approvals"`
}

func PlanQuestionTopics() []string {
	return append([]string(nil), planQuestionTopics...)
}

func PlanQuestionModes() []PlanQuestionMode {
	return append([]PlanQuestionMode(nil), planQuestionModes...)
}

func (g PlanQuestionGroup) Validate() error {
	if g.SchemaVersion != "1" || !validPlanSchemaID(g.ID) || g.Round < 1 || g.Round > 10_000 || !boundedPlanText(g.Goal, 2048) {
		return fmt.Errorf("plan question group identity is invalid")
	}
	if len(g.Questions) < 1 || len(g.Questions) > 16 || len(g.RemainingUncertainties) > 32 {
		return fmt.Errorf("plan question group size is invalid")
	}
	uncertainties := map[string]bool{}
	for _, uncertainty := range g.RemainingUncertainties {
		if !boundedPlanText(uncertainty, 1024) || uncertainties[uncertainty] {
			return fmt.Errorf("plan question group uncertainties are invalid")
		}
		uncertainties[uncertainty] = true
	}
	earlier := map[string]bool{}
	for index, question := range g.Questions {
		if err := validatePlanQuestion(question, earlier); err != nil {
			return fmt.Errorf("plan question %d: %w", index, err)
		}
		if earlier[question.ID] {
			return fmt.Errorf("plan question id is duplicated: %s", question.ID)
		}
		earlier[question.ID] = true
	}
	return nil
}

func (p ProposedPlan) Validate() error {
	if p.SchemaVersion != "1" || !validPlanSchemaID(p.ID) || p.Revision < 1 || p.Revision > 1_000_000 || p.Status != "proposed" || !boundedPlanText(p.Summary, 16*1024) {
		return fmt.Errorf("proposed plan identity is invalid")
	}
	if len(p.Sections) < 1 || len(p.Sections) > 64 {
		return fmt.Errorf("proposed plan sections are invalid")
	}
	seen := map[string]bool{}
	for _, section := range p.Sections {
		if !validPlanSchemaID(section.ID) || !boundedPlanText(section.Title, 512) || !boundedPlanText(section.Objective, 4096) || seen[section.ID] {
			return fmt.Errorf("proposed plan section is invalid")
		}
		seen[section.ID] = true
	}
	if p.Approvals.PlanApproved || p.Approvals.ExecutionApproved || p.Approvals.WriteApproved {
		return fmt.Errorf("a proposed plan cannot carry approval")
	}
	return nil
}

type planQuestionWire struct {
	ID                   string                   `json:"id"`
	Topic                string                   `json:"topic"`
	Prompt               string                   `json:"prompt"`
	Mode                 PlanQuestionMode         `json:"mode"`
	Options              []PlanQuestionOption     `json:"options,omitempty"`
	RecommendedOptionIDs []string                 `json:"recommendedOptionIds,omitempty"`
	AllowCustom          *bool                    `json:"allowCustom"`
	Required             *bool                    `json:"required"`
	DependsOn            []PlanQuestionDependency `json:"dependsOn,omitempty"`
	Scale                *PlanScale               `json:"scale,omitempty"`
}

type planQuestionGroupWire struct {
	SchemaVersion          string             `json:"schemaVersion"`
	ID                     string             `json:"id"`
	Round                  int                `json:"round"`
	Goal                   string             `json:"goal"`
	Questions              []planQuestionWire `json:"questions"`
	RemainingUncertainties []string           `json:"remainingUncertainties"`
}

// DecodePlanQuestionGroup admits one closed bounded schema. Pointer booleans
// distinguish an explicit false from a model silently omitting a required field.
func DecodePlanQuestionGroup(raw []byte) (PlanQuestionGroup, error) {
	var wire planQuestionGroupWire
	if err := decodeStrictPlanJSON(raw, 64*1024, &wire); err != nil {
		return PlanQuestionGroup{}, invalidPlanPayload()
	}
	group := PlanQuestionGroup{
		SchemaVersion:          wire.SchemaVersion,
		ID:                     wire.ID,
		Round:                  wire.Round,
		Goal:                   wire.Goal,
		RemainingUncertainties: wire.RemainingUncertainties,
		Questions:              make([]PlanQuestion, 0, len(wire.Questions)),
	}
	for _, question := range wire.Questions {
		if question.AllowCustom == nil || question.Required == nil {
			return PlanQuestionGroup{}, invalidPlanPayload()
		}
		group.Questions = append(group.Questions, PlanQuestion{
			ID:                   question.ID,
			Topic:                question.Topic,
			Prompt:               question.Prompt,
			Mode:                 question.Mode,
			Options:              question.Options,
			RecommendedOptionIDs: question.RecommendedOptionIDs,
			AllowCustom:          *question.AllowCustom,
			Required:             *question.Required,
			DependsOn:            question.DependsOn,
			Scale:                question.Scale,
		})
	}
	if err := group.Validate(); err != nil {
		return PlanQuestionGroup{}, invalidPlanPayload()
	}
	return group, nil
}

func DecodeProposedPlan(raw []byte) (ProposedPlan, error) {
	var proposal ProposedPlan
	if err := decodeStrictPlanJSON(raw, 64*1024, &proposal); err != nil {
		return ProposedPlan{}, invalidPlanPayload()
	}
	if err := proposal.Validate(); err != nil {
		return ProposedPlan{}, invalidPlanPayload()
	}
	return proposal, nil
}

func decodeStrictPlanJSON(raw []byte, maxBytes int, destination any) error {
	if len(raw) == 0 || len(raw) > maxBytes {
		return invalidPlanPayload()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return invalidPlanPayload()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return invalidPlanPayload()
	}
	return nil
}

func invalidPlanPayload() error {
	return errors.New("plan payload is invalid")
}

func validatePlanQuestion(question PlanQuestion, earlier map[string]bool) error {
	if !validPlanSchemaID(question.ID) || !containsPlanString(planQuestionTopics, question.Topic) || !boundedPlanText(question.Prompt, 4096) || !containsPlanMode(question.Mode) {
		return fmt.Errorf("question identity, topic, prompt, or mode is invalid")
	}
	optionMode := question.Mode == PlanQuestionSingle || question.Mode == PlanQuestionMulti || question.Mode == PlanQuestionRank
	if optionMode {
		if len(question.Options) < 2 || len(question.Options) > 32 || question.Scale != nil {
			return fmt.Errorf("question options are invalid")
		}
		optionIDs := map[string]bool{}
		for _, option := range question.Options {
			if !validPlanSchemaID(option.ID) || !boundedPlanText(option.Label, 256) || (option.Rationale != "" && !boundedPlanText(option.Rationale, 1024)) || optionIDs[option.ID] {
				return fmt.Errorf("question option is invalid")
			}
			optionIDs[option.ID] = true
		}
		recommended := map[string]bool{}
		for _, id := range question.RecommendedOptionIDs {
			if !optionIDs[id] || recommended[id] {
				return fmt.Errorf("recommended option is invalid")
			}
			recommended[id] = true
		}
	} else {
		if len(question.Options) != 0 || len(question.RecommendedOptionIDs) != 0 {
			return fmt.Errorf("options are not allowed for this question mode")
		}
		if question.Mode == PlanQuestionScale {
			if question.Scale == nil || question.Scale.Min >= question.Scale.Max || question.Scale.Step < 1 || (question.Scale.Max-question.Scale.Min)/question.Scale.Step > 100 {
				return fmt.Errorf("question scale is invalid")
			}
		} else if question.Scale != nil {
			return fmt.Errorf("scale is only allowed for scale questions")
		}
	}
	if len(question.DependsOn) > 16 {
		return fmt.Errorf("too many question dependencies")
	}
	dependencies := map[string]bool{}
	for _, dependency := range question.DependsOn {
		if !earlier[dependency.QuestionID] || dependencies[dependency.QuestionID] || !boundedPlanJSON(dependency.Answer) {
			return fmt.Errorf("question dependsOn is invalid")
		}
		dependencies[dependency.QuestionID] = true
	}
	return nil
}

func validPlanSchemaID(value string) bool {
	return planSchemaIDPattern.MatchString(strings.TrimSpace(value)) && strings.TrimSpace(value) == value
}

func boundedPlanText(value string, max int) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= max
}

func boundedPlanJSON(value any) bool {
	data, err := json.Marshal(value)
	return err == nil && len(data) <= 16*1024
}

func containsPlanString(values []string, value string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func containsPlanMode(value PlanQuestionMode) bool {
	for _, candidate := range planQuestionModes {
		if value == candidate {
			return true
		}
	}
	return false
}

type planModelProfile struct {
	ProfileID    string            `json:"profileId"`
	ProviderType ProviderType      `json:"providerType"`
	AdapterID    string            `json:"adapterId"`
	BaseURL      string            `json:"baseUrl,omitempty"`
	Model        string            `json:"model"`
	Capabilities json.RawMessage   `json:"capabilities"`
	TimeoutMS    int               `json:"timeoutMs"`
	ExtraHeaders map[string]string `json:"extraHeaders,omitempty"`
	RuntimeAuth  RuntimeAuth       `json:"runtimeAuth"`
	Resolution   json.RawMessage   `json:"resolution"`
}

func (profile planModelProfile) effective() EffectiveModelProfile {
	return EffectiveModelProfile{
		ProfileID: profile.ProfileID, ProviderType: profile.ProviderType, AdapterID: profile.AdapterID,
		BaseURL: profile.BaseURL, Model: profile.Model, TimeoutMS: profile.TimeoutMS,
		ExtraHeaders: profile.ExtraHeaders, RuntimeAuth: profile.RuntimeAuth,
	}
}

type planRunBudget struct {
	MaxModelCalls     int      `json:"maxModelCalls"`
	MaxToolRounds     int      `json:"maxToolRounds"`
	MaxDelegations    int      `json:"maxDelegations"`
	MaxRevisionRounds int      `json:"maxRevisionRounds"`
	MaxWallTimeMS     int      `json:"maxWallTimeMs"`
	MaxInputTokens    *int     `json:"maxInputTokens,omitempty"`
	MaxOutputTokens   *int     `json:"maxOutputTokens,omitempty"`
	MaxEstimatedCost  *float64 `json:"maxEstimatedCost,omitempty"`
}

type planContentRef struct {
	Ref string `json:"ref"`
}

type planRunRequest struct {
	SchemaVersion         string            `json:"schemaVersion"`
	RequestID             string            `json:"requestId"`
	IdempotencyKey        string            `json:"idempotencyKey"`
	RunID                 string            `json:"runId"`
	SessionID             string            `json:"sessionId"`
	AgentKind             string            `json:"agentKind"`
	Entrypoint            string            `json:"entrypoint"`
	Target                json.RawMessage   `json:"target"`
	CapabilityID          string            `json:"capabilityId,omitempty"`
	UserIntent            string            `json:"userIntent"`
	ExplicitContinue      bool              `json:"explicitContinue,omitempty"`
	PlanMode              bool              `json:"planMode"`
	SelectedSkillIDs      []string          `json:"selectedSkillIds"`
	HarnessProfile        string            `json:"harnessProfile,omitempty"`
	EffectiveModelProfile planModelProfile  `json:"effectiveModelProfile"`
	ContextPackRef        planContentRef    `json:"contextPackRef"`
	ToolManifest          json.RawMessage   `json:"toolCapabilityManifest"`
	Budgets               planRunBudget     `json:"budgets"`
	BaseRevisions         map[string]string `json:"baseRevisions"`
	DisplayLocale         string            `json:"displayLocale"`
}

type planRunResume struct {
	SchemaVersion  string         `json:"schemaVersion"`
	RunID          string         `json:"runId"`
	GroupID        string         `json:"groupId,omitempty"`
	Answers        map[string]any `json:"answers,omitempty"`
	Skip           bool           `json:"skip,omitempty"`
	UseRecommended bool           `json:"useRecommended,omitempty"`
	PlanID         string         `json:"planId,omitempty"`
	Revision       int            `json:"revision,omitempty"`
	Action         string         `json:"action,omitempty"`
	Message        string         `json:"message,omitempty"`
}

type planRuntimeState struct {
	request                  planRunRequest
	adapter                  ModelAdapter
	messages                 []ModelMessage
	asked                    map[string]bool
	lastGroup                *PlanQuestionGroup
	proposal                 *ProposedPlan
	modelCalls               int
	questionRounds           int
	planApproved             bool
	executionApproved        bool
	expectedProposalRevision int
	toolRounds               int
}

type planGenerationError struct{ code, message string }

func (failure *planGenerationError) Error() string { return failure.code }

// PlanFrameRuntime owns only ephemeral orchestration. Public state is emitted
// through EmitRunEvent, whose append-before-write ordering is the authority.
type PlanFrameRuntime struct {
	mu                 sync.Mutex
	store              RuntimeEventStore
	client             *http.Client
	runs               map[string]*planRuntimeState
	idempotency        map[string]string
	responseMu         sync.Mutex
	pendingResponses   map[string]chan yanzhouprotocol.Envelope
	earlyToolResponses map[string]yanzhouprotocol.Envelope
}

func NewPlanFrameRuntime(store RuntimeEventStore, client *http.Client) (*PlanFrameRuntime, error) {
	if store == nil {
		return nil, fmt.Errorf("plan runtime event store is required")
	}
	if client == nil {
		client = &http.Client{}
	}
	return &PlanFrameRuntime{
		store: store, client: client, runs: map[string]*planRuntimeState{}, idempotency: map[string]string{},
		pendingResponses: map[string]chan yanzhouprotocol.Envelope{}, earlyToolResponses: map[string]yanzhouprotocol.Envelope{},
	}, nil
}

func (runtime *PlanFrameRuntime) CancelRun(ctx context.Context, runID string, output io.Writer) error {
	if runtime == nil || !validPlanSchemaID(runID) || output == nil {
		return errors.New("plan run cancel is invalid")
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	state := runtime.runs[runID]
	if state == nil {
		return errors.New("plan run is not active")
	}
	_, err := EmitRunEvent(ctx, runtime.store, output, runID, RuntimeEventInput{Type: RunEventTypeRunAborted, Payload: map[string]any{
		"schemaVersion": "1", "reason": "cancelled", "resumable": false, "partialArtifactRefs": []string{},
	}})
	if err == nil {
		delete(runtime.runs, runID)
		delete(runtime.idempotency, state.request.IdempotencyKey)
	}
	return err
}

// HandleToolResponse reuses the Sidecar's existing tool.response exchange for
// Plan Mode. Only the Task 4 story read tools are admitted before approval.
func (runtime *PlanFrameRuntime) HandleToolResponse(frame yanzhouprotocol.Envelope) error {
	if runtime == nil || frame.Validate() != nil || frame.Kind != yanzhouprotocol.KindToolResponse || !strings.HasPrefix(frame.RequestID, "tool-"+frame.RunID+"-") {
		return errors.New("plan tool response is invalid")
	}
	var payload writingToolResponsePayload
	if err := decodeStrictPlanJSON(frame.Payload, yanzhouprotocol.DefaultMaxFrameBytes, &payload); err != nil || payload.SchemaVersion != "1" || !writingReadTool(payload.ToolID) {
		return errors.New("plan tool response is invalid")
	}
	runtime.responseMu.Lock()
	defer runtime.responseMu.Unlock()
	if pending := runtime.pendingResponses[frame.RequestID]; pending != nil {
		select {
		case pending <- frame:
			return nil
		default:
			return errors.New("plan tool response is duplicated")
		}
	}
	if _, exists := runtime.earlyToolResponses[frame.RequestID]; exists || len(runtime.earlyToolResponses) >= 128 {
		return errors.New("plan tool response is invalid")
	}
	runtime.earlyToolResponses[frame.RequestID] = frame
	return nil
}

func (runtime *PlanFrameRuntime) registerToolResponse(requestID string) (chan yanzhouprotocol.Envelope, *yanzhouprotocol.Envelope, error) {
	runtime.responseMu.Lock()
	defer runtime.responseMu.Unlock()
	if runtime.pendingResponses[requestID] != nil {
		return nil, nil, errors.New("plan tool request is duplicated")
	}
	if early, ok := runtime.earlyToolResponses[requestID]; ok {
		delete(runtime.earlyToolResponses, requestID)
		return nil, &early, nil
	}
	response := make(chan yanzhouprotocol.Envelope, 1)
	runtime.pendingResponses[requestID] = response
	return response, nil, nil
}

func (runtime *PlanFrameRuntime) clearToolResponse(requestID string) {
	runtime.responseMu.Lock()
	delete(runtime.pendingResponses, requestID)
	runtime.responseMu.Unlock()
}

func (runtime *PlanFrameRuntime) HandleFrame(ctx context.Context, frame yanzhouprotocol.Envelope, output io.Writer) error {
	if runtime == nil || output == nil {
		return errors.New("plan frame runtime is unavailable")
	}
	if err := frame.Validate(); err != nil {
		return errors.New("plan frame is invalid")
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	switch frame.Kind {
	case yanzhouprotocol.KindRunStart:
		return runtime.handleStart(ctx, frame, output)
	case yanzhouprotocol.KindRunResume:
		return runtime.handleResume(ctx, frame, output)
	default:
		return errors.New("plan frame kind is not supported")
	}
}

func (runtime *PlanFrameRuntime) handleStart(ctx context.Context, frame yanzhouprotocol.Envelope, output io.Writer) error {
	var request planRunRequest
	if err := decodeStrictPlanJSON(frame.Payload, yanzhouprotocol.DefaultMaxFrameBytes, &request); err != nil || request.Validate(frame.RequestID) != nil {
		return errors.New("plan run request is invalid")
	}
	if existingRunID, exists := runtime.idempotency[request.IdempotencyKey]; exists {
		if existingRunID != request.RunID {
			return errors.New("plan run idempotency conflict")
		}
		return nil
	}
	if _, exists := runtime.runs[request.RunID]; exists {
		return errors.New("plan run already exists")
	}
	adapter, err := NewModelAdapter(request.EffectiveModelProfile.effective())
	if err != nil {
		return errors.New("plan model profile is invalid")
	}
	state := &planRuntimeState{
		request: request, adapter: adapter, asked: map[string]bool{},
		messages: []ModelMessage{
			{Role: "system", Content: planModeSystemInstruction()},
			{Role: "user", Content: request.UserIntent},
		},
	}
	runtime.runs[request.RunID] = state
	runtime.idempotency[request.IdempotencyKey] = request.RunID
	if _, err := EmitRunEvent(ctx, runtime.store, output, request.RunID, RuntimeEventInput{
		Type: RunEventTypeRunStarted,
		Payload: map[string]any{
			"sessionId": request.SessionID, "agentKind": request.AgentKind, "planMode": true,
		},
	}); err != nil {
		delete(runtime.runs, request.RunID)
		delete(runtime.idempotency, request.IdempotencyKey)
		return err
	}
	return runtime.runModelRoundSettled(ctx, state, output)
}

func (runtime *PlanFrameRuntime) handleResume(ctx context.Context, frame yanzhouprotocol.Envelope, output io.Writer) error {
	var resume planRunResume
	if err := decodeStrictPlanJSON(frame.Payload, 64*1024, &resume); err != nil || resume.Validate() != nil {
		return errors.New("plan resume request is invalid")
	}
	state := runtime.runs[resume.RunID]
	if state == nil {
		return errors.New("plan resume state is invalid")
	}
	if resume.Action != "" {
		return runtime.handlePlanCommand(ctx, state, resume, output)
	}
	if state.lastGroup == nil || state.proposal != nil || state.lastGroup.ID != resume.GroupID {
		return errors.New("plan resume state is invalid")
	}
	answerPayload := map[string]any{"groupId": resume.GroupID}
	switch {
	case resume.Skip:
		answerPayload["skipped"] = true
	case resume.UseRecommended:
		answerPayload["useRecommended"] = true
	default:
		if err := validatePlanAnswers(*state.lastGroup, resume.Answers); err != nil {
			return errors.New("plan answers are invalid")
		}
		answerPayload["answers"] = resume.Answers
	}
	answerJSON, err := json.Marshal(answerPayload)
	if err != nil {
		return errors.New("plan answers are invalid")
	}
	state.messages = append(state.messages, ModelMessage{Role: "user", Content: string(answerJSON)})
	state.lastGroup = nil
	return runtime.runModelRoundSettled(ctx, state, output)
}

func (request planRunRequest) Validate(envelopeRequestID string) error {
	if request.SchemaVersion != "1" || request.RequestID != envelopeRequestID || !validPlanSchemaID(request.RequestID) || !validPlanSchemaID(request.IdempotencyKey) || !validPlanSchemaID(request.RunID) || !validPlanSchemaID(request.SessionID) {
		return invalidPlanPayload()
	}
	if request.AgentKind == "" || request.Entrypoint == "" || !boundedPlanText(request.UserIntent, 32*1024) || !request.PlanMode || !boundedPlanText(request.DisplayLocale, 64) {
		return invalidPlanPayload()
	}
	if len(request.SelectedSkillIDs) > 64 || request.Budgets.MaxModelCalls < 1 || request.Budgets.MaxModelCalls > 100 || request.Budgets.MaxWallTimeMS < 1 || request.Budgets.MaxWallTimeMS > 24*60*60*1000 {
		return invalidPlanPayload()
	}
	seenSkills := map[string]bool{}
	for _, id := range request.SelectedSkillIDs {
		if !validPlanSchemaID(id) || seenSkills[id] {
			return invalidPlanPayload()
		}
		seenSkills[id] = true
	}
	if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(request.ContextPackRef.Ref) || len(request.BaseRevisions) > 128 {
		return invalidPlanPayload()
	}
	for key, value := range request.BaseRevisions {
		if !validPlanSchemaID(key) || !boundedPlanText(value, 256) {
			return invalidPlanPayload()
		}
	}
	for _, raw := range []json.RawMessage{request.Target, request.ToolManifest, request.EffectiveModelProfile.Capabilities, request.EffectiveModelProfile.Resolution} {
		if !validPlanOpaqueObject(raw) {
			return invalidPlanPayload()
		}
	}
	return nil
}

func (resume planRunResume) Validate() error {
	if resume.SchemaVersion != "1" || !validPlanSchemaID(resume.RunID) {
		return invalidPlanPayload()
	}
	if resume.Action == "" {
		modeCount := 0
		if len(resume.Answers) > 0 {
			modeCount++
		}
		if resume.Skip {
			modeCount++
		}
		if resume.UseRecommended {
			modeCount++
		}
		if !validPlanSchemaID(resume.GroupID) || modeCount != 1 || len(resume.Answers) > 16 || !boundedPlanJSON(resume.Answers) || resume.PlanID != "" || resume.Revision != 0 || resume.Message != "" {
			return invalidPlanPayload()
		}
		return nil
	}
	if resume.GroupID != "" || len(resume.Answers) != 0 || resume.Skip || resume.UseRecommended {
		return invalidPlanPayload()
	}
	if resume.Action == "exit" {
		if resume.Message != "" || ((resume.PlanID == "") != (resume.Revision == 0)) || (resume.PlanID != "" && (!validPlanSchemaID(resume.PlanID) || resume.Revision < 1 || resume.Revision > 1_000_000)) {
			return invalidPlanPayload()
		}
		return nil
	}
	if !validPlanSchemaID(resume.PlanID) || resume.Revision < 1 || resume.Revision > 1_000_000 {
		return invalidPlanPayload()
	}
	switch resume.Action {
	case "discuss", "modify":
		if !boundedPlanText(resume.Message, 16*1024) {
			return invalidPlanPayload()
		}
	case "exit", "approve_plan", "approve_execution", "approve_write":
		if resume.Message != "" {
			return invalidPlanPayload()
		}
	default:
		return invalidPlanPayload()
	}
	return nil
}

func (runtime *PlanFrameRuntime) handlePlanCommand(ctx context.Context, state *planRuntimeState, resume planRunResume, output io.Writer) error {
	if resume.Action == "exit" {
		_, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{
			Type: RunEventTypeRunAborted,
			Payload: map[string]any{
				"schemaVersion": "1", "reason": "cancelled", "resumable": false, "partialArtifactRefs": []string{},
			},
		})
		return err
	}
	if state.proposal == nil || state.proposal.ID != resume.PlanID || state.proposal.Revision != resume.Revision {
		return errors.New("plan command revision is stale")
	}
	switch resume.Action {
	case "discuss", "modify":
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{
			Type: RunEventTypeRevisionRequested,
			Payload: map[string]any{
				"planId": resume.PlanID, "revision": resume.Revision, "reason": "author_requested",
			},
		}); err != nil {
			return err
		}
		command, _ := json.Marshal(map[string]any{
			"action": resume.Action, "planId": resume.PlanID, "revision": resume.Revision, "message": resume.Message,
		})
		state.messages = append(state.messages, ModelMessage{Role: "user", Content: string(command)})
		state.expectedProposalRevision = resume.Revision + 1
		state.proposal = nil
		state.planApproved = false
		state.executionApproved = false
		return runtime.runModelRoundSettled(ctx, state, output)
	case "approve_plan":
		if state.planApproved {
			return errors.New("plan is already approved")
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{
			Type: RunEventTypePlanApproved,
			Payload: map[string]any{
				"planId": resume.PlanID, "revision": resume.Revision, "approvalKind": "plan",
			},
		}); err != nil {
			return err
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{
			Type: RunEventTypeRunWaitingAuthor,
			Payload: map[string]any{
				"reason": "execution_approval", "planId": resume.PlanID, "revision": resume.Revision,
			},
		}); err != nil {
			return err
		}
		state.planApproved = true
		return nil
	case "approve_execution":
		if !state.planApproved || state.executionApproved {
			return errors.New("plan execution approval transition is invalid")
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{
			Type: RunEventTypePlanApproved,
			Payload: map[string]any{
				"planId": resume.PlanID, "revision": resume.Revision, "approvalKind": "execution",
			},
		}); err != nil {
			return err
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{
			Type: RunEventTypeRunCompleted,
			Payload: map[string]any{
				"schemaVersion": "1", "reason": "completed", "resumable": false, "partialArtifactRefs": []string{},
			},
		}); err != nil {
			return err
		}
		state.executionApproved = true
		return nil
	case "approve_write":
		return errors.New("formal write approval is unavailable in WP4")
	default:
		return errors.New("plan command is invalid")
	}
}

func validPlanOpaqueObject(raw json.RawMessage) bool {
	if len(raw) == 0 || len(raw) > 64*1024 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return false
	}
	return !planValueContainsForbiddenKey(value)
}

func planValueContainsForbiddenKey(value any) bool {
	forbidden := map[string]bool{
		"bookpath": true, "bookroot": true, "booksdir": true, "workspacepath": true,
		"workspaceroot": true, "cwd": true, "filesystem": true, "shell": true, "directwrite": true,
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
			if forbidden[normalized] || planValueContainsForbiddenKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if planValueContainsForbiddenKey(child) {
				return true
			}
		}
	}
	return false
}

func validatePlanAnswers(group PlanQuestionGroup, answers map[string]any) error {
	questions := map[string]PlanQuestion{}
	for _, question := range group.Questions {
		questions[question.ID] = question
	}
	for id, answer := range answers {
		if _, exists := questions[id]; !exists || !boundedPlanJSON(answer) {
			return invalidPlanPayload()
		}
	}
	for _, question := range group.Questions {
		active := true
		for _, dependency := range question.DependsOn {
			answer, exists := answers[dependency.QuestionID]
			if !exists || !equalPlanJSON(answer, dependency.Answer) {
				active = false
				break
			}
		}
		answer, answered := answers[question.ID]
		if !active && answered {
			return invalidPlanPayload()
		}
		if active && question.Required && !answered {
			return invalidPlanPayload()
		}
		if active && answered && !validPlanAnswer(question, answer) {
			return invalidPlanPayload()
		}
	}
	return nil
}

func equalPlanJSON(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func validPlanAnswer(question PlanQuestion, answer any) bool {
	optionIDs := map[string]bool{}
	for _, option := range question.Options {
		optionIDs[option.ID] = true
	}
	validChoice := func(value any) bool {
		choice, ok := value.(string)
		return ok && boundedPlanText(choice, 4096) && (optionIDs[choice] || question.AllowCustom)
	}
	switch question.Mode {
	case PlanQuestionSingle:
		return validChoice(answer)
	case PlanQuestionMulti, PlanQuestionRank:
		values, ok := answer.([]any)
		if !ok || len(values) > 32 || (len(values) == 0 && question.Required) {
			return false
		}
		seen := map[string]bool{}
		for _, value := range values {
			choice, ok := value.(string)
			if !ok || seen[choice] || !validChoice(choice) {
				return false
			}
			seen[choice] = true
		}
		if question.Mode == PlanQuestionRank && !question.AllowCustom {
			if len(values) != len(optionIDs) {
				return false
			}
			for optionID := range optionIDs {
				if !seen[optionID] {
					return false
				}
			}
		}
		return true
	case PlanQuestionFreeform:
		value, ok := answer.(string)
		return ok && boundedPlanText(value, 4096)
	case PlanQuestionScale:
		value, ok := planInteger(answer)
		return ok && question.Scale != nil && value >= question.Scale.Min && value <= question.Scale.Max && (value-question.Scale.Min)%question.Scale.Step == 0
	default:
		return false
	}
}

func planInteger(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case float64:
		integer := int(number)
		return integer, float64(integer) == number
	case json.Number:
		integer, err := number.Int64()
		return int(integer), err == nil && int64(int(integer)) == integer
	default:
		return 0, false
	}
}

func (runtime *PlanFrameRuntime) runModelRound(ctx context.Context, state *planRuntimeState, output io.Writer) error {
	if state.modelCalls >= state.request.Budgets.MaxModelCalls {
		return &planGenerationError{"plan_budget_exhausted", "计划生成调用次数已达上限，正文没有修改"}
	}
	state.modelCalls++
	tools := []ModelTool{
		{Name: "plan_questions", Description: "Ask one bounded group of planning questions using the exact schema", InputSchema: planQuestionsToolSchema()},
		{Name: "proposed_plan", Description: "Propose a discussable plan using the exact schema after uncertainties are resolved", InputSchema: proposedPlanToolSchema()},
	}
	for _, id := range planReadTools(state.request.ToolManifest) {
		tools = append(tools, ModelTool{Name: id, Description: "Read the current story before proposing a plan", InputSchema: map[string]any{"type": "object"}})
	}
	request := ModelRequest{
		Messages:        state.messages,
		Tools:           tools,
		MaxOutputTokens: 4096,
	}
	native, err := state.adapter.BuildRequest(request, false)
	if err != nil {
		return &planGenerationError{"plan_request_invalid", "计划请求无效，正文没有修改"}
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(state.request.EffectiveModelProfile.TimeoutMS)*time.Millisecond)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(callCtx, native.Method, native.URL, bytes.NewReader(native.Body))
	if err != nil {
		return &planGenerationError{"plan_request_invalid", "计划请求无效，正文没有修改"}
	}
	for key, value := range native.Headers {
		httpRequest.Header.Set(key, value)
	}
	response, err := runtime.client.Do(httpRequest)
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return &planGenerationError{"network_timeout", "计划模型响应超时，正文没有修改"}
		}
		return &planGenerationError{"network_error", "无法连接计划模型，正文没有修改"}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(body) > 1024*1024 {
		return &planGenerationError{"plan_response_invalid", "计划模型响应无效，正文没有修改"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		modelError := state.adapter.NormalizeError(response.StatusCode, body)
		message := "计划模型服务拒绝请求，正文没有修改"
		if modelError.Code == "authentication" {
			message = "计划模型凭证不可用，正文没有修改"
		} else if modelError.Retryable {
			message = "计划模型服务暂时不可用，请稍后重试"
		}
		return &planGenerationError{modelError.Code, message}
	}
	modelResponse, err := state.adapter.NormalizeResponse(body)
	if err != nil {
		return &planGenerationError{"plan_response_invalid", "计划模型响应无效，正文没有修改"}
	}
	if len(modelResponse.ToolCalls) == 1 && strings.TrimSpace(modelResponse.Content) == "" && planReadToolAllowed(state.request.ToolManifest, modelResponse.ToolCalls[0].Name) {
		return runtime.requestPlanReadTool(ctx, state, output, modelResponse.ToolCalls[0])
	}
	block, err := parsePlanModelResponse(modelResponse)
	if err == nil {
		err = runtime.acceptPlanBlock(ctx, state, block, output)
	}
	if err != nil {
		if state.modelCalls < state.request.Budgets.MaxModelCalls {
			state.messages = append(state.messages, ModelMessage{Role: "user", Content: "Your previous response was invalid. Call exactly one supplied tool with every required field now and return no prose."})
			return runtime.runModelRound(ctx, state, output)
		}
		return &planGenerationError{"plan_response_invalid", "计划模型没有返回有效问题或计划，正文没有修改"}
	}
	return nil
}

func (runtime *PlanFrameRuntime) runModelRoundSettled(ctx context.Context, state *planRuntimeState, output io.Writer) error {
	if err := runtime.runModelRound(ctx, state, output); err != nil {
		failure := &planGenerationError{code: "plan_generation_failed", message: "计划生成失败，请重试，正文没有修改"}
		var typed *planGenerationError
		if errors.As(err, &typed) {
			failure = typed
		}
		delete(runtime.runs, state.request.RunID)
		delete(runtime.idempotency, state.request.IdempotencyKey)
		_, emitErr := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{Type: RunEventTypeRunFailed, Payload: map[string]any{
			"schemaVersion": "1", "reason": "provider_error", "resumable": false, "partialArtifactRefs": []string{},
			"code": failure.code, "message": failure.message,
		}})
		return emitErr
	}
	return nil
}

func closedPlanSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

func planQuestionsToolSchema() map[string]any {
	option := closedPlanSchema([]string{"id", "label"}, map[string]any{"id": map[string]any{"type": "string"}, "label": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}})
	dependency := closedPlanSchema([]string{"questionId", "answer"}, map[string]any{"questionId": map[string]any{"type": "string"}, "answer": map[string]any{}})
	scale := closedPlanSchema([]string{"min", "max", "step"}, map[string]any{"min": map[string]any{"type": "integer"}, "max": map[string]any{"type": "integer"}, "step": map[string]any{"type": "integer", "minimum": 1}})
	question := closedPlanSchema([]string{"id", "topic", "prompt", "mode", "allowCustom", "required"}, map[string]any{
		"id": map[string]any{"type": "string"}, "topic": map[string]any{"type": "string", "enum": planQuestionTopics},
		"prompt": map[string]any{"type": "string"}, "mode": map[string]any{"type": "string", "enum": planQuestionModes},
		"options":              map[string]any{"type": "array", "items": option},
		"recommendedOptionIds": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"allowCustom":          map[string]any{"type": "boolean"}, "required": map[string]any{"type": "boolean"},
		"dependsOn": map[string]any{"type": "array", "items": dependency}, "scale": scale,
	})
	question["allOf"] = []any{map[string]any{"if": map[string]any{"properties": map[string]any{"mode": map[string]any{"enum": []string{"single", "multi", "rank"}}}}, "then": map[string]any{"required": []string{"options"}, "properties": map[string]any{"options": map[string]any{"minItems": 2, "maxItems": 32}, "scale": false}}}, map[string]any{"if": map[string]any{"properties": map[string]any{"mode": map[string]any{"const": "freeform"}}}, "then": map[string]any{"properties": map[string]any{"options": false, "recommendedOptionIds": false, "scale": false}}}, map[string]any{"if": map[string]any{"properties": map[string]any{"mode": map[string]any{"const": "scale"}}}, "then": map[string]any{"required": []string{"scale"}, "properties": map[string]any{"options": false, "recommendedOptionIds": false}}}}
	return closedPlanSchema([]string{"schemaVersion", "id", "round", "goal", "questions", "remainingUncertainties"}, map[string]any{
		"schemaVersion": map[string]any{"type": "string", "enum": []string{"1"}},
		"id":            map[string]any{"type": "string"}, "round": map[string]any{"type": "integer", "minimum": 1}, "goal": map[string]any{"type": "string"},
		"questions":              map[string]any{"type": "array", "minItems": 1, "items": question},
		"remainingUncertainties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	})
}

func proposedPlanToolSchema() map[string]any {
	section := closedPlanSchema([]string{"id", "title", "objective"}, map[string]any{"id": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "objective": map[string]any{"type": "string"}})
	approvals := closedPlanSchema([]string{"planApproved", "executionApproved", "writeApproved"}, map[string]any{
		"planApproved":      map[string]any{"type": "boolean", "enum": []bool{false}},
		"executionApproved": map[string]any{"type": "boolean", "enum": []bool{false}},
		"writeApproved":     map[string]any{"type": "boolean", "enum": []bool{false}},
	})
	return closedPlanSchema([]string{"schemaVersion", "id", "revision", "status", "summary", "sections", "approvals"}, map[string]any{
		"schemaVersion": map[string]any{"type": "string", "enum": []string{"1"}}, "id": map[string]any{"type": "string"},
		"revision": map[string]any{"type": "integer", "minimum": 1}, "status": map[string]any{"type": "string", "enum": []string{"proposed"}},
		"summary": map[string]any{"type": "string"}, "sections": map[string]any{"type": "array", "minItems": 1, "items": section}, "approvals": approvals,
	})
}

func planReadTools(raw json.RawMessage) []string {
	var manifest struct {
		Capabilities []struct {
			ID   string `json:"id"`
			Mode string `json:"mode"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(raw, &manifest) != nil {
		return nil
	}
	tools := make([]string, 0, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		if capability.Mode == string(ToolCapabilityRead) && writingReadTool(capability.ID) {
			tools = append(tools, capability.ID)
		}
	}
	return tools
}

func planReadToolAllowed(raw json.RawMessage, toolID string) bool {
	for _, candidate := range planReadTools(raw) {
		if candidate == toolID {
			return true
		}
	}
	return false
}

func (runtime *PlanFrameRuntime) requestPlanReadTool(ctx context.Context, state *planRuntimeState, output io.Writer, tool ModelToolCall) error {
	if state.toolRounds >= state.request.Budgets.MaxToolRounds {
		return errors.New("plan tool round budget is exhausted")
	}
	state.toolRounds++
	requestID := fmt.Sprintf("tool-%s-plan-%d", state.request.RunID, state.toolRounds)
	response, early, err := runtime.registerToolResponse(requestID)
	if err != nil {
		return err
	}
	defer runtime.clearToolResponse(requestID)
	if _, err = EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{Type: RunEventTypeToolRequested, Payload: map[string]any{"toolId": tool.Name, "agentId": "primary-writer", "summary": "为计划读取作品资料"}}); err != nil {
		return err
	}
	if _, err = EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{Type: RunEventTypeToolStarted, Payload: map[string]any{"toolId": tool.Name, "agentId": "primary-writer", "summary": "正在读取作品资料"}}); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"schemaVersion": "1", "toolId": tool.Name, "agentId": "primary-writer", "target": json.RawMessage(state.request.Target), "arguments": tool.Arguments})
	if err != nil {
		return err
	}
	if err = yanzhouprotocol.WriteFrame(output, yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindToolRequest, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: requestID, RunID: state.request.RunID, Seq: uint64(10_000 + state.toolRounds), Payload: payload}); err != nil {
		return err
	}
	frame := early
	if frame == nil {
		select {
		case received := <-response:
			frame = &received
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	var responsePayload writingToolResponsePayload
	if err = decodeStrictPlanJSON(frame.Payload, yanzhouprotocol.DefaultMaxFrameBytes, &responsePayload); err != nil || !responsePayload.Success || responsePayload.ToolID != tool.Name || len(responsePayload.Result) == 0 {
		_, _ = EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{Type: RunEventTypeToolCompleted, Payload: map[string]any{"toolId": tool.Name, "agentId": "primary-writer", "status": "failed", "code": "tool_response_invalid", "message": "读取作品资料失败"}})
		return errors.New("plan tool response is invalid")
	}
	if _, err = EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{Type: RunEventTypeToolCompleted, Payload: map[string]any{"toolId": tool.Name, "agentId": "primary-writer", "status": "succeeded", "summary": "已读取作品资料"}}); err != nil {
		return err
	}
	state.messages = append(state.messages,
		ModelMessage{Role: "assistant", Content: fmt.Sprintf("调用了 %s", tool.Name)},
		ModelMessage{Role: "user", Content: fmt.Sprintf("%s 返回：%s", tool.Name, string(responsePayload.Result))},
	)
	return runtime.runModelRound(ctx, state, output)
}

func parsePlanModelResponse(response ModelResponse) (PlanBlock, error) {
	if len(response.ToolCalls) > 0 {
		if len(response.ToolCalls) != 1 || strings.TrimSpace(response.Content) != "" {
			return PlanBlock{}, errors.New("plan model response is ambiguous")
		}
		block, handled, err := ParsePlanToolCall(response.ToolCalls[0].Name, response.ToolCalls[0].Arguments)
		if err != nil || !handled {
			return PlanBlock{}, errors.New("plan model tool call is invalid")
		}
		return block, nil
	}
	parser := NewPlanStreamParser()
	result, err := parser.Push(response.Content)
	if err != nil {
		return PlanBlock{}, errors.New("plan model block is invalid")
	}
	blocks := append([]PlanBlock{}, result.Blocks...)
	if !result.Stop {
		flushed, flushErr := parser.Flush()
		if flushErr != nil {
			return PlanBlock{}, errors.New("plan model block is invalid")
		}
		blocks = append(blocks, flushed.Blocks...)
	}
	if len(blocks) != 1 {
		return PlanBlock{}, errors.New("plan model response must contain one plan block")
	}
	return blocks[0], nil
}

func (runtime *PlanFrameRuntime) acceptPlanBlock(ctx context.Context, state *planRuntimeState, block PlanBlock, output io.Writer) error {
	switch block.Kind {
	case PlanBlockQuestions:
		group, err := DecodePlanQuestionGroup([]byte(block.Content))
		if err != nil || group.Round != state.questionRounds+1 {
			return errors.New("plan question group is invalid")
		}
		for _, question := range group.Questions {
			if state.asked[question.ID] {
				return errors.New("plan question cannot be repeated")
			}
		}
		payload, err := publicPlanPayload(group)
		if err != nil {
			return err
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{Type: RunEventTypePlanQuestions, Payload: payload}); err != nil {
			return err
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{
			Type: RunEventTypeRunWaitingAuthor, Payload: map[string]any{"reason": "plan_questions", "groupId": group.ID},
		}); err != nil {
			return err
		}
		for _, question := range group.Questions {
			state.asked[question.ID] = true
		}
		state.questionRounds++
		state.lastGroup = &group
		encoded, _ := json.Marshal(group)
		state.messages = append(state.messages, ModelMessage{Role: "assistant", Content: string(encoded)})
		return nil
	case PlanBlockProposal:
		proposal, err := DecodeProposedPlan([]byte(block.Content))
		if err != nil || state.lastGroup != nil {
			return errors.New("proposed plan is invalid")
		}
		if state.expectedProposalRevision > 0 {
			if proposal.Revision != state.expectedProposalRevision || (state.proposal != nil && proposal.ID != state.proposal.ID) {
				return errors.New("proposed plan revision is invalid")
			}
		} else if proposal.Revision != 1 {
			return errors.New("initial proposed plan revision is invalid")
		}
		payload, err := publicPlanPayload(proposal)
		if err != nil {
			return err
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{Type: RunEventTypePlanProposed, Payload: payload}); err != nil {
			return err
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{
			Type: RunEventTypeRunWaitingAuthor, Payload: map[string]any{"reason": "plan_proposed", "planId": proposal.ID, "revision": proposal.Revision},
		}); err != nil {
			return err
		}
		state.proposal = &proposal
		state.expectedProposalRevision = 0
		return nil
	default:
		return errors.New("plan block kind is invalid")
	}
}

func publicPlanPayload(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("plan payload is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("plan payload is invalid")
	}
	delete(payload, "schemaVersion")
	// The durable event spine deliberately rejects generic `prompt` keys. Plan
	// questions expose the same author-facing value under a projection-safe key;
	// main reconstructs the InterviewQuestion DTO for the Renderer.
	if questions, ok := payload["questions"].([]any); ok {
		for _, item := range questions {
			question, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("plan payload is invalid")
			}
			question["questionText"] = question["prompt"]
			delete(question, "prompt")
		}
	}
	return payload, nil
}

func planModeSystemInstruction() string {
	return "Stay in Plan Mode and return exactly one supplied tool call with no prose. Use read-only story tools when facts are needed. Otherwise call plan_questions with every required schema field and unique question ids while critical uncertainty remains; when resolved, call proposed_plan with every required schema field. The first question round and plan revision are 1, then increment them after each author answer or modification request. Never imply plan, execution, or write approval; all proposed approvals must be false."
}
