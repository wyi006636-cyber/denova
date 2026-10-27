package conversationapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/config"
	agents "denova/internal/agents"
	agentchat "denova/internal/agents/chat"
	"denova/internal/agents/conversationconfig"
	agentexecution "denova/internal/agents/execution"
	"denova/internal/agents/prompts"
	agentrun "denova/internal/agents/run"
	agentruntime "denova/internal/agents/runtime"
	"denova/internal/agents/session"
	agenttoolruntime "denova/internal/agents/toolruntime"
	"denova/internal/book"
	"denova/internal/book/lore"
	projectdomain "denova/internal/project"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type nativeLoreCursorModel struct {
	calls    int
	promptOK bool
}

func (model *nativeLoreCursorModel) Generate(_ context.Context, messages []*agentschema.Message, _ ...agentmodel.ModelOption) (*agentschema.Message, error) {
	model.observe(messages)
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

func (model *nativeLoreCursorModel) Stream(ctx context.Context, messages []*agentschema.Message, options ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	message, err := model.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return agentstream.StreamReaderFromArray([]*agentschema.Message{message}), nil
}

func (model *nativeLoreCursorModel) observe(messages []*agentschema.Message) {
	for _, message := range messages {
		if message != nil && strings.Contains(message.Content, "visible span canon") {
			model.promptOK = true
		}
	}
}

// TestProbeProductionWritingToolCursor drives the production writing path
// through Prepare, BuildExecution and the real public Agent runtime. The
// fixture model only replaces the model call; the tool and lifecycle remain
// production implementations.
func TestProbeProductionWritingToolCursor(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	workspace := filepath.Join(root, "project")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	state := book.NewState(workspace)
	if err := state.InitWorkspace(); err != nil {
		t.Fatal(err)
	}
	if _, err := lore.NewStore(workspace).Create(lore.ItemInput{
		ID: "cursor-secret", Type: "world", Name: "Cursor Secret",
		Spans: []lore.Span{{ID: "cur", From: "ch00020", Content: "visible span canon"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "chapters", "ch00020-opening.md"), []byte("# Chapter 20\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := conversationconfig.Default(&config.Config{}, config.AgentKindIDE)
	cfg := config.Config{
		Workspace: workspace, ProjectID: "project-probe", ProjectStoreDir: filepath.Join(root, "store"), DenovaDir: root,
		OpenAIBaseURL: "http://127.0.0.1:1/v1", OpenAIAPIKey: "probe-key", OpenAIModel: "probe-model",
	}
	store, err := session.NewStore(filepath.Join(cfg.ProjectStoreDir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sess, err := store.GetOrCreateWithRuntimeConfig("session-probe", selection)
	if err != nil {
		t.Fatal(err)
	}
	runtime := Runtime{
		ProjectID: cfg.ProjectID, ProjectStore: cfg.ProjectStoreDir, ProjectType: projectdomain.TypeBook,
		AgentKind: config.AgentKindIDE, Session: sess, Config: cfg, Workspace: workspace,
		BookService: book.NewService(workspace), State: state,
	}
	realRuntime, err := agentexecution.NewAgentRuntime(ctx, t.TempDir(), agentexecution.WithToolMutationApplier(func(context.Context, agenttoolruntime.CommittedToolMutation) error {
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = realRuntime.Close(context.Background()) })
	runtime.ExecutionRuntime = realRuntime
	engines := agentruntime.NewEngines()
	t.Cleanup(func() { engines.Close() })
	request := agentchat.ChatRequest{
		CommandID: "probe-writing-tool-cursor", Message: "look up canon", Locale: "en-US",
		IDEContext: prompts.IDEContextRef{CurrentFile: "chapters/ch00020-opening.md"},
	}
	runtime, request, err = Prepare(ctx, runtime, request)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	built, err := BuildExecution(ctx, runtime, agents.AgentHostCapabilities{}, engines, "")
	if err != nil {
		t.Fatal(err)
	}
	model := &nativeLoreCursorModel{}
	built.native.Definition.Model = model
	built.native.Definition.ModelIdentity = agentschema.CapabilityIdentity{Kind: "model.test.native-lore-cursor", Version: 1}
	var toolText strings.Builder
	options := agentrun.Options{
		AgentKind: agentrun.AgentKindIDE, ProjectID: cfg.ProjectID, StateRoot: cfg.ProjectStoreDir,
		TaskID: "probe-task", SessionID: sess.ID, Workspace: workspace, Mode: "ide",
	}
	operation, err := built.Start(ctx, request, ProjectConversation(runtime, request), options, func(event agentrun.Event) {
		if event.Type != "tool_result" {
			return
		}
		if data, ok := event.Data.(map[string]any); ok {
			if text, ok := data["content"].(string); ok {
				toolText.WriteString(text)
			}
		}
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	outcome := operation.Wait(ctx)
	if outcome.Status != agentrun.OutcomeCompleted {
		t.Fatalf("native writing outcome = %#v", outcome)
	}
	if !model.promptOK {
		t.Fatal("native writing prompt did not include the focused chapter span")
	}
	if !strings.Contains(toolText.String(), "visible span canon") || strings.Contains(toolText.String(), "not_visible_at_cursor") {
		t.Fatalf("native writing lore tool used the wrong cursor: %s", toolText.String())
	}
}
