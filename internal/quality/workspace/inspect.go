package workspace

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"denova/internal/workspacepath"
	"github.com/Masterminds/semver/v3"
)

const maxMarkerBytes int64 = 1024 * 1024

// InspectorOptions fixes local compatibility capabilities. The adjacent ADR
// example JSON is deliberately never loaded as configuration.
type InspectorOptions struct {
	ApplicationVersion string
	SupportedFeatures  map[string]string
	RelevantTargets    []string
}

// Inspector performs read-only Workspace Schema v1 inspection.
type Inspector struct {
	applicationRaw     string
	applicationVersion *semver.Version
	applicationErr     error
	supportedFeatures  map[string]supportedFeature
	relevantTargets    []string
	writerRange        *semver.Constraints
}

// NewInspector validates local capability configuration without requiring the
// running application version to be valid; a missing/invalid running version is
// a compatibility blocker reported by Inspect rather than a constructor error.
func NewInspector(options InspectorOptions) (*Inspector, error) {
	supported, err := newSupportedFeatures(options.SupportedFeatures)
	if err != nil {
		return nil, err
	}
	writerRange, err := semver.NewConstraint(WriterCompatibilityRangeV1)
	if err != nil {
		return nil, fmt.Errorf("internal Workspace Schema v1 writer range: %w", err)
	}
	writerRange.IncludePrerelease = true
	targets := make([]string, 0, len(options.RelevantTargets))
	seen := make(map[string]struct{}, len(options.RelevantTargets))
	for _, target := range options.RelevantTargets {
		normalized, validateErr := ValidateRelativePath(target, PathOptions{Intent: PathIntentExisting})
		if validateErr != nil {
			return nil, fmt.Errorf("relevant target %q: %w", target, validateErr)
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		targets = append(targets, normalized)
	}
	sort.Strings(targets)

	applicationVersion, applicationErr := semver.StrictNewVersion(options.ApplicationVersion)
	return &Inspector{
		applicationRaw:     options.ApplicationVersion,
		applicationVersion: applicationVersion,
		applicationErr:     applicationErr,
		supportedFeatures:  supported,
		relevantTargets:    targets,
		writerRange:        writerRange,
	}, nil
}

// Inspect pins one canonical workspace and active data root, parses only the
// literal current marker, and reports compatibility without writing anything.
func (inspector *Inspector) Inspect(workspace string) (Inspection, error) {
	if inspector == nil {
		return Inspection{}, &InspectionError{Code: CodeWorkspaceInvalid, Field: "inspector", Value: "nil", Message: "inspector is required"}
	}
	canonicalWorkspace, err := canonicalWorkspace(workspace)
	if err != nil {
		return Inspection{}, err
	}

	targets, conflicts, discoveryIssues, err := discoverWorkspaceInputs(canonicalWorkspace)
	if err != nil {
		return Inspection{}, err
	}
	targets = append(targets, inspector.relevantTargets...)
	rootResolution, resolutionErr := workspacepath.ResolveRoots(canonicalWorkspace, targets...)
	issues := append([]CompatibilityIssue(nil), discoveryIssues...)
	if resolutionErr != nil {
		issuePath := canonicalWorkspace
		issueValue := any(resolutionErr.Error())
		var rootErr *workspacepath.ResolutionError
		if errors.As(resolutionErr, &rootErr) {
			issuePath = rootErr.Path
			issueValue = map[string]string{
				"operation": rootErr.Operation,
				"path":      rootErr.Path,
				"error":     rootErr.Error(),
			}
		}
		issues = append(issues, blockingIssue(
			CodeRootResolutionUnsafe,
			issuePath,
			"root_resolution",
			issueValue,
			"active-root selection could not be completed through a stable workspace-root-bound observation",
		))
	}
	if rootResolution.ActiveRoot != workspacepath.DataDirName {
		issues = append(issues, blockingIssue(
			CodeActiveRootUnsupported,
			rootResolution.ActiveRoot,
			"active_root",
			rootResolution.ActiveRoot,
			"v1-managed paths require the canonical .denova root after explicit adoption or migration",
		))
	}
	for _, target := range rootResolution.Targets {
		if target.Root == rootResolution.ActiveRoot {
			continue
		}
		issues = append(issues, blockingIssue(
			CodeRootResolutionDivergence,
			target.Root+"/"+target.Path,
			"target_resolution",
			map[string]string{"active_root": rootResolution.ActiveRoot, "target_root": target.Root, "target": target.Path},
			"target-specific resolution disagrees with the pinned active root",
		))
	}
	for _, conflict := range conflicts {
		issues = append(issues, blockingIssue(CodeLegacyV1Conflict, conflict, "legacy_path", conflict, "legacy v1-looking input is protected and requires explicit reconciliation"))
	}

	record, parsed, markerIssues, err := readMarkerRecord(canonicalWorkspace)
	if err != nil {
		return Inspection{}, err
	}
	issues = append(issues, markerIssues...)
	unknownOptional := make([]string, 0)
	if parsed {
		compatibilityIssues, optional := evaluateMarkerCompatibility(
			record.Contract,
			inspector.applicationRaw,
			inspector.applicationVersion,
			inspector.applicationErr,
			inspector.supportedFeatures,
			inspector.writerRange,
		)
		issues = append(issues, compatibilityIssues...)
		unknownOptional = optional
	}

	mode := ModeManagedV1
	mutation := MutationAllowed
	for _, issue := range issues {
		if issue.Blocking {
			mode = ModeSafeReadOpen
			mutation = MutationBlocked
			break
		}
	}
	return Inspection{
		Workspace:               canonicalWorkspace,
		ActiveRoot:              rootResolution.ActiveRoot,
		RootResolution:          cloneRootResolution(rootResolution),
		Marker:                  record,
		Mode:                    mode,
		ManagedMutation:         mutation,
		Issues:                  append([]CompatibilityIssue(nil), issues...),
		UnknownOptionalFeatures: append([]string(nil), unknownOptional...),
		LegacyConflicts:         append([]string(nil), conflicts...),
	}, nil
}

func canonicalWorkspace(workspace string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		return "", &InspectionError{Code: CodeWorkspaceInvalid, Field: "workspace", Value: workspace, Message: "workspace path is required"}
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", &InspectionError{Code: CodeWorkspaceInvalid, Path: workspace, Field: "workspace", Value: workspace, Message: "workspace path cannot be made absolute", Err: err}
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", &InspectionError{Code: CodeWorkspaceRead, Path: workspace, Field: "workspace", Value: absolute, Message: "workspace canonical path cannot be resolved", Err: err}
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", &InspectionError{Code: CodeWorkspaceRead, Path: workspace, Field: "workspace", Value: canonical, Message: "workspace cannot be inspected", Err: err}
	}
	if !info.IsDir() {
		return "", &InspectionError{Code: CodeWorkspaceInvalid, Path: workspace, Field: "workspace", Value: canonical, Message: "workspace is not a directory"}
	}
	return filepath.Clean(canonical), nil
}

