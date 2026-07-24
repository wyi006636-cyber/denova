package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxMigrationRecordBytes = 4 * 1024 * 1024

// MigrationArtifactRef binds one durable artifact to its expected bytes.
type MigrationArtifactRef struct {
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256"`
}

// MigrationSwitchIntent is durable before any namespace switch becomes
// visible. PublishedEntry is the one atomic namespace boundary claimed.
type MigrationSwitchIntent struct {
	SourceRoot      string               `json:"source_root"`
	TargetRoot      string               `json:"target_root"`
	BackupManifest  MigrationArtifactRef `json:"backup_manifest"`
	Stage           MigrationArtifactRef `json:"stage"`
	PublishedEntry  string               `json:"published_entry"`
	PublishedSHA256 string               `json:"published_sha256"`
	Boundary        string               `json:"boundary"`
	NextAction      MigrationNextAction  `json:"next_action"`
}

// MigrationFailureEvidence is appended to durable recovery truth rather than
// being downgraded to a warning.
type MigrationFailureEvidence struct {
	Code           ErrorCode           `json:"code"`
	Step           MigrationStep       `json:"step"`
	Path           string              `json:"path,omitempty"`
	ExpectedSHA256 string              `json:"expected_sha256,omitempty"`
	ActualSHA256   string              `json:"actual_sha256,omitempty"`
	NextAction     MigrationNextAction `json:"next_action"`
	Message        string              `json:"message"`
}

// MigrationRecord is the strict versioned recovery truth under
// .denova-migration/<migration-id>/record.json.
type MigrationRecord struct {
	RecordVersion               int                        `json:"record_version"`
	MigrationID                 string                     `json:"migration_id"`
	WorkspaceKind               WorkspaceKind              `json:"workspace_kind"`
	CanonicalWorkspace          string                     `json:"canonical_workspace"`
	CanonicalSourceRoot         string                     `json:"canonical_source_root"`
	CanonicalTargetRoot         string                     `json:"canonical_target_root"`
	TargetIdentityPath          string                     `json:"target_identity_path"`
	WorkspaceIdentity           FilesystemIdentity         `json:"workspace_identity"`
	SourceRootIdentity          FilesystemIdentity         `json:"source_root_identity"`
	TargetIdentity              FilesystemIdentity         `json:"target_identity"`
	SourceRoot                  string                     `json:"source_root"`
	TargetRoot                  string                     `json:"target_root"`
	PreviewSHA256               string                     `json:"preview_sha256"`
	AuthorizationSHA256         string                     `json:"authorization_sha256"`
	Sources                     []SourceExpectation        `json:"sources"`
	TargetSchemaVersion         int                        `json:"target_schema_version"`
	TargetFeatures              []PreviewFeature           `json:"target_features"`
	Confirmation                ConfirmationBinding        `json:"confirmation"`
	RecoveryAuthorizationSHA256 string                     `json:"recovery_authorization_sha256,omitempty"`
	RecoveryConfirmation        *ConfirmationBinding       `json:"recovery_confirmation,omitempty"`
	State                       MigrationState             `json:"state"`
	RollbackFromState           MigrationState             `json:"rollback_from_state,omitempty"`
	NextAction                  MigrationNextAction        `json:"next_action"`
	Backup                      *MigrationArtifactRef      `json:"backup,omitempty"`
	Stage                       *MigrationArtifactRef      `json:"stage,omitempty"`
	Switch                      *MigrationSwitchIntent     `json:"switch,omitempty"`
	SwitchVisible               bool                       `json:"switch_visible"`
	PublishedEntry              string                     `json:"published_entry,omitempty"`
	PublishedMarkerSHA256       string                     `json:"published_marker_sha256,omitempty"`
	Receipt                     *MigrationArtifactRef      `json:"receipt,omitempty"`
	ExpectedFinalReceiptSHA256  string                     `json:"expected_final_receipt_sha256,omitempty"`
	FinalReceiptDurable         bool                       `json:"final_receipt_durable"`
	RollbackAvailable           bool                       `json:"rollback_available"`
	Failures                    []MigrationFailureEvidence `json:"failures"`
	RecoveryFromState           MigrationState             `json:"recovery_from_state,omitempty"`
}

