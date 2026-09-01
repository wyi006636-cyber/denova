package yanzhouadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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
	value["selectedSkillIds"] = []string{}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func primeWritingContext(t *testing.T, runtime *WritingFrameRuntime, runID string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_target", "success": true,
		"result": map[string]any{
			"kind": "read-result", "mutationPerformed": false,
			"data": map[string]any{
				"contextPackRef": "sha256:" + strings.Repeat("a", 64),
				"sections":       []map[string]any{{"kind": "chapter_text", "content": "已处理的章节上下文", "revision": "sha256:" + strings.Repeat("b", 64), "truncated": false}},
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
	if got := events[4].Payload["text"]; got != "雨落在旧站台上，林青没有回头。" {
		t.Fatalf("model delta text = %#v", got)
	}
	if got := events[5].Payload["artifactKind"]; got != "draft" {
		t.Fatalf("artifact kind = %#v", got)
	}
	if got := events[5].Payload["entrypoint"]; got != "agent_chat" {
		t.Fatalf("entrypoint = %#v", got)
	}
}

func TestMergeWritingToolCallKeepsParallelStreamArgumentsSeparated(t *testing.T) {
	calls := []ModelToolCall{}
	for _, delta := range []ModelToolCall{
		{ID: "call-a", Name: "story.search_chapters", Arguments: `{"query":"`, StreamIndex: 0},
		{ID: "call-b", Name: "story.get_characters", Arguments: `{"query":"`, StreamIndex: 1},
		{Arguments: `离开"}`, StreamIndex: 0},
		{Arguments: `林青"}`, StreamIndex: 1},
	} {
		mergeWritingToolCall(&calls, delta)
	}
	if len(calls) != 2 || calls[0].Arguments != `{"query":"离开"}` || calls[1].Arguments != `{"query":"林青"}` {
		t.Fatalf("parallel calls merged incorrectly: %#v", calls)
	}
}

func TestWritingFrameRuntimeRunsMultiTurnReadOnlyAgentChatWithModelSelectedTools(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte("他之前不是说不会离开吗？")) || !bytes.Contains(body, []byte("那是他当时的自我欺骗。")) {
			t.Fatalf("provider request lost conversation history: %s", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []map[string]any{{
						"id": "call-1", "type": "function", "function": map[string]any{"name": "story.search_chapters", "arguments": `{"query":"离开"}`},
					}}},
					"finish_reason": "tool_calls",
				}},
			})
			return
		}
		if !bytes.Contains(body, []byte("call-1")) || !bytes.Contains(body, []byte("旧站台")) {
			t.Fatalf("provider request lost tool result: %s", body)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "他离开不是反悔，而是终于承认自己一直在逃避旧站台的创伤。"},
				"finish_reason": "stop",
			}},
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
	toolPayload, _ := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.search_chapters", "success": true,
		"result": map[string]any{"kind": "read-result", "mutationPerformed": false, "data": map[string]any{"matches": []string{"旧站台"}}},
	})
	if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-plan-run-1-agent-1-1", RunID: "plan-run-1", Seq: 2, Payload: toolPayload,
	}); err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err := json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &request); err != nil {
		t.Fatal(err)
	}
	request["capabilityId"] = "agent.chat"
	request["userIntent"] = "所以这一章人物为什么突然离开？"
	request["conversation"] = []map[string]string{
		{"role": "user", "content": "他之前不是说不会离开吗？"},
		{"role": "assistant", "content": "那是他当时的自我欺骗。"},
	}
	request["budgets"].(map[string]any)["maxToolRounds"] = 4
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
	var artifactCount, proposalCount int
	var finalText string
	for _, event := range events {
		if event.Type == RunEventTypeArtifactCreated {
			artifactCount++
		}
		if event.Type == RunEventTypeProposalReady {
			proposalCount++
		}
		if event.Type == RunEventTypeModelDelta {
			finalText += event.Payload["text"].(string)
		}
	}
	if calls != 2 || artifactCount != 0 || proposalCount != 0 {
		t.Fatalf("calls=%d artifacts=%d proposals=%d events=%#v", calls, artifactCount, proposalCount, events)
	}
	if finalText != "他离开不是反悔，而是终于承认自己一直在逃避旧站台的创伤。" {
		t.Fatalf("final response = %q", finalText)
	}
	if events[len(events)-1].Type != RunEventTypeRunCompleted {
		t.Fatalf("terminal event = %s", events[len(events)-1].Type)
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
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"候选正文"},"finish_reason":"stop"}]}`))
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
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		content := "stage-" + string(rune('0'+calls))
		if calls == 2 {
			content = `{"schemaVersion":"1","status":"pass","findings":[]}`
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": content},
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
	for _, event := range events {
		if event.Type == RunEventTypeArtifactCreated {
			artifactKinds = append(artifactKinds, event.Payload["artifactKind"])
		}
	}
	if calls != 3 {
		t.Fatalf("model calls = %d, want draft + reviewer + revision", calls)
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
}

func runWritingRuntimeCase(t *testing.T, capabilityID string, response func(int) string, finishReasonOverride ...string) ([]RunEvent, int, []byte) {
	t.Helper()
	calls := 0
	var providerBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		providerBody, _ = io.ReadAll(request.Body)
		if capabilityID == "chapter.polish" {
			finishReason := "stop"
			if len(finishReasonOverride) > 0 {
				finishReason = finishReasonOverride[0]
			}
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"完整润色候选\"},\"finish_reason\":\"\"}]}\n\n")
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\""+finishReason+"\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}\n\n")
			_, _ = io.WriteString(writer, "data: [DONE]\n\n")
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		finishReason := "stop"
		if len(finishReasonOverride) > 0 {
			finishReason = finishReasonOverride[0]
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []map[string]any{{
			"message": map[string]any{"content": response(calls)}, "finish_reason": finishReason,
		}}})
	}))
	defer server.Close()
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, _ := NewWritingFrameRuntime(store, server.Client())
	primeWritingContext(t, runtime, "plan-run-1")
	var request map[string]any
	_ = json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &request)
	request["capabilityId"], request["harnessProfile"], request["selectedSkillIds"] = capabilityID, "novel-standard", []string{}
	if capabilityID == "chapter.polish" {
		request["promptComponentSnapshot"] = map[string]any{
			"schemaVersion": "1",
			"slug":          "polish.standard",
			"version":       4,
			"slotValues": map[string]any{
				"style": "standard", "intensity": "moderate",
			},
			"systemInstruction": "你是一名专业的中文小说润色编辑。这不是轻量校对。按适中力度逐段审视表达，保留叙事含义，不保留原句措辞，允许重写句子，系统提升文学性、易读性、节奏与画面表达。开头、中段和后段都必须处理到。落实画面实物化。",
		}
	}
	payload, _ := json.Marshal(request)
	var output bytes.Buffer
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: payload}, &output); err != nil {
		t.Fatal(err)
	}
	events, _ := store.ReplayAfter(context.Background(), "plan-run-1", 0, 40)
	return events, calls, providerBody
}

func TestWritingFrameRuntimePublishesModelDeltaBeforeProviderStreamCompletes(t *testing.T) {
	firstChunkSent := make(chan struct{})
	releaseProvider := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"第一段\"},\"finish_reason\":\"\"}]}\n\n")
		writer.(http.Flusher).Flush()
		close(firstChunkSent)
		<-releaseProvider
		for range 1000 {
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"续\"},\"finish_reason\":\"\"}]}\n\n")
		}
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
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
	_ = json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &request)
	request["capabilityId"], request["harnessProfile"] = "chapter.polish", "novel-standard"
	request["promptComponentSnapshot"] = map[string]any{
		"schemaVersion": "1", "slug": "polish.standard", "version": 4,
		"slotValues":        map[string]any{"style": "standard", "intensity": "moderate"},
		"systemInstruction": "你是一名专业的中文小说润色编辑。这不是轻量校对。按适中力度逐段审视表达，保留叙事含义，不保留原句措辞，允许重写句子，系统提升文学性、易读性、节奏与画面表达。开头、中段和后段都必须处理到。落实画面实物化。",
	}
	payload, _ := json.Marshal(request)
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: payload}, &output)
	}()
	select {
	case <-firstChunkSent:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not send the first stream chunk")
	}
	seenLiveDelta := false
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		events, _ := store.ReplayAfter(context.Background(), "plan-run-1", 0, 20)
		for _, event := range events {
			if event.Type == RunEventTypeModelDelta && event.Payload["text"] == "第一段" {
				seenLiveDelta = true
				break
			}
		}
		if seenLiveDelta {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(releaseProvider)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !seenLiveDelta {
		t.Fatal("model.delta was buffered until the provider stream completed")
	}
	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	published := []string{}
	for _, event := range events {
		if event.Type == RunEventTypeModelDelta && event.Payload["source"] == "model" {
			published = append(published, event.Payload["text"].(string))
		}
	}
	if actual, want := strings.Join(published, ""), "第一段"+strings.Repeat("续", 1000); actual != want {
		t.Fatalf("coalesced live deltas were not lossless: bytes=%d want=%d", len(actual), len(want))
	}
	if len(published) > 16 {
		t.Fatalf("token-sized provider frames produced too many durable deltas: %d", len(published))
	}
}

func TestNormalizeWritingStreamReaderBoundsPublishedDeltas(t *testing.T) {
	content := strings.Repeat("海", 2000)
	payload, err := json.Marshal(map[string]any{
		"choices": []map[string]any{{
			"delta":         map[string]any{"content": content},
			"finish_reason": "stop",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream := "data: " + string(payload) + "\n\ndata: [DONE]\n\n"
	published := []string{}
	response, err := normalizeWritingStreamReader(&openAICompatibleAdapter{}, strings.NewReader(stream), func(text string) error {
		if len([]byte(text)) > 4096 {
			t.Fatalf("published delta exceeded 4096 bytes: %d", len([]byte(text)))
		}
		published = append(published, text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != content || strings.Join(published, "") != content || len(published) < 2 {
		t.Fatalf("large stream event was not losslessly bounded: chunks=%d", len(published))
	}
}

func TestNormalizeWritingStreamReaderRejectsTruncatedStream(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"未完成\"},\"finish_reason\":\"\"}]}\n\n"
	published := []string{}
	response, err := normalizeWritingStreamReader(&openAICompatibleAdapter{}, strings.NewReader(stream), func(text string) error {
		published = append(published, text)
		return nil
	})
	if err == nil || response.Content != "" || strings.Join(published, "") != "未完成" {
		t.Fatalf("truncated stream must fail after publishing only the live partial delta: response=%#v err=%v", response, err)
	}
}

func TestNormalizeWritingStreamReaderPreservesAnthropicToolState(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"可见文字"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool-1","name":"unsafe","input":{}}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"x\"}"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		`data: {"type":"message_stop"}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	published := []string{}
	response, err := normalizeWritingStreamReader(&anthropicAdapter{}, strings.NewReader(stream), func(text string) error {
		published = append(published, text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(published, "") != "可见文字" || response.Content != "可见文字" {
		t.Fatalf("anthropic text stream was not preserved: %#v", response)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].ID != "tool-1" || response.FinishReason != "tool_calls" {
		t.Fatalf("anthropic tool stream state was lost: %#v", response)
	}
}

func TestNormalizeWritingStreamReaderRejectsAnthropicStreamWithoutMessageStop(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":2}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"未完成"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
	}, "\n\n") + "\n\n"
	published := []string{}
	_, err := normalizeWritingStreamReader(&anthropicAdapter{}, strings.NewReader(stream), func(text string) error {
		published = append(published, text)
		return nil
	})
	if err == nil || strings.Join(published, "") != "未完成" {
		t.Fatalf("anthropic stream without message_stop must fail after only a live partial delta: err=%v", err)
	}
}

