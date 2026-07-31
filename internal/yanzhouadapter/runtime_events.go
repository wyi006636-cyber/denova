package yanzhouadapter

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"denova/internal/yanzhouprotocol"
)

const (
	runtimeEventSchemaVersion    = "1"
	runtimeEventLedgerFilename   = "events.jsonl"
	runtimeEventMaxReplayLimit   = 100
	runtimeEventMaxPayloadBytes  = 64 * 1024
	runtimeEventMaxStringBytes   = 8 * 1024
	runtimeEventMaxKeyBytes      = 256
	runtimeEventMaxDepth         = 32
	runtimeEventMaxRunIDBytes    = 128
	runtimeEventMaxRecordBytes   = runtimeEventMaxPayloadBytes + 16*1024
	runtimeEventMaxLedgerBytes   = 16 * 1024 * 1024
	runtimeEventMaxLedgerRecords = 100_000
)

// RunEventType is a closed public event type from the Yanzhou protocol.
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
	runtimeEventTypeSet       = makeRuntimeEventTypeSet(runtimeEventTypes)
	runtimeTerminalTypeSet    = makeRuntimeEventTypeSet(runtimeTerminalEventTypes)
	runtimeEventRunIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	runtimeEventChecksumRegex = regexp.MustCompile(`^[a-f0-9]{64}$`)
	runtimeEventForbiddenKeys = map[string]struct{}{
		"apikey": {}, "runtimeauth": {}, "credential": {}, "credentials": {},
		"password": {}, "secret": {}, "token": {}, "accesstoken": {},
		"authorization": {}, "headers": {}, "providerheaders": {}, "baseurl": {},
		"rawrequest": {}, "rawresponse": {}, "rawtool": {}, "rawtoolrequest": {},
		"rawtoolresponse": {}, "stderr": {}, "prompt": {}, "bookpath": {},
		"bookroot": {}, "workspacepath": {}, "workspaceroot": {},
		"effectivemodelprofile": {}, "modelprofile": {}, "modelprofileid": {},
		"privateprofile": {},
		"schemaversion":  {}, "runid": {}, "seq": {}, "timestamp": {},
		"checksum": {}, "eventid": {}, "eventidentity": {},
	}
)

// RuntimeEventInput deliberately excludes sequence, identity, timestamp, and checksum.
// The durable store owns those fields.
type RuntimeEventInput struct {
	Type    RunEventType
	Payload map[string]any
}

// RunEvent is the public payload of a run.event frame. The checksum belongs only
// to the durable JSONL record and is deliberately absent from this wire DTO.
type RunEvent struct {
	SchemaVersion string         `json:"schemaVersion"`
	RunID         string         `json:"runId"`
	Seq           uint64         `json:"seq"`
	Timestamp     string         `json:"timestamp"`
	Type          RunEventType   `json:"type"`
	Payload       map[string]any `json:"payload"`
}

// RuntimeEventStore is the only state truth for this WP3 event spine.
type RuntimeEventStore interface {
	Append(context.Context, string, RuntimeEventInput) (RunEvent, error)
	ReplayAfter(context.Context, string, uint64, int) ([]RunEvent, error)
	Close() error
}

// RuntimeRecoveryReporter exposes only stable non-sensitive recovery issue
// codes. It does not expose ledger bytes, paths, or underlying errors.
type RuntimeRecoveryReporter interface {
	RecoveryIssueCodes() []string
}

type runtimeEventRecord struct {
	SchemaVersion string         `json:"schemaVersion"`
	RunID         string         `json:"runId"`
	Seq           uint64         `json:"seq"`
	Timestamp     string         `json:"timestamp"`
	Type          RunEventType   `json:"type"`
	Payload       map[string]any `json:"payload"`
	Checksum      string         `json:"checksum"`
}

type runtimeEventChecksumFields struct {
	SchemaVersion string         `json:"schemaVersion"`
	RunID         string         `json:"runId"`
	Seq           uint64         `json:"seq"`
	Timestamp     string         `json:"timestamp"`
	Type          RunEventType   `json:"type"`
	Payload       map[string]any `json:"payload"`
}