func newMigrationRecord(preview MigrationPreview, authorization MigrationAuthorization) (MigrationRecord, error) {
	if err := validateMigrationAuthorization(preview, authorization); err != nil {
		return MigrationRecord{}, err
	}
	target, err := ResolveCanonicalPath(preview.Workspace, preview.TargetRoot, CanonicalOptions{AllowMissing: true})
	if err != nil {
		return MigrationRecord{}, migrationRecordError(authorization.MigrationID, MigrationStepValidate, preview.TargetRoot, "target root cannot be canonically pinned", err)
	}
	state := MigrationPreviewed
	next, err := nextActionForState(state)
	if err != nil {
		return MigrationRecord{}, err
	}
	record := MigrationRecord{
		RecordVersion:       MigrationRecordVersionV1,
		MigrationID:         authorization.MigrationID,
		WorkspaceKind:       preview.Kind,
		CanonicalWorkspace:  preview.Workspace,
		CanonicalSourceRoot: authorization.CanonicalSourceRoot,
		CanonicalTargetRoot: target.Absolute,
		TargetIdentityPath:  authorization.TargetIdentityPath,
		WorkspaceIdentity:   authorization.WorkspaceIdentity,
		SourceRootIdentity:  authorization.SourceRootIdentity,
		TargetIdentity:      authorization.TargetIdentity,
		SourceRoot:          preview.SourceRoot,
		TargetRoot:          preview.TargetRoot,
		PreviewSHA256:       authorization.PreviewSHA256,
		AuthorizationSHA256: authorization.PayloadSHA256,
		Sources:             append([]SourceExpectation(nil), authorization.Sources...),
		TargetSchemaVersion: authorization.TargetSchemaVersion,
		TargetFeatures:      append([]PreviewFeature(nil), authorization.TargetFeatures...),
		Confirmation: ConfirmationBinding{
			ID:             authorization.Confirmation.ID,
			EvidenceSHA256: sha256Hex([]byte(authorization.Confirmation.Evidence)),
		},
		State:             state,
		NextAction:        next,
		RollbackAvailable: false,
		Failures:          make([]MigrationFailureEvidence, 0),
	}
	if err := validateMigrationRecord(record); err != nil {
		return MigrationRecord{}, err
	}
	return record, nil
}

func encodeMigrationRecord(record MigrationRecord) ([]byte, error) {
	if err := validateMigrationRecord(record); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "", "migration record cannot be encoded", err)
	}
	return append(raw, '\n'), nil
}

func decodeMigrationRecord(raw []byte) (MigrationRecord, error) {
	if len(raw) == 0 || len(raw) > maxMigrationRecordBytes || !utf8.Valid(raw) {
		return MigrationRecord{}, migrationRecordError("", MigrationStepLoadRecord, "", "migration record is empty, too large, or invalid UTF-8", nil)
	}
	if duplicate, err := validateMarkerJSON(raw); err != nil {
		path := ""
		if duplicate != "" {
			path = duplicate
		}
		return MigrationRecord{}, migrationRecordError("", MigrationStepLoadRecord, path, "migration record is not unambiguous JSON", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var record MigrationRecord
	if err := decoder.Decode(&record); err != nil {
		return MigrationRecord{}, migrationRecordError("", MigrationStepLoadRecord, "", "migration record cannot be decoded exactly", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return MigrationRecord{}, migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "", "migration record contains trailing data", err)
	}
	if err := validateMigrationRecord(record); err != nil {
		return MigrationRecord{}, err
	}
	return record, nil
}

func validateMigrationRecord(record MigrationRecord) error {
	if record.RecordVersion != MigrationRecordVersionV1 {
		return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "record_version", fmt.Sprintf("unknown migration record version %d", record.RecordVersion), nil)
	}
	if err := ValidateMigrationID(record.MigrationID); err != nil {
		return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "migration_id", "migration record contains an invalid migration ID", err)
	}
	if strings.TrimSpace(record.CanonicalWorkspace) == "" || !filepath.IsAbs(record.CanonicalWorkspace) {
		return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "canonical_workspace", "migration record requires an absolute canonical workspace", nil)
	}
	if strings.TrimSpace(record.CanonicalSourceRoot) == "" || strings.TrimSpace(record.CanonicalTargetRoot) == "" || strings.TrimSpace(record.TargetIdentityPath) == "" || !validFilesystemIdentity(record.WorkspaceIdentity) || !validFilesystemIdentity(record.SourceRootIdentity) || !validFilesystemIdentity(record.TargetIdentity) {
		return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "canonical_roots", "migration record requires canonical source and target roots", nil)
	}
	for field, value := range map[string]string{
		"preview_sha256":               record.PreviewSHA256,
		"authorization_sha256":         record.AuthorizationSHA256,
		"confirmation.evidence_sha256": record.Confirmation.EvidenceSHA256,
	} {
		if !validSHA256(value) {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, field, "migration record requires a SHA-256 digest", nil)
		}
	}
	if strings.TrimSpace(record.Confirmation.ID) == "" {
		return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "confirmation.id", "migration record requires author confirmation binding", nil)
	}
	if record.TargetSchemaVersion != 1 {
		return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "target_schema_version", "migration record target schema must be exact v1", nil)
	}
	if !sort.SliceIsSorted(record.Sources, func(i, j int) bool { return record.Sources[i].Path < record.Sources[j].Path }) {
		return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "sources", "source expectations must use stable path order", nil)
	}
	if !sort.SliceIsSorted(record.TargetFeatures, func(i, j int) bool { return record.TargetFeatures[i].ID < record.TargetFeatures[j].ID }) {
		return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "target_features", "target features must use stable ID order", nil)
	}
	next, err := expectedRecordNextAction(record)
	if err != nil {
		return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "state", "migration record contains an unknown state", err)
	}
	if record.NextAction != next {
		return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "next_action", fmt.Sprintf("state %s requires next action %s", record.State, next), nil)
	}
	if err := validateMigrationRecordBoundaries(record); err != nil {
		return err
	}
	return nil
}

