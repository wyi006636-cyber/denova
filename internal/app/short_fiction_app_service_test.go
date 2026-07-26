package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"denova/config"
	"denova/internal/book"
	"denova/internal/session"
	"denova/internal/shortfiction"
	"denova/internal/workspacechange"
)

func TestFanqieGenerateRejectsNonCanonicalAndSymlinkTargetsBeforeModelCall(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		prepare func(*testing.T, string) string
	}{
		{
			name:   "double separator",
			target: "chapters//short.md",
			prepare: func(t *testing.T, workspace string) string {
				content := "canonical target bytes"
				writeShortFictionTestFile(t, workspace, "chapters/short.md", content)
				return workspacechange.Revision([]byte(content))
			},
		},
		{
			name:   "symlinked parent",
			target: "chapters/short.md",
			prepare: func(t *testing.T, workspace string) string {
				content := "parent symlink bytes"
				writeShortFictionTestFile(t, workspace, "real/short.md", content)
				if err := os.Symlink("real", filepath.Join(workspace, "chapters")); err != nil {
					t.Fatal(err)
				}
				return workspacechange.Revision([]byte(content))
			},
		},
		{
			name:   "target symlink to hidden content",
			target: "chapters/short.md",
			prepare: func(t *testing.T, workspace string) string {
				content := "hidden source bytes"
				writeShortFictionTestFile(t, workspace, ".hidden/secret.md", content)
				if err := os.MkdirAll(filepath.Join(workspace, "chapters"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../.hidden/secret.md", filepath.Join(workspace, "chapters", "short.md")); err != nil {
					t.Fatal(err)
				}
				return workspacechange.Revision([]byte(content))
			},
		},
		{
			name:   "dangling target symlink",
			target: "chapters/short.md",
			prepare: func(t *testing.T, workspace string) string {
				if err := os.MkdirAll(filepath.Join(workspace, "chapters"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../missing/secret.md", filepath.Join(workspace, "chapters", "short.md")); err != nil {
					t.Fatal(err)
				}
				return shortfiction.MissingRevision
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
			revision := test.prepare(t, workspace)
			provider := newShortFictionTestProvider(t, "# forbidden candidate")
			application := newShortFictionTestApp(t, workspace, provider.server.URL)
			beforeTree := snapshotShortFictionTestTree(t, workspace)
			beforeVersions := listShortFictionTestVersions(t, application)
			beforeHistory := listShortFictionTestSessionHistory(t, application)

			_, err := application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
				ProfileID: shortfiction.ProfileFanqieShort,
				Source: shortfiction.SourcePacket{
					Workspace:    workspace,
					TargetPath:   test.target,
					BaseRevision: revision,
					Brief:        "never follow aliases or symlinks",
				},
			}, "en-US")
			if err == nil {
				t.Fatal("non-canonical or symlink target was accepted")
			}
			if provider.calls.Load() != 0 {
				t.Fatalf("provider calls = %d, want 0", provider.calls.Load())
			}
			assertShortFictionTestObservationsUnchanged(t, application, workspace, beforeTree, beforeVersions, beforeHistory)
		})
	}
}

func TestFanqieGenerateColdWorkspaceDoesNotWriteMetadataOrHistory(t *testing.T) {
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/short.md"
	content := "cold workspace source"
	writeShortFictionTestFile(t, workspace, path, content)
	provider := newShortFictionTestProvider(t, "# candidate")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	beforeTree := snapshotShortFictionTestTree(t, workspace)
	beforeVersions := listShortFictionTestVersions(t, application)
	beforeHistory := listShortFictionTestSessionHistory(t, application)

	_, err := application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   path,
			BaseRevision: workspacechange.Revision([]byte(content)),
			Brief:        "preview without cold-start metadata",
		},
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls.Load())
	}
	assertShortFictionTestObservationsUnchanged(t, application, workspace, beforeTree, beforeVersions, beforeHistory)
	assertShortFictionTestNoChangeMetadata(t, workspace)
}

func TestFanqieGenerateColdRejectionDoesNotWriteMetadataOrHistory(t *testing.T) {
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	provider := newShortFictionTestProvider(t, "# forbidden candidate")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	beforeTree := snapshotShortFictionTestTree(t, workspace)
	beforeVersions := listShortFictionTestVersions(t, application)
	beforeHistory := listShortFictionTestSessionHistory(t, application)

	_, err := application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
		ProfileID: "unsupported",
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   "chapters/short.md",
			BaseRevision: shortfiction.MissingRevision,
			Brief:        "reject without cold-start metadata",
		},
	}, "en-US")
	if err == nil {
		t.Fatal("unsupported profile was accepted")
	}
	if provider.calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls.Load())
	}
	assertShortFictionTestObservationsUnchanged(t, application, workspace, beforeTree, beforeVersions, beforeHistory)
	assertShortFictionTestNoChangeMetadata(t, workspace)
}

