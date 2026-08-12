package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"denova/internal/yanzhouprotocol"
)

func writeTestFrame(t *testing.T, output io.Writer, frame yanzhouprotocol.Envelope) {
	t.Helper()
	if err := yanzhouprotocol.WriteFrame(output, frame); err != nil {
		t.Fatal(err)
	}
}

func standardSubAgentSnapshot() map[string]any {
	definitions := []struct {
		id           string
		name         string
		prompt       string
		capabilities []string
	}{
		{id: "general", name: "General", prompt: "Handle a bounded delegated task.", capabilities: []string{"story.get_target"}},
		{id: "context-planner", name: "上下文规划", prompt: "整理完成目标所需的最小上下文引用。", capabilities: []string{"story.get_target"}},
		{id: "writer", name: "正文作者", prompt: "只生成候选正文。", capabilities: []string{"story.get_target", "writing.create_artifact"}},
		{id: "reviewer", name: "审阅者", prompt: "只审阅候选并指出有证据的问题。", capabilities: []string{"story.get_target"}},
		{id: "fixer", name: "修订者", prompt: "只按确认的问题修订候选。", capabilities: []string{"story.get_target", "writing.create_artifact"}},
		{id: "final-gate", name: "终检", prompt: "执行最终一致性检查。", capabilities: []string{"story.get_target"}},
		{id: "memory-patcher", name: "设定建议", prompt: "只生成设定建议。", capabilities: []string{"story.get_target"}},
	}
	agents := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		agents = append(agents, map[string]any{
			"id": definition.id, "name": definition.name, "prompt": definition.prompt,
			"profileId": nil, "enabled": true, "capabilities": definition.capabilities,
		})
	}
	return map[string]any{"schemaVersion": "1", "revision": 1, "agents": agents}
}

