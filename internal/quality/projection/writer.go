package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"

	qualityworkspace "denova/internal/quality/workspace"
)

type buildStageError struct {
	stage FaultPoint
	err   error
}

func (err *buildStageError) Error() string { return err.err.Error() }
func (err *buildStageError) Unwrap() error { return err.err }

func atBuildStage(stage FaultPoint, err error) error {
	if err == nil {
		return nil
	}
	return &buildStageError{stage: stage, err: err}
}

func buildProjectionDatabase(ctx context.Context, request buildRequest) (evidence buildEvidence, resultErr error) {
	if ctx == nil {
		return buildEvidence{}, errors.New("Projection build context is required")
	}
	if err := ctx.Err(); err != nil {
		return buildEvidence{}, err
	}
	if request.Path == "" || request.Snapshot.Hash == "" || request.BuildIdentity == "" {
		return buildEvidence{}, errors.New("Projection build path, source snapshot hash, and build identity are required")
	}
	if _, err := os.Lstat(request.Path); err == nil {
		return buildEvidence{}, fmt.Errorf("Projection build target already exists: %s", request.Path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return buildEvidence{}, fmt.Errorf("inspect Projection build target: %w", err)
	}

	db, err := sql.Open(sqliteDriverName, request.Path)
	if err != nil {
		return buildEvidence{}, fmt.Errorf("open Projection build database: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer func() {
		if err := db.Close(); resultErr == nil && err != nil {
			resultErr = fmt.Errorf("close Projection build database: %w", err)
		}
	}()
	if err := configureProjectionConnection(ctx, db); err != nil {
		return buildEvidence{}, err
	}
	evidence, err = probeSQLiteCapabilities(ctx, db)
	if err != nil {
		return buildEvidence{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return buildEvidence{}, fmt.Errorf("begin Projection writer transaction: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	if err := createProjectionSchema(ctx, tx); err != nil {
		return buildEvidence{}, err
	}
	if err := request.Hooks.fault(FaultAfterSchema); err != nil {
		return buildEvidence{}, atBuildStage(FaultAfterSchema, err)
	}

	documents := append([]qualityworkspace.SourceDocument(nil), request.Snapshot.Documents...)
	sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	for _, document := range documents {
		if err := validateProjectionDocument(document); err != nil {
			return buildEvidence{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO source_documents(
			document_id, canonical_path, revision_hash, profile, kind, content
		) VALUES (?, ?, ?, ?, ?, ?)`, document.ID, document.Path, document.RevisionHash, document.Profile, document.Kind, string(document.Content)); err != nil {
			return buildEvidence{}, fmt.Errorf("insert Projection source document path=%q: %w", document.Path, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO projection_metadata(
		singleton, schema_version, build_identity, driver_module, driver_version,
		libc_module, libc_version, sqlite_version, source_snapshot_hash, source_document_count
	) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		SchemaVersionV1,
		request.BuildIdentity,
		DriverModule,
		DriverVersion,
		LibcModule,
		LibcVersion,
		evidence.SQLiteVersion,
		request.Snapshot.Hash,
		len(documents),
	); err != nil {
		return buildEvidence{}, fmt.Errorf("insert Projection metadata: %w", err)
	}
	if err := request.Hooks.fault(FaultAfterDataWrite); err != nil {
		return buildEvidence{}, atBuildStage(FaultAfterDataWrite, err)
	}
	if err := request.Hooks.fault(FaultBeforeIntegrityCheck); err != nil {
		return buildEvidence{}, atBuildStage(FaultBeforeIntegrityCheck, err)
	}
	if err := runExternalContentIntegrityCheck(ctx, tx, IntegrityBuildCompletion, request.Hooks); err != nil {
		return buildEvidence{}, err
	}
	if request.FreshActivation {
		if err := runExternalContentIntegrityCheck(ctx, tx, IntegrityFreshActivation, request.Hooks); err != nil {
			return buildEvidence{}, err
		}
	}
	if err := request.Hooks.fault(FaultAfterIntegrityCheck); err != nil {
		return buildEvidence{}, atBuildStage(FaultAfterIntegrityCheck, err)
	}
	if err := tx.Commit(); err != nil {
		return buildEvidence{}, fmt.Errorf("commit Projection writer transaction: %w", err)
	}
	return evidence, nil
}

func validateProjectionDocument(document qualityworkspace.SourceDocument) error {
	if document.ID == "" || document.Path == "" || document.RevisionHash == "" || document.Profile == "" || document.Kind == "" {
		return fmt.Errorf("Projection source document has incomplete identity: path=%q", document.Path)
	}
	if document.Size != int64(len(document.Content)) {
		return fmt.Errorf("Projection source document size mismatch path=%q size=%d bytes=%d", document.Path, document.Size, len(document.Content))
	}
	return nil
}
