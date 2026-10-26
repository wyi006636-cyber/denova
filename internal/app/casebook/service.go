// Package casebook adapts the book case format to Project-scoped file APIs.
// The service does not expose a model tool: only an explicit author action can
// promote or demote a case.
package casebook

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	projectfiles "denova/internal/app/projectfiles"
	bookcase "denova/internal/book/casebook"
	workspacechange "denova/internal/workspace/change"
)

const (
	CaseNotFound          = "case_not_found"
	CaseIndexMissing      = "case_index_missing"
	CaseRevisionConflict  = "case_revision_conflict"
	CaseQuoteMissing      = "case_quote_missing"
	CaseQuoteAmbiguous    = "case_quote_ambiguous"
	CaseQuoteTooLong      = "case_quote_too_long"
	CaseQuoteEmpty        = "case_quote_empty"
	CaseSourceMissing     = "case_source_missing"
	CaseSourceOutsideBook = "case_source_outside_book"
	CaseInvalid           = "case_invalid"
	CaseIssueMissing      = "case_file_missing"
	CaseIssueInvalid      = "case_format_invalid"
)

// Error is a stable, localizable error returned by case actions.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Code
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

func errCode(code string, cause error) error { return &Error{Code: code, Err: cause} }

type Host interface {
	ProjectFiles() *projectfiles.Service
}

type Service struct{ host Host }

func NewService(host Host) *Service { return &Service{host: host} }

type CaseDocument struct {
	bookcase.Case
	SourceExists bool   `json:"source_exists"`
	Revision     string `json:"revision"`
	Registered   bool   `json:"registered"`
	Issue        string `json:"issue,omitempty"`
}

type IndexSnapshot struct {
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

type Snapshot struct {
	Index   IndexSnapshot         `json:"index"`
	Entries []bookcase.IndexEntry `json:"entries"`
	Cases   []CaseDocument        `json:"cases"`
}

type ToggleRequest struct {
	Path          string `json:"path"`
	CaseRevision  string `json:"case_revision,omitempty"`
	IndexRevision string `json:"index_revision,omitempty"`
	Revision      string `json:"revision,omitempty"`
	Collected     bool   `json:"collected"`
}

type ToggleResult struct {
	Case      CaseDocument  `json:"case"`
	Index     IndexSnapshot `json:"index"`
	Collected bool          `json:"collected"`
}

func (service *Service) List(ctx context.Context, projectID string) (Snapshot, error) {
	if service == nil || service.host == nil || service.host.ProjectFiles() == nil {
		return Snapshot{}, errors.New("casebook project file service is unavailable")
	}
	files := service.host.ProjectFiles()
	index, err := readOptional(files, ctx, projectID, bookcase.CaseIndexPath)
	if err != nil {
		return Snapshot{}, err
	}
	entries := []bookcase.IndexEntry{}
	if index.Found {
		entries = bookcase.ParseIndexMarkdown(index.Document.Content)
	}
	cases := make([]CaseDocument, 0, len(entries))
	indexed := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		indexed[entry.ID] = struct{}{}
		path := bookcase.CaseDirectory + entry.ID + ".md"
		document, readErr := files.ReadFile(ctx, projectID, path)
		if readErr != nil {
			// Keep the index row visible when a case file is missing; the UI can
			// show an actionable broken-entry state instead of dropping it.
			cases = append(cases, CaseDocument{Case: bookcase.Case{ID: entry.ID, Status: entry.Status, Direction: entry.Direction, Method: entry.Method, Path: path}, Registered: true, Issue: CaseIssueMissing})
			continue
		}
		parsed, parseErr := bookcase.ParseCaseMarkdown(document.Content)
		if parseErr != nil {
			cases = append(cases, CaseDocument{Case: bookcase.Case{ID: entry.ID, Status: entry.Status, Path: path}, Registered: true, Issue: CaseIssueInvalid})
			continue
		}
		parsed.Path = path
		parsed.Revision = document.Revision
		sourceExists := false
		if parsed.Source != "" {
			if sourceDoc, sourceErr := files.ReadFile(ctx, projectID, parsed.Source); sourceErr == nil && sourceDoc.Kind == projectfiles.DocumentText {
				sourceExists = true
			}
		}
		cases = append(cases, CaseDocument{Case: parsed, SourceExists: sourceExists, Revision: document.Revision, Registered: true})
	}
	// A case file can be created before its compact index row. Resolve the
	// directory so authors can still inspect and collect that orphan case.
	for _, path := range service.casePaths(ctx, files, projectID) {
		id := strings.TrimSuffix(strings.TrimPrefix(path, bookcase.CaseDirectory), ".md")
		if _, ok := indexed[id]; ok {
			continue
		}
		document, readErr := files.ReadFile(ctx, projectID, path)
		if readErr != nil {
			cases = append(cases, CaseDocument{Case: bookcase.Case{ID: id, Path: path}, Issue: CaseIssueInvalid})
			continue
		}
		parsed, parseErr := bookcase.ParseCaseMarkdown(document.Content)
		if parseErr != nil {
			cases = append(cases, CaseDocument{Case: bookcase.Case{ID: id, Path: path}, Issue: CaseIssueInvalid})
			continue
		}
		parsed.Path = path
		parsed.Revision = document.Revision
		sourceExists := false
		if parsed.Source != "" {
			if sourceDoc, sourceErr := files.ReadFile(ctx, projectID, parsed.Source); sourceErr == nil && sourceDoc.Kind == projectfiles.DocumentText {
				sourceExists = true
			}
		}
		cases = append(cases, CaseDocument{Case: parsed, SourceExists: sourceExists, Revision: document.Revision})
	}
	return Snapshot{
		Index:   IndexSnapshot{Content: index.Document.Content, Revision: index.Document.Revision},
		Entries: entries,
		Cases:   cases,
	}, nil
}

