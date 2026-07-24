package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"denova/internal/workspacepath"
	"github.com/Masterminds/semver/v3"
)

// BuildMigrationPreview produces a complete read-only manifest and never
// creates the marker, destination root, backup, stage, intent, or receipt.
func BuildMigrationPreview(workspace string, options PreviewOptions) (MigrationPreview, error) {
	inspector, err := NewInspector(options.Inspector)
	if err != nil {
		return MigrationPreview{}, err
	}
	inspection, err := inspector.Inspect(workspace)
	if err != nil {
		return MigrationPreview{}, err
	}
	nodes, scanConflicts, err := scanWorkspaceNodes(inspection.Workspace)
	if err != nil {
		return MigrationPreview{}, err
	}
	secondNodes, secondScanConflicts, err := scanWorkspaceNodes(inspection.Workspace)
	if err != nil {
		return MigrationPreview{}, err
	}
	if conflict := changedObservationConflict(previewSnapshots(nodes), previewSnapshots(secondNodes)); conflict != nil {
		scanConflicts = append(scanConflicts, *conflict)
	}
	scanConflicts = append(scanConflicts, secondScanConflicts...)
	nodes = secondNodes
	secondInspection, err := inspector.Inspect(inspection.Workspace)
	if err != nil {
		return MigrationPreview{}, err
	}
	inspectionChanged := !reflect.DeepEqual(inspection, secondInspection)
	inspection = secondInspection
	kind, sourceRoot, err := detectWorkspaceKind(inspection.Workspace, inspection.ActiveRoot)
	if err != nil {
		return MigrationPreview{}, err
	}

	conflicts := append([]PreviewConflict(nil), scanConflicts...)
	if inspectionChanged {
		conflicts = append(conflicts, PreviewConflict{
			Code:    CodePreviewInspectionChanged,
			Path:    MarkerRelativePath,
			Field:   "inspection.identity",
			Value:   "root, marker, or compatibility changed between read-only observations",
			Message: "workspace inspection changed during preview",
		})
	}
	if conflict := markerSnapshotConflict(inspection, nodes); conflict != nil {
		conflicts = append(conflicts, *conflict)
	}
	conflicts = append(conflicts, compatibilityPreviewConflicts(inspection, kind)...)
	features, featureConflicts := targetPreviewFeatures(inspection, inspector, options.TargetFeatures)
	conflicts = append(conflicts, featureConflicts...)
	if !inspection.Marker.Present {
		conflicts = append(conflicts, plannedWriterPreviewConflicts(inspector)...)
	}

	entries := make([]PreviewEntry, 0)
	operations := make([]PreviewOperation, 0)
	destinationSources := make(map[string][]string)
	destinations := make([]string, 0, len(nodes)+1)
	for _, node := range nodes {
		if node.Path == ".git" {
			continue
		}
		destination := previewDestination(node.Path, kind)
		destinations = append(destinations, destination)
		destinationSources[destination] = append(destinationSources[destination], node.Path)
		conflicts = append(conflicts, validatePreviewDestination(inspection.Workspace, node.Path, destination, options)...)
		if node.NodeType == "directory" {
			continue
		}

		sourceClassification, sourceErr := ClassifyPath(node.Path)
		if sourceErr != nil {
			conflicts = append(conflicts, previewConflictFromError(sourceErr, node.Path, destination))
		}
		destinationClassification, destinationErr := ClassifyPath(destination)
		if destinationErr != nil {
			conflicts = append(conflicts, previewConflictFromError(destinationErr, node.Path, destination))
		}
		entry := PreviewEntry{
			Source:              node.Path,
			Destination:         destination,
			NodeType:            PreviewNodeType(node.NodeType),
			SourceCategory:      sourceClassification.Category,
			DestinationCategory: destinationClassification.Category,
			Size:                node.Size,
			SHA256:              node.SHA256,
			VersionBefore:       sourceClassification.VersionDisposition,
			VersionAfter:        destinationClassification.VersionDisposition,
			VersionChange:       previewVersionChange(sourceClassification.VersionDisposition, destinationClassification.VersionDisposition),
		}
		entries = append(entries, entry)
		operation := PreviewOperation{
			Kind:        OperationPreserve,
			Source:      node.Path,
			Destination: destination,
			Reason:      "preserve existing workspace bytes",
		}
		if kind == WorkspaceKindLegacy && destination != node.Path {
			operation.Kind = OperationCopyToCurrentRoot
			operation.Reason = "describe a future verified copy from the active legacy root"
		}
		operations = append(operations, operation)
	}

	if !inspection.Marker.Present {
		destination := MarkerRelativePath
		destinations = append(destinations, destination)
		destinationSources[destination] = append(destinationSources[destination], "<planned-marker>")
		conflicts = append(conflicts, validatePreviewDestination(inspection.Workspace, "", destination, options)...)
		operations = append(operations, PreviewOperation{
			Kind:        OperationCreateMarker,
			Destination: destination,
			Reason:      "describe future explicit Workspace Schema v1 adoption or migration",
		})
	}

	conflicts = append(conflicts, exactDestinationConflicts(destinationSources)...)
	conflicts = append(conflicts, portablePreviewConflicts(destinations)...)

	sort.Slice(entries, func(i, j int) bool { return entries[i].Source < entries[j].Source })
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Destination != operations[j].Destination {
			return operations[i].Destination < operations[j].Destination
		}
		if operations[i].Source != operations[j].Source {
			return operations[i].Source < operations[j].Source
		}
		return operations[i].Kind < operations[j].Kind
	})
	conflicts = uniquePreviewConflicts(conflicts)
	currentSchema := 0
	if inspection.Marker.Present {
		currentSchema = inspection.Marker.Contract.SchemaVersion
	}
	return MigrationPreview{
		Workspace:            inspection.Workspace,
		Kind:                 kind,
		SourceRoot:           sourceRoot,
		TargetRoot:           workspacepath.DataDirName,
		CurrentSchemaVersion: currentSchema,
		TargetSchemaVersion:  1,
		Features:             features,
		Entries:              entries,
		Operations:           operations,
		Conflicts:            conflicts,
		Compatibility:        inspection,
		snapshot:             previewSnapshots(nodes),
	}, nil
}

