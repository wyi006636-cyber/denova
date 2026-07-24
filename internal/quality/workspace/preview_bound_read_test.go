package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHashBoundPreviewFileRejectsParentSwapToExternalSymlink(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, "safe/file.md", "inside")
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	expected, err := root.Lstat(filepath.FromSlash("safe/file.md"))
	if err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	writeWorkspaceTestFile(t, outside, "file.md", "outside secret")
	if err := os.RemoveAll(filepath.Join(workspace, "safe")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "safe")); err != nil {
		t.Fatal(err)
	}

	_, _, _, err = hashBoundPreviewFile(root, "safe/file.md", expected)
	var pathErr *PathError
	if !errors.As(err, &pathErr) || pathErr.Code != CodePathCanonical {
		t.Fatalf("hash error = %T %v, want canonical *PathError", err, err)
	}
	if got, readErr := os.ReadFile(filepath.Join(outside, "file.md")); readErr != nil || string(got) != "outside secret" {
		t.Fatalf("outside file changed: got=%q err=%v", got, readErr)
	}
}

func TestHashBoundPreviewFileRejectsSamePathIdentityReplacement(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, "file.md", "first inode")
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	expected, err := root.Lstat("file.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(workspace, "file.md"), filepath.Join(workspace, "old.md")); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceTestFile(t, workspace, "file.md", "second inode")

	_, _, _, err = hashBoundPreviewFile(root, "file.md", expected)
	var pathErr *PathError
	if !errors.As(err, &pathErr) || pathErr.Code != CodePathIdentityChanged {
		t.Fatalf("hash error = %T %v, want identity-change *PathError", err, err)
	}
}
