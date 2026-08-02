package yanzhouadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"

	"denova/internal/session"
	"denova/internal/yanzhouprotocol"
)

type writingRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip writingRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type writingRunStartedBarrierStore struct {
	RuntimeEventStore
	appendStarted chan struct{}
	releaseAppend chan struct{}
	once          sync.Once
}

func (store *writingRunStartedBarrierStore) Append(ctx context.Context, runID string, input RuntimeEventInput) (RunEvent, error) {
	if input.Type == RunEventTypeRunStarted {
		store.once.Do(func() { close(store.appendStarted) })
		<-store.releaseAppend
	}
	return store.RuntimeEventStore.Append(ctx, runID, input)
}

func writingRunPayload(t *testing.T, baseURL string, entrypoint string) json.RawMessage {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(testPlanRunRequestPayload(t, baseURL), &value); err != nil {
		t.Fatal(err)
	}
	value["planMode"] = false
	value["entrypoint"] = entrypoint
	value["capabilityId"] = "chapter.generate_from_outline"
	value["harnessProfile"] = "novel-lite"
	value["subAgentSnapshot"] = writingSubAgentSnapshot(true)
	value["selectedSkillIds"] = []string{}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func writingSubAgentSnapshot(reviewerEnabled bool) map[string]any {
	ids := []string{"general", "context-planner", "writer", "reviewer", "fixer", "final-gate", "memory-patcher"}
	agents := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		capabilities := []string{}
		if id == "reviewer" {
			capabilities = []string{"story.get_target"}
		}
		agents = append(agents, map[string]any{
			"id": id, "name": id, "prompt": "configured prompt for " + id,
			"profileId": nil, "enabled": id != "reviewer" || reviewerEnabled,
			"capabilities": capabilities,
		})
	}
	return map[string]any{"schemaVersion": "1", "revision": 1, "agents": agents}
}

func writingDelegationManifest(runID string, target any) map[string]any {
	return map[string]any{
		"schemaVersion": "1", "runId": runID, "agentId": "primary-writer",
		"target": target, "deniedByDefault": true,
		"capabilities": []map[string]any{
			{"id": "story.get_target", "mode": "read", "maxCalls": 8, "maxResultBytes": 262144},
			{"id": "story.get_open_threads", "mode": "read", "maxCalls": 4, "maxResultBytes": 262144},
		},
	}
}

func primeWritingContext(t *testing.T, runtime *WritingFrameRuntime, runID string, skillDocuments ...string) {
	t.Helper()
	sections := []map[string]any{{"kind": "chapter_text", "content": "已处理的章节上下文", "revision": "sha256:" + strings.Repeat("b", 64), "truncated": false}}
	for _, document := range skillDocuments {
		sections = append(sections, map[string]any{"kind": "skill_reference", "content": document, "revision": "sha256:" + strings.Repeat("c", 64), "truncated": false})
	}
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_target", "success": true,
		"result": map[string]any{
			"kind": "read-result", "mutationPerformed": false,
			"data": map[string]any{
				"contextPackRef": "sha256:" + strings.Repeat("a", 64),
				"sections":       sections,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-" + runID + "-context", RunID: runID, Seq: 1, Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWritingFrameRuntimeFeedsToolFactBackToProvider(t *testing.T) {
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, body)
		writer.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "tool_calls": []map[string]any{{"id": "call-threads", "type": "function", "function": map[string]any{"name": "story.get_open_threads", "arguments": "{}"}}}}}}})
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "TASK4-TOOL-FACT 已用于候选"}}}})
	}))
	defer server.Close()
	store, _ := NewFileRuntimeEventStore(t.TempDir())
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	runID := "task4-tool-run"
	primeWritingContext(t, runtime, runID)
	toolPayload, _ := json.Marshal(map[string]any{"schemaVersion": "1", "toolId": "story.get_open_threads", "success": true, "result": map[string]any{"kind": "read-result", "mutationPerformed": false, "data": map[string]any{"summary": "读取未回收伏笔", "fact": "TASK4-TOOL-FACT"}}})
	if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "tool-" + runID + "-model-1-0", RunID: runID, Seq: 2, Payload: toolPayload}); err != nil {
		t.Fatal(err)
	}
	payload := writingRunPayload(t, server.URL, "agent_chat")
	var value map[string]any
	_ = json.Unmarshal(payload, &value)
	value["harnessProfile"] = "novel-heavy"
	value["budgets"].(map[string]any)["maxToolRounds"] = 1
	value["budgets"].(map[string]any)["maxModelCalls"] = 10
	value["runId"] = runID
	value["requestId"] = "request-" + runID
	value["idempotencyKey"] = "idem-" + runID
	payload, _ = json.Marshal(value)
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-" + runID, Payload: payload}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(requests) < 2 || !bytes.Contains(requests[1], []byte("TASK4-TOOL-FACT")) || !bytes.Contains(requests[1], []byte("call-threads")) {
		t.Fatalf("tool fact was not fed back: %q", requests)
	}
	for _, tool := range []string{"story_get_target", "story_get_outline", "story_get_adjacent_chapters", "story_search_chapters", "story_get_characters", "story_get_open_threads"} {
		if !bytes.Contains(requests[0], []byte(tool)) {
			t.Fatalf("first request omitted %s", tool)
		}
	}
	events, err := store.ReplayAfter(context.Background(), runID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	requested, started, completed, terminal := -1, -1, -1, 0
	for index, event := range events {
		if event.Type == RunEventTypeToolRequested && event.Payload["toolId"] == "story.get_open_threads" {
			requested = index
		}
		if event.Type == RunEventTypeToolStarted && event.Payload["toolId"] == "story.get_open_threads" {
			started = index
		}
		if event.Type == RunEventTypeToolCompleted && event.Payload["toolId"] == "story.get_open_threads" {
			completed = index
			if event.Payload["summary"] != "读取未回收伏笔" {
				t.Fatalf("tool summary = %#v", event.Payload)
			}
		}
		if IsTerminalRunEventType(event.Type) {
			terminal++
		}
	}
	if requested < 0 || started <= requested || completed <= started || terminal != 1 || events[len(events)-1].Type != RunEventTypeRunCompleted {
		t.Fatalf("tool/terminal events = %#v", events)
	}
}