func discoverWorkspaceInputs(workspace string) ([]string, []string, []CompatibilityIssue, error) {
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, nil, nil, &InspectionError{Code: CodeWorkspaceRead, Path: workspace, Field: "workspace", Value: workspace, Message: "workspace root handle cannot be opened", Err: err}
	}
	defer workspaceRoot.Close()

	targets := make(map[string]struct{})
	conflicts := make(map[string]struct{})
	issues := make([]CompatibilityIssue, 0)
	for _, dataRoot := range []string{workspacepath.DataDirName, workspacepath.LegacyDataDirName} {
		rootPath := filepath.Join(workspace, dataRoot)
		_, err := workspaceRoot.Lstat(filepath.FromSlash(dataRoot))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, nil, &InspectionError{Code: CodeWorkspaceRead, Path: dataRoot, Field: "data_root", Value: rootPath, Message: "data root cannot be inspected", Err: err}
		}
		if _, canonicalErr := ResolveCanonicalPath(workspace, dataRoot, CanonicalOptions{}); canonicalErr != nil {
			var pathErr *PathError
			if errors.As(canonicalErr, &pathErr) {
				issues = append(issues, blockingIssue(pathErr.Code, dataRoot, pathErr.Field, pathErr.Value, pathErr.Message))
				continue
			}
			return nil, nil, nil, &InspectionError{Code: CodeWorkspaceRead, Path: dataRoot, Field: "data_root", Value: rootPath, Message: "data root canonical path cannot be resolved", Err: canonicalErr}
		}
		dataHandle, err := workspaceRoot.OpenRoot(filepath.FromSlash(dataRoot))
		if err != nil {
			issues = append(issues, blockingIssue(CodePathCanonical, dataRoot, "data_root.handle", dataRoot, "workspace-root-bound data root open failed"))
			continue
		}
		walkErr := fs.WalkDir(dataHandle.FS(), ".", func(child string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if child == "." {
				return nil
			}
			rel := dataRoot + "/" + filepath.ToSlash(child)
			classification, classifyErr := ClassifyPath(rel)
			if classifyErr != nil {
				var pathErr *PathError
				if errors.As(classifyErr, &pathErr) {
					issues = append(issues, blockingIssue(pathErr.Code, rel, pathErr.Field, pathErr.Value, pathErr.Message))
					return nil
				}
				return classifyErr
			}
			if classification.VersionDisposition == VersionInclude {
				target := strings.TrimPrefix(rel, dataRoot+"/")
				if target != rel && target != "" {
					targets[target] = struct{}{}
				}
			}
			if dataRoot == workspacepath.LegacyDataDirName && isLegacyV1LookingPath(rel) {
				conflicts[rel] = struct{}{}
			}
			return nil
		})
		closeErr := dataHandle.Close()
		if walkErr != nil {
			return nil, nil, nil, &InspectionError{Code: CodeWorkspaceRead, Path: dataRoot, Field: "data_root", Value: rootPath, Message: "data root traversal failed", Err: walkErr}
		}
		if closeErr != nil {
			return nil, nil, nil, &InspectionError{Code: CodeWorkspaceRead, Path: dataRoot, Field: "data_root", Value: rootPath, Message: "data root handle close failed", Err: closeErr}
		}
	}
	return sortedSet(targets), sortedSet(conflicts), issues, nil
}

