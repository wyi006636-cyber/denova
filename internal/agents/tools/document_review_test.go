package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"denova/internal/workspace"
	"denova/internal/workspace/documentreview"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestAddReviewCommentsToolPersistsIndependentResults(t *testing.T) {
	workspaceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "第一段\n重复\n第二段\n重复\n"
	if err := os.WriteFile(filepath.Join(workspaceRoot, "chapters", "one.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stateRoot := t.TempDir()
	definitions, err := newDocumentReviewTools(workspaceRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	var tool agenttool.Tool
	for _, definition := range definitions {
		info, infoErr := definition.Tool.Info(context.Background())
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		if info.Name == "add_review_comments" {
			tool = definition.Tool
		}
	}
	if tool == nil {
		t.Fatal("add_review_comments tool missing")
	}
	ctx := ContextWithWorkspaceChangeScope(context.Background(), WorkspaceChangeScope{SessionID: "session-1", RunID: "run-1"})
	result, err := tool.Run(ctx, `{"items":[{"path":"chapters/one.md","quote":"第一段","body":"good"},{"path":"chapters/one.md","quote":"重复","body":"ambiguous"},{"path":"chapters/one.md","quote":"重复","prefix":"第二段\n","body":"good too"},{"path":"outside.json","quote":"x","body":"bad"}]}`)
	if err != nil || result.Status != agentschema.ToolResultSuccess {
		t.Fatalf("tool result=%#v err=%v", result, err)
	}
	var payload addReviewCommentsResult
	if err := json.Unmarshal(result.Details, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Created) != 2 || len(payload.Failed) != 2 {
		t.Fatalf("batch result = %#v", payload)
	}
	service, err := documentreview.ForWorkspaceAt(workspaceRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := service.CurrentThread(context.Background())
	if err != nil || len(thread.Comments) != 2 {
		t.Fatalf("ledger thread = %#v err=%v", thread, err)
	}
	for _, comment := range thread.Comments {
		if comment.Source != documentreview.CommentSourceAgent || comment.AgentSessionID != "session-1" || comment.AgentRunID != "run-1" {
			t.Fatalf("comment provenance = %#v", comment)
		}
	}
}

func TestAddReviewCommentsToolRejectsLoreAndUnsafePaths(t *testing.T) {
	workspaceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceRoot, "chapter.md"), []byte("quote"), 0o600); err != nil {
		t.Fatal(err)
	}
	definitions, err := newDocumentReviewTools(workspaceRoot, filepath.Join(t.TempDir(), workspace.DataDirName))
	if err != nil {
		t.Fatal(err)
	}
	result, err := definitions[0].Tool.Run(context.Background(), `{"items":[{"path":"../chapter.md","quote":"quote","body":"x"},{"path":"lore/item.md","quote":"quote","body":"x"}]}`)
	if err != nil || result.Status != agentschema.ToolResultSuccess {
		t.Fatalf("unsafe path result=%#v err=%v", result, err)
	}
	var payload addReviewCommentsResult
	if err := json.Unmarshal(result.Details, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Created) != 0 || len(payload.Failed) != 2 {
		t.Fatalf("unsafe path batch = %#v", payload)
	}
}
