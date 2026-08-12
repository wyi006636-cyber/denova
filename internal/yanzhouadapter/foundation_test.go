package yanzhouadapter

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"denova/internal/yanzhouprotocol"
)

func foundationProvenance() yanzhouprotocol.Provenance {
	return yanzhouprotocol.Provenance{
		SchemaVersion: "1", UpstreamRepository: "denova",
		UpstreamBaseSHA:   "a" + strings.Repeat("1", 39),
		AdapterCommitSHA:  "b" + strings.Repeat("2", 39),
		SourceTreeSHA:     "c" + strings.Repeat("3", 39),
		BinarySHA256:      strings.Repeat("d", 64),
		SkillsManifestSHA: strings.Repeat("e", 64),
		GoVersion:         "go1.26.5", TargetOS: "darwin", TargetArch: "arm64",
		BuiltAt: "2026-08-12T00:00:00Z",
	}
}

func TestHandshakeNegotiatesFoundationFeatures(t *testing.T) {
	gate, err := NewBootstrapTokenGate("single-use-token")
	if err != nil {
		t.Fatal(err)
	}
	request := yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		ClientBuild:     "yanzhou-test", WorkspaceSchema: "yanzhou-book/1",
		BootstrapToken: "single-use-token",
		RequestedFeatures: []string{
			"handshake", "plan-mode", "writing-harness", "skills-v2", "sub-agents", "future-feature",
		},
	}
	response, err := Handshake(request, gate, foundationProvenance(), "foundation-test")
	if err != nil {
		t.Fatal(err)
	}
	want := "[handshake plan-mode writing-harness skills-v2 sub-agents]"
	if got := fmt.Sprint(response.SupportedFeatures); got != want {
		t.Fatalf("features = %s, want %s", got, want)
	}
	if len(response.AgentKinds) != 9 {
		t.Fatalf("agent kinds = %d, want 9", len(response.AgentKinds))
	}
	if _, err := Handshake(request, gate, foundationProvenance(), "foundation-test"); err == nil {
		t.Fatal("bootstrap token must be single use")
	}
}

func TestPlanControlProtocolHandlesStreamsAndStructuredTools(t *testing.T) {
	parser := NewPlanStreamParser()
	first, err := parser.Push("先确认<plan_ques")
	if err != nil {
		t.Fatal(err)
	}
	second, err := parser.Push(`tions>{"schemaVersion":"1"}</plan_questions>不得显示`)
	if err != nil {
		t.Fatal(err)
	}
	if first.Visible+second.Visible != "先确认" || len(second.Blocks) != 1 || second.Blocks[0].Kind != PlanBlockQuestions || !second.Stop {
		t.Fatalf("stream result = %#v / %#v", first, second)
	}

	raw := `{"schemaVersion":"1","id":"plan-1","revision":1,"status":"proposed","summary":"计划","sections":[],"approvals":{}}`
	block, handled, err := ParsePlanToolCall("proposed_plan", raw)
	if err != nil || !handled || block.Kind != PlanBlockProposal || block.Content != raw {
		t.Fatalf("structured tool = %#v, %t, %v", block, handled, err)
	}
	if _, handled, err := ParsePlanToolCall("plan_questions", `{"questions":`); !handled || err == nil {
		t.Fatalf("truncated tool handled=%t err=%v", handled, err)
	}

	unclosed := NewPlanStreamParser()
	_, _ = unclosed.Push("<proposed_plan>{}")
	if _, err := unclosed.Flush(); err == nil {
		t.Fatal("unclosed plan block must fail")
	}
}

func TestModelAndHarnessContractsAreAvailable(t *testing.T) {
	for provider, adapterID := range map[ProviderType]string{
		ProviderOpenAICompatible: "openai-compatible",
		ProviderAnthropicNative:  "anthropic-native",
		ProviderGeminiNative:     "gemini-native",
		ProviderOllama:           "ollama",
		ProviderLMStudio:         "lm-studio",
		ProviderCustomLocal:      "openai-compatible",
	} {
		adapter, err := NewModelAdapter(EffectiveModelProfile{
			ProfileID: "fixture", ProviderType: provider, AdapterID: adapterID,
			BaseURL: "https://fixture.invalid", Model: "fixture", TimeoutMS: 1000,
			RuntimeAuth: RuntimeAuth{Mode: RuntimeAuthNone},
		})
		if err != nil || adapter.AdapterID() != adapterID {
			t.Fatalf("provider %s = %v, %v", provider, adapter, err)
		}
	}
	for _, profile := range WritingHarnessProfiles() {
		if err := profile.Validate(); err != nil {
			t.Fatalf("profile %s: %v", profile.ID, err)
		}
	}
}

func TestRuntimeEventsAppendReplayAndStopAfterTerminal(t *testing.T) {
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	started, err := store.Append(ctx, "run-1", RuntimeEventInput{
		Type: RunEventTypeRunStarted, Payload: map[string]any{"entrypoint": "agent_chat"},
	})
	if err != nil || started.Seq != 1 {
		t.Fatalf("started = %#v, %v", started, err)
	}
	terminal, err := store.Append(ctx, "run-1", RuntimeEventInput{
		Type: RunEventTypeRunCompleted,
		Payload: map[string]any{
			"schemaVersion": "1", "reason": "completed", "resumable": false,
			"partialArtifactRefs": []string{},
		},
	})
	if err != nil || terminal.Seq != 2 {
		t.Fatalf("terminal = %#v, %v", terminal, err)
	}
	events, err := store.ReplayAfter(ctx, "run-1", 0, 10)
	if err != nil || len(events) != 2 || events[0].Type != RunEventTypeRunStarted || events[1].Type != RunEventTypeRunCompleted {
		t.Fatalf("events = %#v, %v", events, err)
	}
	if _, err := store.Append(ctx, "run-1", RuntimeEventInput{Type: RunEventTypeModelDelta, Payload: map[string]any{"text": "late"}}); err == nil {
		t.Fatal("event after terminal must be rejected")
	}
}
