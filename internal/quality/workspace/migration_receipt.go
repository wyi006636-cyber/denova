package workspace

import (
	"encoding/json"
	"path/filepath"
	"sort"
)

const MigrationReceiptVersionV1 = 1

// MigrationReceiptResult separates a staged draft from terminal evidence.
type MigrationReceiptResult string

const (
	ReceiptPendingVerification       MigrationReceiptResult = "pending_verification"
	ReceiptVerifiedPendingCompletion MigrationReceiptResult = "verified_pending_completion"
	ReceiptCompleted                 MigrationReceiptResult = "completed"
	ReceiptRolledBack                MigrationReceiptResult = "rolled_back"
	ReceiptNeedsRecovery             MigrationReceiptResult = "needs_recovery"
)

// MigrationVerification records normal-reader evidence rather than trusting a
// stage writer's own output.
type MigrationVerification struct {
	Passed          bool                  `json:"passed"`
	Workspace       string                `json:"workspace"`
	ActiveRoot      string                `json:"active_root"`
	Mode            CompatibilityMode     `json:"mode"`
	ManagedMutation ManagedMutationAccess `json:"managed_mutation"`
	MarkerSHA256    string                `json:"marker_sha256,omitempty"`
	ManifestSHA256  string                `json:"manifest_sha256,omitempty"`
	Failure         string                `json:"failure,omitempty"`
}

// MigrationReceipt is the auditable result under the future/current .denova
// namespace. It expressly limits atomicity to one filesystem entry.
type MigrationReceipt struct {
	RecordVersion                 int                        `json:"record_version"`
	MigrationID                   string                     `json:"migration_id"`
	State                         MigrationState             `json:"state"`
	Result                        MigrationReceiptResult     `json:"result"`
	PreviewSHA256                 string                     `json:"preview_sha256"`
	BackupSHA256                  string                     `json:"backup_sha256"`
	StageSHA256                   string                     `json:"stage_sha256,omitempty"`
	ExpectedCompletedMarkerSHA256 string                     `json:"expected_completed_marker_sha256,omitempty"`
	CanonicalWorkspace            string                     `json:"canonical_workspace"`
	CanonicalSourceRoot           string                     `json:"canonical_source_root"`
	CanonicalTargetRoot           string                     `json:"canonical_target_root"`
	SourceRoot                    string                     `json:"source_root"`
	TargetRoot                    string                     `json:"target_root"`
	Confirmation                  ConfirmationBinding        `json:"confirmation"`
	Verification                  MigrationVerification      `json:"verification"`
	RollbackAvailable             bool                       `json:"rollback_available"`
	AtomicityClaim                string                     `json:"atomicity_claim"`
	DurabilityClaim               string                     `json:"durability_claim"`
	Failures                      []MigrationFailureEvidence `json:"failures"`
}

func draftMigrationReceipt(record MigrationRecord) MigrationReceipt {
	backupHash := ""
	if record.Backup != nil {
		backupHash = record.Backup.SHA256
	}
	return MigrationReceipt{
		RecordVersion:       MigrationReceiptVersionV1,
		MigrationID:         record.MigrationID,
		State:               MigrationVerifying,
		Result:              ReceiptPendingVerification,
		PreviewSHA256:       record.PreviewSHA256,
		BackupSHA256:        backupHash,
		CanonicalWorkspace:  record.CanonicalWorkspace,
		CanonicalSourceRoot: record.CanonicalSourceRoot,
		CanonicalTargetRoot: record.CanonicalTargetRoot,
		SourceRoot:          record.SourceRoot,
		TargetRoot:          record.TargetRoot,
		Confirmation:        record.Confirmation,
		Verification:        MigrationVerification{Passed: false, Workspace: record.CanonicalWorkspace},
		RollbackAvailable:   record.RollbackAvailable,
		AtomicityClaim:      "single_same_filesystem_namespace_entry_only_not_cross_filesystem_or_filesystem_plus_git_acid",
		DurabilityClaim:     platformDurabilityClaim(),
		Failures:            append([]MigrationFailureEvidence(nil), record.Failures...),
	}
}

