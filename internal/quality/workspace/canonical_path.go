package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// CanonicalOptions controls resolution of an existing source or a planned
// destination whose final components may not exist yet.
type CanonicalOptions struct {
	AllowMissing bool
}

// CanonicalPath binds a logical workspace-relative path to one canonical root
// and target without losing the stored relative spelling.
type CanonicalPath struct {
	Workspace string
	Absolute  string
	Relative  string
	Exists    bool
}

// ResolveCanonicalPath resolves symlinks in the workspace and longest existing
// target prefix, then proves that the canonical target remains contained.
func ResolveCanonicalPath(workspace, rel string, options CanonicalOptions) (CanonicalPath, error) {
	normalized, err := ValidateRelativePath(rel, PathOptions{Intent: PathIntentExisting})
	if err != nil {
		return CanonicalPath{}, err
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return CanonicalPath{}, canonicalPathError(rel, workspace, err)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(absWorkspace)
	if err != nil {
		return CanonicalPath{}, canonicalPathError(rel, workspace, err)
	}
	canonicalWorkspace = filepath.Clean(canonicalWorkspace)
	info, err := os.Stat(canonicalWorkspace)
	if err != nil {
		return CanonicalPath{}, canonicalPathError(rel, workspace, err)
	}
	if !info.IsDir() {
		return CanonicalPath{}, &PathError{Code: CodePathCanonical, Path: rel, Field: "workspace", Value: workspace, Message: "workspace root is not a directory"}
	}

	logicalTarget := filepath.Join(canonicalWorkspace, filepath.FromSlash(normalized))
	canonicalTarget, exists, err := resolveCanonicalTarget(logicalTarget, options.AllowMissing)
	if err != nil {
		return CanonicalPath{}, canonicalPathError(rel, logicalTarget, err)
	}
	if !pathContained(canonicalWorkspace, canonicalTarget) {
		return CanonicalPath{}, &PathError{
			Code:    CodePathEscape,
			Path:    rel,
			Field:   "canonical_target",
			Value:   canonicalTarget,
			Message: "canonical target escapes the workspace boundary",
		}
	}
	return CanonicalPath{
		Workspace: canonicalWorkspace,
		Absolute:  canonicalTarget,
		Relative:  normalized,
		Exists:    exists,
	}, nil
}

func resolveCanonicalTarget(target string, allowMissing bool) (string, bool, error) {
	resolved, err := filepath.EvalSymlinks(target)
	if err == nil {
		return filepath.Clean(resolved), true, nil
	}
	if !allowMissing || !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}

	candidate := filepath.Clean(target)
	for {
		_, lstatErr := os.Lstat(candidate)
		switch {
		case lstatErr == nil:
			prefix, resolveErr := filepath.EvalSymlinks(candidate)
			if resolveErr != nil {
				return "", false, resolveErr
			}
			suffix, relErr := filepath.Rel(candidate, target)
			if relErr != nil {
				return "", false, relErr
			}
			return filepath.Clean(filepath.Join(prefix, suffix)), false, nil
		case !errors.Is(lstatErr, os.ErrNotExist):
			return "", false, lstatErr
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", false, err
		}
		candidate = parent
	}
}

func pathContained(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func canonicalPathError(rel string, value any, err error) *PathError {
	code := CodePathCanonical
	message := "canonical path resolution failed"
	if isSymlinkLoopError(err) {
		code = CodePathSymlinkLoop
		message = "symbolic link or reparse-point loop"
	}
	return &PathError{Code: code, Path: rel, Field: "canonical_target", Value: value, Message: message, Err: err}
}

func isSymlinkLoopError(err error) bool {
	return errors.Is(err, syscall.ELOOP) || err != nil && err.Error() == "EvalSymlinks: too many links"
}