func TestTask10WritingModelToolsReuseManifestAndRequireRealResults(t *testing.T) {
	manifest, _ := json.Marshal(ToolCapabilityManifest{
		SchemaVersion: "1", RunID: "run-task10", AgentID: "primary-writer", Target: testToolTarget(), DeniedByDefault: true,
		Capabilities: []ToolCapability{
			{ID: "command.run", Mode: ToolCapabilityExecute, MaxCalls: 1, MaxResultBytes: 4096},
			{ID: "web.search", Mode: ToolCapabilityRead, MaxCalls: 1, MaxResultBytes: 4096},
			{ID: "image.generate", Mode: ToolCapabilityPropose, MaxCalls: 1, MaxResultBytes: 4096},
		},
	})
	request := planRunRequest{ToolManifest: manifest}
	names := map[string]bool{}
	for _, tool := range writingModelTools(request) {
		names[tool.Name] = true
	}
	for _, name := range []string{"command.run", "web.search", "image.generate"} {
		if !names[name] {
			t.Fatalf("Task 10 tool %s missing from model tools: %#v", name, names)
		}
	}
	if instruction := writingSystemInstruction("web.search", "novel-lite", nil, WritingHarnessStage{ID: "primary-draft", RoleID: HarnessRolePrimaryWriter}); !strings.Contains(instruction, "Call web.search") || !strings.Contains(instruction, "never substitute model memory") {
		t.Fatalf("web search instruction does not require real sources: %s", instruction)
	}
	if instruction := writingSystemInstruction("image.generate", "novel-lite", []string{"chapter-illustration"}, WritingHarnessStage{ID: "primary-draft", RoleID: HarnessRolePrimaryWriter}); !strings.Contains(instruction, "Call image.generate exactly once") || !strings.Contains(instruction, "chapter-illustration") {
		t.Fatalf("image instruction does not preserve the existing Skill chain: %s", instruction)
	}
	searchResult := json.RawMessage(`{"kind":"read-result","data":{"query":"Codex CLI","results":[{"title":"OpenAI Codex","url":"https://developers.openai.com/codex/cli","summary":"Official docs"}]}}`)
	if answer := writingDirectToolAnswer("web.search", searchResult); !strings.Contains(answer, "[OpenAI Codex](https://developers.openai.com/codex/cli)") {
		t.Fatalf("web result did not become a real Markdown source: %s", answer)
	}
	imageResult := json.RawMessage(`{"kind":"receipt","data":{"summary":"插图已生成","markdown":"![章节插图](file:///tmp/chapter.png)","localPath":"/tmp/chapter.png"}}`)
	if answer := writingDirectToolAnswer("image.generate", imageResult); !strings.Contains(answer, "![章节插图](file:///tmp/chapter.png)") || !strings.Contains(answer, "/tmp/chapter.png") {
		t.Fatalf("image result did not preserve the existing preview/asset result: %s", answer)
	}
	imageCall := writingImageToolCall("run-task10", "生成非剧透插图")
	if imageCall.Name != "image.generate" || !strings.Contains(imageCall.Arguments, "生成非剧透插图") || !strings.Contains(imageCall.Arguments, "1024x1024") {
		t.Fatalf("direct image tool call=%#v", imageCall)
	}
	if kind := writingStageArtifactKind(WritingHarnessStage{RoleID: HarnessRolePrimaryWriter, OutputKind: "draft"}, "web.search"); kind != "report" {
		t.Fatalf("web result artifact kind=%s, want report", kind)
	}
}

func TestTask10AuthorToolStopsAfterRealResultAndForcesFinalAnswer(t *testing.T) {
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, body)
		writer.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{{
							"id": "call-command", "type": "function",
							"function": map[string]any{"name": "command_run", "arguments": `{"command":"pwd"}`},
						}, {
							"id": "call-command-duplicate", "type": "function",
							"function": map[string]any{"name": "command_run", "arguments": `{"command":"pwd"}`},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			})
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "真实目录是 TASK10-PWD"}}}})
	}))
	defer server.Close()
	store, _ := NewFileRuntimeEventStore(t.TempDir())
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	runID := "task10-command-final-run"
	primeWritingContext(t, runtime, runID)
	toolPayload, _ := json.Marshal(map[string]any{"schemaVersion": "1", "toolId": "command.run", "success": true, "result": map[string]any{"kind": "read-result", "mutationPerformed": false, "data": map[string]any{"summary": "前台命令完成", "stdout": "TASK10-PWD"}}})
	if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "tool-" + runID + "-model-1-0", RunID: runID, Seq: 2, Payload: toolPayload}); err != nil {
		t.Fatal(err)
	}
	payload := writingRunPayload(t, server.URL, "agent_chat")
	var value map[string]any
	_ = json.Unmarshal(payload, &value)
	target := ToolTarget{SchemaVersion: "1", Kind: "book", BookID: "book-1", TargetID: "book-1"}
	value["runId"], value["requestId"], value["idempotencyKey"] = runID, "request-"+runID, "idem-"+runID
	value["capabilityId"] = "command.run"
	value["toolCapabilityManifest"] = ToolCapabilityManifest{SchemaVersion: "1", RunID: runID, AgentID: "primary-writer", Target: target, DeniedByDefault: true, Capabilities: []ToolCapability{{ID: "command.run", Mode: ToolCapabilityExecute, MaxCalls: 1, MaxResultBytes: 4096}}}
	value["budgets"].(map[string]any)["maxToolRounds"] = 3
	value["budgets"].(map[string]any)["maxModelCalls"] = 3
	payload, _ = json.Marshal(value)
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-" + runID, Payload: payload}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("provider requests=%d, want tool call then final answer", len(requests))
	}
	var second map[string]any
	if err := json.Unmarshal(requests[1], &second); err != nil {
		t.Fatal(err)
	}
	if _, ok := second["tools"]; ok {
		t.Fatalf("author tool remained available after success: %s", requests[1])
	}
	if !bytes.Contains(requests[1], []byte("TASK10-PWD")) || !bytes.Contains(requests[1], []byte("requested real author tool has completed")) {
		t.Fatalf("real result/final-only instruction missing: %s", requests[1])
	}
}

