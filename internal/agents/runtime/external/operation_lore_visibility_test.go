package external

import (
	"context"
	"testing"

	agentrun "denova/internal/agents/run"
	"denova/internal/book/lore"
)

func TestExternalOperationPreservesCallerLoreVisibilityWhenRequestIsEmpty(t *testing.T) {
	service, request, _ := operationFixture(t)
	request.LoreVisibility = lore.SpanVisibility{}
	base := lore.ContextWithSpanVisibility(t.Context(), lore.SpanVisibility{Cursor: "ch00020", Scope: lore.LoreScopeAsOf})
	request.Adapter = adapterFunc(func(ctx context.Context, _ Input, _ Host) (Result, error) {
		got := lore.SpanVisibilityFromContext(ctx)
		if got.Cursor != "ch00020" || got.Scope != lore.LoreScopeAsOf {
			t.Fatalf("empty request visibility erased caller cursor: %#v", got)
		}
		return Result{Text: "done"}, nil
	})
	operation, err := service.Start(base, request)
	if err != nil {
		t.Fatal(err)
	}
	if outcome := operation.Wait(base); outcome.Status != agentrun.OutcomeCompleted {
		t.Fatalf("outcome=%#v", outcome)
	}
}
