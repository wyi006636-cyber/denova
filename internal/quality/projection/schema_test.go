package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	qualityworkspace "denova/internal/quality/workspace"
)

func TestSchemaV1StoresExactMetadataAndSeparatesPublicIDFromRowID(t *testing.T) {
	snapshot := projectionTestSnapshot(t)
	databasePath := filepath.Join(t.TempDir(), "index.db")
	evidence, err := buildProjectionDatabase(context.Background(), buildRequest{
		Path:            databasePath,
		Snapshot:        snapshot,
		BuildIdentity:   BuildIdentityV1,
		FreshActivation: true,
	})
	if err != nil {
		t.Fatalf("buildProjectionDatabase: %v", err)
	}
	if evidence.SQLiteVersion == "" || !containsStringValue(evidence.CompileOptions, "ENABLE_FTS5") {
		t.Fatalf("runtime capability evidence = %#v", evidence)
	}

	db := openProjectionTestDatabase(t, databasePath)
	defer db.Close()

	var userVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatal(err)
	}
	if userVersion != SchemaVersionV1 {
		t.Fatalf("user_version = %d, want %d", userVersion, SchemaVersionV1)
	}

	var metadata struct {
		SchemaVersion       int
		BuildIdentity       string
		DriverModule        string
		DriverVersion       string
		LibcModule          string
		LibcVersion         string
		SQLiteVersion       string
		SourceSnapshotHash  string
		SourceDocumentCount int
	}
	err = db.QueryRow(`
		SELECT schema_version, build_identity, driver_module, driver_version,
		       libc_module, libc_version, sqlite_version, source_snapshot_hash,
		       source_document_count
		FROM projection_metadata WHERE singleton = 1`).Scan(
		&metadata.SchemaVersion,
		&metadata.BuildIdentity,
		&metadata.DriverModule,
		&metadata.DriverVersion,
		&metadata.LibcModule,
		&metadata.LibcVersion,
		&metadata.SQLiteVersion,
		&metadata.SourceSnapshotHash,
		&metadata.SourceDocumentCount,
	)
	if err != nil {
		t.Fatalf("read projection metadata: %v", err)
	}
	wantMetadata := struct {
		SchemaVersion       int
		BuildIdentity       string
		DriverModule        string
		DriverVersion       string
		LibcModule          string
		LibcVersion         string
		SQLiteVersion       string
		SourceSnapshotHash  string
		SourceDocumentCount int
	}{
		SchemaVersion:       SchemaVersionV1,
		BuildIdentity:       BuildIdentityV1,
		DriverModule:        DriverModule,
		DriverVersion:       DriverVersion,
		LibcModule:          LibcModule,
		LibcVersion:         LibcVersion,
		SQLiteVersion:       evidence.SQLiteVersion,
		SourceSnapshotHash:  snapshot.Hash,
		SourceDocumentCount: len(snapshot.Documents),
	}
	if metadata != wantMetadata {
		t.Fatalf("projection metadata:\n got: %#v\nwant: %#v", metadata, wantMetadata)
	}

	columns := projectionTableColumns(t, db, "source_documents")
	wantColumns := []projectionColumn{
		{Name: "rowid", Type: "INTEGER", PrimaryKey: 1},
		{Name: "document_id", Type: "TEXT"},
		{Name: "canonical_path", Type: "TEXT"},
		{Name: "revision_hash", Type: "TEXT"},
		{Name: "profile", Type: "TEXT"},
		{Name: "kind", Type: "TEXT"},
		{Name: "content", Type: "TEXT"},
	}
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("source_documents columns:\n got: %#v\nwant: %#v", columns, wantColumns)
	}

	var rowID int64
	var documentID string
	if err := db.QueryRow("SELECT rowid, document_id FROM source_documents ORDER BY canonical_path LIMIT 1").Scan(&rowID, &documentID); err != nil {
		t.Fatal(err)
	}
	if rowID <= 0 || documentID == fmt.Sprintf("%d", rowID) || !strings.HasPrefix(documentID, "doc-") {
		t.Fatalf("public ID and FTS rowid are not separate: rowid=%d document_id=%q", rowID, documentID)
	}

	var ftsDDL string
	if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='source_documents_fts'").Scan(&ftsDDL); err != nil {
		t.Fatal(err)
	}
	compactDDL := strings.ReplaceAll(strings.ToLower(ftsDDL), " ", "")
	for _, fragment := range []string{"content='source_documents'", "content_rowid='rowid'", "tokenize='trigram'"} {
		if !strings.Contains(compactDDL, fragment) {
			t.Fatalf("FTS DDL %q does not contain %q", ftsDDL, fragment)
		}
	}

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='trigger' AND tbl_name='source_documents' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		triggers = append(triggers, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	wantTriggers := []string{"source_documents_ad", "source_documents_ai", "source_documents_au"}
	if !reflect.DeepEqual(triggers, wantTriggers) {
		t.Fatalf("triggers = %#v, want %#v", triggers, wantTriggers)
	}
}

