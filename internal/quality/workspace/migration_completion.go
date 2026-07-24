package workspace

import (
	"errors"
	"os"
)

func (executor *MigrationExecutor) completeVerifyingMigration(workspace string, record MigrationRecord, mutated bool) (MigrationResult, error) {
	completedMarkerRaw, err := buildMigrationMarker(record, executor.previewOptions.Inspector.ApplicationVersion, MigrationCompleted)
	if err != nil {
		return migrationResult(record, mutated), err
	}
	expectedCompletedMarkerHash := sha256Hex(completedMarkerRaw)
	if record.PublishedMarkerSHA256 != "" && record.PublishedMarkerSHA256 != expectedCompletedMarkerHash {
		return migrationResult(record, mutated), migrationHashError(record, MigrationStepComplete, MarkerRelativePath, record.PublishedMarkerSHA256, expectedCompletedMarkerHash, "deterministic completed marker no longer matches durable intent")
	}
	record.PublishedMarkerSHA256 = expectedCompletedMarkerHash

	if record.Receipt == nil {
		if err := verifyPublishedDestination(workspace, record, false, false); err != nil {
			return migrationResult(record, mutated), err
		}
		verification, err := verifyWithNormalReader(workspace, record, executor.previewOptions.Inspector, false)
		if err != nil {
			return migrationResult(record, mutated), err
		}
		pendingRaw, err := buildVerifiedPendingReceipt(record, verification)
		if err != nil {
			return migrationResult(record, mutated), err
		}
		ref, receiptWritten, err := publishReceiptBytes(workspace, record, pendingRaw)
		mutated = mutated || receiptWritten
		if err != nil {
			markMigrationErrorMutated(err, mutated)
			return migrationResult(record, mutated), err
		}
		record.Receipt = &ref
		record.NextAction = MigrationNextComplete
		written, err := persistMigrationRecord(workspace, record)
		mutated = mutated || written
		if err != nil {
			return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepPublishReceipt, mutated, err)
		}
		if err := executor.fail(faultAfterReceiptPublication, record, mutated); err != nil {
			return migrationResult(record, mutated), err
		}
	}

	if record.FinalReceiptDurable {
		return executor.finishCompletedRecord(workspace, record, mutated)
	}
	if reconciled, err := reconcileFinalReceiptVisibility(workspace, &record); err != nil {
		return migrationResult(record, mutated), err
	} else if reconciled {
		written, err := persistMigrationRecord(workspace, record)
		mutated = mutated || written
		if err != nil {
			return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepPublishReceipt, mutated, err)
		}
		return executor.finishCompletedRecord(workspace, record, mutated)
	}
	if err := verifyReceiptArtifact(workspace, record); err != nil {
		return migrationResult(record, mutated), err
	}

	stageMarkerHash, err := stagedMarkerHash(workspace, record)
	if err != nil {
		return migrationResult(record, mutated), err
	}
	liveMarkerHash := liveFileHash(workspace, MarkerRelativePath)
	switch liveMarkerHash {
	case stageMarkerHash:
		if err := verifyPublishedDestination(workspace, record, true, false); err != nil {
			return migrationResult(record, mutated), err
		}
		root, err := os.OpenRoot(workspace)
		if err != nil {
			return migrationResult(record, mutated), migrationArtifactError(record, MigrationStepComplete, MarkerRelativePath, "workspace root cannot be opened to complete marker", err)
		}
		markerWritten, err := durableRootWrite(root, workspace, MarkerRelativePath, completedMarkerRaw, 0o600)
		_ = root.Close()
		mutated = mutated || markerWritten
		if err != nil {
			return migrationResult(record, mutated), migrationArtifactError(record, MigrationStepComplete, MarkerRelativePath, "completed marker could not be durably published", err)
		}
	case record.PublishedMarkerSHA256:
		if err := verifyLiveFileHash(workspace, MarkerRelativePath, -1, record.PublishedMarkerSHA256); err != nil {
			return migrationResult(record, mutated), err
		}
	default:
		return migrationResult(record, mutated), migrationHashError(record, MigrationStepComplete, MarkerRelativePath, record.PublishedMarkerSHA256, liveMarkerHash, "live marker matches neither verifying stage nor precommitted completed marker")
	}
	if err := executor.fail(faultAfterCompletedMarkerPublication, record, mutated); err != nil {
		return migrationResult(record, mutated), err
	}

	finalVerification, err := verifyWithNormalReader(workspace, record, executor.previewOptions.Inspector, true)
	if err != nil {
		return migrationResult(record, mutated), err
	}
	finalRaw, err := buildCompletedReceipt(record, finalVerification)
	if err != nil {
		return migrationResult(record, mutated), err
	}
	expectedFinalHash := sha256Hex(finalRaw)
	if record.ExpectedFinalReceiptSHA256 != "" && record.ExpectedFinalReceiptSHA256 != expectedFinalHash {
		return migrationResult(record, mutated), migrationHashError(record, MigrationStepPublishReceipt, receiptRelativePath(record.MigrationID), record.ExpectedFinalReceiptSHA256, expectedFinalHash, "deterministic final receipt changed")
	}
	if record.ExpectedFinalReceiptSHA256 == "" {
		record.ExpectedFinalReceiptSHA256 = expectedFinalHash
		written, err := persistMigrationRecord(workspace, record)
		mutated = mutated || written
		if err != nil {
			return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepPublishReceipt, mutated, err)
		}
	}
	actualReceiptHash := liveFileHash(workspace, receiptRelativePath(record.MigrationID))
	if actualReceiptHash != expectedFinalHash {
		if actualReceiptHash != record.Receipt.SHA256 {
			return migrationResult(record, mutated), migrationHashError(record, MigrationStepPublishReceipt, receiptRelativePath(record.MigrationID), record.Receipt.SHA256, actualReceiptHash, "receipt matches neither pending nor final durable intent")
		}
		ref, receiptWritten, err := publishReceiptBytes(workspace, record, finalRaw)
		mutated = mutated || receiptWritten
		if err != nil {
			return migrationResult(record, mutated), err
		}
		record.Receipt = &ref
	} else {
		record.Receipt = &MigrationArtifactRef{RelativePath: receiptRelativePath(record.MigrationID), SHA256: expectedFinalHash}
	}
	if err := executor.fail(faultAfterFinalReceiptPublication, record, mutated); err != nil {
		return migrationResult(record, mutated), err
	}
	record.FinalReceiptDurable = true
	written, err := persistMigrationRecord(workspace, record)
	mutated = mutated || written
	if err != nil {
		return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepPublishReceipt, mutated, err)
	}
	return executor.finishCompletedRecord(workspace, record, mutated)
}

