package execution

import (
	"context"
	"strings"
	"testing"

	"denova/config"
	agentchat "denova/internal/agents/chat"
	agentconversation "denova/internal/agents/conversation"
	agentrun "denova/internal/agents/run"
	"denova/internal/agents/session"
	agenttoolruntime "denova/internal/agents/toolruntime"
	"denova/internal/agents/tools"
	"denova/internal/book/lore"

	"github.com/alfredxw/denova/agent"
	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
)

type nativeGameLoreCursorModel struct{ calls int }

func (model *nativeGameLoreCursorModel) Generate(_ context.Context, _ []*agentschema.Message, _ ...agentmodel.ModelOption) (*agentschema.Message, error) {
	model.calls++
	if model.calls == 1 {
		return agentschema.AssistantMessage("query", []agentschema.ToolCall{{
			ID: "query-lore", Type: "function", Function: agentschema.FunctionCall{
				Name: "query_lore_items", Arguments: `{"names":["Cursor Secret"],"detail":"full"}`,
			},
		}}), nil
	}
	return agentschema.AssistantMessage("done", nil), nil
}

func (model *nativeGameLoreCursorModel) Stream(ctx context.Context, messages []*agentschema.Message, options ...agentmodel.ModelOption) (*stream.StreamReader[*agentschema.Message], error) {
	message, err := model.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return stream.StreamReaderFromArray([]*agentschema.Message{message}), nil
}

// TestProbeNativeGameToolCursor mirrors the app service's acceptCtx binding,
// lore tool catalog and interactive-story Agent kind, then runs the real
// public Agent lifecycle with only the model replaced by a fixture.
func TestProbeNativeGameToolCursor(t *testing.T) {
	workspace := t.TempDir()
	if _, err := lore.NewStore(workspace).Create(lore.ItemInput{
		ID: "cursor-secret", Type: "world", Name: "Cursor Secret",
		Spans: []lore.Span{{ID: "cur", From: "ch00020", Content: "visible span canon"}},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Workspace: workspace, NovaDir: t.TempDir(), ProjectID: "probe"}
	catalog := tools.NewCatalog(cfg, nil, tools.RuntimeExecutables{})
	definitions, err := catalog.Lore(false)(config.ResolvedAgentToolSettings{config.AgentToolLoreRead: true, config.AgentToolLoreWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	toolset, err := agenttool.StaticToolsIdentified(agentschema.CapabilityIdentity{Kind: "tools.test.native-game-lore-cursor", Version: 1}, definitions...)
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sess, err := store.GetOrCreate("probe-game-cursor")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewAgentRuntime(context.Background(), t.TempDir(), WithToolMutationApplier(
		func(context.Context, agenttoolruntime.CommittedToolMutation) error { return nil },
	))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	// This is the same request-scoped binding performed before native game
	// execution in interactive_agent_app_service.go.
	ctx := lore.ContextWithSpanVisibility(context.Background(), lore.SpanVisibility{Cursor: "ch00020", Scope: lore.LoreScopeAsOf})
	var toolText strings.Builder
	operation, err := runtime.Start(ctx, StartRequest{Cycle: Cycle{
		Definition: agent.Definition{
			Key: "denova.test.native-game-lore-cursor", Name: "probe", Model: &nativeGameLoreCursorModel{},
			ModelIdentity: agentschema.CapabilityIdentity{Kind: "model.test.native-game-lore-cursor", Version: 1},
			Permission:    agentpermission.FullAccess(), Tools: toolset,
		},
		Conversation: agentconversation.NewSessionConversationForAgent(sess, nil, agentrun.AgentKindInteractiveStory),
		Request:      agentchat.ChatRequest{CommandID: "probe-game-cursor", Message: "look up canon", Locale: "en-US", LoreAsOf: "ch00020"},
		Options: agentrun.Options{AgentKind: agentrun.AgentKindInteractiveStory, ProjectID: "probe", SessionID: sess.ID,
			Workspace: workspace, TaskID: "probe-game-cursor-task", RootAgentName: "probe", StoryID: "story-probe", BranchID: "main", Mode: "interactive"},
	}, Emit: func(event agentrun.Event) {
		if event.Type != "tool_result" {
			return
		}
		if data, ok := event.Data.(map[string]any); ok {
			if text, ok := data["content"].(string); ok {
				toolText.WriteString(text)
			}
		}
	}})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	outcome := operation.Wait(ctx)
	if outcome.Status != agentrun.OutcomeCompleted {
		t.Fatalf("native game outcome = %#v", outcome)
	}
	if !strings.Contains(toolText.String(), "visible span canon") || strings.Contains(toolText.String(), "not_visible_at_cursor") {
		t.Fatalf("native game lore tool used the wrong cursor: %s", toolText.String())
	}
}
