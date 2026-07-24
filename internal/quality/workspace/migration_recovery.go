package workspace

import (
	"errors"
	"os"
	"path/filepath"
)

func migrationPathExists(workspace, rel string) (bool, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return false, err
	}
	defer root.Close()
	return safeEntryExists(root, rel)
}

func persistNeedsRecovery(workspace string, record *MigrationRecord, cause *MigrationError) error {
	if record == nil || cause == nil {
		return errors.New("migration recovery record and cause are required")
	}
	if record.State != MigrationNeedsRecovery {
		record.RecoveryFromState = record.State
	}
	record.Failures = append(record.Failures, MigrationFailureEvidence{
		Code:           cause.Code,
		Step:           cause.Step,
		Path:           cause.Path,
		ExpectedSHA256: cause.ExpectedSHA256,
		ActualSHA256:   cause.ActualSHA256,
		NextAction:     MigrationNextManualRecovery,
		Message:        cause.Message,
	})
	record.State = MigrationNeedsRecovery
	record.NextAction = MigrationNextManualRecovery
	written, err := persistMigrationRecord(workspace, *record)
	if err != nil {
		return &MigrationError{Code: CodeMigrationDurability, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepResume, Path: filepath.ToSlash(migrationRecordRelativePath(record.MigrationID)), WorkspaceMutated: written, Durability: DurabilityPending, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "needs_recovery state could not be durably recorded", Err: err}
	}
	cause.State = MigrationNeedsRecovery
	cause.WorkspaceMutated = true
	cause.NextAction = MigrationNextManualRecovery
	return nil
}

func recordResumeEvidence(workspace string, record *MigrationRecord) (bool, error) {
	if record == nil {
		return false, errors.New("migration record is required")
	}
	switch record.State {
	case MigrationNotRequired, MigrationCompleted, MigrationRolledBack, MigrationNeedsRecovery:
		return false, nil
	case MigrationPreviewed, MigrationValidated, MigrationBackedUp, MigrationStaged, MigrationSwitchPending, MigrationSwitched, MigrationVerifying, MigrationRollbackPending:
	}
	message := "resumed from durable state " + string(record.State)
	for _, failure := range record.Failures {
		if failure.Code == CodeMigrationRecoveryRequired && failure.Step == MigrationStepResume && failure.Message == message {
			return false, nil
		}
	}
	record.Failures = append(record.Failures, MigrationFailureEvidence{Code: CodeMigrationRecoveryRequired, Step: MigrationStepResume, Path: migrationRecordRelativePath(record.MigrationID), NextAction: record.NextAction, Message: message})
	written, err := persistMigrationRecord(workspace, *record)
	if err != nil {
		return written, migrationStateWriteError(*record, MigrationStepResume, written, err)
	}
	return written, nil
}
