package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"denova/config"
	"denova/internal/book"
	"denova/internal/shortfiction"
	"denova/internal/workspacechange"
)

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
	beforeVersions := len(listShortFictionTestVersions(t, application))

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
	if len(listShortFictionTestGroups(t, application)) != 0 {
		t.Fatal("generation created ChangeSet")
	}
	if len(listShortFictionTestVersions(t, application)) != beforeVersions {
		t.Fatal("generation created version")
	}
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
