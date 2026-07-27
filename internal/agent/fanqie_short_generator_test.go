package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"denova/config"
	"denova/internal/shortfiction"
)

func TestFanqieGeneratorUsesConfiguredIDEModelWithoutTools(t *testing.T) {
	var payload map[string]any
	server := newFanqieProviderServer(t, "# 逆袭\n故事正文", func(request map[string]any) {
		payload = request
	})

	generation, err := NewFanqieShortGenerator(newFanqieGeneratorConfig(server.URL)).Generate(context.Background(), fanqieGeneratorTestSource(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["tools"]; exists {
		t.Fatalf("tools leaked: %#v", payload["tools"])
	}
	if _, exists := payload["tool_choice"]; exists {
		t.Fatalf("tool_choice leaked: %#v", payload["tool_choice"])
	}
	if payload["model"] != "fanqie-test-model" {
		t.Fatalf("model = %#v", payload["model"])
	}
	if generation.PreviewMarkdown != "# 逆袭\n故事正文" {
		t.Fatalf("preview markdown = %q", generation.PreviewMarkdown)
	}
	if generation.ModelProfileID != "fanqie-test-profile" || generation.Model != "fanqie-test-model" {
		t.Fatalf("provenance = %#v", generation)
	}
	if strings.Contains(generation.ModelProfileID+generation.Model, "test-secret") || strings.Contains(generation.ModelProfileID+generation.Model, server.URL) {
		t.Fatalf("provenance exposed provider credentials or endpoint: %#v", generation)
	}
}

func TestFanqieGeneratorRejectsEmptyMarkdown(t *testing.T) {
	server := newFanqieProviderServer(t, "   ", nil)

	_, err := NewFanqieShortGenerator(newFanqieGeneratorConfig(server.URL)).Generate(context.Background(), fanqieGeneratorTestSource(t))
	if !shortfiction.IsCode(err, "generation_empty") {
		t.Fatalf("error code = %#v, want generation_empty", err)
	}
}

func TestFanqieGeneratorRejectsOversizedMarkdown(t *testing.T) {
	server := newFanqieProviderServer(t, strings.Repeat("x", shortfiction.MaxCandidateBytes+1), nil)

	_, err := NewFanqieShortGenerator(newFanqieGeneratorConfig(server.URL)).Generate(context.Background(), fanqieGeneratorTestSource(t))
	if !shortfiction.IsCode(err, "candidate_too_large") {
		t.Fatalf("error code = %#v, want candidate_too_large", err)
	}
}

func TestFanqieGeneratorDoesNotLogProviderErrorDetails(t *testing.T) {
	const (
		endpointSentinel = "endpoint-sentinel"
		bodySentinel     = "body-sentinel"
		keySentinel      = "key-sentinel"
		promptSentinel   = "prompt-sentinel"
		storySentinel    = "story-sentinel"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, strings.Join([]string{bodySentinel, keySentinel, promptSentinel, storySentinel}, " "), http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	var logs bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	cfg := &config.Config{
		ModelProfiles: []config.ModelProfileSettings{{
			ID:            "fanqie-test-profile",
			OpenAIAPIKey:  keySentinel,
			OpenAIBaseURL: server.URL + "/" + endpointSentinel,
			OpenAIModel:   "fanqie-test-model",
		}},
		AgentModels: config.AgentModelSettings{
			IDE: config.AgentModelOverride{ProfileID: "fanqie-test-profile"},
		},
	}
	source := fanqieGeneratorTestSource(t)
	source.Brief = promptSentinel
	source.Source = storySentinel

	_, err := NewFanqieShortGenerator(cfg).Generate(context.Background(), source)
	if !shortfiction.IsCode(err, "generation_failed") {
		t.Fatalf("error code = %#v, want generation_failed", err)
	}
	for _, sentinel := range []string{endpointSentinel, bodySentinel, keySentinel, promptSentinel, storySentinel} {
		if strings.Contains(logs.String(), sentinel) {
			t.Fatalf("logs leaked %q: %s", sentinel, logs.String())
		}
	}
}

func newFanqieProviderServer(t *testing.T, content string, observe func(map[string]any)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if observe != nil {
			observe(payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func newFanqieGeneratorConfig(baseURL string) *config.Config {
	return &config.Config{
		ModelProfiles: []config.ModelProfileSettings{{
			ID:            "fanqie-test-profile",
			OpenAIAPIKey:  "test-secret",
			OpenAIBaseURL: baseURL + "/v1",
			OpenAIModel:   "fanqie-test-model",
		}},
		AgentModels: config.AgentModelSettings{
			IDE: config.AgentModelOverride{ProfileID: "fanqie-test-profile"},
		},
	}
}

func fanqieGeneratorTestSource(t *testing.T) shortfiction.SourcePacket {
	t.Helper()
	return shortfiction.SourcePacket{
		Workspace:    t.TempDir(),
		TargetPath:   "draft.md",
		BaseRevision: shortfiction.MissingRevision,
		Brief:        "主角在绝境中逆袭。",
		Locale:       "zh-CN",
	}
}
