package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type previewNode struct {
	Path     string
	NodeType string
	Mode     uint32
	Identity FilesystemIdentity
	Size     int64
	SHA256   string
}

func scanWorkspaceNodes(workspace string) ([]previewNode, []PreviewConflict, error) {
	workspaceIdentity, err := pathFilesystemIdentity(workspace)
	if err != nil {
		return nil, nil, &InspectionError{Code: CodeWorkspaceRead, Path: workspace, Field: "workspace.identity", Value: workspace, Message: "workspace filesystem identity cannot be pinned", Err: err}
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, nil, &InspectionError{Code: CodeWorkspaceRead, Path: workspace, Field: "workspace", Value: workspace, Message: "workspace root handle cannot be opened", Err: err}
	}
	nodes := make([]previewNode, 0)
	conflicts := make([]PreviewConflict, 0)
	walkErr := fs.WalkDir(root.FS(), ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if rel == "." {
			return nil
		}
		if _, validateErr := ValidateRelativePath(rel, PathOptions{Intent: PathIntentExisting}); validateErr != nil {
			conflicts = append(conflicts, previewConflictFromError(validateErr, rel, ""))
			if entry.IsDir() {
				return fs.SkipDir
			}
			nodes = append(nodes, previewNode{Path: rel, NodeType: nodeTypeFromEntry(entry)})
			return nil
		}

		info, err := root.Lstat(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, readErr := root.Readlink(filepath.FromSlash(rel))
			if readErr != nil {
				return readErr
			}
			hash := sha256.Sum256([]byte(target))
			nodes = append(nodes, previewNode{
				Path:     rel,
				NodeType: string(PreviewNodeSymlink),
				Size:     int64(len(target)),
				SHA256:   hex.EncodeToString(hash[:]),
			})
			conflicts = append(conflicts, PreviewConflict{
				Code:    CodePreviewReparsePoint,
				Path:    rel,
				Field:   "source.node_type",
				Value:   target,
				Message: "symbolic links and reparse points require an explicit migration choice",
			})
			if _, canonicalErr := ResolveCanonicalPath(workspace, rel, CanonicalOptions{}); canonicalErr != nil {
				conflicts = append(conflicts, previewConflictFromError(canonicalErr, rel, ""))
			}
			return nil
		case info.IsDir():
			identity, identityErr := rootEntryFilesystemIdentity(root, rel, info)
			if identityErr != nil {
				return identityErr
			}
			nodes = append(nodes, previewNode{Path: rel, NodeType: "directory", Mode: uint32(info.Mode().Perm()), Identity: identity})
			if identity.Volume != workspaceIdentity.Volume {
				conflicts = append(conflicts, PreviewConflict{Code: CodePreviewReparsePoint, Path: rel, Field: "source.filesystem", Value: identity.Volume, Message: "mount points and filesystem-boundary traversal require explicit authorization"})
				return fs.SkipDir
			}
			if rel == ".git" {
				return fs.SkipDir
			}
			canonical, canonicalErr := ResolveCanonicalPath(workspace, rel, CanonicalOptions{})
			if canonicalErr != nil {
				conflicts = append(conflicts, previewConflictFromError(canonicalErr, rel, ""))
				return fs.SkipDir
			}
			logicalPath := filepath.Join(workspace, filepath.FromSlash(rel))
			if canonical.Absolute != filepath.Clean(logicalPath) {
				conflicts = append(conflicts, PreviewConflict{
					Code:    CodePreviewReparsePoint,
					Path:    rel,
					Field:   "source.canonical_path",
					Value:   canonical.Absolute,
					Message: "directory resolves through a link, junction, mount alias, or reparse point",
				})
				return fs.SkipDir
			}
			return nil
		case info.Mode().IsRegular():
			identity, identityErr := rootEntryFilesystemIdentity(root, rel, info)
			if identityErr != nil {
				return identityErr
			}
			if identity.Volume != workspaceIdentity.Volume {
				conflicts = append(conflicts, PreviewConflict{Code: CodePreviewReparsePoint, Path: rel, Field: "source.filesystem", Value: identity.Volume, Message: "file crosses the pinned workspace filesystem boundary"})
			}
			canonical, canonicalErr := ResolveCanonicalPath(workspace, rel, CanonicalOptions{})
			if canonicalErr != nil {
				conflicts = append(conflicts, previewConflictFromError(canonicalErr, rel, ""))
				nodes = append(nodes, previewNode{Path: rel, NodeType: string(PreviewNodeFile), Mode: uint32(info.Mode().Perm()), Identity: identity, Size: info.Size()})
				return nil
			}
			logicalPath := filepath.Join(workspace, filepath.FromSlash(rel))
			if canonical.Absolute != filepath.Clean(logicalPath) {
				conflicts = append(conflicts, PreviewConflict{
					Code:    CodePreviewReparsePoint,
					Path:    rel,
					Field:   "source.canonical_path",
					Value:   canonical.Absolute,
					Message: "file resolves through a link, junction, mount alias, or reparse point",
				})
			}
			size, hash, changed, hashErr := hashBoundPreviewFile(root, rel, info)
			if hashErr != nil {
				var pathErr *PathError
				if errors.As(hashErr, &pathErr) {
					conflicts = append(conflicts, previewConflictFromError(hashErr, rel, ""))
					nodes = append(nodes, previewNode{Path: rel, NodeType: string(PreviewNodeFile), Mode: uint32(info.Mode().Perm()), Identity: identity, Size: size, SHA256: hash})
					return nil
				}
				return hashErr
			}
			nodes = append(nodes, previewNode{Path: rel, NodeType: string(PreviewNodeFile), Mode: uint32(info.Mode().Perm()), Identity: identity, Size: size, SHA256: hash})
			if changed {
				conflicts = append(conflicts, PreviewConflict{
					Code:    CodePreviewSourceChanged,
					Path:    rel,
					Field:   "source.hash",
					Value:   hash,
					Message: "source changed while the dry-run preview was hashing it",
				})
			}
			rechecked, recheckErr := ResolveCanonicalPath(workspace, rel, CanonicalOptions{})
			if recheckErr != nil {
				conflicts = append(conflicts, previewConflictFromError(recheckErr, rel, ""))
			} else if rechecked.Absolute != canonical.Absolute {
				conflicts = append(conflicts, PreviewConflict{
					Code:    CodePreviewSourceChanged,
					Path:    rel,
					Field:   "source.canonical_path",
					Value:   map[string]string{"before": canonical.Absolute, "after": rechecked.Absolute},
					Message: "source canonical target changed during preview",
				})
			}
			return nil
		default:
			nodes = append(nodes, previewNode{Path: rel, NodeType: string(PreviewNodeOther), Size: info.Size()})
			conflicts = append(conflicts, PreviewConflict{
				Code:    CodePreviewUnsupportedNode,
				Path:    rel,
				Field:   "source.node_type",
				Value:   info.Mode().Type().String(),
				Message: "special filesystem nodes cannot be represented by the migration contract",
			})
			return nil
		}
	})
	closeErr := root.Close()
	if walkErr != nil {
		return nil, nil, &InspectionError{Code: CodeWorkspaceRead, Path: workspace, Field: "workspace", Value: workspace, Message: "workspace preview traversal failed", Err: walkErr}
	}
	if closeErr != nil {
		return nil, nil, &InspectionError{Code: CodeWorkspaceRead, Path: workspace, Field: "workspace", Value: workspace, Message: "workspace root handle close failed", Err: closeErr}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Path < nodes[j].Path })
	sortPreviewConflicts(conflicts)
	return nodes, conflicts, nil
}

func previewConflictFromError(err error, path, destination string) PreviewConflict {
	var pathErr *PathError
	if errors.As(err, &pathErr) {
		return PreviewConflict{
			Code:        pathErr.Code,
			Path:        path,
			Destination: destination,
			Field:       pathErr.Field,
			Value:       pathErr.Value,
			Message:     pathErr.Message,
		}
	}
	return PreviewConflict{
		Code:        CodeWorkspaceRead,
		Path:        path,
		Destination: destination,
		Field:       "filesystem",
		Value:       fmt.Sprint(err),
		Message:     "filesystem path could not be inspected",
	}
}

func nodeTypeFromEntry(entry fs.DirEntry) string {
	if entry.IsDir() {
		return "directory"
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return string(PreviewNodeSymlink)
	}
	if entry.Type().IsRegular() {
		return string(PreviewNodeFile)
	}
	return string(PreviewNodeOther)
}
