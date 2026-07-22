package skills

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

const (
	defaultArchiveAuditBytes         int64 = 4 << 20
	defaultArchiveAuditExpanded      int64 = 8 << 20
	defaultArchiveAuditFiles               = 5000
	defaultArchiveAuditTextFileBytes int64 = 512 << 10
)

// ArchiveAuditLimits bounds static ZIP inspection. All limits must be positive.
type ArchiveAuditLimits struct {
	MaxArchiveBytes  int64
	MaxExpandedBytes int64
	MaxFiles         int
	MaxTextFileBytes int64
}

// DefaultArchiveAuditLimits returns the bounded limits used for third-party
// Skill package inspection.
func DefaultArchiveAuditLimits() ArchiveAuditLimits {
	return ArchiveAuditLimits{
		MaxArchiveBytes:  defaultArchiveAuditBytes,
		MaxExpandedBytes: defaultArchiveAuditExpanded,
		MaxFiles:         defaultArchiveAuditFiles,
		MaxTextFileBytes: defaultArchiveAuditTextFileBytes,
	}
}

// ArchiveFile is immutable metadata about one audited regular ZIP entry.
type ArchiveFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	Text   bool   `json:"text"`
	Script bool   `json:"script"`
}

// ArchiveAudit contains only bounded package metadata. InspectArchive never
// extracts, stores, or executes archive content.
type ArchiveAudit struct {
	SHA256        string        `json:"sha256"`
	ArchiveBytes  int64         `json:"archive_bytes"`
	ExpandedBytes int64         `json:"expanded_bytes"`
	FileCount     int           `json:"file_count"`
	TextFileCount int           `json:"text_file_count"`
	Files         []ArchiveFile `json:"files"`
}

// InspectArchive validates and hashes a ZIP without extracting it to disk.
func InspectArchive(data []byte, limits ArchiveAuditLimits) (ArchiveAudit, error) {
	if err := validateArchiveAuditLimits(limits); err != nil {
		return ArchiveAudit{}, err
	}
	if int64(len(data)) > limits.MaxArchiveBytes {
		return ArchiveAudit{}, fmt.Errorf("Skill archive exceeds audit size limit")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ArchiveAudit{}, fmt.Errorf("open Skill ZIP failed: %w", err)
	}

	audit := ArchiveAudit{ArchiveBytes: int64(len(data)), Files: make([]ArchiveFile, 0, len(reader.File))}
	archiveHash := sha256.Sum256(data)
	audit.SHA256 = hex.EncodeToString(archiveHash[:])
	seen := make(map[string]struct{}, len(reader.File))
	for _, entry := range reader.File {
		entryPath, err := validateArchiveEntry(entry.Name, entry.FileInfo().Mode())
		if err != nil {
			return ArchiveAudit{}, err
		}
		key := strings.ToLower(entryPath)
		if _, exists := seen[key]; exists {
			return ArchiveAudit{}, fmt.Errorf("Skill ZIP contains duplicate normalized path: %s", entry.Name)
		}
		seen[key] = struct{}{}
		if entry.FileInfo().IsDir() {
			continue
		}
		if !entry.FileInfo().Mode().IsRegular() {
			return ArchiveAudit{}, fmt.Errorf("Skill ZIP contains unsupported file: %s", entry.Name)
		}
		audit.FileCount++
		if audit.FileCount > limits.MaxFiles {
			return ArchiveAudit{}, fmt.Errorf("Skill ZIP contains too many files")
		}
		isScript := archiveScriptPath(entryPath)
		isText := archiveTextPath(entryPath)
		if entry.UncompressedSize64 > uint64(limits.MaxExpandedBytes-audit.ExpandedBytes) {
			return ArchiveAudit{}, fmt.Errorf("Skill ZIP expanded content is too large")
		}
		if isText && entry.UncompressedSize64 > uint64(limits.MaxTextFileBytes) {
			return ArchiveAudit{}, fmt.Errorf("Skill ZIP text file exceeds audit size limit: %s", entry.Name)
		}

		fileHash, size, err := hashArchiveEntry(entry, limits.MaxExpandedBytes-audit.ExpandedBytes)
		if err != nil {
			return ArchiveAudit{}, err
		}
		if isText && size > limits.MaxTextFileBytes {
			return ArchiveAudit{}, fmt.Errorf("Skill ZIP text file exceeds audit size limit: %s", entry.Name)
		}
		audit.ExpandedBytes += size
		if audit.ExpandedBytes > limits.MaxExpandedBytes {
			return ArchiveAudit{}, fmt.Errorf("Skill ZIP expanded content is too large")
		}
		if isText {
			audit.TextFileCount++
		}
		audit.Files = append(audit.Files, ArchiveFile{Path: entryPath, Bytes: size, SHA256: fileHash, Text: isText, Script: isScript})
	}
	sort.Slice(audit.Files, func(i, j int) bool { return audit.Files[i].Path < audit.Files[j].Path })
	return audit, nil
}

func validateArchiveAuditLimits(limits ArchiveAuditLimits) error {
	if limits.MaxArchiveBytes <= 0 || limits.MaxExpandedBytes <= 0 || limits.MaxFiles <= 0 || limits.MaxTextFileBytes <= 0 {
		return fmt.Errorf("Skill archive audit limits must be positive")
	}
	return nil
}

func hashArchiveEntry(entry *zip.File, remaining int64) (string, int64, error) {
	if remaining < 0 {
		return "", 0, fmt.Errorf("Skill ZIP expanded content is too large")
	}
	reader, err := entry.Open()
	if err != nil {
		return "", 0, err
	}
	defer reader.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(reader, remaining+1))
	if err != nil {
		return "", 0, err
	}
	if written > remaining {
		return "", 0, fmt.Errorf("Skill ZIP expanded content is too large")
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}

func validateArchiveEntry(name string, mode os.FileMode) (string, error) {
	entryPath, err := normalizedArchivePath(name)
	if err != nil {
		return "", err
	}
	if mode&os.ModeSymlink != 0 {
		return "", fmt.Errorf("Skill ZIP contains symlink: %s", name)
	}
	return entryPath, nil
}

func normalizedArchivePath(name string) (string, error) {
	cleaned := path.Clean(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if cleaned == "." || cleaned == "/" {
		return "", fmt.Errorf("Skill archive contains invalid path: %s", name)
	}
	if path.IsAbs(cleaned) || isWindowsDrivePath(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("Skill archive contains invalid path: %s", name)
	}
	return cleaned, nil
}

func isWindowsDrivePath(value string) bool {
	return len(value) >= 2 && value[1] == ':' && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z'))
}

func archiveTextPath(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".txt", ".json", ".yaml", ".yml", ".toml", ".csv", ".xml", ".html", ".htm":
		return true
	default:
		return false
	}
}

func archiveScriptPath(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".sh", ".bash", ".zsh", ".fish", ".ps1", ".cmd", ".bat", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".rb", ".pl", ".php", ".lua", ".exe":
		return true
	default:
		return false
	}
}