func TestWritingFrameRuntimeToolLoopHonorsModelBudget(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "tool_calls": []map[string]any{{"id": "call-budget", "type": "function", "function": map[string]any{"name": "story.get_open_threads", "arguments": "{}"}}}}}}})
	}))
	defer server.Close()
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	runID := "task4-budget-run"
	primeWritingContext(t, runtime, runID)
	toolPayload, _ := json.Marshal(map[string]any{"schemaVersion": "1", "toolId": "story.get_open_threads", "success": true, "result": map[string]any{"kind": "read-result", "mutationPerformed": false, "data": map[string]any{"summary": "读取未回收伏笔", "fact": "TASK4-TOOL-FACT"}}})
	if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "tool-" + runID + "-model-1-0", RunID: runID, Seq: 2, Payload: toolPayload}); err != nil {
		t.Fatal(err)
	}
	payload := writingRunPayload(t, server.URL, "agent_chat")
	var value map[string]any
	_ = json.Unmarshal(payload, &value)
	value["harnessProfile"] = "novel-heavy"
	value["budgets"].(map[string]any)["maxToolRounds"] = 1
	value["budgets"].(map[string]any)["maxModelCalls"] = 1
	value["runId"] = runID
	value["requestId"] = "request-" + runID
	value["idempotencyKey"] = "idem-" + runID
	payload, _ = json.Marshal(value)
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-" + runID, Payload: payload}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("provider requests=%d, want 1", requests)
	}
	events, err := store.ReplayAfter(context.Background(), runID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	terminals := 0
	for _, event := range events {
		if IsTerminalRunEventType(event.Type) {
			terminals++
		}
		if event.Type == RunEventTypeProposalReady {
			t.Fatal("budgeted run created proposal")
		}
	}
	terminal := events[len(events)-1]
	if terminals != 1 || terminal.Type != RunEventTypeRunFailed || terminal.Payload["code"] != "model_budget_exhausted" {
		t.Fatalf("budget terminal=%#v", terminal)
	}
}

func TestWritingFrameRuntimeReservesAStandardDraftCallAfterToolGathering(t *testing.T) {
	requests := [][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, body)
		message := map[string]any{"role": "assistant", "content": "候选阶段完成"}
		if bytes.Contains(body, []byte(`"tools"`)) {
			message = map[string]any{"role": "assistant", "tool_calls": []map[string]any{{"id": fmt.Sprintf("call-%d", len(requests)), "type": "function", "function": map[string]any{"name": "story.get_target", "arguments": "{}"}}}}
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []map[string]any{{"message": message}}})
	}))
	defer server.Close()
	store, _ := NewFileRuntimeEventStore(t.TempDir())
	defer store.Close()
	runtime, _ := NewWritingFrameRuntime(store, server.Client())
	runID := "task6-reserved-draft-run"
	primeWritingContext(t, runtime, runID)
	for round := 1; round <= 4; round++ {
		response, _ := json.Marshal(map[string]any{"schemaVersion": "1", "toolId": "story.get_target", "success": true, "result": map[string]any{"kind": "read-result", "mutationPerformed": false, "data": map[string]any{"summary": "已读取当前章节"}}})
		if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: fmt.Sprintf("tool-%s-model-%d-0", runID, round), RunID: runID, Seq: 2, Payload: response}); err != nil {
			t.Fatal(err)
		}
	}
	var request map[string]any
	_ = json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &request)
	request["harnessProfile"], request["runId"], request["requestId"], request["idempotencyKey"] = "novel-standard", runID, "request-"+runID, "idem-"+runID
	request["toolCapabilityManifest"] = writingDelegationManifest(runID, request["target"])
	request["budgets"].(map[string]any)["maxModelCalls"] = 5
	request["budgets"].(map[string]any)["maxToolRounds"] = 4
	payload, _ := json.Marshal(request)
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-" + runID, Payload: payload}, io.Discard); err != nil {
		t.Fatal(err)
	}
	events, _ := store.ReplayAfter(context.Background(), runID, 0, 100)
	if len(requests) != 5 || bytes.Contains(requests[2], []byte(`"tools"`)) || events[len(events)-1].Type != RunEventTypeRunCompleted {
		t.Fatalf("standard run did not reserve its draft call: requests=%d third=%s terminal=%#v", len(requests), requests[2], events[len(events)-1])
	}
}

func TestWritingFrameRuntimeReusesDenovaSessionAcrossRuns(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, payload)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "第一轮 Agent 回复"},
			}},
		})
	}))
	defer server.Close()

	eventStore, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eventStore.Close()
	sessions, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewWritingFrameRuntime(eventStore, server.Client(), sessions)
	if err != nil {
		t.Fatal(err)
	}

	for index, runID := range []string{"session-run-1", "session-run-2"} {
		var payload map[string]any
		if err := json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &payload); err != nil {
			t.Fatal(err)
		}
		payload["runId"] = runID
		payload["requestId"] = "request-" + runID
		payload["idempotencyKey"] = "idem-" + runID
		payload["sessionId"] = "author-book-session"
		payload["userIntent"] = []string{"第一轮要求", "第二轮新要求"}[index]
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		primeWritingContext(t, runtime, runID)
		if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{
			Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
			RequestID: "request-" + runID, Payload: encoded,
		}, io.Discard); err != nil {
			t.Fatal(err)
		}
	}

	if len(requests) < 2 {
		t.Fatalf("provider requests = %d, want two runs", len(requests))
	}
	messages, ok := requests[len(requests)-1]["messages"].([]any)
	if !ok {
		t.Fatalf("second run messages = %#v", requests[len(requests)-1]["messages"])
	}
	encoded, _ := json.Marshal(messages)
	if !strings.Contains(string(encoded), "第一轮要求") || !strings.Contains(string(encoded), "第一轮 Agent 回复") {
		t.Fatalf("second run did not reuse durable Denova session: %s", encoded)
	}
}