type runtimeEventRunState struct {
	lastSeq          uint64
	validBytes       int64
	terminal         bool
	hasTruncatedTail bool
}

type runtimeEventFile interface {
	io.Reader
	io.Writer
	Stat() (os.FileInfo, error)
	Truncate(int64) error
	Sync() error
	Close() error
}

// runtimeEventFileOps is an internal durability seam. Production uses os.Root-
// anchored operations; tests inject concrete write/sync/close failures.
type runtimeEventFileOps interface {
	mkdir(*os.Root, string, os.FileMode) (bool, error)
	openAppend(*os.Root, string, os.FileMode) (runtimeEventFile, bool, error)
	openRepair(*os.Root, string) (runtimeEventFile, error)
	openRead(*os.Root, string) (runtimeEventFile, error)
	syncDirectory(*os.Root, string) error
}

type osRuntimeEventFileOps struct{}

type fileRuntimeEventStore struct {
	mu             sync.Mutex
	root           *os.Root
	ops            runtimeEventFileOps
	closed         bool
	closeErr       error
	quarantined    map[string]error
	recoveryIssues map[string]struct{}
	lastCloseError error
}

// RunEventTypes returns a copy of the exact public type list in protocol order.
func RunEventTypes() []RunEventType {
	return append([]RunEventType(nil), runtimeEventTypes...)
}

// TerminalRunEventTypes returns a copy of the exact terminal type list.
func TerminalRunEventTypes() []RunEventType {
	return append([]RunEventType(nil), runtimeTerminalEventTypes...)
}

// IsTerminalRunEventType reports membership in the closed terminal type set.
func IsTerminalRunEventType(eventType RunEventType) bool {
	_, ok := runtimeTerminalTypeSet[eventType]
	return ok
}

// RecoveryIssueCodes returns a stable copy of recovery conditions observed by
// this store. The issue remains available after a tail has been repaired.
func (s *fileRuntimeEventStore) RecoveryIssueCodes() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	codes := make([]string, 0, len(s.recoveryIssues))
	for code := range s.recoveryIssues {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// NewFileRuntimeEventStore creates an append-only store beneath one isolated runtime root.
func NewFileRuntimeEventStore(runtimeRoot string) (RuntimeEventStore, error) {
	return newFileRuntimeEventStoreWithOps(runtimeRoot, newOSRuntimeEventFileOps())
}

func newOSRuntimeEventFileOps() runtimeEventFileOps {
	return osRuntimeEventFileOps{}
}

func newFileRuntimeEventStoreWithOps(runtimeRoot string, ops runtimeEventFileOps) (RuntimeEventStore, error) {
	if ops == nil {
		return nil, fmt.Errorf("runtime event file operations are required")
	}
	if runtimeRoot == "" || runtimeRoot != strings.TrimSpace(runtimeRoot) || !filepath.IsAbs(runtimeRoot) {
		return nil, fmt.Errorf("runtime root must be a non-empty absolute isolated path")
	}
	clean := filepath.Clean(runtimeRoot)
	if clean != runtimeRoot {
		return nil, fmt.Errorf("runtime root must be clean and cannot contain traversal")
	}
	if filepath.Dir(clean) == clean {
		return nil, fmt.Errorf("runtime root cannot be the filesystem root")
	}
	root, err := openDurableRuntimeRoot(clean, ops)
	if err != nil {
		return nil, err
	}
	store := &fileRuntimeEventStore{
		root:           root,
		ops:            ops,
		quarantined:    make(map[string]error),
		recoveryIssues: make(map[string]struct{}),
	}
	created, err := ops.mkdir(root, "runs", 0o700)
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("prepare runtime runs directory: %w", err)
	}
	if created {
		if err := ops.syncDirectory(root, "."); err != nil {
			_ = root.Close()
			return nil, fmt.Errorf("sync runtime root after runs create: %w", err)
		}
	}
	return store, nil
}

