package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"denova/config"
	"denova/internal/agent"
	"denova/internal/book"
	"denova/internal/shortfiction"
	"denova/internal/workspacechange"
)

// ShortFictionAppService binds no-write fiction previews to one workspace snapshot.
type ShortFictionAppService struct {
	app                *App
	openRoot           shortFictionRootOpener
	workspaceChangeFor shortFictionChangeServiceFactory
}

// shortFictionChangeService keeps confirmation dependent only on the one
// mutation operation that must share its lease with the version checkpoint.
type shortFictionChangeService interface {
	ReplaceFileWithConsistentSnapshot(
		context.Context,
		workspacechange.ReplaceFileRequest,
		workspacechange.ConsistentSnapshotFunc,
	) (workspacechange.ChangeSet, error)
}

type shortFictionChangeServiceFactory func(string) (shortFictionChangeService, error)

type shortFictionRoot interface {
	Lstat(string) (os.FileInfo, error)
	Open(string) (*os.File, error)
	OpenRoot(string) (shortFictionRoot, error)
	Close() error
}

type shortFictionRootOpener func(string) (shortFictionRoot, error)

type osShortFictionRoot struct {
	*os.Root
}

func (r *osShortFictionRoot) OpenRoot(name string) (shortFictionRoot, error) {
	child, err := r.Root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return &osShortFictionRoot{Root: child}, nil
}

// GenerateShortFictionCandidate returns a preview candidate without persisting it or mutating the workspace.
func (a *App) GenerateShortFictionCandidate(
	ctx context.Context,
	req shortfiction.GenerateRequest,
	locale string,
) (shortfiction.GeneratedCandidate, error) {
	return a.shortFiction().generateCandidate(ctx, req, locale)
}

// ConfirmShortFictionCandidate writes one integrity-bound candidate after an
// explicit confirmation and checkpoints the exact committed revision.
func (a *App) ConfirmShortFictionCandidate(
	ctx context.Context,
	req shortfiction.ConfirmRequest,
) (shortfiction.ConfirmationResult, error) {
	return a.shortFiction().confirmCandidate(ctx, req)
}

