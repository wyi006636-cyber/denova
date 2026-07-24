package projection

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	qualityworkspace "denova/internal/quality/workspace"
)

func TestProjectionStatusMissingKeepsWorkspaceInspectorAndSourcesReadable(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "workspace remains readable")
	service, err := newProjectionTestService(t, Options{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.Inspect(context.Background())
	if err != nil {
		t.Fatalf("Inspect missing Projection: %v", err)
	}
	if status.State != StateUnavailable || status.Reason != ReasonMissing || status.DatabasePath == "" {
		t.Fatalf("missing status = %#v", status)
	}
	if _, err := service.Open(context.Background()); err == nil {
		t.Fatal("Open must reject a missing Projection")
	} else {
		var unavailable *UnavailableError
		if !errors.As(err, &unavailable) || unavailable.Status.Reason != ReasonMissing {
			t.Fatalf("Open error = %#v, %v", unavailable, err)
		}
	}
	assertProjectionFailureLeavesWorkspaceReadable(t, workspace, "workspace remains readable")
}

func TestProjectionStatusLockedRetainsLiveDatabaseAndWorkspaceAccess(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "locked projection source")
	service, databasePath := rebuildProjectionForRecoveryTest(t, workspace)

	locker, err := sql.Open(sqliteDriverName, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	locker.SetMaxOpenConns(1)
	connection, err := locker.Conn(context.Background())
	if err != nil {
		locker.Close()
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(context.Background(), "PRAGMA locking_mode=EXCLUSIVE"); err != nil {
		connection.Close()
		locker.Close()
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		connection.Close()
		locker.Close()
		t.Fatal(err)
	}

	status, inspectErr := service.Inspect(context.Background())
	if inspectErr != nil {
		t.Fatalf("Inspect locked Projection: %v", inspectErr)
	}
	if status.State != StateUnavailable || status.Reason != ReasonLocked {
		t.Fatalf("locked status = %#v", status)
	}
	result, rebuildErr := service.Rebuild(context.Background())
	if rebuildErr == nil || result.Activated {
		t.Fatalf("locked Rebuild result=%#v err=%v", result, rebuildErr)
	}
	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("locked Projection was removed: %v", err)
	}
	assertProjectionFailureLeavesWorkspaceReadable(t, workspace, "locked projection source")

	_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
	connection.Close()
	locker.Close()
	status, err = service.Inspect(context.Background())
	if err != nil || status.State != StateAvailable {
		t.Fatalf("status after releasing lock = %#v err=%v", status, err)
	}
}

func TestProjectionRecoveryQuarantinesCorruptBytesAndRebuildsFromTruth(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "authoritative corruption recovery")
	service, databasePath := rebuildProjectionForRecoveryTest(t, workspace)
	corruptBytes := []byte("not a sqlite database\x00corrupt")
	if err := os.WriteFile(databasePath, corruptBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := service.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateUnavailable || status.Reason != ReasonCorrupt {
		t.Fatalf("corrupt status = %#v", status)
	}
	assertProjectionFailureLeavesWorkspaceReadable(t, workspace, "authoritative corruption recovery")

	result, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("recover corrupt Projection: %v", err)
	}
	if !result.Activated || len(result.QuarantinePaths) == 0 {
		t.Fatalf("corruption recovery result = %#v", result)
	}
	foundCorrupt := false
	for _, path := range result.QuarantinePaths {
		data, readErr := os.ReadFile(path)
		if readErr == nil && reflect.DeepEqual(data, corruptBytes) {
			foundCorrupt = true
		}
	}
	if !foundCorrupt {
		t.Fatalf("quarantine paths do not preserve corrupt diagnostic bytes: %#v", result.QuarantinePaths)
	}
	assertProjectionQueryPath(t, service, "corruption", "chapters/ch1.md")
}

func TestProjectionRecoveryQuarantinesUnknownNewerSchema(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "newer schema source")
	service, databasePath := rebuildProjectionForRecoveryTest(t, workspace)
	db := openProjectionTestDatabase(t, databasePath)
	if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	status, err := service.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateUnavailable || status.Reason != ReasonSchemaNewer || status.SchemaVersion != 2 {
		t.Fatalf("newer-schema status = %#v", status)
	}
	result, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.QuarantinePaths) == 0 {
		t.Fatalf("newer schema was not retained for diagnostics: %#v", result)
	}
	db = openProjectionTestDatabase(t, databasePath)
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != SchemaVersionV1 {
		t.Fatalf("rebuilt schema version=%d err=%v", version, err)
	}
}