func TestFanqieGenerateRejectsNonCanonicalActiveWorkspaceBeforeModelCall(t *testing.T) {
	canonicalWorkspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	workspaceAlias := canonicalWorkspace + string(filepath.Separator) + "."
	path := "chapters/short.md"
	content := "workspace alias source"
	writeShortFictionTestFile(t, canonicalWorkspace, path, content)
	provider := newShortFictionTestProvider(t, "# forbidden candidate")
	application := newShortFictionTestApp(t, workspaceAlias, provider.server.URL)
	beforeTree := snapshotShortFictionTestTree(t, canonicalWorkspace)
	beforeVersions := listShortFictionTestVersions(t, application)
	beforeHistory := listShortFictionTestSessionHistory(t, application)

	_, err := application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspaceAlias,
			TargetPath:   path,
			BaseRevision: workspacechange.Revision([]byte(content)),
			Brief:        "reject a non-canonical active workspace",
		},
	}, "en-US")
	if err == nil {
		t.Fatal("non-canonical active workspace was accepted")
	}
	if provider.calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls.Load())
	}
	assertShortFictionTestObservationsUnchanged(t, application, canonicalWorkspace, beforeTree, beforeVersions, beforeHistory)
}

func TestFanqieGenerateBindsSourceWithoutMutation(t *testing.T) {
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/short.md"
	before := "# 旧稿\n\n这是一段不得被预览生成改写的正文。"
	writeShortFictionTestFile(t, workspace, path, before)
	provider := newShortFictionTestProvider(t, "# 完整短篇\n\n候选正文。")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	changeService, err := workspacechange.ForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := changeService.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeGroups := listShortFictionTestGroups(t, application)
	beforeVersions := listShortFictionTestVersions(t, application)
	beforeHistory := listShortFictionTestSessionHistory(t, application)
	beforeTree := snapshotShortFictionTestTree(t, workspace)

	candidate, err := application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   path,
			BaseRevision: revision,
			Brief:        "外卖员发现新订单来自明天。",
			Source:       "caller-supplied source must not become authority",
			Locale:       "ignored",
		},
	}, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Workspace != workspace || candidate.TargetPath != path || candidate.BaseRevision != revision {
		t.Fatalf("candidate authority = %#v", candidate)
	}
	if candidate.Source != before || candidate.Locale != "zh-CN" {
		t.Fatalf("candidate source binding = %#v", candidate)
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls.Load())
	}
	if got := readShortFictionTestFile(t, application.Workspace(), path); got != before {
		t.Fatalf("generation mutated file: %q", got)
	}
	if groups := listShortFictionTestGroups(t, application); !reflect.DeepEqual(groups, beforeGroups) {
		t.Fatalf("generation changed ChangeSets: before=%#v after=%#v", beforeGroups, groups)
	}
	assertShortFictionTestObservationsUnchanged(t, application, workspace, beforeTree, beforeVersions, beforeHistory)
}

func TestFanqieGenerateRejectsStaleRevisionBeforeModelCall(t *testing.T) {
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/short.md"
	writeShortFictionTestFile(t, workspace, path, "old draft")
	changeService, err := workspacechange.ForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, staleRevision, err := changeService.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := "new draft"
	writeShortFictionTestFile(t, workspace, path, before)
	provider := newShortFictionTestProvider(t, "# candidate")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	beforeVersions := len(listShortFictionTestVersions(t, application))

	_, err = application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   path,
			BaseRevision: staleRevision,
			Brief:        "generate from an exact revision",
		},
	}, "en-US")
	if err == nil {
		t.Fatal("stale revision was accepted")
	}
	if provider.calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls.Load())
	}
	if got := readShortFictionTestFile(t, application.Workspace(), path); got != before {
		t.Fatalf("generation mutated file: %q", got)
	}
	if len(listShortFictionTestGroups(t, application)) != 0 {
		t.Fatal("rejected generation created ChangeSet")
	}
	if len(listShortFictionTestVersions(t, application)) != beforeVersions {
		t.Fatal("rejected generation created version")
	}
}

