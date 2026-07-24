package workspace

import (
	"context"
	"fmt"
)

const (
	// MigrationAuthorizationVersionV1 is the exact author-authorization format
	// accepted by the Workspace Schema v1 executor.
	MigrationAuthorizationVersionV1         = 1
	MigrationRecoveryAuthorizationVersionV1 = 1
	// MigrationRecordVersionV1 is the exact durable recovery-record format.
	MigrationRecordVersionV1 = 1
	// MaxMigrationIDBytes keeps the single portable path segment bounded.
	MaxMigrationIDBytes = 128

	MigrationRootRelativePath = ".denova-migration"
)

const (
	CodeMigrationAuthorizationRequired ErrorCode = "migration_authorization_required"
	CodeMigrationAuthorizationMismatch ErrorCode = "migration_authorization_mismatch"
	CodeMigrationIDInvalid             ErrorCode = "migration_id_invalid"
	CodeMigrationIDCollision           ErrorCode = "migration_id_collision"
	CodeMigrationLeaseRequired         ErrorCode = "migration_lease_required"
	CodeMigrationLeaseViolation        ErrorCode = "migration_lease_violation"
	CodeMigrationRecordInvalid         ErrorCode = "migration_record_invalid"
	CodeMigrationRecordConflict        ErrorCode = "migration_record_conflict"
	CodeMigrationArtifactInvalid       ErrorCode = "migration_artifact_invalid"
	CodeMigrationSourceChanged         ErrorCode = "migration_source_changed"
	CodeMigrationPreflight             ErrorCode = "migration_preflight_failed"
	CodeMigrationDurability            ErrorCode = "migration_durability_failed"
	CodeMigrationSwitchConflict        ErrorCode = "migration_switch_conflict"
	CodeMigrationVerification          ErrorCode = "migration_verification_failed"
	CodeMigrationRecoveryRequired      ErrorCode = "migration_recovery_required"
	CodeMigrationRollbackConflict      ErrorCode = "migration_rollback_conflict"
)

// WorkspaceWriterLease is intentionally owned by this consumer package. The
// existing workspacechange.Service satisfies it without an import dependency.
type WorkspaceWriterLease interface {
	WithExclusiveWorkspace(context.Context, func() error) error
}

// MigrationExecutorOptions fixes the shared lease and the exact read-only
// capability configuration used to rebuild a preview under that lease.
type MigrationExecutorOptions struct {
	Lease          WorkspaceWriterLease
	PreviewOptions PreviewOptions
}

// MigrationRequest is the explicit execution boundary. Neither field alone is
// authority to mutate a workspace.
type MigrationRequest struct {
	Preview       MigrationPreview       `json:"preview"`
	Authorization MigrationAuthorization `json:"authorization"`
}

// MigrationRecoveryAction is an explicit author choice, never inferred from a
// failure or from merely reopening an incomplete operation.
type MigrationRecoveryAction string

const (
	RecoveryActionRollForward MigrationRecoveryAction = "roll_forward"
	RecoveryActionRollback    MigrationRecoveryAction = "rollback"
)

// MigrationRecoveryAuthorization binds a recovery choice to the original
// migration authorization plus fresh author evidence.
type MigrationRecoveryAuthorization struct {
	Version                      int                     `json:"version"`
	Action                       MigrationRecoveryAction `json:"action"`
	MigrationID                  string                  `json:"migration_id"`
	MigrationAuthorizationSHA256 string                  `json:"migration_authorization_sha256"`
	Confirmation                 AuthorConfirmation      `json:"confirmation"`
	PayloadSHA256                string                  `json:"payload_sha256"`
}

// MigrationRecoveryRequest carries both the original immutable migration
// request and the separately confirmed recovery choice.
type MigrationRecoveryRequest struct {
	Migration     MigrationRequest               `json:"migration"`
	Authorization MigrationRecoveryAuthorization `json:"authorization"`
}

// MigrationResult is the durable state observed at the end of one attempt.
type MigrationResult struct {
	MigrationID       string                `json:"migration_id"`
	State             MigrationState        `json:"state"`
	NextAction        MigrationNextAction   `json:"next_action"`
	WorkspaceMutated  bool                  `json:"workspace_mutated"`
	RollbackAvailable bool                  `json:"rollback_available"`
	Receipt           *MigrationArtifactRef `json:"receipt,omitempty"`
}

// AuthorConfirmation is explicit author evidence. It is never inferred from
// caller identity, a bool, or the existence of an earlier preview.
type AuthorConfirmation struct {
	ID       string `json:"id"`
	Evidence string `json:"evidence"`
}

// SourceExpectation binds one complete preview snapshot entry.
type SourceExpectation struct {
	Path     string             `json:"path"`
	NodeType string             `json:"node_type"`
	Mode     uint32             `json:"mode"`
	Identity FilesystemIdentity `json:"identity"`
	Size     int64              `json:"size"`
	SHA256   string             `json:"sha256,omitempty"`
}

// FilesystemIdentity pins a path to a volume/device and file ID. ReparsePoint
// is explicit so Windows junctions cannot masquerade as ordinary directories.
type FilesystemIdentity struct {
	Volume       string `json:"volume"`
	FileID       string `json:"file_id"`
	ReparsePoint bool   `json:"reparse_point"`
}