func (s *ShortFictionAppService) confirmCandidate(
	ctx context.Context,
	req shortfiction.ConfirmRequest,
) (shortfiction.ConfirmationResult, error) {
	if s == nil || s.app == nil {
		return shortfiction.ConfirmationResult{}, ErrNoWorkspace
	}
	candidate := req.Candidate
	if err := shortfiction.ValidateCandidate(candidate); err != nil {
		return shortfiction.ConfirmationResult{}, err
	}

	app := s.app
	app.mu.RLock()
	defer app.mu.RUnlock()

	workspace := app.workspace
	bookService := app.bookService
	versionService := app.versionService
	if workspace == "" || bookService == nil || versionService == nil {
		return shortfiction.ConfirmationResult{}, ErrNoWorkspace
	}
	workspaceIdentity, err := os.Lstat(workspace)
	if err != nil {
		return shortfiction.ConfirmationResult{}, err
	}
	if !workspaceIdentity.IsDir() || workspaceIdentity.Mode()&os.ModeSymlink != 0 {
		return shortfiction.ConfirmationResult{}, shortfiction.NewError(
			shortfiction.ErrorCodeInvalidSource,
			"active workspace is not a regular directory",
			map[string]any{"workspace_mutated": false},
		)
	}
	absoluteWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return shortfiction.ConfirmationResult{}, err
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(absoluteWorkspace)
	if err != nil {
		return shortfiction.ConfirmationResult{}, err
	}
	canonicalWorkspace = filepath.Clean(canonicalWorkspace)
	if workspace != canonicalWorkspace || bookService.Workspace() != canonicalWorkspace || candidate.Workspace != canonicalWorkspace {
		return shortfiction.ConfirmationResult{}, shortfiction.NewError(
			shortfiction.ErrorCodeInvalidSource,
			"candidate workspace does not match the active canonical workspace",
			map[string]any{"workspace_mutated": false},
		)
	}
	_, activeRevision, exists, err := readShortFictionSource(
		bookService,
		candidate.TargetPath,
		workspaceIdentity,
		s.shortFictionRootOpener(),
	)
	if err != nil {
		return shortfiction.ConfirmationResult{}, err
	}
	if !exists {
		activeRevision = shortfiction.MissingRevision
	}
	if activeRevision != candidate.BaseRevision {
		return shortfiction.ConfirmationResult{}, &workspacechange.Error{
			Code:    workspacechange.ErrorCodeRevisionConflict,
			Message: "base revision does not match the active target",
			Details: map[string]any{
				"expected_revision": candidate.BaseRevision,
				"actual_revision":   activeRevision,
				"workspace_mutated": false,
			},
		}
	}

	changeService, err := s.workspaceChangeService(workspace)
	if err != nil {
		return shortfiction.ConfirmationResult{}, err
	}
	settings := versionAutoSettingsForConfig(app.cfg)
	var versionResult book.VersionCommandResult
	change, err := changeService.ReplaceFileWithConsistentSnapshot(ctx, workspacechange.ReplaceFileRequest{
		Path:         candidate.TargetPath,
		Content:      candidate.PreviewMarkdown,
		BaseRevision: candidate.BaseRevision,
		Metadata: workspacechange.ChangeMetadata{
			Origin:     workspacechange.OriginAgent,
			AutoAccept: true,
		},
	}, func(applied workspacechange.ChangeSet) error {
		created, createErr := versionService.Create(localizedConfirmationMessage(candidate), book.VersionSourceManual, settings)
		if createErr == nil && created.Version == nil {
			return errors.New("version checkpoint returned no version")
		}
		versionResult = created
		return createErr
	})
	if err != nil {
		if pending := shortFictionDurabilityPendingError(err, change, candidate); pending != nil {
			return shortfiction.ConfirmationResult{}, pending
		}
		if change.ApplyState != workspacechange.ApplyStateApplied || change.ID == "" {
			return shortfiction.ConfirmationResult{}, err
		}
		return shortfiction.ConfirmationResult{
			Status:           shortfiction.ConfirmationWrittenCheckpointFailed,
			CandidateID:      candidate.CandidateID,
			WriteRevision:    change.Revision,
			ChangeGroupID:    change.GroupID,
			ChangeSetID:      change.ID,
			WorkspaceMutated: true,
			CheckpointStatus: shortfiction.CheckpointFailed,
			Retryable:        false,
		}, nil
	}

	return shortfiction.ConfirmationResult{
		Status:           shortfiction.ConfirmationWritten,
		CandidateID:      candidate.CandidateID,
		WriteRevision:    change.Revision,
		ChangeGroupID:    change.GroupID,
		ChangeSetID:      change.ID,
		WorkspaceMutated: true,
		CheckpointStatus: shortfiction.CheckpointCreated,
		Checkpoint: &shortfiction.ConfirmationCheckpoint{
			VersionID: versionResult.Version.ID,
			Source:    versionResult.Version.Source,
			Path:      change.Path,
			Revision:  change.Revision,
		},
		Retryable: false,
	}, nil
}

func shortFictionDurabilityPendingError(
	err error,
	change workspacechange.ChangeSet,
	candidate shortfiction.GeneratedCandidate,
) error {
	var pending *workspacechange.Error
	if !errors.As(err, &pending) || pending.Code != workspacechange.ErrorCodeDurabilityPending || change.ApplyState == workspacechange.ApplyStateApplied {
		return nil
	}
	details := map[string]any{
		"workspace_mutated": pending.Details["workspace_mutated"] == true,
		"recovery_pending":  pending.Details["recovery_pending"] == true,
		"retryable":         false,
		"target_path":       candidate.TargetPath,
	}
	if change.Revision != "" {
		details["write_revision"] = change.Revision
	}
	if change.GroupID != "" {
		details["change_group_id"] = change.GroupID
	}
	if change.ID != "" {
		details["change_set_id"] = change.ID
	}
	return &workspacechange.Error{
		Code:    workspacechange.ErrorCodeDurabilityPending,
		Message: "workspace mutation durability or journal finalization is pending",
		Details: details,
	}
}