func TestProjectionStatusDetectsExternalContentInconsistencyAndRecovers(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "consistent source content")
	service, databasePath := rebuildProjectionForRecoveryTest(t, workspace)
	db := openProjectionTestDatabase(t, databasePath)
	if _, err := db.Exec("DROP TRIGGER source_documents_au"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE source_documents SET content='database-only tamper' WHERE canonical_path='chapters/ch1.md'"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	status, err := service.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateUnavailable || status.Reason != ReasonIntegrityFailed {
		t.Fatalf("external-content status = %#v", status)
	}
	assertProjectionFailureLeavesWorkspaceReadable(t, workspace, "consistent source content")
	result, err := service.Rebuild(context.Background())
	if err != nil || len(result.QuarantinePaths) == 0 {
		t.Fatalf("inconsistency recovery result=%#v err=%v", result, err)
	}
	assertProjectionQueryPath(t, service, "consistent", "chapters/ch1.md")
}

func TestProjectionStatusDetectsIdentityMismatch(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "identity source")
	service, databasePath := rebuildProjectionForRecoveryTest(t, workspace)
	db := openProjectionTestDatabase(t, databasePath)
	if _, err := db.Exec("UPDATE projection_metadata SET build_identity='external-build'"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	status, err := service.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateUnavailable || status.Reason != ReasonIdentityMismatch {
		t.Fatalf("identity status = %#v", status)
	}
}

func TestProjectionStatusMarksExternalSourceEditStaleAndRebuildsWithoutReverseWrite(t *testing.T) {
	workspace := t.TempDir()
	chapterPath := filepath.Join(workspace, "chapters", "ch1.md")
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "old source revision")
	service, _ := rebuildProjectionForRecoveryTest(t, workspace)
	newBytes := []byte("new author source revision")
	if err := os.WriteFile(chapterPath, newBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := service.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateStale || status.Reason != ReasonSourceChanged {
		t.Fatalf("stale status = %#v", status)
	}
	result, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.QuarantinePaths) != 0 {
		t.Fatalf("valid stale Projection should be replaced, not quarantined: %#v", result.QuarantinePaths)
	}
	current, err := os.ReadFile(chapterPath)
	if err != nil || !reflect.DeepEqual(current, newBytes) {
		t.Fatalf("author edit changed during rebuild: %q err=%v", current, err)
	}
	assertProjectionQueryPath(t, service, "author", "chapters/ch1.md")
}

