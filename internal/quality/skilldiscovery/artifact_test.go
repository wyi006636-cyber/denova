package skilldiscovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSnapshotManifestRejectsCompletePageGap(t *testing.T) {
	manifest := validSnapshotManifest()
	manifest.Status = SnapshotPartial
	manifest.Failures = nil
	if err := ValidateSnapshotManifest(manifest); err == nil || !strings.Contains(err.Error(), "partial snapshot requires failures") {
		t.Fatalf("ValidateSnapshotManifest() error = %v", err)
	}
}

func TestValidateSnapshotManifestRejectsFailuresForCompleteSnapshot(t *testing.T) {
	manifest := validSnapshotManifest()
	manifest.Failures = []SnapshotFailure{{Kind: "catalog", Key: "2", Disposition: "retry", Message: "synthetic failure"}}
	if err := ValidateSnapshotManifest(manifest); err == nil || !strings.Contains(err.Error(), "complete snapshot must not include failures") {
		t.Fatalf("ValidateSnapshotManifest() error = %v", err)
	}
}

func TestValidateSkillRecordsRejectsDuplicateID(t *testing.T) {
	records := []SkillRecord{{ID: "skill-1", Name: "甲"}, {ID: "skill-1", Name: "乙"}}
	if err := ValidateSkillRecords(records); err == nil || !strings.Contains(err.Error(), "duplicate skill id") {
		t.Fatalf("ValidateSkillRecords() error = %v", err)
	}
}

func TestWriteJSONArtifactRejectsSignedURL(t *testing.T) {
	value := map[string]string{"avatar": "https://example.test/a.png?sign=secret"}
	err := WriteJSONArtifact(filepath.Join(t.TempDir(), "bad.json"), value)
	if err == nil || !strings.Contains(err.Error(), "signed URL") {
		t.Fatalf("WriteJSONArtifact() error = %v", err)
	}
}

func TestWriteJSONArtifactRejectsSensitiveValues(t *testing.T) {
	err := WriteJSONArtifact(filepath.Join(t.TempDir(), "bad.json"), map[string]string{"authorization": "Bearer synthetic-secret"})
	if err == nil || !strings.Contains(err.Error(), "sensitive value") {
		t.Fatalf("WriteJSONArtifact() error = %v", err)
	}
}

func TestLoadStrictJSONRejectsUnknownAndTrailingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strict.json")
	if err := os.WriteFile(path, []byte(`{"id":"skill-1","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var record SkillRecord
	if err := LoadStrictJSON(path, &record); err == nil {
		t.Fatal("LoadStrictJSON() accepted unknown field")
	}
	if err := os.WriteFile(path, []byte(`{"id":"skill-1"} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadStrictJSON(path, &record); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("LoadStrictJSON() error = %v", err)
	}
}

func TestStableSHA256UsesLowercasePrefixedDigest(t *testing.T) {
	hash := StableSHA256(map[string]string{"b": "two", "a": "one"})
	if !strings.HasPrefix(hash, "sha256:") || len(hash) != len("sha256:")+64 || hash != strings.ToLower(hash) {
		t.Fatalf("StableSHA256() = %q", hash)
	}
}

func TestStableSHA256CanonicalizesSkillRecordOrder(t *testing.T) {
	first := []SkillRecord{{ID: "skill-b", Name: "B"}, {ID: "skill-a", Name: "A"}}
	second := []SkillRecord{{ID: "skill-a", Name: "A"}, {ID: "skill-b", Name: "B"}}
	if got, want := StableSHA256(first), StableSHA256(second); got != want {
		t.Fatalf("StableSHA256() = %q, want canonical skill-record hash %q", got, want)
	}
}

func validSnapshotManifest() SnapshotManifest {
	return SnapshotManifest{
		Contract: "denova.xiaping-snapshot-manifest", Version: "v1",
		SnapshotID: "snapshot-0123456789abcdef", Status: SnapshotComplete,
		StartedAt: "2026-07-21T00:00:00Z", CompletedAt: "2026-07-21T00:01:00Z",
		BaseURL: "https://example.test", NormalizationVersion: "v1",
		ReportedTotal: 1, UniqueSkills: 1,
		Pages:              []PageReceipt{{Kind: "catalog", Key: "1", URL: "https://example.test/api/skills?limit=50&page=1", HTTPStatus: 200, CapturedAt: "2026-07-21T00:00:30Z", SHA256: "sha256:" + strings.Repeat("a", 64), ItemCount: 1}},
		SkillRecordsSHA256: "sha256:" + strings.Repeat("b", 64),
	}
}
