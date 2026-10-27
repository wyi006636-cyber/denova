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
	agentconversation "denova/internal/agents/conversation"
	"denova/internal/agents/conversationconfig"
	"denova/internal/agents/prompts"
	agentrun "denova/internal/agents/run"
	agentruntime "denova/internal/agents/runtime"
	"denova/internal/agents/session"
	"denova/internal/book"
	"denova/internal/book/lore"
	projectdomain "denova/internal/project"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestExternalPreparationUsesProductContextWithoutNativeModel(t *testing.T) {
	for _, kind := range []string{config.AgentKindIDE, config.AgentKindGeneral} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "project")
			if err := os.MkdirAll(workspace, 0o755); err != nil {
				t.Fatal(err)
			}
			selection := config.RuntimeSelection{Kind: config.RuntimeCodex, Codex: &config.CodexRuntimeSettings{Model: "external-model"}}
			cfg := config.Config{Workspace: workspace, ProjectID: "project-1", ProjectStoreDir: filepath.Join(root, "store"), DenovaDir: root, ActiveAgentRuntime: &selection}
			store, err := session.NewStore(filepath.Join(cfg.ProjectStoreDir, "sessions"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			sess, err := store.GetOrCreateWithRuntimeConfig("session-1", conversationconfig.Config{AgentKind: kind, ProfileID: "deleted-native-model", ThinkingLevel: "medium", ApprovalMode: config.AgentApprovalMode("write"), Runtime: &selection})
			if err != nil {
				t.Fatal(err)
			}
			runtime := Runtime{ProjectID: cfg.ProjectID, ProjectStore: cfg.ProjectStoreDir, ProjectType: projectdomain.TypeGeneral, AgentKind: kind, Session: sess, Config: cfg, Workspace: workspace, BookService: book.NewService(workspace)}
			if kind == config.AgentKindIDE {
				runtime.ProjectType, runtime.State = projectdomain.TypeBook, book.NewState(workspace)
				if err := runtime.State.InitWorkspace(); err != nil {
					t.Fatal(err)
				}
			}
			engines := agentruntime.NewEngines()
			defer engines.Close()
			built, err := BuildExecution(context.Background(), runtime, agents.AgentHostCapabilities{}, engines, "")
			if err != nil {
				t.Fatal(err)
			}
			if built.external == nil || built.native.Definition.Model != nil {
				t.Fatal("external execution must be assembled independently of Native Agent")
			}
			foundAsk := false
			for _, definition := range built.external.Tools {
				info, err := definition.Tool.Info(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if info.Name == "ask" {
					foundAsk = true
				}
			}
			if !foundAsk {
				t.Fatal("external product assembly omitted Ask")
			}
			if _, err := BuildExecution(t.Context(), runtime, agents.AgentHostCapabilities{}, engines, "automation"); err == nil {
				t.Fatal("automation used an external runtime")
			}
		})
	}
}

func TestExternalWritingPreparationCarriesFocusedChapterToLoreTool(t *testing.T) {
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
		Spans: []lore.Span{{ID: "opening", From: "ch00020", Content: "opening canon"}},
	}); err != nil {
		t.Fatal(err)
	}
	selection := config.RuntimeSelection{Kind: config.RuntimeClaude, Claude: &config.ClaudeRuntimeSettings{Model: "external-model"}}
	cfg := config.Config{Workspace: workspace, ProjectID: "project-lore", ProjectStoreDir: filepath.Join(root, "store"), DenovaDir: root, ActiveAgentRuntime: &selection}
	sessionStore, err := session.NewStore(filepath.Join(cfg.ProjectStoreDir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer sessionStore.Close()
	sess, err := sessionStore.GetOrCreateWithRuntimeConfig("session-lore", conversationconfig.Config{AgentKind: config.AgentKindIDE, ProfileID: "external", Runtime: &selection})
	if err != nil {
		t.Fatal(err)
	}
	runtime := Runtime{ProjectID: cfg.ProjectID, ProjectStore: cfg.ProjectStoreDir, ProjectType: projectdomain.TypeBook, AgentKind: config.AgentKindIDE, Session: sess, State: state, Config: cfg, Workspace: workspace, BookService: book.NewService(workspace)}
	assembly, err := agents.BuildExternalConversationAssembly(t.Context(), &cfg, state, prompts.IDEStoryTeller{}, config.AgentKindIDE, agents.AgentHostCapabilities{Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	conversation := agentconversation.NewSessionConversationForAgent(sess, &cfg, config.AgentKindIDE)
	request := agentchat.ChatRequest{CommandID: "lore-focused", Message: "Continue the opening", IDEContext: prompts.IDEContextRef{CurrentFile: "chapters/ch00020-opening.md"}}
	prepared, err := prepareExternalInput(t.Context(), runtime, request, conversation, assembly, agentrun.Options{AgentKind: config.AgentKindIDE}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.LoreVisibility.Cursor != "ch00020" {
		t.Fatalf("focused chapter was not resolved for external writing: %#v", prepared.LoreVisibility)
	}
	var query agents.ToolDefinition
	for _, definition := range prepared.Definitions {
		info, infoErr := definition.Tool.Info(t.Context())
		if infoErr == nil && info.Name == "query_lore_items" {
			query = definition
			break
		}
	}
	if query.Tool == nil {
		t.Fatal("external writing assembly did not expose query_lore_items")
	}
	result, err := query.Tool.Run(lore.ContextWithSpanVisibility(t.Context(), prepared.LoreVisibility), `{"names":["Cursor Secret"],"detail":"full"}`)
	if err != nil || result.Status != agentschema.ToolResultSuccess || !strings.Contains(result.ModelContent, "opening canon") {
		t.Fatalf("external writing lore query did not see focused span: %#v err=%v", result, err)
	}
}
