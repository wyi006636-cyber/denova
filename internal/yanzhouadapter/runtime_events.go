package yanzhouadapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"denova/internal/yanzhouprotocol"
)

const (
	runtimeEventSchemaVersion    = "1"
	runtimeEventLedgerFilename   = "events.jsonl"
	runtimeEventMaxReplayLimit   = 100
	runtimeEventMaxPayloadBytes  = 64 * 1024
	runtimeEventMaxRunIDBytes    = 128
	runtimeEventMaxRecordBytes   = runtimeEventMaxPayloadBytes + 8*1024
	runtimeEventMaxLedgerBytes   = 16 * 1024 * 1024
	runtimeEventMaxLedgerRecords = 100_000
)

// RunEventType is the closed public event set emitted to Yanzhou.
type RunEventType string

const (
	RunEventTypeRunStarted          RunEventType = "run.started"
	RunEventTypeContextAccepted     RunEventType = "context.accepted"
	RunEventTypePlanQuestions       RunEventType = "plan.questions"
	RunEventTypePlanProposed        RunEventType = "plan.proposed"
	RunEventTypePlanApproved        RunEventType = "plan.approved"
	RunEventTypeSkillLoadRequested  RunEventType = "skill.load.requested"
	RunEventTypeSkillLoaded         RunEventType = "skill.loaded"
	RunEventTypeDelegationStarted   RunEventType = "delegation.started"
	RunEventTypeDelegationCompleted RunEventType = "delegation.completed"
	RunEventTypeModelDelta          RunEventType = "model.delta"
	RunEventTypeModelReasoningDelta RunEventType = "model.reasoning.delta"
	RunEventTypeToolRequested       RunEventType = "tool.requested"
	RunEventTypeToolStarted         RunEventType = "tool.started"
	RunEventTypeToolCompleted       RunEventType = "tool.completed"
	RunEventTypeArtifactCreated     RunEventType = "artifact.created"
	RunEventTypeCheckCompleted      RunEventType = "check.completed"
	RunEventTypeReviewCompleted     RunEventType = "review.completed"
	RunEventTypeRevisionRequested   RunEventType = "revision.requested"
	RunEventTypeProposalReady       RunEventType = "proposal.ready"
	RunEventTypeRunInterrupted      RunEventType = "run.interrupted"
	RunEventTypeRunWaitingAuthor    RunEventType = "run.waiting_author"
	RunEventTypeRunBudgetExhausted  RunEventType = "run.budget_exhausted"
	RunEventTypeRunCompleted        RunEventType = "run.completed"
	RunEventTypeRunFailed           RunEventType = "run.failed"
	RunEventTypeRunAborted          RunEventType = "run.aborted"
)

