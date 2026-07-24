package workspace

import (
	"errors"
	"os"
)

func (executor *MigrationExecutor) publishNewWorkspaceMarker(preview MigrationPreview, record MigrationRecord) (MigrationResult, error) {
	record.State = MigrationNotRequired
	record.NextAction = MigrationNextNone
	record.RollbackAvailable = false
	markerRaw, err := buildMigrationMarker(record, executor.previewOptions.Inspector.ApplicationVersion, MigrationNotRequired)
	if err != nil {
		return migrationResult(record, false), err
	}
	root, err := os.OpenRoot(preview.Workspace)
	if err != nil {
		return migrationResult(record, false), migrationArtifactError(record, MigrationStepComplete, MarkerRelativePath, "workspace root cannot be opened for new marker", err)
	}
	defer root.Close()
	if err := ensureRootDirectoryTree(root, preview.Workspace, ".denova", 0o700); err != nil {
		return migrationResult(record, false), migrationArtifactError(record, MigrationStepComplete, ".denova", "new Workspace Schema root cannot be durably created", err)
	}
	written, err := durableRootWrite(root, preview.Workspace, MarkerRelativePath, markerRaw, 0o600)
	if err != nil {
		return migrationResult(record, true), &MigrationError{Code: CodeMigrationDurability, MigrationID: record.MigrationID, State: MigrationNotRequired, Step: MigrationStepComplete, Path: MarkerRelativePath, WorkspaceMutated: true, Durability: DurabilityPending, Recovery: RecoveryAvailable, NextAction: MigrationNextResume, Message: "new workspace marker publication did not reach a durable boundary", Err: err}
	}
	inspector, err := NewInspector(executor.previewOptions.Inspector)
	if err != nil {
		return migrationResult(record, written), verificationError(record, MarkerRelativePath, "normal Inspector cannot be constructed for the new marker", err)
	}
	inspection, err := inspector.Inspect(preview.Workspace)
	if err != nil || !inspection.CanManagedMutate() || inspection.Marker.Contract.Migration != MigrationNotRequired {
		return migrationResult(record, written), verificationError(record, MarkerRelativePath, "new not_required marker did not pass normal Inspector verification", err)
	}
	return migrationResult(record, written), nil
}

func matchPublishedNewMarker(workspace string, authorization MigrationAuthorization, options InspectorOptions) (MigrationResult, bool, error) {
	raw, err := readMigrationArtifact(workspace, MarkerRelativePath, maxMarkerBytes)
	if errors.Is(err, os.ErrNotExist) {
		return MigrationResult{}, false, nil
	}
	if err != nil {
		return MigrationResult{}, true, &MigrationError{Code: CodeMigrationRecordConflict, MigrationID: authorization.MigrationID, Step: MigrationStepLoadRecord, Path: MarkerRelativePath, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "published new-workspace marker cannot be read", Err: err}
	}
	marker, err := decodeGeneratedMarker(raw)
	if err != nil {
		return MigrationResult{}, true, &MigrationError{Code: CodeMigrationRecordConflict, MigrationID: authorization.MigrationID, Step: MigrationStepLoadRecord, Path: MarkerRelativePath, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "existing marker is not the authorized new-workspace publication", Err: err}
	}
	evidenceHash := sha256Hex([]byte(authorization.Confirmation.Evidence))
	if marker.Migration.State != MigrationNotRequired || marker.Migration.ID != authorization.MigrationID || marker.Migration.PreviewSHA256 != authorization.PreviewSHA256 || marker.Migration.AuthorizationSHA256 != authorization.PayloadSHA256 || marker.Migration.Confirmation.ID != authorization.Confirmation.ID || marker.Migration.Confirmation.EvidenceSHA256 != evidenceHash {
		return MigrationResult{}, true, &MigrationError{Code: CodeMigrationRecordConflict, MigrationID: authorization.MigrationID, State: MigrationNotRequired, Step: MigrationStepLoadRecord, Path: MarkerRelativePath, ExpectedSHA256: marker.Migration.AuthorizationSHA256, ActualSHA256: authorization.PayloadSHA256, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "new-workspace migration ID already belongs to a different authorization"}
	}
	inspector, err := NewInspector(options)
	if err != nil {
		return MigrationResult{}, true, err
	}
	inspection, err := inspector.Inspect(workspace)
	if err != nil || !inspection.CanManagedMutate() || inspection.Marker.Contract.Migration != MigrationNotRequired {
		return MigrationResult{}, true, verificationError(MigrationRecord{MigrationID: authorization.MigrationID, State: MigrationNotRequired}, MarkerRelativePath, "existing new-workspace marker no longer passes normal Inspector verification", err)
	}
	return MigrationResult{MigrationID: authorization.MigrationID, State: MigrationNotRequired, NextAction: MigrationNextNone}, true, nil
}
