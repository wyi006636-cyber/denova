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
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
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
