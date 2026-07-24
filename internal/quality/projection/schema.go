package projection

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	_ "modernc.org/sqlite"
)

const externalContentIntegritySQL = "INSERT INTO source_documents_fts(source_documents_fts, rank) VALUES ('integrity-check', 1)"

const (
	projectionMetadataDDL = `CREATE TABLE projection_metadata (
		singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
		schema_version INTEGER NOT NULL,
		build_identity TEXT NOT NULL,
		driver_module TEXT NOT NULL,
		driver_version TEXT NOT NULL,
		libc_module TEXT NOT NULL,
		libc_version TEXT NOT NULL,
		sqlite_version TEXT NOT NULL,
		source_snapshot_hash TEXT NOT NULL,
		source_document_count INTEGER NOT NULL CHECK (source_document_count >= 0)
	) STRICT`
	projectionDocumentsDDL = `CREATE TABLE source_documents (
		rowid INTEGER PRIMARY KEY,
		document_id TEXT NOT NULL UNIQUE,
		canonical_path TEXT NOT NULL UNIQUE,
		revision_hash TEXT NOT NULL,
		profile TEXT NOT NULL,
		kind TEXT NOT NULL,
		content TEXT NOT NULL
	) STRICT`
	projectionFTSDDL = `CREATE VIRTUAL TABLE source_documents_fts USING fts5(
		canonical_path,
		content,
		content='source_documents',
		content_rowid='rowid',
		tokenize='trigram'
	)`
	projectionInsertTriggerDDL = `CREATE TRIGGER source_documents_ai AFTER INSERT ON source_documents BEGIN
		INSERT INTO source_documents_fts(rowid, canonical_path, content)
		VALUES (new.rowid, new.canonical_path, new.content);
	END`
	projectionDeleteTriggerDDL = `CREATE TRIGGER source_documents_ad AFTER DELETE ON source_documents BEGIN
		INSERT INTO source_documents_fts(source_documents_fts, rowid, canonical_path, content)
		VALUES ('delete', old.rowid, old.canonical_path, old.content);
	END`
	projectionUpdateTriggerDDL = `CREATE TRIGGER source_documents_au AFTER UPDATE ON source_documents BEGIN
		INSERT INTO source_documents_fts(source_documents_fts, rowid, canonical_path, content)
		VALUES ('delete', old.rowid, old.canonical_path, old.content);
		INSERT INTO source_documents_fts(rowid, canonical_path, content)
		VALUES (new.rowid, new.canonical_path, new.content);
	END`
)

var schemaV1Statements = []string{
	"PRAGMA user_version = 1",
	projectionMetadataDDL,
	projectionDocumentsDDL,
	projectionFTSDDL,
	projectionInsertTriggerDDL,
	projectionDeleteTriggerDDL,
	projectionUpdateTriggerDDL,
}

func configureProjectionConnection(ctx context.Context, db *sql.DB) error {
	for _, statement := range []string{
		"PRAGMA busy_timeout = 0",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = FULL",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure Projection connection with %q: %w", statement, err)
		}
	}
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode = DELETE").Scan(&journalMode); err != nil {
		return fmt.Errorf("configure Projection journal mode: %w", err)
	}
	if journalMode != "delete" {
		return fmt.Errorf("Projection journal mode = %q, want delete", journalMode)
	}
	return nil
}

func probeSQLiteCapabilities(ctx context.Context, db *sql.DB) (buildEvidence, error) {
	var evidence buildEvidence
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&evidence.SQLiteVersion); err != nil {
		return buildEvidence{}, fmt.Errorf("query sqlite_version: %w", err)
	}
	rows, err := db.QueryContext(ctx, "PRAGMA compile_options")
	if err != nil {
		return buildEvidence{}, fmt.Errorf("query SQLite compile options: %w", err)
	}
	for rows.Next() {
		var option string
		if err := rows.Scan(&option); err != nil {
			rows.Close()
			return buildEvidence{}, fmt.Errorf("scan SQLite compile option: %w", err)
		}
		evidence.CompileOptions = append(evidence.CompileOptions, option)
	}
	if err := rows.Close(); err != nil {
		return buildEvidence{}, fmt.Errorf("close SQLite compile options: %w", err)
	}
	if err := rows.Err(); err != nil {
		return buildEvidence{}, fmt.Errorf("iterate SQLite compile options: %w", err)
	}
	sort.Strings(evidence.CompileOptions)
	if !containsCompileOption(evidence.CompileOptions, "ENABLE_FTS5") {
		return buildEvidence{}, fmt.Errorf("SQLite runtime does not advertise ENABLE_FTS5")
	}
	if _, err := db.ExecContext(ctx, "CREATE VIRTUAL TABLE temp.projection_fts5_probe USING fts5(content, tokenize='trigram')"); err != nil {
		return buildEvidence{}, fmt.Errorf("create FTS5 trigram probe table: %w", err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE temp.projection_fts5_probe"); err != nil {
		return buildEvidence{}, fmt.Errorf("drop FTS5 trigram probe table: %w", err)
	}
	return evidence, nil
}

func containsCompileOption(options []string, want string) bool {
	index := sort.SearchStrings(options, want)
	return index < len(options) && options[index] == want
}

func createProjectionSchema(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range schemaV1Statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create Projection schema with %q: %w", statement, err)
		}
	}
	return nil
}
