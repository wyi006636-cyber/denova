package workspace

import (
	"context"
	"errors"
	"fmt"
)

type migrationFaultPoint string

const (
	faultAfterPreviewed                  migrationFaultPoint = "after_previewed"
	faultAfterValidated                  migrationFaultPoint = "after_validated"
	checkpointBeforeBackupPostVerify     migrationFaultPoint = "before_backup_post_verify"
	faultAfterBackedUp                   migrationFaultPoint = "after_backed_up"
	faultAfterStaged                     migrationFaultPoint = "after_staged"
	faultAfterSwitchPending              migrationFaultPoint = "after_switch_pending"
	checkpointBeforeSwitchSourceVerify   migrationFaultPoint = "before_switch_source_verify"
	faultAfterVisibleSwitch              migrationFaultPoint = "after_visible_switch"
	faultAfterSwitchParentSync           migrationFaultPoint = "after_switch_parent_sync"
	faultAfterSwitched                   migrationFaultPoint = "after_switched"
	faultAfterVerifying                  migrationFaultPoint = "after_verifying"
	faultAfterReceiptPublication         migrationFaultPoint = "after_receipt_publication"
	faultAfterCompletedMarkerPublication migrationFaultPoint = "after_completed_marker_publication"
	faultAfterFinalReceiptPublication    migrationFaultPoint = "after_final_receipt_publication"
	faultAfterRollbackPending            migrationFaultPoint = "after_rollback_pending"
	faultAfterRollbackReceiptQuarantined migrationFaultPoint = "after_rollback_receipt_quarantined"
	faultAfterRollbackVisible            migrationFaultPoint = "after_rollback_visible"
)

type migrationExecutorDependencies struct {
	fault     func(migrationFaultPoint) error
	preflight func(MigrationPreflightRequest) (MigrationPreflightCapabilities, error)
}

// MigrationExecutor owns the Workspace Schema migration protocol while the
// injected lease owns serialization with every other workspace writer.
type MigrationExecutor struct {
	lease          WorkspaceWriterLease
	previewOptions PreviewOptions
	dependencies   migrationExecutorDependencies
}

// NewMigrationExecutor constructs an API/UI-independent migration boundary.
func NewMigrationExecutor(options MigrationExecutorOptions) (*MigrationExecutor, error) {
	return newMigrationExecutorWithDependencies(options, migrationExecutorDependencies{})
}

func newMigrationExecutorWithDependencies(options MigrationExecutorOptions, dependencies migrationExecutorDependencies) (*MigrationExecutor, error) {
	if nilInterface(options.Lease) {
		return nil, &MigrationError{
			Code:       CodeMigrationLeaseRequired,
			Step:       MigrationStepAcquireLease,
			Durability: DurabilityNotStarted,
			Recovery:   RecoveryNotRequired,
			NextAction: MigrationNextNone,
			Message:    "the shared workspace writer lease is required",
		}
	}
	return &MigrationExecutor{
		lease:          options.Lease,
		previewOptions: options.PreviewOptions,
		dependencies:   dependencies,
	}, nil
}

// Execute starts or idempotently resumes the exact authorized migration.
func (executor *MigrationExecutor) Execute(ctx context.Context, request MigrationRequest) (MigrationResult, error) {
	if executor == nil || nilInterface(executor.lease) {
		return MigrationResult{}, &MigrationError{
			Code:        CodeMigrationLeaseRequired,
			MigrationID: request.Authorization.MigrationID,
			Step:        MigrationStepAcquireLease,
			Durability:  DurabilityNotStarted,
			Recovery:    RecoveryNotRequired,
			NextAction:  MigrationNextNone,
			Message:     "the shared workspace writer lease is required",
		}
	}
	callbackCalls := 0
	var result MigrationResult
	leaseErr := executor.lease.WithExclusiveWorkspace(ctx, func() error {
		callbackCalls++
		if callbackCalls != 1 {
			return &MigrationError{
				Code:        CodeMigrationLeaseViolation,
				MigrationID: request.Authorization.MigrationID,
				Step:        MigrationStepAcquireLease,
				Durability:  DurabilityNotStarted,
				Recovery:    RecoveryRequired,
				NextAction:  MigrationNextManualRecovery,
				Message:     "writer lease invoked its callback more than once",
			}
		}
		var err error
		result, err = executor.executeUnderLease(request)
		return err
	})
	if callbackCalls != 1 {
		return MigrationResult{}, &MigrationError{
			Code:             CodeMigrationLeaseViolation,
			MigrationID:      request.Authorization.MigrationID,
			Step:             MigrationStepAcquireLease,
			WorkspaceMutated: result.WorkspaceMutated,
			Durability:       DurabilityNotStarted,
			Recovery:         RecoveryNotRequired,
			NextAction:       MigrationNextNone,
			Message:          fmt.Sprintf("writer lease callback count is %d, want exactly one", callbackCalls),
			Err:              leaseErr,
		}
	}
	if leaseErr != nil {
		return result, leaseErr
	}
	return result, nil
}

