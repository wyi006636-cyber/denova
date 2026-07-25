package integration_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"denova/internal/book/versions"
	"denova/internal/quality/projection"
	qualityworkspace "denova/internal/quality/workspace"
)

func TestPhase1VersionSnapshotsIncludeAuthorityAndExcludeDisposableState(t *testing.T) {
	workspace := t.TempDir()
	included := []string{
		"book.json",
		"ideas.md",
		"setting/story.md",
		"chapters/0001.md",
		qualityworkspace.MarkerRelativePath,
		".denova/profile-lock.json",
		".denova/quality/specs/project.json",
		".denova/quality/preferences.jsonl",
		".denova/quality/artifacts/candidate-sets/candidate-set-001.json",
		".denova/quality/artifacts/review-issues/review-issue-001.json",
		".denova/quality/artifacts/allowed/custom-audit.json",
		".denova/quality/decisions/decision-001.json",
		".denova/quality/finalizations/finalization-001.json",
		".denova/quality/migration-receipts/migration-001.json",
		".nova/workspace-schema.json",
		".nova/profile-lock.json",
		".nova/quality/runs/protected-v1-looking-unknown.json",
		".nova/index.db",
		".nova/cache/protected-unknown.bin",
	}
	excluded := []string{
		".denova/index.db",
		".denova/index.db-wal",
		".denova/cache/cache.bin",
		".denova/quality/projections/fts.bin",
		".denova/quality/runs/run-001.json",
		".denova/runs/run-001.jsonl",
		".denova/checkpoints/checkpoint-001.json",
		".denova/sessions/session-001.json",
		".denova/backups/backup-001.bin",
		".denova/messages/message-001.json",
		".denova/changes/change-001.json",
		".denova/reviews/review-001.json",
		".denova/interactive/story-001.json",
		".denova/automations/inbox.json",
		".nova/runs/legacy-run-001.json",
		".nova/checkpoints/legacy-checkpoint-001.json",
		".nova/sessions/legacy-session-001.json",
		".denova-migration/migration-001/state.json",
		".nova-migration/migration-001/state.json",
		".denova-migrate-migration-001.tmp",
		".nova-migrate-migration-001.tmp",
	}
	writeVersionMatrix(t, workspace, included, "version-one:")
	writeVersionMatrix(t, workspace, excluded, "excluded-one:")
	service := versions.NewService(workspace)
	settings := versions.DefaultAutoSettings()
	first, err := service.Create("phase 1 authority one", versions.VersionSourceManual, settings)
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}

	writeVersionMatrix(t, workspace, included, "version-two:")
	writeVersionMatrix(t, workspace, excluded, "excluded-two:")
	if _, err := service.Create("phase 1 authority two", versions.VersionSourceManual, settings); err != nil {
		t.Fatalf("Create second: %v", err)
	}
	writeVersionMatrix(t, workspace, included, "dirty-included:")
	writeVersionMatrix(t, workspace, excluded, "live-excluded:")

	if _, err := service.Restore(first.Version.ID, settings); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, relative := range included {
		want := "version-one:" + relative
		if got := readIntegrationFile(t, workspace, relative); got != want {
			t.Fatalf("included %s = %q, want %q", relative, got, want)
		}
	}
	for _, relative := range excluded {
		want := "live-excluded:" + relative
		if got := readIntegrationFile(t, workspace, relative); got != want {
			t.Fatalf("excluded %s = %q, want %q", relative, got, want)
		}
	}
}