var (
	runtimeEventTypes = []RunEventType{
		RunEventTypeRunStarted,
		RunEventTypeContextAccepted,
		RunEventTypePlanQuestions,
		RunEventTypePlanProposed,
		RunEventTypePlanApproved,
		RunEventTypeSkillLoadRequested,
		RunEventTypeSkillLoaded,
		RunEventTypeDelegationStarted,
		RunEventTypeDelegationCompleted,
		RunEventTypeModelDelta,
		RunEventTypeModelReasoningDelta,
		RunEventTypeToolRequested,
		RunEventTypeToolStarted,
		RunEventTypeToolCompleted,
		RunEventTypeArtifactCreated,
		RunEventTypeCheckCompleted,
		RunEventTypeReviewCompleted,
		RunEventTypeRevisionRequested,
		RunEventTypeProposalReady,
		RunEventTypeRunInterrupted,
		RunEventTypeRunWaitingAuthor,
		RunEventTypeRunBudgetExhausted,
		RunEventTypeRunCompleted,
		RunEventTypeRunFailed,
		RunEventTypeRunAborted,
	}
	runtimeTerminalEventTypes = []RunEventType{
		RunEventTypeRunInterrupted,
		RunEventTypeRunBudgetExhausted,
		RunEventTypeRunCompleted,
		RunEventTypeRunFailed,
		RunEventTypeRunAborted,
	}
	runtimeEventTypeSet      = makeRuntimeEventTypeSet(runtimeEventTypes)
	runtimeTerminalTypeSet   = makeRuntimeEventTypeSet(runtimeTerminalEventTypes)
	runtimeEventRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

type RuntimeEventInput struct {
	Type    RunEventType
	Payload map[string]any
}

// RunEvent is both the public wire payload and the durable JSONL record.
type RunEvent struct {
	SchemaVersion string         `json:"schemaVersion"`
	RunID         string         `json:"runId"`
	Seq           uint64         `json:"seq"`
	Timestamp     string         `json:"timestamp"`
	Type          RunEventType   `json:"type"`
	Payload       map[string]any `json:"payload"`
}

type RuntimeEventStore interface {
	Append(context.Context, string, RuntimeEventInput) (RunEvent, error)
	ReplayAfter(context.Context, string, uint64, int) ([]RunEvent, error)
	Close() error
}

type runtimeEventRunState struct {
	lastSeq  uint64
	terminal bool
}

type fileRuntimeEventStore struct {
	mu     sync.Mutex
	root   string
	closed bool
}

func RunEventTypes() []RunEventType {
	return append([]RunEventType(nil), runtimeEventTypes...)
}

func TerminalRunEventTypes() []RunEventType {
	return append([]RunEventType(nil), runtimeTerminalEventTypes...)
}

func IsTerminalRunEventType(eventType RunEventType) bool {
	_, ok := runtimeTerminalTypeSet[eventType]
	return ok
}

// NewFileRuntimeEventStore keeps sidecar events under the isolated runtime
// directory supplied by Yanzhou. It does not inspect or mutate book files.
func NewFileRuntimeEventStore(runtimeRoot string) (RuntimeEventStore, error) {
	if runtimeRoot == "" || runtimeRoot != strings.TrimSpace(runtimeRoot) || !filepath.IsAbs(runtimeRoot) {
		return nil, errors.New("runtime root must be a non-empty absolute path")
	}
	clean := filepath.Clean(runtimeRoot)
	if clean != runtimeRoot || filepath.Dir(clean) == clean {
		return nil, errors.New("runtime root must be clean and cannot be the filesystem root")
	}
	if err := os.MkdirAll(filepath.Join(clean, "runs"), 0o700); err != nil {
		return nil, fmt.Errorf("prepare runtime event directory: %w", err)
	}
	return &fileRuntimeEventStore{root: clean}, nil
}

// EmitRunEvent appends an event before exposing the matching run.event frame.
func EmitRunEvent(ctx context.Context, store RuntimeEventStore, output io.Writer, runID string, input RuntimeEventInput) (RunEvent, error) {
	if store == nil || output == nil {
		return RunEvent{}, errors.New("runtime event store and output are required")
	}
	event, err := store.Append(ctx, runID, input)
	if err != nil {
		return RunEvent{}, err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return RunEvent{}, fmt.Errorf("encode run event: %w", err)
	}
	frame := yanzhouprotocol.Envelope{
		Kind:            yanzhouprotocol.KindRunEvent,
		ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RunID:           event.RunID,
		Seq:             event.Seq,
		Payload:         payload,
	}
	if err := yanzhouprotocol.WriteFrame(output, frame); err != nil {
		return event, fmt.Errorf("write run event frame: %w", err)
	}
	return event, nil
}

func (s *fileRuntimeEventStore) Append(ctx context.Context, runID string, input RuntimeEventInput) (RunEvent, error) {
	if err := validateRuntimeEventContext(ctx); err != nil {
		return RunEvent{}, err
	}
	if err := validateRuntimeEventRunID(runID); err != nil {
		return RunEvent{}, err
	}
	if _, ok := runtimeEventTypeSet[input.Type]; !ok {
		return RunEvent{}, fmt.Errorf("unknown RunEvent type %q", input.Type)
	}
	payload, err := normalizeRuntimeEventPayload(input.Type, input.Payload)
	if err != nil {
		return RunEvent{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return RunEvent{}, errors.New("runtime event store is closed")
	}
	state, _, err := s.scanRunLocked(ctx, runID, 0, 0)
	if err != nil {
		return RunEvent{}, err
	}
	if state.terminal {
		return RunEvent{}, fmt.Errorf("run %q already has a terminal event", runID)
	}
	if state.lastSeq >= runtimeEventMaxLedgerRecords {
		return RunEvent{}, fmt.Errorf("runtime event ledger reached %d records", runtimeEventMaxLedgerRecords)
	}

	event := RunEvent{
		SchemaVersion: runtimeEventSchemaVersion,
		RunID:         runID,
		Seq:           state.lastSeq + 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		Type:          input.Type,
		Payload:       payload,
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return RunEvent{}, fmt.Errorf("encode runtime event: %w", err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > runtimeEventMaxRecordBytes {
		return RunEvent{}, fmt.Errorf("runtime event record exceeds %d bytes", runtimeEventMaxRecordBytes)
	}
	runDirectory := filepath.Join(s.root, "runs", runID)
	if err := os.MkdirAll(runDirectory, 0o700); err != nil {
		return RunEvent{}, fmt.Errorf("prepare runtime run directory: %w", err)
	}
	ledgerPath := filepath.Join(runDirectory, runtimeEventLedgerFilename)
	info, err := os.Stat(ledgerPath)
	if err == nil && info.Size() > runtimeEventMaxLedgerBytes-int64(len(encoded)) {
		return RunEvent{}, fmt.Errorf("runtime event ledger would exceed %d bytes", runtimeEventMaxLedgerBytes)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return RunEvent{}, fmt.Errorf("inspect runtime event ledger: %w", err)
	}
	file, err := os.OpenFile(ledgerPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return RunEvent{}, fmt.Errorf("open runtime event ledger: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return RunEvent{}, fmt.Errorf("append runtime event: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return RunEvent{}, fmt.Errorf("sync runtime event: %w", err)
	}
	if err := file.Close(); err != nil {
		return RunEvent{}, fmt.Errorf("close runtime event ledger: %w", err)
	}
	return event, nil
}

func (s *fileRuntimeEventStore) ReplayAfter(ctx context.Context, runID string, afterSeq uint64, limit int) ([]RunEvent, error) {
	if err := validateRuntimeEventContext(ctx); err != nil {
		return nil, err
	}
	if err := validateRuntimeEventRunID(runID); err != nil {
		return nil, err
	}
	if limit < 1 || limit > runtimeEventMaxReplayLimit {
		return nil, fmt.Errorf("replay limit must be between 1 and %d", runtimeEventMaxReplayLimit)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("runtime event store is closed")
	}
	_, page, err := s.scanRunLocked(ctx, runID, afterSeq, limit)
	return page, err
}

func (s *fileRuntimeEventStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fileRuntimeEventStore) scanRunLocked(ctx context.Context, runID string, afterSeq uint64, limit int) (runtimeEventRunState, []RunEvent, error) {
	ledgerPath := filepath.Join(s.root, "runs", runID, runtimeEventLedgerFilename)
	file, err := os.Open(ledgerPath)
	if errors.Is(err, os.ErrNotExist) {
		return runtimeEventRunState{}, []RunEvent{}, nil
	}
	if err != nil {
		return runtimeEventRunState{}, nil, fmt.Errorf("open runtime event ledger: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return runtimeEventRunState{}, nil, fmt.Errorf("stat runtime event ledger: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > runtimeEventMaxLedgerBytes {
		return runtimeEventRunState{}, nil, errors.New("runtime event ledger is invalid or too large")
	}

	reader := bufio.NewReaderSize(file, runtimeEventMaxRecordBytes+1)
	state := runtimeEventRunState{}
	page := make([]RunEvent, 0, limit)
	for lineNumber := 1; ; lineNumber++ {
		if err := validateRuntimeEventContext(ctx); err != nil {
			return runtimeEventRunState{}, nil, err
		}
		line, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			if len(line) == 0 {
				break
			}
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event ledger has a truncated final record")
		}
		if readErr != nil {
			return runtimeEventRunState{}, nil, fmt.Errorf("read runtime event ledger: %w", readErr)
		}
		if len(line) > runtimeEventMaxRecordBytes {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event record exceeds %d bytes", runtimeEventMaxRecordBytes)
		}
		if state.lastSeq >= runtimeEventMaxLedgerRecords {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event ledger exceeds %d records", runtimeEventMaxLedgerRecords)
		}
		line = bytes.TrimSuffix(line, []byte{'\n'})
		event, err := decodeRuntimeEvent(line)
		if err != nil {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event ledger line %d: %w", lineNumber, err)
		}
		if event.RunID != runID || event.Seq != state.lastSeq+1 {
			return runtimeEventRunState{}, nil, errors.New("runtime event ledger identity or sequence is invalid")
		}
		if state.terminal {
			return runtimeEventRunState{}, nil, errors.New("runtime event found after terminal")
		}
		state.lastSeq = event.Seq
		state.terminal = IsTerminalRunEventType(event.Type)
		if limit > 0 && event.Seq > afterSeq && len(page) < limit {
			page = append(page, event)
		}
	}
	return state, page, nil
}

func decodeRuntimeEvent(line []byte) (RunEvent, error) {
	if len(bytes.TrimSpace(line)) == 0 {
		return RunEvent{}, errors.New("runtime event record is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var event RunEvent
	if err := decoder.Decode(&event); err != nil {
		return RunEvent{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return RunEvent{}, errors.New("runtime event record must contain one JSON object")
	}
	if event.SchemaVersion != runtimeEventSchemaVersion || event.Seq == 0 {
		return RunEvent{}, errors.New("runtime event schema or sequence is invalid")
	}
	if err := validateRuntimeEventRunID(event.RunID); err != nil {
		return RunEvent{}, err
	}
	if _, ok := runtimeEventTypeSet[event.Type]; !ok {
		return RunEvent{}, fmt.Errorf("unknown RunEvent type %q", event.Type)
	}
	if _, err := time.Parse(time.RFC3339Nano, event.Timestamp); err != nil {
		return RunEvent{}, errors.New("runtime event timestamp is invalid")
	}
	payload, err := normalizeRuntimeEventPayload(event.Type, event.Payload)
	if err != nil {
		return RunEvent{}, err
	}
	event.Payload = payload
	return event, nil
}

func normalizeRuntimeEventPayload(eventType RunEventType, payload map[string]any) (map[string]any, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) > runtimeEventMaxPayloadBytes {
		return nil, fmt.Errorf("runtime event payload is invalid or exceeds %d bytes", runtimeEventMaxPayloadBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized map[string]any
	if err := decoder.Decode(&normalized); err != nil || normalized == nil {
		return nil, errors.New("runtime event payload must be a JSON object")
	}
	if IsTerminalRunEventType(eventType) {
		if err := validateRuntimeTerminalPayload(eventType, normalized); err != nil {
			return nil, err
		}
	}
	return normalized, nil
}

func invalidRuntimeTerminalPayload() error {
	return errors.New("terminal runtime event payload is invalid")
}

func runtimeTerminalPayloadRefs(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok || len(items) > 32 {
		return nil, false
	}
	refs := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		ref, ok := item.(string)
		if !ok || !validResumeIdentityValue(ref) {
			return nil, false
		}
		if _, duplicate := seen[ref]; duplicate {
			return nil, false
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	return refs, true
}

func sameRuntimeTerminalRefs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func terminationCauseForReason(reason string) (TerminationCause, bool) {
	switch reason {
	case "provider_idle_timeout":
		return TerminationCauseProviderIdleTimeout, true
	case "cancelled":
		return TerminationCauseUserCancelled, true
	case "provider_error":
		return TerminationCauseProviderError, true
	case "tool_error":
		return TerminationCauseToolError, true
	case "panic":
		return TerminationCausePanic, true
	case "budget_exhausted":
		return TerminationCauseBudgetExhausted, true
	case "run_wall_timeout":
		return TerminationCauseRunWallTimeout, true
	default:
		return "", false
	}
}

func validateRuntimeTerminalPayload(eventType RunEventType, payload map[string]any) error {
	allowedFields := map[string]struct{}{
		"schemaVersion": {}, "reason": {}, "resumable": {},
		"partialArtifactRefs": {}, "checkpointId": {}, "timeoutType": {},
	}
	for field := range payload {
		if _, allowed := allowedFields[field]; !allowed {
			return invalidRuntimeTerminalPayload()
		}
	}
	if len(payload) < 4 || payload["schemaVersion"] != runtimeEventSchemaVersion {
		return invalidRuntimeTerminalPayload()
	}
	reason, reasonOK := payload["reason"].(string)
	resumable, resumableOK := payload["resumable"].(bool)
	refs, refsOK := runtimeTerminalPayloadRefs(payload["partialArtifactRefs"])
	checkpointValue, checkpointPresent := payload["checkpointId"]
	checkpointID, checkpointIsString := checkpointValue.(string)
	if checkpointPresent && (!checkpointIsString || !validResumeIdentityValue(checkpointID)) {
		return invalidRuntimeTerminalPayload()
	}
	timeoutValue, timeoutPresent := payload["timeoutType"]
	timeoutType, timeoutIsString := timeoutValue.(string)
	if timeoutPresent && (!timeoutIsString || !validRuntimeTimeoutType(RuntimeTimeoutType(timeoutType))) {
		return invalidRuntimeTerminalPayload()
	}
	if !reasonOK || !resumableOK || !refsOK {
		return invalidRuntimeTerminalPayload()
	}
	if reason == "completed" {
		if eventType != RunEventTypeRunCompleted || resumable || timeoutPresent {
			return invalidRuntimeTerminalPayload()
		}
		return nil
	}
	cause, ok := terminationCauseForReason(reason)
	if !ok {
		return invalidRuntimeTerminalPayload()
	}
	decision, err := ClassifyTermination(TerminationInput{
		Cause: cause, Resumable: &resumable, PartialArtifactRefs: refs,
		CheckpointID: checkpointID, TimeoutType: RuntimeTimeoutType(timeoutType),
	})
	if err != nil || decision.EventType != eventType || decision.Reason != reason ||
		decision.Resumable != resumable || decision.CheckpointID != checkpointID ||
		string(decision.TimeoutType) != timeoutType || !sameRuntimeTerminalRefs(decision.PartialArtifactRefs, refs) {
		return invalidRuntimeTerminalPayload()
	}
	return nil
}

func validateRuntimeEventRunID(runID string) error {
	if len(runID) == 0 || len(runID) > runtimeEventMaxRunIDBytes || !runtimeEventRunIDPattern.MatchString(runID) || runID == "." || runID == ".." {
		return errors.New("runId is invalid")
	}
	return nil
}

func validateRuntimeEventContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("runtime event context is required")
	}
	return ctx.Err()
}

func makeRuntimeEventTypeSet(types []RunEventType) map[RunEventType]struct{} {
	set := make(map[RunEventType]struct{}, len(types))
	for _, eventType := range types {
		set[eventType] = struct{}{}
	}
	return set
}
