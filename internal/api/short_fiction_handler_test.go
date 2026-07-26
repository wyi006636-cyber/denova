package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"

	"denova/config"
	runtimeapp "denova/internal/app"
	"denova/internal/book"
	"denova/internal/shortfiction"
	"denova/internal/workspacechange"
)

func TestFanqiePreviewThenConfirmUsesPublicHTTP(t *testing.T) {
	const (
		target    = "chapters/short.md"
		before    = "# 旧稿\n\n等待作者确认。"
		generated = "# 完整短篇\n\n作者确认后的正文。"
	)
	provider := newShortFictionAPIProvider(t, generated, nil)
	application := newShortFictionAPIApplication(t, provider.URL)
	server := NewServer(application, "0")
	writeShortFictionAPIFile(t, application.Workspace(), target, before)

	generateResponse := performJSONRequest(t, server, http.MethodPost, "/api/short-fiction/candidates", shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    application.Workspace(),
			TargetPath:   target,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "一名外卖员发现订单来自明天。",
		},
	})
	if generateResponse.Code != http.StatusOK {
		t.Fatalf("generate status = %d body=%s", generateResponse.Code, generateResponse.Body.String())
	}
	var candidate shortfiction.GeneratedCandidate
	decodeResponse(t, generateResponse.Body.Bytes(), &candidate)
	if candidate.PreviewMarkdown != generated || candidate.CandidateID == "" {
		t.Fatalf("candidate = %#v", candidate)
	}
	if got := readShortFictionAPIFile(t, application.Workspace(), target); got != before {
		t.Fatalf("preview mutated workspace bytes: got=%q want=%q", got, before)
	}

	confirmResponse := performJSONRequest(t, server, http.MethodPost, "/api/short-fiction/candidates/confirm", shortfiction.ConfirmRequest{Candidate: candidate})
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("confirm status = %d body=%s", confirmResponse.Code, confirmResponse.Body.String())
	}
	var result shortfiction.ConfirmationResult
	decodeResponse(t, confirmResponse.Body.Bytes(), &result)
	if result.Status != shortfiction.ConfirmationWritten || result.Checkpoint == nil || result.Checkpoint.VersionID == "" {
		t.Fatalf("confirmation result = %#v", result)
	}
	if result.Checkpoint.Source != book.VersionSourceManual || result.Checkpoint.Revision != workspacechange.Revision([]byte(generated)) {
		t.Fatalf("checkpoint = %#v", result.Checkpoint)
	}
	if got := readShortFictionAPIFile(t, application.Workspace(), target); got != generated {
		t.Fatalf("confirmed bytes = %q, want %q", got, generated)
	}
}

func TestFanqieGenerationHTTPDoesNotWriteOrSendTools(t *testing.T) {
	const (
		target = "chapters/short.md"
		before = "visible source must remain byte-identical"
	)
	var providerPayload map[string]any
	provider := newShortFictionAPIProvider(t, "# Preview only\n\nNo tools may run.", func(payload map[string]any) {
		providerPayload = payload
	})
	application := newShortFictionAPIApplication(t, provider.URL)
	server := NewServer(application, "0")
	writeShortFictionAPIFile(t, application.Workspace(), target, before)
	beforeWorkspace := snapshotShortFictionAPIWorkspace(t, application.Workspace())

	response := performShortFictionJSONRequest(t, server, "/api/short-fiction/candidates", shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    application.Workspace(),
			TargetPath:   target,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "Generate a preview without invoking any tool.",
		},
	}, "en-US")
	if response.Code != http.StatusOK {
		t.Fatalf("generate status = %d body=%s", response.Code, response.Body.String())
	}
	var candidate shortfiction.GeneratedCandidate
	decodeResponse(t, response.Body.Bytes(), &candidate)
	if candidate.Locale != "en-US" {
		t.Fatalf("candidate locale = %q, want en-US", candidate.Locale)
	}
	if _, exists := providerPayload["tools"]; exists {
		t.Fatalf("provider request leaked tools: %#v", providerPayload["tools"])
	}
	if _, exists := providerPayload["tool_choice"]; exists {
		t.Fatalf("provider request leaked tool_choice: %#v", providerPayload["tool_choice"])
	}
	afterWorkspace := snapshotShortFictionAPIWorkspace(t, application.Workspace())
	if !reflect.DeepEqual(afterWorkspace, beforeWorkspace) {
		t.Fatalf("generation mutated workspace: before=%#v after=%#v", beforeWorkspace, afterWorkspace)
	}
}