func TestWritingFrameRuntimeOnlyResumesPendingTaskForExplicitContinue(t *testing.T) {
	var captured []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured, _ = io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "已续接未完成任务"},
			}},
		})
	}))
	defer server.Close()

	eventStore, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eventStore.Close()
	sessions, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	current, err := sessions.GetOrCreate("author-book-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := current.Append(schema.UserMessage("原先未完成的任务")); err != nil {
		t.Fatal(err)
	}
	if err := current.MarkInterrupted("原先未完成的任务", "", "cancelled"); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewWritingFrameRuntime(eventStore, server.Client(), sessions)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &payload); err != nil {
		t.Fatal(err)
	}
	payload["runId"] = "resume-run-1"
	payload["requestId"] = "request-resume-run-1"
	payload["idempotencyKey"] = "idem-resume-run-1"
	payload["sessionId"] = "author-book-session"
	payload["userIntent"] = "继续"
	payload["explicitContinue"] = true
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	primeWritingContext(t, runtime, "resume-run-1")
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-resume-run-1", Payload: encoded,
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(captured), "原先未完成的任务") || !strings.Contains(string(captured), "继续") {
		t.Fatalf("explicit continue did not use the pending Denova interruption: %s", captured)
	}
	if pending := current.PendingInterruption(); pending != nil {
		t.Fatalf("completed explicit continue left interruption pending: %#v", pending)
	}
}

func TestWritingFrameRuntimeExplicitContinueUsesPendingTaskAfterNewRequest(t *testing.T) {
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, payload)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "本轮完成"},
			}},
		})
	}))
	defer server.Close()

	eventStore, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eventStore.Close()
	sessions, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	current, err := sessions.GetOrCreate("author-book-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := current.MarkInterrupted("旧任务：补完雨夜告别", "已有片段：雨停在她伞沿", "cancelled"); err != nil {
		t.Fatal(err)
	}
	pending := current.PendingInterruption()
	if pending == nil {
		t.Fatal("pending interruption was not recorded")
	}
	runtime, err := NewWritingFrameRuntime(eventStore, server.Client(), sessions)
	if err != nil {
		t.Fatal(err)
	}
	run := func(runID, userIntent string, explicitContinue bool) {
		var payload map[string]any
		if err := json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &payload); err != nil {
			t.Fatal(err)
		}
		payload["runId"] = runID
		payload["requestId"] = "request-" + runID
		payload["idempotencyKey"] = "idem-" + runID
		payload["sessionId"] = "author-book-session"
		payload["userIntent"] = userIntent
		payload["explicitContinue"] = explicitContinue
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		primeWritingContext(t, runtime, runID)
		if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{
			Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
			RequestID: "request-" + runID, Payload: encoded,
		}, io.Discard); err != nil {
			t.Fatal(err)
		}
	}

	run("ordinary-new-run", "普通新要求：分析人物关系", false)
	if got := string(requests[0]); strings.Contains(got, "旧任务：补完雨夜告别") || strings.Contains(got, "已有片段：雨停在她伞沿") {
		t.Fatalf("ordinary new request used recovery prompt: %s", got)
	}
	if got := current.PendingInterruption(); got == nil || got.ID != pending.ID {
		t.Fatalf("ordinary new request changed pending interruption: %#v", got)
	}

	run("explicit-resume-run", "继续刚才的任务", true)
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	resumed := string(requests[1])
	if !strings.Contains(resumed, "旧任务：补完雨夜告别") || !strings.Contains(resumed, "已有片段：雨停在她伞沿") {
		t.Fatalf("explicit continue did not include pending task and partial output: %s", resumed)
	}
	if got := current.PendingInterruption(); got != nil {
		t.Fatalf("completed explicit continue left interruption pending: %#v", got)
	}
}

func TestWritingFrameRuntimeCancelledResumeKeepsOriginalPendingInterruption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "已恢复完成"},
			}},
		})
	}))
	defer server.Close()
	eventStore, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eventStore.Close()
	sessions, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	current, err := sessions.GetOrCreate("author-book-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := current.MarkInterrupted("旧任务", "旧片段", "cancelled"); err != nil {
		t.Fatal(err)
	}
	pending := current.PendingInterruption()
	if pending == nil {
		t.Fatal("pending interruption was not recorded")
	}
	runtime, err := NewWritingFrameRuntime(eventStore, server.Client(), sessions)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &payload); err != nil {
		t.Fatal(err)
	}
	payload["runId"] = "resume-cancel-run"
	payload["requestId"] = "request-resume-cancel-run"
	payload["idempotencyKey"] = "idem-resume-cancel-run"
	payload["sessionId"] = "author-book-session"
	payload["userIntent"] = "继续刚才的任务"
	payload["explicitContinue"] = true
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.CancelRun("resume-cancel-run"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-resume-cancel-run", Payload: encoded,
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := current.PendingInterruption(); got == nil || got.ID != pending.ID {
		t.Fatalf("cancelled resume replaced original interruption: %#v", got)
	}
	payload["runId"] = "resume-after-cancel-run"
	payload["requestId"] = "request-resume-after-cancel-run"
	payload["idempotencyKey"] = "idem-resume-after-cancel-run"
	encoded, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	primeWritingContext(t, runtime, "resume-after-cancel-run")
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-resume-after-cancel-run", Payload: encoded,
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := current.PendingInterruption(); got != nil {
		t.Fatalf("completed retry left the original interruption pending: %#v", got)
	}
}

func TestWritingFrameRuntimeLeavesPendingTaskForNewRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "新的任务已完成"},
			}},
		})
	}))
	defer server.Close()

	eventStore, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eventStore.Close()
	sessions, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	current, err := sessions.GetOrCreate("author-book-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := current.MarkInterrupted("未完成的旧任务", "", "cancelled"); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewWritingFrameRuntime(eventStore, server.Client(), sessions)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &payload); err != nil {
		t.Fatal(err)
	}
	payload["runId"] = "new-run-1"
	payload["requestId"] = "request-new-run-1"
	payload["idempotencyKey"] = "idem-new-run-1"
	payload["sessionId"] = "author-book-session"
	payload["userIntent"] = "换个方向，先分析人物关系"
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	primeWritingContext(t, runtime, "new-run-1")
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-new-run-1", Payload: encoded,
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if pending := current.PendingInterruption(); pending == nil || pending.UserMessage != "未完成的旧任务" {
		t.Fatalf("ordinary new request consumed pending interruption: %#v", pending)
	}
}

