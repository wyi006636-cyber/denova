package yanzhouadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"denova/internal/agent"
)

const (
	checkpointMaxIdentityBytes = 128
	checkpointMaxValueBytes    = 256 * 1024
	checkpointMaxEnvelopeBytes = 512 * 1024
)

// CheckpointStatus is deliberately closed: storage internals never cross the
// sidecar boundary.
type CheckpointStatus string

const (
	CheckpointStatusOK       CheckpointStatus = "ok"
	CheckpointStatusMissing  CheckpointStatus = "missing"
	CheckpointStatusDegraded CheckpointStatus = "degraded"
)

const (
	CheckpointCodeWriteFailed         = "checkpoint_write_failed"
	CheckpointCodeDurabilityUncertain = "checkpoint_durability_uncertain"
	CheckpointCodeMalformed           = "checkpoint_malformed"
	CheckpointCodeSchemaUnsupported   = "checkpoint_schema_unsupported"
	CheckpointCodeKeyMismatch         = "checkpoint_key_mismatch"
	CheckpointCodeKeyHashMismatch     = "checkpoint_key_hash_mismatch"
	CheckpointCodeVersionInvalid      = "checkpoint_version_invalid"
	CheckpointCodeTimestampInvalid    = "checkpoint_timestamp_invalid"
	CheckpointCodeValueHashMismatch   = "checkpoint_value_hash_mismatch"
	CheckpointCodeChecksumMismatch    = "checkpoint_checksum_mismatch"
	CheckpointCodeOversize            = "checkpoint_oversize"
	CheckpointCodeSensitiveMaterial   = "checkpoint_sensitive_material"
	CheckpointCodeInvalidRef          = "checkpoint_invalid_ref"
	CheckpointCodeUnsafeFile          = "checkpoint_unsafe_file"
	CheckpointCodeUnsafeRoot          = "checkpoint_unsafe_root"
	CheckpointCodeClosed              = "checkpoint_closed"
)

// CheckpointRef is a stable logical identity. It intentionally has no path,
// workspace, credential, provider profile, raw-frame, or stderr field.
type CheckpointRef struct {
	CheckpointID string `json:"checkpointId"`
	Namespace    string `json:"namespace"`
	AgentKind    string `json:"agentKind"`
	SessionID    string `json:"sessionId,omitempty"`
	TaskID       string `json:"taskId,omitempty"`
	RunID        string `json:"runId,omitempty"`
	AttemptID    string `json:"attemptId,omitempty"`
}

// CheckpointResult is the complete public result. Degraded results contain no
// raw bytes, paths, stderr, credentials, or underlying error text.
type CheckpointResult struct {
	Status       CheckpointStatus `json:"status"`
	Code         string           `json:"code,omitempty"`
	CheckpointID string           `json:"checkpointId,omitempty"`
	Namespace    string           `json:"namespace,omitempty"`
	Version      uint64           `json:"version,omitempty"`
	ObservedAt   string           `json:"observedAt"`
	Resumable    bool             `json:"resumable"`
	Value        []byte           `json:"value,omitempty"`
}

// CheckpointStore persists only sidecar runtime metadata beneath the runtime
// root supplied at construction.
type CheckpointStore interface {
	Set(context.Context, CheckpointRef, []byte) CheckpointResult
	Get(context.Context, CheckpointRef) CheckpointResult
	Remove(context.Context, CheckpointRef) CheckpointResult
	Close() error
}

type checkpointWritePhase string

const (
	checkpointPhaseTempCreate    checkpointWritePhase = "temp-create"
	checkpointPhaseTempWrite     checkpointWritePhase = "temp-write"
	checkpointPhaseTempSync      checkpointWritePhase = "temp-sync"
	checkpointPhaseReplace       checkpointWritePhase = "replace"
	checkpointPhaseDirectorySync checkpointWritePhase = "directory-sync"
)

type checkpointAdapter struct {
	store agent.RuntimeCheckpointStore
}

