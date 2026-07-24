package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Rollback records a separate author choice before moving any published or
// staged namespace entry. A crash after rollback_pending is resumed by Resume.
func (executor *MigrationExecutor) Rollback(ctx context.Context, request MigrationRecoveryRequest) (MigrationResult, error) {
	if executor == nil || nilInterface(executor.lease) {
		return MigrationResult{}, &MigrationError{Code: CodeMigrationLeaseRequired, MigrationID: request.Migration.Authorization.MigrationID, Step: MigrationStepAcquireLease, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextNone, Message: "the shared workspace writer lease is required"}
	}
	callbackCalls := 0
	var result MigrationResult
	leaseErr := executor.lease.WithExclusiveWorkspace(ctx, func() error {
		callbackCalls++
		if callbackCalls != 1 {
			return &MigrationError{Code: CodeMigrationLeaseViolation, MigrationID: request.Migration.Authorization.MigrationID, Step: MigrationStepAcquireLease, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "writer lease invoked rollback callback more than once"}
		}
		var err error
		result, err = executor.rollbackUnderLease(request)
		return err
	})
	if callbackCalls != 1 {
		return MigrationResult{}, &MigrationError{Code: CodeMigrationLeaseViolation, MigrationID: request.Migration.Authorization.MigrationID, Step: MigrationStepAcquireLease, WorkspaceMutated: result.WorkspaceMutated, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextNone, Message: fmt.Sprintf("writer lease rollback callback count is %d, want exactly one", callbackCalls), Err: leaseErr}
	}
	if leaseErr != nil {
		return result, leaseErr
	}
	return result, nil
}

func (executor *MigrationExecutor) rollbackUnderLease(request MigrationRecoveryRequest) (MigrationResult, error) {
	migration := request.Migration
	if err := validateMigrationAuthorization(migration.Preview, migration.Authorization); err != nil {
		return MigrationResult{}, err
	}
	if err := validateMigrationRecoveryAuthorization(migration.Authorization, request.Authorization, RecoveryActionRollback); err != nil {
		return MigrationResult{}, err
	}
	record, _, exists, err := loadMigrationRecord(migration.Preview.Workspace, migration.Authorization.MigrationID)
	if err != nil {
		return MigrationResult{}, err
	}
	if !exists {
		return MigrationResult{}, &MigrationError{Code: CodeMigrationRecordInvalid, MigrationID: migration.Authorization.MigrationID, Step: MigrationStepLoadRecord, Path: migrationRecordRelativePath(migration.Authorization.MigrationID), Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "rollback requires an existing durable migration record"}
	}
	if !recordsMatchRequest(record, migration.Preview, migration.Authorization) {
		return migrationResult(record, false), &MigrationError{Code: CodeMigrationRecordConflict, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepLoadRecord, ExpectedSHA256: record.AuthorizationSHA256, ActualSHA256: migration.Authorization.PayloadSHA256, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "rollback request does not match the durable migration"}
	}
	if record.RecoveryAuthorizationSHA256 != "" && record.RecoveryAuthorizationSHA256 != request.Authorization.PayloadSHA256 {
		return migrationResult(record, false), &MigrationError{Code: CodeMigrationRecordConflict, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepAuthorize, ExpectedSHA256: record.RecoveryAuthorizationSHA256, ActualSHA256: request.Authorization.PayloadSHA256, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "migration already has a different durable recovery choice"}
	}
	if record.State == MigrationRolledBack {
		return migrationResult(record, false), nil
	}
	if record.State == MigrationNotRequired {
		return migrationResult(record, false), &MigrationError{Code: CodeMigrationRollbackConflict, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepRollback, Durability: DurabilityDurable, Recovery: RecoveryNotRequired, NextAction: MigrationNextNone, Message: "new-workspace marker publication has no migration backup to roll back"}
	}
	if record.State != MigrationRollbackPending {
		record.RollbackFromState = record.State
		if record.State == MigrationNeedsRecovery && record.RecoveryFromState != "" {
			record.RollbackFromState = record.RecoveryFromState
		}
		record.RecoveryAuthorizationSHA256 = request.Authorization.PayloadSHA256
		record.RecoveryConfirmation = &ConfirmationBinding{ID: request.Authorization.Confirmation.ID, EvidenceSHA256: sha256Hex([]byte(request.Authorization.Confirmation.Evidence))}
		record.State = MigrationRollbackPending
		record.NextAction = MigrationNextRollback
		written, err := persistMigrationRecord(migration.Preview.Workspace, record)
		if err != nil {
			return migrationResult(record, written), migrationStateWriteError(record, MigrationStepRollback, written, err)
		}
		if err := executor.fail(faultAfterRollbackPending, record, true); err != nil {
			return migrationResult(record, true), err
		}
	}
	return executor.executeRollback(migration.Preview.Workspace, record, true)
}

