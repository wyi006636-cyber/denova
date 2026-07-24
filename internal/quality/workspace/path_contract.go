package workspace

import (
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// PathIntent distinguishes existing names that must remain readable from new
// destination names that must satisfy the portable v1 storage contract.
type PathIntent string

const (
	PathIntentExisting PathIntent = "existing"
	PathIntentNew      PathIntent = "new"
)

// PathPlatform selects platform-specific destination representation checks.
type PathPlatform string

const (
	PathPlatformPortable PathPlatform = "portable"
	PathPlatformWindows  PathPlatform = "windows"
)

// PathLimits describes target-filesystem limits discovered by the caller.
// Zero values mean that no synthetic limit is imposed.
type PathLimits struct {
	MaxPathBytes    int
	MaxSegmentBytes int
}

// PathOptions controls validation without changing existing path bytes.
type PathOptions struct {
	Intent   PathIntent
	Platform PathPlatform
	Limits   PathLimits
}

// ValidateRelativePath validates a workspace-relative stored path. Windows
// separators are normalized only after absolute, drive, and UNC rejection.
func ValidateRelativePath(raw string, options PathOptions) (string, error) {
	fail := func(code ErrorCode, field string, value any, message string) (string, error) {
		return "", &PathError{Code: code, Path: raw, Field: field, Value: value, Message: message}
	}
	if raw == "" {
		return fail(CodePathEmpty, "path", raw, "path is required")
	}
	if strings.ContainsRune(raw, '\x00') {
		return fail(CodePathNUL, "path", raw, "NUL is not allowed")
	}
	if strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, "//") {
		return fail(CodePathUNC, "path", raw, "UNC paths are not workspace-relative")
	}
	if hasDrivePrefix(raw) {
		return fail(CodePathDrive, "path", raw[:2], "drive-prefixed paths are not workspace-relative")
	}
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) {
		return fail(CodePathAbsolute, "path", raw, "absolute paths are not allowed")
	}

	normalized := raw
	if strings.ContainsRune(normalized, '\\') {
		if options.Platform != PathPlatformWindows {
			return fail(CodePathSeparator, "path", `\`, "stored paths use forward slashes")
		}
		normalized = strings.ReplaceAll(normalized, `\`, "/")
	}
	segments := strings.Split(normalized, "/")
	for index, segment := range segments {
		field := "segments"
		if segment == "" {
			return fail(CodePathSegment, field, index, "empty path segments are not allowed")
		}
		if segment == "." {
			return fail(CodePathDotSegment, field, segment, "dot path segments are not allowed")
		}
		if segment == ".." {
			return fail(CodePathParentSegment, field, segment, "parent path segments are not allowed")
		}
		if options.Intent == PathIntentNew && !norm.NFC.IsNormalString(segment) {
			return fail(CodePathNormalization, field, segment, "new path segments must use NFC")
		}
		if options.Platform == PathPlatformWindows && options.Intent == PathIntentNew {
			if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
				return fail(CodePathWindowsTrailing, field, segment, "Windows names cannot end in a dot or space")
			}
			if strings.ContainsRune(segment, ':') {
				return fail(CodePathWindowsADS, field, segment, "alternate data stream syntax is not allowed")
			}
			if strings.ContainsAny(segment, `<>"|?*`) {
				return fail(CodePathWindowsCharacter, field, segment, "path segment is not representable on Windows")
			}
			if isWindowsReservedName(segment) {
				return fail(CodePathWindowsReserved, field, segment, "reserved Windows device name")
			}
		}
		if limit := options.Limits.MaxSegmentBytes; limit > 0 && len(segment) > limit {
			return fail(CodePathTooLong, field, map[string]int{"bytes": len(segment), "limit": limit}, "path segment exceeds target filesystem limit")
		}
	}
	if limit := options.Limits.MaxPathBytes; limit > 0 && len(normalized) > limit {
		return fail(CodePathTooLong, "path", map[string]int{"bytes": len(normalized), "limit": limit}, "path exceeds target filesystem limit")
	}
	if !utf8.ValidString(normalized) {
		return fail(CodePathNormalization, "path", raw, "path must contain valid UTF-8")
	}
	return normalized, nil
}

func hasDrivePrefix(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':'
}

func isWindowsReservedName(segment string) bool {
	base := segment
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	base = strings.ToUpper(strings.TrimRight(base, " ."))
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(base) == 4 && ((strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9')
}

// CollisionKind identifies a cross-platform name collision.
type CollisionKind string

const (
	CollisionCaseFold      CollisionKind = "case_fold"
	CollisionNormalization CollisionKind = "normalization"
)

// PathCollision records all original names that collapse to one portable key.
type PathCollision struct {
	Kind  CollisionKind
	Key   string
	Paths []string
}

// DetectPortablePathCollisions finds NFC and Unicode case-fold collisions in
// stable order while preserving every original path.
func DetectPortablePathCollisions(paths []string) []PathCollision {
	exact := make(map[string]struct{}, len(paths))
	groups := map[string][]string{}
	for _, path := range paths {
		if _, exists := exact[path]; exists {
			continue
		}
		exact[path] = struct{}{}
		key := cases.Fold().String(norm.NFC.String(path))
		groups[key] = append(groups[key], path)
	}

	result := make([]PathCollision, 0)
	for foldKey, members := range groups {
		if len(members) < 2 {
			continue
		}
		sort.Strings(members)
		normalized := map[string]struct{}{}
		for _, member := range members {
			normalized[norm.NFC.String(member)] = struct{}{}
		}
		kind := CollisionCaseFold
		key := foldKey
		if len(normalized) == 1 {
			kind = CollisionNormalization
			key = norm.NFC.String(members[0])
		}
		result = append(result, PathCollision{Kind: kind, Key: key, Paths: append([]string(nil), members...)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Key < result[j].Key
	})
	return result
}
