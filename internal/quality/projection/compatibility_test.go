package projection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	qualityworkspace "denova/internal/quality/workspace"
)

const projectionTestWorkspaceMarker = `{
  "schema_version": 1,
  "reader": {
    "min_schema_version": 1,
    "max_schema_version": 1,
    "min_denova_version": "1.0.0"
  },
  "writer": {
    "schema_version": 1,
    "min_denova_version": "1.0.0",
    "compatibility_range": ">=1.0.0 <2.0.0",
    "version": "1.0.0"
  },
  "features": {
    "fts_projection": {"version": "1.0.0", "required": false},
    "quality_harness": {"version": "1.1.0", "required": true}
  },
  "migration": {"state": "not_required"}
}
`

func projectionTestServiceOptions(workspace string) Options {
	return Options{
		Workspace:          workspace,
		WorkspaceInspector: WorkspaceSchemaInspectorOptions("1.6.2"),
	}
}

func newProjectionTestService(t *testing.T, options Options) (*Service, error) {
	t.Helper()
	options.WorkspaceInspector = WorkspaceSchemaInspectorOptions("1.6.2")
	return NewService(options)
}

func TestRebuildBlocksIncompatibleWorkspaceSchemaWithoutProjectionMutation(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionWorkspaceMarker(t, workspace, projectionTestWorkspaceMarker)
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "compatible source")
	service, err := NewService(projectionTestServiceOptions(workspace))
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	databaseBefore, err := os.ReadFile(initial.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}

	newerMarker := strings.Replace(projectionTestWorkspaceMarker, `"schema_version": 1,`, `"schema_version": 2,`, 1)
	writeProjectionWorkspaceMarker(t, workspace, newerMarker)
	result, err := service.Rebuild(context.Background())
	if err == nil || result.Activated || len(result.QuarantinePaths) != 0 {
		t.Fatalf("incompatible rebuild result=%#v err=%v", result, err)
	}
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || unavailable.Status.Reason != ReasonWorkspaceIncompatible {
		t.Fatalf("incompatible error=%T %v status=%#v", err, err, unavailable)
	}
	var blocked *qualityworkspace.MutationBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("incompatible error does not retain workspace blocker: %v", err)
	}
	databaseAfter, err := os.ReadFile(initial.DatabasePath)
	if err != nil || !reflect.DeepEqual(databaseAfter, databaseBefore) {
		t.Fatalf("incompatible rebuild changed Projection bytes err=%v", err)
	}
	markerAfter, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(qualityworkspace.MarkerRelativePath)))
	if err != nil || string(markerAfter) != newerMarker {
		t.Fatalf("incompatible marker changed: %q err=%v", markerAfter, err)
	}
}

func TestRebuildBlocksMissingRunningApplicationVersionWithoutProjectionMutation(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionWorkspaceMarker(t, workspace, projectionTestWorkspaceMarker)
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "compatible source")
	options := projectionTestServiceOptions(workspace)
	options.WorkspaceInspector.ApplicationVersion = ""
	service, err := NewService(options)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Rebuild(context.Background())
	if err == nil || result.Activated {
		t.Fatalf("missing-version rebuild result=%#v err=%v", result, err)
	}
	var blocked *qualityworkspace.MutationBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("missing-version error=%T %v, want Workspace Schema blocker", err, err)
	}
	found := false
	for _, issue := range blocked.Issues {
		if issue.Code == qualityworkspace.CodeApplicationVersionInvalid {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing-version blockers=%#v", blocked.Issues)
	}
	if _, statErr := os.Lstat(filepath.Join(workspace, filepath.FromSlash(DatabaseRelativePath))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("missing running version created Projection: %v", statErr)
	}
}

func writeProjectionWorkspaceMarker(t *testing.T, workspace, raw string) {
	t.Helper()
	path := filepath.Join(workspace, filepath.FromSlash(qualityworkspace.MarkerRelativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}