func readMarkerRecord(workspace string) (MarkerRecord, bool, []CompatibilityIssue, error) {
	markerPath := filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath))
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return MarkerRecord{}, false, nil, &InspectionError{Code: CodeWorkspaceRead, Path: MarkerRelativePath, Field: "marker", Value: workspace, Message: "workspace root handle cannot be opened for marker inspection", Err: err}
	}
	defer root.Close()

	_, err = root.Lstat(filepath.FromSlash(MarkerRelativePath))
	if errors.Is(err, os.ErrNotExist) {
		return MarkerRecord{}, false, []CompatibilityIssue{
			blockingIssue(CodeMarkerMissing, MarkerRelativePath, "marker", "missing", "workspace is unversioned; explicit adoption or migration is required for v1-managed mutation"),
		}, nil
	}
	if err != nil {
		return MarkerRecord{}, false, nil, &InspectionError{Code: CodeWorkspaceRead, Path: MarkerRelativePath, Field: "marker", Value: markerPath, Message: "workspace marker cannot be inspected", Err: err}
	}
	record := MarkerRecord{Present: true}
	canonical, err := ResolveCanonicalPath(workspace, MarkerRelativePath, CanonicalOptions{})
	if err != nil {
		var pathErr *PathError
		if errors.As(err, &pathErr) {
			return record, false, []CompatibilityIssue{blockingIssue(pathErr.Code, MarkerRelativePath, pathErr.Field, pathErr.Value, pathErr.Message)}, nil
		}
		return record, false, nil, &InspectionError{Code: CodeWorkspaceRead, Path: MarkerRelativePath, Field: "marker", Value: markerPath, Message: "workspace marker canonical path cannot be resolved", Err: err}
	}
	info, err := root.Stat(filepath.FromSlash(MarkerRelativePath))
	if err != nil {
		pathErr := &PathError{Code: CodePathCanonical, Path: MarkerRelativePath, Field: "marker.handle", Value: MarkerRelativePath, Message: "workspace-root-bound marker stat failed", Err: err}
		return record, false, []CompatibilityIssue{blockingIssue(pathErr.Code, MarkerRelativePath, pathErr.Field, pathErr.Value, pathErr.Message)}, nil
	}
	if !info.Mode().IsRegular() {
		return record, false, []CompatibilityIssue{blockingIssue(CodeMarkerMalformed, MarkerRelativePath, "marker", info.Mode().String(), "workspace marker must be a regular JSON file")}, nil
	}
	if info.Size() > maxMarkerBytes {
		return record, false, []CompatibilityIssue{blockingIssue(CodeMarkerTooLarge, MarkerRelativePath, "marker.size", info.Size(), "workspace marker exceeds the read-only adapter limit")}, nil
	}
	file, openedInfo, err := openBoundRegularFile(root, MarkerRelativePath, info)
	if err != nil {
		var pathErr *PathError
		if errors.As(err, &pathErr) {
			return record, false, []CompatibilityIssue{blockingIssue(pathErr.Code, MarkerRelativePath, pathErr.Field, pathErr.Value, pathErr.Message)}, nil
		}
		return record, false, nil, &InspectionError{Code: CodeWorkspaceRead, Path: MarkerRelativePath, Field: "marker", Value: canonical.Absolute, Message: "workspace marker cannot be opened", Err: err}
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maxMarkerBytes+1))
	afterInfo, statErr := file.Stat()
	closeErr := file.Close()
	if readErr != nil {
		return record, false, nil, &InspectionError{Code: CodeWorkspaceRead, Path: MarkerRelativePath, Field: "marker", Value: canonical.Absolute, Message: "workspace marker read failed", Err: readErr}
	}
	if statErr != nil {
		return record, false, nil, &InspectionError{Code: CodeWorkspaceRead, Path: MarkerRelativePath, Field: "marker", Value: canonical.Absolute, Message: "workspace marker identity cannot be rechecked", Err: statErr}
	}
	if closeErr != nil {
		return record, false, nil, &InspectionError{Code: CodeWorkspaceRead, Path: MarkerRelativePath, Field: "marker", Value: canonical.Absolute, Message: "workspace marker close failed", Err: closeErr}
	}
	record.raw = append([]byte(nil), raw...)
	issues := make([]CompatibilityIssue, 0)
	identityChanged := openedInfo.Size() != afterInfo.Size() || openedInfo.ModTime() != afterInfo.ModTime() || openedInfo.Mode() != afterInfo.Mode()
	currentInfo, currentErr := root.Stat(filepath.FromSlash(MarkerRelativePath))
	if currentErr != nil || !os.SameFile(openedInfo, currentInfo) {
		identityChanged = true
	}
	rechecked, recheckErr := ResolveCanonicalPath(workspace, MarkerRelativePath, CanonicalOptions{})
	if recheckErr != nil {
		var pathErr *PathError
		if errors.As(recheckErr, &pathErr) {
			issues = append(issues, blockingIssue(pathErr.Code, MarkerRelativePath, pathErr.Field, pathErr.Value, pathErr.Message))
		} else {
			identityChanged = true
		}
	} else if rechecked.Absolute != canonical.Absolute {
		identityChanged = true
	}
	if identityChanged {
		issues = append(issues, blockingIssue(CodePathIdentityChanged, MarkerRelativePath, "marker.identity", MarkerRelativePath, "workspace marker identity changed during inspection"))
	}
	if int64(len(raw)) > maxMarkerBytes {
		issues = append(issues, blockingIssue(CodeMarkerTooLarge, MarkerRelativePath, "marker.size", len(raw), "workspace marker changed beyond the read-only adapter limit during inspection"))
		return record, false, issues, nil
	}
	marker, markerIssues := parseMarker(raw)
	issues = append(issues, markerIssues...)
	record.Contract = marker
	parsed := true
	for _, issue := range issues {
		if issue.Code == CodeMarkerMalformed {
			parsed = false
			break
		}
	}
	return record, parsed, issues, nil
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneRootResolution(resolution workspacepath.RootResolution) workspacepath.RootResolution {
	return workspacepath.RootResolution{
		ActiveRoot: resolution.ActiveRoot,
		Targets:    append([]workspacepath.TargetResolution(nil), resolution.Targets...),
	}
}