func openDurableRuntimeRoot(runtimeRoot string, ops runtimeEventFileOps) (*os.Root, error) {
	rootInfo, err := os.Lstat(runtimeRoot)
	if err == nil {
		if rootInfo.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("runtime root cannot be a symlink")
		}
		if !rootInfo.IsDir() {
			return nil, fmt.Errorf("runtime root is not a directory")
		}
		parentPath := filepath.Dir(runtimeRoot)
		if filepath.Dir(parentPath) == parentPath {
			return nil, fmt.Errorf("runtime root cannot require syncing the filesystem root")
		}
		parentRoot, err := openVerifiedRuntimeRoot(parentPath)
		if err != nil {
			return nil, fmt.Errorf("open runtime root parent: %w", err)
		}
		finalRoot, err := openVerifiedChildRoot(parentRoot, filepath.Base(runtimeRoot))
		if err != nil {
			_ = parentRoot.Close()
			return nil, fmt.Errorf("open existing runtime root: %w", err)
		}
		if err := ops.syncDirectory(parentRoot, "."); err != nil {
			_ = finalRoot.Close()
			_ = parentRoot.Close()
			return nil, fmt.Errorf("sync existing runtime root parent: %w", err)
		}
		_ = parentRoot.Close()
		return finalRoot, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect runtime root: %w", err)
	}

	missing := []string{filepath.Base(runtimeRoot)}
	ancestorPath := filepath.Dir(runtimeRoot)
	for {
		ancestorInfo, statErr := os.Lstat(ancestorPath)
		if statErr == nil {
			if ancestorInfo.Mode()&os.ModeSymlink != 0 || !ancestorInfo.IsDir() {
				return nil, fmt.Errorf("nearest runtime root ancestor is not a safe directory")
			}
			break
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect runtime root ancestor: %w", statErr)
		}
		parentPath := filepath.Dir(ancestorPath)
		if parentPath == ancestorPath {
			return nil, fmt.Errorf("runtime root has no non-root existing ancestor")
		}
		missing = append(missing, filepath.Base(ancestorPath))
		ancestorPath = parentPath
	}
	if filepath.Dir(ancestorPath) == ancestorPath {
		return nil, fmt.Errorf("runtime root creation cannot sync the filesystem root")
	}
	ancestorRoot, err := openVerifiedRuntimeRoot(ancestorPath)
	if err != nil {
		return nil, fmt.Errorf("open nearest runtime root ancestor: %w", err)
	}
	parentRelative := "."
	for index := len(missing) - 1; index >= 0; index-- {
		childRelative := missing[index]
		if parentRelative != "." {
			childRelative = filepath.Join(parentRelative, childRelative)
		}
		if _, err := ops.mkdir(ancestorRoot, childRelative, 0o700); err != nil {
			_ = ancestorRoot.Close()
			return nil, fmt.Errorf("create runtime root component %q: %w", childRelative, err)
		}
		if err := ops.syncDirectory(ancestorRoot, parentRelative); err != nil {
			_ = ancestorRoot.Close()
			return nil, fmt.Errorf("sync parent after runtime root component %q: %w", childRelative, err)
		}
		parentRelative = childRelative
	}
	finalRoot, err := openVerifiedChildRoot(ancestorRoot, parentRelative)
	if err != nil {
		_ = ancestorRoot.Close()
		return nil, fmt.Errorf("open created runtime root: %w", err)
	}
	_ = ancestorRoot.Close()
	return finalRoot, nil
}