func TestSingleWriterTransactionRollsBackSchemaAndDataTogether(t *testing.T) {
	snapshot := projectionTestSnapshot(t)
	databasePath := filepath.Join(t.TempDir(), "index.db")
	injected := errors.New("injected after data write")
	_, err := buildProjectionDatabase(context.Background(), buildRequest{
		Path:          databasePath,
		Snapshot:      snapshot,
		BuildIdentity: BuildIdentityV1,
		Hooks: Hooks{OnFault: func(point FaultPoint) error {
			if point == FaultAfterDataWrite {
				return injected
			}
			return nil
		}},
	})
	if !errors.Is(err, injected) {
		t.Fatalf("build error = %v, want injected failure", err)
	}

	db := openProjectionTestDatabase(t, databasePath)
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN ('projection_metadata', 'source_documents', 'source_documents_fts')").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed writer left %d schema objects visible", count)
	}
}

func projectionTestSnapshot(t *testing.T) qualityworkspace.ProjectionSourceSnapshot {
	t.Helper()
	workspace := t.TempDir()
	writeProjectionTestFile(t, workspace, "chapters/ch1.md", "A quick brown fox crosses the silent harbor.")
	writeProjectionTestFile(t, workspace, "chapters/ch2.md", "小说创作投影必须可以安全删除重建。")
	snapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(context.Background(), workspace, qualityworkspace.ProjectionSourceOptions{})
	if err != nil {
		t.Fatalf("BuildProjectionSourceSnapshot: %v", err)
	}
	return snapshot
}

func writeProjectionTestSource(t *testing.T, workspace, relative, content string) {
	t.Helper()
	markerPath := filepath.Join(workspace, filepath.FromSlash(qualityworkspace.MarkerRelativePath))
	if _, err := os.Lstat(markerPath); errors.Is(err, os.ErrNotExist) {
		writeProjectionWorkspaceMarker(t, workspace, projectionTestWorkspaceMarker)
	} else if err != nil {
		t.Fatal(err)
	}
	writeProjectionTestFile(t, workspace, relative, content)
}

func writeProjectionTestFile(t *testing.T, workspace, relative, content string) {
	t.Helper()
	path := filepath.Join(workspace, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func projectionTestStagePaths(t *testing.T, workspace string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(workspace, ".denova", projectionStagePrefix+"*"+projectionStageSuffix))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func currentProjectionTestStagePath(t *testing.T, workspace string) string {
	t.Helper()
	paths := projectionTestStagePaths(t, workspace)
	if len(paths) != 1 {
		t.Fatalf("current Projection stages = %#v", paths)
	}
	return paths[0]
}

func assertNoProjectionTestStages(t *testing.T, workspace string) {
	t.Helper()
	if paths := projectionTestStagePaths(t, workspace); len(paths) != 0 {
		t.Fatalf("owned Projection stages remain: %#v", paths)
	}
}

func openProjectionTestDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open(sqliteDriverName, path)
	if err != nil {
		t.Fatalf("open projection database: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping projection database: %v", err)
	}
	return db
}

type projectionColumn struct {
	Name       string
	Type       string
	PrimaryKey int
}

func projectionTableColumns(t *testing.T, db *sql.DB, table string) []projectionColumn {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var columns []projectionColumn
	for rows.Next() {
		var column projectionColumn
		var cid, notNull int
		var defaultValue any
		if err := rows.Scan(&cid, &column.Name, &column.Type, &notNull, &defaultValue, &column.PrimaryKey); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return columns
}

func containsStringValue(values []string, want string) bool {
	return sort.SearchStrings(values, want) < len(values) && values[sort.SearchStrings(values, want)] == want
}
