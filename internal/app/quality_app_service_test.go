package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"denova/internal/buildinfo"
	"denova/internal/quality/profile"
	"denova/internal/quality/workspace"
)

func TestQualityCatalogLoadsExactEmbeddedProfilesDeterministicallyAndReturnsClones(t *testing.T) {
	service, err := newQualityAppService(nil)
	if err != nil {
		t.Fatalf("newQualityAppService: %v", err)
	}

	first, err := service.Profiles()
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	wantIDs := []string{"long_serial", "fanqie_short", "zhihu_salt_short"}
	wantHashes := []string{
		"47761b8de3b05a29e67475b2eff0d611f50c1f9379c35be28570305cdbbe2b23",
		"6db51f1971529d5b68246417aa4555b4ab1fa754113332923e883093a599208d",
		"ef780b82470d97d27675455f963e4a3ee4aa67b7c31aad3eca506d595bbc3663",
	}
	if got := profileIDs(first); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("profile order = %#v, want %#v", got, wantIDs)
	}
	for index := range first {
		if first[index].SourceSHA256 != wantHashes[index] || first[index].AccessMode != "read_only_catalog" {
			t.Fatalf("profile[%d] metadata = %#v", index, first[index])
		}
		if first[index].Summary.Zh == "" || first[index].Summary.En == "" {
			t.Fatalf("profile[%d] summary is not bilingual: %#v", index, first[index].Summary)
		}
	}
	first[0].Summary.En = "mutated"
	second, err := service.Profiles()
	if err != nil {
		t.Fatalf("Profiles again: %v", err)
	}
	if second[0].Summary.En == "mutated" {
		t.Fatal("Profiles leaked shared mutable state")
	}

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	third, err := service.Profiles()
	if err != nil {
		t.Fatalf("Profiles after cwd change from %q: %v", original, err)
	}
	if !reflect.DeepEqual(profileIDs(third), wantIDs) {
		t.Fatalf("cwd changed catalog: %#v", profileIDs(third))
	}
}

func TestQualityCatalogRejectsMalformedUnknownNewerAndOversizedAssets(t *testing.T) {
	valid := defaultQualityCatalogAssets()
	tests := []struct {
		name   string
		mutate func(*qualityCatalogAssets)
	}{
		{"malformed schema", func(assets *qualityCatalogAssets) { assets.profileSchema = []byte("{") }},
		{"unknown profile", func(assets *qualityCatalogAssets) {
			assets.profiles[0] = bytes.Replace(assets.profiles[0], []byte(`"long_serial"`), []byte(`"unknown_profile"`), 1)
		}},
		{"newer contract", func(assets *qualityCatalogAssets) {
			assets.profiles[0] = bytes.Replace(assets.profiles[0], []byte(`"version": "v1"`), []byte(`"version": "v2"`), 1)
		}},
		{"oversized source", func(assets *qualityCatalogAssets) {
			assets.profiles[0] = bytes.Repeat([]byte("x"), maxQualityProfileSourceBytes+1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assets := valid.clone()
			test.mutate(&assets)
			_, err := newQualityAppService(&assets)
			var appErr *QualityAppError
			if !errors.As(err, &appErr) || appErr.Code != QualityCodeAssetsUnavailable {
				t.Fatalf("error = %T %v, want %s", err, err, QualityCodeAssetsUnavailable)
			}
		})
	}
}

func TestQualityProfileDTOsExposeBoundSpecContractVersion(t *testing.T) {
	service, err := newQualityAppService(nil)
	if err != nil {
		t.Fatal(err)
	}
	list, err := service.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.QualitySpec.ContractVersion != "v1" {
			t.Fatalf("list Profile %q QualitySpec contract version = %q, want v1", item.ProfileID, item.QualitySpec.ContractVersion)
		}
	}
	detail, err := service.Profile("long_serial")
	if err != nil {
		t.Fatal(err)
	}
	if detail.QualitySpec.ContractVersion != "v1" {
		t.Fatalf("detail QualitySpec contract version = %q, want v1", detail.QualitySpec.ContractVersion)
	}
}

