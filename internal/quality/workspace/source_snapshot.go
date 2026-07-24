package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"denova/internal/workspacepath"
)

const (
	// ProjectionProfileWorkspace identifies source records that are not yet
	// bound to one of the three Quality Profile contracts.
	ProjectionProfileWorkspace = "workspace"

	defaultSourceSnapshotMaxFiles      = 100_000
	defaultSourceSnapshotMaxEntries    = 200_000
	defaultSourceSnapshotMaxPathBytes  = 64 * 1024 * 1024
	defaultSourceSnapshotMaxFileBytes  = 16 * 1024 * 1024
	defaultSourceSnapshotMaxTotalBytes = 512 * 1024 * 1024
)

// SourceSnapshotLimits bounds a complete Projection source observation.
// Zero values select the documented defaults; negative values are invalid.
type SourceSnapshotLimits struct {
	MaxFiles      int
	MaxEntries    int
	MaxPathBytes  int64
	MaxFileBytes  int64
	MaxTotalBytes int64
}

// ProjectionSourceOptions selects the exact authoritative inputs visible to
// Projection v1. Review Artifacts are excluded unless their canonical paths
// are explicitly listed in ApprovedArtifactPaths.
type ProjectionSourceOptions struct {
	Limits                SourceSnapshotLimits
	ApprovedArtifactPaths []string
	Profile               string
}

// SourceDocument is one immutable, bounded input copied from workspace truth.
type SourceDocument struct {
	ID           string
	Path         string
	RevisionHash string
	Profile      string
	Kind         string
	Size         int64
	Content      []byte
}

// ProjectionSourceSnapshot is a deterministic observation used as the
// compare-and-swap boundary for a Projection rebuild.
type ProjectionSourceSnapshot struct {
	Workspace string
	Hash      string
	Documents []SourceDocument
}

// SourceSnapshotError locates a bounded read or authority-selection failure.
type SourceSnapshotError struct {
	Path    string
	Field   string
	Value   any
	Message string
	Err     error
}

func (err *SourceSnapshotError) Error() string {
	if err == nil {
		return "<nil>"
	}
	message := err.Message
	if message == "" {
		message = "Projection source snapshot failed"
	}
	return fmt.Sprintf("projection source snapshot path=%q field=%s value=%v: %s", err.Path, err.Field, err.Value, message)
}

func (err *SourceSnapshotError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// BuildProjectionSourceSnapshot copies a canonical, workspace-root-bound,
// finite set of authoritative text records. It never writes the workspace.
func BuildProjectionSourceSnapshot(ctx context.Context, workspacePath string, options ProjectionSourceOptions) (ProjectionSourceSnapshot, error) {
	if ctx == nil {
		return ProjectionSourceSnapshot{}, &SourceSnapshotError{Field: "context", Value: "nil", Message: "context is required"}
	}
	if err := ctx.Err(); err != nil {
		return ProjectionSourceSnapshot{}, err
	}
	canonical, err := canonicalWorkspace(workspacePath)
	if err != nil {
		return ProjectionSourceSnapshot{}, err
	}
	limits, err := EffectiveProjectionSourceLimits(options.Limits)
	if err != nil {
		return ProjectionSourceSnapshot{}, err
	}
	profile := strings.TrimSpace(options.Profile)
	if profile == "" {
		profile = ProjectionProfileWorkspace
	}
	if strings.ContainsRune(profile, '\x00') {
		return ProjectionSourceSnapshot{}, &SourceSnapshotError{Field: "profile", Value: options.Profile, Message: "profile cannot contain NUL"}
	}
	approved, err := normalizeApprovedArtifactPaths(options.ApprovedArtifactPaths)
	if err != nil {
		return ProjectionSourceSnapshot{}, err
	}
	resolution, err := workspacepath.ResolveRoots(canonical)
	if err != nil {
		return ProjectionSourceSnapshot{}, &SourceSnapshotError{Path: canonical, Field: "active_root", Value: resolution.ActiveRoot, Message: "active workspace data root cannot be pinned", Err: err}
	}

	documents, seenApproved, err := collectProjectionSourceDocuments(ctx, canonical, resolution.ActiveRoot, profile, approved, limits)
	if err != nil {
		return ProjectionSourceSnapshot{}, err
	}
	for path := range approved {
		if _, ok := seenApproved[path]; !ok {
			return ProjectionSourceSnapshot{}, &SourceSnapshotError{Path: path, Field: "approved_artifact_paths", Value: path, Message: "approved Artifact is missing or is not an eligible bounded text source"}
		}
	}

	sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	return ProjectionSourceSnapshot{
		Workspace: canonical,
		Hash:      ProjectionSourceFingerprint(documents),
		Documents: documents,
	}, nil
}

// EffectiveProjectionSourceLimits validates configured limits and fills zero
// values with the finite Projection snapshot defaults.
func EffectiveProjectionSourceLimits(input SourceSnapshotLimits) (SourceSnapshotLimits, error) {
	if input.MaxFiles < 0 || input.MaxEntries < 0 || input.MaxPathBytes < 0 || input.MaxFileBytes < 0 || input.MaxTotalBytes < 0 {
		return SourceSnapshotLimits{}, &SourceSnapshotError{Field: "limits", Value: input, Message: "source snapshot limits cannot be negative"}
	}
	if input.MaxFiles == 0 {
		input.MaxFiles = defaultSourceSnapshotMaxFiles
	}
	if input.MaxEntries == 0 {
		input.MaxEntries = defaultSourceSnapshotMaxEntries
	}
	if input.MaxPathBytes == 0 {
		input.MaxPathBytes = defaultSourceSnapshotMaxPathBytes
	}
	if input.MaxFileBytes == 0 {
		input.MaxFileBytes = defaultSourceSnapshotMaxFileBytes
	}
	if input.MaxTotalBytes == 0 {
		input.MaxTotalBytes = defaultSourceSnapshotMaxTotalBytes
	}
	return input, nil
}

func normalizeApprovedArtifactPaths(paths []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		normalized, err := ValidateRelativePath(raw, PathOptions{Intent: PathIntentExisting})
		if err != nil {
			return nil, err
		}
		classification, err := ClassifyPath(normalized)
		if err != nil {
			return nil, err
		}
		if classification.Category != CategoryReviewArtifact {
			return nil, &SourceSnapshotError{Path: normalized, Field: "approved_artifact_paths", Value: normalized, Message: "only canonical review-Artifact paths can be approved Projection inputs"}
		}
		result[normalized] = struct{}{}
	}
	return result, nil
}