func writingPayload(t *testing.T, baseURL, runID string) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "requestId": "request-" + runID,
		"idempotencyKey": "idem-" + runID, "runId": runID,
		"sessionId": "session-" + runID, "agentKind": "ide",
		"entrypoint": "structured_action", "capabilityId": "chapter.generate_from_outline",
		"userIntent": "根据章纲生成候选正文", "explicitContinue": false, "planMode": false,
		"selectedSkillIds": []string{}, "harnessProfile": "novel-standard",
		"subAgentSnapshot": standardSubAgentSnapshot(),
		"target": map[string]any{
			"schemaVersion": "1", "kind": "chapter", "bookId": "book-1",
			"targetId": "chapter-1", "parentIds": []string{"volume-1"},
		},
		"effectiveModelProfile": map[string]any{
			"profileId": "fixture", "providerType": "openai-compatible",
			"adapterId": "openai-compatible", "baseUrl": baseURL, "model": "fixture",
			"capabilities": map[string]any{"streaming": true}, "timeoutMs": 30000,
			"runtimeAuth": map[string]any{"mode": "none"},
			"resolution":  map[string]any{"source": "run"},
		},
		"contextPackRef": map[string]any{"ref": "sha256:" + strings.Repeat("f", 64)},
		"toolCapabilityManifest": map[string]any{
			"schemaVersion": "1", "runId": runID, "agentKind": "ide",
			"target": map[string]any{
				"schemaVersion": "1", "kind": "chapter", "bookId": "book-1",
				"targetId": "chapter-1", "parentIds": []string{"volume-1"},
			},
			"capabilities": []map[string]any{
				{"id": "story.get_target", "mode": "read", "maxCalls": 8, "maxResultBytes": 262144},
				{"id": "story.get_outline", "mode": "read", "maxCalls": 4, "maxResultBytes": 262144},
				{"id": "story.search_chapters", "mode": "read", "maxCalls": 4, "maxResultBytes": 262144},
				{"id": "writing.create_artifact", "mode": "propose", "maxCalls": 8, "maxResultBytes": 262144},
				{"id": "writing.create_proposal", "mode": "propose", "maxCalls": 4, "maxResultBytes": 262144},
			},
			"deniedByDefault": true, "issuedAt": "2026-08-12T00:00:00.000Z",
		},
		"budgets": map[string]any{
			"maxModelCalls": 3, "maxToolRounds": 4, "maxDelegations": 1,
			"maxRevisionRounds": 1, "maxWallTimeMs": 30000,
			"maxInputTokens": 64000, "maxOutputTokens": 16000,
		},
		"baseRevisions": map[string]string{"chapter-1": "revision-1"},
		"displayLocale": "zh-CN",
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func planPayload(t *testing.T, baseURL, runID string) json.RawMessage {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(writingPayload(t, baseURL, runID), &payload); err != nil {
		t.Fatal(err)
	}
	payload["entrypoint"] = "agent_chat"
	payload["capabilityId"] = "chapter.generate_from_outline"
	payload["planMode"] = true
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func contextResponse(t *testing.T) json.RawMessage {
	return contextResponseWithSections(t, []map[string]any{{
		"kind": "chapter_text", "content": "旧站台的铜钟只响三次",
		"revision": "sha256:" + strings.Repeat("a", 64), "truncated": false,
	}})
}

func contextResponseWithSections(t *testing.T, sections []map[string]any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_target", "success": true,
		"result": map[string]any{
			"kind": "read-result", "mutationPerformed": false,
			"data": map[string]any{
				"contextPackRef": "sha256:" + strings.Repeat("f", 64),
				"sections":       sections,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestSelectedSkillContentReachesModelContext(t *testing.T) {
	providerSawSkill := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read provider request: %v", err)
		}
		providerSawSkill = providerSawSkill || bytes.Contains(body, []byte("短句与动作优先，删除机械连接词"))
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "自然化候选"}, "finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 2, "total_tokens": 6},
		})
	}))
	defer server.Close()
	setSidecarEnvironment(t)
	payload := map[string]any{}
	if err := json.Unmarshal(writingPayload(t, server.URL, "run-skill"), &payload); err != nil {
		t.Fatal(err)
	}
	payload["harnessProfile"] = "novel-lite"
	delete(payload, "subAgentSnapshot")
	payload["selectedSkillIds"] = []string{"skill-naturalize"}
	payload["skillSnapshot"] = map[string]any{
		"schemaVersion": "1", "runId": "run-skill",
		"skills": []map[string]any{{
			"schemaVersion": "1", "id": "skill-naturalize", "revision": 3,
			"checksum": "sha256:" + strings.Repeat("b", 64), "source": "builtin", "resources": []any{},
		}},
	}
	payloadJSON, _ := json.Marshal(payload)
	handshake, _ := json.Marshal(yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion, ClientBuild: "yanzhou-test",
		WorkspaceSchema: "yanzhou-book/1", BootstrapToken: "foundation-token",
		RequestedFeatures: []string{"handshake", "plan-mode", "writing-harness", "skills-v2", "sub-agents"},
	})
	var input bytes.Buffer
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindHandshakeRequest, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "handshake-skill", Payload: handshake,
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-run-skill", RunID: "run-skill", Payload: payloadJSON,
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-run-skill-context", RunID: "run-skill", Seq: 1,
		Payload: contextResponseWithSections(t, []map[string]any{
			{
				"kind": "chapter_text", "content": "原文",
				"revision": "sha256:" + strings.Repeat("a", 64), "truncated": false,
			},
			{
				"kind": "skill_reference", "content": "# skill-naturalize\n短句与动作优先，删除机械连接词",
				"revision": "sha256:" + strings.Repeat("b", 64), "truncated": false,
			},
		}),
	})
	var output bytes.Buffer
	if err := run(context.Background(), []string{"--runtime-root", t.TempDir()}, &input, &output); err != nil {
		t.Fatal(err)
	}
	if !providerSawSkill {
		t.Fatal("selected Skill content did not reach the model request")
	}
}

func setSidecarEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv(bootstrapTokenEnv, "foundation-token")
	t.Setenv("YANZHOU_UPSTREAM_REPOSITORY", "denova")
	t.Setenv("YANZHOU_UPSTREAM_BASE_SHA", "a"+strings.Repeat("1", 39))
	t.Setenv("YANZHOU_ADAPTER_COMMIT_SHA", "b"+strings.Repeat("2", 39))
	t.Setenv("YANZHOU_SOURCE_TREE_SHA", "c"+strings.Repeat("3", 39))
	t.Setenv("YANZHOU_BINARY_SHA256", strings.Repeat("d", 64))
	t.Setenv("YANZHOU_SKILLS_MANIFEST_SHA", strings.Repeat("e", 64))
	t.Setenv("YANZHOU_BUILT_AT", "2026-08-12T00:00:00Z")
	t.Setenv("YANZHOU_SIDECAR_BUILD", "foundation-test")
}

func TestSidecarHandshakeAndStandardWritingRun(t *testing.T) {
	providerCalls := 0
	providerSystems := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerCalls++
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		for _, message := range body.Messages {
			if message.Role == "system" {
				providerSystems = append(providerSystems, message.Content)
				break
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "stage-output"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 2, "total_tokens": 6},
		})
	}))
	defer server.Close()
	setSidecarEnvironment(t)

	var input bytes.Buffer
	handshake, _ := json.Marshal(yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion, ClientBuild: "yanzhou-test",
		WorkspaceSchema: "yanzhou-book/1", BootstrapToken: "foundation-token",
		RequestedFeatures: []string{"handshake", "plan-mode", "writing-harness", "skills-v2", "sub-agents"},
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindHandshakeRequest, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "handshake-1", Payload: handshake,
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-run-1", Payload: writingPayload(t, server.URL, "run-1"),
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-run-1-context", RunID: "run-1", Seq: 1, Payload: contextResponse(t),
	})

	var output bytes.Buffer
	if err := run(context.Background(), []string{"--runtime-root", t.TempDir()}, &input, &output); err != nil {
		t.Fatal(err)
	}
	reader := yanzhouprotocol.NewReader(&output, yanzhouprotocol.DefaultMaxFrameBytes)
	artifactKinds := []string{}
	delegations := []struct {
		Type    string
		Payload map[string]any
	}{}
	completed := false
	for index := 0; ; index++ {
		frame, err := reader.ReadFrame()
		if err != nil {
			break
		}
		if index == 0 && frame.Kind != yanzhouprotocol.KindHandshakeResponse {
			t.Fatalf("first frame = %s", frame.Kind)
		}
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
			artifactKinds = append(artifactKinds, event.Payload["artifactKind"].(string))
		}
		if strings.HasPrefix(event.Type, "delegation.") {
			delegations = append(delegations, struct {
				Type    string
				Payload map[string]any
			}{Type: event.Type, Payload: event.Payload})
		}
		completed = completed || event.Type == "run.completed"
	}
	if providerCalls != 3 || strings.Join(artifactKinds, ",") != "draft,review,transform,report" || !completed {
		t.Fatalf("calls=%d artifacts=%v completed=%t", providerCalls, artifactKinds, completed)
	}
	if len(providerSystems) != 3 || !strings.Contains(providerSystems[1], "只审阅候选并指出有证据的问题。") {
		t.Fatalf("reviewer system prompts = %#v", providerSystems)
	}
	if len(delegations) != 2 || delegations[0].Type != "delegation.started" || delegations[1].Type != "delegation.completed" {
		t.Fatalf("delegations = %#v", delegations)
	}
	started := delegations[0].Payload
	if started["taskId"] != "task-run-1-review" || started["parentRunId"] != "run-1" || started["subAgentId"] != "reviewer" || started["status"] != "running" {
		t.Fatalf("delegation.started = %#v", started)
	}
	completedPayload := delegations[1].Payload
	if completedPayload["taskId"] != "task-run-1-review" || completedPayload["parentRunId"] != "run-1" || completedPayload["subAgentId"] != "reviewer" || completedPayload["status"] != "completed" {
		t.Fatalf("delegation.completed = %#v", completedPayload)
	}
	if refs, ok := completedPayload["outputArtifactRefs"].([]any); !ok || len(refs) != 1 || refs[0] == "" {
		t.Fatalf("delegation.completed output refs = %#v", completedPayload["outputArtifactRefs"])
	}
}

