package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"denova/internal/buildinfo"
	qualityworkspace "denova/internal/quality/workspace"
)

const (
	maxQualityPublicIssues     = 100
	defaultQualityPreviewLimit = 100
	maxQualityPreviewLimit     = 500
)

type QualityTruncation struct {
	Total     int  `json:"total"`
	Returned  int  `json:"returned"`
	Truncated bool `json:"truncated"`
}

type QualityFeature struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Required bool   `json:"required"`
}

type QualityMarker struct {
	Present       bool             `json:"present"`
	SchemaVersion int              `json:"schema_version,omitempty"`
	Features      []QualityFeature `json:"features"`
}

type QualityIssue struct {
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Field    string `json:"field,omitempty"`
	Blocking bool   `json:"blocking"`
}

type QualityProjectDTO struct {
	ResourceID      string                                 `json:"resource_id"`
	ActiveRoot      string                                 `json:"active_root,omitempty"`
	Mode            qualityworkspace.CompatibilityMode     `json:"mode"`
	ManagedMutation qualityworkspace.ManagedMutationAccess `json:"managed_mutation"`
	Marker          QualityMarker                          `json:"marker"`
	Issues          []QualityIssue                         `json:"issues"`
	IssueTruncation QualityTruncation                      `json:"issue_truncation"`
	UnknownOptional []string                               `json:"unknown_optional_features"`
	LegacyConflicts []string                               `json:"legacy_conflicts"`
}

type QualityMigrationPreviewRequest struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

type QualityPage struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

type QualityPreviewTotals struct {
	Entries    int `json:"entries"`
	Operations int `json:"operations"`
	Conflicts  int `json:"conflicts"`
}

type QualityPreviewEntry struct {
	Source              string `json:"source"`
	Destination         string `json:"destination"`
	NodeType            string `json:"node_type"`
	SourceCategory      string `json:"source_category"`
	DestinationCategory string `json:"destination_category"`
	Size                int64  `json:"size"`
	SHA256              string `json:"sha256"`
}

type QualityPreviewOperation struct {
	Kind        string `json:"kind"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination"`
}

type QualityPreviewConflict struct {
	Code        string `json:"code"`
	Path        string `json:"path,omitempty"`
	Destination string `json:"destination,omitempty"`
	Field       string `json:"field,omitempty"`
}

type QualityPageResult[T any] struct {
	Items     []T  `json:"items"`
	Total     int  `json:"total"`
	Truncated bool `json:"truncated"`
}

type QualityMigrationPreviewDTO struct {
	ResourceID           string                                     `json:"resource_id"`
	Digest               string                                     `json:"digest"`
	WorkspaceKind        qualityworkspace.WorkspaceKind             `json:"workspace_kind"`
	SourceRoot           string                                     `json:"source_root,omitempty"`
	TargetRoot           string                                     `json:"target_root"`
	CurrentSchemaVersion int                                        `json:"current_schema_version"`
	TargetSchemaVersion  int                                        `json:"target_schema_version"`
	Features             []QualityFeature                           `json:"features"`
	Compatibility        QualityProjectDTO                          `json:"compatibility"`
	Totals               QualityPreviewTotals                       `json:"totals"`
	Page                 QualityPage                                `json:"page"`
	Entries              QualityPageResult[QualityPreviewEntry]     `json:"entries"`
	Operations           QualityPageResult[QualityPreviewOperation] `json:"operations"`
	Conflicts            QualityPageResult[QualityPreviewConflict]  `json:"conflicts"`
}

func (a *App) QualityProject() (QualityProjectDTO, error) {
	workspacePath := a.Workspace()
	if workspacePath == "" {
		return QualityProjectDTO{}, &QualityAppError{Code: QualityCodeNoWorkspace}
	}
	inspector, err := qualityworkspace.NewInspector(qualityInspectorOptions())
	if err != nil {
		return QualityProjectDTO{}, &QualityAppError{Code: QualityCodeInspectionFailed, Cause: err}
	}
	inspection, err := inspector.Inspect(workspacePath)
	if err != nil {
		return QualityProjectDTO{}, &QualityAppError{Code: QualityCodeInspectionFailed, Cause: err}
	}
	return qualityProjectProjection(inspection), nil
}

func qualityInspectorOptions() qualityworkspace.InspectorOptions {
	return qualityworkspace.InspectorOptions{ApplicationVersion: buildinfo.Version, SupportedFeatures: map[string]string{
		"quality_harness": ">=1.0.0 <2.0.0", "fts_projection": ">=1.0.0 <2.0.0",
	}}
}

