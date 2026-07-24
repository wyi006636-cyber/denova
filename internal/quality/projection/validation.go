package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	qualityworkspace "denova/internal/quality/workspace"
)

// validateProjectionStage independently reopens a completed sibling and
// validates every persisted contract before source CAS and activation. The
// connection is closed before this function returns.
func validateProjectionStage(ctx context.Context, path, buildIdentity string, snapshot qualityworkspace.ProjectionSourceSnapshot, snapshotLimits qualityworkspace.SourceSnapshotLimits, hooks Hooks) (resultErr error) {
	if ctx == nil {
		return errors.New("Projection validation context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect completed Projection sibling: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return errors.New("completed Projection sibling is not a regular file")
	}
	dsn, err := projectionFileURI(path, "rw")
	if err != nil {
		return err
	}
	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return fmt.Errorf("open completed Projection sibling: %w", err)
	}
	db.SetMaxOpenConns(1)
	connection, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return fmt.Errorf("pin completed Projection validation connection: %w", err)
	}
	defer func() {
		if closeErr := connection.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("close completed Projection validation connection: %w", closeErr)
		}
		if closeErr := db.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("close completed Projection validation database: %w", closeErr)
		}
	}()
	if _, err := connection.ExecContext(ctx, "PRAGMA busy_timeout = 0"); err != nil {
		return fmt.Errorf("configure completed Projection sibling: %w", err)
	}
	return validateProjectionConnection(ctx, connection, path, pathInfo, buildIdentity, snapshot, snapshotLimits, hooks)
}

func validateProjectionConnection(ctx context.Context, connection projectionSQLConnection, path string, pathInfo os.FileInfo, buildIdentity string, snapshot qualityworkspace.ProjectionSourceSnapshot, snapshotLimits qualityworkspace.SourceSnapshotLimits, hooks Hooks) error {
	var schemaVersion int
	if err := connection.QueryRowContext(ctx, "PRAGMA user_version").Scan(&schemaVersion); err != nil {
		return fmt.Errorf("read completed Projection schema version: %w", err)
	}
	if schemaVersion != SchemaVersionV1 {
		return fmt.Errorf("completed Projection schema version = %d, want %d", schemaVersion, SchemaVersionV1)
	}
	metadata, err := readProjectionMetadata(ctx, connection)
	if err != nil {
		return err
	}
	if metadata.SchemaVersion != SchemaVersionV1 || metadata.BuildIdentity != buildIdentity ||
		metadata.DriverModule != DriverModule || metadata.DriverVersion != DriverVersion ||
		metadata.LibcModule != LibcModule || metadata.LibcVersion != LibcVersion ||
		metadata.SQLiteVersion == "" || metadata.SourceSnapshotHash == "" || metadata.DocumentCount < 0 {
		return errors.New("completed Projection metadata identity is invalid")
	}
	var runtimeSQLiteVersion string
	if err := connection.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&runtimeSQLiteVersion); err != nil {
		return fmt.Errorf("read completed Projection SQLite version: %w", err)
	}
	if metadata.SQLiteVersion != runtimeSQLiteVersion {
		return fmt.Errorf("completed Projection SQLite version = %q, runtime = %q", metadata.SQLiteVersion, runtimeSQLiteVersion)
	}
	if err := validateProjectionSchemaObjects(ctx, connection); err != nil {
		return err
	}
	if err := runProjectionQuickCheck(ctx, connection); err != nil {
		return err
	}
	limits, err := qualityworkspace.EffectiveProjectionSourceLimits(snapshotLimits)
	if err != nil {
		return err
	}
	if err := validateProjectionStorageBounds(ctx, connection, limits); err != nil {
		return err
	}
	documents, err := readProjectionDocuments(ctx, connection)
	if err != nil {
		return err
	}
	if len(documents) != metadata.DocumentCount || !projectionDocumentsSelfConsistent(documents) ||
		qualityworkspace.ProjectionSourceFingerprint(documents) != metadata.SourceSnapshotHash {
		return errors.New("completed Projection rows, revisions, and snapshot fingerprint are inconsistent")
	}
	if err := runExternalContentIntegrityCheck(ctx, connection, IntegrityCorruptionCheck, hooks); err != nil {
		return err
	}
	currentInfo, err := os.Lstat(path)
	if err != nil || currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.Mode().IsRegular() || !os.SameFile(pathInfo, currentInfo) {
		return errors.New("completed Projection sibling identity changed during validation")
	}
	if metadata.SourceSnapshotHash != snapshot.Hash || metadata.DocumentCount != len(snapshot.Documents) ||
		!projectionDocumentsMatchSnapshot(documents, snapshot.Documents) {
		return ErrSourceChanged
	}
	return nil
}

func validateProjectionStorageBounds(ctx context.Context, db projectionSQLConnection, limits qualityworkspace.SourceSnapshotLimits) error {
	var documentCount, totalPathBytes, largestDocument, totalBytes int64
	err := db.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(length(CAST(canonical_path AS BLOB))), 0),
		COALESCE(MAX(length(CAST(content AS BLOB))), 0),
		COALESCE(SUM(length(CAST(content AS BLOB))), 0)
		FROM source_documents`).Scan(&documentCount, &totalPathBytes, &largestDocument, &totalBytes)
	if err != nil {
		return fmt.Errorf("measure Projection storage bounds: %w", err)
	}
	if documentCount > int64(limits.MaxFiles) || totalPathBytes > limits.MaxPathBytes || largestDocument > limits.MaxFileBytes || totalBytes > limits.MaxTotalBytes {
		return fmt.Errorf("Projection storage exceeds source snapshot limits: documents=%d/%d path_bytes=%d/%d largest_bytes=%d/%d total_bytes=%d/%d",
			documentCount, limits.MaxFiles, totalPathBytes, limits.MaxPathBytes, largestDocument, limits.MaxFileBytes, totalBytes, limits.MaxTotalBytes)
	}
	return nil
}

func validateProjectionSchemaObjects(ctx context.Context, db projectionSQLConnection) error {
	expected := []struct {
		objectType string
		name       string
		table      string
		definition string
	}{
		{objectType: "table", name: "projection_metadata", table: "projection_metadata", definition: projectionMetadataDDL},
		{objectType: "table", name: "source_documents", table: "source_documents", definition: projectionDocumentsDDL},
		{objectType: "table", name: "source_documents_fts", table: "source_documents_fts", definition: projectionFTSDDL},
		{objectType: "trigger", name: "source_documents_ai", table: "source_documents", definition: projectionInsertTriggerDDL},
		{objectType: "trigger", name: "source_documents_ad", table: "source_documents", definition: projectionDeleteTriggerDDL},
		{objectType: "trigger", name: "source_documents_au", table: "source_documents", definition: projectionUpdateTriggerDDL},
	}
	for _, object := range expected {
		var definition string
		err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema
			WHERE type = ? AND name = ? AND tbl_name = ?`, object.objectType, object.name, object.table).Scan(&definition)
		if err != nil {
			return fmt.Errorf("read Projection schema object type=%s name=%s: %w", object.objectType, object.name, err)
		}
		if normalizeProjectionSQL(definition) != normalizeProjectionSQL(object.definition) {
			return fmt.Errorf("Projection schema object type=%s name=%s does not match schema v1", object.objectType, object.name)
		}
	}
	return nil
}

func normalizeProjectionSQL(statement string) string {
	return strings.Map(func(value rune) rune {
		if unicode.IsSpace(value) {
			return -1
		}
		return unicode.ToLower(value)
	}, statement)
}
