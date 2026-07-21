package skilldiscovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheInitializeRejectsExistingUnownedRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("do not claim"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Cache{Root: root}).Initialize(); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("Initialize() error = %v, want ownership rejection", err)
	}
}

func TestCacheInitializeRejectsMismatchedOwnershipMarker(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "OWNER"), []byte("other-cache/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Cache{Root: root}).Initialize(); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("Initialize() error = %v, want ownership rejection", err)
	}
}

func TestCacheInitializeCreatesAndPreservesOwnershipMarker(t *testing.T) {
	cache := Cache{Root: filepath.Join(t.TempDir(), "new")}
	if err := cache.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := cache.Initialize(); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(cache.Root, "OWNER"))
	if err != nil || string(marker) != cacheOwnershipMarker+"\n" {
		t.Fatalf("marker=%q err=%v", marker, err)
	}
}
