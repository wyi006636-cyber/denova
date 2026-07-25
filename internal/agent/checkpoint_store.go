package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"denova/internal/workspacepath"
)

const (
	defaultCheckpointDirectory = workspacepath.DataDirName + "/checkpoints"
	checkpointSchemaVersion    = "1"
	checkpointMaxKeyBytes      = 1024
	checkpointMaxValueBytes    = 256 * 1024
	checkpointMaxEnvelopeBytes = 512 * 1024
)

const (
	CheckpointErrorWriteFailed         = "write_failed"
	CheckpointErrorDurabilityUncertain = "durability_uncertain"
	CheckpointErrorMalformed           = "malformed"
	CheckpointErrorSchemaUnsupported   = "schema_unsupported"
	CheckpointErrorKeyMismatch         = "key_mismatch"
	CheckpointErrorKeyHashMismatch     = "key_hash_mismatch"
	CheckpointErrorVersionInvalid      = "version_invalid"
	CheckpointErrorTimestampInvalid    = "timestamp_invalid"
	CheckpointErrorValueHashMismatch   = "value_hash_mismatch"
	CheckpointErrorChecksumMismatch    = "checksum_mismatch"
	CheckpointErrorOversize            = "oversize"
	CheckpointErrorSensitiveMaterial   = "sensitive_material"
	CheckpointErrorUnsafeFile          = "unsafe_file"
	CheckpointErrorUnsafeRoot          = "unsafe_root"
	CheckpointErrorClosed              = "closed"
)

// CheckpointWritePhase identifies a crash boundary in the atomic replacement
// sequence. A barrier is used by durability adapters and fault-injection tests.
type CheckpointWritePhase string

const (
	CheckpointWritePhaseTempCreate    CheckpointWritePhase = "temp-create"
	CheckpointWritePhaseTempWrite     CheckpointWritePhase = "temp-write"
	CheckpointWritePhaseTempSync      CheckpointWritePhase = "temp-sync"
	CheckpointWritePhaseReplace       CheckpointWritePhase = "replace"
	CheckpointWritePhaseDirectorySync CheckpointWritePhase = "directory-sync"
)

// CheckpointWriteBarrier can fail one durability phase without receiving the
// checkpoint key, value, path, or envelope bytes.
type CheckpointWriteBarrier func(CheckpointWritePhase) error

// CheckpointStoreError is a fail-closed typed storage error. Error deliberately
// exposes only a stable code and never the underlying path, bytes, or secret.
type CheckpointStoreError struct {
	code    string
	version uint64
}

func (e *CheckpointStoreError) Error() string {
	if e == nil {
		return "checkpoint failure"
	}
	return "checkpoint failure: " + e.code
}

// Code returns the stable failure code.
func (e *CheckpointStoreError) Code() string {
	if e == nil {
		return ""
	}
	return e.code
}

// Version returns a parsed version only when the failing decoder had already
// established that the positive integer field itself was trustworthy.
func (e *CheckpointStoreError) Version() uint64 {
	if e == nil {
		return 0
	}
	return e.version
}

// CheckpointErrorCode extracts a stable code without exposing an inner error.
func CheckpointErrorCode(err error) string {
	var checkpointErr *CheckpointStoreError
	if errors.As(err, &checkpointErr) {
		return checkpointErr.Code()
	}
	return CheckpointErrorWriteFailed
}

// CheckpointErrorVersion extracts a trustworthy positive version when present.
func CheckpointErrorVersion(err error) uint64 {
	var checkpointErr *CheckpointStoreError
	if errors.As(err, &checkpointErr) {
		return checkpointErr.Version()
	}
	return 0
}

// DurableCheckpoint is an immutable decoded checkpoint value.
type DurableCheckpoint struct {
	Version   uint64
	UpdatedAt time.Time
	Value     []byte
}

// RuntimeCheckpointStore is the path-free port used by the Yanzhou adapter.
type RuntimeCheckpointStore interface {
	Put(context.Context, string, []byte) (DurableCheckpoint, error)
	Load(context.Context, string) (DurableCheckpoint, bool, error)
	Remove(context.Context, string) error
	Close() error
}