func TestFanqieGenerateRejectsDifferentActiveWorkspace(t *testing.T) {
	requestedWorkspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	activeWorkspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/short.md"
	requestedBefore := "same bytes in two different workspaces"
	activeBefore := requestedBefore
	writeShortFictionTestFile(t, requestedWorkspace, path, requestedBefore)
	writeShortFictionTestFile(t, activeWorkspace, path, activeBefore)
	requestedService, err := workspacechange.ForWorkspace(requestedWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	_, requestedRevision, err := requestedService.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	provider := newShortFictionTestProvider(t, "# wrong workspace candidate")
	application := newShortFictionTestApp(t, activeWorkspace, provider.server.URL)
	beforeVersions := len(listShortFictionTestVersions(t, application))

	_, err = application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    requestedWorkspace,
			TargetPath:   path,
			BaseRevision: requestedRevision,
			Brief:        "do not redirect this request",
		},
	}, "en-US")
	if err == nil {
		t.Fatal("request for a different active workspace was accepted")
	}
	if provider.calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls.Load())
	}
	if got := readShortFictionTestFile(t, requestedWorkspace, path); got != requestedBefore {
		t.Fatalf("generation mutated requested workspace file: %q", got)
	}
	if got := readShortFictionTestFile(t, activeWorkspace, path); got != activeBefore {
		t.Fatalf("generation mutated active workspace file: %q", got)
	}
	if len(listShortFictionTestGroups(t, application)) != 0 {
		t.Fatal("rejected generation created ChangeSet")
	}
	if len(listShortFictionTestVersions(t, application)) != beforeVersions {
		t.Fatal("rejected generation created version")
	}
}

func TestFanqieGenerateBindsMissingTargetWithoutCreation(t *testing.T) {
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/new-short.md"
	provider := newShortFictionTestProvider(t, "# new candidate")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	beforeVersions := len(listShortFictionTestVersions(t, application))

	candidate, err := application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   path,
			BaseRevision: shortfiction.MissingRevision,
			Brief:        "create a preview for a new target",
			Source:       "caller data is not the missing target snapshot",
		},
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.BaseRevision != shortfiction.MissingRevision || candidate.Source != "" {
		t.Fatalf("missing target authority = %#v", candidate)
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls.Load())
	}
	if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(path))); !os.IsNotExist(err) {
		t.Fatalf("generation created missing target: %v", err)
	}
	if len(listShortFictionTestGroups(t, application)) != 0 {
		t.Fatal("generation created ChangeSet")
	}
	if len(listShortFictionTestVersions(t, application)) != beforeVersions {
		t.Fatal("generation created version")
	}
}

func TestFanqieGenerateRejectsContentRevisionForMissingTargetBeforeModelCall(t *testing.T) {
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/new-short.md"
	provider := newShortFictionTestProvider(t, "# forbidden candidate")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	contentRevision := workspacechange.Revision([]byte("content that does not exist"))

	_, err := application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   path,
			BaseRevision: contentRevision,
			Brief:        "do not accept a revision for a missing file",
		},
	}, "en-US")
	if err == nil {
		t.Fatal("content revision was accepted for a missing target")
	}
	if provider.calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls.Load())
	}
	if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(path))); !os.IsNotExist(err) {
		t.Fatalf("rejected generation created missing target: %v", err)
	}
}

func TestFanqieGenerateRejectsOversizedSourceBeforeModelCall(t *testing.T) {
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/oversized.md"
	before := strings.Repeat("x", shortfiction.MaxSourceBytes+1)
	writeShortFictionTestFile(t, workspace, path, before)
	changeService, err := workspacechange.ForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := changeService.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	provider := newShortFictionTestProvider(t, "# forbidden candidate")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)

	_, err = application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   path,
			BaseRevision: revision,
			Brief:        "reject an oversized source snapshot",
		},
	}, "en-US")
	if err == nil {
		t.Fatal("oversized source was accepted")
	}
	if provider.calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls.Load())
	}
	if got := readShortFictionTestFile(t, workspace, path); got != before {
		t.Fatal("rejected generation mutated oversized source")
	}
}

func TestFanqieGenerateRejectsNonVisibleMarkdownTargetBeforeModelCall(t *testing.T) {
	for _, target := range []string{"chapters/short.txt", "chapters/.short.md", "chapters/../short.md"} {
		t.Run(target, func(t *testing.T) {
			workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
			provider := newShortFictionTestProvider(t, "# forbidden candidate")
			application := newShortFictionTestApp(t, workspace, provider.server.URL)

			_, err := application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
				ProfileID: shortfiction.ProfileFanqieShort,
				Source: shortfiction.SourcePacket{
					Workspace:    workspace,
					TargetPath:   target,
					BaseRevision: shortfiction.MissingRevision,
					Brief:        "reject a non-visible Markdown target",
				},
			}, "en-US")
			if err == nil {
				t.Fatal("invalid target was accepted")
			}
			if provider.calls.Load() != 0 {
				t.Fatalf("provider calls = %d, want 0", provider.calls.Load())
			}
		})
	}
}