func detectWorkspaceKind(workspace, activeRoot string) (WorkspaceKind, string, error) {
	currentExists, err := pathEntryExists(filepath.Join(workspace, workspacepath.DataDirName))
	if err != nil {
		return "", "", err
	}
	legacyExists, err := pathEntryExists(filepath.Join(workspace, workspacepath.LegacyDataDirName))
	if err != nil {
		return "", "", err
	}
	if !currentExists && !legacyExists {
		return WorkspaceKindNew, "", nil
	}
	if activeRoot == workspacepath.LegacyDataDirName {
		return WorkspaceKindLegacy, workspacepath.LegacyDataDirName, nil
	}
	return WorkspaceKindCurrent, workspacepath.DataDirName, nil
}

func pathEntryExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, &InspectionError{Code: CodeWorkspaceRead, Path: path, Field: "workspace_kind", Value: path, Message: "workspace root presence cannot be inspected", Err: err}
}

func compatibilityPreviewConflicts(inspection Inspection, kind WorkspaceKind) []PreviewConflict {
	conflicts := make([]PreviewConflict, 0)
	for _, issue := range inspection.Issues {
		if !issue.Blocking || issue.Code == CodeMarkerMissing {
			continue
		}
		if issue.Code == CodeActiveRootUnsupported && kind == WorkspaceKindLegacy {
			continue
		}
		conflicts = append(conflicts, PreviewConflict{
			Code:    issue.Code,
			Path:    issue.Path,
			Field:   issue.Field,
			Value:   issue.Value,
			Message: issue.Message,
		})
	}
	return conflicts
}

