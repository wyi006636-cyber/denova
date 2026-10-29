package grok

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	agentrun "denova/internal/agents/run"
	"denova/internal/agents/runtime/external"
	"denova/internal/book/lore"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type visibilityHost struct {
	got lore.SpanVisibility
}

func (host *visibilityHost) Emit(agentrun.Event) error { return nil }

func (host *visibilityHost) CallTool(ctx context.Context, _ external.ToolCall) (external.ToolResult, error) {
	host.got = lore.SpanVisibilityFromContext(ctx)
	return external.ToolResult{Text: "Committed product outcome", Success: true}, nil
}

func TestBridgePropagatesLoreVisibilityToHostTool(t *testing.T) {
	base := lore.ContextWithSpanVisibility(t.Context(), lore.SpanVisibility{Cursor: "ch00030", Scope: lore.LoreScopeAsOf})
	host := &visibilityHost{}
	bridge, err := startBridge(base, []external.Tool{{Name: "record", Description: "Record", Schema: json.RawMessage(`{"type":"object"}`)}}, host)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	session, err := client.Connect(base, &mcp.StreamableClientTransport{Endpoint: bridge.url, HTTPClient: &http.Client{Transport: bearerTransport{"Bearer " + bridge.token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.CallTool(base, &mcp.CallToolParams{Name: "record", Arguments: map[string]any{"value": "ok"}}); err != nil {
		t.Fatal(err)
	}
	if host.got.Cursor != "ch00030" || host.got.Scope != lore.LoreScopeAsOf {
		t.Fatalf("Grok bridge lost lore visibility: %#v", host.got)
	}
}
