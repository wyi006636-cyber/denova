package skilldiscovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	signedQueryPattern = regexp.MustCompile(`(?i)[?&](?:sign|signature|sig|x-amz-(?:algorithm|credential|date|expires|security-token|signature)|access[_-]?token|token|policy)=[^&#]*`)
	bearerPattern      = regexp.MustCompile(`(?i)bearer\s+[^\s"\\]+`)
	secretFieldPattern = regexp.MustCompile(`(?i)"(?:api[_-]?key|secret|password|access[_-]?token|authorization)"\s*:\s*"[^"]+"`)
	privateKeyPattern  = regexp.MustCompile(`(?i)-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----`)
	sha256Pattern      = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

// StableSHA256 returns a prefixed lowercase SHA-256 digest of canonical Go JSON.
func StableSHA256(value any) string {
	if records, ok := value.([]SkillRecord); ok {
		canonical := append([]SkillRecord(nil), records...)
		sort.Slice(canonical, func(i, j int) bool { return canonical[i].ID < canonical[j].ID })
		value = canonical
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		encoded = []byte("null")
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// LoadStrictJSON decodes exactly one JSON value and rejects unknown fields.
func LoadStrictJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open JSON artifact: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON artifact: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
}

// WriteJSONArtifact writes a safe, indented JSON artifact atomically within its directory.
func WriteJSONArtifact(path string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal JSON artifact: %w", err)
	}
	if err := rejectSensitiveArtifact(string(encoded)); err != nil {
		return err
	}
	indented, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("indent JSON artifact: %w", err)
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".skilldiscovery-*.json")
	if err != nil {
		return fmt.Errorf("create artifact temp file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(append(indented, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write artifact temp file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close artifact temp file: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("rename artifact temp file: %w", err)
	}
	return nil
}

func rejectSensitiveArtifact(encoded string) error {
	if signedQueryPattern.MatchString(encoded) {
		return fmt.Errorf("refusing artifact containing signed URL")
	}
	if bearerPattern.MatchString(encoded) || secretFieldPattern.MatchString(encoded) || privateKeyPattern.MatchString(encoded) {
		return fmt.Errorf("refusing artifact containing sensitive value")
	}
	return nil
}

// ValidateSnapshotManifest checks the snapshot completeness and hash invariants.
func ValidateSnapshotManifest(manifest SnapshotManifest) error {
	if manifest.Contract == "" || manifest.Version == "" || manifest.SnapshotID == "" || manifest.BaseURL == "" || manifest.NormalizationVersion == "" {
		return fmt.Errorf("snapshot manifest has required empty field")
	}
	if manifest.Status != SnapshotComplete && manifest.Status != SnapshotPartial {
		return fmt.Errorf("invalid snapshot status %q", manifest.Status)
	}
	if manifest.Status == SnapshotPartial && len(manifest.Failures) == 0 {
		return fmt.Errorf("partial snapshot requires failures")
	}
	if manifest.Status == SnapshotComplete && len(manifest.Failures) != 0 {
		return fmt.Errorf("complete snapshot must not include failures")
	}
	if manifest.ReportedTotal < 0 || manifest.UniqueSkills < 0 {
		return fmt.Errorf("snapshot totals must not be negative")
	}
	if _, err := time.Parse(time.RFC3339, manifest.StartedAt); err != nil {
		return fmt.Errorf("invalid started_at: %w", err)
	}
	if _, err := time.Parse(time.RFC3339, manifest.CompletedAt); err != nil {
		return fmt.Errorf("invalid completed_at: %w", err)
	}
	if !isSHA256(manifest.SkillRecordsSHA256) {
		return fmt.Errorf("invalid skill_records_sha256")
	}
	if manifest.PreviousSnapshotSHA256 != "" && !isSHA256(manifest.PreviousSnapshotSHA256) {
		return fmt.Errorf("invalid previous_snapshot_sha256")
	}
	seen := make(map[string]struct{}, len(manifest.Pages))
	for _, page := range manifest.Pages {
		if page.Kind == "" || page.Key == "" || page.URL == "" || page.CapturedAt == "" {
			return fmt.Errorf("page receipt has required empty field")
		}
		pageID := page.Kind + "\x00" + page.Key
		if _, exists := seen[pageID]; exists {
			return fmt.Errorf("duplicate page receipt %s/%s", page.Kind, page.Key)
		}
		seen[pageID] = struct{}{}
		if page.HTTPStatus == 0 && page.Error == "" {
			return fmt.Errorf("request-failure page receipt requires an error")
		}
		if (page.HTTPStatus != 0 && (page.HTTPStatus < 100 || page.HTTPStatus > 599)) || page.ItemCount < 0 {
			return fmt.Errorf("invalid page receipt %s/%s", page.Kind, page.Key)
		}
		if _, err := time.Parse(time.RFC3339, page.CapturedAt); err != nil {
			return fmt.Errorf("invalid page captured_at: %w", err)
		}
		if !isSHA256(page.SHA256) {
			return fmt.Errorf("invalid page sha256")
		}
	}
	for _, failure := range manifest.Failures {
		if failure.Kind == "" || failure.Key == "" || failure.Disposition == "" || failure.Message == "" {
			return fmt.Errorf("snapshot failure has required empty field")
		}
	}
	return nil
}

// ValidateSkillRecords verifies that a normalized catalog has stable, unique IDs.
func ValidateSkillRecords(records []SkillRecord) error {
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if strings.TrimSpace(record.ID) == "" {
			return fmt.Errorf("skill id is required")
		}
		if _, exists := seen[record.ID]; exists {
			return fmt.Errorf("duplicate skill id %q", record.ID)
		}
		seen[record.ID] = struct{}{}
	}
	return nil
}

func isSHA256(value string) bool { return sha256Pattern.MatchString(value) }
