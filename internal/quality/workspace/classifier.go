package workspace

import (
	"strings"

	"denova/internal/workspacepath"
)

// Category is the exhaustive Workspace Schema v1 path category.
type Category string

const (
	CategoryFormalAuthoritative    Category = "formal_authoritative"
	CategoryReviewArtifact         Category = "review_artifact"
	CategoryRuntimeRecovery        Category = "runtime_recovery"
	CategoryRebuildableProjection  Category = "rebuildable_projection"
	CategoryProtectedLegacyUnknown Category = "protected_legacy_unknown"
)

var categories = [...]Category{
	CategoryFormalAuthoritative,
	CategoryReviewArtifact,
	CategoryRuntimeRecovery,
	CategoryRebuildableProjection,
	CategoryProtectedLegacyUnknown,
}

// AllCategories returns the stable exhaustive v1 category order.
func AllCategories() []Category {
	result := make([]Category, len(categories))
	copy(result, categories[:])
	return result
}

// VersionDisposition is the exact workspace-version treatment for a path.
type VersionDisposition string

const (
	VersionInclude VersionDisposition = "include"
	VersionExclude VersionDisposition = "exclude"
)

// Classification records the first exact ADR rule that matched a path.
type Classification struct {
	Path               string
	Category           Category
	VersionDisposition VersionDisposition
	Rule               string
}

// ClassifyPath applies the ordered Workspace Schema v1 table. Unknown paths
// are protected and included; no path is silently treated as disposable.
func ClassifyPath(rel string) (Classification, error) {
	path, err := ValidateRelativePath(rel, PathOptions{Intent: PathIntentExisting})
	if err != nil {
		return Classification{}, err
	}
	include := func(category Category, rule string) (Classification, error) {
		return Classification{Path: path, Category: category, VersionDisposition: VersionInclude, Rule: rule}, nil
	}
	exclude := func(category Category, rule string) (Classification, error) {
		return Classification{Path: path, Category: category, VersionDisposition: VersionExclude, Rule: rule}, nil
	}

	for _, root := range []string{
		workspacepath.CurrentRel("quality", "artifacts"),
		workspacepath.CurrentRel("quality", "decisions"),
		workspacepath.CurrentRel("quality", "finalizations"),
		workspacepath.CurrentRel("quality", "migration-receipts"),
	} {
		if isPathTree(path, root) {
			return include(CategoryReviewArtifact, root+"/**")
		}
	}

	for _, dataRoot := range []string{workspacepath.DataDirName, workspacepath.LegacyDataDirName} {
		for _, name := range []string{"runs", "checkpoints", "sessions", "changes", "reviews", "interactive", "backups", "messages"} {
			root := dataRoot + "/" + name
			if isPathTree(path, root) {
				return exclude(CategoryRuntimeRecovery, "${dataRoot}/"+name+"/**")
			}
		}
		if path == dataRoot+"/automations/inbox.json" {
			return exclude(CategoryRuntimeRecovery, "${dataRoot}/automations/inbox.json")
		}
	}
	if isPathTree(path, workspacepath.CurrentRel("quality", "runs")) {
		return exclude(CategoryRuntimeRecovery, workspacepath.CurrentRel("quality", "runs")+"/**")
	}
	if isPathTree(path, ".denova-migration") || isPathTree(path, ".nova-migration") {
		return exclude(CategoryRuntimeRecovery, "migration-state/**")
	}
	if isRootMigrationTemp(path) {
		return exclude(CategoryRuntimeRecovery, "migration-temp")
	}
	if isPathTree(path, ".git") {
		return exclude(CategoryRuntimeRecovery, ".git/**")
	}

	if path == workspacepath.CurrentRel("index.db") || isCurrentIndexSidecar(path) ||
		isPathTree(path, workspacepath.CurrentRel("cache")) || isPathTree(path, workspacepath.CurrentRel("quality", "projections")) {
		return exclude(CategoryRebuildableProjection, workspacepath.DataDirName+"/projection")
	}

	if isRootVisible(path) {
		return include(CategoryFormalAuthoritative, "root-visible/**")
	}
	if path == MarkerRelativePath || path == workspacepath.CurrentRel("profile-lock.json") ||
		path == workspacepath.CurrentRel("quality", "preferences.jsonl") {
		return include(CategoryFormalAuthoritative, "canonical-v1-authority")
	}
	if isPathTree(path, workspacepath.CurrentRel("quality", "specs")) {
		return include(CategoryFormalAuthoritative, workspacepath.CurrentRel("quality", "specs")+"/**")
	}
	for _, dataRoot := range []string{workspacepath.DataDirName, workspacepath.LegacyDataDirName} {
		if isPathTree(path, dataRoot+"/lore") || path == dataRoot+"/chapter_statuses.json" {
			return include(CategoryFormalAuthoritative, "${dataRoot}/creative-authority")
		}
		for _, name := range []string{"skills", "styles", "image-presets", "story-tellers", "story-directors", "story-director-modules"} {
			if isPathTree(path, dataRoot+"/"+name) {
				return include(CategoryFormalAuthoritative, "${dataRoot}/configuration")
			}
		}
		if path == dataRoot+"/automations/tasks.json" {
			return include(CategoryFormalAuthoritative, "${dataRoot}/automations/tasks.json")
		}
	}

	if isLegacyV1LookingPath(path) {
		return include(CategoryProtectedLegacyUnknown, ".nova/v1-looking-protected")
	}
	return include(CategoryProtectedLegacyUnknown, "default-protect")
}

func isPathTree(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

func isRootVisible(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return first != "" && !strings.HasPrefix(first, ".")
}

func isRootMigrationTemp(path string) bool {
	if strings.ContainsRune(path, '/') || !strings.HasSuffix(path, ".tmp") {
		return false
	}
	return strings.HasPrefix(path, ".denova-migrate-") || strings.HasPrefix(path, ".nova-migrate-")
}

func isCurrentIndexSidecar(path string) bool {
	prefix := workspacepath.CurrentRel("index.db-")
	return strings.HasPrefix(path, prefix) && !strings.ContainsRune(strings.TrimPrefix(path, workspacepath.DataDirName+"/"), '/')
}

func isLegacyV1LookingPath(path string) bool {
	return path == workspacepath.LegacyRel("workspace-schema.json") || path == workspacepath.LegacyRel("profile-lock.json") ||
		isPathTree(path, workspacepath.LegacyRel("quality")) || path == workspacepath.LegacyRel("index.db") ||
		strings.HasPrefix(path, workspacepath.LegacyRel("index.db-")) && !strings.ContainsRune(strings.TrimPrefix(path, workspacepath.LegacyDataDirName+"/"), '/') ||
		isPathTree(path, workspacepath.LegacyRel("cache"))
}
