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

func TestToggleRequiresIndexRevisionAndAllowsDotsInsideSourceFilename(t *testing.T) {
	service, files, projectID, workspace := newCasebookService(t)
	quote := "合法摘录"
	writeCasebookFixture(t, workspace, quote)
	sourcePath := filepath.Join(workspace, "references", "sources", "a..b.txt")
	if err := os.Rename(filepath.Join(workspace, "references", "sources", "source.txt"), sourcePath); err != nil {
		t.Fatal(err)
	}
	casePath := filepath.Join(workspace, "setting", "cases", "c-one.md")
	caseContent, err := os.ReadFile(casePath)
	if err != nil {
		t.Fatal(err)
	}
	caseContent = []byte(strings.ReplaceAll(string(caseContent), "references/sources/source.txt", "references/sources/a..b.txt"))
	if err := os.WriteFile(casePath, caseContent, 0o644); err != nil {
		t.Fatal(err)
	}
	document, err := files.ReadFile(context.Background(), projectID, "setting/cases/c-one.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, Collected: true}); err == nil {
		t.Fatal("missing index revision unexpectedly succeeded")
	} else if typed, ok := err.(*Error); !ok || typed.Code != CaseRevisionConflict {
		t.Fatalf("missing index revision error = %T %v", err, err)
	}
	index, err := files.ReadFile(context.Background(), projectID, "setting/case-index.md")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, IndexRevision: index.Revision, Collected: true})
	if err != nil || !result.Collected {
		t.Fatalf("filename with two dots should be allowed: %#v, %v", result, err)
	}
}

func TestToggleSourcePathRejectsParentSegment(t *testing.T) {
	service, files, projectID, workspace := newCasebookService(t)
	writeCasebookFixture(t, workspace, "正确原文")
	casePath := filepath.Join(workspace, "setting", "cases", "c-one.md")
	content, err := os.ReadFile(casePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(casePath, []byte(strings.ReplaceAll(string(content), "references/sources/source.txt", "references/sources/../../secret.txt")), 0o644); err != nil {
		t.Fatal(err)
	}
	document, _ := files.ReadFile(context.Background(), projectID, "setting/cases/c-one.md")
	index, _ := files.ReadFile(context.Background(), projectID, "setting/case-index.md")
	if _, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, IndexRevision: index.Revision, Collected: true}); err == nil {
		t.Fatal("parent source path unexpectedly succeeded")
	} else if typed, ok := err.(*Error); !ok || typed.Code != CaseSourceOutsideBook {
		t.Fatalf("source path error = %T %v", err, err)
	}
}

func TestToggleRejectsMissingSourceAndStaleIndexRevision(t *testing.T) {
	service, files, projectID, workspace := newCasebookService(t)
	writeCasebookFixture(t, workspace, "正确原文")
	casePath := filepath.Join(workspace, "setting", "cases", "c-one.md")
	content := string(mustReadFile(t, casePath))
	if err := os.WriteFile(casePath, []byte(strings.ReplaceAll(content, "references/sources/source.txt", "references/sources/missing.txt")), 0o644); err != nil {
		t.Fatal(err)
	}
	document, _ := files.ReadFile(context.Background(), projectID, "setting/cases/c-one.md")
	index, _ := files.ReadFile(context.Background(), projectID, "setting/case-index.md")
	if _, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, IndexRevision: index.Revision, Collected: true}); err == nil {
		t.Fatal("missing source unexpectedly succeeded")
	} else if typed, ok := err.(*Error); !ok || typed.Code != CaseSourceMissing {
		t.Fatalf("missing source error = %T %v", err, err)
	}
	// Restore a valid source/case pair, then verify the index revision is also
	// required to match the author's snapshot.
	if err := os.WriteFile(casePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	document, _ = files.ReadFile(context.Background(), projectID, "setting/cases/c-one.md")
	if _, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, IndexRevision: "sha256:stale", Collected: true}); err == nil {
		t.Fatal("stale index revision unexpectedly succeeded")
	} else if typed, ok := err.(*Error); !ok || typed.Code != CaseRevisionConflict {
		t.Fatalf("stale index error = %T %v", err, err)
	}
}