// Resume explicitly continues the same author-bound durable operation under
// the identical shared writer lease used by Execute.
func (executor *MigrationExecutor) Resume(ctx context.Context, request MigrationRequest) (MigrationResult, error) {
	return executor.Execute(ctx, request)
}

func (executor *MigrationExecutor) executeUnderLease(request MigrationRequest) (MigrationResult, error) {
	authorization := request.Authorization
	if err := validateMigrationAuthorization(request.Preview, authorization); err != nil {
		return MigrationResult{}, err
	}
	if request.Preview.Kind == WorkspaceKindNew {
		if result, matched, err := matchPublishedNewMarker(request.Preview.Workspace, authorization, executor.previewOptions.Inspector); matched || err != nil {
			return result, err
		}
	}

	record, _, exists, err := loadMigrationRecord(request.Preview.Workspace, authorization.MigrationID)
	if err != nil {
		return MigrationResult{}, err
	}
	if exists {
		if !recordsMatchRequest(record, request.Preview, authorization) {
			return migrationResult(record, false), &MigrationError{
				Code:           CodeMigrationRecordConflict,
				MigrationID:    authorization.MigrationID,
				State:          record.State,
				Step:           MigrationStepLoadRecord,
				Path:           migrationRecordRelativePath(authorization.MigrationID),
				ExpectedSHA256: record.AuthorizationSHA256,
				ActualSHA256:   authorization.PayloadSHA256,
				Durability:     DurabilityDurable,
				Recovery:       RecoveryRequired,
				NextAction:     MigrationNextManualRecovery,
				Message:        "migration ID already belongs to a different preview or author confirmation",
			}
		}
		if err := validateMigrationPins(request.Preview.Workspace, record); err != nil {
			return migrationResult(record, false), err
		}
		resumeRecorded, err := recordResumeEvidence(request.Preview.Workspace, &record)
		if err != nil {
			return migrationResult(record, resumeRecorded), err
		}
		return executor.runForward(request.Preview, record, resumeRecorded)
	}

	if err := verifyAuthorizationFilesystemBinding(request.Preview, authorization); err != nil {
		return MigrationResult{}, err
	}
	if err := VerifyMigrationPreview(request.Preview); err != nil {
		return MigrationResult{}, migrationPreviewError(authorization.MigrationID, err)
	}
	rebuilt, err := BuildMigrationPreview(request.Preview.Workspace, executor.previewOptions)
	if err != nil {
		return MigrationResult{}, migrationPreviewError(authorization.MigrationID, err)
	}
	if err := rebuilt.RequireConflictFree(); err != nil {
		return MigrationResult{}, &MigrationError{
			Code:        CodeMigrationPreflight,
			MigrationID: authorization.MigrationID,
			Step:        MigrationStepValidate,
			Durability:  DurabilityNotStarted,
			Recovery:    RecoveryNotRequired,
			NextAction:  MigrationNextNone,
			Message:     "migration preview still contains conflicts under the writer lease",
			Err:         err,
		}
	}
	if err := validateMigrationAuthorization(rebuilt, authorization); err != nil {
		return MigrationResult{}, err
	}
	if rebuilt.Kind == WorkspaceKindCurrent && rebuilt.Compatibility.Marker.Present {
		return MigrationResult{}, &MigrationError{Code: CodeMigrationPreflight, MigrationID: authorization.MigrationID, State: rebuilt.Compatibility.Marker.Contract.Migration, Step: MigrationStepValidate, Path: MarkerRelativePath, WorkspaceMutated: false, Durability: DurabilityNotStarted, Recovery: RecoveryNotRequired, NextAction: MigrationNextNone, Message: "workspace already has an exact managed Workspace Schema v1 marker and requires no adoption migration"}
	}
	record, err = newMigrationRecord(rebuilt, authorization)
	if err != nil {
		return MigrationResult{}, err
	}
	if rebuilt.Kind == WorkspaceKindNew {
		return executor.publishNewWorkspaceMarker(rebuilt, record)
	}
	written, err := persistMigrationRecord(rebuilt.Workspace, record)
	if err != nil {
		return MigrationResult{}, &MigrationError{
			Code:             CodeMigrationDurability,
			MigrationID:      authorization.MigrationID,
			State:            MigrationPreviewed,
			Step:             MigrationStepLoadRecord,
			Path:             migrationRecordRelativePath(authorization.MigrationID),
			WorkspaceMutated: written,
			Durability:       DurabilityPending,
			Recovery:         RecoveryRequired,
			NextAction:       MigrationNextResume,
			Message:          "previewed recovery record could not be durably published",
			Err:              err,
		}
	}
	result := migrationResult(record, written)
	if err := executor.fail(faultAfterPreviewed, record, written); err != nil {
		return result, err
	}
	return executor.runForward(rebuilt, record, written)
}