func TestQualityProfileDetailMetadataUnknownAndSerializedCap(t *testing.T) {
	service, err := newQualityAppService(nil)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.Profile("long_serial")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if detail.ProfileID != "long_serial" || detail.ContractVersion != "v1" || detail.QualitySpec.SpecID != "qs-long-serial-chapter-12" || detail.QualitySpec.Revision != 1 || len(detail.QualitySpec.SHA256) != sha256.Size*2 || detail.Profile == nil {
		t.Fatalf("detail metadata = %#v", detail)
	}
	payload, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > maxQualityProfileDetailBytes {
		t.Fatalf("serialized detail = %d bytes", len(payload))
	}
	detail.Profile.DisplayName.En = "mutated"
	again, err := service.Profile("long_serial")
	if err != nil {
		t.Fatal(err)
	}
	if again.Profile.DisplayName.En == "mutated" {
		t.Fatal("Profile detail leaked shared mutable state")
	}
	_, err = service.Profile("unknown_profile")
	var appErr *QualityAppError
	if !errors.As(err, &appErr) || appErr.Code != QualityCodeProfileNotFound {
		t.Fatalf("unknown error = %T %v", err, err)
	}

	item := service.items["long_serial"]
	item.profile.Settings.RequiredArtifacts = make([]profile.Setting, 12000)
	for index := range item.profile.Settings.RequiredArtifacts {
		item.profile.Settings.RequiredArtifacts[index].ID = strings.Repeat("x", 32)
	}
	service.items["long_serial"] = item
	_, err = service.Profile("long_serial")
	if !errors.As(err, &appErr) || appErr.Code != QualityCodeAssetsUnavailable {
		t.Fatalf("oversized detail error = %T %v", err, err)
	}
}

func TestQualityProjectInspectionKeepsCompatibilityFactsBoundedAndPrivate(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeQualityMarker(t, workspaceRoot, map[string]any{
		"quality_harness":       map[string]any{"version": "1.0.0", "required": true},
		"future_secret_feature": map[string]any{"version": "9.0.0", "required": true},
	})
	application := &App{workspace: workspaceRoot}
	project, err := application.QualityProject()
	if err != nil {
		t.Fatalf("QualityProject: %v", err)
	}
	if project.ResourceID != "current" || project.Mode != workspace.ModeSafeReadOpen || project.ManagedMutation != workspace.MutationBlocked {
		t.Fatalf("project = %#v", project)
	}
	if !qualityIssueCodes(project.Issues, string(workspace.CodeApplicationVersionInvalid), string(workspace.CodeFeatureRequiredUnsupported)) {
		t.Fatalf("issues = %#v", project.Issues)
	}
	payload, err := json.Marshal(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{workspaceRoot, "future_secret_feature=9.0.0", "wrapped internal"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("project leaked %q: %s", secret, payload)
		}
	}

	noWorkspace := &App{}
	_, err = noWorkspace.QualityProject()
	var appErr *QualityAppError
	if !errors.As(err, &appErr) || appErr.Code != QualityCodeNoWorkspace {
		t.Fatalf("no workspace error = %T %v", err, err)
	}
}

func TestQualityPublicDTOProjectionsRejectCrossPlatformUnsafePaths(t *testing.T) {
	invalidPaths := []struct {
		name string
		path string
	}{
		{name: "drive slash", path: "C:/workspace/chapter.md"},
		{name: "drive backslash", path: `C:\workspace\chapter.md`},
		{name: "drive relative", path: `C:workspace\chapter.md`},
		{name: "UNC", path: `\\server\share\chapter.md`},
		{name: "leading slash", path: "/etc/passwd"},
		{name: "leading backslash", path: `\etc\passwd`},
		{name: "backslash traversal", path: `chapters\..\secret.md`},
		{name: "embedded traversal", path: "chapters/../../secret.md"},
		{name: "NUL", path: "chapters/\x00secret.md"},
		{name: "empty segment", path: "chapters//secret.md"},
		{name: "current segment", path: "chapters/./secret.md"},
		{name: "parent segment", path: "chapters/../secret.md"},
		{name: "trailing separator", path: "chapters/"},
		{name: "backslash separator", path: `chapters\01.md`},
		{name: "surrounding whitespace", path: " chapters/01.md"},
	}

	for _, test := range invalidPaths {
		t.Run(test.name, func(t *testing.T) {
			project := qualityProjectProjection(workspace.Inspection{
				ActiveRoot:      test.path,
				Issues:          []workspace.CompatibilityIssue{{Code: "unsafe_path", Path: test.path}},
				LegacyConflicts: []string{test.path},
			})
			if project.ActiveRoot != "" || project.Issues[0].Path != "" || len(project.LegacyConflicts) != 0 {
				t.Fatalf("project exposed unsafe path %q: %#v", test.path, project)
			}

			preview := projectQualityPreview(workspace.MigrationPreview{
				SourceRoot: test.path,
				TargetRoot: test.path,
				Entries: []workspace.PreviewEntry{{
					Source: test.path, Destination: test.path,
				}},
				Operations: []workspace.PreviewOperation{{
					Source: test.path, Destination: test.path,
				}},
				Conflicts: []workspace.PreviewConflict{{
					Path: test.path, Destination: test.path,
				}},
			})
			if preview.SourceRoot != "" || preview.TargetRoot != "" ||
				preview.Entries[0].Source != "" || preview.Entries[0].Destination != "" ||
				preview.Operations[0].Source != "" || preview.Operations[0].Destination != "" ||
				preview.Conflicts[0].Path != "" || preview.Conflicts[0].Destination != "" {
				t.Fatalf("preview exposed unsafe path %q: %#v", test.path, preview)
			}
		})
	}
}