func (s *ShortFictionAppService) workspaceChangeService(workspace string) (shortFictionChangeService, error) {
	if s != nil && s.workspaceChangeFor != nil {
		return s.workspaceChangeFor(workspace)
	}
	return workspacechange.ForWorkspace(workspace)
}

func localizedConfirmationMessage(candidate shortfiction.GeneratedCandidate) string {
	if strings.HasPrefix(strings.ToLower(candidate.Locale), "zh") {
		return "确认番茄短篇：" + candidate.TargetPath
	}
	return "Confirm Fanqie short fiction: " + candidate.TargetPath
}

func (s *ShortFictionAppService) generateCandidate(
	ctx context.Context,
	req shortfiction.GenerateRequest,
	locale string,
) (shortfiction.GeneratedCandidate, error) {
	cfg, source, err := s.sourceSnapshot(req, locale)
	if err != nil {
		return shortfiction.GeneratedCandidate{}, err
	}
	generation, err := agent.NewFanqieShortGenerator(&cfg).Generate(ctx, source)
	if err != nil {
		return shortfiction.GeneratedCandidate{}, err
	}
	return shortfiction.NewCandidate(source, generation)
}

func (s *ShortFictionAppService) sourceSnapshot(
	req shortfiction.GenerateRequest,
	locale string,
) (config.Config, shortfiction.SourcePacket, error) {
	if s == nil || s.app == nil {
		return config.Config{}, shortfiction.SourcePacket{}, ErrNoWorkspace
	}
	app := s.app
	app.mu.RLock()
	defer app.mu.RUnlock()

	workspace := app.workspace
	bookService := app.bookService
	if workspace == "" || bookService == nil {
		return config.Config{}, shortfiction.SourcePacket{}, ErrNoWorkspace
	}
	if app.cfg == nil {
		return config.Config{}, shortfiction.SourcePacket{}, fmt.Errorf("runtime config is not initialized")
	}
	workspaceIdentity, err := os.Lstat(workspace)
	if err != nil {
		return config.Config{}, shortfiction.SourcePacket{}, err
	}
	if !workspaceIdentity.IsDir() || workspaceIdentity.Mode()&os.ModeSymlink != 0 {
		return config.Config{}, shortfiction.SourcePacket{}, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "active workspace is not a regular directory", nil)
	}
	absoluteWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return config.Config{}, shortfiction.SourcePacket{}, err
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(absoluteWorkspace)
	if err != nil {
		return config.Config{}, shortfiction.SourcePacket{}, err
	}
	canonicalWorkspace = filepath.Clean(canonicalWorkspace)
	if workspace != canonicalWorkspace || bookService.Workspace() != canonicalWorkspace {
		return config.Config{}, shortfiction.SourcePacket{}, fmt.Errorf("%w: active workspace is not canonical", ErrWorkspaceChanged)
	}
	if req.Source.Workspace != workspace {
		return config.Config{}, shortfiction.SourcePacket{}, fmt.Errorf("%w: expected=%q actual=%q", ErrWorkspaceChanged, req.Source.Workspace, workspace)
	}
	if req.ProfileID != shortfiction.ProfileFanqieShort {
		return config.Config{}, shortfiction.SourcePacket{}, shortfiction.NewError(shortfiction.ErrorCodeInvalidProfile, "generation profile is not supported", nil)
	}

	source := req.Source
	source.Source = ""
	source.Locale = locale
	validated, err := shortfiction.NewCandidate(source, shortfiction.Generation{PreviewMarkdown: "validation"})
	if err != nil {
		return config.Config{}, shortfiction.SourcePacket{}, err
	}
	if validated.TargetPath != req.Source.TargetPath {
		return config.Config{}, shortfiction.SourcePacket{}, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "target path must use its canonical form", nil)
	}
	source = shortfiction.SourcePacket{
		Workspace:    validated.Workspace,
		TargetPath:   validated.TargetPath,
		BaseRevision: validated.BaseRevision,
		Brief:        validated.Brief,
		Locale:       validated.Locale,
	}
	var exists bool
	source.Source, source.BaseRevision, exists, err = readShortFictionSource(bookService, source.TargetPath, workspaceIdentity, s.shortFictionRootOpener())
	if err != nil {
		return config.Config{}, shortfiction.SourcePacket{}, err
	}
	if !exists {
		if req.Source.BaseRevision != shortfiction.MissingRevision {
			return config.Config{}, shortfiction.SourcePacket{}, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "missing target requires the missing revision", nil)
		}
		source.BaseRevision = shortfiction.MissingRevision
	} else if source.BaseRevision != req.Source.BaseRevision {
		return config.Config{}, shortfiction.SourcePacket{}, shortfiction.NewError(
			shortfiction.ErrorCodeRevisionConflict,
			"base revision does not match the active target",
			map[string]any{
				"expected_revision": req.Source.BaseRevision,
				"actual_revision":   source.BaseRevision,
				"workspace_mutated": false,
			},
		)
	}
	if _, err := shortfiction.NewCandidate(source, shortfiction.Generation{PreviewMarkdown: "validation"}); err != nil {
		return config.Config{}, shortfiction.SourcePacket{}, err
	}
	return *app.cfg, source, nil
}

