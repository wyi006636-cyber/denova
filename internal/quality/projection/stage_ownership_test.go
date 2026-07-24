package projection

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEachRebuildUsesAUniqueOwnedSibling(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "unique stage source")
	stages := make([]string, 0, 2)
	service, err := newProjectionTestService(t, Options{
		Workspace: workspace,
		Hooks: Hooks{OnFault: func(point FaultPoint) error {
			if point != FaultAfterConnectionClose {
				return nil
			}
			matches, err := filepath.Glob(filepath.Join(workspace, ".denova", "index.db-rebuild-*.tmp"))
			if err != nil || len(matches) != 1 {
				t.Fatalf("owned sibling matches=%#v err=%v", matches, err)
			}
			stages = append(stages, filepath.Base(matches[0]))
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(stages) != 2 || stages[0] == stages[1] {
		t.Fatalf("stage names = %#v", stages)
	}
	for _, stage := range stages {
		if !strings.HasPrefix(stage, "index.db-rebuild-") || !strings.HasSuffix(stage, ".tmp") {
			t.Fatalf("stage name %q does not use owned format", stage)
		}
	}
	matches, err := filepath.Glob(filepath.Join(workspace, ".denova", "index.db-rebuild-*.tmp*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("owned sibling residue=%#v err=%v", matches, err)
	}
}

func TestActivationQuarantinesLateSQLiteSidecarsWithPriorMain(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "sidecar source truth")
	initial, err := newProjectionTestService(t, Options{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	first, err := initial.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sidecarPath := first.DatabasePath + "-wal"
	sidecarBytes := []byte("late external WAL bytes")
	service, err := newProjectionTestService(t, Options{
		Workspace: workspace,
		Hooks: Hooks{OnFault: func(point FaultPoint) error {
			if point == FaultAfterSourceRecheck {
				return os.WriteFile(sidecarPath, sidecarBytes, 0o600)
			}
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(sidecarPath); !os.IsNotExist(err) {
		t.Fatalf("late sidecar remains beside new main: %v", err)
	}
	found := false
	for _, path := range result.QuarantinePaths {
		content, readErr := os.ReadFile(path)
		if readErr == nil && string(content) == string(sidecarBytes) {
			found = true
		}
	}
	if !found {
		t.Fatalf("sidecar diagnostic not quarantined: %#v", result.QuarantinePaths)
	}
	assertProjectionQueryPath(t, service, "sidecar", "chapters/ch1.md")
}
