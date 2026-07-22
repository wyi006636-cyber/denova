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

func TestBuildEvidenceVectorsRetainsMediaOnlyCandidateForAudit(t *testing.T) {
	candidate := CandidateRecord{
		Skill: SkillRecord{ID: "media", Name: "video storyboard", Downloads: 100},
		Capabilities: []CapabilityMatch{{
			CapabilityID: "outline.build-short-structure",
			Status:       MatchMatched,
			Evidence:     []FieldEvidence{{Field: "name", Term: "storyboard"}},
		}},
	}
	if vectors := BuildEvidenceVectors([]CandidateRecord{candidate}, nil, nil); len(vectors) != 1 || vectors[0].EvidenceCacheStatus != "EVIDENCE-CACHE-MISSING" {
		t.Fatalf("media-only candidate vectors: %#v", vectors)
	}
}

func TestBuildEvidenceVectorsKeepsMediaOnlyAuditOutOfScoringPool(t *testing.T) {
	writing := CandidateRecord{Skill: SkillRecord{ID: "writing", Name: "prose", Downloads: 100}, Capabilities: []CapabilityMatch{{CapabilityID: "style.revise-prose", Status: MatchMatched}}}
	media := CandidateRecord{Skill: SkillRecord{ID: "media", Name: "video storyboard", Downloads: 1000}, Capabilities: []CapabilityMatch{{CapabilityID: "style.revise-prose", Status: MatchMatched}}}
	reviews := map[string]ReviewEvidence{
		"writing": {AverageStarsX100: 400, EffectiveRaters: 10, SubstantiveComments: 5, EvidenceCacheStatus: "EVIDENCE-CACHE-AVAILABLE"},
		"media":   {AverageStarsX100: 500, EffectiveRaters: 100, SubstantiveComments: 100, EvidenceCacheStatus: "EVIDENCE-CACHE-AVAILABLE"},
	}
	base := BuildEvidenceVectors([]CandidateRecord{writing}, reviews, nil)[0]
	withMedia := BuildEvidenceVectors([]CandidateRecord{writing, media}, reviews, nil)
	if len(withMedia) != 2 || withMedia[0].SkillID != "media" || withMedia[1].SkillID != "writing" || withMedia[1].DownloadPercentile != base.DownloadPercentile || withMedia[1].BayesianStarsX100 != base.BayesianStarsX100 {
		t.Fatalf("base=%#v withMedia=%#v", base, withMedia)
	}
}

func TestBuildEvidenceVectorsAddsUnmappedCandidateAuditVector(t *testing.T) {
	candidate := CandidateRecord{Skill: SkillRecord{ID: "unmapped"}}
	vectors := BuildEvidenceVectors([]CandidateRecord{candidate}, nil, nil)
	if len(vectors) != 1 || vectors[0].SkillID != "unmapped" || vectors[0].CapabilityID != auditUnmappedCapability || vectors[0].EvidenceCacheStatus != "EVIDENCE-CACHE-MISSING" || vectors[0].PlatformDataRich {
		t.Fatalf("vectors=%#v", vectors)
	}
}

func TestBuildEvidenceVectorsKeepsSingletonsAndNeverUsesCatalogStars(t *testing.T) {
	candidates := []CandidateRecord{
		{Skill: SkillRecord{ID: "single", Downloads: 100, AverageStars: 500}, Capabilities: []CapabilityMatch{{CapabilityID: "cap", Status: MatchMatched}}},
		{Skill: SkillRecord{ID: "representative", Downloads: 90}, Capabilities: []CapabilityMatch{{CapabilityID: "cap", Status: MatchMatched}}},
		{Skill: SkillRecord{ID: "copy", Downloads: 80}, Capabilities: []CapabilityMatch{{CapabilityID: "cap", Status: MatchMatched}}},
	}
	vectors := BuildEvidenceVectors(candidates, map[string]ReviewEvidence{"representative": {EffectiveRaters: 10, SubstantiveComments: 5, AverageStarsX100: 400}}, []DuplicateCluster{{RepresentativeID: "representative", MemberIDs: []string{"representative", "copy"}}})
	if len(vectors) != 3 {
		t.Fatalf("vectors = %#v", vectors)
	}
	for _, vector := range vectors {
		if vector.SkillID == "single" && (vector.Review.AverageStarsX100 != 0 || vector.BayesianStarsX100 != 400 || vector.EvidenceCacheStatus != "EVIDENCE-CACHE-MISSING") {
			t.Fatalf("singleton vector = %#v", vector)
		}
		if vector.SkillID == "copy" && vector.EvidenceCacheStatus != "EVIDENCE-CACHE-MISSING" {
			t.Fatalf("duplicate audit vector = %#v", vector)
		}
	}
}

func TestReviewPriorUsesLowerIntegerMedianForEvenCounts(t *testing.T) {
	mean, strength := reviewPrior([]CandidateRecord{{Skill: SkillRecord{ID: "a"}}, {Skill: SkillRecord{ID: "b"}}}, map[string]ReviewEvidence{"a": {EffectiveRaters: 3, AverageStarsX100: 300}, "b": {EffectiveRaters: 8, AverageStarsX100: 500}})
	if mean != 445.45454545454544 || strength != 5 {
		t.Fatalf("prior=(%v,%d)", mean, strength)
	}
}

func TestPlatformDataRichRequiresDownloadsAndAvailableCache(t *testing.T) {
	candidate := CandidateRecord{Skill: SkillRecord{ID: "a", Downloads: 49}, Capabilities: []CapabilityMatch{{CapabilityID: "cap", Status: MatchMatched}}}
	review := ReviewEvidence{EffectiveRaters: 10, SubstantiveComments: 5, AverageStarsX100: 400}
	if vector := BuildEvidenceVectors([]CandidateRecord{candidate}, map[string]ReviewEvidence{"a": review}, nil)[0]; vector.PlatformDataRich {
		t.Fatalf("downloads below gate: %#v", vector)
	}
	candidate.Skill.Downloads = 50
	if vector := BuildEvidenceVectors([]CandidateRecord{candidate}, map[string]ReviewEvidence{}, nil)[0]; vector.PlatformDataRich || vector.EvidenceCacheStatus != "EVIDENCE-CACHE-MISSING" {
		t.Fatalf("missing cache: %#v", vector)
	}
}

func TestBuildEvidenceVectorsOrdersMultipleCapabilitiesDeterministically(t *testing.T) {
	candidates := []CandidateRecord{{Skill: SkillRecord{ID: "z", Downloads: 50}, Capabilities: []CapabilityMatch{{CapabilityID: "z-cap", Status: MatchMatched}, {CapabilityID: "a-cap", Status: MatchMatched}}}}
	vectors := BuildEvidenceVectors(candidates, map[string]ReviewEvidence{"z": {}}, nil)
	if len(vectors) != 2 || vectors[0].CapabilityID != "a-cap" || vectors[1].CapabilityID != "z-cap" {
		t.Fatalf("vectors=%#v", vectors)
	}
}