func (service *Service) Toggle(ctx context.Context, projectID string, request ToggleRequest) (ToggleResult, error) {
	if service == nil || service.host == nil || service.host.ProjectFiles() == nil {
		return ToggleResult{}, errors.New("casebook project file service is unavailable")
	}
	path, id, err := normalizeCasePath(request.Path)
	if err != nil {
		return ToggleResult{}, errCode(CaseInvalid, err)
	}
	files := service.host.ProjectFiles()
	caseDocument, err := files.ReadFile(ctx, projectID, path)
	if err != nil {
		if os.IsNotExist(err) {
			return ToggleResult{}, errCode(CaseNotFound, err)
		}
		return ToggleResult{}, err
	}
	parsed, err := bookcase.ParseCaseMarkdown(caseDocument.Content)
	if err != nil {
		return ToggleResult{}, errCode(CaseInvalid, err)
	}
	if parsed.ID != id {
		return ToggleResult{}, errCode(CaseInvalid, fmt.Errorf("case id %q does not match path %q", parsed.ID, path))
	}
	index, err := readOptional(files, ctx, projectID, bookcase.CaseIndexPath)
	if err != nil {
		return ToggleResult{}, err
	}
	if !index.Found {
		return ToggleResult{}, errCode(CaseIndexMissing, errors.New("case index is missing"))
	}
	caseRevision := strings.TrimSpace(request.CaseRevision)
	if caseRevision == "" {
		caseRevision = strings.TrimSpace(request.Revision)
	}
	if caseRevision == "" {
		return ToggleResult{}, errCode(CaseRevisionConflict, errors.New("case revision is required"))
	}
	if caseRevision != caseDocument.Revision {
		return ToggleResult{}, errCode(CaseRevisionConflict, fmt.Errorf("case revision changed"))
	}
	indexRevision := strings.TrimSpace(request.IndexRevision)
	if indexRevision == "" {
		return ToggleResult{}, errCode(CaseRevisionConflict, errors.New("case index revision is required"))
	}
	if indexRevision != index.Document.Revision {
		return ToggleResult{}, errCode(CaseRevisionConflict, errors.New("case index revision changed"))
	}

	targetStatus := bookcase.StatusPending
	quoteLocation := 0
	if request.Collected {
		targetStatus = bookcase.StatusCollected
		if !isSourcePathInsideBook(parsed.Source) {
			return ToggleResult{}, errCode(CaseSourceOutsideBook, errors.New("case source must be under references/sources"))
		}
		source, sourceErr := files.ReadFile(ctx, projectID, parsed.Source)
		if sourceErr != nil {
			return ToggleResult{}, errCode(CaseSourceMissing, sourceErr)
		}
		if source.Kind != projectfiles.DocumentText {
			return ToggleResult{}, errCode(CaseSourceMissing, errors.New("case source is not a text file"))
		}
		location, locateErr := bookcase.QuoteLocatedWithContext(source.Content, parsed.Quote, parsed.Prefix, parsed.Suffix)
		if locateErr != nil {
			code := quoteErrorCode(locateErr)
			if code == "" {
				code = CaseQuoteMissing
			}
			return ToggleResult{}, errCode(code, locateErr)
		}
		quoteLocation = location.Line
	}
	caseContent, err := bookcase.PatchCaseStatus(caseDocument.Content, targetStatus, quoteLocation)
	if err != nil {
		return ToggleResult{}, errCode(CaseInvalid, err)
	}
	indexContent, err := bookcase.PatchIndexStatus(index.Document.Content, id, targetStatus)
	if err != nil && !bookcase.IndexHasEntry(index.Document.Content, id) && request.Collected {
		indexContent, err = bookcase.EnsureIndexEntry(index.Document.Content, id, parsed.Direction, parsed.Method, targetStatus)
	}
	if err != nil {
		return ToggleResult{}, errCode(CaseInvalid, err)
	}
	// Both revisions are checked before the first write. If the second write
	// still fails, restore the first through the same revision-aware API.
	caseSaved, err := files.SaveFile(ctx, projectID, projectfiles.SaveRequest{Path: path, Content: caseContent, BaseRevision: caseDocument.Revision})
	if err != nil {
		return ToggleResult{}, mapProjectFileError(err)
	}
	indexSaved, err := files.SaveFile(ctx, projectID, projectfiles.SaveRequest{Path: bookcase.CaseIndexPath, Content: indexContent, BaseRevision: index.Document.Revision})
	if err != nil {
		if rollbackErr := rollbackCase(ctx, files, projectID, path, caseSaved.Revision, caseDocument.Content); rollbackErr != nil {
			return ToggleResult{}, fmt.Errorf("case index save failed and rollback failed: %w; rollback: %v", err, rollbackErr)
		}
		return ToggleResult{}, mapProjectFileError(err)
	}
	updated, parseErr := bookcase.ParseCaseMarkdown(caseContent)
	if parseErr != nil {
		return ToggleResult{}, errCode(CaseInvalid, parseErr)
	}
	updated.Path = path
	updated.Revision = caseSaved.Revision
	return ToggleResult{Case: CaseDocument{Case: updated, Revision: caseSaved.Revision, SourceExists: true, Registered: true}, Index: IndexSnapshot{Content: indexContent, Revision: indexSaved.Revision}, Collected: request.Collected}, nil
}

