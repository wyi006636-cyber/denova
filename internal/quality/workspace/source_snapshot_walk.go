package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const projectionSourceReadDirBatch = 256

func collectProjectionSourceDocuments(ctx context.Context, workspace, activeRoot, profile string, approved map[string]struct{}, limits SourceSnapshotLimits) ([]SourceDocument, map[string]struct{}, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, nil, &SourceSnapshotError{Path: workspace, Field: "workspace", Value: workspace, Message: "workspace root handle cannot be opened", Err: err}
	}
	defer root.Close()

	documents := make([]SourceDocument, 0)
	seenApproved := make(map[string]struct{}, len(approved))
	directories := []string{"."}
	var visitedEntries int
	var visitedPathBytes int64
	var totalBytes int64

	for len(directories) > 0 {
		directoryPath := directories[len(directories)-1]
		directories = directories[:len(directories)-1]
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		directory, err := openBoundProjectionDirectory(root, directoryPath)
		if err != nil {
			return nil, nil, err
		}
		for {
			entries, readErr := directory.ReadDir(projectionSourceReadDirBatch)
			for _, entry := range entries {
				relative := entry.Name()
				if directoryPath != "." {
					relative = filepath.ToSlash(filepath.Join(directoryPath, entry.Name()))
				}
				visitedEntries++
				if visitedEntries > limits.MaxEntries {
					directory.Close()
					return nil, nil, sourceLimitError(relative, "limits.max_entries", visitedEntries, limits.MaxEntries)
				}
				if int64(len(relative)) > limits.MaxPathBytes-visitedPathBytes {
					directory.Close()
					return nil, nil, sourceLimitError(relative, "limits.max_path_bytes", visitedPathBytes+int64(len(relative)), limits.MaxPathBytes)
				}
				visitedPathBytes += int64(len(relative))
				if _, err := ValidateRelativePath(relative, PathOptions{Intent: PathIntentExisting}); err != nil {
					directory.Close()
					return nil, nil, err
				}
				if entry.Type()&os.ModeSymlink != 0 {
					directory.Close()
					return nil, nil, &SourceSnapshotError{Path: relative, Field: "source.node_type", Value: entry.Type().String(), Message: "reparse-point sources are not allowed"}
				}
				if entry.IsDir() {
					skip, err := skipProjectionSourceDirectory(relative, activeRoot)
					if err != nil {
						directory.Close()
						return nil, nil, err
					}
					if !skip {
						directories = append(directories, relative)
					}
					continue
				}
				if !entry.Type().IsRegular() {
					directory.Close()
					return nil, nil, &SourceSnapshotError{Path: relative, Field: "source.node_type", Value: entry.Type().String(), Message: "source must be a regular file"}
				}

				classification, err := ClassifyPath(relative)
				if err != nil {
					directory.Close()
					return nil, nil, err
				}
				include := classification.Category == CategoryFormalAuthoritative
				if classification.Category == CategoryReviewArtifact {
					_, include = approved[relative]
					if include {
						seenApproved[relative] = struct{}{}
					}
				}
				if !include || !projectionTextPath(relative) {
					continue
				}
				document, err := readProjectionSourceDocument(root, workspace, relative, profile, limits.MaxFileBytes)
				if err != nil {
					directory.Close()
					return nil, nil, err
				}
				if len(documents) >= limits.MaxFiles {
					directory.Close()
					return nil, nil, sourceLimitError(relative, "limits.max_files", len(documents)+1, limits.MaxFiles)
				}
				if document.Size > limits.MaxTotalBytes-totalBytes {
					directory.Close()
					return nil, nil, sourceLimitError(relative, "limits.max_total_bytes", totalBytes+document.Size, limits.MaxTotalBytes)
				}
				totalBytes += document.Size
				documents = append(documents, document)
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				directory.Close()
				return nil, nil, &SourceSnapshotError{Path: directoryPath, Field: "source.directory", Value: directoryPath, Message: "source directory read failed", Err: readErr}
			}
		}
		if err := directory.Close(); err != nil {
			return nil, nil, &SourceSnapshotError{Path: directoryPath, Field: "source.directory", Value: directoryPath, Message: "source directory close failed", Err: err}
		}
	}
	return documents, seenApproved, nil
}

func openBoundProjectionDirectory(root *os.Root, relative string) (*os.File, error) {
	pathInfo, err := root.Lstat(filepath.FromSlash(relative))
	if err != nil {
		return nil, &SourceSnapshotError{Path: relative, Field: "source.directory", Value: relative, Message: "source directory identity cannot be inspected", Err: err}
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() {
		return nil, &SourceSnapshotError{Path: relative, Field: "source.node_type", Value: pathInfo.Mode().String(), Message: "source directory must not be a reparse point"}
	}
	directory, err := root.Open(filepath.FromSlash(relative))
	if err != nil {
		return nil, &SourceSnapshotError{Path: relative, Field: "source.directory", Value: relative, Message: "source directory cannot be opened", Err: err}
	}
	handleInfo, err := directory.Stat()
	if err != nil || !handleInfo.IsDir() || !os.SameFile(pathInfo, handleInfo) {
		directory.Close()
		if err == nil {
			err = errors.New("source directory identity changed while opening")
		}
		return nil, &SourceSnapshotError{Path: relative, Field: "source.directory", Value: relative, Message: "source directory identity cannot be pinned", Err: err}
	}
	return directory, nil
}

func readProjectionSourceDocument(root *os.Root, workspace, relative, profile string, maxFileBytes int64) (SourceDocument, error) {
	canonicalPath, err := ResolveCanonicalPath(workspace, relative, CanonicalOptions{})
	if err != nil {
		return SourceDocument{}, err
	}
	info, err := root.Lstat(filepath.FromSlash(relative))
	if err != nil {
		return SourceDocument{}, &SourceSnapshotError{Path: relative, Field: "source.identity", Value: relative, Message: "source identity cannot be inspected", Err: err}
	}
	if !info.Mode().IsRegular() {
		return SourceDocument{}, &SourceSnapshotError{Path: relative, Field: "source.node_type", Value: info.Mode().String(), Message: "source must be a regular file"}
	}
	if info.Size() > maxFileBytes {
		return SourceDocument{}, sourceLimitError(relative, "limits.max_file_bytes", info.Size(), maxFileBytes)
	}
	content, err := readProjectionSourceFile(root, workspace, canonicalPath, relative, info, maxFileBytes)
	if err != nil {
		return SourceDocument{}, err
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return SourceDocument{}, &SourceSnapshotError{Path: relative, Field: "source.content", Value: "non_text", Message: "Projection sources must be valid UTF-8 text without NUL"}
	}
	return SourceDocument{
		ID:           projectionSourceDocumentID(relative),
		Path:         relative,
		RevisionHash: sourceSHA256(content),
		Profile:      profile,
		Kind:         projectionSourceKind(relative),
		Size:         int64(len(content)),
		Content:      append([]byte(nil), content...),
	}, nil
}