func (executor *MigrationExecutor) finishCompletedRecord(workspace string, record MigrationRecord, mutated bool) (MigrationResult, error) {
	if !record.FinalReceiptDurable || record.Receipt == nil || record.Receipt.SHA256 != record.ExpectedFinalReceiptSHA256 {
		return migrationResult(record, mutated), migrationArtifactError(record, MigrationStepComplete, receiptRelativePath(record.MigrationID), "terminal receipt is not durably bound before completion", nil)
	}
	if err := verifyReceiptArtifact(workspace, record); err != nil {
		return migrationResult(record, mutated), err
	}
	if err := verifyLiveFileHash(workspace, MarkerRelativePath, -1, record.PublishedMarkerSHA256); err != nil {
		return migrationResult(record, mutated), err
	}
	if _, err := verifyWithNormalReader(workspace, record, executor.previewOptions.Inspector, true); err != nil {
		return migrationResult(record, mutated), err
	}
	if err := setForwardState(&record, MigrationCompleted); err != nil {
		return migrationResult(record, mutated), err
	}
	written, err := persistMigrationRecord(workspace, record)
	mutated = mutated || written
	if err != nil {
		return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepComplete, mutated, err)
	}
	return migrationResult(record, mutated), nil
}

func reconcileFinalReceiptVisibility(workspace string, record *MigrationRecord) (bool, error) {
	if record.ExpectedFinalReceiptSHA256 == "" {
		return false, nil
	}
	actual := liveFileHash(workspace, receiptRelativePath(record.MigrationID))
	if actual == record.Receipt.SHA256 {
		return false, nil
	}
	if actual != record.ExpectedFinalReceiptSHA256 {
		return false, migrationHashError(*record, MigrationStepPublishReceipt, receiptRelativePath(record.MigrationID), record.ExpectedFinalReceiptSHA256, actual, "receipt differs from pending and precommitted final bytes")
	}
	record.Receipt = &MigrationArtifactRef{RelativePath: receiptRelativePath(record.MigrationID), SHA256: actual}
	record.FinalReceiptDurable = true
	return true, nil
}

func stagedMarkerHash(workspace string, record MigrationRecord) (string, error) {
	manifest, err := loadStageManifest(workspace, record)
	if err != nil {
		return "", err
	}
	for _, entry := range manifest.Entries {
		path := entry.Path
		if record.WorkspaceKind == WorkspaceKindCurrent {
			path = MarkerRelativePath
		}
		if path == MarkerRelativePath {
			return entry.SHA256, nil
		}
	}
	return "", errors.New("stage manifest contains no workspace marker")
}