type shortFictionTestProvider struct {
	server *httptest.Server
	calls  atomic.Int64
}

func newShortFictionTestProvider(t *testing.T, content string) *shortFictionTestProvider {
	t.Helper()
	provider := &shortFictionTestProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.calls.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("provider path = %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			}},
		}); err != nil {
			t.Errorf("encode provider response: %v", err)
		}
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

func newShortFictionTestApp(t *testing.T, workspace, providerURL string) *App {
	t.Helper()
	sessionStore, err := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	activeSession, err := sessionStore.GetActiveOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	application := &App{
		cfg: &config.Config{
			Workspace: workspace,
			NovaDir:   filepath.Join(t.TempDir(), "denova-data"),
			ModelProfiles: []config.ModelProfileSettings{{
				ID:            "fanqie-test-profile",
				OpenAIAPIKey:  "test-secret",
				OpenAIBaseURL: providerURL + "/v1",
				OpenAIModel:   "fanqie-test-model",
			}},
			AgentModels: config.AgentModelSettings{
				IDE: config.AgentModelOverride{ProfileID: "fanqie-test-profile"},
			},
		},
		workspace:      workspace,
		bookService:    book.NewService(workspace),
		versionService: book.NewVersionService(workspace),
		sessionStore:   sessionStore,
		session:        activeSession,
	}
	t.Cleanup(application.Close)
	return application
}

func writeShortFictionTestFile(t *testing.T, workspace, path, content string) {
	t.Helper()
	absolute := filepath.Join(workspace, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func canonicalShortFictionTestWorkspace(t *testing.T, workspace string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(canonical)
}

func readShortFictionTestFile(t *testing.T, workspace, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func listShortFictionTestGroups(t *testing.T, application *App) []workspacechange.ChangeGroupSummary {
	t.Helper()
	service, err := application.WorkspaceChangeService()
	if err != nil {
		t.Fatal(err)
	}
	groups, err := service.ListGroups(context.Background(), workspacechange.ChangeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return groups
}

func listShortFictionTestVersions(t *testing.T, application *App) []book.VersionEntry {
	t.Helper()
	versions, err := application.VersionHistory(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return versions
}

func listShortFictionTestSessionHistory(t *testing.T, application *App) []session.HistoryEntry {
	t.Helper()
	history, err := application.SessionMessages("")
	if err != nil {
		t.Fatal(err)
	}
	return history
}

type shortFictionTestTreeEntry struct {
	Mode fs.FileMode
	Data string
	Link string
}

func snapshotShortFictionTestTree(t *testing.T, workspace string) map[string]shortFictionTestTreeEntry {
	t.Helper()
	snapshot := map[string]shortFictionTestTreeEntry{}
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := shortFictionTestTreeEntry{Mode: info.Mode()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			item.Link, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var data []byte
			data, err = os.ReadFile(path)
			item.Data = string(data)
		}
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(relative)] = item
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertShortFictionTestObservationsUnchanged(
	t *testing.T,
	application *App,
	workspace string,
	beforeTree map[string]shortFictionTestTreeEntry,
	beforeVersions []book.VersionEntry,
	beforeHistory []session.HistoryEntry,
) {
	t.Helper()
	if afterTree := snapshotShortFictionTestTree(t, workspace); !reflect.DeepEqual(afterTree, beforeTree) {
		t.Fatalf("workspace tree changed:\nbefore=%#v\nafter=%#v", beforeTree, afterTree)
	}
	if afterVersions := listShortFictionTestVersions(t, application); !reflect.DeepEqual(afterVersions, beforeVersions) {
		t.Fatalf("version history changed:\nbefore=%#v\nafter=%#v", beforeVersions, afterVersions)
	}
	if afterHistory := listShortFictionTestSessionHistory(t, application); !reflect.DeepEqual(afterHistory, beforeHistory) {
		t.Fatalf("session history changed:\nbefore=%#v\nafter=%#v", beforeHistory, afterHistory)
	}
}

func assertShortFictionTestNoChangeMetadata(t *testing.T, workspace string) {
	t.Helper()
	_, err := os.Lstat(filepath.Join(workspace, ".denova", "changes"))
	if !os.IsNotExist(err) {
		t.Fatalf("workspace change metadata exists after cold preview: %v", err)
	}
}
