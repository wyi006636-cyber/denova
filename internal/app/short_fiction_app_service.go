package app

import (
	"context"
	"errors"
	"fmt"

	"denova/config"
	"denova/internal/agent"
	"denova/internal/shortfiction"
	"denova/internal/workspacechange"
)

// ShortFictionAppService binds no-write fiction previews to one workspace snapshot.
type ShortFictionAppService struct {
	app *App
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
	changeService, err := workspacechange.ForWorkspace(workspace)
	if err != nil {
		return config.Config{}, shortfiction.SourcePacket{}, err
	}
	if changeService.Workspace() != workspace || bookService.Workspace() != workspace {
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
	source = shortfiction.SourcePacket{
		Workspace:    validated.Workspace,
		TargetPath:   validated.TargetPath,
		BaseRevision: validated.BaseRevision,
		Brief:        validated.Brief,
		Locale:       validated.Locale,
	}
	var revision string
	source.Source, revision, err = changeService.ReadFile(source.TargetPath)
	if err != nil {
		var changeErr *workspacechange.Error
		if !errors.As(err, &changeErr) || changeErr.Code != workspacechange.ErrorCodeNotFound || source.BaseRevision != shortfiction.MissingRevision {
			return config.Config{}, shortfiction.SourcePacket{}, err
		}
		source.Source = ""
	} else if revision != source.BaseRevision {
		return config.Config{}, shortfiction.SourcePacket{}, shortfiction.NewError(shortfiction.ErrorCodeInvalidSource, "base revision does not match the active target", nil)
	}
	if _, err := shortfiction.NewCandidate(source, shortfiction.Generation{PreviewMarkdown: "validation"}); err != nil {
		return config.Config{}, shortfiction.SourcePacket{}, err
	}
	return *app.cfg, source, nil
}
