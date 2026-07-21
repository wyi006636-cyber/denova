package skilldiscovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteDiscoveryArtifactsWritesSixSchemaBoundOutputs(t *testing.T) {
	root := t.TempDir()
	artifacts := artifactFixture(t)
	if err := WriteDiscoveryArtifacts(root, artifacts); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"xiaping-snapshot-manifest-v1.json",
		"xiaping-writing-candidates-v1.json",
		"xiaping-capability-proposals-v1.json",
		"xiaping-duplicate-clusters-v1.json",
		"xiaping-evidence-shortlist-v1.json",
		"XIAPING_EVIDENCE_REPORT.md",
	}
	for _, name := range paths {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	jsonPaths := make([]string, 0, 5)
	for _, name := range paths[:5] {
		jsonPaths = append(jsonPaths, filepath.Join(root, name))
	}
	if err := ValidateArtifactSchema(filepath.Join("..", "..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-discovery-v1.schema.json"), jsonPaths); err != nil {
		t.Fatal(err)
	}
}

func TestRenderEvidenceReportStatesPlatformEvidenceLimitationInBothLanguages(t *testing.T) {
	report, err := RenderEvidenceReport(artifactFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(report)
	for _, wanted := range []string{"Platform evidence is not a writing-quality result.", "平台证据不是写作质量结果。", "COMPLETE", "DATA-RICH", "EXPLORATION"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("report missing %q:\n%s", wanted, text)
		}
	}
	if strings.Contains(text, "reviewer-") || strings.Contains(text, "https://") {
		t.Fatalf("report leaks forbidden content: %s", text)
	}
}

func TestWriteDiscoveryArtifactsRollsBackAllFilesWhenMiddlePublishFails(t *testing.T) {
	root := t.TempDir()
	for _, name := range artifactNames {
		if err := os.WriteFile(filepath.Join(root, name), []byte("old-"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	previous := artifactRename
	defer func() { artifactRename = previous }()
	calls := 0
	artifactRename = func(from, to string) error {
		calls++
		if calls == 3 {
			return os.ErrPermission
		}
		return os.Rename(from, to)
	}
	if err := WriteDiscoveryArtifacts(root, artifactFixture(t)); err == nil {
		t.Fatal("expected publish failure")
	}
	for _, name := range artifactNames {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(got) != "old-"+name {
			t.Fatalf("%s=%q err=%v", name, got, err)
		}
	}
}

func TestValidateDiscoveryArtifactsRejectsAlteredEmbeddedEvidence(t *testing.T) {
	artifacts := artifactFixture(t)
	artifacts.Shortlist.Entries[0].Evidence.BayesianStarsX100++
	if err := validateDiscoveryArtifacts(artifacts); err == nil || !strings.Contains(err.Error(), "exact") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateArtifactSchemaRejectsNestedUnknownAndRemoteReference(t *testing.T) {
	root := t.TempDir()
	artifacts := artifactFixture(t)
	if err := WriteDiscoveryArtifacts(root, artifacts); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "xiaping-writing-candidates-v1.json")
	if err := os.WriteFile(path, []byte(`{"contract":"denova.xiaping-writing-candidates","version":"v1","snapshot_id":"x","candidates":[{"skill":{},"profiles":[],"capabilities":[],"unknown":true}],"provenance":{"source":"x","purpose":"x","input_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","max_bytes":262144}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	schema := filepath.Join("..", "..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-discovery-v1.schema.json")
	if err := ValidateArtifactSchema(schema, []string{path}); err == nil {
		t.Fatal("expected nested schema rejection")
	}
	remote := filepath.Join(root, "remote.json")
	if err := os.WriteFile(remote, []byte(`{"$ref":"\u0068\u0074\u0074\u0070\u0073://example.test/a"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateArtifactSchema(remote, nil); err == nil || !strings.Contains(err.Error(), "remote") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateDiscoveryArtifactsRejectsUnlinkedEvidenceAndEscapesReport(t *testing.T) {
	artifacts := artifactFixture(t)
	artifacts.Evidence[0].CapabilityID = "unknown"
	if err := validateDiscoveryArtifacts(artifacts); err == nil || !strings.Contains(err.Error(), "credible") {
		t.Fatalf("err=%v", err)
	}
	artifacts = artifactFixture(t)
	artifacts.Manifest.SnapshotID = "x\n|<script>`"
	artifacts.Shortlist.SnapshotID = artifacts.Manifest.SnapshotID
	report, err := RenderEvidenceReport(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(report), "<script>") || strings.Contains(string(report), "\n|<script>") {
		t.Fatalf("unsafe report=%s", report)
	}
}

func TestSchemaRejectsNestedCorruptionForEveryArtifactFamily(t *testing.T) {
	root := t.TempDir()
	if err := WriteDiscoveryArtifacts(root, artifactFixture(t)); err != nil {
		t.Fatal(err)
	}
	schema := filepath.Join("..", "..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-discovery-v1.schema.json")
	for _, name := range artifactNames[:5] {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, name)
			var doc map[string]any
			b, _ := os.ReadFile(path)
			_ = json.Unmarshal(b, &doc)
			doc["unexpected"] = true
			bad, _ := json.Marshal(doc)
			_ = os.WriteFile(path, bad, 0o600)
			if err := ValidateArtifactSchema(schema, []string{path}); err == nil {
				t.Fatal("accepted unknown root")
			}
		})
	}
}

func artifactFixture(t *testing.T) DiscoveryArtifacts {
	t.Helper()
	manifest := SnapshotManifest{
		Contract: "denova.xiaping-snapshot-manifest", Version: "v1", SnapshotID: "snapshot-1", Status: SnapshotComplete,
		StartedAt: "2026-07-21T00:00:00Z", CompletedAt: "2026-07-21T00:01:00Z", BaseURL: "https://catalog.example", NormalizationVersion: "v1",
		ReportedTotal: 1, UniqueSkills: 1, SkillRecordsSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Pages: []PageReceipt{{Kind: "catalog", Key: "page-1", URL: "https://catalog.example/page-1", HTTPStatus: 200, CapturedAt: "2026-07-21T00:00:00Z", SHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ItemCount: 1}},
	}
	candidates := []CandidateRecord{{Skill: SkillRecord{ID: "skill-1", Name: "文风润色", OwnerID: "owner-1", CurrentVersion: "1.0.0"}, Profiles: []string{"prose"}, Capabilities: []CapabilityMatch{{CapabilityID: "style.revise-prose", Status: MatchMatched, Evidence: []FieldEvidence{{Field: "name", Term: "文风"}}}}}}
	vectors := []EvidenceVector{{SkillID: "skill-1", CapabilityID: "style.revise-prose", PlatformDataRich: true, BayesianStarsX100: 450, Review: ReviewEvidence{EffectiveRaters: 10, SubstantiveComments: 5, AnomalyFlags: []string{}}, EvidenceCacheStatus: "EVIDENCE-CACHE-AVAILABLE"}}
	shortlist, err := BuildShortlist("snapshot-1", candidates, vectors, nil)
	if err != nil {
		t.Fatal(err)
	}
	return DiscoveryArtifacts{Manifest: manifest, Candidates: candidates, Proposals: []CapabilityProposal{}, Clusters: []DuplicateCluster{}, Evidence: vectors, Shortlist: shortlist}
}
