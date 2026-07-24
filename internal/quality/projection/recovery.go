package projection

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	qualityworkspace "denova/internal/quality/workspace"
)

type projectionMetadata struct {
	SchemaVersion      int
	BuildIdentity      string
	DriverModule       string
	DriverVersion      string
	LibcModule         string
	LibcVersion        string
	SQLiteVersion      string
	SourceSnapshotHash string
	DocumentCount      int
}

type workspaceCompatibilityResult struct {
	status  Status
	blocker error
}

func (service *Service) inspectWorkspaceCompatibility() (workspaceCompatibilityResult, error) {
	status := Status{
		State:        StateAvailable,
		Reason:       ReasonNone,
		DatabasePath: filepath.Join(service.workspace, filepath.FromSlash(DatabaseRelativePath)),
	}
	inspection, err := service.inspector.Inspect(service.workspace)
	if err != nil {
		return workspaceCompatibilityResult{}, err
	}
	if blocker := inspection.RequireManagedMutation(); blocker != nil {
		status.State = StateUnavailable
		status.Reason = ReasonWorkspaceIncompatible
		status.Detail = boundedProjectionDetail(blocker)
		return workspaceCompatibilityResult{status: status, blocker: blocker}, nil
	}
	return workspaceCompatibilityResult{status: status}, nil
}

