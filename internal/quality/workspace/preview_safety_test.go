package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestVerifyMigrationPreviewRejectsChangedSourceHashWithoutWriting(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "chapters", "one.md")
	writeWorkspaceTestFile(t, workspace, "chapters/one.md", "before")
	preview, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = VerifyMigrationPreview(preview)
	var stale *PreviewStaleError
	if !errors.As(err, &stale) {
		t.Fatalf("VerifyMigrationPreview error = %T %v, want *PreviewStaleError", err, err)
	}
	if stale.Code != CodePreviewSourceChanged || stale.Path != "chapters/one.md" || stale.Expected == stale.Actual {
		t.Fatalf("stale error = %#v", stale)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != "after" {
		t.Fatalf("verification rewrote changed source: got=%q err=%v", got, readErr)
	}
}

func TestBuildMigrationPreviewReportsCanonicalEscapeAndReparsePoint(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	writeWorkspaceTestFile(t, outside, "secret.md", "outside bytes")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(workspace, "linked.md")); err != nil {
		t.Fatal(err)
	}
	before := workspaceTreeDigest(t, workspace)

	preview, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
	if err != nil {
		t.Fatalf("BuildMigrationPreview: %v", err)
	}
	requirePreviewConflict(t, preview, CodePathEscape)
	requirePreviewConflict(t, preview, CodePreviewReparsePoint)
	if !preview.HasConflicts() || preview.RequireConflictFree() == nil {
		t.Fatalf("unsafe preview must remain conflict-blocked: %#v", preview)
	}
	after := workspaceTreeDigest(t, workspace)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("unsafe preview changed workspace: before=%#v after=%#v", before, after)
	}
	if got, readErr := os.ReadFile(filepath.Join(outside, "secret.md")); readErr != nil || string(got) != "outside bytes" {
		t.Fatalf("outside target changed: got=%q err=%v", got, readErr)
	}
}

func TestBuildMigrationPreviewRejectsUnrepresentableTargetsAndPortableCollisions(t *testing.T) {
	workspace := t.TempDir()
	for _, rel := range []string{
		".nova/lore/CON.txt",
		".nova/lore/name.",
		".nova/lore/stream:ads",
		".nova/lore/这是一个目标文件系统无法表达的超长名称.md",
	} {
		writeWorkspaceTestFile(t, workspace, rel, rel)
	}
	options := schemaV1PreviewOptions()
	options.TargetPlatform = PathPlatformWindows
	options.TargetLimits = PathLimits{MaxPathBytes: 48, MaxSegmentBytes: 32}

	preview, err := BuildMigrationPreview(workspace, options)
	if err != nil {
		t.Fatalf("BuildMigrationPreview: %v", err)
	}
	for _, code := range []ErrorCode{
		CodePathWindowsReserved,
		CodePathWindowsTrailing,
		CodePathWindowsADS,
		CodePathTooLong,
	} {
		requirePreviewConflict(t, preview, code)
	}
	nonNFC := validatePreviewDestination(workspace, ".nova/lore/e\u0301.md", ".denova/lore/e\u0301.md", options)
	if len(nonNFC) == 0 || nonNFC[0].Code != CodePathNormalization {
		t.Fatalf("non-NFC preview destination conflicts = %#v", nonNFC)
	}
	portable := portablePreviewConflicts([]string{
		".denova/lore/Case.md",
		".denova/lore/case.md",
		".denova/lore/é.md",
		".denova/lore/e\u0301.md",
	})
	if len(portable) != 2 {
		t.Fatalf("portable preview collisions = %#v, want case-fold and normalization collisions", portable)
	}
	for _, conflict := range portable {
		if conflict.Code != CodePreviewPortableCollision {
			t.Fatalf("portable conflict = %#v", conflict)
		}
	}
	if !preview.HasConflicts() {
		t.Fatal("unrepresentable targets must block a future migration")
	}
}

func TestBuildMigrationPreviewReadsChineseSpacesAndLongPathsWithoutSyntheticLimit(t *testing.T) {
	workspace := t.TempDir()
	rel := "chapters/这是一个很长但宿主文件系统能够正常表达的中文 章节名称/第一 幕.md"
	writeWorkspaceTestFile(t, workspace, rel, "可读取正文")

	preview, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
	if err != nil {
		t.Fatalf("BuildMigrationPreview: %v", err)
	}
	entry := previewEntryBySource(t, preview, rel)
	if entry.Size == 0 || entry.SHA256 == "" || entry.Destination != rel {
		t.Fatalf("long Chinese entry = %#v", entry)
	}
	if preview.HasConflicts() {
		t.Fatalf("host-supported path was rejected: %#v", preview.Conflicts)
	}
}

func TestPreviewKeepsExistingNonNFCPathReadableButRejectsItAsNewDestination(t *testing.T) {
	workspace := t.TempDir()
	nonNFC := "notes/Cafe\u0301.md"
	options := schemaV1PreviewOptions()

	preserved := validatePreviewDestination(workspace, nonNFC, nonNFC, options)
	if len(preserved) != 0 {
		t.Fatalf("existing non-NFC path should remain readable: %#v", preserved)
	}
	created := validatePreviewDestination(workspace, ".nova/"+nonNFC, ".denova/"+nonNFC, options)
	if len(created) == 0 || created[0].Code != CodePathNormalization {
		t.Fatalf("new non-NFC destination conflicts = %#v", created)
	}
}

func TestBuildMigrationPreviewRequiresValidFutureWriterForMissingMarker(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		code    ErrorCode
	}{
		{name: "missing", version: "", code: CodeApplicationVersionInvalid},
		{name: "outside range", version: "2.0.0", code: CodeApplicationVersionUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeWorkspaceTestFile(t, workspace, "ideas.md", "idea")
			options := schemaV1PreviewOptions()
			options.Inspector.ApplicationVersion = test.version
			preview, err := BuildMigrationPreview(workspace, options)
			if err != nil {
				t.Fatalf("BuildMigrationPreview: %v", err)
			}
			requirePreviewConflict(t, preview, test.code)
		})
	}
}

func TestLegacyV1LookingPreviewPreservesInputsWithoutPromotingAuthority(t *testing.T) {
	workspace := t.TempDir()
	paths := []string{
		".nova/workspace-schema.json",
		".nova/profile-lock.json",
		".nova/quality/specs/fake.json",
		".nova/index.db-wal",
		".nova/cache/blob.bin",
	}
	for _, rel := range paths {
		writeWorkspaceTestFile(t, workspace, rel, "protected:"+rel)
	}

	preview, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
	if err != nil {
		t.Fatalf("BuildMigrationPreview: %v", err)
	}
	requirePreviewConflict(t, preview, CodeLegacyV1Conflict)
	for _, rel := range paths {
		entry := previewEntryBySource(t, preview, rel)
		if entry.Destination != rel || entry.SourceCategory != CategoryProtectedLegacyUnknown || entry.DestinationCategory != CategoryProtectedLegacyUnknown ||
			entry.VersionBefore != VersionInclude || entry.VersionAfter != VersionInclude || entry.VersionChange != VersionPolicyUnchanged {
			t.Fatalf("legacy protected entry was promoted: %#v", entry)
		}
		found := false
		for _, operation := range preview.Operations {
			if operation.Source != rel {
				continue
			}
			found = true
			if operation.Kind != OperationPreserve || operation.Destination != rel {
				t.Fatalf("legacy protected operation was promoted: %#v", operation)
			}
		}
		if !found {
			t.Fatalf("missing preserve operation for %s", rel)
		}
	}
}
