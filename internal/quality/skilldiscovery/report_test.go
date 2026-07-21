package skilldiscovery

import (
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