func rollbackCase(ctx context.Context, files *projectfiles.Service, projectID, path, revision, content string) error {
	_, err := files.SaveFile(ctx, projectID, projectfiles.SaveRequest{Path: path, Content: content, BaseRevision: revision})
	return err
}

type optionalDocument struct {
	Found    bool
	Document projectfiles.Document
}

func readOptional(files *projectfiles.Service, ctx context.Context, projectID, path string) (optionalDocument, error) {
	document, err := files.ReadFile(ctx, projectID, path)
	if err == nil {
		return optionalDocument{Found: true, Document: document}, nil
	}
	if os.IsNotExist(err) {
		return optionalDocument{}, nil
	}
	var changeErr *workspacechange.Error
	if errors.As(err, &changeErr) && changeErr.Code == workspacechange.ErrorCodeNotFound {
		return optionalDocument{}, nil
	}
	return optionalDocument{}, err
}

func mapProjectFileError(err error) error {
	var changeErr *workspacechange.Error
	if errors.As(err, &changeErr) && changeErr.Code == workspacechange.ErrorCodeRevisionConflict {
		return errCode(CaseRevisionConflict, err)
	}
	return err
}

func normalizeCasePath(input string) (string, string, error) {
	input = strings.TrimSpace(strings.ReplaceAll(input, `\`, "/"))
	if input == "" {
		return "", "", errors.New("case path is required")
	}
	if filepath.IsAbs(input) || filepath.VolumeName(input) != "" || !strings.HasPrefix(input, bookcase.CaseDirectory) || !strings.HasSuffix(strings.ToLower(input), ".md") {
		return "", "", errors.New("case path must be a Markdown file under setting/cases")
	}
	id := strings.TrimSuffix(strings.TrimPrefix(input, bookcase.CaseDirectory), ".md")
	if id == "" || strings.Contains(id, "/") || id == "." || id == ".." || hasParentPathSegment(input) {
		return "", "", errors.New("case path contains an invalid id")
	}
	return bookcase.CaseDirectory + id + ".md", id, nil
}

func (service *Service) casePaths(ctx context.Context, files *projectfiles.Service, projectID string) []string {
	response, err := files.ResolveTree(ctx, projectID, projectfiles.TreeResolveRequest{
		Targets:     []projectfiles.TreeResolveTarget{{Path: strings.TrimSuffix(bookcase.CaseDirectory, "/")}},
		EntryBudget: 100_000,
	})
	if err != nil || len(response.Results) == 0 || !response.Results[0].OK {
		return nil
	}
	paths := make([]string, 0)
	for _, directory := range response.Results[0].Directories {
		for _, entry := range directory.Entries {
			if entry.Type == projectfiles.EntryFile && strings.HasPrefix(entry.Path, bookcase.CaseDirectory) && strings.HasSuffix(strings.ToLower(entry.Path), ".md") {
				paths = append(paths, entry.Path)
			}
		}
	}
	return paths
}

func hasParentPathSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func isSourcePathInsideBook(value string) bool {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if value == "" || filepath.IsAbs(value) || filepath.VolumeName(value) != "" || !strings.HasPrefix(value, "references/sources/") {
		return false
	}
	return !hasParentPathSegment(value)
}

func quoteErrorCode(err error) string {
	switch bookcase.QuoteErrorCode(err) {
	case bookcase.QuoteErrorEmpty:
		return CaseQuoteEmpty
	case bookcase.QuoteErrorAmbiguous:
		return CaseQuoteAmbiguous
	case bookcase.QuoteErrorTooLong:
		return CaseQuoteTooLong
	case bookcase.QuoteErrorMissing:
		return CaseQuoteMissing
	default:
		return ""
	}
}
