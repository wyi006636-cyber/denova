package workspace

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestValidateRelativePathAcceptsPortableNamesAndNormalizesWindowsSeparators(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		options PathOptions
		want    string
	}{
		{
			name:    "Chinese names and spaces",
			path:    "章节/第一 章.md",
			options: PathOptions{Intent: PathIntentExisting},
			want:    "章节/第一 章.md",
		},
		{
			name:    "long readable path",
			path:    "chapters/" + strings.Repeat("长", 80) + ".md",
			options: PathOptions{Intent: PathIntentExisting},
			want:    "chapters/" + strings.Repeat("长", 80) + ".md",
		},
		{
			name:    "Windows relative separators",
			path:    `setting\角色.md`,
			options: PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows},
			want:    "setting/角色.md",
		},
		{
			name:    "existing non NFC name remains readable",
			path:    "notes/Cafe\u0301.md",
			options: PathOptions{Intent: PathIntentExisting},
			want:    "notes/Cafe\u0301.md",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ValidateRelativePath(test.path, test.options)
			if err != nil {
				t.Fatalf("ValidateRelativePath(%q): %v", test.path, err)
			}
			if got != test.want {
				t.Fatalf("ValidateRelativePath(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestValidateRelativePathRejectsUnsafeOrUnrepresentableNamesPrecisely(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		options PathOptions
		code    ErrorCode
	}{
		{"empty", "", PathOptions{}, CodePathEmpty},
		{"absolute", "/chapters/a.md", PathOptions{}, CodePathAbsolute},
		{"drive absolute", `C:\chapters\a.md`, PathOptions{Platform: PathPlatformWindows}, CodePathDrive},
		{"drive relative", `C:chapters\a.md`, PathOptions{Platform: PathPlatformWindows}, CodePathDrive},
		{"UNC backslash", `\\server\share\a.md`, PathOptions{Platform: PathPlatformWindows}, CodePathUNC},
		{"UNC slash", "//server/share/a.md", PathOptions{Platform: PathPlatformWindows}, CodePathUNC},
		{"empty segment", "chapters//a.md", PathOptions{}, CodePathSegment},
		{"trailing empty segment", "chapters/", PathOptions{}, CodePathSegment},
		{"dot segment", "chapters/./a.md", PathOptions{}, CodePathDotSegment},
		{"parent segment", "chapters/../a.md", PathOptions{}, CodePathParentSegment},
		{"NUL", "chapters/a\x00.md", PathOptions{}, CodePathNUL},
		{"non NFC new name", "notes/Cafe\u0301.md", PathOptions{Intent: PathIntentNew}, CodePathNormalization},
		{"reserved bare", "CON", PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows}, CodePathWindowsReserved},
		{"reserved extension", "notes/con.txt", PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows}, CodePathWindowsReserved},
		{"reserved numbered", "notes/LPT9.log", PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows}, CodePathWindowsReserved},
		{"trailing dot", "notes/name.", PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows}, CodePathWindowsTrailing},
		{"trailing space", "notes/name ", PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows}, CodePathWindowsTrailing},
		{"alternate data stream", "notes/name:stream", PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows}, CodePathWindowsADS},
		{"Windows invalid character", "notes/a?.md", PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows}, CodePathWindowsCharacter},
		{"segment limit", "notes/abcdef.md", PathOptions{Intent: PathIntentNew, Limits: PathLimits{MaxSegmentBytes: 5}}, CodePathTooLong},
		{"path limit", "notes/abcdef.md", PathOptions{Intent: PathIntentNew, Limits: PathLimits{MaxPathBytes: 8}}, CodePathTooLong},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateRelativePath(test.path, test.options)
			var pathErr *PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("error = %T %v, want *PathError", err, err)
			}
			if pathErr.Code != test.code || pathErr.Path != test.path || pathErr.Value == nil {
				t.Fatalf("PathError = %#v, want code=%q path=%q with value", pathErr, test.code, test.path)
			}
		})
	}
}

func TestDetectPortablePathCollisionsIsDeterministic(t *testing.T) {
	paths := []string{
		"章节/Cafe\u0301.md",
		"章节/Caf\u00e9.md",
		"Setting/Hero.md",
		"setting/hero.md",
		"普通/文件.md",
	}
	want := []PathCollision{
		{
			Kind:  CollisionCaseFold,
			Key:   "setting/hero.md",
			Paths: []string{"Setting/Hero.md", "setting/hero.md"},
		},
		{
			Kind:  CollisionNormalization,
			Key:   "章节/Caf\u00e9.md",
			Paths: []string{"章节/Cafe\u0301.md", "章节/Caf\u00e9.md"},
		},
	}

	got := DetectPortablePathCollisions(paths)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectPortablePathCollisions() = %#v, want %#v", got, want)
	}
	if repeated := DetectPortablePathCollisions([]string{paths[3], paths[1], paths[0], paths[2]}); !reflect.DeepEqual(repeated, want) {
		t.Fatalf("collision order changed with input order: %#v", repeated)
	}
}
