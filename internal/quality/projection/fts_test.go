package projection

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExternalContentTriggersKeepInsertUpdateDeleteConsistent(t *testing.T) {
	snapshot := projectionTestSnapshot(t)
	databasePath := filepath.Join(t.TempDir(), "index.db")
	_, err := buildProjectionDatabase(context.Background(), buildRequest{
		Path:          databasePath,
		Snapshot:      snapshot,
		BuildIdentity: BuildIdentityV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	db := openProjectionTestDatabase(t, databasePath)
	defer db.Close()

	assertProjectionFTSCount(t, db, "quick", 1)
	assertProjectionFTSCount(t, db, "小说创", 1)

	var rowID int64
	if err := db.QueryRow("SELECT rowid FROM source_documents WHERE canonical_path='chapters/ch1.md'").Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE source_documents SET content=? WHERE rowid=?", "English replacement contains lighthouse signal.", rowID); err != nil {
		t.Fatal(err)
	}
	assertProjectionFTSCount(t, db, "quick", 0)
	assertProjectionFTSCount(t, db, "lighthouse", 1)

	if _, err := db.Exec("DELETE FROM source_documents WHERE rowid=?", rowID); err != nil {
		t.Fatal(err)
	}
	assertProjectionFTSCount(t, db, "lighthouse", 0)

	if _, err := db.Exec(`INSERT INTO source_documents(document_id, canonical_path, revision_hash, profile, kind, content)
		VALUES ('doc-manual', 'chapters/manual.md', 'revision', 'workspace', 'markdown', 'manual insertion remains searchable')`); err != nil {
		t.Fatal(err)
	}
	assertProjectionFTSCount(t, db, "searchable", 1)

	if err := runExternalContentIntegrityCheck(context.Background(), db, IntegrityCorruptionCheck, Hooks{}); err != nil {
		t.Fatalf("rank=1 integrity after trigger mutations: %v", err)
	}
}

func TestProjectionIntegrityUsesRankOneForEveryRequiredPurpose(t *testing.T) {
	snapshot := projectionTestSnapshot(t)
	databasePath := filepath.Join(t.TempDir(), "index.db")
	var purposes []IntegrityPurpose
	_, err := buildProjectionDatabase(context.Background(), buildRequest{
		Path:            databasePath,
		Snapshot:        snapshot,
		BuildIdentity:   BuildIdentityV1,
		FreshActivation: true,
		Hooks: Hooks{OnIntegrity: func(purpose IntegrityPurpose) {
			purposes = append(purposes, purpose)
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantBuildPurposes := []IntegrityPurpose{IntegrityBuildCompletion, IntegrityFreshActivation}
	if !reflect.DeepEqual(purposes, wantBuildPurposes) {
		t.Fatalf("build integrity purposes = %#v, want %#v", purposes, wantBuildPurposes)
	}

	db := openProjectionTestDatabase(t, databasePath)
	defer db.Close()
	if err := runExternalContentIntegrityCheck(context.Background(), db, IntegrityCorruptionCheck, Hooks{OnIntegrity: func(purpose IntegrityPurpose) {
		purposes = append(purposes, purpose)
	}}); err != nil {
		t.Fatal(err)
	}
	wantAll := append(wantBuildPurposes, IntegrityCorruptionCheck)
	if !reflect.DeepEqual(purposes, wantAll) {
		t.Fatalf("all integrity purposes = %#v, want %#v", purposes, wantAll)
	}
}

func TestEveryFreshSiblingActivationRunsRankOneWhenReplacingAnExistingProjection(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "replacement integrity source")
	service, err := newProjectionTestService(t, Options{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}

	var purposes []IntegrityPurpose
	replacement, err := newProjectionTestService(t, Options{
		Workspace: workspace,
		Hooks: Hooks{OnIntegrity: func(purpose IntegrityPurpose) {
			purposes = append(purposes, purpose)
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replacement.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []IntegrityPurpose{
		IntegrityCorruptionCheck,
		IntegrityBuildCompletion,
		IntegrityFreshActivation,
		IntegrityCorruptionCheck,
	}
	if !reflect.DeepEqual(purposes, want) {
		t.Fatalf("replacement integrity purposes = %#v, want %#v", purposes, want)
	}
}

func TestExternalContentIntegrityDetectsContentIndexMismatch(t *testing.T) {
	snapshot := projectionTestSnapshot(t)
	databasePath := filepath.Join(t.TempDir(), "index.db")
	_, err := buildProjectionDatabase(context.Background(), buildRequest{
		Path:          databasePath,
		Snapshot:      snapshot,
		BuildIdentity: BuildIdentityV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	db := openProjectionTestDatabase(t, databasePath)
	defer db.Close()

	if _, err := db.Exec("DROP TRIGGER source_documents_au"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE source_documents SET content='tampered without FTS update' WHERE canonical_path='chapters/ch1.md'"); err != nil {
		t.Fatal(err)
	}
	if err := runExternalContentIntegrityCheck(context.Background(), db, IntegrityCorruptionCheck, Hooks{}); err == nil {
		t.Fatal("rank=1 integrity-check must reject external-content inconsistency")
	}
}

func assertProjectionFTSCount(t *testing.T, db *sql.DB, term string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM source_documents_fts WHERE source_documents_fts MATCH ?", term).Scan(&count); err != nil {
		t.Fatalf("FTS query %q: %v", term, err)
	}
	if count != want {
		t.Fatalf("FTS query %q count = %d, want %d", term, count, want)
	}
}