func TestProjectionStatusRejectsPersistedContentOutsideConfiguredSnapshotBounds(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "source")
	options := qualityworkspace.ProjectionSourceOptions{Limits: qualityworkspace.SourceSnapshotLimits{
		MaxFiles:      10,
		MaxEntries:    100,
		MaxPathBytes:  4096,
		MaxFileBytes:  1024,
		MaxTotalBytes: 4096,
	}}
	service, err := newProjectionTestService(t, Options{Workspace: workspace, SourceOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	path := "chapters/ch1.md"
	content := []byte(strings.Repeat("x", 1025))
	pathDigest := sha256.Sum256([]byte(path))
	contentDigest := sha256.Sum256(content)
	document := qualityworkspace.SourceDocument{
		ID:           "doc-" + hex.EncodeToString(pathDigest[:]),
		Path:         path,
		RevisionHash: hex.EncodeToString(contentDigest[:]),
		Profile:      qualityworkspace.ProjectionProfileWorkspace,
		Kind:         "markdown",
		Size:         int64(len(content)),
		Content:      content,
	}
	db := openProjectionTestDatabase(t, result.DatabasePath)
	documents, err := readProjectionDocuments(context.Background(), db)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE source_documents SET revision_hash = ?, content = ? WHERE canonical_path = ?", document.RevisionHash, string(content), path); err != nil {
		db.Close()
		t.Fatal(err)
	}
	for index := range documents {
		if documents[index].Path == path {
			documents[index] = document
		}
	}
	if _, err := db.Exec("UPDATE projection_metadata SET source_snapshot_hash = ?", qualityworkspace.ProjectionSourceFingerprint(documents)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	status, err := service.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateUnavailable || status.Reason != ReasonIntegrityFailed {
		t.Fatalf("out-of-bounds persisted status = %#v", status)
	}
}

func TestProjectionStatusRejectsPersistedPathsOutsideConfiguredSnapshotBounds(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "source")
	options := qualityworkspace.ProjectionSourceOptions{Limits: qualityworkspace.SourceSnapshotLimits{
		MaxFiles:      10,
		MaxEntries:    100,
		MaxPathBytes:  256,
		MaxFileBytes:  1024,
		MaxTotalBytes: 4096,
	}}
	service, err := newProjectionTestService(t, Options{Workspace: workspace, SourceOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	db := openProjectionTestDatabase(t, result.DatabasePath)
	documents, err := readProjectionDocuments(context.Background(), db)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	longPath := "chapters/" + strings.Repeat("a", 240) + ".md"
	pathDigest := sha256.Sum256([]byte(longPath))
	for index := range documents {
		if documents[index].Path != "chapters/ch1.md" {
			continue
		}
		documents[index].Path = longPath
		documents[index].ID = "doc-" + hex.EncodeToString(pathDigest[:])
		if _, err := db.Exec("UPDATE source_documents SET document_id = ?, canonical_path = ? WHERE canonical_path = ?", documents[index].ID, longPath, "chapters/ch1.md"); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("UPDATE projection_metadata SET source_snapshot_hash = ?", qualityworkspace.ProjectionSourceFingerprint(documents)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	status, err := service.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateUnavailable || status.Reason != ReasonIntegrityFailed {
		t.Fatalf("out-of-bounds persisted paths status = %#v", status)
	}
}

func TestProjectionStatusRejectsNonCanonicalPersistedPath(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "canonical source")
	service, databasePath := rebuildProjectionForRecoveryTest(t, workspace)

	path := "../outside.md"
	content := []byte("canonical source")
	pathDigest := sha256.Sum256([]byte(path))
	contentDigest := sha256.Sum256(content)
	document := qualityworkspace.SourceDocument{
		ID:           "doc-" + hex.EncodeToString(pathDigest[:]),
		Path:         path,
		RevisionHash: hex.EncodeToString(contentDigest[:]),
		Profile:      qualityworkspace.ProjectionProfileWorkspace,
		Kind:         "markdown",
		Size:         int64(len(content)),
		Content:      content,
	}
	db := openProjectionTestDatabase(t, databasePath)
	documents, err := readProjectionDocuments(context.Background(), db)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE source_documents SET document_id = ?, canonical_path = ? WHERE canonical_path = 'chapters/ch1.md'", document.ID, document.Path); err != nil {
		db.Close()
		t.Fatal(err)
	}
	for index := range documents {
		if documents[index].Path == "chapters/ch1.md" {
			documents[index] = document
		}
	}
	if _, err := db.Exec("UPDATE projection_metadata SET source_snapshot_hash = ?", qualityworkspace.ProjectionSourceFingerprint(documents)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	status, err := service.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateUnavailable || status.Reason != ReasonIntegrityFailed {
		t.Fatalf("non-canonical persisted path status = %#v", status)
	}
}

func rebuildProjectionForRecoveryTest(t *testing.T, workspace string) (*Service, string) {
	t.Helper()
	service, err := newProjectionTestService(t, Options{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Rebuild(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return service, result.DatabasePath
}

func assertProjectionFailureLeavesWorkspaceReadable(t *testing.T, workspace, wantChapter string) {
	t.Helper()
	inspector, err := qualityworkspace.NewInspector(qualityworkspace.InspectorOptions{ApplicationVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := inspector.Inspect(workspace)
	if err != nil {
		t.Fatalf("workspace Inspector after Projection failure: %v", err)
	}
	if !inspection.CanOpen() {
		t.Fatalf("workspace Inspector cannot open: %#v", inspection)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "chapters", "ch1.md"))
	if err != nil || string(content) != wantChapter {
		t.Fatalf("formal chapter = %q err=%v", content, err)
	}
}

func assertProjectionQueryPath(t *testing.T, service *Service, term, wantPath string) {
	t.Helper()
	reader, err := service.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	response, err := reader.Query(context.Background(), QueryRequest{Text: term})
	if err != nil || len(response.Results) != 1 || response.Results[0].Path != wantPath {
		t.Fatalf("Query(%q) = %#v err=%v", term, response, err)
	}
}

func projectionQuarantineFiles(t *testing.T, workspace string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(workspace, ".denova", "index.db-quarantine-*"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func isLockedSQLiteMessage(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "locked") || strings.Contains(message, "busy")
}