func TestWritingFrameRuntimeRunsExistingStartFrameWithFakeProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "雨落在旧站台上，林青没有回头。"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 18, "completion_tokens": 16, "total_tokens": 34},
		})
	}))
	defer server.Close()

	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	primeWritingContext(t, runtime, "plan-run-1")
	var output bytes.Buffer
	frame := yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-1", Payload: writingRunPayload(t, server.URL, "agent_chat"),
	}
	if err := runtime.HandleFrame(context.Background(), frame, &output); err != nil {
		t.Fatal(err)
	}

	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	want := []RunEventType{
		RunEventTypeRunStarted,
		RunEventTypeToolRequested,
		RunEventTypeToolStarted,
		RunEventTypeToolCompleted,
		RunEventTypeContextAccepted,
		RunEventTypeModelDelta,
		RunEventTypeArtifactCreated,
		RunEventTypeModelDelta,
		RunEventTypeArtifactCreated,
		RunEventTypeCheckCompleted,
		RunEventTypeProposalReady,
		RunEventTypeRunCompleted,
	}
	if len(events) != len(want) {
		t.Fatalf("events = %d, want %d: %#v", len(events), len(want), events)
	}
	for index := range want {
		if events[index].Type != want[index] {
			t.Fatalf("event %d = %s, want %s", index, events[index].Type, want[index])
		}
	}
	if got := events[5].Payload["text"]; got != "雨落在旧站台上，林青没有回头。" {
		t.Fatalf("model delta text = %#v", got)
	}
	if got := events[6].Payload["artifactKind"]; got != "draft" {
		t.Fatalf("artifact kind = %#v", got)
	}
	if got := events[6].Payload["entrypoint"]; got != "agent_chat" {
		t.Fatalf("entrypoint = %#v", got)
	}
}

func TestWritingFrameRuntimeProjectsToolFailureBeforeItsSingleTerminal(t *testing.T) {
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_target", "success": false, "errorCode": "tool_unavailable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-plan-run-1-context", RunID: "plan-run-1", Seq: 1, Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-1", Payload: writingRunPayload(t, "http://fixture.invalid", "agent_chat"),
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 || events[3].Type != RunEventTypeToolCompleted || events[4].Type != RunEventTypeRunFailed {
		t.Fatalf("event sequence = %#v", events)
	}
	if events[3].Payload["status"] != "failed" || events[3].Payload["code"] != "tool_unavailable" {
		t.Fatalf("tool failure projection = %#v", events[3].Payload)
	}
	if events[4].Payload["reason"] != "tool_error" || events[4].Payload["code"] != "tool_unavailable" {
		t.Fatalf("terminal failure projection = %#v", events[4].Payload)
	}
}

func TestWritingFrameRuntimePreservesProviderFailureCategoryWithoutItsRawResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(`{"error":{"message":"private provider response"}}`))
	}))
	defer server.Close()
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	primeWritingContext(t, runtime, "plan-run-1")
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-1", Payload: writingRunPayload(t, server.URL, "agent_chat"),
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	terminal := events[len(events)-1]
	if terminal.Type != RunEventTypeRunFailed || terminal.Payload["code"] != "provider_unavailable" || terminal.Payload["message"] != "模型服务暂时不可用，请稍后重试" {
		t.Fatalf("provider failure projection = %#v", terminal)
	}
	if strings.Contains(terminal.Payload["message"].(string), "private provider response") {
		t.Fatalf("raw provider response leaked: %#v", terminal.Payload)
	}
}

