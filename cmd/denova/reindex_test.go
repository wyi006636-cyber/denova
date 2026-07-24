package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/internal/buildinfo"
)

const reindexTestWorkspaceMarker = `{
  "schema_version": 1,
  "reader": {"min_schema_version": 1, "max_schema_version": 1, "min_denova_version": "1.0.0"},
  "writer": {"schema_version": 1, "min_denova_version": "1.0.0", "compatibility_range": ">=1.0.0 <2.0.0", "version": "1.0.0"},
  "features": {
    "fts_projection": {"version": "1.0.0", "required": false},
    "quality_harness": {"version": "1.1.0", "required": true}
  },
  "migration": {"state": "not_required"}
}
`

func TestReindexCommandRebuildsFromZeroAndBypassesApplicationStartup(t *testing.T) {
	setReindexTestBuildVersion(t, "1.6.2")
	workspace := writeReindexTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	applicationStarts := 0
	exitCode := runCommand(context.Background(), []string{"reindex", "--workspace", workspace}, &stdout, &stderr, func() {
		applicationStarts++
	})
	if exitCode != 0 || applicationStarts != 0 {
		t.Fatalf("runCommand exit=%d application_starts=%d stderr=%q", exitCode, applicationStarts, stderr.String())
	}
	canonical, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(canonical, ".denova", "index.db")
	if info, err := os.Stat(databasePath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("reindexed database info=%v err=%v", info, err)
	}
	output := stdout.String()
	for _, want := range []string{
		"投影重建完成",
		"Projection rebuilt",
		"工作区 / Workspace:",
		"数据库 / Database: " + databasePath,
		"文档 / Documents: 2",
		"源快照 / Source snapshot:",
		"SQLite:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("reindex output %q does not contain %q", output, want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("successful reindex stderr = %q", stderr.String())
	}
}

func TestReindexCommandBlocksBelowSchemaV1BuildWithoutStartingApplication(t *testing.T) {
	setReindexTestBuildVersion(t, "0.3.0")
	workspace := writeReindexTestWorkspace(t)
	var stdout, stderr bytes.Buffer
	applicationStarts := 0
	exitCode := runCommand(context.Background(), []string{"reindex", "--workspace", workspace}, &stdout, &stderr, func() {
		applicationStarts++
	})
	if exitCode != 1 || applicationStarts != 0 {
		t.Fatalf("runCommand exit=%d application_starts=%d stdout=%q stderr=%q", exitCode, applicationStarts, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "application_version") || !strings.Contains(stderr.String(), "0.3.0") {
		t.Fatalf("below-minimum diagnostic = %q", stderr.String())
	}
	if _, err := os.Lstat(filepath.Join(workspace, ".denova", "index.db")); !os.IsNotExist(err) {
		t.Fatalf("below-minimum build created Projection: %v", err)
	}
}

func writeReindexTestWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	markerPath := filepath.Join(workspace, ".denova", "workspace-schema.json")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, []byte(reindexTestWorkspaceMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	chapterPath := filepath.Join(workspace, "chapters", "ch1.md")
	if err := os.MkdirAll(filepath.Dir(chapterPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chapterPath, []byte("离线重建 offline rebuild source"), 0o644); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func setReindexTestBuildVersion(t *testing.T, version string) {
	t.Helper()
	previous := buildinfo.Version
	buildinfo.Version = version
	t.Cleanup(func() { buildinfo.Version = previous })
}

func TestReindexCommandRequiresWorkspaceWithoutStartingApplication(t *testing.T) {
	var stdout, stderr bytes.Buffer
	applicationStarts := 0
	exitCode := runCommand(context.Background(), []string{"reindex"}, &stdout, &stderr, func() {
		applicationStarts++
	})
	if exitCode != 2 || applicationStarts != 0 {
		t.Fatalf("runCommand exit=%d application_starts=%d", exitCode, applicationStarts)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "--workspace") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunCommandDelegatesNonOfflineArgumentsToApplication(t *testing.T) {
	applicationStarts := 0
	exitCode := runCommand(context.Background(), []string{"--no-open"}, &bytes.Buffer{}, &bytes.Buffer{}, func() {
		applicationStarts++
	})
	if exitCode != 0 || applicationStarts != 1 {
		t.Fatalf("runCommand exit=%d application_starts=%d", exitCode, applicationStarts)
	}
}
