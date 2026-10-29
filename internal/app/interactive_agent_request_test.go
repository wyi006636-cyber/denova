package app

import (
	"testing"

	agentchat "denova/internal/agents/chat"
)

func TestAcceptedInteractiveRequestKeepsStoryLoreCursor(t *testing.T) {
	caller := agentchat.ChatRequest{Message: "continue", Locale: "zh-CN"}
	resolved := agentchat.ChatRequest{Message: "resolved", LoreAsOf: "ch00020", LoreScope: "as_of"}

	got := acceptedInteractiveRequest(caller, resolved)

	if got.Message != "continue" || got.Locale != "zh-CN" {
		t.Fatalf("caller snapshot was not preserved: %+v", got)
	}
	if got.LoreAsOf != "ch00020" || got.LoreScope != "as_of" {
		t.Fatalf("story lore cursor was dropped: lore_as_of=%q lore_scope=%q", got.LoreAsOf, got.LoreScope)
	}
}