func TestPhase1CandidateAndUnconfirmedPreferenceAreNotDefaultProjectionSources(t *testing.T) {
	workspace := t.TempDir()
	candidatePath := ".denova/quality/artifacts/candidate-sets/candidate-set-001.json"
	unconfirmedPreferencePath := ".denova/quality/artifacts/preference-proposals/preference-001.json"
	writeIntegrationFiles(t, workspace, map[string]string{
		qualityworkspace.MarkerRelativePath: phase1ProjectionMarker,
		"chapters/0001.md":                  "formal manuscript phrase alpha-authority\n",
		candidatePath:                       `{"candidate":"candidate-only phrase beta-candidate"}`,
		unconfirmedPreferencePath:           `{"status":"candidate_only","preference":"unconfirmed phrase gamma-preference"}`,
	})

	snapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(
		context.Background(),
		workspace,
		qualityworkspace.ProjectionSourceOptions{},
	)
	if err != nil {
		t.Fatalf("BuildProjectionSourceSnapshot: %v", err)
	}
	paths := make([]string, 0, len(snapshot.Documents))
	for _, document := range snapshot.Documents {
		paths = append(paths, document.Path)
	}
	sort.Strings(paths)
	for _, forbidden := range []string{candidatePath, unconfirmedPreferencePath} {
		if index := sort.SearchStrings(paths, forbidden); index < len(paths) && paths[index] == forbidden {
			t.Fatalf("pending authority entered default Projection sources: %s in %#v", forbidden, paths)
		}
	}
	if index := sort.SearchStrings(paths, "chapters/0001.md"); index >= len(paths) || paths[index] != "chapters/0001.md" {
		t.Fatalf("formal chapter missing from Projection sources: %#v", paths)
	}

	service := newIntegrationProjectionService(t, workspace)
	if _, err := service.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	responses := integrationProjectionQueries(t, service, []string{"alpha-authority", "beta-candidate", "gamma-preference"})
	if len(responses[0].Results) != 1 || responses[0].Results[0].Path != "chapters/0001.md" {
		t.Fatalf("formal query = %#v", responses[0])
	}
	if len(responses[1].Results) != 0 || len(responses[2].Results) != 0 {
		t.Fatalf("pending Candidate/Preference became searchable by default: %#v", responses)
	}

	approved, err := qualityworkspace.BuildProjectionSourceSnapshot(
		context.Background(),
		workspace,
		qualityworkspace.ProjectionSourceOptions{ApprovedArtifactPaths: []string{candidatePath}},
	)
	if err != nil {
		t.Fatalf("approved Candidate Artifact snapshot: %v", err)
	}
	approvedPaths := make([]string, 0, len(approved.Documents))
	for _, document := range approved.Documents {
		approvedPaths = append(approvedPaths, document.Path)
	}
	sort.Strings(approvedPaths)
	if index := sort.SearchStrings(approvedPaths, candidatePath); index >= len(approvedPaths) || approvedPaths[index] != candidatePath {
		t.Fatalf("explicitly approved Artifact missing: %#v", approvedPaths)
	}
	if index := sort.SearchStrings(approvedPaths, unconfirmedPreferencePath); index < len(approvedPaths) && approvedPaths[index] == unconfirmedPreferencePath {
		t.Fatalf("unconfirmed Preference was implicitly approved: %#v", approvedPaths)
	}
}

func TestPhase1LegacyV1LookingUnknownRemainsProtectedAndNotProjected(t *testing.T) {
	workspace := t.TempDir()
	legacyUnknown := ".nova/quality/preferences.jsonl"
	writeIntegrationFiles(t, workspace, map[string]string{
		"chapters/0001.md": "legacy workspace formal chapter\n",
		legacyUnknown:      "legacy-looking bytes must stay protected\n",
	})
	before := integrationSelectedBytes(t, workspace, []string{"chapters/0001.md", legacyUnknown})
	inspection, err := newIntegrationInspector(t).Inspect(workspace)
	if err != nil || !inspection.CanOpen() || inspection.ActiveRoot != ".nova" {
		t.Fatalf("legacy inspection = %#v, %v", inspection, err)
	}
	classification, err := qualityworkspace.ClassifyPath(legacyUnknown)
	if err != nil || classification.Category != qualityworkspace.CategoryProtectedLegacyUnknown || classification.VersionDisposition != qualityworkspace.VersionInclude {
		t.Fatalf("legacy classification = %#v, %v", classification, err)
	}
	snapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range snapshot.Documents {
		if document.Path == legacyUnknown {
			t.Fatalf("protected legacy unknown entered Projection: %#v", document)
		}
	}
	if after := integrationSelectedBytes(t, workspace, []string{"chapters/0001.md", legacyUnknown}); !reflect.DeepEqual(after, before) {
		t.Fatalf("legacy inspection/snapshot changed protected bytes\nbefore=%#v\nafter=%#v", before, after)
	}

	_, err = projection.NewService(projection.Options{
		Workspace:          workspace,
		WorkspaceInspector: projection.WorkspaceSchemaInspectorOptions("1.6.2"),
	})
	if err != nil {
		t.Fatalf("NewService must remain read-only for legacy workspace: %v", err)
	}
}

func writeVersionMatrix(t *testing.T, workspace string, paths []string, prefix string) {
	t.Helper()
	files := make(map[string]string, len(paths))
	for _, relative := range paths {
		files[relative] = prefix + relative
	}
	writeIntegrationFiles(t, workspace, files)
}
