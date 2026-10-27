package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"denova/internal/agents/runtime/external"
	"denova/internal/book/lore"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type authorizedTransport struct{ token string }

func (t authorizedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestBridgeScopesToolsAndCancelsPendingQuestion(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan external.ToolCall, 1)
	host := &testHost{call: func(ctx context.Context, call external.ToolCall) (external.ToolResult, error) {
		started <- call
		<-ctx.Done()
		return external.ToolResult{}, ctx.Err()
	}}
	bridge, err := startBridge(ctx, []external.Tool{{Name: "ask", Description: "Ask", Schema: json.RawMessage(`{"type":"object"}`)}}, host, cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()
	response, err := http.Get(bridge.url)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("unscoped client accessed bridge")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: bridge.url, HTTPClient: &http.Client{Transport: authorizedTransport{bridge.token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 1 || list.Tools[0].Name != "ask" {
		t.Fatalf("tools: %#v %v", list, err)
	}
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "write", Arguments: map[string]any{}}); err == nil {
		t.Fatal("unregistered tool was accepted")
	}
	done := make(chan error, 1)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer func() {
			if v := recover(); v != nil {
				done <- fmt.Errorf("test call panic: %v", v)
			}
		}()
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Meta: mcp.Meta{"claudecode/toolUseId": "ask-1"}, Name: "ask", Arguments: map[string]any{"question": "Wait"}})
		done <- err
	}()
	select {
	case call := <-started:
		if call.ID != "ask-1" || call.Name != "ask" {
			t.Fatal("missing scoped call identity")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("question did not arrive")
	}
	select {
	case err := <-done:
		t.Fatalf("question returned before answer or cancellation: %v", err)
	default:
	}
	cancel()
	workers.Wait()
	if err := <-done; err == nil {
		t.Fatal("cancelled question succeeded")
	}
	if err := bridge.close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBridgeExposesRegisteredReviewCommentTool(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	bridge, err := startBridge(ctx, []external.Tool{
		{Name: "ask", Description: "Ask", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "add_review_comments", Description: "Add anchored review comments", Schema: json.RawMessage(`{"type":"object","properties":{"items":{"type":"array"}}}`)},
	}, &testHost{}, cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   bridge.url,
		HTTPClient: &http.Client{Transport: authorizedTransport{bridge.token}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, tool := range list.Tools {
		if tool.Name == "add_review_comments" {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatalf("Claude bridge did not expose add_review_comments: %#v", list.Tools)
	}
}

func TestBridgePropagatesLoreVisibilityToHostTool(t *testing.T) {
	base := lore.ContextWithSpanVisibility(t.Context(), lore.SpanVisibility{Cursor: "ch00030", Scope: lore.LoreScopeAsOf})
	var got lore.SpanVisibility
	host := &testHost{call: func(ctx context.Context, _ external.ToolCall) (external.ToolResult, error) {
		got = lore.SpanVisibilityFromContext(ctx)
		return external.ToolResult{Text: "ok", Success: true}, nil
	}}
	bridge, err := startBridge(base, []external.Tool{{Name: "ask", Description: "Ask", Schema: json.RawMessage(`{"type":"object"}`)}}, host, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	session, err := client.Connect(base, &mcp.StreamableClientTransport{Endpoint: bridge.url, HTTPClient: &http.Client{Transport: authorizedTransport{bridge.token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.CallTool(base, &mcp.CallToolParams{Meta: mcp.Meta{"claudecode/toolUseId": "visibility-1"}, Name: "ask", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if got.Cursor != "ch00030" || got.Scope != lore.LoreScopeAsOf {
		t.Fatalf("Claude bridge lost lore visibility: %#v", got)
	}
}

func TestBridgeQueryLoreUsesFocusedChapterCursor(t *testing.T) {
	workspace := t.TempDir()
	if _, err := lore.NewStore(workspace).Create(lore.ItemInput{
		ID: "cursor-secret", Type: "world", Name: "Cursor Secret",
		Spans: []lore.Span{{ID: "opening", From: "ch00020", Content: "opening canon"}},
	}); err != nil {
		t.Fatal(err)
	}
	base := lore.ContextWithSpanVisibility(t.Context(), lore.ResolveSpanVisibility("chapters/ch00020-opening.md", "", lore.LoreScopeAsOf))
	host := &testHost{call: func(ctx context.Context, call external.ToolCall) (external.ToolResult, error) {
		if call.Name != "query_lore_items" {
			return external.ToolResult{Text: "unexpected tool", Success: false}, nil
		}
		visibility := lore.SpanVisibilityFromContext(ctx)
		items, err := lore.NewStore(workspace).List()
		if err != nil {
			return external.ToolResult{}, err
		}
		for _, item := range lore.VisibleItems(items, visibility.Cursor, visibility.Scope) {
			if item.ID == "cursor-secret" && strings.Contains(item.Spans[0].Content, "opening canon") {
				return external.ToolResult{Text: "opening canon", Success: true}, nil
			}
		}
		return external.ToolResult{Text: "cursor span missing", Success: false}, nil
	}}
	bridge, err := startBridge(base, []external.Tool{{Name: "query_lore_items", Description: "Query Lore", Schema: json.RawMessage(`{"type":"object"}`)}}, host, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	session, err := client.Connect(base, &mcp.StreamableClientTransport{Endpoint: bridge.url, HTTPClient: &http.Client{Transport: authorizedTransport{bridge.token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(base, &mcp.CallToolParams{Meta: mcp.Meta{"claudecode/toolUseId": "query-1"}, Name: "query_lore_items", Arguments: map[string]any{"names": []string{"Cursor Secret"}, "detail": "full"}})
	if err != nil || result.IsError || len(result.Content) == 0 {
		t.Fatalf("query_lore_items bridge call failed: %#v %v", result, err)
	}
	if text, ok := result.Content[0].(*mcp.TextContent); !ok || !strings.Contains(text.Text, "opening canon") {
		t.Fatalf("bridge query lost focused span: %#v", result.Content)
	}
}
