package chat

import (
	"context"
	"testing"

	"denova/internal/agents/prompts"
	agentrun "denova/internal/agents/run"
	"denova/internal/book/lore"
)

func TestPublicHostMiddlewareBindsRequestLoreCursor(t *testing.T) {
	request := ChatRequest{IDEContext: prompts.IDEContextRef{CurrentFile: "chapters/ch00020-opening.md"}}
	ctx, _, err := NewPublicHostMiddleware(request, agentrun.Options{}, nil).BeforeAgent(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := lore.SpanVisibilityFromContext(ctx); got.Cursor != "ch00020" || got.Scope != lore.LoreScopeAsOf {
		t.Fatalf("request cursor not bound: %+v", got)
	}
}

func TestPublicHostMiddlewareKeepsInheritedLoreCursorForChildAgents(t *testing.T) {
	parent := lore.ContextWithSpanVisibility(context.Background(), lore.SpanVisibility{Cursor: "ch00020", Scope: lore.LoreScopeAsOf})
	ctx, _, err := NewPublicHostMiddleware(ChatRequest{}, agentrun.Options{}, nil).BeforeAgent(parent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := lore.SpanVisibilityFromContext(ctx); got.Cursor != "ch00020" {
		t.Fatalf("child Agent lost the inherited cursor: %+v", got)
	}

	book := lore.ContextWithSpanVisibility(context.Background(), lore.SpanVisibility{Scope: lore.LoreScopeBook})
	ctx, _, err = NewPublicHostMiddleware(ChatRequest{}, agentrun.Options{}, nil).BeforeAgent(book, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := lore.SpanVisibilityFromContext(ctx); got.Scope != lore.LoreScopeBook {
		t.Fatalf("child Agent lost the inherited book scope: %+v", got)
	}
}