func TestWritingFrameRuntimeConsumesMainOwnedContextThroughExistingToolFrames(t *testing.T) {
	const chapterContext = "ContextPack 中独有的章节事实：铜钟只在第三次退潮后响起。"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte(chapterContext)) {
			t.Fatalf("provider request did not consume main-owned context: %s", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"候选正文"}}]}`))
	}))
	defer server.Close()

	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	responsePayload, _ := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_target", "success": true,
		"result": map[string]any{
			"kind": "read-result", "mutationPerformed": false,
			"data": map[string]any{
				"contextPackRef": "sha256:" + strings.Repeat("a", 64),
				"sections":       []map[string]any{{"kind": "chapter_text", "content": chapterContext, "revision": "sha256:" + strings.Repeat("b", 64), "truncated": false}},
			},
		},
	})
	if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-plan-run-1-context", RunID: "plan-run-1", Seq: 1, Payload: responsePayload,
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	frame := yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: writingRunPayload(t, server.URL, "agent_chat")}
	if err := runtime.HandleFrame(context.Background(), frame, &output); err != nil {
		t.Fatal(err)
	}
}

func TestWritingFrameRuntimeExecutesTheExistingStandardHarnessGraph(t *testing.T) {
	calls := 0
	requestBodies := [][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		requestBodies = append(requestBodies, body)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "stage-" + string(rune('0'+calls))},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 2, "total_tokens": 6},
		})
	}))
	defer server.Close()

	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	primeWritingContext(t, runtime, "plan-run-1")
	var request map[string]any
	if err := json.Unmarshal(writingRunPayload(t, server.URL, "structured_action"), &request); err != nil {
		t.Fatal(err)
	}
	request["harnessProfile"] = "novel-standard"
	request["subAgentSnapshot"] = writingSubAgentSnapshot(true)
	request["toolCapabilityManifest"] = writingDelegationManifest("plan-run-1", request["target"])
	payload, _ := json.Marshal(request)
	var output bytes.Buffer
	frame := yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: payload}
	if err := runtime.HandleFrame(context.Background(), frame, &output); err != nil {
		t.Fatal(err)
	}

	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 40)
	if err != nil {
		t.Fatal(err)
	}
	artifactKinds := []any{}
	artifactIDs := []any{}
	for _, event := range events {
		if event.Type == RunEventTypeArtifactCreated {
			artifactKinds = append(artifactKinds, event.Payload["artifactKind"])
			artifactIDs = append(artifactIDs, event.Payload["artifactId"])
		}
	}
	if calls != 3 {
		t.Fatalf("model calls = %d, want draft + reviewer + revision; terminal=%#v", calls, events[len(events)-1])
	}
	if !bytes.Contains(requestBodies[1], []byte("configured prompt for reviewer")) || !bytes.Contains(requestBodies[1], []byte("stage-1")) {
		t.Fatalf("reviewer request does not contain its prompt and draft Artifact: %s", requestBodies[1])
	}
	if !bytes.Contains(requestBodies[2], []byte("stage-2")) {
		t.Fatalf("primary revision request does not contain review Artifact: %s", requestBodies[2])
	}
	wantKinds := []any{"draft", "review", "transform", "report"}
	if !equalAnySlice(artifactKinds, wantKinds) {
		t.Fatalf("artifact kinds = %#v, want %#v", artifactKinds, wantKinds)
	}
	for _, required := range []RunEventType{RunEventTypeDelegationStarted, RunEventTypeReviewCompleted, RunEventTypeRevisionRequested, RunEventTypeCheckCompleted, RunEventTypeProposalReady, RunEventTypeRunCompleted} {
		found := false
		for _, event := range events {
			if event.Type == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("standard Harness event %s is missing", required)
		}
	}
	var started, completed *RunEvent
	for index := range events {
		switch events[index].Type {
		case RunEventTypeDelegationStarted:
			started = &events[index]
		case RunEventTypeDelegationCompleted:
			completed = &events[index]
		}
	}
	if started == nil || completed == nil {
		t.Fatal("delegation event pair is missing")
	}
	draftID, reviewID := artifactIDs[0], artifactIDs[1]
	for _, event := range []*RunEvent{started, completed} {
		if event.Payload["taskId"] != "task-plan-run-1-review" || event.Payload["parentRunId"] != "plan-run-1" || event.Payload["subAgentId"] != "reviewer" {
			t.Fatalf("delegation identity = %#v", event.Payload)
		}
		if !equalAnySlice(event.Payload["inputArtifactRefs"].([]any), []any{draftID}) {
			t.Fatalf("delegation input refs = %#v", event.Payload["inputArtifactRefs"])
		}
	}
	if completed.Payload["status"] != "completed" || !equalAnySlice(completed.Payload["outputArtifactRefs"].([]any), []any{reviewID}) {
		t.Fatalf("delegation completion = %#v", completed.Payload)
	}
}

func TestWritingFrameRuntimeStopsBeforeInvalidReviewerModelCall(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "disabled reviewer", mutate: func(request map[string]any) {
			request["subAgentSnapshot"] = writingSubAgentSnapshot(false)
		}},
		{name: "target mismatch", mutate: func(request map[string]any) {
			manifest := writingDelegationManifest("plan-run-1", map[string]any{"schemaVersion": "1", "kind": "chapter", "bookId": "book-other", "targetId": "chapter-other"})
			request["toolCapabilityManifest"] = manifest
		}},
		{name: "capability expansion", mutate: func(request map[string]any) {
			snapshot := writingSubAgentSnapshot(true)
			snapshot["agents"].([]map[string]any)[3]["capabilities"] = []string{"story.get_target", "writing.create_artifact"}
			request["subAgentSnapshot"] = snapshot
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				writer.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "draft only"}}}})
			}))
			defer server.Close()
			store, err := NewFileRuntimeEventStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			runtime, err := NewWritingFrameRuntime(store, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			primeWritingContext(t, runtime, "plan-run-1")
			var request map[string]any
			if err := json.Unmarshal(writingRunPayload(t, server.URL, "structured_action"), &request); err != nil {
				t.Fatal(err)
			}
			request["harnessProfile"] = "novel-standard"
			request["toolCapabilityManifest"] = writingDelegationManifest("plan-run-1", request["target"])
			test.mutate(request)
			payload, _ := json.Marshal(request)
			if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: payload}, io.Discard); err != nil {
				t.Fatal(err)
			}
			events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 40)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("model calls = %d, want draft only", calls)
			}
			for _, event := range events {
				if event.Type == RunEventTypeDelegationStarted || event.Type == RunEventTypeDelegationCompleted || event.Type == RunEventTypeReviewCompleted || event.Type == RunEventTypeProposalReady {
					t.Fatalf("invalid reviewer emitted %s", event.Type)
				}
				if event.Type == RunEventTypeArtifactCreated && event.Payload["artifactKind"] != "draft" {
					t.Fatalf("invalid reviewer created %#v", event.Payload)
				}
			}
			terminal := events[len(events)-1]
			if terminal.Type != RunEventTypeRunFailed || terminal.Payload["code"] != "delegation_invalid" {
				t.Fatalf("terminal = %#v", terminal)
			}
		})
	}
}

func TestWritingDelegationLinkRejectsInputArtifactMismatch(t *testing.T) {
	target := ToolTarget{SchemaVersion: "1", Kind: "chapter", BookID: "book-1", TargetID: "chapter-1"}
	targetJSON, _ := json.Marshal(target)
	request := planRunRequest{RunID: "run-1", Target: targetJSON}
	stage := WritingHarnessStage{ID: "review", RoleID: HarnessRoleReviewer, Delegated: true}
	artifacts := []writingRuntimeArtifact{{ID: "artifact-draft-1", Kind: "draft"}}
	base := DelegationRequest{
		TaskID: "task-run-1-review", ParentRunID: "run-1", SubAgentID: "reviewer",
		Target: target, InputArtifactRefs: []string{"artifact-draft-1"}, OutputContract: "harness-review-v1",
	}
	base.InputArtifactRefs = []string{"artifact-other"}
	if validateWritingDelegationLink(base, request, stage, artifacts, target) == nil {
		t.Fatal("mismatched delegation link was accepted")
	}
}

func TestWritingFrameRuntimeKeepsAuthorReadToolsInTheInitialStandardStage(t *testing.T) {
	const instruction = "请先调用读取未回收伏笔工具，找出验收暗号。不要猜。"
	const anchor = "TASK4-THREAD-ANCHOR-青铜钥匙缺口朝内"
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, body)
		writer.Header().Set("Content-Type", "application/json")
		message := map[string]any{"role": "assistant"}
		switch len(requests) {
		case 1:
			message["tool_calls"] = []map[string]any{{"id": "call-threads", "type": "function", "function": map[string]any{"name": "story.get_open_threads", "arguments": "{}"}}}
		case 2:
			message["tool_calls"] = []map[string]any{{"id": "call-characters", "type": "function", "function": map[string]any{"name": "story.get_characters", "arguments": "{}"}}}
		case 3:
			message["content"] = anchor + " 已写入首稿。"
		case 4:
			message["content"] = "审阅首稿并保留青铜钥匙伏笔。"
		case 5:
			message["content"] = anchor + " 修订稿。"
		default:
			t.Fatalf("unexpected provider request %d", len(requests))
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []map[string]any{{"message": message}}})
	}))
	defer server.Close()

	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sessions, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewWritingFrameRuntime(store, server.Client(), sessions)
	if err != nil {
		t.Fatal(err)
	}
	runID := "task4-standard-read-run"
	primeWritingContext(t, runtime, runID)
	for _, tool := range []struct {
		requestID string
		toolID    string
	}{
		{requestID: "tool-" + runID + "-model-1-0", toolID: "story.get_open_threads"},
		{requestID: "tool-" + runID + "-model-2-0", toolID: "story.get_characters"},
	} {
		response, marshalErr := json.Marshal(map[string]any{
			"schemaVersion": "1", "toolId": tool.toolID, "success": true,
			"result": map[string]any{"kind": "read-result", "mutationPerformed": false, "data": map[string]any{"summary": "读取作品资料", "fact": anchor}},
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: tool.requestID, RunID: runID, Seq: 2, Payload: response}); err != nil {
			t.Fatal(err)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &payload); err != nil {
		t.Fatal(err)
	}
	payload["harnessProfile"] = "novel-standard"
	payload["toolCapabilityManifest"] = writingDelegationManifest(runID, payload["target"])
	payload["userIntent"] = instruction
	payload["runId"] = runID
	payload["requestId"] = "request-" + runID
	payload["idempotencyKey"] = "idem-" + runID
	payload["sessionId"] = "task4-standard-session"
	payload["budgets"].(map[string]any)["maxModelCalls"] = 5
	payload["budgets"].(map[string]any)["maxToolRounds"] = 4
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-" + runID, Payload: encoded}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 5 {
		t.Fatalf("provider requests = %d, want two read rounds plus three standard stages", len(requests))
	}
	for index, request := range requests {
		if index < 3 {
			if !bytes.Contains(request, []byte("story_get_open_threads")) || !bytes.Contains(request, []byte(instruction)) {
				t.Fatalf("initial-stage request %d lost read authority or instruction: %s", index, request)
			}
			continue
		}
		if bytes.Contains(request, []byte("story_get_open_threads")) {
			t.Fatalf("later-stage request %d retained read authority: %s", index, request)
		}
		if !bytes.Contains(request, []byte("Initial data gathering is complete.")) || !bytes.Contains(request, []byte(instruction)) || !bytes.Contains(request, []byte(anchor)) {
			t.Fatalf("later-stage request %d lost the fact-bearing Artifact: %s", index, request)
		}
	}
	events, err := store.ReplayAfter(context.Background(), runID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	proposals, completed, terminals := 0, 0, 0
	for _, event := range events {
		if event.Type == RunEventTypeProposalReady {
			proposals++
		}
		if event.Type == RunEventTypeRunCompleted {
			completed++
		}
		if IsTerminalRunEventType(event.Type) {
			terminals++
		}
	}
	if proposals != 1 || completed != 1 || terminals != 1 || events[len(events)-1].Type != RunEventTypeRunCompleted {
		t.Fatalf("standard task4 terminal events = %#v", events)
	}
	writingSession, err := sessions.GetOrCreate("task4-standard-session")
	if err != nil {
		t.Fatal(err)
	}
	messages := writingSession.GetEffectiveMessages()
	if len(messages) == 0 || messages[len(messages)-1].Role != schema.Assistant || !strings.Contains(messages[len(messages)-1].Content, anchor) {
		t.Fatalf("final proposal candidate lost task4 anchor: %#v", messages)
	}
}

func equalAnySlice(left, right []any) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestWritingFrameRuntimeRejectsPlanAndUnknownCapabilityWithoutEvents(t *testing.T) {
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(map[string]any){
		func(value map[string]any) { value["planMode"] = true },
		func(value map[string]any) { value["capabilityId"] = "chapter.telepathy" },
	} {
		var value map[string]any
		if err := json.Unmarshal(writingRunPayload(t, "http://127.0.0.1:1", "structured_action"), &value); err != nil {
			t.Fatal(err)
		}
		mutate(value)
		payload, _ := json.Marshal(value)
		var output bytes.Buffer
		err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: payload}, &output)
		if err == nil {
			t.Fatal("invalid writing request was accepted")
		}
		if output.Len() != 0 {
			t.Fatalf("invalid request emitted output: %q", output.Bytes())
		}
	}
}

func TestWritingFrameRuntimeAcceptsValidatedSkillSelectionAndProjectsItIntoTheModelInstruction(t *testing.T) {
	const skillDocument = "# 章节重写与修改\nTASK7-FULL-SKILL-DOCUMENT\n保持角色性格和说话方式的一致性"
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte("Skill: rewrite")) || !bytes.Contains(body, []byte("TASK7-FULL-SKILL-DOCUMENT")) {
			t.Fatalf("model request did not contain validated Skill selection: %s", body)
		}
		if !bytes.Contains(body, []byte("Do not call read_file or write_file")) || !bytes.Contains(body, []byte("story.*")) {
			t.Fatalf("model request did not map legacy Skill file steps onto Yanzhou tools: %s", body)
		}
		requests = append(requests, body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"更自然的正文"}}]}`))
	}))
	defer server.Close()

	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	primeWritingContext(t, runtime, "plan-run-1", skillDocument)
	var request map[string]any
	if err := json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &request); err != nil {
		t.Fatal(err)
	}
	request["selectedSkillIds"] = []string{"rewrite"}
	request["capabilityId"] = "chapter.rewrite"
	request["budgets"].(map[string]any)["maxToolRounds"] = 4
	request["skillSnapshot"] = map[string]any{
		"schemaVersion": "1", "runId": "plan-run-1",
		"skills": []map[string]any{{
			"schemaVersion": "1", "id": "rewrite", "revision": 1,
			"checksum": "sha256:" + strings.Repeat("d", 64), "source": "builtin",
			"resources": []map[string]any{{"path": "SKILL.md", "checksum": "sha256:" + strings.Repeat("e", 64), "size": len(skillDocument)}},
		}},
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	frame := yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: payload}
	var output bytes.Buffer
	if err := runtime.HandleFrame(context.Background(), frame, &output); err != nil {
		t.Fatalf("HandleFrame rejected validated Skill selection: %v", err)
	}
	if len(requests) != 1 || bytes.Contains(requests[0], []byte(`"tools"`)) || !bytes.Contains(requests[0], []byte("Tool use is now forbidden")) {
		t.Fatalf("selected Skill did not use the supplied ContextPack directly: %q", requests)
	}
	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var loaded bool
	var todoStates [][]string
	for _, event := range events {
		if event.Type == RunEventTypeSkillLoaded && event.Payload["id"] == "rewrite" {
			loaded = true
		}
		if event.Type == RunEventTypeToolCompleted && event.Payload["toolId"] == "write_todos" {
			raw, _ := json.Marshal(event.Payload["todos"])
			var todos []writingTodo
			if json.Unmarshal(raw, &todos) == nil {
				states := make([]string, len(todos))
				for index := range todos {
					states[index] = todos[index].Status
				}
				todoStates = append(todoStates, states)
			}
		}
	}
	if !loaded || len(todoStates) < 2 || todoStates[0][0] != "in_progress" || todoStates[len(todoStates)-1][0] != "completed" {
		t.Fatalf("Skill/Todo run evidence is incomplete: loaded=%v states=%v", loaded, todoStates)
	}
}

