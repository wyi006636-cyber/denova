package projection

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	qualityworkspace "denova/internal/quality/workspace"
)

func TestQueryUsesTrigramForNormalEnglishAndChineseTerms(t *testing.T) {
	databasePath := buildQueryTestProjection(t, map[string]string{
		"chapters/en.md": "A quick brown fox crosses the harbor.",
		"chapters/zh.md": "小说创作投影必须可以安全删除重建。",
	})
	reader := openQueryTestReader(t, databasePath)
	defer reader.Close()

	for _, test := range []struct {
		name     string
		term     string
		wantPath string
	}{
		{name: "English", term: "quick", wantPath: "chapters/en.md"},
		{name: "Chinese", term: "小说创", wantPath: "chapters/zh.md"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := reader.Query(context.Background(), QueryRequest{Text: test.term})
			if err != nil {
				t.Fatalf("Query(%q): %v", test.term, err)
			}
			if response.Strategy != QueryStrategyTrigram || len(response.Results) != 1 || response.Results[0].Path != test.wantPath {
				t.Fatalf("Query(%q) = %#v", test.term, response)
			}
			assertCompleteProjectionQueryResult(t, response.Results[0])
		})
	}
}

func TestQueryFallsBackForEveryTermShorterThanThreeUnicodeCharacters(t *testing.T) {
	databasePath := buildQueryTestProjection(t, map[string]string{
		"chapters/en.md": "A quick brown fox.",
		"chapters/zh.md": "小说创作需要真源。",
	})
	reader := openQueryTestReader(t, databasePath)
	defer reader.Close()

	for _, test := range []struct {
		term     string
		wantPath string
	}{
		{term: "A", wantPath: "chapters/en.md"},
		{term: "qu", wantPath: "chapters/en.md"},
		{term: "小", wantPath: "chapters/zh.md"},
		{term: "小说", wantPath: "chapters/zh.md"},
	} {
		t.Run(test.term, func(t *testing.T) {
			response, err := reader.Query(context.Background(), QueryRequest{Text: test.term})
			if err != nil {
				t.Fatalf("Query(%q): %v", test.term, err)
			}
			if response.Strategy != QueryStrategyExactScan || len(response.Results) != 1 || response.Results[0].Path != test.wantPath {
				t.Fatalf("short Query(%q) = %#v", test.term, response)
			}
		})
	}
}

func TestQueryEscapesFTSOperatorsAndQuotesAsLiteralText(t *testing.T) {
	databasePath := buildQueryTestProjection(t, map[string]string{
		"chapters/literal.md": `The literal token AND and the sequence a"b are searchable.`,
	})
	reader := openQueryTestReader(t, databasePath)
	defer reader.Close()

	for _, term := range []string{"AND", `a"b`} {
		response, err := reader.Query(context.Background(), QueryRequest{Text: term})
		if err != nil {
			t.Fatalf("Query(%q): %v", term, err)
		}
		if response.Strategy != QueryStrategyTrigram || len(response.Results) != 1 || response.Results[0].Path != "chapters/literal.md" {
			t.Fatalf("literal Query(%q) = %#v", term, response)
		}
	}
}

func TestQueryReturnsDeterministicBoundedResults(t *testing.T) {
	databasePath := buildQueryTestProjection(t, map[string]string{
		"chapters/c.md": "sharedterm",
		"chapters/a.md": "sharedterm",
		"chapters/b.md": "sharedterm",
	})
	reader := openQueryTestReader(t, databasePath)
	defer reader.Close()

	response, err := reader.Query(context.Background(), QueryRequest{Text: "sharedterm"})
	if err != nil {
		t.Fatal(err)
	}
	paths := projectionQueryPaths(response.Results)
	wantPaths := []string{"chapters/a.md", "chapters/b.md", "chapters/c.md"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("query paths = %#v, want %#v", paths, wantPaths)
	}

	limited, err := reader.Query(context.Background(), QueryRequest{Text: "sharedterm", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Results) != 1 || limited.Results[0].Path != "chapters/a.md" {
		t.Fatalf("limited query = %#v", limited)
	}
	if _, err := reader.Query(context.Background(), QueryRequest{Text: "sharedterm", Limit: MaxQueryResults + 1}); err == nil {
		t.Fatal("query above the hard result bound must fail")
	}
}

func TestQueryRejectsEmptyTextAndCancelledContext(t *testing.T) {
	databasePath := buildQueryTestProjection(t, map[string]string{"chapters/a.md": "searchable"})
	reader := openQueryTestReader(t, databasePath)
	defer reader.Close()

	if _, err := reader.Query(context.Background(), QueryRequest{Text: "  \n\t  "}); err == nil {
		t.Fatal("empty query must fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Query(ctx, QueryRequest{Text: "searchable"}); err == nil {
		t.Fatal("cancelled query must fail")
	}
}

func buildQueryTestProjection(t *testing.T, sources map[string]string) string {
	t.Helper()
	workspacePath := t.TempDir()
	for relative, content := range sources {
		path := filepath.Join(workspacePath, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspacePath, qualityworkspace.ProjectionSourceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(t.TempDir(), "index.db")
	if _, err := buildProjectionDatabase(context.Background(), buildRequest{
		Path:          databasePath,
		Snapshot:      snapshot,
		BuildIdentity: BuildIdentityV1,
	}); err != nil {
		t.Fatal(err)
	}
	return databasePath
}

func openQueryTestReader(t *testing.T, path string) *Reader {
	t.Helper()
	reader, err := openProjectionReader(context.Background(), path)
	if err != nil {
		t.Fatalf("openProjectionReader: %v", err)
	}
	return reader
}

func assertCompleteProjectionQueryResult(t *testing.T, result QueryResult) {
	t.Helper()
	if result.DocumentID == "" || result.Path == "" || result.RevisionHash == "" || result.Profile == "" || result.Kind == "" {
		t.Fatalf("incomplete query result: %#v", result)
	}
}

func projectionQueryPaths(results []QueryResult) []string {
	paths := make([]string, 0, len(results))
	for _, result := range results {
		paths = append(paths, result.Path)
	}
	return paths
}
