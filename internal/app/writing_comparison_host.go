package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"denova/config"
	"denova/internal/agents/skills"
	writingcomparisonapp "denova/internal/app/writingcomparison"
	projectdomain "denova/internal/project"
	"denova/internal/writingtemplates"
)

type writingComparisonHost struct{ app *App }

func (host writingComparisonHost) Config(_ context.Context, layout projectdomain.Layout) (config.Config, error) {
	if host.app == nil || host.app.cfg == nil {
		return config.Config{}, fmt.Errorf("application config is unavailable")
	}
	dataDir := host.app.cfg.DataDir()
	cfg, _, err := config.LoadWithProject(dataDir, layout.ContentRoot, layout.ConfigPath())
	if err != nil {
		return config.Config{}, err
	}
	cfg.ProjectID = layout.ProjectID
	cfg.ProjectStoreDir = layout.StoreRoot
	return *cfg, nil
}

func (host writingComparisonHost) SkillDirectories(_ context.Context, layout projectdomain.Layout) ([]skills.Directory, error) {
	if host.app == nil || host.app.cfg == nil {
		return nil, fmt.Errorf("application config is unavailable")
	}
	return skills.NewDirectories(host.app.cfg.SkillsDir, host.app.cfg.DataDir(), layout.ContentRoot), nil
}

var _ writingcomparisonapp.Host = writingComparisonHost{}

type writingTemplateResolver struct{ catalog *writingtemplates.Catalog }

func (resolver writingTemplateResolver) ResolveCandidateSource(ctx context.Context, ref writingcomparisonapp.WritingSourceRef) (writingcomparisonapp.WritingSourceSnapshot, error) {
	if resolver.catalog == nil {
		return writingcomparisonapp.WritingSourceSnapshot{}, &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeTemplateCatalogUnavailable, Message: "template catalog unavailable"}
	}
	version, err := strconv.Atoi(strings.TrimSpace(ref.Version))
	if err != nil || version <= 0 {
		return writingcomparisonapp.WritingSourceSnapshot{}, &writingcomparisonapp.Error{Code: writingcomparisonapp.CodeTemplateVersionRequired, Message: "template version is required"}
	}
	id := strings.TrimSpace(ref.ID)
	if id == "" {
		id = strings.TrimSpace(ref.Name)
	}
	snapshot, err := resolver.catalog.ResolveForCandidate(ctx, id, version)
	if err != nil {
		code := writingcomparisonapp.CodeTemplateCatalogUnavailable
		switch {
		case errors.Is(err, writingtemplates.ErrNotFound):
			code = writingcomparisonapp.CodeTemplateMissing
		case errors.Is(err, writingtemplates.ErrVersionRequired):
			code = writingcomparisonapp.CodeTemplateVersionRequired
		case errors.Is(err, writingtemplates.ErrVersionChanged):
			code = writingcomparisonapp.CodeTemplateVersionChanged
		case errors.Is(err, writingtemplates.ErrNotUsableForCandidate), errors.Is(err, writingtemplates.ErrCategoryMissing):
			code = writingcomparisonapp.CodeTemplateNotUsable
		}
		return writingcomparisonapp.WritingSourceSnapshot{}, &writingcomparisonapp.Error{Code: code, Message: err.Error(), Cause: err}
	}
	return writingcomparisonapp.WritingSourceSnapshot{
		Kind: snapshot.Kind, ID: snapshot.ID, Version: strconv.Itoa(snapshot.ContentVersion), Scope: snapshot.Scope,
		Title: snapshot.Title, ContentSHA256: writingcomparisonapp.AssetSHA256([]byte(snapshot.Body)), Body: snapshot.Body,
	}, nil
}