func validateMigrationRecordBoundaries(record MigrationRecord) error {
	requireRef := func(ref *MigrationArtifactRef, wantPath, field string) error {
		if ref == nil || ref.RelativePath != wantPath || !validSHA256(ref.SHA256) {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, field, "durable artifact reference is missing or invalid", nil)
		}
		return nil
	}
	requireBackup := func() error {
		if err := requireRef(record.Backup, backupManifestRelativePath(record.MigrationID), "backup"); err != nil {
			return err
		}
		if !record.RollbackAvailable {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "rollback_available", "verified backup must make rollback available", nil)
		}
		return nil
	}
	requireStage := func() error {
		if err := requireBackup(); err != nil {
			return err
		}
		return requireRef(record.Stage, stageManifestRelativePath(record.MigrationID), "stage")
	}
	requireSwitch := func() error {
		if err := requireStage(); err != nil {
			return err
		}
		if record.Switch == nil || record.Switch.BackupManifest != *record.Backup || record.Switch.Stage != *record.Stage || record.Switch.Boundary != SwitchBoundarySameFilesystemNamespaceRename || record.Switch.NextAction != MigrationNextSwitch || !validSHA256(record.Switch.PublishedSHA256) || record.Switch.PublishedEntry == "" {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "switch", "durable switch intent is missing or inconsistent", nil)
		}
		return nil
	}

	for _, source := range record.Sources {
		if !validFilesystemIdentity(source.Identity) || source.Mode > 0o777 || source.Size < 0 {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, source.Path, "source expectation lacks stable identity, mode, or size", nil)
		}
		if source.NodeType == string(PreviewNodeFile) && !validSHA256(source.SHA256) {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, source.Path, "file source expectation lacks SHA-256", nil)
		}
	}

	switch record.State {
	case MigrationNotRequired, MigrationPreviewed, MigrationValidated:
	case MigrationBackedUp:
		return requireBackup()
	case MigrationStaged:
		return requireStage()
	case MigrationSwitchPending:
		return requireSwitch()
	case MigrationSwitched:
		if err := requireSwitch(); err != nil {
			return err
		}
		if !record.SwitchVisible || record.PublishedEntry != record.Switch.PublishedEntry {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "published_entry", "switched state requires a visible published entry", nil)
		}
	case MigrationVerifying:
		if err := requireSwitch(); err != nil {
			return err
		}
		if !record.SwitchVisible || record.PublishedEntry != record.Switch.PublishedEntry {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "published_entry", "verifying state requires the switched entry", nil)
		}
		if record.Receipt != nil {
			if err := requireRef(record.Receipt, receiptRelativePath(record.MigrationID), "receipt"); err != nil {
				return err
			}
			if !validSHA256(record.PublishedMarkerSHA256) {
				return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "published_marker_sha256", "receipt intent requires the precommitted completed marker hash", nil)
			}
		}
		if record.FinalReceiptDurable && (record.Receipt == nil || !validSHA256(record.ExpectedFinalReceiptSHA256) || record.Receipt.SHA256 != record.ExpectedFinalReceiptSHA256) {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "final_receipt_durable", "final receipt flag lacks matching durable hash", nil)
		}
	case MigrationCompleted:
		if err := requireSwitch(); err != nil {
			return err
		}
		if !record.SwitchVisible || record.PublishedEntry != record.Switch.PublishedEntry || !validSHA256(record.PublishedMarkerSHA256) || !record.FinalReceiptDurable || record.Receipt == nil || !validSHA256(record.ExpectedFinalReceiptSHA256) || record.Receipt.SHA256 != record.ExpectedFinalReceiptSHA256 {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "completed", "completed state requires switched marker and terminal receipt evidence", nil)
		}
	case MigrationRollbackPending, MigrationRolledBack:
		if !validSHA256(record.RecoveryAuthorizationSHA256) || record.RecoveryConfirmation == nil || record.RecoveryConfirmation.ID == "" || !validSHA256(record.RecoveryConfirmation.EvidenceSHA256) || record.RollbackFromState == "" || !validMigrationState(record.RollbackFromState) {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "rollback", "rollback state lacks explicit author recovery binding or source state", nil)
		}
	case MigrationNeedsRecovery:
		if record.RecoveryFromState == "" || !validMigrationState(record.RecoveryFromState) || len(record.Failures) == 0 {
			return migrationRecordError(record.MigrationID, MigrationStepLoadRecord, "needs_recovery", "recovery state lacks origin and failure evidence", nil)
		}
	}
	return nil
}