type checkpointRecord struct {
	SchemaVersion string `json:"schemaVersion"`
	Key           string `json:"key"`
	KeyHash       string `json:"keyHash"`
	Version       uint64 `json:"version"`
	UpdatedAt     string `json:"updatedAt"`
	ValueHash     string `json:"valueHash"`
	Value         []byte `json:"value"`
	Checksum      string `json:"checksum"`
}

type checkpointChecksumFields struct {
	SchemaVersion string `json:"schemaVersion"`
	Key           string `json:"key"`
	KeyHash       string `json:"keyHash"`
	Version       uint64 `json:"version"`
	UpdatedAt     string `json:"updatedAt"`
	ValueHash     string `json:"valueHash"`
	Value         []byte `json:"value"`
}

type fileCheckpointStore struct {
	dir        string
	anchorPath string
	anchorRoot *os.Root
	root       *os.Root
	barrier    CheckpointWriteBarrier
	mu         sync.Mutex
	closed     bool
}

type copyingMemoryCheckpointStore struct {
	mu  sync.Mutex
	mem map[string][]byte
}

var (
	checkpointHashPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	checkpointSecretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bapi[\s_-]*key\b["']?\s*[:=]`),
		regexp.MustCompile(`(?i)\bruntime[\s_-]*auth\b["']?\s*[:=]`),
		regexp.MustCompile(`(?i)\bauthorization\b["']?\s*[:=]`),
		regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`),
		regexp.MustCompile(`(?i)\bsk[-_][A-Za-z0-9_-]{8,}\b`),
	}
)

func newCheckpointStore(workspace, agentKind string) interface {
	Set(context.Context, string, []byte) error
	Get(context.Context, string) ([]byte, bool, error)
} {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return &copyingMemoryCheckpointStore{mem: map[string][]byte{}}
	}
	agentKind = sanitizeCheckpointSegment(agentKind)
	if agentKind == "" {
		agentKind = AgentKindUnknown
	}
	return &fileCheckpointStore{dir: workspacepath.Path(workspace, "checkpoints", agentKind)}
}

// NewRuntimeCheckpointStore opens an isolated runtime metadata root. The root
// must already exist; this constructor never accepts or discovers a book path.
func NewRuntimeCheckpointStore(runtimeRoot string, barrier CheckpointWriteBarrier) (RuntimeCheckpointStore, error) {
	if runtimeRoot == "" || runtimeRoot != strings.TrimSpace(runtimeRoot) || !filepath.IsAbs(runtimeRoot) {
		return nil, checkpointFailure(CheckpointErrorUnsafeRoot, 0, nil)
	}
	clean := filepath.Clean(runtimeRoot)
	if clean != runtimeRoot || filepath.Dir(clean) == clean {
		return nil, checkpointFailure(CheckpointErrorUnsafeRoot, 0, nil)
	}
	info, err := os.Lstat(clean)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, checkpointFailure(CheckpointErrorUnsafeRoot, 0, err)
	}
	anchor, err := os.OpenRoot(clean)
	if err != nil {
		return nil, checkpointFailure(CheckpointErrorUnsafeRoot, 0, err)
	}
	created := false
	if err := anchor.Mkdir("checkpoints", 0o700); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		_ = anchor.Close()
		return nil, checkpointFailure(CheckpointErrorUnsafeRoot, 0, err)
	}
	checkpointInfo, err := anchor.Lstat("checkpoints")
	if err != nil || checkpointInfo.Mode()&os.ModeSymlink != 0 || !checkpointInfo.IsDir() {
		_ = anchor.Close()
		return nil, checkpointFailure(CheckpointErrorUnsafeRoot, 0, err)
	}
	if err := anchor.Chmod("checkpoints", 0o700); err != nil {
		_ = anchor.Close()
		return nil, checkpointFailure(CheckpointErrorUnsafeRoot, 0, err)
	}
	root, err := anchor.OpenRoot("checkpoints")
	if err != nil {
		_ = anchor.Close()
		return nil, checkpointFailure(CheckpointErrorUnsafeRoot, 0, err)
	}
	if created {
		if err := syncCheckpointDirectory(anchor); err != nil {
			_ = root.Close()
			_ = anchor.Close()
			return nil, checkpointFailure(CheckpointErrorDurabilityUncertain, 0, err)
		}
	}
	store := &fileCheckpointStore{
		dir:        filepath.Join(clean, "checkpoints"),
		anchorPath: clean,
		anchorRoot: anchor,
		root:       root,
		barrier:    barrier,
	}
	if err := store.verifyRootIdentityLocked(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

// removeCheckpoint discards only the hashed internal checkpoint target.
func removeCheckpoint(workspace, agentKind, key string) error {
	workspace = strings.TrimSpace(workspace)
	key = strings.TrimSpace(key)
	if workspace == "" || key == "" {
		return nil
	}
	agentKind = sanitizeCheckpointSegment(agentKind)
	if agentKind == "" {
		agentKind = AgentKindUnknown
	}
	store := &fileCheckpointStore{dir: workspacepath.Path(workspace, "checkpoints", agentKind)}
	defer store.Close()
	return store.Remove(context.Background(), key)
}

func (s *fileCheckpointStore) Set(ctx context.Context, key string, value []byte) error {
	_, err := s.Put(ctx, key, value)
	return err
}

func (s *fileCheckpointStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	record, ok, err := s.Load(ctx, key)
	return append([]byte(nil), record.Value...), ok, err
}

func (s *fileCheckpointStore) Put(ctx context.Context, key string, value []byte) (DurableCheckpoint, error) {
	if s == nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorClosed, 0, nil)
	}
	key = strings.TrimSpace(key)
	if err := validateCheckpointInput(key, value); err != nil {
		return DurableCheckpoint{}, err
	}
	value = append([]byte(nil), value...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpenLocked(); err != nil {
		return DurableCheckpoint{}, err
	}
	if err := s.verifyRootIdentityLocked(); err != nil {
		return DurableCheckpoint{}, err
	}
	if err := checkpointContextError(ctx); err != nil {
		return DurableCheckpoint{}, err
	}
	existing, found, err := s.loadLocked(key)
	if err != nil {
		return DurableCheckpoint{}, err
	}
	version := uint64(1)
	if found {
		if existing.Version == ^uint64(0) {
			return DurableCheckpoint{}, checkpointFailure(CheckpointErrorVersionInvalid, existing.Version, nil)
		}
		version = existing.Version + 1
	}
	record, data, err := createCheckpointRecord(key, version, value)
	if err != nil {
		return DurableCheckpoint{}, err
	}
	target := s.pathForKey(key)
	if err := validateCheckpointTargetType(s.root, target, true); err != nil {
		return DurableCheckpoint{}, err
	}
	if err := s.runBarrier(CheckpointWritePhaseTempCreate); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	tempName, file, err := createCheckpointTemp(s.root)
	if err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	tempActive := true
	defer func() {
		if tempActive {
			_ = file.Close()
			_ = s.root.Remove(tempName)
		}
	}()
	if err := s.runBarrier(CheckpointWritePhaseTempWrite); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	if err := writeCheckpointBytes(file, data); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	if err := s.runBarrier(CheckpointWritePhaseTempSync); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	if err := file.Sync(); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	tempInfo, err := file.Stat()
	if err != nil || !tempInfo.Mode().IsRegular() {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorUnsafeFile, 0, err)
	}
	if err := file.Close(); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	if err := s.runBarrier(CheckpointWritePhaseReplace); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	if err := s.verifyRootIdentityLocked(); err != nil {
		return DurableCheckpoint{}, err
	}
	if err := validateCheckpointTempIdentity(s.root, tempName, tempInfo); err != nil {
		return DurableCheckpoint{}, err
	}
	if err := validateCheckpointTargetType(s.root, target, true); err != nil {
		return DurableCheckpoint{}, err
	}
	if err := s.root.Rename(tempName, target); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	tempActive = false
	if err := s.runBarrier(CheckpointWritePhaseDirectorySync); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorDurabilityUncertain, version, err)
	}
	if err := s.verifyRootIdentityLocked(); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorDurabilityUncertain, version, err)
	}
	if err := syncCheckpointDirectory(s.root); err != nil {
		return DurableCheckpoint{}, checkpointFailure(CheckpointErrorDurabilityUncertain, version, err)
	}
	return DurableCheckpoint{Version: record.Version, UpdatedAt: mustCheckpointTime(record.UpdatedAt), Value: append([]byte(nil), value...)}, nil
}

func (s *fileCheckpointStore) Load(ctx context.Context, key string) (DurableCheckpoint, bool, error) {
	if s == nil {
		return DurableCheckpoint{}, false, checkpointFailure(CheckpointErrorClosed, 0, nil)
	}
	key = strings.TrimSpace(key)
	if err := validateCheckpointKey(key); err != nil {
		return DurableCheckpoint{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpenLocked(); err != nil {
		return DurableCheckpoint{}, false, err
	}
	if err := s.verifyRootIdentityLocked(); err != nil {
		return DurableCheckpoint{}, false, err
	}
	if err := checkpointContextError(ctx); err != nil {
		return DurableCheckpoint{}, false, err
	}
	return s.loadLocked(key)
}

func (s *fileCheckpointStore) loadLocked(key string) (DurableCheckpoint, bool, error) {
	target := s.pathForKey(key)
	if err := validateCheckpointTargetType(s.root, target, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DurableCheckpoint{}, false, nil
		}
		return DurableCheckpoint{}, false, err
	}
	file, err := s.root.Open(target)
	if errors.Is(err, os.ErrNotExist) {
		return DurableCheckpoint{}, false, nil
	}
	if err != nil {
		return DurableCheckpoint{}, false, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return DurableCheckpoint{}, false, checkpointFailure(CheckpointErrorUnsafeFile, 0, err)
	}
	pathInfo, err := s.root.Lstat(target)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.Mode().IsRegular() || !pathInfo.Mode().IsRegular() || !os.SameFile(openedInfo, pathInfo) {
		return DurableCheckpoint{}, false, checkpointFailure(CheckpointErrorUnsafeFile, 0, err)
	}
	if openedInfo.Size() < 0 || openedInfo.Size() > checkpointMaxEnvelopeBytes {
		return DurableCheckpoint{}, false, checkpointFailure(CheckpointErrorOversize, 0, nil)
	}
	data, err := io.ReadAll(io.LimitReader(file, checkpointMaxEnvelopeBytes+1))
	if err != nil {
		return DurableCheckpoint{}, false, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	if len(data) > checkpointMaxEnvelopeBytes {
		return DurableCheckpoint{}, false, checkpointFailure(CheckpointErrorOversize, 0, nil)
	}
	record, updatedAt, err := decodeCheckpointRecord(data, key)
	if err != nil {
		return DurableCheckpoint{}, false, err
	}
	return DurableCheckpoint{Version: record.Version, UpdatedAt: updatedAt, Value: append([]byte(nil), record.Value...)}, true, nil
}

func (s *fileCheckpointStore) Remove(ctx context.Context, key string) error {
	if s == nil {
		return checkpointFailure(CheckpointErrorClosed, 0, nil)
	}
	key = strings.TrimSpace(key)
	if err := validateCheckpointKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpenLocked(); err != nil {
		return err
	}
	if err := s.verifyRootIdentityLocked(); err != nil {
		return err
	}
	if err := checkpointContextError(ctx); err != nil {
		return err
	}
	target := s.pathForKey(key)
	if err := s.root.Remove(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	if err := syncCheckpointDirectory(s.root); err != nil {
		return checkpointFailure(CheckpointErrorDurabilityUncertain, 0, err)
	}
	return nil
}

func (s *fileCheckpointStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var closeErr error
	if s.root != nil {
		closeErr = s.root.Close()
		s.root = nil
	}
	if s.anchorRoot != nil {
		if err := s.anchorRoot.Close(); closeErr == nil {
			closeErr = err
		}
		s.anchorRoot = nil
	}
	return closeErr
}

func (s *fileCheckpointStore) ensureOpenLocked() error {
	if s.closed {
		return checkpointFailure(CheckpointErrorClosed, 0, nil)
	}
	if s.root != nil {
		return nil
	}
	if s.dir == "" {
		return checkpointFailure(CheckpointErrorUnsafeRoot, 0, nil)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	info, err := os.Lstat(s.dir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return checkpointFailure(CheckpointErrorUnsafeRoot, 0, err)
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return checkpointFailure(CheckpointErrorUnsafeRoot, 0, err)
	}
	s.root = root
	return nil
}

func (s *fileCheckpointStore) verifyRootIdentityLocked() error {
	if s.root == nil {
		return checkpointFailure(CheckpointErrorUnsafeRoot, 0, nil)
	}
	openedInfo, err := s.root.Stat(".")
	if err != nil || !openedInfo.IsDir() {
		return checkpointFailure(CheckpointErrorUnsafeRoot, 0, err)
	}
	if s.anchorRoot != nil {
		anchorOpened, anchorErr := s.anchorRoot.Stat(".")
		pathInfo, pathErr := os.Lstat(s.anchorPath)
		if anchorErr != nil || pathErr != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() || !os.SameFile(anchorOpened, pathInfo) {
			return checkpointFailure(CheckpointErrorUnsafeRoot, 0, errors.Join(anchorErr, pathErr))
		}
		checkpointInfo, checkpointErr := s.anchorRoot.Lstat("checkpoints")
		if checkpointErr != nil || checkpointInfo.Mode()&os.ModeSymlink != 0 || !checkpointInfo.IsDir() || !os.SameFile(openedInfo, checkpointInfo) {
			return checkpointFailure(CheckpointErrorUnsafeRoot, 0, checkpointErr)
		}
		return nil
	}
	pathInfo, err := os.Lstat(s.dir)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() || !os.SameFile(openedInfo, pathInfo) {
		return checkpointFailure(CheckpointErrorUnsafeRoot, 0, err)
	}
	return nil
}

func (s *fileCheckpointStore) runBarrier(phase CheckpointWritePhase) error {
	if s.barrier == nil {
		return nil
	}
	return s.barrier(phase)
}

func (s *fileCheckpointStore) pathForKey(key string) string {
	return checkpointKeyHash(key) + ".json"
}

func (s *copyingMemoryCheckpointStore) Set(_ context.Context, key string, value []byte) error {
	if err := validateCheckpointInput(strings.TrimSpace(key), value); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mem[key] = append([]byte(nil), value...)
	return nil
}

func (s *copyingMemoryCheckpointStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.mem[key]
	return append([]byte(nil), value...), ok, nil
}

func createCheckpointRecord(key string, version uint64, value []byte) (checkpointRecord, []byte, error) {
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	record := checkpointRecord{
		SchemaVersion: checkpointSchemaVersion,
		Key:           key,
		KeyHash:       checkpointKeyHash(key),
		Version:       version,
		UpdatedAt:     updatedAt,
		ValueHash:     checkpointValueHash(value),
		Value:         append([]byte(nil), value...),
	}
	checksum, err := checkpointChecksum(record)
	if err != nil {
		return checkpointRecord{}, nil, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	record.Checksum = checksum
	data, err := json.Marshal(record)
	if err != nil {
		return checkpointRecord{}, nil, checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	data = append(data, '\n')
	if len(data) > checkpointMaxEnvelopeBytes {
		return checkpointRecord{}, nil, checkpointFailure(CheckpointErrorOversize, 0, nil)
	}
	return record, data, nil
}

func decodeCheckpointRecord(data []byte, expectedKey string) (checkpointRecord, time.Time, error) {
	if err := validateCheckpointEnvelopeFields(data); err != nil {
		return checkpointRecord{}, time.Time{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var record checkpointRecord
	if err := decoder.Decode(&record); err != nil {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorMalformed, 0, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorMalformed, 0, err)
	}
	if record.SchemaVersion != checkpointSchemaVersion {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorSchemaUnsupported, 0, nil)
	}
	if record.Key != expectedKey {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorKeyMismatch, 0, nil)
	}
	if !checkpointHashPattern.MatchString(record.KeyHash) || record.KeyHash != checkpointKeyHash(record.Key) {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorKeyHashMismatch, 0, nil)
	}
	if record.Version == 0 {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorVersionInvalid, 0, nil)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, record.UpdatedAt)
	if err != nil || updatedAt.Location() != time.UTC || updatedAt.Format(time.RFC3339Nano) != record.UpdatedAt {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorTimestampInvalid, record.Version, err)
	}
	if len(record.Value) > checkpointMaxValueBytes {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorOversize, record.Version, nil)
	}
	if !checkpointHashPattern.MatchString(record.ValueHash) || record.ValueHash != checkpointValueHash(record.Value) {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorValueHashMismatch, record.Version, nil)
	}
	if !checkpointHashPattern.MatchString(record.Checksum) {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorChecksumMismatch, record.Version, nil)
	}
	wantChecksum, err := checkpointChecksum(record)
	if err != nil || record.Checksum != wantChecksum {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorChecksumMismatch, record.Version, err)
	}
	if containsCheckpointSensitiveMaterial(record.Value) {
		return checkpointRecord{}, time.Time{}, checkpointFailure(CheckpointErrorSensitiveMaterial, record.Version, nil)
	}
	return record, updatedAt, nil
}

func validateCheckpointEnvelopeFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return checkpointFailure(CheckpointErrorMalformed, 0, err)
	}
	want := map[string]struct{}{
		"schemaVersion": {},
		"key":           {},
		"keyHash":       {},
		"version":       {},
		"updatedAt":     {},
		"valueHash":     {},
		"value":         {},
		"checksum":      {},
	}
	seen := make(map[string]struct{}, len(want))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return checkpointFailure(CheckpointErrorMalformed, 0, err)
		}
		field, ok := token.(string)
		if !ok {
			return checkpointFailure(CheckpointErrorMalformed, 0, nil)
		}
		if _, allowed := want[field]; !allowed {
			return checkpointFailure(CheckpointErrorMalformed, 0, nil)
		}
		if _, duplicate := seen[field]; duplicate {
			return checkpointFailure(CheckpointErrorMalformed, 0, nil)
		}
		seen[field] = struct{}{}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return checkpointFailure(CheckpointErrorMalformed, 0, err)
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != len(want) {
		return checkpointFailure(CheckpointErrorMalformed, 0, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return checkpointFailure(CheckpointErrorMalformed, 0, err)
	}
	return nil
}

func checkpointChecksum(record checkpointRecord) (string, error) {
	fields := checkpointChecksumFields{
		SchemaVersion: record.SchemaVersion,
		Key:           record.Key,
		KeyHash:       record.KeyHash,
		Version:       record.Version,
		UpdatedAt:     record.UpdatedAt,
		ValueHash:     record.ValueHash,
		Value:         record.Value,
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func checkpointKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func checkpointValueHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func validateCheckpointInput(key string, value []byte) error {
	if err := validateCheckpointKey(key); err != nil {
		return err
	}
	if len(value) > checkpointMaxValueBytes {
		return checkpointFailure(CheckpointErrorOversize, 0, nil)
	}
	if containsCheckpointSensitiveMaterial(value) {
		return checkpointFailure(CheckpointErrorSensitiveMaterial, 0, nil)
	}
	return nil
}

func validateCheckpointKey(key string) error {
	if key == "" || len(key) > checkpointMaxKeyBytes || strings.TrimSpace(key) != key {
		return checkpointFailure(CheckpointErrorKeyMismatch, 0, nil)
	}
	if containsCheckpointSensitiveMaterial([]byte(key)) {
		return checkpointFailure(CheckpointErrorSensitiveMaterial, 0, nil)
	}
	return nil
}

func containsCheckpointSensitiveMaterial(data []byte) bool {
	for _, pattern := range checkpointSecretPatterns {
		if pattern.Match(data) {
			return true
		}
	}
	return false
}

func validateCheckpointTargetType(root *os.Root, target string, missingOK bool) error {
	info, err := root.Lstat(target)
	if errors.Is(err, os.ErrNotExist) && missingOK {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return checkpointFailure(CheckpointErrorUnsafeFile, 0, nil)
	}
	return nil
}

func validateCheckpointTempIdentity(root *os.Root, name string, expected os.FileInfo) error {
	info, err := root.Lstat(name)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !os.SameFile(expected, info) {
		return checkpointFailure(CheckpointErrorUnsafeFile, 0, err)
	}
	return nil
}

func createCheckpointTemp(root *os.Root) (string, *os.File, error) {
	for attempts := 0; attempts < 16; attempts++ {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", nil, err
		}
		name := ".checkpoint-" + hex.EncodeToString(nonce[:]) + ".tmp"
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return name, file, nil
	}
	return "", nil, errors.New("checkpoint temporary name exhausted")
}

func writeCheckpointBytes(file *os.File, data []byte) error {
	for len(data) > 0 {
		written, err := file.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(data) {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func syncCheckpointDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || !info.IsDir() {
		return errors.Join(err, errors.New("checkpoint directory is unsafe"))
	}
	return directory.Sync()
}

func checkpointContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return checkpointFailure(CheckpointErrorWriteFailed, 0, err)
	}
	return nil
}

func checkpointFailure(code string, version uint64, _ error) error {
	return &CheckpointStoreError{code: code, version: version}
}

func mustCheckpointTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func sanitizeCheckpointSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}
