package projection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRebuildFromDeletedProjectionPreservesSnapshotAndQueries(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "ideas.md", "warm harbor mystery")
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "A quick brown fox crosses the harbor.")
	writeProjectionTestSource(t, workspace, "chapters/ch2.md", "小说创作投影可以安全删除重建。")
	authoritativeBefore := projectionFormalFileHashes(t, workspace, []string{"ideas.md", "chapters/ch1.md", "chapters/ch2.md"})

	service, err := newProjectionTestService(t, Options{Workspace: workspace})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	first, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("first Rebuild: %v", err)
	}
	if !first.Fresh || !first.Activated || !first.ParentSynced || first.DocumentCount != 4 || first.SourceSnapshotHash == "" || first.SQLiteVersion == "" {
		t.Fatalf("first rebuild result = %#v", first)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if first.DatabasePath != filepath.Join(canonicalWorkspace, filepath.FromSlash(DatabaseRelativePath)) {
		t.Fatalf("database path = %q", first.DatabasePath)
	}
	firstRows := readProjectionDocumentRows(t, first.DatabasePath)
	firstQueries := queryProjectionTerms(t, service, []string{"quick", "小说", "删除重建"})

	if err := os.Remove(first.DatabasePath); err != nil {
		t.Fatal(err)
	}
	second, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("second Rebuild: %v", err)
	}
	if !second.Fresh || second.SourceSnapshotHash != first.SourceSnapshotHash || second.DocumentCount != first.DocumentCount {
		t.Fatalf("second rebuild result = %#v, first = %#v", second, first)
	}
	secondRows := readProjectionDocumentRows(t, second.DatabasePath)
	secondQueries := queryProjectionTerms(t, service, []string{"quick", "小说", "删除重建"})
	if !reflect.DeepEqual(secondRows, firstRows) {
		t.Fatalf("rebuild rows differ:\nfirst=%#v\nsecond=%#v", firstRows, secondRows)
	}
	if !reflect.DeepEqual(secondQueries, firstQueries) {
		t.Fatalf("rebuild queries differ:\nfirst=%#v\nsecond=%#v", firstQueries, secondQueries)
	}
	if got := projectionFormalFileHashes(t, workspace, []string{"ideas.md", "chapters/ch1.md", "chapters/ch2.md"}); !reflect.DeepEqual(got, authoritativeBefore) {
		t.Fatalf("Projection rebuild changed formal files:\nbefore=%#v\nafter=%#v", authoritativeBefore, got)
	}
	assertNoProjectionTestStages(t, workspace)
}

type projectionDocumentRow struct {
	DocumentID   string
	Path         string
	RevisionHash string
	Profile      string
	Kind         string
}

func readProjectionDocumentRows(t *testing.T, databasePath string) []projectionDocumentRow {
	t.Helper()
	db := openProjectionTestDatabase(t, databasePath)
	defer db.Close()
	rows, err := db.Query("SELECT document_id, canonical_path, revision_hash, profile, kind FROM source_documents ORDER BY canonical_path")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []projectionDocumentRow
	for rows.Next() {
		var row projectionDocumentRow
		if err := rows.Scan(&row.DocumentID, &row.Path, &row.RevisionHash, &row.Profile, &row.Kind); err != nil {
			t.Fatal(err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func queryProjectionTerms(t *testing.T, service *Service, terms []string) []QueryResponse {
	t.Helper()
	reader, err := service.Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reader.Close()
	responses := make([]QueryResponse, 0, len(terms))
	for _, term := range terms {
		response, err := reader.Query(context.Background(), QueryRequest{Text: term})
		if err != nil {
			t.Fatalf("Query(%q): %v", term, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func projectionFormalFileHashes(t *testing.T, workspace string, paths []string) map[string]string {
	t.Helper()
	result := make(map[string]string, len(paths))
	for _, relative := range paths {
		content, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		result[relative] = hex.EncodeToString(digest[:])
	}
	return result
}
