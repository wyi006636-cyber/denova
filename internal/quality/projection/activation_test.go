package projection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRebuildOrdersFaultBoundariesAroundClosedSiblingAndActivation(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "closed sibling activation")
	finalPath := filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath))
	var points []FaultPoint
	service, err := newProjectionTestService(t, Options{
		Workspace: workspace,
		Hooks: Hooks{OnFault: func(point FaultPoint) error {
			points = append(points, point)
			switch point {
			case FaultAfterConnectionClose, FaultAfterSourceRecheck:
				stagePath := currentProjectionTestStagePath(t, workspace)
				if info, err := os.Lstat(stagePath); err != nil || !info.Mode().IsRegular() {
					t.Fatalf("stage at %s: info=%v err=%v", point, info, err)
				}
			case FaultAfterVisibleActivation, FaultBeforeParentSync:
				if info, err := os.Lstat(finalPath); err != nil || !info.Mode().IsRegular() {
					t.Fatalf("final at %s: info=%v err=%v", point, info, err)
				}
				assertNoProjectionTestStages(t, workspace)
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
	if !result.Activated || !result.ParentSynced {
		t.Fatalf("rebuild result = %#v", result)
	}
	want := []FaultPoint{
		FaultAfterSchema,
		FaultAfterDataWrite,
		FaultBeforeIntegrityCheck,
		FaultAfterIntegrityCheck,
		FaultAfterConnectionClose,
		FaultAfterSourceRecheck,
		FaultAfterVisibleActivation,
		FaultBeforeParentSync,
	}
	if !reflect.DeepEqual(points, want) {
		t.Fatalf("fault order:\n got: %#v\nwant: %#v", points, want)
	}
}

func TestRebuildFailureAfterVisibleActivationLeavesCompleteRecoverableProjection(t *testing.T) {
	for _, faultPoint := range []FaultPoint{FaultAfterVisibleActivation, FaultBeforeParentSync} {
		t.Run(string(faultPoint), func(t *testing.T) {
			workspace := t.TempDir()
			writeProjectionTestSource(t, workspace, "chapters/ch1.md", "visible complete projection")
			injected := errors.New("activation boundary failure")
			service, err := newProjectionTestService(t, Options{
				Workspace: workspace,
				Hooks: Hooks{OnFault: func(point FaultPoint) error {
					if point == faultPoint {
						return injected
					}
					return nil
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Rebuild(context.Background())
			if !errors.Is(err, injected) || !result.Activated {
				t.Fatalf("Rebuild result=%#v err=%v", result, err)
			}
			assertNoProjectionTestStages(t, workspace)

			restarted, err := newProjectionTestService(t, Options{Workspace: workspace})
			if err != nil {
				t.Fatal(err)
			}
			reader, err := restarted.Open(context.Background())
			if err != nil {
				t.Fatalf("restart Open: %v", err)
			}
			response, err := reader.Query(context.Background(), QueryRequest{Text: "complete"})
			reader.Close()
			if err != nil || len(response.Results) != 1 {
				t.Fatalf("activated Projection query=%#v err=%v", response, err)
			}
		})
	}
}

func TestRebuildFailureBeforeVisibleActivationCanDiscardAndRetry(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "retry from source truth")
	injected := errors.New("before activation")
	service, err := newProjectionTestService(t, Options{
		Workspace: workspace,
		Hooks: Hooks{OnFault: func(point FaultPoint) error {
			if point == FaultAfterSourceRecheck {
				return injected
			}
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Rebuild(context.Background())
	if !errors.Is(err, injected) || result.Activated {
		t.Fatalf("Rebuild result=%#v err=%v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath))); !os.IsNotExist(err) {
		t.Fatalf("pre-activation failure published authority: %v", err)
	}
	assertNoProjectionTestStages(t, workspace)

	restarted, err := newProjectionTestService(t, Options{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Rebuild(context.Background()); err != nil {
		t.Fatalf("restart rebuild: %v", err)
	}
}

func TestSidecarAppearingImmediatelyAfterNamespaceReplaceIsQuarantined(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "post replace sidecar source")
	options := projectionTestServiceOptions(workspace)
	options.Hooks.OnAfterNamespaceReplace = func() error {
		return os.WriteFile(
			filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath)+"-wal"),
			[]byte("injected unsafe sidecar"),
			0o600,
		)
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Rebuild(context.Background())
	if err == nil || !result.Activated {
		t.Fatalf("post-replace sidecar result=%#v err=%v", result, err)
	}
	if len(result.QuarantinePaths) != 2 {
		t.Fatalf("post-replace quarantine paths=%#v", result.QuarantinePaths)
	}
	for _, path := range []string{
		filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath)),
		filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath)+"-wal"),
	} {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("unsafe visible Projection entry remains at %q: %v", path, statErr)
		}
	}
	assertNoProjectionTestStages(t, workspace)

	restarted, err := NewService(projectionTestServiceOptions(workspace))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Rebuild(context.Background()); err != nil {
		t.Fatalf("restart rebuild after post-replace quarantine: %v", err)
	}
	assertProjectionQueryPath(t, restarted, "sidecar", "chapters/ch1.md")
}

func TestNonRegularSidecarCannotRollActivatedMainBackIntoVisibility(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "nonregular sidecar source")
	options := projectionTestServiceOptions(workspace)
	options.Hooks.OnAfterNamespaceReplace = func() error {
		return os.Mkdir(filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath)+"-wal"), 0o700)
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Rebuild(context.Background())
	if err == nil || !result.Activated || len(result.QuarantinePaths) != 1 {
		t.Fatalf("nonregular sidecar result=%#v err=%v", result, err)
	}
	if _, statErr := os.Lstat(result.DatabasePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("activated main was rolled back into visibility: %v", statErr)
	}
	sidecarPath := result.DatabasePath + "-wal"
	if info, statErr := os.Lstat(sidecarPath); statErr != nil || !info.IsDir() {
		t.Fatalf("retained nonregular sidecar info=%v err=%v", info, statErr)
	}
	if content, readErr := os.ReadFile(filepath.Join(workspace, "chapters", "ch1.md")); readErr != nil || string(content) != "nonregular sidecar source" {
		t.Fatalf("formal source=%q err=%v", content, readErr)
	}
}

func TestQuarantinePartialFailureKeepsActivatedMainUnpublished(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "partial quarantine source")
	injected := errors.New("injected sidecar quarantine failure")
	options := projectionTestServiceOptions(workspace)
	options.Hooks.OnAfterNamespaceReplace = func() error {
		return os.WriteFile(filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath)+"-wal"), []byte("diagnostic sidecar"), 0o600)
	}
	options.Hooks.OnQuarantineRename = func(source, _ string) error {
		if source == filepath.Base(DatabaseRelativePath)+"-wal" {
			return injected
		}
		return nil
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Rebuild(context.Background())
	if !errors.Is(err, injected) || !result.Activated || len(result.QuarantinePaths) != 1 {
		t.Fatalf("partial quarantine result=%#v err=%v", result, err)
	}
	if _, statErr := os.Lstat(result.DatabasePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partially quarantined main remains visible: %v", statErr)
	}
	if content, readErr := os.ReadFile(result.DatabasePath + "-wal"); readErr != nil || string(content) != "diagnostic sidecar" {
		t.Fatalf("retained sidecar=%q err=%v", content, readErr)
	}
}

func TestQuarantineRejectsSourceIdentitySubstitutionBeforeRename(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside authority"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "identity substitution source")
	options := projectionTestServiceOptions(workspace)
	options.Hooks.OnAfterNamespaceReplace = func() error {
		return os.WriteFile(filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath)+"-wal"), []byte("sidecar"), 0o600)
	}
	options.Hooks.OnQuarantineRename = func(source, _ string) error {
		if source != filepath.Base(DatabaseRelativePath) {
			return nil
		}
		mainPath := filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath))
		if err := os.Rename(mainPath, filepath.Join(workspace, ".denova", "substituted-original.db")); err != nil {
			return err
		}
		return os.Symlink(outside, mainPath)
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Rebuild(context.Background())
	if err == nil || !result.Activated || len(result.QuarantinePaths) != 0 {
		t.Fatalf("identity substitution result=%#v err=%v", result, err)
	}
	if content, readErr := os.ReadFile(outside); readErr != nil || string(content) != "outside authority" {
		t.Fatalf("outside authority=%q err=%v", content, readErr)
	}
	if info, statErr := os.Lstat(result.DatabasePath); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("substituted source path info=%v err=%v", info, statErr)
	}
}
