package yanzhouadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"denova/internal/yanzhouprotocol"
)

type panicOnceRuntimeStore struct {
	RuntimeEventStore
	panicked bool
}

func (store *panicOnceRuntimeStore) Append(ctx context.Context, runID string, input RuntimeEventInput) (RunEvent, error) {
	if !store.panicked {
		store.panicked = true
		panic("fixture runtime panic")
	}
	return store.RuntimeEventStore.Append(ctx, runID, input)
}

func TestWritingFramePanicEmitsFailedTerminal(t *testing.T) {
	if os.Getenv("DENOVA_WRITING_PANIC_HELPER") == "1" {
		baseStore, err := NewFileRuntimeEventStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer baseStore.Close()
		runtime, err := NewWritingFrameRuntime(&panicOnceRuntimeStore{RuntimeEventStore: baseStore}, nil)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(planRunRequest{
			SchemaVersion: "1", RequestID: "request-run-panic", IdempotencyKey: "idem-run-panic",
			RunID: "run-panic", SessionID: "session-run-panic", AgentKind: "ide",
			Entrypoint: "structured_action", CapabilityID: "chapter.generate_from_outline",
			UserIntent: "生成候选", PlanMode: false, SelectedSkillIDs: []string{}, HarnessProfile: "novel-lite",
			Target: json.RawMessage(`{"schemaVersion":"1","kind":"chapter","bookId":"book-1","targetId":"chapter-1"}`),
			EffectiveModelProfile: planModelProfile{
				ProfileID: "fixture", ProviderType: ProviderOpenAICompatible, AdapterID: "openai-compatible",
				BaseURL: "http://127.0.0.1:1", Model: "fixture", TimeoutMS: 1000,
				Capabilities: json.RawMessage(`{"streaming":true}`), RuntimeAuth: RuntimeAuth{Mode: RuntimeAuthNone},
				Resolution: json.RawMessage(`{"source":"run"}`),
			},
			ContextPackRef: planContentRef{Ref: "sha256:" + strings.Repeat("f", 64)},
			ToolManifest:   json.RawMessage(`{"schemaVersion":"1"}`),
			Budgets:        planRunBudget{MaxModelCalls: 1, MaxWallTimeMS: 1000},
			BaseRevisions:  map[string]string{"chapter-1": "revision-1"}, DisplayLocale: "zh-CN",
		})
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		frame := yanzhouprotocol.Envelope{
			Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
			RequestID: "request-run-panic", RunID: "run-panic", Payload: payload,
		}
		done := make(chan error, 1)
		go func() {
			defer func() {
				if recover() != nil {
					done <- runtime.FailPanic(context.Background(), frame, &output)
				}
			}()
			done <- runtime.HandleFrame(context.Background(), frame, &output)
		}()
		err = <-done
		if err != nil {
			t.Fatal(err)
		}
		reader := yanzhouprotocol.NewReader(&output, yanzhouprotocol.DefaultMaxFrameBytes)
		outputFrame, err := reader.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		var event RunEvent
		if err := json.Unmarshal(outputFrame.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != RunEventTypeRunFailed || event.Payload["reason"] != "panic" {
			t.Fatalf("panic terminal = %#v", event)
		}
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestWritingFramePanicEmitsFailedTerminal$")
	command.Env = append(os.Environ(), "DENOVA_WRITING_PANIC_HELPER=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("writing frame panic escaped its runtime boundary: %v\n%s", err, output)
	}
}