func openVerifiedRuntimeRoot(path string) (*os.Root, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() {
		return nil, fmt.Errorf("path is not a non-symlink directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	rootDirectory, err := root.Open(".")
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	rootDirectoryInfo, statErr := rootDirectory.Stat()
	_ = rootDirectory.Close()
	if statErr != nil || !rootDirectoryInfo.IsDir() || !os.SameFile(pathInfo, rootDirectoryInfo) {
		_ = root.Close()
		return nil, fmt.Errorf("held directory identity mismatch")
	}
	return root, nil
}

func openVerifiedChildRoot(parentRoot *os.Root, childRelative string) (*os.Root, error) {
	childInfo, err := parentRoot.Lstat(childRelative)
	if err != nil {
		return nil, err
	}
	if childInfo.Mode()&os.ModeSymlink != 0 || !childInfo.IsDir() {
		return nil, fmt.Errorf("runtime root child is not a non-symlink directory")
	}
	childRoot, err := parentRoot.OpenRoot(childRelative)
	if err != nil {
		return nil, err
	}
	childDirectory, err := childRoot.Open(".")
	if err != nil {
		_ = childRoot.Close()
		return nil, err
	}
	heldInfo, statErr := childDirectory.Stat()
	_ = childDirectory.Close()
	if statErr != nil || !heldInfo.IsDir() || !os.SameFile(childInfo, heldInfo) {
		_ = childRoot.Close()
		return nil, fmt.Errorf("held runtime root child identity mismatch")
	}
	return childRoot, nil
}

// EmitRunEvent durably appends before emitting the existing run.event frame kind.
func EmitRunEvent(ctx context.Context, store RuntimeEventStore, output io.Writer, runID string, input RuntimeEventInput) (RunEvent, error) {
	if store == nil {
		return RunEvent{}, fmt.Errorf("runtime event store is required")
	}
	if output == nil {
		return RunEvent{}, fmt.Errorf("run event output is required")
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
		return event, fmt.Errorf("write durable run event frame: %w", err)
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
		return RunEvent{}, fmt.Errorf("runtime event store is closed")
	}
	if quarantineErr, quarantined := s.quarantined[runID]; quarantined {
		return RunEvent{}, fmt.Errorf("run %q is quarantined after an uncertain append: %w", runID, quarantineErr)
	}
	if err := validateRuntimeEventContext(ctx); err != nil {
		return RunEvent{}, err
	}
	state, err := s.loadRunStateLocked(ctx, runID)
	if err != nil {
		return RunEvent{}, err
	}
	if state.terminal {
		return RunEvent{}, fmt.Errorf("run %q already has a terminal event", runID)
	}
	if state.lastSeq == ^uint64(0) {
		return RunEvent{}, fmt.Errorf("run %q sequence is exhausted", runID)
	}
	if state.lastSeq >= runtimeEventMaxLedgerRecords {
		return RunEvent{}, fmt.Errorf("runtime event ledger reached %d records", runtimeEventMaxLedgerRecords)
	}
	if state.hasTruncatedTail {
		if err := s.repairTruncatedTailLocked(runID, state.validBytes); err != nil {
			s.quarantineLocked(runID, err)
			return RunEvent{}, fmt.Errorf("runtime event truncated-tail recovery failed")
		}
	}
	runDirectory := filepath.Join("runs", runID)
	createdRun, err := s.ops.mkdir(s.root, runDirectory, 0o700)
	if err != nil {
		return RunEvent{}, fmt.Errorf("prepare runtime run directory: %w", err)
	}
	if createdRun {
		if err := s.ops.syncDirectory(s.root, "runs"); err != nil {
			s.quarantineLocked(runID, err)
			return RunEvent{}, fmt.Errorf("sync runs directory after run create: %w", err)
		}
	}
	event := RunEvent{
		SchemaVersion: runtimeEventSchemaVersion,
		RunID:         runID,
		Seq:           state.lastSeq + 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		Type:          input.Type,
		Payload:       payload,
	}
	record, err := newRuntimeEventRecord(event)
	if err != nil {
		return RunEvent{}, err
	}
	uncertain, closeErr, err := s.appendRecordLocked(record)
	if closeErr != nil {
		s.lastCloseError = closeErr
	}
	if err != nil {
		if uncertain {
			s.quarantineLocked(runID, err)
		}
		return RunEvent{}, err
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
		return nil, fmt.Errorf("runtime event store is closed")
	}
	if quarantineErr, quarantined := s.quarantined[runID]; quarantined {
		return nil, fmt.Errorf("run %q is quarantined after an uncertain append: %w", runID, quarantineErr)
	}
	_, page, err := s.scanAndValidateRunLocked(ctx, runID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	return page, nil
}

func (s *fileRuntimeEventStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	s.closeErr = s.root.Close()
	s.quarantined = nil
	return s.closeErr
}

func (s *fileRuntimeEventStore) quarantineLocked(runID string, cause error) {
	if _, exists := s.quarantined[runID]; !exists {
		s.quarantined[runID] = cause
	}
}

func (s *fileRuntimeEventStore) recordRecoveryIssueLocked(code string) {
	if s.recoveryIssues == nil {
		s.recoveryIssues = make(map[string]struct{})
	}
	s.recoveryIssues[code] = struct{}{}
}

func (s *fileRuntimeEventStore) loadRunStateLocked(ctx context.Context, runID string) (runtimeEventRunState, error) {
	state, _, err := s.scanAndValidateRunLocked(ctx, runID, 0, 0)
	if err != nil {
		return runtimeEventRunState{}, err
	}
	return state, nil
}

func (s *fileRuntimeEventStore) appendRecordLocked(record runtimeEventRecord) (uncertain bool, closeErr error, err error) {
	encoded, err := json.Marshal(record)
	if err != nil {
		return false, nil, fmt.Errorf("encode runtime event record: %w", err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > runtimeEventMaxRecordBytes {
		return false, nil, fmt.Errorf("runtime event record exceeds %d bytes", runtimeEventMaxRecordBytes)
	}
	ledgerPath := filepath.Join("runs", record.RunID, runtimeEventLedgerFilename)
	file, created, err := s.ops.openAppend(s.root, ledgerPath, 0o600)
	if err != nil {
		return created, nil, fmt.Errorf("open runtime event ledger: %w", err)
	}
	info, statErr := file.Stat()
	if statErr != nil {
		closeErr := file.Close()
		return created, closeErr, fmt.Errorf("stat runtime event ledger before append: %w", statErr)
	}
	if info.Size() < 0 || info.Size() > runtimeEventMaxLedgerBytes-int64(len(encoded)) {
		closeErr := file.Close()
		return created, closeErr, fmt.Errorf("runtime event ledger would exceed %d bytes", runtimeEventMaxLedgerBytes)
	}
	written, writeErr := file.Write(encoded)
	if writeErr == nil && written != len(encoded) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		closeErr := file.Close()
		return true, closeErr, fmt.Errorf("append runtime event: %w", writeErr)
	}
	if err := file.Sync(); err != nil {
		closeErr := file.Close()
		return true, closeErr, fmt.Errorf("sync runtime event: %w", err)
	}
	closeErr = file.Close()
	if created {
		runDirectory := filepath.Join("runs", record.RunID)
		if err := s.ops.syncDirectory(s.root, runDirectory); err != nil {
			return true, closeErr, fmt.Errorf("sync run directory after ledger create: %w", err)
		}
	}
	// Once file fsync (and, for first create, directory fsync) succeeds, the
	// event is committed. A later close error cannot turn it into a ghost event.
	return false, closeErr, nil
}

func (s *fileRuntimeEventStore) repairTruncatedTailLocked(runID string, validBytes int64) error {
	if validBytes < 0 || validBytes > runtimeEventMaxLedgerBytes {
		return fmt.Errorf("runtime event truncated-tail offset is invalid")
	}
	ledgerPath := filepath.Join("runs", runID, runtimeEventLedgerFilename)
	file, err := s.ops.openRepair(s.root, ledgerPath)
	if err != nil {
		return fmt.Errorf("open runtime event ledger for recovery: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat runtime event ledger for recovery: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= validBytes || info.Size() > runtimeEventMaxLedgerBytes {
		_ = file.Close()
		return fmt.Errorf("runtime event truncated-tail recovery state is invalid")
	}
	if err := file.Truncate(validBytes); err != nil {
		_ = file.Close()
		return fmt.Errorf("truncate runtime event tail: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync truncated runtime event ledger: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close truncated runtime event ledger: %w", err)
	}
	return nil
}

func (s *fileRuntimeEventStore) scanAndValidateRunLocked(ctx context.Context, runID string, afterSeq uint64, limit int) (runtimeEventRunState, []RunEvent, error) {
	ledgerPath := filepath.Join("runs", runID, runtimeEventLedgerFilename)
	file, err := s.ops.openRead(s.root, ledgerPath)
	if errors.Is(err, os.ErrNotExist) {
		return runtimeEventRunState{}, []RunEvent{}, nil
	}
	if err != nil {
		return runtimeEventRunState{}, nil, fmt.Errorf("open runtime event ledger for replay: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return runtimeEventRunState{}, nil, fmt.Errorf("stat runtime event ledger: %w", err)
	}
	if !info.Mode().IsRegular() {
		return runtimeEventRunState{}, nil, fmt.Errorf("runtime event ledger is not a regular file")
	}
	if info.Size() > runtimeEventMaxLedgerBytes {
		return runtimeEventRunState{}, nil, fmt.Errorf("runtime event ledger exceeds %d bytes", runtimeEventMaxLedgerBytes)
	}
	reader := bufio.NewReaderSize(file, runtimeEventMaxRecordBytes+1)
	page := make([]RunEvent, 0, limit)
	state := runtimeEventRunState{}
	var recordCount uint64
	var totalBytes int64
	for lineNumber := 1; ; lineNumber++ {
		if err := validateRuntimeEventContext(ctx); err != nil {
			return runtimeEventRunState{}, nil, err
		}
		line, readErr := reader.ReadSlice('\n')
		totalBytes += int64(len(line))
		if totalBytes > runtimeEventMaxLedgerBytes {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event ledger exceeds %d bytes", runtimeEventMaxLedgerBytes)
		}
		if errors.Is(readErr, bufio.ErrBufferFull) || len(line) > runtimeEventMaxRecordBytes {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event record exceeds %d bytes at line %d", runtimeEventMaxRecordBytes, lineNumber)
		}
		if errors.Is(readErr, io.EOF) {
			if len(line) == 0 {
				break
			}
			state.hasTruncatedTail = true
			s.recordRecoveryIssueLocked("truncated_tail")
			break
		}
		if readErr != nil {
			return runtimeEventRunState{}, nil, fmt.Errorf("read runtime event ledger: %w", readErr)
		}
		if recordCount >= runtimeEventMaxLedgerRecords {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event ledger exceeds %d validated records", runtimeEventMaxLedgerRecords)
		}
		line = line[:len(line)-1]
		if len(bytes.TrimSpace(line)) == 0 {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event ledger has a blank record at line %d", lineNumber)
		}
		record, err := decodeRuntimeEventRecord(line)
		if err != nil {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event ledger line %d: %w", lineNumber, err)
		}
		if record.RunID != runID {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event ledger runId mismatch at seq %d", record.Seq)
		}
		expectedSeq := recordCount + 1
		if record.Seq != expectedSeq {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event seq gap: expected %d, received %d", expectedSeq, record.Seq)
		}
		if state.terminal {
			return runtimeEventRunState{}, nil, fmt.Errorf("runtime event found after terminal at seq %d", record.Seq)
		}
		recordCount++
		state.lastSeq = record.Seq
		state.validBytes = totalBytes
		if IsTerminalRunEventType(record.Type) {
			state.terminal = true
		}
		if limit > 0 && record.Seq > afterSeq && len(page) < limit {
			page = append(page, record.publicEvent())
		}
	}
	return state, page, nil
}

func (osRuntimeEventFileOps) mkdir(root *os.Root, name string, perm os.FileMode) (bool, error) {
	created := false
	if err := root.Mkdir(name, perm); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		return false, err
	}
	directory, err := root.Open(name)
	if err != nil {
		return created, err
	}
	if err := validateOpenedRuntimePath(root, name, directory, true); err != nil {
		_ = directory.Close()
		return created, err
	}
	_ = directory.Close()
	return created, nil
}

func (osRuntimeEventFileOps) openAppend(root *os.Root, name string, perm os.FileMode) (runtimeEventFile, bool, error) {
	created := false
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL, perm)
	if err == nil {
		created = true
	} else if errors.Is(err, os.ErrExist) {
		file, err = root.OpenFile(name, os.O_WRONLY|os.O_APPEND, perm)
	}
	if err != nil {
		return nil, created, err
	}
	if err := validateOpenedRuntimePath(root, name, file, false); err != nil {
		_ = file.Close()
		return nil, created, err
	}
	return file, created, nil
}

func (osRuntimeEventFileOps) openRepair(root *os.Root, name string) (runtimeEventFile, error) {
	file, err := root.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	if err := validateOpenedRuntimePath(root, name, file, false); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (osRuntimeEventFileOps) openRead(root *os.Root, name string) (runtimeEventFile, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	if err := validateOpenedRuntimePath(root, name, file, false); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (osRuntimeEventFileOps) syncDirectory(root *os.Root, name string) error {
	directory, err := root.Open(name)
	if err != nil {
		return err
	}
	if err := validateOpenedRuntimePath(root, name, directory, true); err != nil {
		_ = directory.Close()
		return err
	}
	if runtime.GOOS == "windows" {
		_ = directory.Close()
		return nil
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	// Directory fsync is the commit boundary. A subsequent close error cannot
	// make the already-synced directory entry uncertain.
	_ = directory.Close()
	return nil
}

func validateOpenedRuntimePath(root *os.Root, name string, file *os.File, wantDirectory bool) error {
	openedInfo, err := file.Stat()
	if err != nil {
		return err
	}
	pathInfo, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("runtime path %q is a symlink", name)
	}
	if wantDirectory {
		if !openedInfo.IsDir() || !pathInfo.IsDir() {
			return fmt.Errorf("runtime path %q is not a directory", name)
		}
	} else if !openedInfo.Mode().IsRegular() || !pathInfo.Mode().IsRegular() {
		return fmt.Errorf("runtime path %q is not a regular file", name)
	}
	if !os.SameFile(openedInfo, pathInfo) {
		return fmt.Errorf("runtime path %q changed while opening", name)
	}
	return nil
}

func newRuntimeEventRecord(event RunEvent) (runtimeEventRecord, error) {
	record := runtimeEventRecord{
		SchemaVersion: event.SchemaVersion,
		RunID:         event.RunID,
		Seq:           event.Seq,
		Timestamp:     event.Timestamp,
		Type:          event.Type,
		Payload:       event.Payload,
	}
	checksum, err := runtimeEventChecksum(record)
	if err != nil {
		return runtimeEventRecord{}, err
	}
	record.Checksum = checksum
	return record, nil
}

func (record runtimeEventRecord) publicEvent() RunEvent {
	return RunEvent{
		SchemaVersion: record.SchemaVersion,
		RunID:         record.RunID,
		Seq:           record.Seq,
		Timestamp:     record.Timestamp,
		Type:          record.Type,
		Payload:       record.Payload,
	}
}

func decodeRuntimeEventRecord(line []byte) (runtimeEventRecord, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var record runtimeEventRecord
	if err := decoder.Decode(&record); err != nil {
		return runtimeEventRecord{}, fmt.Errorf("invalid JSON record: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return runtimeEventRecord{}, fmt.Errorf("record must contain exactly one JSON value")
	}
	if record.SchemaVersion != runtimeEventSchemaVersion {
		return runtimeEventRecord{}, fmt.Errorf("unsupported runtime event schemaVersion %q", record.SchemaVersion)
	}
	if err := validateRuntimeEventRunID(record.RunID); err != nil {
		return runtimeEventRecord{}, err
	}
	if record.Seq == 0 {
		return runtimeEventRecord{}, fmt.Errorf("runtime event seq must be positive")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.Timestamp); err != nil {
		return runtimeEventRecord{}, fmt.Errorf("runtime event timestamp is invalid: %w", err)
	}
	if _, ok := runtimeEventTypeSet[record.Type]; !ok {
		return runtimeEventRecord{}, fmt.Errorf("unknown RunEvent type %q", record.Type)
	}
	if record.Payload == nil {
		return runtimeEventRecord{}, fmt.Errorf("runtime event payload must be an object")
	}
	if !runtimeEventChecksumRegex.MatchString(record.Checksum) {
		return runtimeEventRecord{}, fmt.Errorf("runtime event checksum format is invalid")
	}
	wantChecksum, err := runtimeEventChecksum(record)
	if err != nil {
		return runtimeEventRecord{}, err
	}
	if record.Checksum != wantChecksum {
		return runtimeEventRecord{}, fmt.Errorf("runtime event checksum mismatch")
	}
	normalized, err := normalizeRuntimeEventPayload(record.Type, record.Payload)
	if err != nil {
		return runtimeEventRecord{}, err
	}
	record.Payload = normalized
	return record, nil
}

func runtimeEventChecksum(record runtimeEventRecord) (string, error) {
	fields := runtimeEventChecksumFields{
		SchemaVersion: record.SchemaVersion,
		RunID:         record.RunID,
		Seq:           record.Seq,
		Timestamp:     record.Timestamp,
		Type:          record.Type,
		Payload:       record.Payload,
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return "", fmt.Errorf("encode runtime event checksum fields: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeRuntimeEventPayload(eventType RunEventType, payload map[string]any) (map[string]any, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("runtime event payload must be JSON-safe: %w", err)
	}
	if len(encoded) > runtimeEventMaxPayloadBytes {
		return nil, fmt.Errorf("runtime event payload exceeds %d bytes", runtimeEventMaxPayloadBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized map[string]any
	if err := decoder.Decode(&normalized); err != nil || normalized == nil {
		return nil, fmt.Errorf("runtime event payload must be a JSON object")
	}
	if _, terminal := runtimeTerminalTypeSet[eventType]; terminal {
		if err := validateRuntimeTerminalPayload(eventType, normalized); err != nil {
			return nil, err
		}
		return normalized, nil
	}
	if err := validateRuntimeEventPayloadValue(normalized, 0); err != nil {
		return nil, err
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
		"schemaVersion":       {},
		"reason":              {},
		"resumable":           {},
		"partialArtifactRefs": {},
		"checkpointId":        {},
		"timeoutType":         {},
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
		Cause:               cause,
		Resumable:           &resumable,
		PartialArtifactRefs: refs,
		CheckpointID:        checkpointID,
		TimeoutType:         RuntimeTimeoutType(timeoutType),
	})
	if err != nil ||
		decision.EventType != eventType ||
		decision.Reason != reason ||
		decision.Resumable != resumable ||
		decision.CheckpointID != checkpointID ||
		string(decision.TimeoutType) != timeoutType ||
		!sameRuntimeTerminalRefs(decision.PartialArtifactRefs, refs) {
		return invalidRuntimeTerminalPayload()
	}
	return nil
}

func validateRuntimeEventPayloadValue(value any, depth int) error {
	if depth > runtimeEventMaxDepth {
		return fmt.Errorf("runtime event payload exceeds maximum nesting depth")
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if len(key) == 0 || len(key) > runtimeEventMaxKeyBytes {
				return fmt.Errorf("runtime event payload key length is unsafe")
			}
			normalizedKey := normalizeRuntimeEventKey(key)
			if _, forbidden := runtimeEventForbiddenKeys[normalizedKey]; forbidden {
				return fmt.Errorf("runtime event payload key %q is forbidden", key)
			}
			if err := validateRuntimeEventPayloadValue(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateRuntimeEventPayloadValue(child, depth+1); err != nil {
				return err
			}
		}
	case string:
		if len(typed) > runtimeEventMaxStringBytes {
			return fmt.Errorf("runtime event payload string exceeds %d bytes", runtimeEventMaxStringBytes)
		}
		if containsSensitiveString(typed) {
			return fmt.Errorf("runtime event payload contains sensitive string material")
		}
	case nil, bool, json.Number:
		return nil
	default:
		return fmt.Errorf("runtime event payload contains unsupported JSON value %T", value)
	}
	return nil
}

func normalizeRuntimeEventKey(key string) string {
	var normalized strings.Builder
	for _, character := range strings.ToLower(key) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}

func validateRuntimeEventRunID(runID string) error {
	if len(runID) == 0 || len(runID) > runtimeEventMaxRunIDBytes || !runtimeEventRunIDPattern.MatchString(runID) {
		return fmt.Errorf("runId is invalid or unsafe")
	}
	if runID == "." || runID == ".." {
		return fmt.Errorf("runId is invalid or unsafe")
	}
	return nil
}

func validateRuntimeEventContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("runtime event context is required")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func makeRuntimeEventTypeSet(types []RunEventType) map[RunEventType]struct{} {
	set := make(map[RunEventType]struct{}, len(types))
	for _, eventType := range types {
		set[eventType] = struct{}{}
	}
	return set
}