func qualityTargetFeatures() map[string]qualityworkspace.FeatureContract {
	return map[string]qualityworkspace.FeatureContract{
		"quality_harness": {Version: "1.0.0", Required: true},
		"fts_projection":  {Version: "1.0.0", Required: false},
	}
}

func qualityProjectProjection(inspection qualityworkspace.Inspection) QualityProjectDTO {
	features := make([]QualityFeature, 0, len(inspection.Marker.Contract.Features))
	for id, feature := range inspection.Marker.Contract.Features {
		features = append(features, QualityFeature{ID: boundedQualityString(id), Version: boundedQualityString(feature.Version), Required: feature.Required})
	}
	sort.Slice(features, func(i, j int) bool { return features[i].ID < features[j].ID })
	orderedIssues := append([]qualityworkspace.CompatibilityIssue(nil), inspection.Issues...)
	sort.Slice(orderedIssues, func(i, j int) bool {
		if orderedIssues[i].Code != orderedIssues[j].Code {
			return orderedIssues[i].Code < orderedIssues[j].Code
		}
		if orderedIssues[i].Path != orderedIssues[j].Path {
			return orderedIssues[i].Path < orderedIssues[j].Path
		}
		return orderedIssues[i].Field < orderedIssues[j].Field
	})
	issueTotal := len(orderedIssues)
	issueLimit := issueTotal
	if issueLimit > maxQualityPublicIssues {
		issueLimit = maxQualityPublicIssues
	}
	issues := make([]QualityIssue, 0, issueLimit)
	for _, issue := range orderedIssues[:issueLimit] {
		issues = append(issues, QualityIssue{Code: boundedQualityString(string(issue.Code)), Path: safeQualityRelativePath(issue.Path), Field: boundedQualityString(issue.Field), Blocking: issue.Blocking})
	}
	return QualityProjectDTO{
		ResourceID: "current", ActiveRoot: safeQualityRelativePath(inspection.ActiveRoot), Mode: inspection.Mode, ManagedMutation: inspection.ManagedMutation,
		Marker: QualityMarker{Present: inspection.Marker.Present, SchemaVersion: inspection.Marker.Contract.SchemaVersion, Features: features},
		Issues: issues, IssueTruncation: QualityTruncation{Total: issueTotal, Returned: len(issues), Truncated: len(issues) < issueTotal},
		UnknownOptional: safeQualityStrings(inspection.UnknownOptionalFeatures), LegacyConflicts: safeQualityPaths(inspection.LegacyConflicts),
	}
}

func (a *App) QualityMigrationPreview(request QualityMigrationPreviewRequest) (QualityMigrationPreviewDTO, error) {
	workspacePath := a.Workspace()
	if workspacePath == "" {
		return QualityMigrationPreviewDTO{}, &QualityAppError{Code: QualityCodeNoWorkspace}
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultQualityPreviewLimit
	}
	if request.Offset < 0 || limit < 1 || limit > maxQualityPreviewLimit {
		return QualityMigrationPreviewDTO{}, &QualityAppError{Code: QualityCodeInvalidRequest}
	}
	preview, err := qualityworkspace.BuildMigrationPreview(workspacePath, qualityworkspace.PreviewOptions{Inspector: qualityInspectorOptions(), TargetFeatures: qualityTargetFeatures()})
	if err != nil {
		return QualityMigrationPreviewDTO{}, &QualityAppError{Code: QualityCodeInspectionFailed, Cause: err}
	}
	complete := projectQualityPreview(preview)
	digestPayload, err := json.Marshal(complete)
	if err != nil {
		return QualityMigrationPreviewDTO{}, &QualityAppError{Code: QualityCodeInspectionFailed, Cause: err}
	}
	digest := sha256.Sum256(digestPayload)
	return pageQualityPreview(complete, request.Offset, limit, hex.EncodeToString(digest[:])), nil
}

type completeQualityPreview struct {
	ResourceID           string                         `json:"resource_id"`
	WorkspaceKind        qualityworkspace.WorkspaceKind `json:"workspace_kind"`
	SourceRoot           string                         `json:"source_root,omitempty"`
	TargetRoot           string                         `json:"target_root"`
	CurrentSchemaVersion int                            `json:"current_schema_version"`
	TargetSchemaVersion  int                            `json:"target_schema_version"`
	Features             []QualityFeature               `json:"features"`
	Compatibility        QualityProjectDTO              `json:"compatibility"`
	Entries              []QualityPreviewEntry          `json:"entries"`
	Operations           []QualityPreviewOperation      `json:"operations"`
	Conflicts            []QualityPreviewConflict       `json:"conflicts"`
}

