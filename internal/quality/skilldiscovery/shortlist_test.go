package skilldiscovery

import (
	"strings"
	"testing"
)

func TestBuildShortlistKeepsThreeDataRichAndTwoExplorationSlots(t *testing.T) {
	candidates, vectors := shortlistFixture()
	got, err := BuildShortlist("snapshot-1", candidates, vectors, nil)
	if err != nil {
		t.Fatal(err)
	}
	dataRich, exploration, keptCold := 0, 0, false
	for _, entry := range got.Entries {
		if entry.CapabilityID != "style.revise-prose" {
			continue
		}
		if entry.Lane == LaneDataRich {
			dataRich++
		}
		if entry.Lane == LaneExploration {
			exploration++
		}
		if entry.SkillID == "cold-but-distinct" && entry.Lane == LaneExploration {
			keptCold = true
		}
	}
	if dataRich != 3 || exploration != 2 || !keptCold {
		t.Fatalf("entries = %#v", got.Entries)
	}
}

func TestBuildShortlistRejectsPartialSnapshot(t *testing.T) {
	_, err := BuildShortlistFromSnapshot(LocalSnapshot{Manifest: SnapshotManifest{Status: SnapshotPartial}}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "complete snapshot") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildShortlistUsesClusterRepresentativeAndRejectsNonWritingMatches(t *testing.T) {
	candidates := []CandidateRecord{
		{Skill: SkillRecord{ID: "representative", OwnerID: "owner-a"}, Capabilities: []CapabilityMatch{{CapabilityID: "cap", Status: MatchMatched, Evidence: []FieldEvidence{{Field: "name", Term: "write"}}}}},
		{Skill: SkillRecord{ID: "duplicate", OwnerID: "owner-b"}, Capabilities: []CapabilityMatch{{CapabilityID: "cap", Status: MatchMatched, Evidence: []FieldEvidence{{Field: "name", Term: "write"}}}}},
		{Skill: SkillRecord{ID: "ambiguous", OwnerID: "owner-c"}, Capabilities: []CapabilityMatch{{CapabilityID: "cap", Status: MatchAmbiguous, Evidence: []FieldEvidence{{Field: "name", Term: "write"}}}}},
		{Skill: SkillRecord{ID: "excluded", OwnerID: "owner-d", Name: "video"}, Capabilities: []CapabilityMatch{{CapabilityID: "cap", Status: MatchMatched, Evidence: []FieldEvidence{{Field: "name", Term: "write"}}}}},
	}
	vectors := []EvidenceVector{
		{SkillID: "representative", CapabilityID: "cap", PlatformDataRich: true},
		{SkillID: "duplicate", CapabilityID: "cap", PlatformDataRich: true},
		{SkillID: "ambiguous", CapabilityID: "cap"},
		{SkillID: "excluded", CapabilityID: "cap", PlatformDataRich: true},
	}
	clusters := []DuplicateCluster{{ClusterID: "cluster-a", RepresentativeID: "representative", MemberIDs: []string{"duplicate", "representative"}, Reasons: []string{"same_author"}}}
	got, err := BuildShortlist("snapshot-1", candidates, vectors, clusters)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 || got.Entries[0].SkillID != "representative" || got.Entries[1].SkillID != "ambiguous" {
		t.Fatalf("entries = %#v", got.Entries)
	}
	if !strings.Contains(strings.Join(got.Entries[0].Reasons, ","), "cluster_members:duplicate,representative") {
		t.Fatalf("cluster references missing: %#v", got.Entries[0].Reasons)
	}
}

func TestBuildShortlistRejectsDuplicateOrUnknownEvidenceAndProducesAllCoreGaps(t *testing.T) {
	candidates := []CandidateRecord{{Skill: SkillRecord{ID: "a", Name: "prose"}, Capabilities: []CapabilityMatch{{CapabilityID: "style.revise-prose", Status: MatchMatched, Evidence: []FieldEvidence{{Field: "name", Term: "prose"}}}}}}
	vector := EvidenceVector{SkillID: "a", CapabilityID: "style.revise-prose"}
	if _, err := BuildShortlist("snapshot", candidates, []EvidenceVector{vector, vector}, nil); err == nil || !strings.Contains(err.Error(), "duplicate evidence") { t.Fatalf("err=%v", err) }
	got, err := BuildShortlist("snapshot", candidates, []EvidenceVector{vector}, nil)
	if err != nil { t.Fatal(err) }
	if len(got.Gaps) != len(CoreCapabilityIDs) { t.Fatalf("gaps=%d", len(got.Gaps)) }
}

func TestBuildShortlistRejectsMalformedClusters(t *testing.T) {
	candidates := []CandidateRecord{{Skill: SkillRecord{ID: "a"}}}
	_, err := BuildShortlist("snapshot", candidates, nil, []DuplicateCluster{{ClusterID: "c", RepresentativeID: "missing", MemberIDs: []string{"a"}}})
	if err == nil || !strings.Contains(err.Error(), "representative") { t.Fatalf("err=%v", err) }
}

func shortlistFixture() ([]CandidateRecord, []EvidenceVector) {
	ids := []string{"rich-a", "rich-b", "rich-c", "cold-but-distinct", "cold-second"}
	candidates := make([]CandidateRecord, 0, len(ids))
	vectors := make([]EvidenceVector, 0, len(ids))
	for index, id := range ids {
		candidates = append(candidates, CandidateRecord{Skill: SkillRecord{ID: id, OwnerID: "owner-" + id, Name: id}, Capabilities: []CapabilityMatch{{CapabilityID: "style.revise-prose", Status: MatchMatched, Evidence: []FieldEvidence{{Field: "name", Term: "文风"}}}}})
		vectors = append(vectors, EvidenceVector{SkillID: id, CapabilityID: "style.revise-prose", DownloadPercentile: float64(5-index) / 5, BayesianStarsX100: float64(450 - index), Review: ReviewEvidence{EffectiveRaters: 20 - index, SubstantiveComments: 8 - index}, PlatformDataRich: index < 3, MaturityVersionCount: index + 1, EvidenceCacheStatus: "AVAILABLE"})
	}
	return candidates, vectors
}