func (executor *MigrationExecutor) executeRollback(workspace string, record MigrationRecord, mutated bool) (MigrationResult, error) {
	if record.Backup != nil {
		if err := verifyBackupArtifact(workspace, record); err != nil {
			return migrationResult(record, mutated), err
		}
	}
	var visible bool
	var err error
	if !record.SwitchVisible && stateBeforeSwitch(record.RollbackFromState) {
		visible, err = rollbackBeforeSwitch(workspace, record)
	} else {
		visible, err = rollbackAfterSwitch(workspace, record, executor.checkpoint)
	}
	mutated = mutated || visible
	if err != nil {
		var checkpointErr *migrationCheckpointError
		if errors.As(err, &checkpointErr) {
			return migrationResult(record, mutated), executor.boundaryError(checkpointErr.point, record, mutated, checkpointErr.err)
		}
		conflict := rollbackConflict(record, err)
		if persistErr := persistNeedsRecovery(workspace, &record, conflict); persistErr != nil {
			return migrationResult(record, true), persistErr
		}
		return migrationResult(record, true), conflict
	}
	if err := executor.fail(faultAfterRollbackVisible, record, mutated); err != nil {
		return migrationResult(record, mutated), err
	}
	record.State = MigrationRolledBack
	record.NextAction = MigrationNextNone
	written, err := persistMigrationRecord(workspace, record)
	mutated = mutated || written
	if err != nil {
		return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepRollback, mutated, err)
	}
	return migrationResult(record, mutated), nil
}

func stateBeforeSwitch(state MigrationState) bool {
	switch state {
	case MigrationPreviewed, MigrationValidated, MigrationBackedUp, MigrationStaged, MigrationSwitchPending:
		return true
	case MigrationSwitched, MigrationVerifying, MigrationCompleted, MigrationNeedsRecovery, MigrationRollbackPending, MigrationRolledBack, MigrationNotRequired:
		return false
	}
	return false
}

func rollbackBeforeSwitch(workspace string, record MigrationRecord) (bool, error) {
	_, liveDestination, _, _ := switchPaths(record)
	if exists, err := migrationPathExists(workspace, liveDestination); err != nil {
		return false, err
	} else if exists {
		return false, fmt.Errorf("live destination %s exists before a pre-switch rollback", liveDestination)
	}
	if record.Stage == nil {
		return false, nil
	}
	if err := verifyStageArtifact(workspace, record); err != nil {
		if ok, quarantineErr := verifyQuarantinedStage(workspace, record); ok && quarantineErr == nil {
			return false, nil
		}
		return false, err
	}
	return quarantineEntry(workspace, record, MigrationRootRelativePath+"/"+record.MigrationID+"/stage", rollbackRoot(record)+"/stage")
}

func rollbackAfterSwitch(workspace string, record MigrationRecord, checkpoint func(migrationFaultPoint) error) (bool, error) {
	if record.WorkspaceKind == WorkspaceKindCurrent {
		return rollbackCurrentAfterSwitch(workspace, record, checkpoint)
	}
	alreadyQuarantined, err := verifyRollbackQuarantine(workspace, record)
	if err != nil {
		return false, err
	}
	if alreadyQuarantined {
		return false, nil
	}
	if err := verifyPublishedDestination(workspace, record, record.Receipt != nil, validSHA256(record.PublishedMarkerSHA256)); err != nil {
		return false, err
	}
	return quarantineEntry(workspace, record, ".denova", rollbackRoot(record)+"/.denova")
}