func (executor *MigrationExecutor) runForward(preview MigrationPreview, record MigrationRecord, mutated bool) (MigrationResult, error) {
	for {
		switch record.State {
		case MigrationPreviewed:
			if err := validateMigrationPins(preview.Workspace, record); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyMigrationSources(preview.Workspace, record.MigrationID, record.Sources, record.State, MigrationStepValidate); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := runMigrationPreflight(preview, record, executor.dependencies.preflight); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := setForwardState(&record, MigrationValidated); err != nil {
				return migrationResult(record, mutated), err
			}
			written, err := persistMigrationRecord(preview.Workspace, record)
			mutated = mutated || written
			if err != nil {
				return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepValidate, mutated, err)
			}
			if err := executor.fail(faultAfterValidated, record, mutated); err != nil {
				return migrationResult(record, mutated), err
			}

		case MigrationValidated:
			if err := runMigrationPreflight(preview, record, executor.dependencies.preflight); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyMigrationSources(preview.Workspace, record.MigrationID, record.Sources, record.State, MigrationStepBackup); err != nil {
				return migrationResult(record, mutated), err
			}
			manifest, backupMutated, err := prepareMigrationBackup(preview, record)
			mutated = mutated || backupMutated
			if err != nil {
				markMigrationErrorMutated(err, mutated)
				return migrationResult(record, mutated), err
			}
			if err := executor.checkpoint(checkpointBeforeBackupPostVerify); err != nil {
				return migrationResult(record, mutated), executor.boundaryError(checkpointBeforeBackupPostVerify, record, mutated, err)
			}
			if err := verifyMigrationSources(preview.Workspace, record.MigrationID, record.Sources, record.State, MigrationStepBackup); err != nil {
				markMigrationErrorMutated(err, mutated)
				return migrationResult(record, mutated), err
			}
			ref, manifestWritten, err := publishBackupManifest(preview.Workspace, record, manifest)
			mutated = mutated || manifestWritten
			if err != nil {
				markMigrationErrorMutated(err, mutated)
				return migrationResult(record, mutated), err
			}
			record.Backup = &ref
			record.RollbackAvailable = true
			if err := setForwardState(&record, MigrationBackedUp); err != nil {
				return migrationResult(record, mutated), err
			}
			written, err := persistMigrationRecord(preview.Workspace, record)
			mutated = mutated || written
			if err != nil {
				return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepBackup, mutated, err)
			}
			if err := verifyBackupArtifact(preview.Workspace, record); err != nil {
				markMigrationErrorMutated(err, mutated)
				return migrationResult(record, mutated), err
			}
			if err := executor.fail(faultAfterBackedUp, record, mutated); err != nil {
				return migrationResult(record, mutated), err
			}

		case MigrationBackedUp:
			if err := runMigrationPreflight(preview, record, executor.dependencies.preflight); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyBackupArtifact(preview.Workspace, record); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyMigrationSources(preview.Workspace, record.MigrationID, record.Sources, record.State, MigrationStepStage); err != nil {
				return migrationResult(record, mutated), err
			}
			manifest, stageMutated, err := prepareMigrationStage(preview, record, executor.previewOptions.Inspector.ApplicationVersion)
			mutated = mutated || stageMutated
			if err != nil {
				markMigrationErrorMutated(err, mutated)
				return migrationResult(record, mutated), err
			}
			ref, manifestWritten, err := publishStageManifest(preview.Workspace, record, manifest)
			mutated = mutated || manifestWritten
			if err != nil {
				markMigrationErrorMutated(err, mutated)
				return migrationResult(record, mutated), err
			}
			record.Stage = &ref
			if err := setForwardState(&record, MigrationStaged); err != nil {
				return migrationResult(record, mutated), err
			}
			written, err := persistMigrationRecord(preview.Workspace, record)
			mutated = mutated || written
			if err != nil {
				return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepStage, mutated, err)
			}
			if err := verifyStageArtifact(preview.Workspace, record); err != nil {
				markMigrationErrorMutated(err, mutated)
				return migrationResult(record, mutated), err
			}
			if err := executor.fail(faultAfterStaged, record, mutated); err != nil {
				return migrationResult(record, mutated), err
			}

		case MigrationStaged:
			if err := runMigrationPreflight(preview, record, executor.dependencies.preflight); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyBackupArtifact(preview.Workspace, record); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyStageArtifact(preview.Workspace, record); err != nil {
				return migrationResult(record, mutated), err
			}
			intent, err := buildSwitchIntent(preview.Workspace, record)
			if err != nil {
				return migrationResult(record, mutated), err
			}
			record.Switch = &intent
			if err := setForwardState(&record, MigrationSwitchPending); err != nil {
				return migrationResult(record, mutated), err
			}
			written, err := persistMigrationRecord(preview.Workspace, record)
			mutated = mutated || written
			if err != nil {
				return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepPrepareSwitch, mutated, err)
			}
			if err := executor.fail(faultAfterSwitchPending, record, mutated); err != nil {
				return migrationResult(record, mutated), err
			}

		case MigrationSwitchPending:
			if err := validateMigrationPins(preview.Workspace, record); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := runMigrationPreflight(preview, record, executor.dependencies.preflight); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyBackupArtifact(preview.Workspace, record); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := executor.checkpoint(checkpointBeforeSwitchSourceVerify); err != nil {
				return migrationResult(record, mutated), executor.boundaryError(checkpointBeforeSwitchSourceVerify, record, mutated, err)
			}
			_, destinationRel, _, _ := switchPaths(record)
			destinationExists, err := migrationPathExists(preview.Workspace, destinationRel)
			if err != nil {
				return migrationResult(record, mutated), migrationArtifactError(record, MigrationStepSwitch, destinationRel, "switch destination cannot be inspected", err)
			}
			owned := []string(nil)
			if destinationExists {
				if err := verifyPublishedDestination(preview.Workspace, record, false, false); err != nil {
					conflict := switchConflict(record, destinationRel, err)
					if persistErr := persistNeedsRecovery(preview.Workspace, &record, conflict); persistErr != nil {
						return migrationResult(record, true), persistErr
					}
					return migrationResult(record, true), conflict
				}
				owned = publishedOwnedPaths(record)
				record.SwitchVisible = true
			}
			if err := verifyMigrationSourcesExcluding(preview.Workspace, record.MigrationID, record.Sources, record.State, MigrationStepSwitch, owned); err != nil {
				markMigrationErrorMutated(err, mutated)
				if destinationExists {
					var sourceErr *MigrationError
					if errors.As(err, &sourceErr) {
						if persistErr := persistNeedsRecovery(preview.Workspace, &record, sourceErr); persistErr != nil {
							return migrationResult(record, true), persistErr
						}
						return migrationResult(record, true), sourceErr
					}
				}
				return migrationResult(record, mutated), err
			}
			visible, err := performNamespaceSwitch(preview.Workspace, record, executor.checkpoint)
			mutated = mutated || visible
			record.SwitchVisible = record.SwitchVisible || visible
			if err != nil {
				var checkpointErr *migrationCheckpointError
				if errors.As(err, &checkpointErr) {
					return migrationResult(record, mutated), executor.boundaryError(checkpointErr.point, record, mutated, checkpointErr.err)
				}
				markMigrationErrorMutated(err, mutated)
				return migrationResult(record, mutated), err
			}
			record.PublishedEntry = record.Switch.PublishedEntry
			if err := setForwardState(&record, MigrationSwitched); err != nil {
				return migrationResult(record, mutated), err
			}
			written, err := persistMigrationRecord(preview.Workspace, record)
			mutated = mutated || written
			if err != nil {
				return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepSwitch, mutated, err)
			}
			if err := executor.fail(faultAfterSwitched, record, mutated); err != nil {
				return migrationResult(record, mutated), err
			}

		case MigrationSwitched:
			if err := runMigrationPreflight(preview, record, executor.dependencies.preflight); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyBackupArtifact(preview.Workspace, record); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyPublishedDestination(preview.Workspace, record, false, false); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyMigrationSourcesExcluding(preview.Workspace, record.MigrationID, record.Sources, record.State, MigrationStepVerify, publishedOwnedPaths(record)); err != nil {
				var sourceErr *MigrationError
				if errors.As(err, &sourceErr) {
					if persistErr := persistNeedsRecovery(preview.Workspace, &record, sourceErr); persistErr != nil {
						return migrationResult(record, true), persistErr
					}
					return migrationResult(record, true), sourceErr
				}
				return migrationResult(record, mutated), err
			}
			if err := setForwardState(&record, MigrationVerifying); err != nil {
				return migrationResult(record, mutated), err
			}
			written, err := persistMigrationRecord(preview.Workspace, record)
			mutated = mutated || written
			if err != nil {
				return migrationResult(record, mutated), migrationStateWriteError(record, MigrationStepVerify, mutated, err)
			}
			if err := executor.fail(faultAfterVerifying, record, mutated); err != nil {
				return migrationResult(record, mutated), err
			}

		case MigrationVerifying:
			if err := runMigrationPreflight(preview, record, executor.dependencies.preflight); err != nil {
				return migrationResult(record, mutated), err
			}
			return executor.completeVerifyingMigration(preview.Workspace, record, mutated)

		case MigrationCompleted:
			if err := verifyBackupArtifact(preview.Workspace, record); err != nil {
				return migrationResult(record, mutated), err
			}
			if err := verifyReceiptArtifact(preview.Workspace, record); err != nil {
				return migrationResult(record, mutated), err
			}
			if !validSHA256(record.PublishedMarkerSHA256) {
				return migrationResult(record, mutated), migrationArtifactError(record, MigrationStepVerify, MarkerRelativePath, "completed marker hash is missing", nil)
			}
			if err := verifyLiveFileHash(preview.Workspace, MarkerRelativePath, -1, record.PublishedMarkerSHA256); err != nil {
				return migrationResult(record, mutated), migrationHashError(record, MigrationStepVerify, MarkerRelativePath, record.PublishedMarkerSHA256, liveFileHash(preview.Workspace, MarkerRelativePath), "completed marker differs from durable record")
			}
			if _, err := verifyWithNormalReader(preview.Workspace, record, executor.previewOptions.Inspector, true); err != nil {
				return migrationResult(record, mutated), err
			}
			return migrationResult(record, false), nil

		case MigrationRollbackPending:
			return executor.executeRollback(preview.Workspace, record, mutated)

		case MigrationNotRequired, MigrationRolledBack, MigrationNeedsRecovery:
			return migrationResult(record, mutated), nil
		}
	}
}
