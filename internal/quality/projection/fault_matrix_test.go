package projection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFaultMatrixNeverPublishesPartialAuthorityAndRestartIsDeterministic(t *testing.T) {
	points := []FaultPoint{
		FaultAfterSchema,
		FaultAfterDataWrite,
		FaultBeforeIntegrityCheck,
		FaultAfterIntegrityCheck,
		FaultAfterConnectionClose,
		FaultAfterSourceRecheck,
		FaultAfterVisibleActivation,
		FaultBeforeParentSync,
	}
	for _, point := range points {
		t.Run(string(point), func(t *testing.T) {
			workspace := t.TempDir()
			writeProjectionTestSource(t, workspace, "chapters/ch1.md", "fault matrix source truth")
			authorityBefore := projectionFormalFileHashes(t, workspace, []string{"chapters/ch1.md"})
			injected := errors.New("injected fault matrix boundary")
			calls := 0
			service, err := newProjectionTestService(t, Options{
				Workspace: workspace,
				Hooks: Hooks{OnFault: func(observed FaultPoint) error {
					if observed == point {
						calls++
						return injected
					}
					return nil
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Rebuild(context.Background())
			if !errors.Is(err, injected) || calls != 1 {
				t.Fatalf("fault %s result=%#v calls=%d err=%v", point, result, calls, err)
			}
			var rebuildErr *RebuildError
			if !errors.As(err, &rebuildErr) || rebuildErr.Stage != point {
				t.Fatalf("fault %s rebuild error=%#v", point, rebuildErr)
			}
			if got := projectionFormalFileHashes(t, workspace, []string{"chapters/ch1.md"}); !reflect.DeepEqual(got, authorityBefore) {
				t.Fatalf("fault %s changed authority: before=%#v after=%#v", point, authorityBefore, got)
			}
			assertNoProjectionTestStages(t, workspace)

			restarted, err := newProjectionTestService(t, Options{Workspace: workspace})
			if err != nil {
				t.Fatal(err)
			}
			if point == FaultAfterVisibleActivation || point == FaultBeforeParentSync {
				if !result.Activated {
					t.Fatalf("fault %s did not report visible activation", point)
				}
				status, err := restarted.Inspect(context.Background())
				if err != nil || status.State != StateAvailable {
					t.Fatalf("fault %s restart status=%#v err=%v", point, status, err)
				}
				assertProjectionQueryPath(t, restarted, "matrix", "chapters/ch1.md")
				return
			}
			if result.Activated {
				t.Fatalf("fault %s reported activation before visibility", point)
			}
			if _, err := restarted.Rebuild(context.Background()); err != nil {
				t.Fatalf("fault %s restart rebuild: %v", point, err)
			}
			assertProjectionQueryPath(t, restarted, "matrix", "chapters/ch1.md")
		})
	}
}

func TestRestartDiscardsUntrustedOwnedSiblingAndRebuildsFromTruth(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "orphan cleanup source")
	if err := os.MkdirAll(filepath.Join(workspace, ".denova"), 0o755); err != nil {
		t.Fatal(err)
	}
	stagePath := filepath.Join(workspace, ".denova", projectionStagePrefix+strings.Repeat("a", 32)+projectionStageSuffix)
	if err := os.WriteFile(stagePath, []byte("untrusted partial sqlite bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := newProjectionTestService(t, Options{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild with orphan stage: %v", err)
	}
	if _, err := os.Lstat(stagePath); !os.IsNotExist(err) {
		t.Fatalf("orphan stage remains: %v", err)
	}
	assertProjectionQueryPath(t, service, "orphan", "chapters/ch1.md")
}