func TestWritingFrameRuntimeDecouplesPolishAndReview(t *testing.T) {
	for _, tc := range []struct {
		capability, content, artifactKind string
		proposal                          bool
	}{
		{"chapter.polish", "完整润色候选", "transform", true},
		{"chapter.review", `{"schemaVersion":"1","status":"fail","findings":[]}`, "review", false},
	} {
		t.Run(tc.capability, func(t *testing.T) {
			events, calls, body := runWritingRuntimeCase(t, tc.capability, func(int) string { return tc.content })
			if calls != 1 {
				t.Fatalf("model calls = %d, want 1", calls)
			}
			seenProposal := false
			firstArtifactID, proposalArtifactID := "", ""
			for _, event := range events {
				if event.Type == RunEventTypeDelegationStarted || event.Type == RunEventTypeRevisionRequested || (tc.capability == "chapter.polish" && event.Type == RunEventTypeReviewCompleted) {
					t.Fatalf("coupled event %s", event.Type)
				}
				if event.Type == RunEventTypeArtifactCreated {
					if firstArtifactID == "" {
						firstArtifactID, _ = event.Payload["artifactId"].(string)
					}
					if event.Payload["artifactKind"] != tc.artifactKind && event.Payload["artifactKind"] != "report" {
						t.Fatalf("artifact kind = %#v", event.Payload["artifactKind"])
					}
				}
				if event.Type == RunEventTypeProposalReady {
					seenProposal = true
					proposalArtifactID, _ = event.Payload["artifactId"].(string)
				}
			}
			if seenProposal != tc.proposal || (tc.proposal && proposalArtifactID != firstArtifactID) {
				t.Fatalf("proposal.ready = %t, want %t", seenProposal, tc.proposal)
			}
			if tc.capability == "chapter.polish" {
				for _, required := range []string{"polish.standard", "不是轻量校对", "适中力度", "保留叙事含义，不保留原句措辞", "允许重写句子", "文学性、易读性、节奏与画面表达", "开头、中段和后段都必须处理到", "待润色正文·第1/4部分", "待润色正文·第4/4部分", "画面实物化", "\"stream\":true", "完整候选正文", "不输出分析"} {
					if !bytes.Contains(body, []byte(required)) {
						t.Fatalf("polish instruction is missing %q", required)
					}
				}
				if bytes.Contains(body, []byte("未需修改的位置尽量原样保留")) {
					t.Fatal("polish instruction regressed to conservative proofreading")
				}
			}
		})
	}
}

