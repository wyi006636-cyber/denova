package workspace

import (
	"fmt"
	"reflect"
)

func (executor *MigrationExecutor) fail(point migrationFaultPoint, record MigrationRecord, mutated bool) error {
	err := executor.checkpoint(point)
	if err == nil {
		return nil
	}
	return executor.boundaryError(point, record, mutated, err)
}

func (executor *MigrationExecutor) checkpoint(point migrationFaultPoint) error {
	if executor.dependencies.fault == nil {
		return nil
	}
	return executor.dependencies.fault(point)
}

func (executor *MigrationExecutor) boundaryError(point migrationFaultPoint, record MigrationRecord, mutated bool, err error) *MigrationError {
	return &MigrationError{
		Code:             CodeMigrationDurability,
		MigrationID:      record.MigrationID,
		State:            record.State,
		Step:             MigrationStepResume,
		Path:             migrationRecordRelativePath(record.MigrationID),
		WorkspaceMutated: mutated,
		Durability:       DurabilityDurable,
		Recovery:         RecoveryAvailable,
		NextAction:       record.NextAction,
		Message:          fmt.Sprintf("injected failure at durable boundary %s", point),
		Err:              err,
	}
}

func migrationResult(record MigrationRecord, mutated bool) MigrationResult {
	var receipt *MigrationArtifactRef
	if record.Receipt != nil && record.State != MigrationRolledBack {
		copy := *record.Receipt
		receipt = &copy
	}
	return MigrationResult{
		MigrationID:       record.MigrationID,
		State:             record.State,
		NextAction:        record.NextAction,
		WorkspaceMutated:  mutated,
		RollbackAvailable: record.RollbackAvailable,
		Receipt:           receipt,
	}
}

func migrationPreviewError(migrationID string, err error) *MigrationError {
	return &MigrationError{
		Code:        CodeMigrationSourceChanged,
		MigrationID: migrationID,
		Step:        MigrationStepValidate,
		Durability:  DurabilityNotStarted,
		Recovery:    RecoveryNotRequired,
		NextAction:  MigrationNextNone,
		Message:     "preview or source identity changed before the first write",
		Err:         err,
	}
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	}
	return false
}

func migrationStateWriteError(record MigrationRecord, step MigrationStep, mutated bool, err error) *MigrationError {
	return &MigrationError{Code: CodeMigrationDurability, MigrationID: record.MigrationID, State: record.State, Step: step, Path: migrationRecordRelativePath(record.MigrationID), WorkspaceMutated: mutated, Durability: DurabilityPending, Recovery: RecoveryRequired, NextAction: MigrationNextResume, Message: "migration state could not be durably advanced", Err: err}
}

func markMigrationErrorMutated(err error, mutated bool) {
	if migrationErr, ok := err.(*MigrationError); ok {
		migrationErr.WorkspaceMutated = migrationErr.WorkspaceMutated || mutated
	}
}