func TestQualityPublicDTOProjectionsPreserveNormalizedRelativePaths(t *testing.T) {
	validPaths := []string{
		"chapters/01.md",
		"setting/world map.md",
		".nova/lore/items.json",
		".denova/workspace-schema.json",
	}
	for _, validPath := range validPaths {
		t.Run(validPath, func(t *testing.T) {
			project := qualityProjectProjection(workspace.Inspection{
				ActiveRoot:      validPath,
				Issues:          []workspace.CompatibilityIssue{{Code: "path", Path: validPath}},
				LegacyConflicts: []string{validPath},
			})
			if project.ActiveRoot != validPath || project.Issues[0].Path != validPath || !reflect.DeepEqual(project.LegacyConflicts, []string{validPath}) {
				t.Fatalf("project changed normalized relative path %q: %#v", validPath, project)
			}

			preview := projectQualityPreview(workspace.MigrationPreview{
				SourceRoot: validPath,
				TargetRoot: validPath,
				Entries:    []workspace.PreviewEntry{{Source: validPath, Destination: validPath}},
				Operations: []workspace.PreviewOperation{{Source: validPath, Destination: validPath}},
				Conflicts:  []workspace.PreviewConflict{{Path: validPath, Destination: validPath}},
			})
			if preview.SourceRoot != validPath || preview.TargetRoot != validPath ||
				preview.Entries[0].Source != validPath || preview.Entries[0].Destination != validPath ||
				preview.Operations[0].Source != validPath || preview.Operations[0].Destination != validPath ||
				preview.Conflicts[0].Path != validPath || preview.Conflicts[0].Destination != validPath {
				t.Fatalf("preview changed normalized relative path %q: %#v", validPath, preview)
			}
		})
	}
}

