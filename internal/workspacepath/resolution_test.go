package workspacepath

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveRootsPinsActiveRootAndReportsTargetDivergence(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, DataDirName, "lore", "items.json"), `{"items":[{"id":"current"}]}`)
	writeFile(t, filepath.Join(workspace, LegacyDataDirName, "profile-lock.json"), `{"profile":"legacy"}`)

	resolution, err := ResolveRoots(workspace, "profile-lock.json", "lore/items.json", "profile-lock.json")
	if err != nil {
		t.Fatalf("ResolveRoots: %v", err)
	}

	want := RootResolution{
		ActiveRoot: DataDirName,
		Targets: []TargetResolution{
			{Path: "lore/items.json", Root: DataDirName},
			{Path: "profile-lock.json", Root: LegacyDataDirName},
		},
	}
	if !reflect.DeepEqual(resolution, want) {
		t.Fatalf("unexpected pinned resolution:\nwant=%#v\n got=%#v", want, resolution)
	}
	if resolution.Consistent() {
		t.Fatal("target-specific legacy resolution must report split-root divergence")
	}
}

func TestResolveRootsKeepsExistingLegacySelectionRules(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, DataDirName, "runs", "run.jsonl"), `{"type":"run_created"}`)
	writeFile(t, filepath.Join(workspace, LegacyDataDirName, "lore", "items.json"), `{"items":[{"id":"legacy"}]}`)

	resolution, err := ResolveRoots(workspace, "lore/items.json")
	if err != nil {
		t.Fatalf("ResolveRoots: %v", err)
	}

	if resolution.ActiveRoot != LegacyDataDirName {
		t.Fatalf("existing workspace rule should pin legacy root, got %q", resolution.ActiveRoot)
	}
	if !resolution.Consistent() {
		t.Fatalf("target resolution should agree with pinned root: %#v", resolution)
	}
}

func TestResolveRootsRejectsReparseTargetWithoutReadingOutside(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, DataDirName, "lore", "items.json"), `{"items":[{"id":"current"}]}`)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.md"), "outside secret")
	if err := os.MkdirAll(filepath.Join(workspace, LegacyDataDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, LegacyDataDirName, "styles")); err != nil {
		t.Fatal(err)
	}

	resolution, err := ResolveRoots(workspace, "styles/secret.md")
	if err == nil {
		t.Fatalf("reparse target must block strict root resolution: %#v", resolution)
	}
	if resolution.ActiveRoot != DataDirName {
		t.Fatalf("safe partial resolution should retain current active root: %#v", resolution)
	}
	data, readErr := os.ReadFile(filepath.Join(outside, "secret.md"))
	if readErr != nil || string(data) != "outside secret" {
		t.Fatalf("outside target changed: data=%q err=%v", data, readErr)
	}
}

func TestResolveRootsRejectsReparseDataRootWithoutReadingOutside(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, DataDirName, "lore", "items.json"), `{"items":[{"id":"current"}]}`)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "lore", "items.json"), `{"items":[{"id":"outside"}]}`)
	if err := os.Symlink(outside, filepath.Join(workspace, LegacyDataDirName)); err != nil {
		t.Fatal(err)
	}

	resolution, err := ResolveRoots(workspace, "lore/items.json")
	if err == nil {
		t.Fatalf("reparse data root must block strict root resolution: %#v", resolution)
	}
	var resolutionErr *ResolutionError
	if !errors.As(err, &resolutionErr) || resolutionErr.Path != LegacyDataDirName {
		t.Fatalf("structured root error = %#v, err=%v", resolutionErr, err)
	}
	if resolution.ActiveRoot != DataDirName {
		t.Fatalf("safe partial resolution should retain current active root: %#v", resolution)
	}
}
