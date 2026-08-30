package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"denova/internal/interactive"
)

// blockingReadyConversation delays every readiness probe until the final
// TurnResult submission has stored ready=true. This deterministically forces
// the consumer to observe the "future" ready state while handling prose events
// that were produced earlier — the exact interleaving the first-candidate lock
// must survive, without relying on -count repetition to hit it by chance.
type blockingReadyConversation struct {
	interactiveProtocolConversation
	barrier  chan struct{}
	timedOut atomic.Bool
}

func (c *blockingReadyConversation) InteractiveNarrativeReady() bool {
	select {
	case <-c.barrier:
	case <-time.After(10 * time.Second):
		c.timedOut.Store(true)
	}
	return c.ready.Load()
}

func TestInteractiveTurnProtocolLocksFirstCandidateWhenReadyAdvancesBeforeConsumption(t *testing.T) {
	ctx := context.Background()
	var ready atomic.Bool
	barrier := make(chan struct{})
	barrierClosed := false
	var mu sync.Mutex
	patchesAccepted := false
	choicesAccepted := false
	tools, err := newInteractiveTurnTools(InteractiveStoryToolContext{
		SubmitTurnResult: func(_ context.Context, input interactive.TurnSubmissionInput) (interactive.TurnSubmissionReceipt, error) {
			mu.Lock()
			defer mu.Unlock()
			patchRejected := false
			for _, diagnostic := range input.Diagnostics {
				if diagnostic.Module == interactive.TurnSubmissionModuleStateChanges {
					patchRejected = true
				}
			}
			if input.StateUpdates != nil {
				patchesAccepted = true
			}
			if input.Choices != nil {
				choicesAccepted = true
			}
			settled := patchesAccepted && choicesAccepted
			if settled {
				ready.Store(true)
				if !barrierClosed {
					barrierClosed = true
					close(barrier)
				}
			}
			patchStatus := interactive.TurnSubmissionModuleMissing
			switch {
			case patchesAccepted:
				patchStatus = interactive.TurnSubmissionModuleAccepted
			case patchRejected:
				patchStatus = interactive.TurnSubmissionModuleRejected
			}
			choiceStatus := interactive.TurnSubmissionModuleMissing
			if choicesAccepted {
				choiceStatus = interactive.TurnSubmissionModuleAccepted
			}
			return interactive.TurnSubmissionReceipt{Ready: settled, ModuleStatus: interactive.TurnSubmissionModuleStatus{
				StateChanges: patchStatus, Choices: choiceStatus,
			}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	const candidateA = "乱石坡上，主角藏在巨石后观察二十五丈外的敌人。"
	const laterProseB = "废弃灵木料场里，主角躲在断木桩后，看见十五丈外持破灵镜的瘦高个。"
	chatModel := &interactiveTurnProtocolChatModel{responses: []*schema.Message{
		schema.AssistantMessage(candidateA, nil),
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "bad-state-good-choices", Function: schema.FunctionCall{Name: interactiveTurnSubmissionToolName, Arguments: `{"state_changes":"not-an-array","choices":["继续观察","绕到侧面","悄然后退","制造声响","询问同伴"]}`},
		}}),
		schema.AssistantMessage(laterProseB, nil),
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "good-state", Function: schema.FunctionCall{Name: interactiveTurnSubmissionToolName, Arguments: `{"state_changes":[{"op":"replace","actor_id":"story","field_id":"当前事件","value":"主角在乱石坡观察敌情"}]}`},
		}}),
	}}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "interactive-protocol-deterministic-lock-test", Description: "test", Instruction: "test", Model: chatModel, MaxIterations: 6,
		Handlers:         []adk.ChatModelAgentMiddleware{newInteractiveTurnProtocolMiddleware(ready.Load)},
		ToolsConfig:      adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools}},
		ModelRetryConfig: &adk.ModelRetryConfig{MaxRetries: 1, ShouldRetry: newInteractiveCompletionGuard(ready.Load), BackoffFunc: func(context.Context, int) time.Duration { return time.Nanosecond }},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	conversation := &blockingReadyConversation{barrier: barrier}
	conversation.ready = &ready
	NewRuntime(DefaultLoopPolicy()).Run(ctx, runner, conversation, nil, ChatRequest{Message: "观察敌情"}, RunOptions{
		AgentKind: AgentKindInteractiveStory, RootAgentName: "interactive-protocol-deterministic-lock-test",
	}, func(Event) {})

	if conversation.timedOut.Load() {
		t.Fatal("readiness barrier timed out; the forced producer/consumer interleaving was not established")
	}
	calls, _, _ := chatModel.snapshot()
	if calls != 4 || !ready.Load() {
		t.Fatalf("protocol did not settle as scripted: calls=%d ready=%t", calls, ready.Load())
	}
	if conversation.assistant != candidateA {
		t.Fatalf("first candidate was not locked once ready advanced ahead of consumption: assistant=%q", conversation.assistant)
	}
}
