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
	Close() error
}

type shortFictionRootOpener func(string) (shortFictionRoot, error)

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
		return os.OpenRoot(workspace)
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
	defer root.Close()

	openedRootIdentity, err := root.Lstat(".")
	if err != nil {
		return "", "", false, err
	}
	if !openedRootIdentity.IsDir() || !os.SameFile(workspaceIdentity, openedRootIdentity) {
		return "", "", false, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "opened workspace identity does not match active workspace", nil)
	}

	var targetIdentity os.FileInfo
	exists, targetIdentity, err = inspectShortFictionTarget(root, target)
	if err != nil || !exists {
		return "", "", exists, err
	}
	file, err := root.Open(filepath.FromSlash(target))
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

func inspectShortFictionTarget(root shortFictionRoot, target string) (bool, os.FileInfo, error) {
	components := strings.Split(target, "/")
	prefix := ""
	for index, component := range components {
		prefix = filepath.Join(prefix, component)
		info, err := root.Lstat(prefix)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil, nil
		}
		if err != nil {
			return false, nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, nil, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "target path contains a symbolic link", nil)
		}
		if index < len(components)-1 {
			if !info.IsDir() {
				return false, nil, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "target parent is not a directory", nil)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return false, nil, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "target is not a regular file", nil)
		}
		return true, info, nil
	}
	return false, nil, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "target path is empty", nil)
}
