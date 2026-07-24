package workspace

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func TestBuildMigrationPreviewIsStableAndZeroWriteForWorkspaceKinds(t *testing.T) {
	tests := []struct {
		name       string
		kind       WorkspaceKind
		sourceRoot string
		build      func(*testing.T, string)
		assert     func(*testing.T, MigrationPreview)
	}{
		{
			name:       "new workspace",
			kind:       WorkspaceKindNew,
			sourceRoot: "",
			build: func(t *testing.T, workspace string) {
				writeWorkspaceTestFile(t, workspace, "ideas.md", "一个新想法")
				writeWorkspaceTestFile(t, workspace, "chapters/很长的中文章节 名称.md", "正文")
			},
			assert: func(t *testing.T, preview MigrationPreview) {
				entry := previewEntryBySource(t, preview, "chapters/很长的中文章节 名称.md")
				if entry.Destination != entry.Source || entry.SourceCategory != CategoryFormalAuthoritative || entry.VersionBefore != VersionInclude || entry.VersionAfter != VersionInclude || entry.SHA256 == "" {
					t.Fatalf("new workspace entry = %#v", entry)
				}
			},
		},
		{
			name:       "current workspace without marker",
			kind:       WorkspaceKindCurrent,
			sourceRoot: ".denova",
			build: func(t *testing.T, workspace string) {
				writeWorkspaceTestFile(t, workspace, ".denova/lore/items.json", `{"items":[]}`)
				writeWorkspaceTestFile(t, workspace, ".denova/runs/run.jsonl", `{"event":"runtime"}`)
				writeWorkspaceTestFile(t, workspace, ".denova/cache/context.bin", "cache")
			},
			assert: func(t *testing.T, preview MigrationPreview) {
				runtimeEntry := previewEntryBySource(t, preview, ".denova/runs/run.jsonl")
				projectionEntry := previewEntryBySource(t, preview, ".denova/cache/context.bin")
				if runtimeEntry.SourceCategory != CategoryRuntimeRecovery || runtimeEntry.VersionBefore != VersionExclude || runtimeEntry.VersionAfter != VersionExclude {
					t.Fatalf("runtime entry = %#v", runtimeEntry)
				}
				if projectionEntry.SourceCategory != CategoryRebuildableProjection || projectionEntry.VersionBefore != VersionExclude || projectionEntry.VersionAfter != VersionExclude {
					t.Fatalf("projection entry = %#v", projectionEntry)
				}
			},
		},
		{
			name:       "legacy workspace",
			kind:       WorkspaceKindLegacy,
			sourceRoot: ".nova",
			build: func(t *testing.T, workspace string) {
				writeWorkspaceTestFile(t, workspace, "ideas.md", "legacy idea")
				writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[{"id":"legacy"}]}`)
				writeWorkspaceTestFile(t, workspace, ".nova/runs/run.jsonl", `{"event":"runtime"}`)
			},
			assert: func(t *testing.T, preview MigrationPreview) {
				lore := previewEntryBySource(t, preview, ".nova/lore/items.json")
				runtimeEntry := previewEntryBySource(t, preview, ".nova/runs/run.jsonl")
				if lore.Destination != ".denova/lore/items.json" || lore.SourceCategory != CategoryFormalAuthoritative || lore.DestinationCategory != CategoryFormalAuthoritative || lore.VersionChange != VersionPolicyUnchanged {
					t.Fatalf("legacy lore entry = %#v", lore)
				}
				if runtimeEntry.Destination != ".denova/runs/run.jsonl" || runtimeEntry.VersionBefore != VersionExclude || runtimeEntry.VersionAfter != VersionExclude {
					t.Fatalf("legacy runtime entry = %#v", runtimeEntry)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			test.build(t, workspace)
			before := workspaceTreeDigest(t, workspace)

			first, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
			if err != nil {
				t.Fatalf("BuildMigrationPreview: %v", err)
			}
			second, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
			if err != nil {
				t.Fatalf("second BuildMigrationPreview: %v", err)
			}
			after := workspaceTreeDigest(t, workspace)

			if !reflect.DeepEqual(before, after) {
				t.Fatalf("preview changed workspace tree:\nbefore=%#v\n after=%#v", before, after)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("preview is not deterministic:\nfirst=%#v\nsecond=%#v", first, second)
			}
			firstJSON, err := json.Marshal(first)
			if err != nil {
				t.Fatal(err)
			}
			secondJSON, err := json.Marshal(second)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(firstJSON, secondJSON) {
				t.Fatalf("serialized manifest is unstable:\n%s\n%s", firstJSON, secondJSON)
			}
			if first.Kind != test.kind || first.SourceRoot != test.sourceRoot || first.TargetRoot != ".denova" || first.TargetSchemaVersion != 1 || first.CurrentSchemaVersion != 0 {
				t.Fatalf("preview identity = %#v", first)
			}
			if first.HasConflicts() {
				t.Fatalf("unexpected conflicts: %#v", first.Conflicts)
			}
			if err := first.RequireConflictFree(); err != nil {
				t.Fatalf("RequireConflictFree: %v", err)
			}
			if !sort.SliceIsSorted(first.Entries, func(i, j int) bool { return first.Entries[i].Source < first.Entries[j].Source }) {
				t.Fatalf("entries are not sorted: %#v", first.Entries)
			}
			if !sort.SliceIsSorted(first.Operations, func(i, j int) bool {
				if first.Operations[i].Destination != first.Operations[j].Destination {
					return first.Operations[i].Destination < first.Operations[j].Destination
				}
				return first.Operations[i].Source < first.Operations[j].Source
			}) {
				t.Fatalf("operations are not sorted: %#v", first.Operations)
			}
			markerOperation := false
			for _, operation := range first.Operations {
				switch operation.Kind {
				case OperationPreserve, OperationCopyToCurrentRoot:
				case OperationCreateMarker:
					markerOperation = true
				default:
					t.Fatalf("preview exposed an execution-stage operation: %#v", operation)
				}
			}
			if !markerOperation {
				t.Fatalf("unversioned workspace preview lacks marker plan: %#v", first.Operations)
			}
			test.assert(t, first)
		})
	}
}