func TestReviewerFailureCompletesDelegationBeforeRunFailure(t *testing.T) {
	providerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerCalls++
		writer.Header().Set("Content-Type", "application/json")
		if providerCalls == 2 {
			writer.WriteHeader(http.StatusBadGateway)
			_, _ = writer.Write([]byte(`{"error":{"message":"reviewer unavailable"}}`))
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "draft-output"}, "finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 2, "total_tokens": 6},
		})
	}))
	defer server.Close()
	setSidecarEnvironment(t)
	var input bytes.Buffer
	handshake, _ := json.Marshal(yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion, ClientBuild: "yanzhou-test",
		WorkspaceSchema: "yanzhou-book/1", BootstrapToken: "foundation-token",
		RequestedFeatures: []string{"handshake", "writing-harness", "sub-agents"},
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindHandshakeRequest, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "handshake-review-failure", Payload: handshake,
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-run-review-failure", RunID: "run-review-failure",
		Payload: writingPayload(t, server.URL, "run-review-failure"),
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-run-review-failure-context", RunID: "run-review-failure", Seq: 1, Payload: contextResponse(t),
	})
	var output bytes.Buffer
	if err := run(context.Background(), []string{"--runtime-root", t.TempDir()}, &input, &output); err != nil {
		t.Fatal(err)
	}
	reader := yanzhouprotocol.NewReader(&output, yanzhouprotocol.DefaultMaxFrameBytes)
	eventOrder := []string{}
	failedDelegation := false
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			break
		}
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
		if event.Type == "delegation.completed" || event.Type == "run.failed" {
			eventOrder = append(eventOrder, event.Type)
		}
		failedDelegation = failedDelegation || event.Type == "delegation.completed" && event.Payload["status"] == "failed"
	}
	if !failedDelegation || strings.Join(eventOrder, ",") != "delegation.completed,run.failed" {
		t.Fatalf("reviewer failure event order = %v", eventOrder)
	}
}

func TestHeavyWritingRunUsesBuiltInDelegatedAgentsWithoutSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "heavy-stage-output"}, "finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 2, "total_tokens": 6},
		})
	}))
	defer server.Close()
	setSidecarEnvironment(t)
	var payload map[string]any
	if err := json.Unmarshal(writingPayload(t, server.URL, "run-heavy"), &payload); err != nil {
		t.Fatal(err)
	}
	payload["harnessProfile"] = "novel-heavy"
	delete(payload, "subAgentSnapshot")
	payload["budgets"].(map[string]any)["maxModelCalls"] = 7
	payload["budgets"].(map[string]any)["maxDelegations"] = 7
	payloadJSON, _ := json.Marshal(payload)
	handshake, _ := json.Marshal(yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion, ClientBuild: "yanzhou-test",
		WorkspaceSchema: "yanzhou-book/1", BootstrapToken: "foundation-token",
		RequestedFeatures: []string{"handshake", "writing-harness", "sub-agents"},
	})
	var input bytes.Buffer
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindHandshakeRequest, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "handshake-heavy", Payload: handshake,
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-run-heavy", RunID: "run-heavy", Payload: payloadJSON,
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-run-heavy-context", RunID: "run-heavy", Seq: 1, Payload: contextResponse(t),
	})
	var output bytes.Buffer
	if err := run(context.Background(), []string{"--runtime-root", t.TempDir()}, &input, &output); err != nil {
		t.Fatal(err)
	}
	reader := yanzhouprotocol.NewReader(&output, yanzhouprotocol.DefaultMaxFrameBytes)
	completed := false
	delegatedAgents := []string{}
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			break
		}
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
		completed = completed || event.Type == "run.completed"
		if strings.HasPrefix(event.Type, "delegation.") {
			delegatedAgents = append(delegatedAgents, event.Payload["subAgentId"].(string))
		}
	}
	if !completed {
		t.Fatal("novel-heavy writing run did not complete with built-in SubAgent defaults")
	}
	if strings.Join(delegatedAgents, ",") != "reviewer,reviewer" {
		t.Fatalf("public delegated agents = %v", delegatedAgents)
	}
}

