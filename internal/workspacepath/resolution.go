package workspacepath

import (
	"fmt"
	"reflect"
	"sort"
)

// TargetResolution records the data root selected by the existing
// target-specific compatibility rules for one data-root-relative path.
type TargetResolution struct {
	Path string
	Root string
}

// RootResolution is an immutable snapshot of the active root and all relevant
// target-specific resolutions for one inspect or preview operation.
type RootResolution struct {
	ActiveRoot string
	Targets    []TargetResolution
}

// ResolutionError reports a path that could not be inspected without losing
// the workspace-root boundary. Callers must treat it as a compatibility
// blocker instead of falling back to the path-based compatibility helpers.
type ResolutionError struct {
	Path      string
	Operation string
	Err       error
}

func (err *ResolutionError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Err == nil {
		return fmt.Sprintf("workspace root resolution %s path=%q", err.Operation, err.Path)
	}
	return fmt.Sprintf("workspace root resolution %s path=%q: %v", err.Operation, err.Path, err.Err)
}

func (err *ResolutionError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// ResolveRoots evaluates the existing workspace selection rules through a
// workspace-root-bound handle and returns targets in deterministic order. A
// second observation must match the first, so callers never combine discovery
// facts with a different active-root snapshot. Callers must pass validated,
// data-root-relative paths using forward slashes.
func ResolveRoots(workspace string, targets ...string) (RootResolution, error) {
	ordered := orderedResolutionTargets(targets)
	first, err := resolveRootsOnce(workspace, ordered)
	if err != nil {
		return first, err
	}
	second, err := resolveRootsOnce(workspace, ordered)
	if err != nil {
		return first, err
	}
	if !reflect.DeepEqual(first, second) {
		return first, &ResolutionError{
			Path:      workspace,
			Operation: "stability_check",
			Err:       fmt.Errorf("workspace root selection changed between read-only observations"),
		}
	}
	return first, nil
}

func orderedResolutionTargets(targets []string) []string {
	unique := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target != "" {
			unique[target] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(unique))
	for target := range unique {
		ordered = append(ordered, target)
	}
	sort.Strings(ordered)
	return ordered
}

// Consistent reports whether every target-specific selection agrees with the
// pinned active root.
func (resolution RootResolution) Consistent() bool {
	for _, target := range resolution.Targets {
		if target.Root != resolution.ActiveRoot {
			return false
		}
	}
	return true
}
