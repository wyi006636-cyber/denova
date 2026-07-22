package skills

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestInspectArchiveReturnsSortedStableMetadata(t *testing.T) {
	data := makeAuditZip(t, []auditZipEntry{
		{name: "skill/README.md", body: "# Read me\n"},
		{name: "skill/assets/data.json", body: `{"ok":true}`},
		{name: "skill/SKILL.md", body: "---\nname: test\n---\n"},
	})
	audit, err := InspectArchive(data, DefaultArchiveAuditLimits())
	if err != nil {
		t.Fatalf("InspectArchive() error = %v", err)
	}
	wantHash := sha256.Sum256(data)
	if audit.SHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("SHA256 = %q, want %x", audit.SHA256, wantHash)
	}
	if audit.ArchiveBytes != int64(len(data)) || audit.FileCount != 3 || audit.TextFileCount != 3 {
		t.Fatalf("unexpected counts: %#v", audit)
	}
	if got := inventoryPaths(audit.Files); strings.Join(got, ",") != "skill/README.md,skill/SKILL.md,skill/assets/data.json" {
		t.Fatalf("sorted paths = %v", got)
	}
	for _, file := range audit.Files {
		if file.SHA256 == "" || !file.Text || file.Script {
			t.Fatalf("file metadata = %#v", file)
		}
	}
}

func TestInspectArchiveRejectsUnsafePathsAndSymlinks(t *testing.T) {
	tests := []struct {
		name  string
		entry auditZipEntry
	}{
		{name: "traversal", entry: auditZipEntry{name: "../escape.md", body: "x"}},
		{name: "absolute", entry: auditZipEntry{name: "/escape.md", body: "x"}},
		{name: "windows drive", entry: auditZipEntry{name: "C:/escape.md", body: "x"}},
		{name: "backslash traversal", entry: auditZipEntry{name: `dir\\..\\..\\escape.md`, body: "x"}},
		{name: "symlink", entry: auditZipEntry{name: "skill/link", body: "target", symlink: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := InspectArchive(makeAuditZip(t, []auditZipEntry{tt.entry}), DefaultArchiveAuditLimits()); err == nil {
				t.Fatalf("InspectArchive() accepted %s", tt.name)
			}
		})
	}
}

func TestInspectArchiveRejectsDuplicateNormalizedPaths(t *testing.T) {
	for _, entries := range [][]auditZipEntry{
		{{name: "skill/a.md", body: "a"}, {name: "skill/./a.md", body: "b"}},
		{{name: "skill/a.md", body: "a"}, {name: "SKILL/A.md", body: "b"}},
	} {
		if _, err := InspectArchive(makeAuditZip(t, entries), DefaultArchiveAuditLimits()); err == nil {
			t.Fatalf("InspectArchive() accepted duplicate normalized paths: %#v", entries)
		}
	}
}

func TestInspectArchiveEnforcesBoundsAndRejectsScripts(t *testing.T) {
	base := DefaultArchiveAuditLimits()
	tests := []struct {
		name   string
		data   []byte
		limits ArchiveAuditLimits
	}{
		{name: "archive bytes", data: bytes.Repeat([]byte("x"), 32), limits: ArchiveAuditLimits{MaxArchiveBytes: 1, MaxExpandedBytes: base.MaxExpandedBytes, MaxFiles: base.MaxFiles, MaxTextFileBytes: base.MaxTextFileBytes}},
		{name: "expanded bytes", data: makeAuditZip(t, []auditZipEntry{{name: "skill/a.md", body: "12345"}}), limits: ArchiveAuditLimits{MaxArchiveBytes: base.MaxArchiveBytes, MaxExpandedBytes: 4, MaxFiles: base.MaxFiles, MaxTextFileBytes: base.MaxTextFileBytes}},
		{name: "file count", data: makeAuditZip(t, []auditZipEntry{{name: "skill/a.md", body: "a"}, {name: "skill/b.md", body: "b"}}), limits: ArchiveAuditLimits{MaxArchiveBytes: base.MaxArchiveBytes, MaxExpandedBytes: base.MaxExpandedBytes, MaxFiles: 1, MaxTextFileBytes: base.MaxTextFileBytes}},
		{name: "text file bytes", data: makeAuditZip(t, []auditZipEntry{{name: "skill/a.md", body: "12345"}}), limits: ArchiveAuditLimits{MaxArchiveBytes: base.MaxArchiveBytes, MaxExpandedBytes: base.MaxExpandedBytes, MaxFiles: base.MaxFiles, MaxTextFileBytes: 4}},
		{name: "script", data: makeAuditZip(t, []auditZipEntry{{name: "skill/install.sh", body: "echo unsafe"}}), limits: base},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := InspectArchive(tt.data, tt.limits); err == nil {
				t.Fatalf("InspectArchive() accepted %s", tt.name)
			}
		})
	}
}

func TestInspectArchiveEnforcesDefaultLimits(t *testing.T) {
	limits := DefaultArchiveAuditLimits()
	tests := []struct {
		name string
		data []byte
	}{
		{name: "archive bytes", data: makeStoredAuditZip(t, "skill/blob.bin", deterministicBytes(int(limits.MaxArchiveBytes)))},
		{name: "expanded bytes", data: makeAuditZip(t, []auditZipEntry{{name: "skill/blob.bin", body: strings.Repeat("x", int(limits.MaxExpandedBytes+1))}})},
		{name: "file count", data: makeManyAuditZip(t, limits.MaxFiles+1)},
		{name: "text file bytes", data: makeAuditZip(t, []auditZipEntry{{name: "skill/a.md", body: strings.Repeat("x", int(limits.MaxTextFileBytes+1))}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := InspectArchive(tt.data, limits); err == nil {
				t.Fatalf("InspectArchive() accepted %s above its default bound", tt.name)
			}
		})
	}
}

type auditZipEntry struct {
	name    string
	body    string
	symlink bool
}

func makeAuditZip(t *testing.T, entries []auditZipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name}
		if entry.symlink {
			header.SetMode(os.ModeSymlink | 0o777)
		}
		out, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeStoredAuditZip(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	out, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeManyAuditZip(t *testing.T, count int) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for i := 0; i < count; i++ {
		out, err := writer.Create("skill/file" + strconv.Itoa(i) + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func deterministicBytes(length int) []byte {
	out := make([]byte, length)
	for i := range out {
		out[i] = byte((i*31 + i/251) % 251)
	}
	return out
}

func inventoryPaths(files []ArchiveFile) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}