// MigrationAuthorization binds the complete migration payload to explicit
// author evidence. PayloadSHA256 covers every preceding field.
type MigrationAuthorization struct {
	Version             int                 `json:"version"`
	MigrationID         string              `json:"migration_id"`
	CanonicalWorkspace  string              `json:"canonical_workspace"`
	CanonicalSourceRoot string              `json:"canonical_source_root"`
	TargetIdentityPath  string              `json:"target_identity_path"`
	WorkspaceIdentity   FilesystemIdentity  `json:"workspace_identity"`
	SourceRootIdentity  FilesystemIdentity  `json:"source_root_identity"`
	TargetIdentity      FilesystemIdentity  `json:"target_identity"`
	PreviewSHA256       string              `json:"preview_sha256"`
	Sources             []SourceExpectation `json:"sources"`
	TargetSchemaVersion int                 `json:"target_schema_version"`
	TargetFeatures      []PreviewFeature    `json:"target_features"`
	Confirmation        AuthorConfirmation  `json:"confirmation"`
	PayloadSHA256       string              `json:"payload_sha256"`
}

// ConfirmationBinding is the durable, non-plaintext author evidence stored in
// records and receipts.
type ConfirmationBinding struct {
	ID             string `json:"id"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}

// MigrationStep identifies the exact execution boundary that failed.
type MigrationStep string

const (
	MigrationStepAuthorize      MigrationStep = "authorize"
	MigrationStepAcquireLease   MigrationStep = "acquire_lease"
	MigrationStepLoadRecord     MigrationStep = "load_record"
	MigrationStepValidate       MigrationStep = "validate"
	MigrationStepPreflight      MigrationStep = "preflight"
	MigrationStepBackup         MigrationStep = "backup"
	MigrationStepStage          MigrationStep = "stage"
	MigrationStepPrepareSwitch  MigrationStep = "prepare_switch"
	MigrationStepSwitch         MigrationStep = "switch"
	MigrationStepVerify         MigrationStep = "verify"
	MigrationStepPublishReceipt MigrationStep = "publish_receipt"
	MigrationStepComplete       MigrationStep = "complete"
	MigrationStepResume         MigrationStep = "resume"
	MigrationStepRollback       MigrationStep = "rollback"
)

// MigrationNextAction is persisted rather than guessed after a crash.
type MigrationNextAction string

const (
	MigrationNextNone           MigrationNextAction = "none"
	MigrationNextValidate       MigrationNextAction = "validate"
	MigrationNextBackup         MigrationNextAction = "backup"
	MigrationNextStage          MigrationNextAction = "stage"
	MigrationNextPrepareSwitch  MigrationNextAction = "prepare_switch"
	MigrationNextSwitch         MigrationNextAction = "switch"
	MigrationNextVerify         MigrationNextAction = "verify"
	MigrationNextPublishReceipt MigrationNextAction = "publish_receipt"
	MigrationNextComplete       MigrationNextAction = "complete"
	MigrationNextResume         MigrationNextAction = "resume"
	MigrationNextRollback       MigrationNextAction = "rollback"
	MigrationNextManualRecovery MigrationNextAction = "manual_recovery"
)

// DurabilityStatus describes what is known about the failed boundary.
type DurabilityStatus string

const (
	DurabilityNotStarted DurabilityStatus = "not_started"
	DurabilityPending    DurabilityStatus = "pending"
	DurabilityDurable    DurabilityStatus = "durable"
	DurabilityBestEffort DurabilityStatus = "best_effort"
)

// RecoveryStatus describes the deterministic recovery contract after failure.
type RecoveryStatus string

const (
	RecoveryNotRequired RecoveryStatus = "not_required"
	RecoveryAvailable   RecoveryStatus = "available"
	RecoveryRequired    RecoveryStatus = "required"
	RecoveryCompleted   RecoveryStatus = "completed"
)

// MigrationError retains enough evidence to diagnose and resume without
// flattening a mutation or durability failure into a warning.
type MigrationError struct {
	Code             ErrorCode           `json:"code"`
	MigrationID      string              `json:"migration_id,omitempty"`
	State            MigrationState      `json:"state,omitempty"`
	Step             MigrationStep       `json:"step"`
	Path             string              `json:"path,omitempty"`
	ExpectedSHA256   string              `json:"expected_sha256,omitempty"`
	ActualSHA256     string              `json:"actual_sha256,omitempty"`
	WorkspaceMutated bool                `json:"workspace_mutated"`
	Durability       DurabilityStatus    `json:"durability"`
	Recovery         RecoveryStatus      `json:"recovery"`
	NextAction       MigrationNextAction `json:"next_action"`
	Message          string              `json:"message"`
	Err              error               `json:"-"`
}

func (err *MigrationError) Error() string {
	if err == nil {
		return "<nil>"
	}
	message := err.Message
	if message == "" {
		message = string(err.Code)
	}
	return fmt.Sprintf(
		"workspace migration %s migration_id=%q state=%s step=%s path=%q expected_sha256=%q actual_sha256=%q workspace_mutated=%t durability=%s recovery=%s next_action=%s: %s",
		err.Code,
		err.MigrationID,
		err.State,
		err.Step,
		err.Path,
		err.ExpectedSHA256,
		err.ActualSHA256,
		err.WorkspaceMutated,
		err.Durability,
		err.Recovery,
		err.NextAction,
		message,
	)
}

func (err *MigrationError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}
