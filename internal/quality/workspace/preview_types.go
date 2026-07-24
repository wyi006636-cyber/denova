package workspace

import "fmt"

// WorkspaceKind is the source-layout shape observed by a dry-run preview.
type WorkspaceKind string

const (
	WorkspaceKindNew     WorkspaceKind = "new"
	WorkspaceKindCurrent WorkspaceKind = "current_denova"
	WorkspaceKindLegacy  WorkspaceKind = "legacy_nova"
)

// PreviewNodeType identifies the byte-bearing source node in a manifest.
type PreviewNodeType string

const (
	PreviewNodeFile    PreviewNodeType = "file"
	PreviewNodeSymlink PreviewNodeType = "symlink"
	PreviewNodeOther   PreviewNodeType = "other"
)

// VersionPolicyChange makes inclusion changes explicit for every source and
// destination pair.
type VersionPolicyChange string

const (
	VersionPolicyUnchanged        VersionPolicyChange = "unchanged"
	VersionPolicyIncludeToExclude VersionPolicyChange = "include_to_exclude"
	VersionPolicyExcludeToInclude VersionPolicyChange = "exclude_to_include"
)

// PreviewOperationKind is descriptive only. P1-T02A deliberately exposes no
// executor, durable intent, stage, switch, receipt, resume, or rollback token.
type PreviewOperationKind string

const (
	OperationPreserve          PreviewOperationKind = "preserve"
	OperationCopyToCurrentRoot PreviewOperationKind = "copy_to_current_root"
	OperationCreateMarker      PreviewOperationKind = "create_marker"
)

// PreviewFeature is a deterministically ordered target feature declaration.
type PreviewFeature struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Required bool   `json:"required"`
}

// PreviewEntry records immutable facts about one existing source node and its
// proposed logical destination.
type PreviewEntry struct {
	Source              string              `json:"source"`
	Destination         string              `json:"destination"`
	NodeType            PreviewNodeType     `json:"node_type"`
	SourceCategory      Category            `json:"source_category"`
	DestinationCategory Category            `json:"destination_category"`
	Size                int64               `json:"size"`
	SHA256              string              `json:"sha256"`
	VersionBefore       VersionDisposition  `json:"version_before"`
	VersionAfter        VersionDisposition  `json:"version_after"`
	VersionChange       VersionPolicyChange `json:"version_change"`
}

// PreviewOperation describes a future intent without carrying executable
// state or authorizing any mutation.
type PreviewOperation struct {
	Kind        PreviewOperationKind `json:"kind"`
	Source      string               `json:"source,omitempty"`
	Destination string               `json:"destination"`
	Reason      string               `json:"reason"`
}

// PreviewConflict is a deterministic, structured preflight blocker.
type PreviewConflict struct {
	Code        ErrorCode `json:"code"`
	Path        string    `json:"path"`
	Destination string    `json:"destination,omitempty"`
	Field       string    `json:"field"`
	Value       any       `json:"value"`
	Message     string    `json:"message"`
}

// PreviewOptions contains only read-time capability and destination
// representation policy. TargetFeatures are exact versions for a possible
// future marker; no marker is generated or written by this package.
type PreviewOptions struct {
	Inspector      InspectorOptions
	TargetFeatures map[string]FeatureContract
	TargetPlatform PathPlatform
	TargetLimits   PathLimits
}

// MigrationPreview is a deterministic dry-run manifest. snapshot is an
// internal verification seal over source paths and bytes, not durable intent.
type MigrationPreview struct {
	Workspace            string             `json:"workspace"`
	Kind                 WorkspaceKind      `json:"workspace_kind"`
	SourceRoot           string             `json:"source_root"`
	TargetRoot           string             `json:"target_root"`
	CurrentSchemaVersion int                `json:"current_schema_version"`
	TargetSchemaVersion  int                `json:"target_schema_version"`
	Features             []PreviewFeature   `json:"features"`
	Entries              []PreviewEntry     `json:"entries"`
	Operations           []PreviewOperation `json:"operations"`
	Conflicts            []PreviewConflict  `json:"conflicts"`
	Compatibility        Inspection         `json:"compatibility"`
	snapshot             []previewSnapshot
}

// HasConflicts reports whether a future migration/adoption would require an
// explicit resolution before P1-T02B could act.
func (preview MigrationPreview) HasConflicts() bool {
	return len(preview.Conflicts) != 0
}

// RequireConflictFree returns all dry-run blockers. It never executes the plan.
func (preview MigrationPreview) RequireConflictFree() error {
	if !preview.HasConflicts() {
		return nil
	}
	return &PreviewConflictError{Conflicts: append([]PreviewConflict(nil), preview.Conflicts...)}
}

// PreviewConflictError preserves the complete conflict manifest.
type PreviewConflictError struct {
	Conflicts []PreviewConflict
}

func (err *PreviewConflictError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if len(err.Conflicts) == 0 {
		return "migration preview contains conflicts"
	}
	conflict := err.Conflicts[0]
	return fmt.Sprintf("migration preview conflict: code=%s path=%q destination=%q field=%s value=%v", conflict.Code, conflict.Path, conflict.Destination, conflict.Field, conflict.Value)
}

// PreviewStaleError means source identity changed after the preview snapshot.
type PreviewStaleError struct {
	Code     ErrorCode
	Path     string
	Expected any
	Actual   any
	Message  string
}

func (err *PreviewStaleError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return fmt.Sprintf("migration preview stale: code=%s path=%q expected=%v actual=%v: %s", err.Code, err.Path, err.Expected, err.Actual, err.Message)
}

type previewSnapshot struct {
	Path     string
	NodeType string
	Size     int64
	SHA256   string
}
