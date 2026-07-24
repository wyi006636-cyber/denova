package workspace_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	qualityworkspace "denova/internal/quality/workspace"
)

func TestBuildProjectionSourceSnapshotSelectsBoundedAuthority(t *testing.T) {
	workspace := t.TempDir()
	files := map[string]string{
		"ideas.md":                                      "核心创作方向",
		"notes.txt":                                     "reader notes",
		"setting/outline.md":                            "第一幕",
		"chapters/ch1.md":                               "雨落在旧码头。",
		".denova/workspace-schema.json":                 `{"schema_version":1}`,
		".denova/profile-lock.json":                     `{"profile_id":"long_serial"}`,
		".denova/lore/items.json":                       `{"items":[]}`,
		".denova/quality/specs/spec.json":               `{"kind":"quality-spec"}`,
		".denova/quality/preferences.jsonl":             `{"kind":"preference"}` + "\n",
		".denova/quality/artifacts/approved.json":       `{"status":"approved"}`,
		".denova/quality/artifacts/pending.json":        `{"status":"pending"}`,
		".denova/quality/runs/run.json":                 `{"runtime":true}`,
		".denova/runs/agent.jsonl":                      `{"runtime":true}`,
		".denova/cache/cache.json":                      `{"cache":true}`,
		".denova/quality/projections/other.json":        `{"projection":true}`,
		".denova/index.db":                              "not-a-live-database",
		".denova/index.db-wal":                          "derived-sidecar",
		".denova-migration/migration/backup/chapter.md": "migration backup",
		".git/config":                                   "git runtime",
		".nova/lore/items.json":                         `{"items":[{"id":"inactive"}]}`,
		".nova/workspace-schema.json":                   `{"schema_version":1}`,
		".nova/quality/artifacts/looks-approved.json":   `{"status":"approved"}`,
		".nova/index.db":                                "legacy-looking",
		".nova/cache/cache.json":                        "legacy-looking",
		"assets/image.png":                              "\x00\x01\x02",
	}
	for path, content := range files {
		writeProjectionSourceFile(t, workspace, path, []byte(content))
	}

	snapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{
		ApprovedArtifactPaths: []string{".denova/quality/artifacts/approved.json"},
	})
	if err != nil {
		t.Fatalf("BuildProjectionSourceSnapshot: %v", err)
	}

	wantPaths := []string{
		".denova/lore/items.json",
		".denova/profile-lock.json",
		".denova/quality/artifacts/approved.json",
		".denova/quality/preferences.jsonl",
		".denova/quality/specs/spec.json",
		".denova/workspace-schema.json",
		"chapters/ch1.md",
		"ideas.md",
		"notes.txt",
		"setting/outline.md",
	}
	gotPaths := make([]string, 0, len(snapshot.Documents))
	for _, document := range snapshot.Documents {
		gotPaths = append(gotPaths, document.Path)
		wantBytes := []byte(files[document.Path])
		if !reflect.DeepEqual(document.Content, wantBytes) {
			t.Fatalf("document %q content = %q, want %q", document.Path, document.Content, wantBytes)
		}
		if document.ID != projectionDocumentID(document.Path) {
			t.Fatalf("document %q ID = %q", document.Path, document.ID)
		}
		if document.RevisionHash != projectionSHA256(wantBytes) {
			t.Fatalf("document %q revision = %q", document.Path, document.RevisionHash)
		}
		if document.Profile != qualityworkspace.ProjectionProfileWorkspace {
			t.Fatalf("document %q profile = %q", document.Path, document.Profile)
		}
		if document.Kind == "" || document.Size != int64(len(wantBytes)) {
			t.Fatalf("document %q metadata = %#v", document.Path, document)
		}
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("snapshot paths:\n got: %#v\nwant: %#v", gotPaths, wantPaths)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Workspace != canonicalWorkspace || snapshot.Hash == "" {
		t.Fatalf("snapshot identity = %#v", snapshot)
	}

	snapshot.Documents[0].Content[0] ^= 0xff
	rebuilt, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{
		ApprovedArtifactPaths: []string{".denova/quality/artifacts/approved.json"},
	})
	if err != nil {
		t.Fatalf("rebuild source snapshot: %v", err)
	}
	if rebuilt.Hash != snapshot.Hash {
		t.Fatalf("deterministic snapshot hash changed: first=%s second=%s", snapshot.Hash, rebuilt.Hash)
	}
	if !reflect.DeepEqual(rebuilt.Documents[0].Content, []byte(files[rebuilt.Documents[0].Path])) {
		t.Fatal("returned source content aliases mutable prior snapshot bytes")
	}
}