// Inspect validates the disposable database and compares it with a fresh,
// bounded source snapshot. Expected unavailability is returned as Status,
// leaving formal workspace inspection and editing independent of SQLite.
func (service *Service) Inspect(ctx context.Context) (Status, error) {
	if service == nil {
		return Status{}, errors.New("Projection service is required")
	}
	if ctx == nil {
		return Status{}, errors.New("Projection inspection context is required")
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	compatibility, err := service.inspectWorkspaceCompatibility()
	if err != nil {
		return Status{}, err
	}
	if compatibility.blocker != nil {
		return compatibility.status, nil
	}
	snapshot, err := qualityworkspace.BuildProjectionSourceSnapshot(ctx, service.workspace, service.sourceOptions)
	if err != nil {
		return Status{}, err
	}
	return service.inspectWithSnapshot(ctx, snapshot)
}

func (service *Service) inspectWithSnapshot(ctx context.Context, snapshot qualityworkspace.ProjectionSourceSnapshot) (status Status, resultErr error) {
	status = Status{
		State:              StateUnavailable,
		Reason:             ReasonOpenFailed,
		DatabasePath:       filepath.Join(service.workspace, filepath.FromSlash(DatabaseRelativePath)),
		SourceSnapshotHash: snapshot.Hash,
	}
	pathInfo, err := os.Lstat(status.DatabasePath)
	if errors.Is(err, os.ErrNotExist) {
		status.Reason = ReasonMissing
		return status, nil
	}
	if err != nil {
		status.Detail = boundedProjectionDetail(err)
		return status, nil
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		status.Reason = ReasonIdentityMismatch
		status.Detail = "Projection database path is not a regular file"
		return status, nil
	}

	dsn, err := projectionFileURI(status.DatabasePath, "rw")
	if err != nil {
		return Status{}, err
	}
	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return classifiedProjectionStatus(status, err), nil
	}
	db.SetMaxOpenConns(1)
	defer func() {
		if closeErr := db.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("close Projection inspection database: %w", closeErr)
		}
	}()
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 0"); err != nil {
		return classifiedProjectionStatus(status, err), nil
	}
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&status.SchemaVersion); err != nil {
		return classifiedProjectionStatus(status, err), nil
	}
	if status.SchemaVersion > SchemaVersionV1 {
		status.Reason = ReasonSchemaNewer
		return status, nil
	}
	if status.SchemaVersion != SchemaVersionV1 {
		status.Reason = ReasonIdentityMismatch
		status.Detail = fmt.Sprintf("Projection schema version %d does not match %d", status.SchemaVersion, SchemaVersionV1)
		return status, nil
	}

	metadata, err := readProjectionMetadata(ctx, db)
	if err != nil {
		return classifiedProjectionStatus(status, err), nil
	}
	applyProjectionMetadata(&status, metadata)
	if metadata.SchemaVersion != SchemaVersionV1 ||
		metadata.BuildIdentity != service.buildIdentity ||
		metadata.DriverModule != DriverModule || metadata.DriverVersion != DriverVersion ||
		metadata.LibcModule != LibcModule || metadata.LibcVersion != LibcVersion ||
		metadata.SQLiteVersion == "" || metadata.DocumentCount < 0 || metadata.SourceSnapshotHash == "" {
		status.Reason = ReasonIdentityMismatch
		status.Detail = "Projection metadata identity does not match this build"
		return status, nil
	}
	var runtimeSQLiteVersion string
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&runtimeSQLiteVersion); err != nil {
		return classifiedProjectionStatus(status, err), nil
	}
	if metadata.SQLiteVersion != runtimeSQLiteVersion {
		status.Reason = ReasonIdentityMismatch
		status.Detail = "Projection SQLite runtime identity does not match persisted metadata"
		return status, nil
	}
	if err := validateProjectionSchemaObjects(ctx, db); err != nil {
		status.Reason = ReasonIntegrityFailed
		status.Detail = boundedProjectionDetail(err)
		return status, nil
	}
	if err := runProjectionQuickCheck(ctx, db); err != nil {
		status = classifiedProjectionStatus(status, err)
		if status.Reason == ReasonOpenFailed {
			status.Reason = ReasonCorrupt
		}
		return status, nil
	}
	limits, err := qualityworkspace.EffectiveProjectionSourceLimits(service.sourceOptions.Limits)
	if err != nil {
		return Status{}, err
	}
	if err := validateProjectionStorageBounds(ctx, db, limits); err != nil {
		status.Reason = ReasonIntegrityFailed
		status.Detail = boundedProjectionDetail(err)
		return status, nil
	}

	documents, err := readProjectionDocuments(ctx, db)
	if err != nil {
		status = classifiedProjectionStatus(status, err)
		if status.Reason == ReasonOpenFailed {
			status.Reason = ReasonIntegrityFailed
		}
		return status, nil
	}
	status.DocumentCount = len(documents)
	if len(documents) != metadata.DocumentCount || !projectionDocumentsSelfConsistent(documents) ||
		qualityworkspace.ProjectionSourceFingerprint(documents) != metadata.SourceSnapshotHash {
		status.Reason = ReasonIntegrityFailed
		status.Detail = "Projection rows do not match persisted metadata and revision hashes"
		return status, nil
	}
	if err := runExternalContentIntegrityCheck(ctx, db, IntegrityCorruptionCheck, service.hooks); err != nil {
		status = classifiedProjectionStatus(status, err)
		if status.Reason == ReasonOpenFailed || status.Reason == ReasonCorrupt {
			status.Reason = ReasonIntegrityFailed
		}
		return status, nil
	}
	currentInfo, err := os.Lstat(status.DatabasePath)
	if err != nil || currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.Mode().IsRegular() || !os.SameFile(pathInfo, currentInfo) {
		status.Reason = ReasonIdentityMismatch
		status.Detail = "Projection database identity changed during inspection"
		return status, nil
	}
	if !projectionDocumentsMatchSnapshot(documents, snapshot.Documents) || metadata.SourceSnapshotHash != snapshot.Hash {
		status.State = StateStale
		status.Reason = ReasonSourceChanged
		return status, nil
	}
	status.State = StateAvailable
	status.Reason = ReasonNone
	status.Detail = ""
	return status, nil
}

func readProjectionMetadata(ctx context.Context, db projectionSQLConnection) (projectionMetadata, error) {
	var metadata projectionMetadata
	err := db.QueryRowContext(ctx, `SELECT schema_version, build_identity, driver_module,
		driver_version, libc_module, libc_version, sqlite_version,
		source_snapshot_hash, source_document_count
		FROM projection_metadata WHERE singleton = 1`).Scan(
		&metadata.SchemaVersion,
		&metadata.BuildIdentity,
		&metadata.DriverModule,
		&metadata.DriverVersion,
		&metadata.LibcModule,
		&metadata.LibcVersion,
		&metadata.SQLiteVersion,
		&metadata.SourceSnapshotHash,
		&metadata.DocumentCount,
	)
	if err != nil {
		return projectionMetadata{}, fmt.Errorf("read Projection metadata: %w", err)
	}
	return metadata, nil
}

