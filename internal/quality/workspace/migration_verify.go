package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

func verifyWithNormalReader(workspace string, record MigrationRecord, options InspectorOptions, completed bool) (MigrationVerification, error) {
	inspector, err := NewInspector(options)
	if err != nil {
		return MigrationVerification{}, verificationError(record, workspace, "normal Inspector cannot be constructed", err)
	}
	inspection, err := inspector.Inspect(workspace)
	if err != nil {
		return MigrationVerification{}, verificationError(record, workspace, "normal Inspector could not reopen the switched workspace", err)
	}
	if !inspection.CanOpen() || inspection.ActiveRoot != ".denova" || !inspection.Marker.Present || inspection.Marker.Contract.SchemaVersion != 1 {
		return MigrationVerification{}, verificationError(record, MarkerRelativePath, "normal Inspector did not reopen the expected Workspace Schema v1 root", nil)
	}
	if completed {
		if !inspection.CanManagedMutate() || inspection.Marker.Contract.Migration != MigrationCompleted {
			return MigrationVerification{}, verificationError(record, MarkerRelativePath, "completed marker did not pass normal managed-mutation verification", nil)
		}
	} else if inspection.Marker.Contract.Migration != MigrationVerifying || !onlyExpectedMigrationBlocker(inspection.Issues) {
		return MigrationVerification{}, verificationError(record, MarkerRelativePath, "switched marker has blockers beyond its expected verifying state", nil)
	}
	return MigrationVerification{
		Passed:          true,
		Workspace:       inspection.Workspace,
		ActiveRoot:      inspection.ActiveRoot,
		Mode:            inspection.Mode,
		ManagedMutation: inspection.ManagedMutation,
		MarkerSHA256:    sha256Hex(inspection.Marker.RawBytes()),
		ManifestSHA256:  record.Stage.SHA256,
	}, nil
}

func verificationError(record MigrationRecord, path, message string, err error) *MigrationError {
	return &MigrationError{Code: CodeMigrationVerification, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepVerify, Path: path, WorkspaceMutated: true, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextRollback, Message: message, Err: err}
}

func onlyExpectedMigrationBlocker(issues []CompatibilityIssue) bool {
	blocking := 0
	for _, issue := range issues {
		if !issue.Blocking {
			continue
		}
		blocking++
		if issue.Code != CodeMigrationStateIncomplete {
			return false
		}
	}
	return blocking == 1
}

func receiptRelativePath(migrationID string) string {
	return ".denova/quality/migration-receipts/" + migrationID + ".json"
}

func buildVerifiedPendingReceipt(record MigrationRecord, verification MigrationVerification) ([]byte, error) {
	receipt := draftMigrationReceipt(record)
	receipt.State = MigrationVerifying
	receipt.Result = ReceiptVerifiedPendingCompletion
	receipt.StageSHA256 = record.Stage.SHA256
	receipt.ExpectedCompletedMarkerSHA256 = record.PublishedMarkerSHA256
	receipt.Verification = verification
	receipt.Failures = append([]MigrationFailureEvidence(nil), record.Failures...)
	sortMigrationFailures(receipt.Failures)
	return encodeMigrationReceipt(receipt)
}

func buildCompletedReceipt(record MigrationRecord, verification MigrationVerification) ([]byte, error) {
	receipt := draftMigrationReceipt(record)
	receipt.State = MigrationCompleted
	receipt.Result = ReceiptCompleted
	receipt.StageSHA256 = record.Stage.SHA256
	receipt.ExpectedCompletedMarkerSHA256 = record.PublishedMarkerSHA256
	receipt.Verification = verification
	receipt.Failures = append([]MigrationFailureEvidence(nil), record.Failures...)
	sortMigrationFailures(receipt.Failures)
	return encodeMigrationReceipt(receipt)
}

func sortMigrationFailures(failures []MigrationFailureEvidence) {
	sort.Slice(failures, func(i, j int) bool {
		if failures[i].Step != failures[j].Step {
			return failures[i].Step < failures[j].Step
		}
		return failures[i].Path < failures[j].Path
	})
}

func publishReceiptBytes(workspace string, record MigrationRecord, raw []byte) (MigrationArtifactRef, bool, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return MigrationArtifactRef{}, false, migrationArtifactError(record, MigrationStepPublishReceipt, "", "workspace root cannot be opened for receipt", err)
	}
	defer root.Close()
	rel := receiptRelativePath(record.MigrationID)
	if err := ensureRootDirectoryTree(root, workspace, filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel))), 0o700); err != nil {
		return MigrationArtifactRef{}, false, migrationArtifactError(record, MigrationStepPublishReceipt, rel, "receipt parent cannot be durably created", err)
	}
	written, err := durableRootWrite(root, workspace, rel, raw, 0o600)
	if err != nil {
		return MigrationArtifactRef{}, written, migrationArtifactError(record, MigrationStepPublishReceipt, rel, "receipt could not be durably published", err)
	}
	return MigrationArtifactRef{RelativePath: rel, SHA256: sha256Hex(raw)}, written, nil
}

func verifyReceiptArtifact(workspace string, record MigrationRecord) error {
	if record.Receipt == nil || record.Receipt.RelativePath != receiptRelativePath(record.MigrationID) || !validSHA256(record.Receipt.SHA256) {
		return migrationArtifactError(record, MigrationStepPublishReceipt, receiptRelativePath(record.MigrationID), "final receipt reference is missing", nil)
	}
	raw, err := readMigrationArtifact(workspace, record.Receipt.RelativePath, maxMigrationManifestBytes)
	if err != nil {
		return migrationArtifactError(record, MigrationStepPublishReceipt, record.Receipt.RelativePath, "final receipt is missing or unreadable", err)
	}
	actual := sha256Hex(raw)
	if actual != record.Receipt.SHA256 {
		return migrationHashError(record, MigrationStepPublishReceipt, record.Receipt.RelativePath, record.Receipt.SHA256, actual, "final receipt hash differs from durable record")
	}
	receipt, err := decodeMigrationReceipt(raw)
	if err != nil {
		return err
	}
	wantResult := ReceiptVerifiedPendingCompletion
	if record.FinalReceiptDurable || record.State == MigrationCompleted {
		wantResult = ReceiptCompleted
	}
	if receipt.MigrationID != record.MigrationID || receipt.PreviewSHA256 != record.PreviewSHA256 || receipt.BackupSHA256 != record.Backup.SHA256 || receipt.StageSHA256 != record.Stage.SHA256 || receipt.Confirmation != record.Confirmation || receipt.Result != wantResult || !receipt.Verification.Passed || receipt.ExpectedCompletedMarkerSHA256 != record.PublishedMarkerSHA256 {
		return migrationArtifactError(record, MigrationStepPublishReceipt, record.Receipt.RelativePath, "final receipt payload does not match durable migration truth", errors.New("receipt binding mismatch"))
	}
	if wantResult == ReceiptCompleted && receipt.Verification.MarkerSHA256 != record.PublishedMarkerSHA256 {
		return migrationHashError(record, MigrationStepPublishReceipt, record.Receipt.RelativePath, record.PublishedMarkerSHA256, receipt.Verification.MarkerSHA256, "completed receipt verified the wrong marker")
	}
	return nil
}
