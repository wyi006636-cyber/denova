package projection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSourceChangeAfterSnapshotPreventsActivationAndPreservesAuthorBytes(t *testing.T) {
	workspace := t.TempDir()
	chapterPath := filepath.Join(workspace, "chapters", "ch1.md")
	writeProjectionTestSource(t, workspace, "ideas.md", "unchanged direction")
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "initial indexed chapter")

	initialService, err := newProjectionTestService(t, Options{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := initialService.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	initialDatabaseBytes, err := os.ReadFile(initial.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(chapterPath, []byte("snapshot input chapter"), 0o644); err != nil {
		t.Fatal(err)
	}
	otherBefore := projectionFormalFileHashes(t, workspace, []string{"ideas.md"})
	latestAuthorBytes := []byte("author edit after snapshot")
	hookCalls := 0
	service, err := newProjectionTestService(t, Options{
		Workspace: workspace,
		Hooks: Hooks{OnFault: func(point FaultPoint) error {
			if point != FaultAfterConnectionClose {
				return nil
			}
			hookCalls++
			return os.WriteFile(chapterPath, latestAuthorBytes, 0o644)
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Rebuild(context.Background())
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("Rebuild error = %v, want ErrSourceChanged", err)
	}
	if result.Activated || hookCalls != 1 {
		t.Fatalf("source-CAS rebuild result=%#v hook_calls=%d", result, hookCalls)
	}
	currentDatabaseBytes, readErr := os.ReadFile(initial.DatabasePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(currentDatabaseBytes, initialDatabaseBytes) {
		t.Fatal("source-CAS failure replaced the prior visible Projection")
	}
	chapterBytes, readErr := os.ReadFile(chapterPath)
	if readErr != nil || !reflect.DeepEqual(chapterBytes, latestAuthorBytes) {
		t.Fatalf("author bytes = %q err=%v", chapterBytes, readErr)
	}
	if got := projectionFormalFileHashes(t, workspace, []string{"ideas.md"}); !reflect.DeepEqual(got, otherBefore) {
		t.Fatalf("unrelated formal bytes changed: before=%#v after=%#v", otherBefore, got)
	}
	assertNoProjectionTestStages(t, workspace)
}

func TestSourceChangeAfterSuccessfulRecheckCannotPublishAsCurrent(t *testing.T) {
	workspace := t.TempDir()
	chapterPath := filepath.Join(workspace, "chapters", "ch1.md")
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "source before final compare")
	latest := []byte("author edit after successful recheck")
	service, err := newProjectionTestService(t, Options{
		Workspace: workspace,
		Hooks: Hooks{OnFault: func(point FaultPoint) error {
			if point == FaultAfterSourceRecheck {
				return os.WriteFile(chapterPath, latest, 0o644)
			}
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Rebuild(context.Background())
	if !errors.Is(err, ErrSourceChanged) || result.Activated {
		t.Fatalf("post-recheck edit result=%#v err=%v", result, err)
	}
	current, readErr := os.ReadFile(chapterPath)
	if readErr != nil || !reflect.DeepEqual(current, latest) {
		t.Fatalf("author edit=%q err=%v", current, readErr)
	}
}

func TestSourceChangeAfterVisibleActivationIsDurableAndReportedStale(t *testing.T) {
	workspace := t.TempDir()
	chapterPath := filepath.Join(workspace, "chapters", "ch1.md")
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "source at activation")
	latest := []byte("author edit after visible activation")
	service, err := newProjectionTestService(t, Options{
		Workspace: workspace,
		Hooks: Hooks{OnFault: func(point FaultPoint) error {
			if point == FaultAfterVisibleActivation {
				return os.WriteFile(chapterPath, latest, 0o644)
			}
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Rebuild(context.Background())
	if !errors.Is(err, ErrSourceChanged) || !result.Activated || !result.ParentSynced {
		t.Fatalf("post-activation edit result=%#v err=%v", result, err)
	}
	status, inspectErr := service.Inspect(context.Background())
	if inspectErr != nil || status.State != StateStale || status.Reason != ReasonSourceChanged {
		t.Fatalf("post-activation status=%#v err=%v", status, inspectErr)
	}
	current, readErr := os.ReadFile(chapterPath)
	if readErr != nil || !reflect.DeepEqual(current, latest) {
		t.Fatalf("author edit=%q err=%v", current, readErr)
	}
}