func TestWritingFrameRuntimeTruncatedPolishOutputFailsClosed(t *testing.T) {
	events, calls, _ := runWritingRuntimeCase(t, "chapter.polish", func(int) string {
		return "不应使用的非流式候选"
	}, "length")
	if calls != 1 {
		t.Fatalf("model calls = %d, want 1", calls)
	}
	for _, event := range events {
		if event.Type == RunEventTypeArtifactCreated || event.Type == RunEventTypeProposalReady || event.Type == RunEventTypeRunCompleted {
			t.Fatalf("truncated polish emitted %s", event.Type)
		}
	}
	if len(events) == 0 || events[len(events)-1].Type != RunEventTypeRunFailed {
		t.Fatalf("terminal event = %#v, want run.failed", events)
	}
}

func TestWritingFrameRuntimeNonStreamWithoutStopFinishFailsClosed(t *testing.T) {
	for _, finishReason := range []string{"", "unknown", "refusal"} {
		t.Run(finishReason, func(t *testing.T) {
			events, calls, _ := runWritingRuntimeCase(t, "chapter.continue", func(int) string {
				return "看似完整但未确认正常结束的正文"
			}, finishReason)
			if calls != 1 || len(events) == 0 || events[len(events)-1].Type != RunEventTypeRunFailed {
				t.Fatalf("finish=%q calls=%d terminal=%#v", finishReason, calls, events)
			}
			for _, event := range events {
				if event.Type == RunEventTypeArtifactCreated || event.Type == RunEventTypeProposalReady || event.Type == RunEventTypeRunCompleted {
					t.Fatalf("finish=%q emitted unsafe %s", finishReason, event.Type)
				}
			}
		})
	}
}