func rollbackCurrentAfterSwitch(workspace string, record MigrationRecord, checkpoint func(migrationFaultPoint) error) (bool, error) {
	expectedMarker := record.PublishedMarkerSHA256
	if !validSHA256(expectedMarker) {
		manifest, err := loadStageManifest(workspace, record)
		if err != nil {
			return false, err
		}
		expectedMarker = manifest.Entries[0].SHA256
	}
	markerLive, markerQuarantined, err := verifyRollbackFileLocations(workspace, MarkerRelativePath, rollbackRoot(record)+"/published-workspace-schema.json", expectedMarker)
	if err != nil {
		return false, err
	}
	receiptLive := false
	receiptQuarantined := record.Receipt == nil
	if record.Receipt != nil {
		receiptLive, receiptQuarantined, err = verifyRollbackFileLocations(workspace, record.Receipt.RelativePath, rollbackRoot(record)+"/migration-receipt.json", record.Receipt.SHA256)
		if err != nil {
			return false, err
		}
	}
	if markerQuarantined && receiptQuarantined {
		return false, nil
	}
	mutated := false
	if receiptLive {
		visible, err := quarantineFile(workspace, record, record.Receipt.RelativePath, rollbackRoot(record)+"/migration-receipt.json", record.Receipt.SHA256)
		mutated = mutated || visible
		if err != nil {
			return mutated, err
		}
		if checkpoint != nil {
			if err := checkpoint(faultAfterRollbackReceiptQuarantined); err != nil {
				return mutated, &migrationCheckpointError{point: faultAfterRollbackReceiptQuarantined, err: err}
			}
		}
	}
	if markerLive {
		visible, err := quarantineFile(workspace, record, MarkerRelativePath, rollbackRoot(record)+"/published-workspace-schema.json", expectedMarker)
		mutated = mutated || visible
		if err != nil {
			return mutated, err
		}
	}
	backup, err := loadBackupManifest(workspace, record)
	if err != nil {
		return mutated, err
	}
	for _, entry := range backup.Entries {
		if entry.Path != MarkerRelativePath || !entry.Exists {
			continue
		}
		root, err := os.OpenRoot(workspace)
		if err != nil {
			return mutated, err
		}
		written, copyErr := copyVerifiedRootFile(root, workspace, entry.BackupPath, MarkerRelativePath, entry.Size, entry.SHA256, os.FileMode(entry.Mode))
		_ = root.Close()
		return mutated || written, copyErr
	}
	return mutated, nil
}

func verifyRollbackFileLocations(workspace, livePath, quarantinePath, expectedHash string) (live, quarantined bool, err error) {
	live, err = migrationPathExists(workspace, livePath)
	if err != nil {
		return false, false, err
	}
	quarantined, err = migrationPathExists(workspace, quarantinePath)
	if err != nil {
		return false, false, err
	}
	if live == quarantined {
		return live, quarantined, errors.New("rollback file must exist in exactly one live or quarantine location")
	}
	path := quarantinePath
	if live {
		path = livePath
	}
	if err := verifyLiveFileHash(workspace, path, -1, expectedHash); err != nil {
		return live, quarantined, err
	}
	return live, quarantined, nil
}

func rollbackRoot(record MigrationRecord) string {
	return MigrationRootRelativePath + "/" + record.MigrationID + "/rollback"
}

func quarantineEntry(workspace string, record MigrationRecord, source, destination string) (bool, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return false, err
	}
	defer root.Close()
	if err := ensureRootDirectoryTree(root, workspace, filepath.ToSlash(filepath.Dir(filepath.FromSlash(destination))), 0o700); err != nil {
		return false, err
	}
	sourceExists, err := safeEntryExists(root, source)
	if err != nil {
		return false, err
	}
	destinationExists, err := safeEntryExists(root, destination)
	if err != nil {
		return false, err
	}
	if !sourceExists {
		if destinationExists {
			return false, nil
		}
		return false, errors.New("rollback source and quarantine destination are both absent")
	}
	if destinationExists {
		return false, errors.New("rollback source and quarantine destination both exist")
	}
	if err := root.Rename(filepath.FromSlash(source), filepath.FromSlash(destination)); err != nil {
		return false, err
	}
	sourceParent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(source)))
	destinationParent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(destination)))
	if err := syncRootDirectory(root, workspace, destinationParent); err != nil {
		return true, err
	}
	if sourceParent != destinationParent {
		if err := syncRootDirectory(root, workspace, sourceParent); err != nil {
			return true, err
		}
	}
	return true, nil
}

