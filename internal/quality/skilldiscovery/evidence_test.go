package skilldiscovery

import "testing"

func TestDownloadPercentilesUsesCapabilityPoolsAndTieRanks(t *testing.T) {
	candidates := []CandidateRecord{
		{Skill: SkillRecord{ID: "a", Downloads: 10}, Capabilities: []CapabilityMatch{{CapabilityID: "continuity.review-facts", Status: MatchMatched}}},
		{Skill: SkillRecord{ID: "b", Downloads: 10}, Capabilities: []CapabilityMatch{{CapabilityID: "continuity.review-facts", Status: MatchMatched}}},
		{Skill: SkillRecord{ID: "c", Downloads: 30}, Capabilities: []CapabilityMatch{{CapabilityID: "continuity.review-facts", Status: MatchMatched}}},
	}
	got := DownloadPercentiles(candidates)
	if got["continuity.review-facts"]["a"] != 0.25 || got["continuity.review-facts"]["b"] != 0.25 || got["continuity.review-facts"]["c"] != 1 {
		t.Fatalf("percentiles = %#v", got)
	}
}

func TestBayesianAdjustedStarsUsesFrozenFormula(t *testing.T) {
	got := BayesianAdjustedStars(500, 10, 400, 5)
	if got != 466.6666666666667 {
		t.Fatalf("adjusted stars = %v", got)
	}
}

func TestBuildEvidenceVectorsUsesExplorationForMissingEvidence(t *testing.T) {
	candidate := CandidateRecord{Skill: SkillRecord{ID: "a", Downloads: 100, VersionCount: 2}, Capabilities: []CapabilityMatch{{CapabilityID: "continuity.review-facts", Status: MatchMatched}}}
	vectors := BuildEvidenceVectors([]CandidateRecord{candidate}, map[string]ReviewEvidence{"a": {EvidenceCacheStatus: "EVIDENCE-CACHE-MISSING"}}, nil)
	if len(vectors) != 1 || vectors[0].PlatformDataRich || vectors[0].EvidenceCacheStatus != "EVIDENCE-CACHE-MISSING" {
		t.Fatalf("vectors = %#v", vectors)
	}
}