func applyProjectionMetadata(status *Status, metadata projectionMetadata) {
	status.SchemaVersion = metadata.SchemaVersion
	status.BuildIdentity = metadata.BuildIdentity
	status.DriverModule = metadata.DriverModule
	status.DriverVersion = metadata.DriverVersion
	status.LibcModule = metadata.LibcModule
	status.LibcVersion = metadata.LibcVersion
	status.SQLiteVersion = metadata.SQLiteVersion
	status.ProjectionSnapshotHash = metadata.SourceSnapshotHash
	status.DocumentCount = metadata.DocumentCount
}

func runProjectionQuickCheck(ctx context.Context, db projectionSQLConnection) error {
	rows, err := db.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return fmt.Errorf("run Projection quick_check: %w", err)
	}
	defer rows.Close()
	results := make([]string, 0, 1)
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("scan Projection quick_check: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate Projection quick_check: %w", err)
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("Projection quick_check failed: %s", strings.Join(results, "; "))
	}
	return nil
}

func readProjectionDocuments(ctx context.Context, db projectionSQLConnection) ([]qualityworkspace.SourceDocument, error) {
	rows, err := db.QueryContext(ctx, `SELECT document_id, canonical_path, revision_hash, profile, kind, content
		FROM source_documents ORDER BY canonical_path`)
	if err != nil {
		return nil, fmt.Errorf("read Projection source documents: %w", err)
	}
	defer rows.Close()
	documents := make([]qualityworkspace.SourceDocument, 0)
	for rows.Next() {
		var document qualityworkspace.SourceDocument
		var content string
		if err := rows.Scan(&document.ID, &document.Path, &document.RevisionHash, &document.Profile, &document.Kind, &content); err != nil {
			return nil, fmt.Errorf("scan Projection source document: %w", err)
		}
		document.Content = []byte(content)
		document.Size = int64(len(document.Content))
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Projection source documents: %w", err)
	}
	return documents, nil
}

func projectionDocumentsSelfConsistent(documents []qualityworkspace.SourceDocument) bool {
	previousPath := ""
	for _, document := range documents {
		normalizedPath, pathErr := qualityworkspace.ValidateRelativePath(document.Path, qualityworkspace.PathOptions{Intent: qualityworkspace.PathIntentExisting})
		pathDigest := sha256.Sum256([]byte(document.Path))
		contentDigest := sha256.Sum256(document.Content)
		if pathErr != nil || normalizedPath != document.Path || document.Path <= previousPath ||
			document.ID != "doc-"+hex.EncodeToString(pathDigest[:]) ||
			document.RevisionHash != hex.EncodeToString(contentDigest[:]) ||
			document.Profile == "" || document.Kind == "" || document.Size != int64(len(document.Content)) ||
			!utf8.Valid(document.Content) || bytes.IndexByte(document.Content, 0) >= 0 {
			return false
		}
		previousPath = document.Path
	}
	return true
}

func projectionDocumentsMatchSnapshot(projected, source []qualityworkspace.SourceDocument) bool {
	if len(projected) != len(source) {
		return false
	}
	for index := range projected {
		left, right := projected[index], source[index]
		if left.ID != right.ID || left.Path != right.Path || left.RevisionHash != right.RevisionHash ||
			left.Profile != right.Profile || left.Kind != right.Kind || left.Size != right.Size ||
			string(left.Content) != string(right.Content) {
			return false
		}
	}
	return true
}

func classifiedProjectionStatus(status Status, err error) Status {
	status.Detail = boundedProjectionDetail(err)
	message := strings.ToLower(status.Detail)
	switch {
	case strings.Contains(message, "locked"), strings.Contains(message, "busy"):
		status.Reason = ReasonLocked
	case strings.Contains(message, "malformed"), strings.Contains(message, "not a database"), strings.Contains(message, "disk image is malformed"), strings.Contains(message, "file is encrypted"):
		status.Reason = ReasonCorrupt
	default:
		status.Reason = ReasonOpenFailed
	}
	return status
}

func boundedProjectionDetail(err error) string {
	if err == nil {
		return ""
	}
	const maxDetailBytes = 2048
	detail := err.Error()
	if len(detail) > maxDetailBytes {
		detail = detail[:maxDetailBytes]
	}
	return detail
}