func expectedRecordNextAction(record MigrationRecord) (MigrationNextAction, error) {
	if record.State == MigrationVerifying && record.Receipt != nil {
		return MigrationNextComplete, nil
	}
	return nextActionForState(record.State)
}

func nextActionForState(state MigrationState) (MigrationNextAction, error) {
	switch state {
	case MigrationNotRequired:
		return MigrationNextNone, nil
	case MigrationPreviewed:
		return MigrationNextValidate, nil
	case MigrationValidated:
		return MigrationNextBackup, nil
	case MigrationBackedUp:
		return MigrationNextStage, nil
	case MigrationStaged:
		return MigrationNextPrepareSwitch, nil
	case MigrationSwitchPending:
		return MigrationNextSwitch, nil
	case MigrationSwitched:
		return MigrationNextVerify, nil
	case MigrationVerifying:
		return MigrationNextPublishReceipt, nil
	case MigrationCompleted:
		return MigrationNextNone, nil
	case MigrationRollbackPending:
		return MigrationNextRollback, nil
	case MigrationRolledBack:
		return MigrationNextNone, nil
	case MigrationNeedsRecovery:
		return MigrationNextManualRecovery, nil
	}
	return "", &MigrationError{
		Code:       CodeMigrationRecordInvalid,
		State:      state,
		Step:       MigrationStepLoadRecord,
		Durability: DurabilityNotStarted,
		Recovery:   RecoveryRequired,
		NextAction: MigrationNextManualRecovery,
		Message:    "unknown migration state",
	}
}

func migrationRecordError(migrationID string, step MigrationStep, path, message string, err error) *MigrationError {
	return &MigrationError{
		Code:        CodeMigrationRecordInvalid,
		MigrationID: migrationID,
		Step:        step,
		Path:        path,
		Durability:  DurabilityNotStarted,
		Recovery:    RecoveryRequired,
		NextAction:  MigrationNextManualRecovery,
		Message:     message,
		Err:         err,
	}
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func recordsMatchRequest(record MigrationRecord, preview MigrationPreview, authorization MigrationAuthorization) bool {
	expected, err := newMigrationRecord(preview, authorization)
	if err != nil {
		return false
	}
	return record.MigrationID == expected.MigrationID &&
		record.CanonicalWorkspace == expected.CanonicalWorkspace &&
		record.CanonicalSourceRoot == expected.CanonicalSourceRoot &&
		record.TargetIdentityPath == expected.TargetIdentityPath &&
		record.WorkspaceIdentity == expected.WorkspaceIdentity &&
		record.SourceRootIdentity == expected.SourceRootIdentity &&
		record.TargetIdentity == expected.TargetIdentity &&
		record.PreviewSHA256 == expected.PreviewSHA256 &&
		record.AuthorizationSHA256 == expected.AuthorizationSHA256 &&
		reflect.DeepEqual(record.Sources, expected.Sources) &&
		record.TargetSchemaVersion == expected.TargetSchemaVersion &&
		reflect.DeepEqual(record.TargetFeatures, expected.TargetFeatures) &&
		record.Confirmation == expected.Confirmation
}