func (s *ShortFictionAppService) shortFictionRootOpener() shortFictionRootOpener {
	if s != nil && s.openRoot != nil {
		return s.openRoot
	}
	return func(workspace string) (shortFictionRoot, error) {
		root, err := os.OpenRoot(workspace)
		if err != nil {
			return nil, err
		}
		return &osShortFictionRoot{Root: root}, nil
	}
}

func readShortFictionSource(
	service *book.Service,
	target string,
	workspaceIdentity os.FileInfo,
	openRoot shortFictionRootOpener,
) (content, revision string, exists bool, err error) {
	root, err := openRoot(service.Workspace())
	if err != nil {
		return "", "", false, err
	}
	openedRoots := []shortFictionRoot{root}
	defer func() {
		for index := len(openedRoots) - 1; index >= 0; index-- {
			_ = openedRoots[index].Close()
		}
	}()

	openedRootIdentity, err := root.Lstat(".")
	if err != nil {
		return "", "", false, err
	}
	if !openedRootIdentity.IsDir() || !os.SameFile(workspaceIdentity, openedRootIdentity) {
		return "", "", false, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "opened workspace identity does not match active workspace", nil)
	}

	components := strings.Split(target, "/")
	currentRoot := root
	for _, component := range components[:len(components)-1] {
		parentIdentity, statErr := currentRoot.Lstat(component)
		if errors.Is(statErr, os.ErrNotExist) {
			return "", "", false, nil
		}
		if statErr != nil {
			return "", "", false, statErr
		}
		if parentIdentity.Mode()&os.ModeSymlink != 0 {
			return "", "", false, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "target path contains a symbolic link", nil)
		}
		if !parentIdentity.IsDir() {
			return "", "", false, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "target parent is not a directory", nil)
		}
		childRoot, openErr := currentRoot.OpenRoot(component)
		if openErr != nil {
			return "", "", false, openErr
		}
		openedRoots = append(openedRoots, childRoot)
		openedChildIdentity, childStatErr := childRoot.Lstat(".")
		if childStatErr != nil {
			return "", "", false, childStatErr
		}
		if !openedChildIdentity.IsDir() || !os.SameFile(parentIdentity, openedChildIdentity) {
			return "", "", false, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "opened parent identity does not match preflight parent", nil)
		}
		currentRoot = childRoot
	}

	basename := components[len(components)-1]
	targetIdentity, err := currentRoot.Lstat(basename)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if targetIdentity.Mode()&os.ModeSymlink != 0 {
		return "", "", false, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "target path contains a symbolic link", nil)
	}
	if !targetIdentity.Mode().IsRegular() {
		return "", "", false, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "target is not a regular file", nil)
	}
	file, err := currentRoot.Open(basename)
	if err != nil {
		return "", "", false, err
	}
	defer file.Close()
	openedTargetIdentity, err := file.Stat()
	if err != nil {
		return "", "", false, err
	}
	if !openedTargetIdentity.Mode().IsRegular() || !os.SameFile(targetIdentity, openedTargetIdentity) {
		return "", "", false, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "opened target identity does not match preflight target", nil)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", "", false, err
	}
	sum := sha256.Sum256(data)
	return string(data), fmt.Sprintf("sha256:%x", sum[:]), true, nil
}
