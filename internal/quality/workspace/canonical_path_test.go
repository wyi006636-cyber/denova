package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCanonicalPathKeepsLogicalRelativePathInsideCanonicalWorkspace(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, "章节/第一 章.md", "正文")
	alias := filepath.Join(t.TempDir(), "workspace-alias")
	if err := os.Symlink(workspace, alias); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveCanonicalPath(alias, "章节/第一 章.md", CanonicalOptions{})
	if err != nil {
		t.Fatalf("ResolveCanonicalPath: %v", err)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	wantFile := filepath.Join(canonicalWorkspace, "章节", "第一 章.md")
	if resolved.Workspace != canonicalWorkspace || resolved.Absolute != wantFile || resolved.Relative != "章节/第一 章.md" || !resolved.Exists {
		t.Fatalf("resolved = %#v, want workspace=%q absolute=%q", resolved, canonicalWorkspace, wantFile)
	}
}

func TestResolveCanonicalPathAllowsContainedSymlinkAndRejectsEscape(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, "actual/inside.md", "inside")
	if err := os.Symlink("actual", filepath.Join(workspace, "linked")); err != nil {
		t.Fatal(err)
	}
	inside, err := ResolveCanonicalPath(workspace, "linked/inside.md", CanonicalOptions{})
	if err != nil {
		t.Fatalf("contained symlink should resolve: %v", err)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if inside.Relative != "linked/inside.md" || inside.Absolute != filepath.Join(canonicalWorkspace, "actual", "inside.md") {
		t.Fatalf("contained symlink resolution = %#v", inside)
	}

	outside := t.TempDir()
	writeWorkspaceTestFile(t, outside, "secret.md", "outside")
	if err := os.Symlink(outside, filepath.Join(workspace, "escaped")); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveCanonicalPath(workspace, "escaped/secret.md", CanonicalOptions{})
	assertWorkspacePathError(t, err, CodePathEscape, "escaped/secret.md")
	if got, readErr := os.ReadFile(filepath.Join(outside, "secret.md")); readErr != nil || string(got) != "outside" {
		t.Fatalf("outside target changed: content=%q err=%v", got, readErr)
	}
}

func TestResolveCanonicalPathRejectsSymlinkLoop(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Symlink("b", filepath.Join(workspace, "a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", filepath.Join(workspace, "b")); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveCanonicalPath(workspace, "a/file.md", CanonicalOptions{})
	assertWorkspacePathError(t, err, CodePathSymlinkLoop, "a/file.md")
}

func TestResolveCanonicalPathValidatesExistingPrefixForMissingDestination(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "safe"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveCanonicalPath(workspace, "safe/new/file.md", CanonicalOptions{AllowMissing: true})
	if err != nil {
		t.Fatalf("missing contained destination: %v", err)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Exists || resolved.Absolute != filepath.Join(canonicalWorkspace, "safe", "new", "file.md") {
		t.Fatalf("missing destination resolution = %#v", resolved)
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "unsafe")); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveCanonicalPath(workspace, "unsafe/new/file.md", CanonicalOptions{AllowMissing: true})
	assertWorkspacePathError(t, err, CodePathEscape, "unsafe/new/file.md")
}

func assertWorkspacePathError(t *testing.T, err error, code ErrorCode, path string) *PathError {
	t.Helper()
	var pathErr *PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error = %T %v, want *PathError", err, err)
	}
	if pathErr.Code != code || pathErr.Path != path || pathErr.Value == nil {
		t.Fatalf("PathError = %#v, want code=%q path=%q with value", pathErr, code, path)
	}
	return pathErr
}

func writeWorkspaceTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
