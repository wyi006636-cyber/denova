package workspace

import "fmt"

func validateMigrationPins(workspace string, record MigrationRecord) error {
	canonical, err := canonicalWorkspace(workspace)
	if err != nil || canonical != record.CanonicalWorkspace {
		return &MigrationError{Code: CodeMigrationPreflight, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepPreflight, Path: workspace, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "canonical workspace identity changed", Err: err}
	}
	workspaceIdentity, identityErr := pathFilesystemIdentity(canonical)
	if identityErr != nil || workspaceIdentity != record.WorkspaceIdentity {
		return &MigrationError{Code: CodeMigrationPreflight, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepPreflight, Path: canonical, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "workspace root filesystem identity changed", Err: identityErr}
	}
	sourceCanonical := canonical
	if record.SourceRoot != "" {
		source, err := ResolveCanonicalPath(workspace, record.SourceRoot, CanonicalOptions{})
		if err != nil || source.Absolute != record.CanonicalSourceRoot {
			return &MigrationError{Code: CodeMigrationPreflight, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepPreflight, Path: record.SourceRoot, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "canonical source root changed", Err: err}
		}
		sourceCanonical = source.Absolute
	}
	if sourceCanonical != record.CanonicalSourceRoot {
		return &MigrationError{Code: CodeMigrationPreflight, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepPreflight, Path: record.SourceRoot, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "canonical source root no longer matches the authorization"}
	}
	sourceIdentity, identityErr := pathFilesystemIdentity(record.CanonicalSourceRoot)
	if identityErr != nil || sourceIdentity != record.SourceRootIdentity {
		return &MigrationError{Code: CodeMigrationPreflight, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepPreflight, Path: record.CanonicalSourceRoot, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "source root filesystem identity changed", Err: identityErr}
	}
	targetIdentity, identityErr := pathFilesystemIdentity(record.TargetIdentityPath)
	if identityErr != nil || targetIdentity != record.TargetIdentity {
		return &MigrationError{Code: CodeMigrationPreflight, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepPreflight, Path: record.TargetIdentityPath, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "target root anchor filesystem identity changed", Err: identityErr}
	}
	target, err := ResolveCanonicalPath(workspace, record.TargetRoot, CanonicalOptions{AllowMissing: true})
	if err != nil || target.Absolute != record.CanonicalTargetRoot {
		return &MigrationError{Code: CodeMigrationPreflight, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepPreflight, Path: record.TargetRoot, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "canonical target root changed", Err: err}
	}
	if record.WorkspaceKind == WorkspaceKindLegacy && target.Exists && stateBeforeSwitch(record.State) && record.State != MigrationSwitchPending {
		return &MigrationError{Code: CodeMigrationSwitchConflict, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepPreflight, Path: record.TargetRoot, Durability: DurabilityNotStarted, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "legacy migration requires an absent .denova target for a one-entry atomic switch"}
	}
	return nil
}

func setForwardState(record *MigrationRecord, next MigrationState) error {
	allowed := false
	switch record.State {
	case MigrationPreviewed:
		allowed = next == MigrationValidated
	case MigrationValidated:
		allowed = next == MigrationBackedUp
	case MigrationBackedUp:
		allowed = next == MigrationStaged
	case MigrationStaged:
		allowed = next == MigrationSwitchPending
	case MigrationSwitchPending:
		allowed = next == MigrationSwitched
	case MigrationSwitched:
		allowed = next == MigrationVerifying
	case MigrationVerifying:
		allowed = next == MigrationCompleted
	case MigrationNotRequired, MigrationCompleted, MigrationRollbackPending, MigrationRolledBack, MigrationNeedsRecovery:
		allowed = false
	}
	if !allowed {
		return &MigrationError{Code: CodeMigrationRecordInvalid, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepResume, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: fmt.Sprintf("invalid forward state transition %s -> %s", record.State, next)}
	}
	record.State = next
	nextAction, err := nextActionForState(next)
	if err != nil {
		return err
	}
	record.NextAction = nextAction
	return nil
}
