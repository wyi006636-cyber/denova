package app

import (
	"context"
	"fmt"

	"denova/config"
	"denova/internal/agents/skills"
	writingcomparisonapp "denova/internal/app/writingcomparison"
	projectdomain "denova/internal/project"
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