func TestFanqieConfirmReturnsRevisionConflictWithoutWrite(t *testing.T) {
	const (
		target = "chapters/short.md"
		before = "candidate base bytes"
		later  = "newer author edit"
	)
	provider := newShortFictionAPIProvider(t, "# Stale candidate", nil)
	application := newShortFictionAPIApplication(t, provider.URL)
	server := NewServer(application, "0")
	writeShortFictionAPIFile(t, application.Workspace(), target, before)
	candidate := generateShortFictionAPICandidate(t, server, application, target, before, "en-US")
	writeShortFictionAPIFile(t, application.Workspace(), target, later)

	response := performShortFictionJSONRequest(t, server, "/api/short-fiction/candidates/confirm", shortfiction.ConfirmRequest{Candidate: candidate}, "en-US")
	assertShortFictionAPIError(t, response, http.StatusConflict, "revision_conflict", "The target changed after preview. Review the current file and generate again.")
	if got := readShortFictionAPIFile(t, application.Workspace(), target); got != later {
		t.Fatalf("revision conflict changed file: got=%q want=%q", got, later)
	}
}

func TestFanqieConfirmRejectsIdenticalContentWithoutMutation(t *testing.T) {
	tests := []struct {
		locale  string
		message string
	}{
		{locale: "zh-CN", message: "候选内容与目标文件相同，无需确认写入。"},
		{locale: "en-US", message: "The candidate already matches the target file; no confirmation write is needed."},
	}
	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			const (
				target  = "chapters/short.md"
				content = "# Same\n\nThe generated candidate is already current."
			)
			provider := newShortFictionAPIProvider(t, content, nil)
			application := newShortFictionAPIApplication(t, provider.URL)
			server := NewServer(application, "0")
			writeShortFictionAPIFile(t, application.Workspace(), target, content)
			baseline, err := application.CreateVersion(context.Background(), "baseline")
			if err != nil || baseline.Version == nil {
				t.Fatalf("initialize version history: result=%#v err=%v", baseline, err)
			}
			candidate := generateShortFictionAPICandidate(t, server, application, target, content, test.locale)
			beforeWorkspace := snapshotShortFictionAPIWorkspace(t, application.Workspace())
			beforeVersions, err := application.VersionHistory(context.Background(), 100)
			if err != nil {
				t.Fatal(err)
			}
			changeService, err := application.WorkspaceChangeService()
			if err != nil {
				t.Fatal(err)
			}
			beforeGroups, err := changeService.ListGroups(context.Background(), workspacechange.ChangeFilter{})
			if err != nil {
				t.Fatal(err)
			}

			response := performShortFictionJSONRequest(t, server, "/api/short-fiction/candidates/confirm", shortfiction.ConfirmRequest{Candidate: candidate}, test.locale)
			assertShortFictionAPIError(t, response, http.StatusBadRequest, workspacechange.ErrorCodeInvalidEdit, test.message)
			if got := readShortFictionAPIFile(t, application.Workspace(), target); got != content {
				t.Fatalf("no-op confirmation changed target bytes: got=%q want=%q", got, content)
			}
			afterWorkspace := snapshotShortFictionAPIWorkspace(t, application.Workspace())
			if !reflect.DeepEqual(afterWorkspace, beforeWorkspace) {
				t.Fatalf("no-op confirmation mutated workspace: before=%#v after=%#v", beforeWorkspace, afterWorkspace)
			}
			afterVersions, err := application.VersionHistory(context.Background(), 100)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(afterVersions, beforeVersions) {
				t.Fatalf("no-op confirmation changed checkpoints: before=%#v after=%#v", beforeVersions, afterVersions)
			}
			afterGroups, err := changeService.ListGroups(context.Background(), workspacechange.ChangeFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(afterGroups, beforeGroups) {
				t.Fatalf("no-op confirmation changed ChangeSets: before=%#v after=%#v", beforeGroups, afterGroups)
			}
		})
	}
}