func quarantineFile(workspace string, record MigrationRecord, source, destination, expectedHash string) (bool, error) {
	if exists, _ := migrationPathExists(workspace, source); exists {
		if err := verifyLiveFileHash(workspace, source, -1, expectedHash); err != nil {
			return false, err
		}
	}
	visible, err := quarantineEntry(workspace, record, source, destination)
	if err != nil {
		return visible, err
	}
	if err := verifyLiveFileHash(workspace, destination, -1, expectedHash); err != nil {
		return visible, err
	}
	return visible, nil
}

func verifyQuarantinedStage(workspace string, record MigrationRecord) (bool, error) {
	manifestPath := rollbackRoot(record) + "/stage/manifest.json"
	exists, err := migrationPathExists(workspace, manifestPath)
	if err != nil || !exists {
		return false, err
	}
	if err := verifyLiveFileHash(workspace, manifestPath, -1, record.Stage.SHA256); err != nil {
		return true, err
	}
	raw, err := readMigrationArtifact(workspace, manifestPath, maxMigrationManifestBytes)
	if err != nil {
		return true, err
	}
	manifest, err := decodeStageManifest(raw)
	if err != nil {
		return true, err
	}
	for _, entry := range manifest.Entries {
		path := rollbackRoot(record) + "/stage/" + entry.Path
		if err := verifyLiveFileHash(workspace, path, entry.Size, entry.SHA256); err != nil {
			return true, err
		}
	}
	return true, nil
}

func verifyRollbackQuarantine(workspace string, record MigrationRecord) (bool, error) {
	if record.WorkspaceKind == WorkspaceKindLegacy {
		quarantine := rollbackRoot(record) + "/.denova"
		exists, err := migrationPathExists(workspace, quarantine)
		if err != nil || !exists {
			return false, err
		}
		if live, err := migrationPathExists(workspace, ".denova"); err != nil || live {
			return false, fmt.Errorf("legacy live destination still exists during rollback reconciliation: %w", err)
		}
		return true, verifyQuarantinedLegacyNamespace(workspace, record)
	}
	quarantine := rollbackRoot(record) + "/published-workspace-schema.json"
	exists, err := migrationPathExists(workspace, quarantine)
	if err != nil || !exists {
		return false, err
	}
	expected := record.PublishedMarkerSHA256
	if !validSHA256(expected) {
		manifest, loadErr := loadStageManifest(workspace, record)
		if loadErr != nil {
			return false, loadErr
		}
		expected = manifest.Entries[0].SHA256
	}
	return true, verifyLiveFileHash(workspace, quarantine, -1, expected)
}

func verifyQuarantinedLegacyNamespace(workspace string, record MigrationRecord) error {
	manifest, err := loadStageManifest(workspace, record)
	if err != nil {
		return err
	}
	for _, entry := range manifest.Entries {
		path := rollbackRoot(record) + "/" + entry.Path
		expected := entry.SHA256
		if entry.Path == MarkerRelativePath && validSHA256(record.PublishedMarkerSHA256) {
			expected = record.PublishedMarkerSHA256
		}
		if entry.Path == receiptRelativePath(record.MigrationID) && record.Receipt != nil {
			expected = record.Receipt.SHA256
		}
		if err := verifyLiveFileHash(workspace, path, -1, expected); err != nil {
			return err
		}
	}
	return nil
}

func rollbackConflict(record MigrationRecord, err error) *MigrationError {
	return &MigrationError{Code: CodeMigrationRollbackConflict, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepRollback, Path: record.PublishedEntry, WorkspaceMutated: true, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "rollback compare-and-swap guard found divergent live or quarantine bytes", Err: err}
}
