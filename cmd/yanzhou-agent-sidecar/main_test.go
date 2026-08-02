package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"denova/internal/yanzhouprotocol"
)

func writeInputFrame(t *testing.T, buffer *bytes.Buffer, frame yanzhouprotocol.Envelope) {
	t.Helper()
	if err := yanzhouprotocol.WriteFrame(buffer, frame); err != nil {
		t.Fatal(err)
	}
}

func sidecarWritingPayload(t *testing.T, baseURL, runID, entrypoint, harnessProfile string, maxModelCalls int) json.RawMessage {
	t.Helper()
	requestID := "request-" + runID
	target := map[string]any{"schemaVersion": "1", "kind": "chapter", "bookId": "book-wp8", "targetId": "chapter-wp8", "parentIds": []string{"volume-wp8"}}
	agentIDs := []string{"general", "context-planner", "writer", "reviewer", "fixer", "final-gate", "memory-patcher"}
	agents := make([]map[string]any, 0, len(agentIDs))
	for _, id := range agentIDs {
		capabilities := []string{}
		if id == "reviewer" {
			capabilities = []string{"story.get_target"}
		}
		agents = append(agents, map[string]any{"id": id, "name": id, "prompt": "configured prompt for " + id, "profileId": nil, "enabled": true, "capabilities": capabilities})
	}
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "requestId": requestID, "idempotencyKey": "idem-" + runID,
		"runId": runID, "sessionId": "session-" + runID, "agentKind": "ide",
		"entrypoint": entrypoint, "target": target,
		"capabilityId": "chapter.generate_from_outline", "userIntent": "根据章纲成文", "planMode": false,
		"selectedSkillIds": []string{}, "harnessProfile": harnessProfile,
		"subAgentSnapshot": map[string]any{"schemaVersion": "1", "revision": 1, "agents": agents},
		"effectiveModelProfile": map[string]any{
			"profileId": "profile-wp8", "providerType": "openai-compatible", "adapterId": "openai-compatible", "baseUrl": baseURL, "model": "fixture-model",
			"capabilities": map[string]any{"streaming": true}, "timeoutMs": 30000, "runtimeAuth": map[string]any{"mode": "none"}, "resolution": map[string]any{"source": "run"},
		},
		"contextPackRef":         map[string]any{"ref": "sha256:" + strings.Repeat("f", 64)},
		"toolCapabilityManifest": map[string]any{"schemaVersion": "1", "runId": runID, "agentId": "primary-writer", "target": target, "deniedByDefault": true, "capabilities": []map[string]any{{"id": "story.get_target", "mode": "read", "maxCalls": 8, "maxResultBytes": 262144}}},
		"budgets":                map[string]any{"maxModelCalls": maxModelCalls, "maxToolRounds": 4, "maxDelegations": 1, "maxRevisionRounds": 1, "maxWallTimeMs": 30000, "maxInputTokens": 64000, "maxOutputTokens": 16000},
		"baseRevisions":          map[string]string{"chapter-wp8": "revision-1"}, "displayLocale": "zh-CN",
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func sidecarContextResponsePayload(t *testing.T) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_target", "success": true,
		"result": map[string]any{
			"kind": "read-result", "mutationPerformed": false,
			"data": map[string]any{
				"contextPackRef": "sha256:" + strings.Repeat("f", 64),
				"sections":       []map[string]any{{"kind": "chapter_text", "content": "fixture 章节上下文", "revision": "sha256:" + strings.Repeat("a", 64), "truncated": false}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestRunDispatchesBothEntrypointsThroughNegotiatedWritingHarnesses(t *testing.T) {
	providerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerCalls++
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "站台阶段-" + string(rune('0'+providerCalls))}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 8, "completion_tokens": 9, "total_tokens": 17},
		})
	}))
	defer server.Close()

	t.Setenv(bootstrapTokenEnv, "wp8-token")
	t.Setenv("YANZHOU_UPSTREAM_REPOSITORY", "denova")
	t.Setenv("YANZHOU_UPSTREAM_BASE_SHA", "a"+strings.Repeat("1", 39))
	t.Setenv("YANZHOU_ADAPTER_COMMIT_SHA", "b"+strings.Repeat("2", 39))
	t.Setenv("YANZHOU_SOURCE_TREE_SHA", "c"+strings.Repeat("3", 39))
	t.Setenv("YANZHOU_BINARY_SHA256", strings.Repeat("d", 64))
	t.Setenv("YANZHOU_SKILLS_MANIFEST_SHA", strings.Repeat("e", 64))
	t.Setenv("YANZHOU_BUILT_AT", "2026-07-25T00:00:00Z")
	t.Setenv("YANZHOU_SIDECAR_BUILD", "wp8-test")

	var input bytes.Buffer
	handshakePayload, _ := json.Marshal(yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion, ClientBuild: "wp8-test",
		WorkspaceSchema: "yanzhou-book/1", BootstrapToken: "wp8-token",
		RequestedFeatures: []string{"handshake", "writing-harness"},
	})
	writeInputFrame(t, &input, yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindHandshakeRequest, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "handshake-1", Payload: handshakePayload})
	for _, run := range []struct {
		runID, entrypoint, harness string
		maxModelCalls              int
	}{
		{runID: "run-agent", entrypoint: "agent_chat", harness: "novel-lite", maxModelCalls: 1},
		{runID: "run-structured", entrypoint: "structured_action", harness: "novel-standard", maxModelCalls: 3},
	} {
		payload := sidecarWritingPayload(t, server.URL, run.runID, run.entrypoint, run.harness, run.maxModelCalls)
		writeInputFrame(t, &input, yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-" + run.runID, Payload: payload})
		writeInputFrame(t, &input, yanzhouprotocol.Envelope{
			Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
			RequestID: "tool-" + run.runID + "-context", RunID: run.runID, Seq: 1,
			Payload: sidecarContextResponsePayload(t),
		})
	}

	var output bytes.Buffer
	if err := run(context.Background(), []string{"--runtime-root", t.TempDir()}, &input, &output); err != nil {
		t.Fatal(err)
	}
	reader := yanzhouprotocol.NewReader(&output, yanzhouprotocol.DefaultMaxFrameBytes)
	frames := []yanzhouprotocol.Envelope{}
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			break
		}
		frames = append(frames, frame)
	}
	if frames[0].Kind != yanzhouprotocol.KindHandshakeResponse {
		t.Fatalf("first frame = %s", frames[0].Kind)
	}
	for index := 1; index < len(frames); index++ {
		if frames[index].Kind != yanzhouprotocol.KindRunEvent && frames[index].Kind != yanzhouprotocol.KindToolRequest {
			t.Fatalf("frame %d = %s", index, frames[index].Kind)
		}
	}
	if providerCalls != 4 {
		t.Fatalf("provider calls = %d, want lite 1 + standard 3", providerCalls)
	}
	artifacts := map[string][]string{}
	completed := map[string]bool{}
	for _, frame := range frames[1:] {
		if frame.Kind != yanzhouprotocol.KindRunEvent {
			continue
		}
		var event struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(frame.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "artifact.created" {
			artifacts[frame.RunID] = append(artifacts[frame.RunID], event.Payload["artifactKind"].(string))
		}
		if event.Type == "run.completed" {
			completed[frame.RunID] = true
		}
	}
	if strings.Join(artifacts["run-agent"], ",") != "draft,report" {
		t.Fatalf("agent artifacts = %#v", artifacts["run-agent"])
	}
	if strings.Join(artifacts["run-structured"], ",") != "draft,review,transform,report" {
		t.Fatalf("structured artifacts = %#v", artifacts["run-structured"])
	}
	if !completed["run-agent"] || !completed["run-structured"] {
		t.Fatalf("completed runs = %#v", completed)
	}
}

