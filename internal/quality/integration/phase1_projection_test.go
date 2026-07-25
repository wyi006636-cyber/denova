package integration_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"denova/internal/api/sse"
	"denova/internal/app"
	"denova/internal/quality/harness"
	"denova/internal/quality/projection"
	qualityworkspace "denova/internal/quality/workspace"
)

const phase1ProjectionMarker = `{
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
    "version": "1.6.2"
  },
  "features": {
    "fts_projection": {"version": "1.0.0", "required": false},
    "quality_harness": {"version": "1.1.0", "required": true}
  },
  "migration": {"state": "not_required"}
}
`

func TestPhase1ProjectionDeleteAndRebuildPreservesQueryIdentityAndTruth(t *testing.T) {
	workspace := t.TempDir()
	writeIntegrationFiles(t, workspace, map[string]string{
		qualityworkspace.MarkerRelativePath: phase1ProjectionMarker,
		"ideas.md":                          "harbor mystery seed\n",
		"chapters/0001.md":                  "A quick brown fox crosses the warm harbor.\n",
		"chapters/0002.md":                  "小说创作投影必须可以安全删除重建。\n",
	})
	formalPaths := []string{qualityworkspace.MarkerRelativePath, "ideas.md", "chapters/0001.md", "chapters/0002.md"}
	formalBefore := integrationSelectedBytes(t, workspace, formalPaths)
	service := newIntegrationProjectionService(t, workspace)

	first, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("first Rebuild: %v", err)
	}
	if !first.Fresh || !first.Activated || !first.ParentSynced || first.SourceSnapshotHash == "" {
		t.Fatalf("first rebuild = %#v", first)
	}
	firstStatus, err := service.Inspect(context.Background())
	if err != nil || firstStatus.State != projection.StateAvailable || firstStatus.Reason != projection.ReasonNone {
		t.Fatalf("first status = %#v, %v", firstStatus, err)
	}
	firstQueries := integrationProjectionQueries(t, service, []string{"quick", "小说", "删除重建"})
	firstSnapshot := integrationProjectionSourceSnapshot(t, workspace)

	if err := os.Remove(first.DatabasePath); err != nil {
		t.Fatal(err)
	}
	missing, err := service.Inspect(context.Background())
	if err != nil || missing.State != projection.StateUnavailable || missing.Reason != projection.ReasonMissing {
		t.Fatalf("missing Projection status = %#v, %v", missing, err)
	}
	inspection, err := newIntegrationInspectorWithProjection(t).Inspect(workspace)
	if err != nil || !inspection.CanOpen() {
		t.Fatalf("workspace inspection with missing Projection = %#v, %v", inspection, err)
	}
	if got := integrationSelectedBytes(t, workspace, formalPaths); !reflect.DeepEqual(got, formalBefore) {
		t.Fatal("deleting Projection changed formal files")
	}

	second, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("second Rebuild: %v", err)
	}
	secondQueries := integrationProjectionQueries(t, service, []string{"quick", "小说", "删除重建"})
	secondSnapshot := integrationProjectionSourceSnapshot(t, workspace)
	if second.SourceSnapshotHash != first.SourceSnapshotHash || second.DocumentCount != first.DocumentCount {
		t.Fatalf("rebuild identity changed\nfirst=%#v\nsecond=%#v", first, second)
	}
	if !reflect.DeepEqual(secondQueries, firstQueries) {
		t.Fatalf("query identity changed\nfirst=%#v\nsecond=%#v", firstQueries, secondQueries)
	}
	if !reflect.DeepEqual(secondSnapshot, firstSnapshot) {
		t.Fatalf("source document identity/revision changed\nfirst=%#v\nsecond=%#v", firstSnapshot, secondSnapshot)
	}
	if got := integrationSelectedBytes(t, workspace, formalPaths); !reflect.DeepEqual(got, formalBefore) {
		t.Fatal("Projection rebuild wrote back to formal files")
	}
}