func TestRunCancelPayloadIsClosed(t *testing.T) {
	for _, raw := range []string{`{}`, `{"runId":""}`, `{"runId":"run-1","extra":true}`} {
		if _, err := runCancelRunID(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid cancel accepted: %s", raw)
		}
	}
	if runID, err := runCancelRunID(json.RawMessage(`{"runId":"run-1"}`)); err != nil || runID != "run-1" {
		t.Fatalf("valid cancel = %q, %v", runID, err)
	}
}

func TestPlanRunCancelEmitsTerminalWithoutKillingSidecar(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	releaseRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestStarted <- struct{}{}
		select {
		case <-request.Context().Done():
		case <-releaseRequest:
		}
	}))
	defer server.Close()
	setSidecarEnvironment(t)

	inputReader, inputWriter := io.Pipe()
	var output bytes.Buffer
	runContext, cancelRun := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancelRun()
	done := make(chan error, 1)
	go func() {
		done <- run(runContext, []string{"--runtime-root", t.TempDir()}, inputReader, &output)
	}()
	handshake, _ := json.Marshal(yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion, ClientBuild: "yanzhou-test",
		WorkspaceSchema: "yanzhou-book/1", BootstrapToken: "foundation-token",
		RequestedFeatures: []string{"handshake", "plan-mode"},
	})
	writeTestFrame(t, inputWriter, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindHandshakeRequest, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "handshake-plan-cancel", Payload: handshake,
	})
	writeTestFrame(t, inputWriter, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-plan-cancel", RunID: "plan-cancel", Payload: planPayload(t, server.URL, "plan-cancel"),
	})
	select {
	case <-requestStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("plan model request did not start")
	}
	cancelPayload := json.RawMessage(`{"runId":"plan-cancel"}`)
	writeTestFrame(t, inputWriter, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunCancel, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "cancel-plan-cancel", RunID: "plan-cancel", Payload: cancelPayload,
	})
	_ = inputWriter.Close()
	if err := <-done; err != nil {
		t.Fatalf("sidecar run after Plan cancel: %v", err)
	}
	close(releaseRequest)
	reader := yanzhouprotocol.NewReader(&output, yanzhouprotocol.DefaultMaxFrameBytes)
	aborted := false
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			break
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
		aborted = aborted || event.Type == "run.aborted"
	}
	if !aborted {
		t.Fatal("Plan cancel did not emit run.aborted")
	}
}

func TestWritingToolFailureEmitsRunFailedTerminal(t *testing.T) {
	setSidecarEnvironment(t)
	var payload map[string]any
	if err := json.Unmarshal(writingPayload(t, "http://127.0.0.1:1", "run-tool-failure"), &payload); err != nil {
		t.Fatal(err)
	}
	payload["harnessProfile"] = "novel-lite"
	delete(payload, "subAgentSnapshot")
	payloadJSON, _ := json.Marshal(payload)
	handshake, _ := json.Marshal(yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion, ClientBuild: "yanzhou-test",
		WorkspaceSchema: "yanzhou-book/1", BootstrapToken: "foundation-token",
		RequestedFeatures: []string{"handshake", "plan-mode", "writing-harness", "skills-v2", "sub-agents"},
	})
	toolFailure, _ := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_target", "success": false,
		"errorCode": "tool_unavailable",
	})
	var input bytes.Buffer
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindHandshakeRequest, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "handshake-tool-failure", Payload: handshake,
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-run-tool-failure", RunID: "run-tool-failure", Payload: payloadJSON,
	})
	writeTestFrame(t, &input, yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-run-tool-failure-context", RunID: "run-tool-failure", Seq: 1, Payload: toolFailure,
	})
	var output bytes.Buffer
	if err := run(context.Background(), []string{"--runtime-root", t.TempDir()}, &input, &output); err != nil {
		t.Fatal(err)
	}
	reader := yanzhouprotocol.NewReader(&output, yanzhouprotocol.DefaultMaxFrameBytes)
	found := false
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			break
		}
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
		found = found || event.Type == "run.failed" && event.Payload["reason"] == "tool_error"
	}
	if !found {
		t.Fatal("tool failure did not emit run.failed with tool_error")
	}
}