func TestFanqieConfirmReturnsHTTP200PartialSuccess(t *testing.T) {
	const (
		target    = "chapters/short.md"
		before    = "checkpoint failure source"
		generated = "# Durable candidate\n\nThe Markdown write must remain committed."
	)
	provider := newShortFictionAPIProvider(t, generated, nil)
	application := newShortFictionAPIApplication(t, provider.URL)
	server := NewServer(application, "0")
	writeShortFictionAPIFile(t, application.Workspace(), target, before)
	baseline, err := application.CreateVersion(context.Background(), "baseline")
	if err != nil || baseline.Version == nil {
		t.Fatalf("initialize version history: result=%#v err=%v", baseline, err)
	}
	candidate := generateShortFictionAPICandidate(t, server, application, target, before, "en-US")
	if err := os.WriteFile(filepath.Join(application.Workspace(), ".git", "HEAD"), []byte("invalid head\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	response := performShortFictionJSONRequest(t, server, "/api/short-fiction/candidates/confirm", shortfiction.ConfirmRequest{Candidate: candidate}, "en-US")
	if response.Code != http.StatusOK {
		t.Fatalf("partial confirm status = %d body=%s", response.Code, response.Body.String())
	}
	var result shortfiction.ConfirmationResult
	decodeResponse(t, response.Body.Bytes(), &result)
	if result.Status != shortfiction.ConfirmationWrittenCheckpointFailed || !result.WorkspaceMutated {
		t.Fatalf("partial result = %#v", result)
	}
	if result.CheckpointStatus != shortfiction.CheckpointFailed || result.Checkpoint != nil || result.Retryable {
		t.Fatalf("partial checkpoint result = %#v", result)
	}
	if result.WriteRevision != workspacechange.Revision([]byte(generated)) || result.ChangeGroupID == "" || result.ChangeSetID == "" {
		t.Fatalf("partial write identity = %#v", result)
	}
	if got := readShortFictionAPIFile(t, application.Workspace(), target); got != generated {
		t.Fatalf("partial success bytes = %q, want %q", got, generated)
	}
}

func TestFanqieErrorsUseChineseAndEnglishLocaleHeaders(t *testing.T) {
	provider := newShortFictionAPIProvider(t, "# unused", nil)
	application := newShortFictionAPIApplication(t, provider.URL)
	server := NewServer(application, "0")

	tests := []struct {
		locale string
		want   string
	}{
		{locale: "zh-CN", want: "不支持该短篇生成配置。"},
		{locale: "en-US", want: "This short-fiction profile is not supported."},
	}
	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			response := performShortFictionJSONRequest(t, server, "/api/short-fiction/candidates", shortfiction.GenerateRequest{
				ProfileID: "unknown",
				Source: shortfiction.SourcePacket{
					Workspace:    application.Workspace(),
					TargetPath:   "chapters/short.md",
					BaseRevision: shortfiction.MissingRevision,
					Brief:        "locale-specific validation",
				},
			}, test.locale)
			assertShortFictionAPIError(t, response, http.StatusBadRequest, shortfiction.ErrorCodeInvalidProfile, test.want)
		})
	}

	oversizedSource := strings.Repeat("x", shortfiction.MaxSourceBytes+1)
	writeShortFictionAPIFile(t, application.Workspace(), "chapters/oversized.md", oversizedSource)
	oversizedResponse := performShortFictionJSONRequest(t, server, "/api/short-fiction/candidates", shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    application.Workspace(),
			TargetPath:   "chapters/oversized.md",
			BaseRevision: workspacechange.Revision([]byte(oversizedSource)),
			Brief:        "reject an oversized existing source",
		},
	}, "en-US")
	assertShortFictionAPIError(t, oversizedResponse, http.StatusRequestEntityTooLarge, shortfiction.ErrorCodeOversized, "The short-fiction source exceeds the supported size.")

	emptyProvider := newShortFictionAPIProvider(t, "   ", nil)
	emptyApplication := newShortFictionAPIApplication(t, emptyProvider.URL)
	emptyServer := NewServer(emptyApplication, "0")
	emptyResponse := performShortFictionJSONRequest(t, emptyServer, "/api/short-fiction/candidates", shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    emptyApplication.Workspace(),
			TargetPath:   "chapters/short.md",
			BaseRevision: shortfiction.MissingRevision,
			Brief:        "provider returns empty output",
		},
	}, "en-US")
	assertShortFictionAPIError(t, emptyResponse, http.StatusBadGateway, "generation_empty", "The configured model returned an empty short-fiction candidate.")
}

