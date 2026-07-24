package projection

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	qualityworkspace "denova/internal/quality/workspace"
)

func TestOpenValidatesTheExactPinnedReaderAfterPathSubstitution(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "current authoritative source")
	initial, err := newProjectionTestService(t, Options{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	result, err := initial.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	replacementWorkspace := t.TempDir()
	writeProjectionTestSource(t, replacementWorkspace, "chapters/ch1.md", "stale substituted source")
	replacementSnapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), replacementWorkspace, qualityworkspace.ProjectionSourceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	replacementPath := filepath.Join(t.TempDir(), "replacement.db")
	if _, err := buildProjectionDatabase(context.Background(), buildRequest{
		Path:            replacementPath,
		Snapshot:        replacementSnapshot,
		BuildIdentity:   BuildIdentityV1,
		FreshActivation: true,
	}); err != nil {
		t.Fatal(err)
	}

	hookCalls := 0
	service, err := newProjectionTestService(t, Options{
		Workspace: workspace,
		Hooks: Hooks{OnBeforeReaderOpen: func() error {
			hookCalls++
			return replaceProjectionFile(replacementPath, result.DatabasePath)
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := service.Open(context.Background())
	if reader != nil {
		reader.Close()
		t.Fatal("Open returned a Reader for the substituted stale database")
	}
	if hookCalls != 1 {
		t.Fatalf("before-reader hook calls = %d", hookCalls)
	}
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || unavailable.Status.State != StateStale || unavailable.Status.Reason != ReasonSourceChanged {
		t.Fatalf("substituted reader error=%T %v status=%#v", err, err, unavailable)
	}
}
