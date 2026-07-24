package workspace

import (
	"fmt"
	"sort"
	"strings"

	"denova/internal/workspacepath"
	"github.com/Masterminds/semver/v3"
)

// CompatibilityMode distinguishes a fully managed v1 workspace from a
// best-effort safe read/open workspace.
type CompatibilityMode string

const (
	ModeManagedV1    CompatibilityMode = "managed_v1"
	ModeSafeReadOpen CompatibilityMode = "safe_read_open"
)

// ManagedMutationAccess records whether v1-owned paths may be mutated.
type ManagedMutationAccess string

const (
	MutationAllowed ManagedMutationAccess = "allowed"
	MutationBlocked ManagedMutationAccess = "blocked"
)

// CompatibilityIssue is a stable, structured compatibility diagnostic.
type CompatibilityIssue struct {
	Code     ErrorCode
	Path     string
	Field    string
	Value    any
	Message  string
	Blocking bool
}

// Inspection is an immutable read-only compatibility snapshot. Its marker raw
// bytes, root selection, and issues all belong to the same inspection pass.
type Inspection struct {
	Workspace               string
	ActiveRoot              string
	RootResolution          workspacepath.RootResolution
	Marker                  MarkerRecord
	Mode                    CompatibilityMode
	ManagedMutation         ManagedMutationAccess
	Issues                  []CompatibilityIssue
	UnknownOptionalFeatures []string
	LegacyConflicts         []string
}

// CanOpen reports the adapter's safe-read decision. Filesystem failures are
// returned directly by Inspector.Inspect and therefore never produce a false
// positive Inspection.
func (inspection Inspection) CanOpen() bool {
	return inspection.Mode == ModeManagedV1 || inspection.Mode == ModeSafeReadOpen
}

// CanManagedMutate reports whether all schema, feature, writer, and root guards
// allow a future v1-managed writer.
func (inspection Inspection) CanManagedMutate() bool {
	return inspection.Mode == ModeManagedV1 && inspection.ManagedMutation == MutationAllowed
}

// RequireManagedMutation returns every current blocker without modifying the
// workspace or attempting a fallback.
func (inspection Inspection) RequireManagedMutation() error {
	if inspection.CanManagedMutate() {
		return nil
	}
	issues := make([]CompatibilityIssue, 0, len(inspection.Issues))
	for _, issue := range inspection.Issues {
		if issue.Blocking {
			issues = append(issues, issue)
		}
	}
	return &MutationBlockedError{Issues: issues}
}

// MutationBlockedError preserves structured blocking fields, paths, and values.
type MutationBlockedError struct {
	Issues []CompatibilityIssue
}

func (err *MutationBlockedError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if len(err.Issues) == 0 {
		return "Workspace Schema v1 managed mutation is blocked"
	}
	issue := err.Issues[0]
	return fmt.Sprintf("Workspace Schema v1 managed mutation blocked: code=%s field=%s path=%q value=%v", issue.Code, issue.Field, issue.Path, issue.Value)
}

type supportedFeature struct {
	constraint *semver.Constraints
}

func blockingIssue(code ErrorCode, path, field string, value any, message string) CompatibilityIssue {
	return CompatibilityIssue{Code: code, Path: path, Field: field, Value: value, Message: message, Blocking: true}
}

func optionalIssue(code ErrorCode, path, field string, value any, message string) CompatibilityIssue {
	return CompatibilityIssue{Code: code, Path: path, Field: field, Value: value, Message: message, Blocking: false}
}

func evaluateMarkerCompatibility(marker Marker, applicationRaw string, applicationVersion *semver.Version, applicationErr error, supported map[string]supportedFeature, writerRange *semver.Constraints) ([]CompatibilityIssue, []string) {
	issues := make([]CompatibilityIssue, 0)
	unknownOptional := make([]string, 0)

	if applicationErr != nil {
		value := applicationRaw
		if value == "" {
			value = "missing"
		}
		issues = append(issues, blockingIssue(CodeApplicationVersionInvalid, "", "application_version", value, "running Denova version must be strict SemVer without a leading v"))
	} else if !writerRange.Check(applicationVersion) {
		issues = append(issues, blockingIssue(CodeApplicationVersionUnsupported, "", "application_version", applicationRaw, "running Denova version is outside the local v1 writer range"))
	}

	if marker.Writer.Version != "" {
		writer, err := semver.StrictNewVersion(marker.Writer.Version)
		switch {
		case err != nil:
			issues = append(issues, blockingIssue(CodeWriterVersionInvalid, MarkerRelativePath, "writer.version", marker.Writer.Version, "recorded writer must be strict SemVer without a leading v"))
		case !writerRange.Check(writer):
			issues = append(issues, blockingIssue(CodeWriterVersionUnsupported, MarkerRelativePath, "writer.version", marker.Writer.Version, "recorded writer is outside the local v1 writer range"))
		}
	}

	featureIDs := make([]string, 0, len(marker.Features))
	for id := range marker.Features {
		featureIDs = append(featureIDs, id)
	}
	sort.Strings(featureIDs)
	for _, id := range featureIDs {
		feature := marker.Features[id]
		version, err := semver.StrictNewVersion(feature.Version)
		if err != nil {
			issues = append(issues, blockingIssue(CodeFeatureMalformed, MarkerRelativePath, "features."+id+".version", feature.Version, "feature version must be strict SemVer without a leading v"))
			continue
		}
		support, known := supported[id]
		if known && support.constraint.Check(version) {
			continue
		}
		if feature.Required {
			issues = append(issues, blockingIssue(CodeFeatureRequiredUnsupported, MarkerRelativePath, "features."+id, feature.Version, "required feature is unknown or outside its supported range"))
			continue
		}
		unknownOptional = append(unknownOptional, id)
		message := "optional feature is preserved but not owned by this reader"
		if known {
			message = "optional feature version is outside its supported range and is preserved"
		}
		issues = append(issues, optionalIssue(CodeFeatureOptionalUnknown, MarkerRelativePath, "features."+id, feature.Version, message))
	}

	if validMigrationState(marker.Migration) && marker.Migration != MigrationNotRequired && marker.Migration != MigrationCompleted {
		issues = append(issues, blockingIssue(CodeMigrationStateIncomplete, MarkerRelativePath, "migration.state", marker.Migration, "migration state requires explicit P1-T02B recovery handling"))
	}
	return issues, unknownOptional
}

func newSupportedFeatures(input map[string]string) (map[string]supportedFeature, error) {
	result := make(map[string]supportedFeature, len(input))
	ids := make([]string, 0, len(input))
	for id := range input {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rangeText := input[id]
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("supported feature ID is required")
		}
		constraint, err := semver.NewConstraint(rangeText)
		if err != nil {
			return nil, fmt.Errorf("supported feature %q range %q: %w", id, rangeText, err)
		}
		constraint.IncludePrerelease = true
		result[id] = supportedFeature{constraint: constraint}
	}
	return result, nil
}