func skipProjectionSourceDirectory(relative, activeRoot string) (bool, error) {
	if relative == workspacepath.DataDirName || relative == workspacepath.LegacyDataDirName {
		return relative != activeRoot, nil
	}
	if relative == activeRoot+"/quality" || relative == activeRoot+"/automations" {
		return false, nil
	}
	classification, err := ClassifyPath(relative)
	if err != nil {
		return false, err
	}
	switch classification.Category {
	case CategoryFormalAuthoritative, CategoryReviewArtifact:
		return false, nil
	case CategoryRuntimeRecovery, CategoryRebuildableProjection, CategoryProtectedLegacyUnknown:
		return true, nil
	}
	return true, &SourceSnapshotError{Path: relative, Field: "classification.category", Value: classification.Category, Message: "unknown Workspace Schema category"}
}

func projectionTextPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".txt", ".json", ".jsonl", ".toml", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func projectionSourceKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return "markdown"
	case ".txt":
		return "text"
	case ".json":
		return "json"
	case ".jsonl":
		return "jsonl"
	case ".toml", ".yaml", ".yml":
		return "configuration"
	}
	return "text"
}

func readProjectionSourceFile(root *os.Root, workspace string, canonicalBefore CanonicalPath, relative string, expected os.FileInfo, maxBytes int64) ([]byte, error) {
	file, opened, err := openBoundRegularFile(root, relative, expected)
	if err != nil {
		return nil, err
	}
	content, readErr := io.ReadAll(io.LimitReader(file, maxBytes+1))
	after, statErr := file.Stat()
	closeErr := file.Close()
	if readErr != nil {
		return nil, &SourceSnapshotError{Path: relative, Field: "source.content", Value: relative, Message: "source read failed", Err: readErr}
	}
	if statErr != nil {
		return nil, &SourceSnapshotError{Path: relative, Field: "source.identity", Value: relative, Message: "source identity cannot be rechecked", Err: statErr}
	}
	if closeErr != nil {
		return nil, &SourceSnapshotError{Path: relative, Field: "source.handle", Value: relative, Message: "source close failed", Err: closeErr}
	}
	if int64(len(content)) > maxBytes {
		return nil, sourceLimitError(relative, "limits.max_file_bytes", len(content), maxBytes)
	}
	current, err := root.Stat(filepath.FromSlash(relative))
	if err != nil {
		return nil, &PathError{Code: CodePathIdentityChanged, Path: relative, Field: "source.identity", Value: relative, Message: "source path changed after the bound read", Err: err}
	}
	canonicalAfter, err := ResolveCanonicalPath(workspace, relative, CanonicalOptions{})
	if err != nil {
		return nil, err
	}
	identityChanged := !os.SameFile(opened, after) || !os.SameFile(opened, current) ||
		opened.Size() != after.Size() || opened.ModTime() != after.ModTime() || opened.Mode() != after.Mode() ||
		int64(len(content)) != after.Size() || canonicalBefore.Absolute != canonicalAfter.Absolute
	if identityChanged {
		return nil, &PathError{Code: CodePathIdentityChanged, Path: relative, Field: "source.identity", Value: relative, Message: "source identity changed during the bound read"}
	}
	return content, nil
}

func sourceLimitError(path, field string, observed, limit any) error {
	return &SourceSnapshotError{Path: path, Field: field, Value: map[string]any{"observed": observed, "limit": limit}, Message: "Projection source snapshot limit exceeded"}
}

func projectionSourceDocumentID(path string) string {
	return "doc-" + sourceSHA256([]byte(path))
}

func sourceSHA256(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

// ProjectionSourceFingerprint returns the deterministic identity of an ordered
// source-document set. Projection recovery uses the same algorithm to prove
// that persisted rows match the metadata fingerprint before comparing them to
// current workspace truth.
func ProjectionSourceFingerprint(documents []SourceDocument) string {
	documents = append([]SourceDocument(nil), documents...)
	sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	hasher := sha256.New()
	var length [8]byte
	writeField := func(value string) {
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hasher.Write(length[:])
		_, _ = io.WriteString(hasher, value)
	}
	for _, document := range documents {
		writeField(document.ID)
		writeField(document.Path)
		writeField(document.RevisionHash)
		writeField(document.Profile)
		writeField(document.Kind)
		writeField(fmt.Sprintf("%d", document.Size))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}