func TestPhase1CorruptProjectionDoesNotBlockWorkspaceAuthority(t *testing.T) {
	workspace := t.TempDir()
	writeIntegrationFiles(t, workspace, map[string]string{
		qualityworkspace.MarkerRelativePath: phase1ProjectionMarker,
		"chapters/0001.md":                  "authoritative chapter survives corrupt index\n",
	})
	service := newIntegrationProjectionService(t, workspace)
	result, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.DatabasePath, []byte("not a sqlite database\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := service.Inspect(context.Background())
	if err != nil || status.State != projection.StateUnavailable || status.Reason != projection.ReasonCorrupt {
		t.Fatalf("corrupt Projection status = %#v, %v", status, err)
	}
	inspection, err := newIntegrationInspectorWithProjection(t).Inspect(workspace)
	if err != nil || !inspection.CanOpen() {
		t.Fatalf("workspace inspection = %#v, %v", inspection, err)
	}
	if got := readIntegrationFile(t, workspace, "chapters/0001.md"); got != "authoritative chapter survives corrupt index\n" {
		t.Fatalf("author chapter = %q", got)
	}
}

func TestPhase1ExternalMarkdownEditMarksProjectionStaleAndKeepsEventBoundary(t *testing.T) {
	workspace := t.TempDir()
	writeIntegrationFiles(t, workspace, map[string]string{
		qualityworkspace.MarkerRelativePath:                         phase1ProjectionMarker,
		"chapters/0001.md":                                          "original author chapter\n",
		"setting/story.md":                                          "stable setting bytes\n",
		".denova/quality/artifacts/reviews/review-001.json":         `{"kind":"audit-artifact","value":"keep"}`,
		".denova/quality/finalizations/finalization-001.json":       `{"kind":"audit-version-record","value":"keep"}`,
		".denova/quality/specs/project-quality-spec-reference.json": `{"kind":"quality-spec","value":"keep"}`,
	})
	service := newIntegrationProjectionService(t, workspace)
	if _, err := service.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	unrelatedPaths := []string{
		"setting/story.md",
		".denova/quality/artifacts/reviews/review-001.json",
		".denova/quality/finalizations/finalization-001.json",
		".denova/quality/specs/project-quality-spec-reference.json",
	}
	unrelatedBefore := integrationSelectedBytes(t, workspace, unrelatedPaths)
	latest := "external author edit remains truth\n"
	writeIntegrationFiles(t, workspace, map[string]string{"chapters/0001.md": latest})

	inspection, err := newIntegrationInspectorWithProjection(t).Inspect(workspace)
	if err != nil || !inspection.CanOpen() {
		t.Fatalf("workspace inspection after external edit = %#v, %v", inspection, err)
	}
	status, err := service.Inspect(context.Background())
	if err != nil || status.State != projection.StateStale || status.Reason != projection.ReasonSourceChanged {
		t.Fatalf("stale status = %#v, %v", status, err)
	}
	if got := readIntegrationFile(t, workspace, "chapters/0001.md"); got != latest {
		t.Fatalf("external author bytes = %q", got)
	}
	if unrelatedAfter := integrationSelectedBytes(t, workspace, unrelatedPaths); !reflect.DeepEqual(unrelatedAfter, unrelatedBefore) {
		t.Fatalf("external edit changed unrelated formal or audit records\nbefore=%#v\nafter=%#v", unrelatedBefore, unrelatedAfter)
	}

	event := harness.Event{
		Contract:   harness.Contract{Kind: harness.ContractKind, Version: harness.ContractVersionV1},
		EventType:  harness.EventWorkflowInputInvalidated,
		EventID:    "event-phase1-input-invalidated",
		RunID:      "run-phase1-contract-only",
		OccurredAt: "2026-07-25T13:00:00Z",
		Sequence:   1,
		Summary: harness.Summary{
			Code: "quality.event.workflow.input.invalidated",
			Arguments: []harness.SummaryArgument{
				{Name: harness.SummaryArgumentReasonCode, Value: "source_changed"},
			},
		},
	}
	if err := harness.ValidateEvent(event); err != nil {
		t.Fatalf("ValidateEvent: %v", err)
	}
	transport, err := app.AdaptQualityEvent(event)
	if err != nil {
		t.Fatalf("AdaptQualityEvent: %v", err)
	}
	var frame bytes.Buffer
	if err := sse.WriteQualityEventFrame(&frame, transport); err != nil {
		t.Fatalf("WriteQualityEventFrame: %v", err)
	}
	encoded := frame.String()
	for _, required := range []string{
		"id: event-phase1-input-invalidated",
		"event: workflow.input.invalidated",
		`"reason_code","value":"source_changed"`,
	} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("SSE frame %q does not contain %q", encoded, required)
		}
	}
	if strings.Contains(encoded, latest) {
		t.Fatal("invalidation event leaked author content")
	}

	var unavailable *projection.UnavailableError
	if _, err := service.Open(context.Background()); !errors.As(err, &unavailable) || unavailable.Status.Reason != projection.ReasonSourceChanged {
		t.Fatalf("stale Projection Open error = %T %v", err, err)
	}
}

func newIntegrationProjectionService(t *testing.T, workspace string) *projection.Service {
	t.Helper()
	service, err := projection.NewService(projection.Options{
		Workspace:          workspace,
		WorkspaceInspector: projection.WorkspaceSchemaInspectorOptions("1.6.2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func newIntegrationInspectorWithProjection(t *testing.T) *qualityworkspace.Inspector {
	t.Helper()
	inspector, err := qualityworkspace.NewInspector(projection.WorkspaceSchemaInspectorOptions("1.6.2"))
	if err != nil {
		t.Fatal(err)
	}
	return inspector
}

func integrationProjectionQueries(t *testing.T, service *projection.Service, terms []string) []projection.QueryResponse {
	t.Helper()
	reader, err := service.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	responses := make([]projection.QueryResponse, 0, len(terms))
	for _, term := range terms {
		response, err := reader.Query(context.Background(), projection.QueryRequest{Text: term})
		if err != nil {
			t.Fatalf("Query(%q): %v", term, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func integrationProjectionSourceSnapshot(t *testing.T, workspace string) qualityworkspace.ProjectionSourceSnapshot {
	t.Helper()
	snapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func integrationSelectedBytes(t *testing.T, workspace string, paths []string) map[string]string {
	t.Helper()
	result := make(map[string]string, len(paths))
	for _, relative := range paths {
		raw, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		result[relative] = string(raw)
	}
	return result
}