func projectQualityPreview(preview qualityworkspace.MigrationPreview) completeQualityPreview {
	result := completeQualityPreview{ResourceID: "current", WorkspaceKind: preview.Kind, SourceRoot: safeQualityRelativePath(preview.SourceRoot), TargetRoot: safeQualityRelativePath(preview.TargetRoot), CurrentSchemaVersion: preview.CurrentSchemaVersion, TargetSchemaVersion: preview.TargetSchemaVersion, Compatibility: qualityProjectProjection(preview.Compatibility)}
	for _, feature := range preview.Features {
		result.Features = append(result.Features, QualityFeature{ID: boundedQualityString(feature.ID), Version: boundedQualityString(feature.Version), Required: feature.Required})
	}
	for _, entry := range preview.Entries {
		result.Entries = append(result.Entries, QualityPreviewEntry{Source: safeQualityRelativePath(entry.Source), Destination: safeQualityRelativePath(entry.Destination), NodeType: string(entry.NodeType), SourceCategory: string(entry.SourceCategory), DestinationCategory: string(entry.DestinationCategory), Size: entry.Size, SHA256: entry.SHA256})
	}
	for _, operation := range preview.Operations {
		result.Operations = append(result.Operations, QualityPreviewOperation{Kind: string(operation.Kind), Source: safeQualityRelativePath(operation.Source), Destination: safeQualityRelativePath(operation.Destination)})
	}
	for _, conflict := range preview.Conflicts {
		result.Conflicts = append(result.Conflicts, QualityPreviewConflict{Code: string(conflict.Code), Path: safeQualityRelativePath(conflict.Path), Destination: safeQualityRelativePath(conflict.Destination), Field: boundedQualityString(conflict.Field)})
	}
	return result
}

func pageQualityPreview(complete completeQualityPreview, offset, limit int, digest string) QualityMigrationPreviewDTO {
	return QualityMigrationPreviewDTO{
		ResourceID: complete.ResourceID, Digest: digest, WorkspaceKind: complete.WorkspaceKind, SourceRoot: complete.SourceRoot, TargetRoot: complete.TargetRoot, CurrentSchemaVersion: complete.CurrentSchemaVersion, TargetSchemaVersion: complete.TargetSchemaVersion, Features: append([]QualityFeature(nil), complete.Features...), Compatibility: complete.Compatibility,
		Totals: QualityPreviewTotals{Entries: len(complete.Entries), Operations: len(complete.Operations), Conflicts: len(complete.Conflicts)}, Page: QualityPage{Offset: offset, Limit: limit},
		Entries: pageQualityItems(complete.Entries, offset, limit), Operations: pageQualityItems(complete.Operations, offset, limit), Conflicts: pageQualityItems(complete.Conflicts, offset, limit),
	}
}

func pageQualityItems[T any](items []T, offset, limit int) QualityPageResult[T] {
	start := offset
	if start > len(items) {
		start = len(items)
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := append([]T(nil), items[start:end]...)
	return QualityPageResult[T]{Items: page, Total: len(items), Truncated: start > 0 || end < len(items)}
}

func safeQualityPaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if safe := safeQualityRelativePath(path); safe != "" {
			result = append(result, safe)
		}
	}
	sort.Strings(result)
	return result
}

func safeQualityStrings(values []string) []string {
	result := make([]string, len(values))
	for index := range values {
		result[index] = boundedQualityString(values[index])
	}
	sort.Strings(result)
	return result
}

func safeQualityRelativePath(path string) string {
	if path == "" || strings.TrimSpace(path) != path || strings.ContainsRune(path, '\x00') {
		return ""
	}
	normalized := strings.ReplaceAll(path, `\`, "/")
	if normalized != path || strings.HasPrefix(normalized, "/") ||
		(len(normalized) >= 2 && normalized[1] == ':' &&
			((normalized[0] >= 'a' && normalized[0] <= 'z') || (normalized[0] >= 'A' && normalized[0] <= 'Z'))) {
		return ""
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return ""
		}
	}
	return boundedQualityString(normalized)
}