func TestWritingFrameRuntimeRejectsMissingOrMismatchedSkillSnapshotBeforeModel(t *testing.T) {
	modelCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		modelCalls++
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	for name, snapshot := range map[string]any{
		"missing":     nil,
		"wrong-run":   map[string]any{"schemaVersion": "1", "runId": "other-run", "skills": []any{}},
		"wrong-skill": map[string]any{"schemaVersion": "1", "runId": "plan-run-1", "skills": []map[string]any{{"schemaVersion": "1", "id": "continue", "revision": 1, "checksum": "sha256:" + strings.Repeat("d", 64), "source": "builtin", "resources": []any{}}}},
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := NewFileRuntimeEventStore(t.TempDir())
			defer store.Close()
			runtime, err := NewWritingFrameRuntime(store, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			var request map[string]any
			_ = json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &request)
			request["selectedSkillIds"] = []string{"rewrite"}
			if snapshot != nil {
				request["skillSnapshot"] = snapshot
			}
			payload, _ := json.Marshal(request)
			var output bytes.Buffer
			if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: payload}, &output); err == nil {
				t.Fatal("invalid Skill snapshot was accepted")
			}
			if output.Len() != 0 {
				t.Fatalf("invalid Skill snapshot emitted run output: %q", output.Bytes())
			}
		})
	}
	if modelCalls != 0 {
		t.Fatalf("invalid Skill snapshot reached model %d times", modelCalls)
	}
}