func targetPreviewFeatures(inspection Inspection, inspector *Inspector, configured map[string]FeatureContract) ([]PreviewFeature, []PreviewConflict) {
	source := configured
	validateTarget := true
	if inspection.Marker.Present {
		source = inspection.Marker.Contract.Features
		validateTarget = false
	}
	ids := make([]string, 0, len(source))
	for id := range source {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	features := make([]PreviewFeature, 0, len(ids))
	conflicts := make([]PreviewConflict, 0)
	for _, id := range ids {
		feature := source[id]
		features = append(features, PreviewFeature{ID: id, Version: feature.Version, Required: feature.Required})
		if !validateTarget {
			continue
		}
		version, err := semver.StrictNewVersion(feature.Version)
		if err != nil {
			conflicts = append(conflicts, PreviewConflict{Code: CodeFeatureMalformed, Path: MarkerRelativePath, Field: "features." + id + ".version", Value: feature.Version, Message: "planned feature version must be strict SemVer"})
			continue
		}
		support, known := inspector.supportedFeatures[id]
		if !known || !support.constraint.Check(version) {
			conflicts = append(conflicts, PreviewConflict{Code: CodeFeatureRequiredUnsupported, Path: MarkerRelativePath, Field: "features." + id, Value: feature.Version, Message: "preview cannot plan a marker feature outside local support"})
		}
	}
	return features, conflicts
}

func previewDestination(source string, kind WorkspaceKind) string {
	if kind == WorkspaceKindLegacy && strings.HasPrefix(source, workspacepath.LegacyDataDirName+"/") {
		if isLegacyV1LookingPath(source) {
			return source
		}
		return workspacepath.DataDirName + strings.TrimPrefix(source, workspacepath.LegacyDataDirName)
	}
	return source
}

func validatePreviewDestination(workspace, source, destination string, options PreviewOptions) []PreviewConflict {
	conflicts := make([]PreviewConflict, 0, 2)
	intent := PathIntentNew
	if source != "" && source == destination {
		intent = PathIntentExisting
	}
	normalized, err := ValidateRelativePath(destination, PathOptions{
		Intent:   intent,
		Platform: options.TargetPlatform,
		Limits:   options.TargetLimits,
	})
	if err != nil {
		conflict := previewConflictFromError(err, source, destination)
		conflicts = append(conflicts, conflict)
		return conflicts
	}
	_, err = ResolveCanonicalPath(workspace, normalized, CanonicalOptions{AllowMissing: true})
	if err != nil {
		conflicts = append(conflicts, previewConflictFromError(err, source, destination))
	}
	return conflicts
}

func plannedWriterPreviewConflicts(inspector *Inspector) []PreviewConflict {
	if inspector.applicationErr != nil {
		value := inspector.applicationRaw
		if value == "" {
			value = "missing"
		}
		return []PreviewConflict{{
			Code:    CodeApplicationVersionInvalid,
			Path:    MarkerRelativePath,
			Field:   "application_version",
			Value:   value,
			Message: "a future v1 marker writer must be strict SemVer without a leading v",
		}}
	}
	if !inspector.writerRange.Check(inspector.applicationVersion) {
		return []PreviewConflict{{
			Code:    CodeApplicationVersionUnsupported,
			Path:    MarkerRelativePath,
			Field:   "application_version",
			Value:   inspector.applicationRaw,
			Message: "a future v1 marker writer must be inside the local v1 writer range",
		}}
	}
	return nil
}

func portablePreviewConflicts(destinations []string) []PreviewConflict {
	conflicts := make([]PreviewConflict, 0)
	for _, collision := range DetectPortablePathCollisions(destinations) {
		conflicts = append(conflicts, PreviewConflict{
			Code:    CodePreviewPortableCollision,
			Path:    collision.Paths[0],
			Field:   "destination.portable_key",
			Value:   collision,
			Message: "destination paths collide after NFC normalization or Unicode case-folding",
		})
	}
	sortPreviewConflicts(conflicts)
	return conflicts
}

func exactDestinationConflicts(destinationSources map[string][]string) []PreviewConflict {
	conflicts := make([]PreviewConflict, 0)
	for destination, sources := range destinationSources {
		if len(sources) < 2 {
			continue
		}
		sort.Strings(sources)
		conflicts = append(conflicts, PreviewConflict{
			Code:        CodePreviewDestinationCollision,
			Path:        sources[0],
			Destination: destination,
			Field:       "destination",
			Value:       append([]string(nil), sources...),
			Message:     "multiple source or planned records resolve to the same destination",
		})
	}
	return conflicts
}

func previewVersionChange(before, after VersionDisposition) VersionPolicyChange {
	switch {
	case before == after:
		return VersionPolicyUnchanged
	case before == VersionInclude && after == VersionExclude:
		return VersionPolicyIncludeToExclude
	case before == VersionExclude && after == VersionInclude:
		return VersionPolicyExcludeToInclude
	default:
		return VersionPolicyChange(fmt.Sprintf("%s_to_%s", before, after))
	}
}

func previewSnapshots(nodes []previewNode) []previewSnapshot {
	snapshots := make([]previewSnapshot, 0, len(nodes))
	for _, node := range nodes {
		snapshots = append(snapshots, previewSnapshot{Path: node.Path, NodeType: node.NodeType, Size: node.Size, SHA256: node.SHA256})
	}
	return snapshots
}

func changedObservationConflict(before, after []previewSnapshot) *PreviewConflict {
	beforeByPath := make(map[string]previewSnapshot, len(before))
	afterByPath := make(map[string]previewSnapshot, len(after))
	for _, snapshot := range before {
		beforeByPath[snapshot.Path] = snapshot
	}
	for _, snapshot := range after {
		afterByPath[snapshot.Path] = snapshot
	}
	paths := make([]string, 0, len(beforeByPath)+len(afterByPath))
	seen := make(map[string]struct{}, len(beforeByPath)+len(afterByPath))
	for path := range beforeByPath {
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for path := range afterByPath {
		if _, exists := seen[path]; !exists {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		first, firstExists := beforeByPath[path]
		second, secondExists := afterByPath[path]
		if firstExists && secondExists && first == second {
			continue
		}
		code := CodePreviewTreeChanged
		if firstExists && secondExists && (first.NodeType == string(PreviewNodeFile) || first.NodeType == string(PreviewNodeSymlink)) {
			code = CodePreviewSourceChanged
		}
		return &PreviewConflict{
			Code:    code,
			Path:    path,
			Field:   "source.observation",
			Value:   map[string]any{"before": first, "before_exists": firstExists, "after": second, "after_exists": secondExists},
			Message: "workspace source changed between read-only preview observations",
		}
	}
	return nil
}

func markerSnapshotConflict(inspection Inspection, nodes []previewNode) *PreviewConflict {
	var markerNode *previewNode
	for index := range nodes {
		if nodes[index].Path == MarkerRelativePath {
			markerNode = &nodes[index]
			break
		}
	}
	raw := inspection.Marker.RawBytes()
	if !inspection.Marker.Present {
		if markerNode == nil {
			return nil
		}
		return &PreviewConflict{Code: CodePreviewInspectionChanged, Path: MarkerRelativePath, Field: "marker.presence", Value: "scanner observed marker after inspection", Message: "marker presence changed during preview"}
	}
	if len(raw) == 0 || markerNode == nil || markerNode.NodeType != string(PreviewNodeFile) {
		return nil
	}
	hash := sha256.Sum256(raw)
	wantHash := hex.EncodeToString(hash[:])
	if markerNode.Size == int64(len(raw)) && markerNode.SHA256 == wantHash {
		return nil
	}
	return &PreviewConflict{
		Code:    CodePreviewSourceChanged,
		Path:    MarkerRelativePath,
		Field:   "marker.sha256",
		Value:   map[string]any{"inspection_size": len(raw), "inspection_sha256": wantHash, "manifest_size": markerNode.Size, "manifest_sha256": markerNode.SHA256},
		Message: "marker bytes differ between compatibility inspection and manifest scan",
	}
}

func uniquePreviewConflicts(conflicts []PreviewConflict) []PreviewConflict {
	sortPreviewConflicts(conflicts)
	result := make([]PreviewConflict, 0, len(conflicts))
	previous := ""
	for _, conflict := range conflicts {
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%v\x00%s", conflict.Code, conflict.Path, conflict.Destination, conflict.Field, conflict.Value, conflict.Message)
		if key == previous {
			continue
		}
		previous = key
		result = append(result, conflict)
	}
	return result
}

func sortPreviewConflicts(conflicts []PreviewConflict) {
	sort.SliceStable(conflicts, func(i, j int) bool {
		if conflicts[i].Path != conflicts[j].Path {
			return conflicts[i].Path < conflicts[j].Path
		}
		if conflicts[i].Destination != conflicts[j].Destination {
			return conflicts[i].Destination < conflicts[j].Destination
		}
		if conflicts[i].Code != conflicts[j].Code {
			return conflicts[i].Code < conflicts[j].Code
		}
		if conflicts[i].Field != conflicts[j].Field {
			return conflicts[i].Field < conflicts[j].Field
		}
		return conflicts[i].Message < conflicts[j].Message
	})
}
