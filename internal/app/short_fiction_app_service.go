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
)

// ShortFictionAppService binds no-write fiction previews to one workspace snapshot.
type ShortFictionAppService struct {
	app      *App
	openRoot shortFictionRootOpener
}

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
		return config.Config{}, shortfiction.SourcePacket{}, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "base revision does not match the active target", nil)
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