func TestWritingFrameRuntimeCancelDuringStartedAppendEmitsAborted(t *testing.T) {
	baseStore, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer baseStore.Close()
	store := &writingRunStartedBarrierStore{
		RuntimeEventStore: baseStore,
		appendStarted:     make(chan struct{}),
		releaseAppend:     make(chan struct{}),
	}
	runtime, err := NewWritingFrameRuntime(store, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	frame := yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-1", Payload: writingRunPayload(t, "http://fixture.invalid", "agent_chat"),
	}
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- runtime.HandleFrame(context.Background(), frame, &output) }()

	select {
	case <-store.appendStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("writing run did not reach the first durable event append")
	}
	if err := runtime.CancelRun("plan-run-1"); err != nil {
		close(store.releaseAppend)
		t.Fatalf("CancelRun rejected the registered run: %v", err)
	}
	close(store.releaseAppend)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancel during run.started append returned an infrastructure error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled writing run did not terminate")
	}

	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != RunEventTypeRunAborted {
		t.Fatalf("events = %#v, want one run.aborted", events)
	}
}

func TestWritingFrameRuntimeCancelInterruptsTheExistingRunAndPreservesItsTerminal(t *testing.T) {
	started := make(chan struct{})
	client := &http.Client{Transport: writingRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}

	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, client)
	if err != nil {
		t.Fatal(err)
	}
	primeWritingContext(t, runtime, "plan-run-1")
	frame := yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-1", Payload: writingRunPayload(t, "http://fixture.invalid", "agent_chat"),
	}
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- runtime.HandleFrame(context.Background(), frame, &output) }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("writing provider call did not start")
	}
	if err := runtime.CancelRun("plan-run-1"); err != nil {
		t.Fatalf("CancelRun rejected the active run: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("canceled run returned an infrastructure error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled writing run did not terminate")
	}

	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].Type != RunEventTypeRunAborted {
		t.Fatalf("terminal events = %#v, want run.aborted", events)
	}
	for _, event := range events {
		if event.Type == RunEventTypeRunCompleted || event.Type == RunEventTypeRunFailed {
			t.Fatalf("cancel emitted conflicting terminal %s", event.Type)
		}
	}
}

func TestWritingStageInputTruncatesLongChineseArtifactAtAUTF8Boundary(t *testing.T) {
	content := strings.Repeat("你", 6000)
	input := writingStageInput("继续", "", []writingRuntimeArtifact{{StageID: "draft", Kind: "draft", Content: content}})
	if !utf8.ValidString(input) {
		t.Fatal("writing stage input split a UTF-8 code point")
	}
}

func TestWritingHarnessFeatureIsExplicitlyNegotiated(t *testing.T) {
	gate, err := NewBootstrapTokenGate("fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	response, err := Handshake(yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		ClientBuild:     "wp8-test", WorkspaceSchema: "yanzhou-book/1",
		BootstrapToken: "fixture-token", RequestedFeatures: []string{"handshake", "writing-harness"},
	}, gate, yanzhouprotocol.Provenance{
		SchemaVersion: "1", UpstreamRepository: "denova",
		UpstreamBaseSHA:   "a111111111111111111111111111111111111111",
		AdapterCommitSHA:  "b222222222222222222222222222222222222222",
		SourceTreeSHA:     "c333333333333333333333333333333333333333",
		BinarySHA256:      "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		SkillsManifestSHA: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		GoVersion:         "go1.26.5", TargetOS: "darwin", TargetArch: "arm64", BuiltAt: "2026-07-25T00:00:00Z",
	}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(response.SupportedFeatures) != 2 || response.SupportedFeatures[1] != "writing-harness" {
		t.Fatalf("supported features = %#v", response.SupportedFeatures)
	}
}
