package workspace

import "fmt"

// ErrorCode identifies a stable Workspace Schema v1 inspection failure.
type ErrorCode string

const (
	CodeWorkspaceInvalid              ErrorCode = "workspace_invalid"
	CodeWorkspaceRead                 ErrorCode = "workspace_read"
	CodeMarkerMissing                 ErrorCode = "marker_missing"
	CodeMarkerMalformed               ErrorCode = "marker_malformed"
	CodeMarkerTooLarge                ErrorCode = "marker_too_large"
	CodeMarkerFieldMissing            ErrorCode = "marker_field_missing"
	CodeMarkerFieldMismatch           ErrorCode = "marker_field_mismatch"
	CodeSchemaNewer                   ErrorCode = "schema_newer"
	CodeSchemaUnsupported             ErrorCode = "schema_unsupported"
	CodeWriterRangeMismatch           ErrorCode = "writer_range_mismatch"
	CodeWriterVersionInvalid          ErrorCode = "writer_version_invalid"
	CodeWriterVersionUnsupported      ErrorCode = "writer_version_unsupported"
	CodeApplicationVersionInvalid     ErrorCode = "application_version_invalid"
	CodeApplicationVersionUnsupported ErrorCode = "application_version_unsupported"
	CodeFeatureMalformed              ErrorCode = "feature_malformed"
	CodeFeatureRequiredUnsupported    ErrorCode = "feature_required_unsupported"
	CodeFeatureOptionalUnknown        ErrorCode = "feature_optional_unknown"
	CodeMigrationStateInvalid         ErrorCode = "migration_state_invalid"
	CodeMigrationStateIncomplete      ErrorCode = "migration_state_incomplete"
	CodeActiveRootUnsupported         ErrorCode = "active_root_unsupported"
	CodeRootResolutionDivergence      ErrorCode = "root_resolution_divergence"
	CodeRootResolutionUnsafe          ErrorCode = "root_resolution_unsafe"
	CodeLegacyV1Conflict              ErrorCode = "legacy_v1_conflict"
	CodePreviewReparsePoint           ErrorCode = "preview_reparse_point"
	CodePreviewPortableCollision      ErrorCode = "preview_portable_collision"
	CodePreviewDestinationCollision   ErrorCode = "preview_destination_collision"
	CodePreviewUnsupportedNode        ErrorCode = "preview_unsupported_node"
	CodePreviewSourceChanged          ErrorCode = "preview_source_changed"
	CodePreviewTreeChanged            ErrorCode = "preview_tree_changed"
	CodePreviewInspectionChanged      ErrorCode = "preview_inspection_changed"
	CodePathEmpty                     ErrorCode = "path_empty"
	CodePathAbsolute                  ErrorCode = "path_absolute"
	CodePathDrive                     ErrorCode = "path_drive"
	CodePathUNC                       ErrorCode = "path_unc"
	CodePathSegment                   ErrorCode = "path_empty_segment"
	CodePathDotSegment                ErrorCode = "path_dot_segment"
	CodePathParentSegment             ErrorCode = "path_parent_segment"
	CodePathNUL                       ErrorCode = "path_nul"
	CodePathSeparator                 ErrorCode = "path_separator"
	CodePathNormalization             ErrorCode = "path_normalization"
	CodePathWindowsReserved           ErrorCode = "path_windows_reserved"
	CodePathWindowsTrailing           ErrorCode = "path_windows_trailing"
	CodePathWindowsADS                ErrorCode = "path_windows_ads"
	CodePathWindowsCharacter          ErrorCode = "path_windows_character"
	CodePathTooLong                   ErrorCode = "path_too_long"
	CodePathCanonical                 ErrorCode = "path_canonical"
	CodePathEscape                    ErrorCode = "path_escape"
	CodePathSymlinkLoop               ErrorCode = "path_symlink_loop"
	CodePathIdentityChanged           ErrorCode = "path_identity_changed"
)

// InspectionError means the read-only adapter could not safely inspect the
// workspace itself. Compatibility blockers inside an otherwise readable
// workspace are returned in Inspection.Issues instead.
type InspectionError struct {
	Code    ErrorCode
	Path    string
	Field   string
	Value   any
	Message string
	Err     error
}

func (err *InspectionError) Error() string {
	if err == nil {
		return "<nil>"
	}
	message := err.Message
	if message == "" {
		message = string(err.Code)
	}
	return fmt.Sprintf("workspace inspection %s at %s path=%q value=%v: %s", err.Code, err.Field, err.Path, err.Value, message)
}

func (err *InspectionError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// PathError retains the exact path and offending value for safe diagnostics.
type PathError struct {
	Code    ErrorCode
	Path    string
	Field   string
	Value   any
	Message string
	Err     error
}

func (err *PathError) Error() string {
	if err == nil {
		return "<nil>"
	}
	message := err.Message
	if message == "" {
		message = string(err.Code)
	}
	if err.Field != "" {
		return fmt.Sprintf("workspace path %s at %s path=%q value=%v: %s", err.Code, err.Field, err.Path, err.Value, message)
	}
	return fmt.Sprintf("workspace path %s path=%q value=%v: %s", err.Code, err.Path, err.Value, message)
}

func (err *PathError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}