func TestListShowsOrphanAndBrokenRowsAndCollectsOrphan(t *testing.T) {
	service, files, projectID, workspace := newCasebookService(t)
	writeCasebookFixture(t, workspace, "正确原文")
	orphanPath := filepath.Join(workspace, "setting", "cases", "c-orphan.md")
	orphan := strings.Replace(string(mustReadFile(t, filepath.Join(workspace, "setting", "cases", "c-one.md"))), "c-one", "c-orphan", 1)
	if err := os.WriteFile(orphanPath, []byte(orphan), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "setting", "cases", "c-broken.md"), []byte("not a case"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(workspace, "setting", "case-index.md")
	indexContent := string(mustReadFile(t, indexPath)) + "- c-missing | 节奏 | 文件已删除\n- c-broken | 结构 | 无法读取\n"
	if err := os.WriteFile(indexPath, []byte(indexContent), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.List(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]CaseDocument, len(snapshot.Cases))
	for _, item := range snapshot.Cases {
		byID[item.ID] = item
	}
	if item := byID["c-orphan"]; item.Registered || item.Issue != "" || item.Revision == "" {
		t.Fatalf("orphan = %#v", item)
	}
	if item := byID["c-missing"]; item.Revision != "" || item.Issue != CaseIssueMissing {
		t.Fatalf("missing = %#v", item)
	}
	if item := byID["c-broken"]; item.Revision != "" || item.Issue != CaseIssueInvalid {
		t.Fatalf("broken = %#v", item)
	}
	orphanDoc, err := files.ReadFile(context.Background(), projectID, "setting/cases/c-orphan.md")
	if err != nil {
		t.Fatal(err)
	}
	indexDoc, err := files.ReadFile(context.Background(), projectID, "setting/case-index.md")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-orphan.md", CaseRevision: orphanDoc.Revision, IndexRevision: indexDoc.Revision, Collected: true})
	if err != nil || !result.Collected {
		t.Fatalf("collect orphan = %#v, %v", result, err)
	}
	updatedIndex, _ := os.ReadFile(indexPath)
	if !strings.Contains(string(updatedIndex), "- c-orphan | 开篇 | 先写异常") {
		t.Fatalf("orphan index row missing: %q", updatedIndex)
	}
}

func TestToggleCanUnfavoriteAndUsesStableEmptyQuoteCode(t *testing.T) {
	service, files, projectID, workspace := newCasebookService(t)
	writeCasebookFixture(t, workspace, "正确原文")
	document, _ := files.ReadFile(context.Background(), projectID, "setting/cases/c-one.md")
	index, _ := files.ReadFile(context.Background(), projectID, "setting/case-index.md")
	if _, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, IndexRevision: index.Revision, Collected: true}); err != nil {
		t.Fatal(err)
	}
	document, _ = files.ReadFile(context.Background(), projectID, "setting/cases/c-one.md")
	index, _ = files.ReadFile(context.Background(), projectID, "setting/case-index.md")
	result, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, IndexRevision: index.Revision, Collected: false})
	if err != nil || result.Collected {
		t.Fatalf("unfavorite = %#v, %v", result, err)
	}
	casePath := filepath.Join(workspace, "setting", "cases", "c-one.md")
	content, _ := os.ReadFile(casePath)
	content = []byte(strings.Replace(string(content), "原文摘录：正确原文", "原文摘录：   ", 1))
	if err := os.WriteFile(casePath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	document, _ = files.ReadFile(context.Background(), projectID, "setting/cases/c-one.md")
	index, _ = files.ReadFile(context.Background(), projectID, "setting/case-index.md")
	if _, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, IndexRevision: index.Revision, Collected: true}); err == nil {
		t.Fatal("empty quote unexpectedly succeeded")
	} else if typed, ok := err.(*Error); !ok || typed.Code != CaseQuoteEmpty {
		t.Fatalf("empty quote error = %T %v", err, err)
	}
}

func TestToggleRollsBackCaseWhenIndexWriteFails(t *testing.T) {
	service, files, projectID, workspace := newCasebookService(t)
	writeCasebookFixture(t, workspace, "正确原文")
	document, _ := files.ReadFile(context.Background(), projectID, "setting/cases/c-one.md")
	index, _ := files.ReadFile(context.Background(), projectID, "setting/case-index.md")
	casePath := filepath.Join(workspace, "setting", "cases", "c-one.md")
	indexPath := filepath.Join(workspace, "setting", "case-index.md")
	caseBefore := string(mustReadFile(t, casePath))
	indexBefore := string(mustReadFile(t, indexPath))
	if err := os.Chmod(indexPath, 0o444); err != nil {
		t.Skipf("read-only file mode is unavailable: %v", err)
	}
	defer os.Chmod(indexPath, 0o644)
	_, err := service.Toggle(context.Background(), projectID, ToggleRequest{Path: "setting/cases/c-one.md", CaseRevision: document.Revision, IndexRevision: index.Revision, Collected: true})
	if err == nil {
		t.Skip("the current platform permits writing a read-only index")
	}
	if got := string(mustReadFile(t, casePath)); got != caseBefore {
		t.Fatalf("case was not rolled back: %q", got)
	}
	if got := string(mustReadFile(t, indexPath)); got != indexBefore {
		t.Fatalf("index changed after failed write: %q", got)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