func TestQualityMigrationPreviewPagesCompleteSafeProjectionAndWritesNothing(t *testing.T) {
	workspaceRoot := t.TempDir()
	for _, item := range []struct{ path, body string }{
		{"chapters/02.md", "two"},
		{"chapters/01.md", "one"},
		{"setting/secret.md", "do-not-return-bytes"},
	} {
		path := filepath.Join(workspaceRoot, filepath.FromSlash(item.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(item.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := qualityTreeSnapshot(t, workspaceRoot)
	application := &App{workspace: workspaceRoot}
	first, err := application.QualityMigrationPreview(QualityMigrationPreviewRequest{})
	if err != nil {
		t.Fatalf("preview defaults: %v", err)
	}
	second, err := application.QualityMigrationPreview(QualityMigrationPreviewRequest{Offset: 1, Limit: 1})
	if err != nil {
		t.Fatalf("preview page: %v", err)
	}
	after := qualityTreeSnapshot(t, workspaceRoot)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("preview wrote workspace\nbefore=%#v\nafter=%#v", before, after)
	}
	if first.Digest == "" || first.Digest != second.Digest || first.Totals != second.Totals {
		t.Fatalf("digest/totals changed across pages: first=%#v second=%#v", first, second)
	}
	if second.Page.Offset != 1 || second.Page.Limit != 1 || len(second.Entries.Items) != 1 || !second.Entries.Truncated {
		t.Fatalf("page = %#v", second)
	}
	payload, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), workspaceRoot) || strings.Contains(string(payload), "do-not-return-bytes") {
		t.Fatalf("preview leaked path/content: %s", payload)
	}

	for _, request := range []QualityMigrationPreviewRequest{{Offset: -1, Limit: 1}, {Limit: -1}, {Limit: 501}} {
		_, err := application.QualityMigrationPreview(request)
		var appErr *QualityAppError
		if !errors.As(err, &appErr) || appErr.Code != QualityCodeInvalidRequest {
			t.Fatalf("request %#v error = %T %v", request, err, err)
		}
	}
}

func TestQualityProjectCoversCurrentLegacySplitAndMarkerCompatibilityShapes(t *testing.T) {
	previousVersion := buildinfo.Version
	buildinfo.Version = "1.7.0"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	tests := []struct {
		name      string
		arrange   func(*testing.T, string)
		wantMode  workspace.CompatibilityMode
		wantCodes []string
	}{
		{
			name: "managed current",
			arrange: func(t *testing.T, root string) {
				writeQualityMarker(t, root, map[string]any{"quality_harness": map[string]any{"version": "1.0.0", "required": true}, "fts_projection": map[string]any{"version": "1.0.0", "required": false}})
			},
			wantMode: workspace.ModeManagedV1,
		},
		{
			name:     "legacy only",
			arrange:  func(t *testing.T, root string) { writeQualityTestFile(t, root, ".nova/lore/items.json", "legacy") },
			wantMode: workspace.ModeSafeReadOpen, wantCodes: []string{string(workspace.CodeActiveRootUnsupported), string(workspace.CodeMarkerMissing)},
		},
		{
			name: "split root",
			arrange: func(t *testing.T, root string) {
				writeQualityTestFile(t, root, ".denova/lore/items.json", "current")
				writeQualityTestFile(t, root, ".nova/lore/items.json", "legacy")
			},
			wantMode: workspace.ModeSafeReadOpen,
		},
		{
			name: "unknown required feature",
			arrange: func(t *testing.T, root string) {
				writeQualityMarker(t, root, map[string]any{"future": map[string]any{"version": "1.0.0", "required": true}})
			},
			wantMode: workspace.ModeSafeReadOpen, wantCodes: []string{string(workspace.CodeFeatureRequiredUnsupported)},
		},
		{
			name: "newer marker",
			arrange: func(t *testing.T, root string) {
				writeQualityMarker(t, root, map[string]any{"quality_harness": map[string]any{"version": "1.0.0", "required": true}})
				path := filepath.Join(root, ".denova", "workspace-schema.json")
				raw, _ := os.ReadFile(path)
				raw = bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1)
				if err := os.WriteFile(path, raw, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantMode: workspace.ModeSafeReadOpen, wantCodes: []string{string(workspace.CodeSchemaNewer)},
		},
		{
			name: "malformed marker",
			arrange: func(t *testing.T, root string) {
				writeQualityTestFile(t, root, ".denova/workspace-schema.json", `{secret`)
			},
			wantMode: workspace.ModeSafeReadOpen, wantCodes: []string{string(workspace.CodeMarkerMalformed)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.arrange(t, root)
			project, err := (&App{workspace: root}).QualityProject()
			if err != nil {
				t.Fatalf("QualityProject: %v", err)
			}
			if project.Mode != test.wantMode {
				t.Fatalf("mode=%q issues=%#v, want %q", project.Mode, project.Issues, test.wantMode)
			}
			if !qualityIssueCodes(project.Issues, test.wantCodes...) {
				t.Fatalf("issues=%#v, want codes=%#v", project.Issues, test.wantCodes)
			}
			payload, err := json.Marshal(project)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(payload), root) || strings.Contains(string(payload), "{secret") {
				t.Fatalf("project leaked private data: %s", payload)
			}
		})
	}
}

func TestQualityProjectIssuesAreBoundedAndDeterministicallyOrdered(t *testing.T) {
	issues := make([]workspace.CompatibilityIssue, 0, 110)
	for index := 109; index >= 0; index-- {
		issues = append(issues, workspace.CompatibilityIssue{Code: workspace.ErrorCode(fmt.Sprintf("code_%03d", index)), Path: ".denova/item", Field: "field", Blocking: true})
	}
	project := qualityProjectProjection(workspace.Inspection{Issues: issues})
	if len(project.Issues) != 100 || !project.IssueTruncation.Truncated || project.IssueTruncation.Total != 110 {
		t.Fatalf("truncation = %#v issues=%d", project.IssueTruncation, len(project.Issues))
	}
	if project.Issues[0].Code != "code_000" || project.Issues[99].Code != "code_099" {
		t.Fatalf("issue order = first=%#v last=%#v", project.Issues[0], project.Issues[99])
	}
}

func profileIDs(items []QualityProfileSummary) []string {
	ids := make([]string, len(items))
	for index := range items {
		ids[index] = items[index].ProfileID
	}
	return ids
}

func qualityIssueCodes(issues []QualityIssue, wanted ...string) bool {
	seen := make(map[string]bool, len(issues))
	for _, issue := range issues {
		seen[issue.Code] = true
	}
	for _, code := range wanted {
		if !seen[code] {
			return false
		}
	}
	return true
}

func writeQualityMarker(t *testing.T, root string, features map[string]any) {
	t.Helper()
	marker := map[string]any{
		"schema_version": 1,
		"reader":         map[string]any{"min_schema_version": 1, "max_schema_version": 1, "min_denova_version": "1.0.0"},
		"writer":         map[string]any{"schema_version": 1, "min_denova_version": "1.0.0", "compatibility_range": ">=1.0.0 <2.0.0", "version": "1.1.0"},
		"features":       features,
		"migration":      map[string]any{"state": "not_required"},
	}
	raw, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".denova", "workspace-schema.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeQualityTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func qualityTreeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	items := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			items = append(items, "D "+filepath.ToSlash(rel))
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		items = append(items, "F "+filepath.ToSlash(rel)+" "+hex.EncodeToString(digest[:]))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(items)
	return items
}
