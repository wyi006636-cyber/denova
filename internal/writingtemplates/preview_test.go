package writingtemplates

import (
	"context"
	"errors"
	"strings"
	"testing"

	"denova/internal/agents/prompts"
)

func TestPreviewHasStableThreeSegmentContract(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	preview, err := catalog.Preview(context.Background(), "draft.payoff-climax", "author target")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(preview.Segments))
	}
	wantKinds := []string{"candidate_contract", "template_body", "target_hint"}
	for index, want := range wantKinds {
		if preview.Segments[index].Kind != want || preview.Segments[index].Text == "" {
			t.Fatalf("segment %d = %+v", index, preview.Segments[index])
		}
	}
	if preview.Segments[1].Text != mustBuiltinBody(t, "draft.payoff-climax") {
		t.Fatal("preview body differs from embedded template")
	}
	if preview.Segments[2].Text != "author target" {
		t.Fatalf("target hint = %q", preview.Segments[2].Text)
	}

	defaultPreview, err := catalog.Preview(context.Background(), "draft.payoff-climax", "  ")
	if err != nil || defaultPreview.Segments[2].Text != TargetHintText {
		t.Fatalf("default target hint = %+v, err=%v", defaultPreview, err)
	}
	item, err := catalog.ResolveForCandidate(context.Background(), "draft.payoff-climax", 1)
	if err != nil {
		t.Fatal(err)
	}
	parts := []string{defaultPreview.Segments[0].Text, defaultPreview.Segments[1].Text, defaultPreview.Segments[2].Text}
	if got, want := strings.Join(parts, "\n\n"), prompts.ComposeCandidateDraftInstruction(item.Body, ""); got != want {
		t.Fatalf("preview/system mismatch: got %q want %q", got, want)
	}
}

func TestPreviewMissingTemplateReturnsNotFound(t *testing.T) {
	_, err := NewCatalog(t.TempDir()).Preview(context.Background(), "draft.missing", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want %v", err, ErrNotFound)
	}
}

func mustBuiltinBody(t *testing.T, id string) string {
	t.Helper()
	item, ok := builtinTemplateByID(id)
	if !ok {
		t.Fatalf("missing builtin %s", id)
	}
	return item.Body
}
