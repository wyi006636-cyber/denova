package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectMissingMarkerKeepsCurrentAndLegacyFormalFilesReadable(t *testing.T) {
	tests := []struct {
		name     string
		dataRoot string
	}{
		{name: "current", dataRoot: ".denova"},
		{name: "legacy", dataRoot: ".nova"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			formalPath := filepath.Join(workspace, test.dataRoot, "lore", "items.json")
			want := []byte(`{"items":[{"name":"原样保留"}]}`)
			if err := os.MkdirAll(filepath.Dir(formalPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(formalPath, want, 0o644); err != nil {
				t.Fatal(err)
			}

			inspection, err := newSchemaV1Inspector(t, "1.3.0").Inspect(workspace)
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if inspection.Marker.Present || inspection.Mode != ModeSafeReadOpen || inspection.CanManagedMutate() {
				t.Fatalf("missing-marker inspection = %#v", inspection)
			}
			issue := requireIssue(t, inspection, CodeMarkerMissing)
			if issue.Field != "marker" || issue.Value != "missing" {
				t.Fatalf("missing marker issue = %#v", issue)
			}
			got, readErr := os.ReadFile(formalPath)
			if readErr != nil || !bytes.Equal(got, want) {
				t.Fatalf("formal file changed or unreadable: got=%q err=%v", got, readErr)
			}
		})
	}
}

func TestInspectPinsOneRootAndBlocksTargetResolutionDivergence(t *testing.T) {
	workspace := t.TempDir()
	writeSchemaMarker(t, workspace, newSchemaV1Marker(t, nil))
	writeWorkspaceTestFile(t, workspace, ".denova/lore/items.json", "current lore")
	writeWorkspaceTestFile(t, workspace, ".nova/styles/legacy.md", "legacy style")

	inspection, err := newSchemaV1Inspector(t, "1.5.0").Inspect(workspace)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if inspection.ActiveRoot != ".denova" || inspection.RootResolution.Consistent() || inspection.CanManagedMutate() {
		t.Fatalf("root inspection = %#v", inspection)
	}
	var divergencePaths []string
	for _, issue := range inspection.Issues {
		if issue.Code == CodeRootResolutionDivergence {
			if issue.Field != "target_resolution" || issue.Value == nil {
				t.Fatalf("root divergence issue = %#v", issue)
			}
			divergencePaths = append(divergencePaths, issue.Path)
		}
	}
	wantDivergence := []string{".nova/styles", ".nova/styles/legacy.md"}
	if !reflect.DeepEqual(divergencePaths, wantDivergence) {
		t.Fatalf("root divergence paths = %#v, want %#v", divergencePaths, wantDivergence)
	}
}

func TestInspectTreatsLegacyV1LookingNamesAsProtectedConflicts(t *testing.T) {
	workspace := t.TempDir()
	writeSchemaMarker(t, workspace, newSchemaV1Marker(t, nil))
	writeWorkspaceTestFile(t, workspace, ".denova/lore/items.json", "current lore")
	for _, rel := range []string{
		".nova/cache/blob.bin",
		".nova/index.db-wal",
		".nova/profile-lock.json",
		".nova/quality/specs/fake.json",
		".nova/workspace-schema.json",
	} {
		writeWorkspaceTestFile(t, workspace, rel, "legacy bytes")
	}

	inspection, err := newSchemaV1Inspector(t, "1.5.0").Inspect(workspace)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	want := []string{
		".nova/cache",
		".nova/cache/blob.bin",
		".nova/index.db-wal",
		".nova/profile-lock.json",
		".nova/quality",
		".nova/quality/specs",
		".nova/quality/specs/fake.json",
		".nova/workspace-schema.json",
	}
	if !reflect.DeepEqual(inspection.LegacyConflicts, want) {
		t.Fatalf("legacy conflicts:\nwant=%#v\n got=%#v", want, inspection.LegacyConflicts)
	}
	if inspection.CanManagedMutate() {
		t.Fatal("legacy v1-looking inputs must block v1-managed mutation")
	}
	issue := requireIssue(t, inspection, CodeLegacyV1Conflict)
	if issue.Path == "" || issue.Field != "legacy_path" || issue.Value == nil {
		t.Fatalf("legacy conflict issue = %#v", issue)
	}
}

func TestInspectNeverLoadsLegacyMarkerAsV1Authority(t *testing.T) {
	workspace := t.TempDir()
	legacyRaw := []byte(`{"schema_version":1,"writer":{"version":"1.0.0"}}`)
	writeWorkspaceTestFile(t, workspace, ".nova/workspace-schema.json", string(legacyRaw))

	inspection, err := newSchemaV1Inspector(t, "1.5.0").Inspect(workspace)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if inspection.Marker.Present || len(inspection.Marker.RawBytes()) != 0 {
		t.Fatalf("legacy marker was loaded as authority: %#v", inspection.Marker)
	}
	if inspection.ActiveRoot != ".nova" || inspection.CanManagedMutate() {
		t.Fatalf("legacy-only inspection = %#v", inspection)
	}
	requireIssue(t, inspection, CodeMarkerMissing)
	requireIssue(t, inspection, CodeActiveRootUnsupported)
}

func TestInspectBlocksManagedMutationWhenRelevantTargetTraversesReparsePoint(t *testing.T) {
	workspace := t.TempDir()
	writeSchemaMarker(t, workspace, newSchemaV1Marker(t, nil))
	writeWorkspaceTestFile(t, workspace, ".denova/lore/items.json", "current")
	outside := t.TempDir()
	writeWorkspaceTestFile(t, outside, "secret.md", "outside")
	if err := os.MkdirAll(filepath.Join(workspace, ".nova"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, ".nova", "styles")); err != nil {
		t.Fatal(err)
	}
	inspector, err := NewInspector(InspectorOptions{
		ApplicationVersion: "1.5.0",
		SupportedFeatures:  map[string]string{"quality_harness": ">=1.0.0 <2.0.0"},
		RelevantTargets:    []string{"styles/secret.md"},
	})
	if err != nil {
		t.Fatal(err)
	}

	inspection, err := inspector.Inspect(workspace)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if inspection.CanManagedMutate() || inspection.Mode != ModeSafeReadOpen {
		t.Fatalf("reparse target must force safe compatibility: %#v", inspection)
	}
	issue := requireIssue(t, inspection, CodeRootResolutionUnsafe)
	if issue.Field != "root_resolution" || issue.Value == nil {
		t.Fatalf("root resolution issue = %#v", issue)
	}
}