func TestBuildProjectionSourceSnapshotRequiresExactApprovedArtifacts(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionSourceFile(t, workspace, "chapters/ch1.md", []byte("chapter"))
	writeProjectionSourceFile(t, workspace, ".denova/quality/artifacts/approved.json", []byte(`{"status":"approved"}`))

	for _, path := range []string{
		"chapters/ch1.md",
		".denova/quality/artifacts/missing.json",
		".nova/quality/artifacts/looks-approved.json",
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			_, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{
				ApprovedArtifactPaths: []string{path},
			})
			if err == nil {
				t.Fatalf("approved Artifact path %q must be rejected", path)
			}
		})
	}
}

func TestBuildProjectionSourceSnapshotEnforcesEveryBound(t *testing.T) {
	tests := []struct {
		name   string
		limits qualityworkspace.SourceSnapshotLimits
	}{
		{name: "file count", limits: qualityworkspace.SourceSnapshotLimits{MaxFiles: 1, MaxFileBytes: 1024, MaxTotalBytes: 1024}},
		{name: "file bytes", limits: qualityworkspace.SourceSnapshotLimits{MaxFiles: 10, MaxFileBytes: 3, MaxTotalBytes: 1024}},
		{name: "total bytes", limits: qualityworkspace.SourceSnapshotLimits{MaxFiles: 10, MaxFileBytes: 1024, MaxTotalBytes: 7}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeProjectionSourceFile(t, workspace, "chapters/a.md", []byte("four"))
			writeProjectionSourceFile(t, workspace, "chapters/b.md", []byte("five"))
			_, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{Limits: test.limits})
			if err == nil {
				t.Fatalf("limits %#v must reject the snapshot", test.limits)
			}
		})
	}
}

func TestBuildProjectionSourceSnapshotRejectsReparseSource(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	outsidePath := filepath.Join(outside, "outside.md")
	writeProjectionSourceFile(t, outside, "outside.md", []byte("outside truth"))
	if err := os.Symlink(outsidePath, filepath.Join(workspace, "linked.md")); err != nil {
		t.Fatal(err)
	}

	_, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{})
	if err == nil {
		t.Fatal("source snapshot must reject a reparse-point source")
	}
	data, readErr := os.ReadFile(outsidePath)
	if readErr != nil || string(data) != "outside truth" {
		t.Fatalf("outside source changed: data=%q err=%v", data, readErr)
	}
}

func TestBuildProjectionSourceSnapshotHonorsCancellation(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionSourceFile(t, workspace, "chapters/ch1.md", []byte("chapter"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := qualityworkspace.BuildProjectionSourceSnapshot(ctx, workspace, qualityworkspace.ProjectionSourceOptions{})
	if err == nil {
		t.Fatal("cancelled source snapshot must fail")
	}
}

func TestBuildProjectionSourceSnapshotUsesOnlyPinnedLegacyRoot(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionSourceFile(t, workspace, ".denova/runs/run.jsonl", []byte(`{"runtime":true}`))
	writeProjectionSourceFile(t, workspace, ".nova/lore/items.json", []byte(`{"items":[{"id":"legacy"}]}`))
	writeProjectionSourceFile(t, workspace, "chapters/ch1.md", []byte("formal chapter"))

	snapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{})
	if err != nil {
		t.Fatalf("BuildProjectionSourceSnapshot: %v", err)
	}
	paths := make([]string, 0, len(snapshot.Documents))
	for _, document := range snapshot.Documents {
		paths = append(paths, document.Path)
	}
	want := []string{".nova/lore/items.json", "chapters/ch1.md"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("legacy-active source paths = %#v, want %#v", paths, want)
	}
}

func TestBuildProjectionSourceSnapshotBoundsAllVisitedEntriesAndPathBytes(t *testing.T) {
	workspace := t.TempDir()
	for _, relative := range []string{
		"assets/one.png",
		"assets/two.png",
		"assets/three.png",
		"assets/four.png",
	} {
		writeProjectionSourceFile(t, workspace, relative, []byte("excluded binary-like asset"))
	}

	_, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{
		Limits: qualityworkspace.SourceSnapshotLimits{MaxEntries: 3, MaxPathBytes: 1024},
	})
	if err == nil {
		t.Fatal("snapshot traversal must bound excluded and non-text entries")
	}
	var snapshotErr *qualityworkspace.SourceSnapshotError
	if !errors.As(err, &snapshotErr) || snapshotErr.Field != "limits.max_entries" {
		t.Fatalf("entry-bound error = %T %#v", err, err)
	}

	_, err = qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{
		Limits: qualityworkspace.SourceSnapshotLimits{MaxEntries: 100, MaxPathBytes: 12},
	})
	if err == nil {
		t.Fatal("snapshot traversal must bound total visited path bytes")
	}
	if !errors.As(err, &snapshotErr) || snapshotErr.Field != "limits.max_path_bytes" {
		t.Fatalf("path-byte-bound error = %T %#v", err, err)
	}
}

func writeProjectionSourceFile(t *testing.T, workspace, relative string, content []byte) {
	t.Helper()
	path := filepath.Join(workspace, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func projectionDocumentID(path string) string {
	return "doc-" + projectionSHA256([]byte(path))
}

func projectionSHA256(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