var (
	checkpointIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	sensitiveStringPatterns   = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bapi[\s_-]*key\b["']?\s*[:=]`),
		regexp.MustCompile(`(?i)\bruntime[\s_-]*auth\b["']?\s*[:=]`),
		regexp.MustCompile(`(?i)\bauthorization\b["']?\s*[:=]`),
		regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`),
		regexp.MustCompile(`(?i)\bsk[-_][A-Za-z0-9_-]{8,}\b`),
	}
)

// NewCheckpointStore opens a checkpoint adapter at a clean absolute,
// non-filesystem-root runtime metadata directory.
func NewCheckpointStore(runtimeRoot string) (CheckpointStore, error) {
	return newCheckpointAdapterWithBarrier(runtimeRoot, nil)
}

func newCheckpointAdapterWithBarrier(runtimeRoot string, barrier func(checkpointWritePhase) error) (CheckpointStore, error) {
	var agentBarrier agent.CheckpointWriteBarrier
	if barrier != nil {
		agentBarrier = func(phase agent.CheckpointWritePhase) error {
			return barrier(checkpointWritePhase(phase))
		}
	}
	store, err := agent.NewRuntimeCheckpointStore(runtimeRoot, agentBarrier)
	if err != nil {
		return nil, errors.New("checkpoint runtime root is unavailable")
	}
	return &checkpointAdapter{store: store}, nil
}

func (s *checkpointAdapter) Set(ctx context.Context, ref CheckpointRef, value []byte) CheckpointResult {
	logicalKey, result, ok := prepareCheckpointOperation(ref)
	if !ok {
		return result
	}
	entry, err := s.store.Put(ctx, logicalKey, append([]byte(nil), value...))
	if err != nil {
		return checkpointDegradedResult(ref, err)
	}
	return CheckpointResult{
		Status:       CheckpointStatusOK,
		CheckpointID: ref.CheckpointID,
		Namespace:    ref.Namespace,
		Version:      entry.Version,
		ObservedAt:   checkpointObservedAt(),
		Resumable:    false,
	}
}

func (s *checkpointAdapter) Get(ctx context.Context, ref CheckpointRef) CheckpointResult {
	logicalKey, result, ok := prepareCheckpointOperation(ref)
	if !ok {
		return result
	}
	entry, found, err := s.store.Load(ctx, logicalKey)
	if err != nil {
		return checkpointDegradedResult(ref, err)
	}
	if !found {
		return checkpointMissingResult(ref)
	}
	return CheckpointResult{
		Status:       CheckpointStatusOK,
		CheckpointID: ref.CheckpointID,
		Namespace:    ref.Namespace,
		Version:      entry.Version,
		ObservedAt:   checkpointObservedAt(),
		Resumable:    true,
		Value:        append([]byte(nil), entry.Value...),
	}
}

func (s *checkpointAdapter) Remove(ctx context.Context, ref CheckpointRef) CheckpointResult {
	logicalKey, result, ok := prepareCheckpointOperation(ref)
	if !ok {
		return result
	}
	if err := s.store.Remove(ctx, logicalKey); err != nil {
		return checkpointDegradedResult(ref, err)
	}
	return checkpointMissingResult(ref)
}

func (s *checkpointAdapter) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	if err := s.store.Close(); err != nil {
		return errors.New("checkpoint store close failed")
	}
	return nil
}

func prepareCheckpointOperation(ref CheckpointRef) (string, CheckpointResult, bool) {
	if err := validateCheckpointRef(ref); err != nil {
		return "", CheckpointResult{
			Status:     CheckpointStatusDegraded,
			Code:       CheckpointCodeInvalidRef,
			ObservedAt: checkpointObservedAt(),
			Resumable:  false,
		}, false
	}
	return checkpointLogicalKeyForTest(ref), CheckpointResult{}, true
}

func validateCheckpointRef(ref CheckpointRef) error {
	values := []string{
		ref.CheckpointID,
		ref.Namespace,
		ref.AgentKind,
		ref.SessionID,
		ref.TaskID,
		ref.RunID,
		ref.AttemptID,
	}
	for index, value := range values {
		if index < 3 && value == "" {
			return errors.New("required identity is absent")
		}
		if value != "" && (len(value) > checkpointMaxIdentityBytes || !checkpointIdentityPattern.MatchString(value)) {
			return errors.New("identity is unsafe")
		}
	}
	if ref.SessionID == "" && ref.TaskID == "" && ref.RunID == "" {
		return errors.New("lineage is absent")
	}
	if ref.AttemptID != "" && ref.RunID == "" {
		return errors.New("attempt requires run lineage")
	}
	for _, value := range values {
		if containsSensitiveString(value) {
			return errors.New("identity is sensitive")
		}
		normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "_", ""), "-", ""))
		for _, forbidden := range []string{"bookpath", "workspacepath", "bookroot", "workspaceroot", "apikey", "runtimeauth", "authorization", "privateprofile", "rawframe", "stderr"} {
			if strings.Contains(normalized, forbidden) {
				return errors.New("identity is forbidden")
			}
		}
	}
	return nil
}

func containsSensitiveString(value string) bool {
	for _, pattern := range sensitiveStringPatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func checkpointDegradedResult(ref CheckpointRef, err error) CheckpointResult {
	return CheckpointResult{
		Status:       CheckpointStatusDegraded,
		Code:         checkpointPublicCode(agent.CheckpointErrorCode(err)),
		CheckpointID: ref.CheckpointID,
		Namespace:    ref.Namespace,
		Version:      agent.CheckpointErrorVersion(err),
		ObservedAt:   checkpointObservedAt(),
		Resumable:    false,
	}
}

func checkpointMissingResult(ref CheckpointRef) CheckpointResult {
	return CheckpointResult{
		Status:       CheckpointStatusMissing,
		CheckpointID: ref.CheckpointID,
		Namespace:    ref.Namespace,
		ObservedAt:   checkpointObservedAt(),
		Resumable:    false,
	}
}

func checkpointPublicCode(code string) string {
	switch code {
	case agent.CheckpointErrorWriteFailed:
		return CheckpointCodeWriteFailed
	case agent.CheckpointErrorDurabilityUncertain:
		return CheckpointCodeDurabilityUncertain
	case agent.CheckpointErrorMalformed:
		return CheckpointCodeMalformed
	case agent.CheckpointErrorSchemaUnsupported:
		return CheckpointCodeSchemaUnsupported
	case agent.CheckpointErrorKeyMismatch:
		return CheckpointCodeKeyMismatch
	case agent.CheckpointErrorKeyHashMismatch:
		return CheckpointCodeKeyHashMismatch
	case agent.CheckpointErrorVersionInvalid:
		return CheckpointCodeVersionInvalid
	case agent.CheckpointErrorTimestampInvalid:
		return CheckpointCodeTimestampInvalid
	case agent.CheckpointErrorValueHashMismatch:
		return CheckpointCodeValueHashMismatch
	case agent.CheckpointErrorChecksumMismatch:
		return CheckpointCodeChecksumMismatch
	case agent.CheckpointErrorOversize:
		return CheckpointCodeOversize
	case agent.CheckpointErrorSensitiveMaterial:
		return CheckpointCodeSensitiveMaterial
	case agent.CheckpointErrorUnsafeFile:
		return CheckpointCodeUnsafeFile
	case agent.CheckpointErrorUnsafeRoot:
		return CheckpointCodeUnsafeRoot
	case agent.CheckpointErrorClosed:
		return CheckpointCodeClosed
	default:
		return CheckpointCodeWriteFailed
	}
}

func checkpointObservedAt() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func checkpointLogicalKeyForTest(ref CheckpointRef) string {
	data, err := json.Marshal(ref)
	if err != nil {
		panic(fmt.Sprintf("encode checkpoint logical identity: %v", err))
	}
	return string(data)
}

func checkpointTargetPathForTest(runtimeRoot string, ref CheckpointRef) string {
	logicalKey := checkpointLogicalKeyForTest(ref)
	sum := sha256.Sum256([]byte(logicalKey))
	return filepath.Join(runtimeRoot, "checkpoints", hex.EncodeToString(sum[:])+".json")
}
