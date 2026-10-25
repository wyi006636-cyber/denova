package external

import (
	"context"
	"testing"

	"denova/config"
	"denova/internal/agents/toolruntime"

	agenttool "github.com/alfredxw/denova/agent/tool"
	publictools "github.com/alfredxw/denova/agent/tool/builtin"
)

func TestPrepareToolsIncludesDocumentReviewFromIDECatalogOnly(t *testing.T) {
	cfg := &config.Config{
		Workspace:       t.TempDir(),
		ProjectStoreDir: t.TempDir(),
		AgentTools:      config.DefaultAgentToolSettings(),
	}
	ctx := context.Background()

	ideDefinitions, err := toolruntime.NewCatalogWithContext(ctx, cfg).IDE()(config.ResolveAgentTools(cfg, config.AgentKindIDE))
	if err != nil {
		t.Fatal(err)
	}
	asks, err := publictools.Ask().PrepareTools(ctx, agenttool.ToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_, wire, err := prepareTools(ctx, append(ideDefinitions, asks...))
	if err != nil {
		t.Fatal(err)
	}
	if !toolNamesContain(wire, "add_review_comments") {
		t.Fatalf("IDE catalog wire tools = %v", toolNames(wire))
	}

	for _, test := range []struct {
		name      string
		factory   func(config.ResolvedAgentToolSettings) ([]agenttool.ToolDefinition, error)
		agentKind string
	}{
		{name: "general", factory: toolruntime.NewCatalogWithContext(ctx, cfg).Configuration(), agentKind: config.AgentKindGeneral},
		{name: "interactive_story", factory: toolruntime.NewCatalogWithContext(ctx, cfg).InteractiveStory(toolruntime.ProjectInteractiveContext()), agentKind: config.AgentKindInteractiveStory},
	} {
		t.Run(test.name, func(t *testing.T) {
			definitions, err := test.factory(config.ResolveAgentTools(cfg, test.agentKind))
			if err != nil {
				t.Fatal(err)
			}
			for _, definition := range definitions {
				info, err := definition.Tool.Info(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if info.Name == "add_review_comments" {
					t.Fatalf("%s catalog exposed document review", test.agentKind)
				}
			}
		})
	}
}

func toolNames(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func toolNamesContain(tools []Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
