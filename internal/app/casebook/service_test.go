package casebook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	projectfiles "denova/internal/app/projectfiles"
	projectdomain "denova/internal/project"
)

type casebookHost struct{ files *projectfiles.Service }

func (host casebookHost) ProjectFiles() *projectfiles.Service { return host.files }

func newCasebookService(t *testing.T) (*Service, *projectfiles.Service, string, string) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "book")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := projectdomain.NewRegistry(filepath.Join(root, ".denova"))
	record, err := registry.Add(workspace, projectdomain.TypeBook, "Casebook")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.EnsureStore(record); err != nil {
		t.Fatal(err)
	}
	files := projectfiles.NewService(registry)
	t.Cleanup(files.Close)
	return NewService(casebookHost{files: files}), files, record.ID, workspace
}

func writeCasebookFixture(t *testing.T, workspace, quote string) {
	t.Helper()
	files := map[string]string{
		"references/sources/source.txt": "第一行。\n" + quote + "\n",
		"setting/case-index.md":         "# 写法案例\n\n## 已收藏\n\n## 待确认\n\n- c-one | 开篇 | 先写异常\n",
		"setting/cases/c-one.md":        "# c-one 先给异常\n\n- 状态：待确认\n- 方向：开篇\n- 适用场景：章首\n- 方法：先写异常，再让人物处理。\n- 不要搬用：不使用原作人物和原句。\n- 来源：references/sources/source.txt\n- 定位：未知\n- 原文摘录：" + quote + "\n",
	}
	for relative, content := range files {
		path := filepath.Join(workspace, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestToggleCollectsCaseAndMovesIndexLine(t *testing.T) {
	service, files, projectID, workspace := newCasebookService(t)
	quote := "异常灯光在午夜自己亮了。"
	writeCasebookFixture(t, workspace, quote)
	document, err := files.ReadFile(context.Background(), projectID, "setting/cases/c-one.md")
	if err != nil {
		t.Fatal(err)
	}
	index, err := files.ReadFile(context.Background(), projectID, "setting/case-index.md")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, IndexRevision: index.Revision, Collected: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Collected || result.Case.Status != "已收藏" || result.Case.Location != "第 2 行" {
		t.Fatalf("toggle result = %#v", result)
	}
	caseAfter, _ := os.ReadFile(filepath.Join(workspace, "setting", "cases", "c-one.md"))
	indexAfter, _ := os.ReadFile(filepath.Join(workspace, "setting", "case-index.md"))
	if !strings.Contains(string(caseAfter), "状态：已收藏") || !strings.Contains(string(indexAfter), "## 已收藏\n\n- c-one") || strings.Contains(string(indexAfter), "## 待确认\n\n- c-one") {
		t.Fatalf("persisted case/index = %q / %q", caseAfter, indexAfter)
	}
}

func TestToggleRejectsBadQuoteAndRevisionBeforeWriting(t *testing.T) {
	service, files, projectID, workspace := newCasebookService(t)
	writeCasebookFixture(t, workspace, "正确原文")
	document, err := files.ReadFile(context.Background(), projectID, "setting/cases/c-one.md")
	if err != nil {
		t.Fatal(err)
	}
	index, err := files.ReadFile(context.Background(), projectID, "setting/case-index.md")
	if err != nil {
		t.Fatal(err)
	}
	caseBefore, _ := os.ReadFile(filepath.Join(workspace, "setting", "cases", "c-one.md"))
	// Change the source after the case was written: collection must leave both files untouched.
	if err := os.WriteFile(filepath.Join(workspace, "references", "sources", "source.txt"), []byte("来源已变化\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, IndexRevision: index.Revision, Collected: true}); err == nil {
		t.Fatal("bad quote unexpectedly collected")
	} else if typed, ok := err.(*Error); !ok || typed.Code != CaseQuoteMissing {
		t.Fatalf("bad quote error = %T %v", err, err)
	}
	caseAfter, _ := os.ReadFile(filepath.Join(workspace, "setting", "cases", "c-one.md"))
	if string(caseAfter) != string(caseBefore) {
		t.Fatal("quote failure changed the case file")
	}
	if _, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: "sha256:stale", IndexRevision: index.Revision, Collected: true}); err == nil {
		t.Fatal("stale revision unexpectedly succeeded")
	} else if typed, ok := err.(*Error); !ok || typed.Code != CaseRevisionConflict {
		t.Fatalf("revision error = %T %v", err, err)
	}
}