func encodeMigrationReceipt(receipt MigrationReceipt) ([]byte, error) {
	if err := validateMigrationReceipt(receipt); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func decodeMigrationReceipt(raw []byte) (MigrationReceipt, error) {
	var receipt MigrationReceipt
	if err := decodeStrictMigrationJSON(raw, maxMigrationManifestBytes, &receipt); err != nil {
		return MigrationReceipt{}, &MigrationError{Code: CodeMigrationArtifactInvalid, Step: MigrationStepPublishReceipt, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "migration receipt is not strict unambiguous JSON", Err: err}
	}
	if err := validateMigrationReceipt(receipt); err != nil {
		return MigrationReceipt{}, err
	}
	return receipt, nil
}

func validateMigrationReceipt(receipt MigrationReceipt) error {
	record := MigrationRecord{MigrationID: receipt.MigrationID, State: receipt.State}
	if receipt.RecordVersion != MigrationReceiptVersionV1 || ValidateMigrationID(receipt.MigrationID) != nil {
		return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt", "migration receipt identity or version is invalid", nil)
	}
	if !validSHA256(receipt.PreviewSHA256) || !validSHA256(receipt.BackupSHA256) || !validSHA256(receipt.Confirmation.EvidenceSHA256) || receipt.Confirmation.ID == "" {
		return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt", "migration receipt binding is incomplete", nil)
	}
	if !filepath.IsAbs(receipt.CanonicalWorkspace) || receipt.CanonicalSourceRoot == "" || receipt.CanonicalTargetRoot == "" {
		return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt", "migration receipt canonical roots are incomplete", nil)
	}
	switch receipt.Result {
	case ReceiptPendingVerification:
		if receipt.State != MigrationVerifying || receipt.Verification.Passed {
			return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt.result", "pending receipt cannot claim successful verification", nil)
		}
	case ReceiptVerifiedPendingCompletion:
		if receipt.State != MigrationVerifying || !receipt.Verification.Passed || !validSHA256(receipt.StageSHA256) || !validSHA256(receipt.ExpectedCompletedMarkerSHA256) || !validSHA256(receipt.Verification.MarkerSHA256) {
			return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt.result", "verified-pending receipt requires stage, current-marker, and expected-completed-marker evidence", nil)
		}
	case ReceiptCompleted:
		if receipt.State != MigrationCompleted || !receipt.Verification.Passed || !validSHA256(receipt.StageSHA256) || !validSHA256(receipt.ExpectedCompletedMarkerSHA256) || receipt.Verification.MarkerSHA256 != receipt.ExpectedCompletedMarkerSHA256 {
			return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt.result", "completed receipt requires verified stage evidence", nil)
		}
	case ReceiptRolledBack:
		if receipt.State != MigrationRolledBack {
			return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt.result", "rolled-back receipt has the wrong state", nil)
		}
	case ReceiptNeedsRecovery:
		if receipt.State != MigrationNeedsRecovery {
			return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt.result", "recovery receipt has the wrong state", nil)
		}
	default:
		return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt.result", "migration receipt result is unknown", nil)
	}
	if receipt.AtomicityClaim != "single_same_filesystem_namespace_entry_only_not_cross_filesystem_or_filesystem_plus_git_acid" {
		return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt.atomicity_claim", "migration receipt overstates or changes the atomicity boundary", nil)
	}
	if receipt.DurabilityClaim == "" || !sort.SliceIsSorted(receipt.Failures, func(i, j int) bool {
		if receipt.Failures[i].Step != receipt.Failures[j].Step {
			return receipt.Failures[i].Step < receipt.Failures[j].Step
		}
		return receipt.Failures[i].Path < receipt.Failures[j].Path
	}) {
		return migrationArtifactError(record, MigrationStepPublishReceipt, "receipt", "migration receipt durability or failure evidence is invalid", nil)
	}
	return nil
}
