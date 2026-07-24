package versions

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestWorkspaceSchemaV1VersionPolicyUsesExactDefaultIncludeRules(t *testing.T) {
	excluded := []string{
		".git",
		".git/objects/aa",
		".denova-migration",
		".denova-migration/m1/backup/file",
		".nova-migration/m1/state.json",
		".denova-migrate-m1.tmp",
		".nova-migrate-m1.tmp",
		".denova/automations/inbox.json",
		".nova/automations/inbox.json",
		".denova/quality/runs/run/checkpoint.json",
		".denova/index.db",
		".denova/index.db-wal",
		".denova/index.db-shm",
		".denova/cache/context.bin",
		".denova/quality/projections/fts.bin",
	}
	for _, root := range []string{".denova", ".nova"} {
		for _, name := range []string{"runs", "checkpoints", "sessions", "backups", "messages", "changes", "reviews", "interactive"} {
			excluded = append(excluded, root+"/"+name, root+"/"+name+"/nested/file")
		}
	}
	included := []string{
		"ideas.md",
		"chapters/one.md",
		".denova/workspace-schema.json",
		".denova/profile-lock.json",
		".denova/quality/specs/project.json",
		".denova/quality/preferences.jsonl",
		".denova/quality/artifacts/reviews/r1.json",
		".denova/quality/artifacts/runs/output.json",
		".denova/quality/decisions/d1.json",
		".denova/quality/finalizations/f1.json",
		".denova/quality/migration-receipts/m1.json",
		".nova/workspace-schema.json",
		".nova/profile-lock.json",
		".nova/quality/runs/not-v1-runtime.json",
		".nova/index.db",
		".nova/index.db-wal",
		".nova/cache/context.bin",
		".denova/index.db.bak",
		".denova/index.db-wal/nested-protected.bin",
		".denova/cache-old/context.bin",
		".denova/quality/projections-old/index.bin",
		".denova/quality/runs-old/checkpoint.json",
		".denova/automations/inbox.json.bak",
		".denova/automations/inbox.json/unknown.bin",
		"other/reviews/author-note.md",
		"notes/.denova-migrate-m1.tmp",
		".denova-migrate-m1.tmp/unknown.bin",
		".gitignore",
		".unknown/private.bin",
	}

	for _, path := range excluded {
		t.Run("exclude/"+path, func(t *testing.T) {
			if !isVersionExcludedRelPath(path) {
				t.Fatalf("path must be excluded: %s", path)
			}
		})
	}
	for _, path := range included {
		t.Run("include/"+path, func(t *testing.T) {
			if isVersionExcludedRelPath(path) {
				t.Fatalf("default-protected path was over-excluded: %s", path)
			}
		})
	}
}

func TestWorkspaceFileSetCollectsExactV1PolicyWithoutSkippingExactPathLookalikeTrees(t *testing.T) {
	workspace := t.TempDir()
	included := []string{
		"chapters/one.md",
		".denova/workspace-schema.json",
		".denova/profile-lock.json",
		".denova/quality/specs/project.json",
		".denova/quality/preferences.jsonl",
		".denova/quality/artifacts/reviews/r1.json",
		".denova/quality/decisions/d1.json",
		".denova/quality/finalizations/f1.json",
		".denova/quality/migration-receipts/m1.json",
		".nova/workspace-schema.json",
		".nova/profile-lock.json",
		".nova/quality/runs/protected.json",
		".nova/index.db-wal",
		".nova/cache/protected.bin",
		".denova/automations/inbox.json/unknown.bin",
		".denova-migrate-m1.tmp/unknown.bin",
		".denova/quality/artifacts/runs/not-runtime.json",
		".unknown/private.bin",
	}
	excluded := []string{
		".denova/runs/run.jsonl",
		".nova/checkpoints/c1.json",
		".denova/sessions/s1.json",
		".nova/backups/b1.bin",
		".denova/messages/m1.json",
		".nova/changes/c1.jsonl",
		".denova/reviews/r1.jsonl",
		".nova/interactive/story.json",
		".nova/automations/inbox.json",
		".denova/quality/runs/q1.json",
		".denova/index.db",
		".denova/index.db-wal",
		".denova/cache/blob.bin",
		".denova/quality/projections/fts.bin",
		".denova-migration/m1/state.json",
		".nova-migration/m1/state.json",
		".nova-migrate-m1.tmp",
		".git/objects/test",
	}
	for _, path := range included {
		writeFile(t, workspace, path, "included:"+path)
	}
	for _, path := range excluded {
		writeFile(t, workspace, path, "excluded:"+path)
	}

	files, err := (WorkspaceFileSet{root: workspace}).Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := make([]string, 0, len(files))
	for _, file := range files {
		got = append(got, file.Path)
	}
	sort.Strings(included)
	if !reflect.DeepEqual(got, included) {
		t.Fatalf("collected paths:\nwant=%#v\n got=%#v", included, got)
	}
}