func TestShortFictionUnknownProfileDoesNotFallback(t *testing.T) {
	var providerCalls atomic.Int64
	provider := newShortFictionAPIProvider(t, "# forbidden fallback", func(map[string]any) {
		providerCalls.Add(1)
	})
	application := newShortFictionAPIApplication(t, provider.URL)
	server := NewServer(application, "0")

	response := performShortFictionJSONRequest(t, server, "/api/short-fiction/candidates", shortfiction.GenerateRequest{
		ProfileID: "future_profile",
		Source: shortfiction.SourcePacket{
			Workspace:    application.Workspace(),
			TargetPath:   "chapters/short.md",
			BaseRevision: shortfiction.MissingRevision,
			Brief:        "must not fall back to Fanqie",
		},
	}, "en-US")
	assertShortFictionAPIError(t, response, http.StatusBadRequest, shortfiction.ErrorCodeInvalidProfile, "This short-fiction profile is not supported.")
	if providerCalls.Load() != 0 {
		t.Fatalf("unknown profile reached provider %d times", providerCalls.Load())
	}
}

func newShortFictionAPIProvider(t *testing.T, content string, observe func(map[string]any)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("provider path = %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		if observe != nil {
			observe(payload)
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
	t.Cleanup(server.Close)
	return server
}

func newShortFictionAPIApplication(t *testing.T, providerURL string) *runtimeapp.App {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := runtimeapp.New(context.Background(), &config.Config{
		Workspace:           filepath.Clean(workspace),
		NovaDir:             t.TempDir(),
		ResumeLastWorkspace: false,
		ModelProfiles: []config.ModelProfileSettings{{
			ID:            "fanqie-api-test-profile",
			OpenAIAPIKey:  "test-secret",
			OpenAIBaseURL: providerURL + "/v1",
			OpenAIModel:   "fanqie-api-test-model",
		}},
		AgentModels: config.AgentModelSettings{
			IDE: config.AgentModelOverride{ProfileID: "fanqie-api-test-profile"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Close)
	return application
}

func writeShortFictionAPIFile(t *testing.T, workspace, path, content string) {
	t.Helper()
	absolute := filepath.Join(workspace, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readShortFictionAPIFile(t *testing.T, workspace, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func generateShortFictionAPICandidate(t *testing.T, server *Server, application *runtimeapp.App, target, source, locale string) shortfiction.GeneratedCandidate {
	t.Helper()
	response := performShortFictionJSONRequest(t, server, "/api/short-fiction/candidates", shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    application.Workspace(),
			TargetPath:   target,
			BaseRevision: workspacechange.Revision([]byte(source)),
			Brief:        "Generate a complete candidate for confirmation.",
		},
	}, locale)
	if response.Code != http.StatusOK {
		t.Fatalf("generate status = %d body=%s", response.Code, response.Body.String())
	}
	var candidate shortfiction.GeneratedCandidate
	decodeResponse(t, response.Body.Bytes(), &candidate)
	return candidate
}

func performShortFictionJSONRequest(t *testing.T, server *Server, path string, body any, locale string) *ut.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return ut.PerformRequest(
		server.engine.Engine,
		http.MethodPost,
		path,
		&ut.Body{Body: bytes.NewReader(data), Len: len(data)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
		ut.Header{Key: "X-Denova-Locale", Value: locale},
	)
}

func assertShortFictionAPIError(t *testing.T, response *ut.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("error status = %d body=%s, want %d", response.Code, response.Body.String(), status)
	}
	var raw map[string]json.RawMessage
	decodeResponse(t, response.Body.Bytes(), &raw)
	if len(raw) != 3 || raw["error"] == nil || raw["code"] == nil || raw["details"] == nil {
		t.Fatalf("error JSON keys = %#v, want exactly error, code, details", raw)
	}
	var payload struct {
		Error   string         `json:"error"`
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	decodeResponse(t, response.Body.Bytes(), &payload)
	if payload.Error != message || payload.Code != code {
		t.Fatalf("error payload = %#v, want code=%q error=%q", payload, code, message)
	}
	if mutated, ok := payload.Details["workspace_mutated"].(bool); !ok || mutated {
		t.Fatalf("error details = %#v, want workspace_mutated=false", payload.Details)
	}
}

func snapshotShortFictionAPIWorkspace(t *testing.T, workspace string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(workspace, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(relative)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
