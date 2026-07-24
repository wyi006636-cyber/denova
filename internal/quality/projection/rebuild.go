package projection

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"denova/internal/keyedlock"
	qualityworkspace "denova/internal/quality/workspace"
	"denova/internal/workspacepath"
)

var projectionRebuildLocks = keyedlock.New(nil)

// Service owns Projection rebuild/open behavior for one canonical workspace.
type Service struct {
	workspace     string
	sourceOptions qualityworkspace.ProjectionSourceOptions
	inspector     *qualityworkspace.Inspector
	buildIdentity string
	hooks         Hooks
}

// NewService validates and pins a workspace without creating Projection data.
func NewService(options Options) (*Service, error) {
	if strings.TrimSpace(options.Workspace) == "" {
		return nil, errors.New("Projection workspace is required")
	}
	absolute, err := filepath.Abs(options.Workspace)
	if err != nil {
		return nil, fmt.Errorf("make Projection workspace absolute: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve Projection workspace: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, fmt.Errorf("inspect Projection workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("Projection workspace is not a directory: %s", canonical)
	}
	buildIdentity := strings.TrimSpace(options.BuildIdentity)
	if buildIdentity == "" {
		buildIdentity = BuildIdentityV1
	}
	sourceOptions := options.SourceOptions
	sourceOptions.ApprovedArtifactPaths = append([]string(nil), options.SourceOptions.ApprovedArtifactPaths...)
	inspector, err := qualityworkspace.NewInspector(options.WorkspaceInspector)
	if err != nil {
		return nil, fmt.Errorf("configure Projection Workspace Schema inspector: %w", err)
	}
	return &Service{
		workspace:     filepath.Clean(canonical),
		sourceOptions: sourceOptions,
		inspector:     inspector,
		buildIdentity: buildIdentity,
		hooks:         options.Hooks,
	}, nil
}

// Rebuild creates and validates a complete sibling database, rechecks source
// truth, and then publishes one atomic namespace replacement.
func (service *Service) Rebuild(ctx context.Context) (result RebuildResult, resultErr error) {
	if service == nil {
		return result, errors.New("Projection service is required")
	}
	if ctx == nil {
		return result, errors.New("Projection rebuild context is required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	unlock := projectionRebuildLocks.Lock(service.workspace)
	defer unlock()
	compatibility, err := service.inspectWorkspaceCompatibility()
	if err != nil {
		return result, err
	}
	if compatibility.blocker != nil {
		return result, &UnavailableError{Status: compatibility.status, Err: compatibility.blocker}
	}
	fileLock, err := acquireProjectionFileLock(service.workspace, false)
	if err != nil {
		return result, err
	}
	defer func() {
		if closeErr := fileLock.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("release Projection rebuild lock: %w", closeErr)
		}
	}()
	compatibility, err = service.inspectWorkspaceCompatibility()
	if err != nil {
		return result, err
	}
	if compatibility.blocker != nil {
		return result, &UnavailableError{Status: compatibility.status, Err: compatibility.blocker}
	}

	rootResolution, err := workspacepath.ResolveRoots(service.workspace)
	if err != nil {
		return result, fmt.Errorf("resolve Projection workspace root: %w", err)
	}
	if rootResolution.ActiveRoot != workspacepath.DataDirName {
		return result, ErrLegacyActive
	}
	snapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(ctx, service.workspace, service.sourceOptions)
	if err != nil {
		return result, err
	}
	result.SourceSnapshotHash = snapshot.Hash
	result.DocumentCount = len(snapshot.Documents)
	result.DatabasePath = filepath.Join(service.workspace, filepath.FromSlash(DatabaseRelativePath))

	status, err := service.inspectWithSnapshot(ctx, snapshot)
	if err != nil {
		return result, err
	}
	result.Fresh = status.Reason == ReasonMissing
	if status.Reason == ReasonLocked {
		return result, &UnavailableError{Status: status}
	}
	quarantineRequired := status.State == StateUnavailable && status.Reason != ReasonMissing

	dataRoot, err := openProjectionDataRoot(service.workspace)
	if err != nil {
		return result, err
	}
	defer func() {
		if !result.Activated {
			if cleanupErr := dataRoot.cleanupStage(); resultErr == nil && cleanupErr != nil {
				resultErr = cleanupErr
			}
		}
		if closeErr := dataRoot.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("close Projection data roots: %w", closeErr)
		}
	}()
	if err := dataRoot.cleanupStage(); err != nil {
		return result, err
	}
	stageName, err := dataRoot.newStageName()
	if err != nil {
		return result, err
	}
	stagePath := filepath.Join(dataRoot.path, stageName)
	evidence, err := buildProjectionDatabase(ctx, buildRequest{
		Path:            stagePath,
		Snapshot:        snapshot,
		BuildIdentity:   service.buildIdentity,
		FreshActivation: true,
		Hooks:           service.hooks,
	})
	result.SQLiteVersion = evidence.SQLiteVersion
	if err != nil {
		stage := FaultAfterIntegrityCheck
		var stageErr *buildStageError
		if errors.As(err, &stageErr) {
			stage = stageErr.stage
		}
		return result, rebuildError(stage, false, err)
	}
	if err := service.hooks.fault(FaultAfterConnectionClose); err != nil {
		return result, rebuildError(FaultAfterConnectionClose, false, err)
	}
	_, err = dataRoot.syncStage(stageName)
	if err != nil {
		return result, rebuildError(FaultAfterConnectionClose, false, err)
	}
	if err := dataRoot.verify(); err != nil {
		return result, rebuildError(FaultAfterConnectionClose, false, err)
	}
	if err := validateProjectionStage(ctx, stagePath, service.buildIdentity, snapshot, service.sourceOptions.Limits, service.hooks); err != nil {
		return result, rebuildError(FaultAfterConnectionClose, false, err)
	}
	stageInfo, err := dataRoot.syncStage(stageName)
	if err != nil {
		return result, rebuildError(FaultAfterConnectionClose, false, err)
	}
	rechecked, err := qualityworkspace.BuildProjectionSourceSnapshot(ctx, service.workspace, service.sourceOptions)
	if err != nil {
		return result, rebuildError(FaultAfterSourceRecheck, false, err)
	}
	if rechecked.Hash != snapshot.Hash {
		return result, rebuildError(FaultAfterSourceRecheck, false, ErrSourceChanged)
	}
	if err := service.hooks.fault(FaultAfterSourceRecheck); err != nil {
		return result, rebuildError(FaultAfterSourceRecheck, false, err)
	}
	finalRecheck, err := qualityworkspace.BuildProjectionSourceSnapshot(ctx, service.workspace, service.sourceOptions)
	if err != nil {
		return result, rebuildError(FaultAfterSourceRecheck, false, err)
	}
	if finalRecheck.Hash != snapshot.Hash {
		return result, rebuildError(FaultAfterSourceRecheck, false, ErrSourceChanged)
	}
	quarantineReason := status.Reason
	sidecars, sidecarErr := dataRoot.existingFinalSidecars()
	if sidecarErr != nil || len(sidecars) != 0 {
		quarantineRequired = true
		quarantineReason = ReasonIntegrityFailed
	}
	if quarantineRequired {
		result.QuarantinePaths, err = dataRoot.quarantineProjection(quarantineReason, service.hooks)
		if err != nil {
			return result, rebuildError(FaultAfterSourceRecheck, false, errors.Join(sidecarErr, err))
		}
	}
	visible, activationQuarantinePaths, err := dataRoot.activate(stageName, stageInfo, service.hooks)
	result.QuarantinePaths = append(result.QuarantinePaths, activationQuarantinePaths...)
	if visible {
		result.Activated = true
	}
	if err != nil {
		return result, rebuildError(FaultAfterVisibleActivation, visible, err)
	}
	visibleHookErr := service.hooks.fault(FaultAfterVisibleActivation)
	lateQuarantinePaths, sidecarErr := dataRoot.quarantineVisibleSidecars(service.hooks)
	result.QuarantinePaths = append(result.QuarantinePaths, lateQuarantinePaths...)
	if visibleHookErr != nil || sidecarErr != nil {
		return result, rebuildError(FaultAfterVisibleActivation, true, errors.Join(visibleHookErr, sidecarErr))
	}
	parentHookErr := service.hooks.fault(FaultBeforeParentSync)
	lateQuarantinePaths, sidecarErr = dataRoot.quarantineVisibleSidecars(service.hooks)
	result.QuarantinePaths = append(result.QuarantinePaths, lateQuarantinePaths...)
	if parentHookErr != nil || sidecarErr != nil {
		return result, rebuildError(FaultBeforeParentSync, true, errors.Join(parentHookErr, sidecarErr))
	}
	if err := syncProjectionDirectory(dataRoot.path, dataRoot.identity); err != nil {
		return result, rebuildError(FaultBeforeParentSync, true, err)
	}
	result.ParentSynced = true
	activatedSource, err := qualityworkspace.BuildProjectionSourceSnapshot(ctx, service.workspace, service.sourceOptions)
	if err != nil {
		return result, rebuildError(FaultAfterVisibleActivation, true, err)
	}
	if activatedSource.Hash != snapshot.Hash {
		return result, rebuildError(FaultAfterVisibleActivation, true, ErrSourceChanged)
	}
	return result, nil
}

// Open validates source freshness and database integrity before returning a
// read-only query handle.
func (service *Service) Open(ctx context.Context) (*Reader, error) {
	if service == nil {
		return nil, errors.New("Projection service is required")
	}
	if ctx == nil {
		return nil, errors.New("Projection reader context is required")
	}
	compatibility, err := service.inspectWorkspaceCompatibility()
	if err != nil {
		return nil, err
	}
	if compatibility.blocker != nil {
		return nil, &UnavailableError{Status: compatibility.status, Err: compatibility.blocker}
	}
	snapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(ctx, service.workspace, service.sourceOptions)
	if err != nil {
		return nil, err
	}
	if err := service.hooks.beforeReaderOpen(); err != nil {
		return nil, fmt.Errorf("before Projection reader open: %w", err)
	}
	databasePath := filepath.Join(service.workspace, filepath.FromSlash(DatabaseRelativePath))
	reader, err := openProjectionReaderCandidate(ctx, databasePath, "rw")
	if err != nil {
		status := Status{State: StateUnavailable, Reason: ReasonOpenFailed, DatabasePath: databasePath, SourceSnapshotHash: snapshot.Hash}
		if errors.Is(err, os.ErrNotExist) {
			status.Reason = ReasonMissing
			status.Detail = boundedProjectionDetail(err)
		} else {
			status = classifiedProjectionStatus(status, err)
		}
		return nil, &UnavailableError{Status: status, Err: err}
	}
	validationErr := validateProjectionConnection(ctx, reader.connection, databasePath, reader.identity, service.buildIdentity, snapshot, service.sourceOptions.Limits, service.hooks)
	if validationErr != nil {
		reader.Close()
		status := Status{State: StateUnavailable, Reason: ReasonIntegrityFailed, DatabasePath: databasePath, SourceSnapshotHash: snapshot.Hash, Detail: boundedProjectionDetail(validationErr)}
		if errors.Is(validationErr, ErrSourceChanged) {
			status.State = StateStale
			status.Reason = ReasonSourceChanged
		}
		return nil, &UnavailableError{Status: status, Err: validationErr}
	}
	rechecked, err := qualityworkspace.BuildProjectionSourceSnapshot(ctx, service.workspace, service.sourceOptions)
	if err != nil {
		reader.Close()
		return nil, err
	}
	if rechecked.Hash != snapshot.Hash {
		reader.Close()
		status := Status{State: StateStale, Reason: ReasonSourceChanged, DatabasePath: databasePath, SourceSnapshotHash: rechecked.Hash, ProjectionSnapshotHash: snapshot.Hash}
		return nil, &UnavailableError{Status: status, Err: ErrSourceChanged}
	}
	if err := reader.enableQueryOnly(ctx); err != nil {
		reader.Close()
		return nil, err
	}
	return reader, nil
}

func rebuildError(stage FaultPoint, activated bool, err error) error {
	return &RebuildError{Stage: stage, Activated: activated, Err: err}
}
