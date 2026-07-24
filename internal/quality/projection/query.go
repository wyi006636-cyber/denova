package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	DefaultQueryResults = 50
	MaxQueryResults     = 200
)

// QueryStrategy identifies the exact retrieval path used for a request.
type QueryStrategy string

const (
	QueryStrategyTrigram   QueryStrategy = "fts5_trigram"
	QueryStrategyExactScan QueryStrategy = "exact_scan"
)

// QueryRequest is a bounded literal text lookup.
type QueryRequest struct {
	Text  string
	Limit int
}

// QueryResult identifies one authoritative source revision represented by the
// disposable Projection. It deliberately exposes no mutation method.
type QueryResult struct {
	DocumentID   string
	Path         string
	RevisionHash string
	Profile      string
	Kind         string
}

// QueryResponse reports both results and the retrieval strategy.
type QueryResponse struct {
	Strategy QueryStrategy
	Results  []QueryResult
}

// Reader owns one validated, pinned SQLite connection. Validation and queries
// never cross a path reopen boundary.
type Reader struct {
	database   *sql.DB
	connection *sql.Conn
	path       string
	identity   os.FileInfo
}

func openProjectionReader(ctx context.Context, path string) (*Reader, error) {
	reader, err := openProjectionReaderCandidate(ctx, path, "ro")
	if err != nil {
		return nil, err
	}
	if err := reader.enableQueryOnly(ctx); err != nil {
		reader.Close()
		return nil, err
	}
	return reader, nil
}

func openProjectionReaderCandidate(ctx context.Context, path, mode string) (*Reader, error) {
	if ctx == nil {
		return nil, errors.New("Projection reader context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect Projection database: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Projection database is not a regular file: %s", path)
	}
	dsn, err := projectionFileURI(path, mode)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open Projection reader: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	connection, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("pin Projection reader connection: %w", err)
	}
	if err := connection.PingContext(ctx); err != nil {
		connection.Close()
		db.Close()
		return nil, fmt.Errorf("ping Projection reader: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "PRAGMA busy_timeout = 0"); err != nil {
		connection.Close()
		db.Close()
		return nil, fmt.Errorf("configure Projection reader busy timeout: %w", err)
	}
	current, err := os.Lstat(path)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		connection.Close()
		db.Close()
		return nil, errors.New("Projection database identity changed while pinning reader")
	}
	return &Reader{database: db, connection: connection, path: path, identity: info}, nil
}

func (reader *Reader) enableQueryOnly(ctx context.Context) error {
	if reader == nil || reader.connection == nil {
		return errors.New("Projection reader is closed or unavailable")
	}
	if _, err := reader.connection.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
		return fmt.Errorf("configure Projection reader query-only mode: %w", err)
	}
	return nil
}

// Query performs a literal trigram lookup or an exact scan for terms shorter
// than three Unicode characters, which FTS5 trigram cannot serve.
func (reader *Reader) Query(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	if reader == nil || reader.connection == nil {
		return QueryResponse{}, errors.New("Projection reader is closed or unavailable")
	}
	if ctx == nil {
		return QueryResponse{}, errors.New("Projection query context is required")
	}
	if err := ctx.Err(); err != nil {
		return QueryResponse{}, err
	}
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return QueryResponse{}, errors.New("Projection query text is required")
	}
	limit := request.Limit
	if limit == 0 {
		limit = DefaultQueryResults
	}
	if limit < 1 || limit > MaxQueryResults {
		return QueryResponse{}, fmt.Errorf("Projection query limit %d is outside 1..%d", limit, MaxQueryResults)
	}

	strategy := QueryStrategyTrigram
	statement := `
		SELECT source_documents.document_id, source_documents.canonical_path,
		       source_documents.revision_hash, source_documents.profile, source_documents.kind
		FROM source_documents_fts
		JOIN source_documents ON source_documents.rowid = source_documents_fts.rowid
		WHERE source_documents_fts MATCH ?
		ORDER BY bm25(source_documents_fts), source_documents.canonical_path
		LIMIT ?`
	arguments := []any{ftsLiteral(text), limit}
	if utf8.RuneCountInString(text) < 3 {
		strategy = QueryStrategyExactScan
		statement = `
			SELECT document_id, canonical_path, revision_hash, profile, kind
			FROM source_documents
			WHERE instr(canonical_path, ?) > 0 OR instr(content, ?) > 0
			ORDER BY canonical_path
			LIMIT ?`
		arguments = []any{text, text, limit}
	}
	rows, err := reader.connection.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("query Projection strategy=%s: %w", strategy, err)
	}
	defer rows.Close()
	response := QueryResponse{Strategy: strategy, Results: make([]QueryResult, 0)}
	for rows.Next() {
		var result QueryResult
		if err := rows.Scan(&result.DocumentID, &result.Path, &result.RevisionHash, &result.Profile, &result.Kind); err != nil {
			return QueryResponse{}, fmt.Errorf("scan Projection query result: %w", err)
		}
		response.Results = append(response.Results, result)
	}
	if err := rows.Err(); err != nil {
		return QueryResponse{}, fmt.Errorf("iterate Projection query results: %w", err)
	}
	return response, nil
}

// Close releases the read-only Projection connection pool.
func (reader *Reader) Close() error {
	if reader == nil || reader.connection == nil {
		return nil
	}
	connection := reader.connection
	database := reader.database
	reader.connection = nil
	reader.database = nil
	first := connection.Close()
	if database != nil {
		if err := database.Close(); first == nil {
			first = err
		}
	}
	return first
}

func ftsLiteral(text string) string {
	return `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
}

func projectionFileURI(path, mode string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("make Projection database path absolute: %w", err)
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	query := uri.Query()
	query.Set("mode", mode)
	uri.RawQuery = query.Encode()
	return uri.String(), nil
}