func TestWritingFrameRuntimeMalformedReviewerOutputFailsClosed(t *testing.T) {
	events, calls, _ := runWritingRuntimeCase(t, "chapter.rewrite", func(call int) string {
		if call == 1 {
			return "初次候选正文"
		}
		return strings.Repeat("这是一整章正文，不是结构化审稿报告。", 100)
	})
	if calls != 2 || len(events) == 0 || events[len(events)-1].Type != RunEventTypeRunFailed {
		t.Fatalf("calls=%d terminal=%#v", calls, events)
	}
	for _, event := range events {
		if event.Type == RunEventTypeReviewCompleted && event.Payload["status"] == "pass" {
			t.Fatal("malformed reviewer output was marked pass")
		}
		if event.Type == RunEventTypeRevisionRequested || event.Type == RunEventTypeProposalReady {
			t.Fatalf("malformed review continued into %s", event.Type)
		}
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
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte("Skill: rewrite")) {
			t.Fatalf("model request did not contain validated Skill selection: %s", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"更自然的正文"},"finish_reason":"stop"}]}`))
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
	if err := json.Unmarshal(writingRunPayload(t, server.URL, "agent_chat"), &request); err != nil {
		t.Fatal(err)
	}
	request["selectedSkillIds"] = []string{"rewrite"}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	frame := yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: payload}
	var output bytes.Buffer
	if err := runtime.HandleFrame(context.Background(), frame, &output); err != nil {
		t.Fatalf("HandleFrame rejected validated Skill selection: %v", err)
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