func TestRunRoutesExistingCancelToWritingRuntimeAndEmitsOneAbortedTerminal(t *testing.T) {
	providerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerCalls++
		<-request.Context().Done()
	}))
	defer server.Close()

	t.Setenv(bootstrapTokenEnv, "wp8-cancel-token")
	t.Setenv("YANZHOU_UPSTREAM_REPOSITORY", "denova")
	t.Setenv("YANZHOU_UPSTREAM_BASE_SHA", "a"+strings.Repeat("1", 39))
	t.Setenv("YANZHOU_ADAPTER_COMMIT_SHA", "b"+strings.Repeat("2", 39))
	t.Setenv("YANZHOU_SOURCE_TREE_SHA", "c"+strings.Repeat("3", 39))
	t.Setenv("YANZHOU_BINARY_SHA256", strings.Repeat("d", 64))
	t.Setenv("YANZHOU_SKILLS_MANIFEST_SHA", strings.Repeat("e", 64))
	t.Setenv("YANZHOU_BUILT_AT", "2026-07-25T00:00:00Z")

	var input bytes.Buffer
	handshakePayload, _ := json.Marshal(yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion, ClientBuild: "wp8-test",
		WorkspaceSchema: "yanzhou-book/1", BootstrapToken: "wp8-cancel-token",
		RequestedFeatures: []string{"handshake", "writing-harness"},
	})
	writeInputFrame(t, &input, yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindHandshakeRequest, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "handshake-cancel", Payload: handshakePayload})
	payload := sidecarWritingPayload(t, server.URL, "run-cancel", "agent_chat", "novel-lite", 1)
	writeInputFrame(t, &input, yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-run-cancel", Payload: payload})
	writeInputFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-run-cancel-context", RunID: "run-cancel", Seq: 1,
		Payload: sidecarContextResponsePayload(t),
	})
	cancelPayload, _ := json.Marshal(map[string]string{"runId": "run-cancel"})
	writeInputFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunCancel, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "cancel-run-cancel", Payload: cancelPayload,
	})

	var output bytes.Buffer
	if err := run(context.Background(), []string{"--runtime-root", t.TempDir()}, &input, &output); err != nil {
		t.Fatal(err)
	}
	reader := yanzhouprotocol.NewReader(&output, yanzhouprotocol.DefaultMaxFrameBytes)
	terminals := []string{}
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			break
		}
		if frame.Kind == yanzhouprotocol.KindRuntimeError {
			t.Fatalf("cancel produced runtime.error: %s", frame.Payload)
		}
		if frame.Kind != yanzhouprotocol.KindRunEvent {
			continue
		}
		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(frame.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(event.Type, "run.") && event.Type != "run.started" {
			terminals = append(terminals, event.Type)
		}
	}
	if strings.Join(terminals, ",") != "run.aborted" {
		t.Fatalf("terminals = %#v, want one run.aborted", terminals)
	}
	if providerCalls != 0 {
		t.Fatalf("provider calls after pending cancel = %d", providerCalls)
	}
}

func TestRunCancelPayloadIsClosedAndRequiresRunID(t *testing.T) {
	for _, payload := range []string{`{}`, `{"runId":""}`, `{"runId":"run-1","extra":true}`, `{"runId":"run-1"} trailing`} {
		if _, err := runCancelRunID(json.RawMessage(payload)); err == nil {
			t.Fatalf("invalid cancel payload was accepted: %s", payload)
		}
	}
	if runID, err := runCancelRunID(json.RawMessage(`{"runId":"run-1"}`)); err != nil || runID != "run-1" {
		t.Fatalf("valid cancel = %q, %v", runID, err)
	}
}

func TestMain(m *testing.M) { os.Exit(m.Run()) }