func TestWorkspaceSchemaV1RestorePreservesEveryExistingExcludedEntry(t *testing.T) {
	workspace := t.TempDir()
	service := NewService(workspace)
	settings := DefaultAutoSettings()
	writeFile(t, workspace, "chapters/one.md", "version one")
	writeFile(t, workspace, ".nova/index.db", "protected legacy v1-looking version one")
	writeFile(t, workspace, ".denova/quality/artifacts/reviews/r1.json", "artifact version one")

	excluded := []string{
		".denova/runs/run.jsonl",
		".nova/checkpoints/c1.json",
		".denova/sessions/s1.json",
		".nova/backups/b1.bin",
		".denova/messages/m1.json",
		".nova/changes/c1.jsonl",
		".denova/reviews/r1.jsonl",
		".nova/interactive/story.json",
		".denova/automations/inbox.json",
		".nova/automations/inbox.json",
		".denova/quality/runs/q1.json",
		".denova/index.db",
		".denova/index.db-wal",
		".denova/cache/blob.bin",
		".denova/quality/projections/fts.bin",
		".denova-migration/m1/state.json",
		".nova-migration/m1/state.json",
		".denova-migrate-m1.tmp",
		".nova-migrate-m1.tmp",
	}
	for _, path := range excluded {
		writeFile(t, workspace, path, "initial excluded:"+path)
	}

	first, err := service.Create("first", VersionSourceManual, settings)
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	writeFile(t, workspace, "chapters/one.md", "version two")
	writeFile(t, workspace, ".nova/index.db", "protected legacy v1-looking version two")
	writeFile(t, workspace, ".denova/quality/artifacts/reviews/r1.json", "artifact version two")
	if _, err := service.Create("second", VersionSourceManual, settings); err != nil {
		t.Fatalf("Create second: %v", err)
	}

	for _, path := range excluded {
		writeFile(t, workspace, path, "live excluded:"+path)
	}
	writeFile(t, workspace, "chapters/new.md", "remove me")
	writeFile(t, workspace, "chapters/one.md", "dirty before restore")
	writeFile(t, workspace, ".nova/index.db", "dirty included legacy input")
	writeFile(t, workspace, ".denova/quality/artifacts/reviews/r1.json", "dirty included artifact")

	if _, err := service.Restore(first.Version.ID, settings); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, path := range excluded {
		if got := readFile(t, workspace, path); got != "live excluded:"+path {
			t.Fatalf("excluded path %s changed during restore: %q", path, got)
		}
	}
	if got := readFile(t, workspace, "chapters/one.md"); got != "version one" {
		t.Fatalf("included formal file was not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(workspace, "chapters", "new.md")); !os.IsNotExist(err) {
		t.Fatalf("included file absent from target should be deleted, err=%v", err)
	}
	if got := readFile(t, workspace, ".nova/index.db"); got != "protected legacy v1-looking version one" {
		t.Fatalf("legacy v1-looking protected input must remain included: %q", got)
	}
	if got := readFile(t, workspace, ".denova/quality/artifacts/reviews/r1.json"); got != "artifact version one" {
		t.Fatalf("review artifact must remain included: %q", got)
	}
}

func TestWorkspaceSchemaV1RestoreDoesNotOverProtectExactFileLookalikeDirectories(t *testing.T) {
	workspace := t.TempDir()
	service := NewService(workspace)
	settings := DefaultAutoSettings()
	paths := []string{
		".denova/automations/inbox.json/unknown.bin",
		".denova/index.db-wal/nested-protected.bin",
		".denova-migrate-m1.tmp/unknown.bin",
	}
	writeFile(t, workspace, "chapters/one.md", "version one")
	for _, path := range paths {
		writeFile(t, workspace, path, "version one:"+path)
	}
	first, err := service.Create("first", VersionSourceManual, settings)
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	writeFile(t, workspace, "chapters/one.md", "version two")
	for _, path := range paths {
		writeFile(t, workspace, path, "version two:"+path)
	}
	if _, err := service.Create("second", VersionSourceManual, settings); err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if _, err := service.Restore(first.Version.ID, settings); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, path := range paths {
		if got := readFile(t, workspace, path); got != "version one:"+path {
			t.Fatalf("included child beneath exact-file lookalike %s was over-protected: %q", path, got)
		}
	}
}

func TestSafeVisiblePathRejectsOnlyExactV1ExcludedPaths(t *testing.T) {
	workspace := t.TempDir()
	for _, path := range []string{
		".denova/checkpoints/c1.json",
		".nova/messages/m1.json",
		".denova/automations/inbox.json",
		".denova/quality/runs/q1.json",
		".denova/index.db-wal",
		".denova/cache/c1.bin",
		".denova/quality/projections/p1.bin",
		".denova-migration/m1/state.json",
		".denova-migrate-m1.tmp",
	} {
		if _, err := safeVisiblePath(workspace, path); err == nil {
			t.Fatalf("excluded path should be rejected: %s", path)
		}
	}
	for _, path := range []string{
		".nova/quality/runs/protected.json",
		".nova/index.db-wal",
		".nova/cache/protected.bin",
		".denova/quality/artifacts/runs/output.json",
		".denova/automations/inbox.json.bak",
		".denova/index.db.bak",
	} {
		if _, err := safeVisiblePath(workspace, path); err != nil {
			t.Fatalf("included lookalike path %s was rejected: %v", path, err)
		}
	}
}
