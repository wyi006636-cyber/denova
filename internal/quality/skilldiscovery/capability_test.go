package skilldiscovery

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestClassifyWritingCandidatesRecallsLifecycleTerms(t *testing.T) {
	lexicon := testLexicon(t)
	records := []SkillRecord{
		{ID: "world-a", Name: "世界观规则库", Description: "维护魔法规则和设定约束"},
		{ID: "world-b", Name: "设定一致性助手", Description: "检查世界规则与能力体系约束"},
		{ID: "dialogue", Name: "对白声线", Tags: []string{"人物", "台词"}},
		{ID: "video", Name: "小说转视频", Description: "生成分镜和视频提示词"},
	}
	got, proposals := ClassifyWritingCandidates(records, lexicon)
	if idsOf(got) != "dialogue,world-a,world-b" {
		t.Fatalf("candidate ids = %s", idsOf(got))
	}
	if proposalIDs(proposals) != "worldbuilding.build-rules" {
		t.Fatalf("proposal ids = %s", proposalIDs(proposals))
	}
}

func TestClassifyWritingCandidatesKeepsAmbiguousAndDirectWritingExceptions(t *testing.T) {
	lexicon := testLexicon(t)
	got, _ := ClassifyWritingCandidates([]SkillRecord{
		{ID: "tag-only", Tags: []string{"设定"}},
		{ID: "adapt", Name: "把视频剧本改写成小说"},
	}, lexicon)
	if idsOf(got) != "adapt,tag-only" {
		t.Fatalf("candidate ids = %s", idsOf(got))
	}
	if got[1].Capabilities[0].Status != MatchAmbiguous {
		t.Fatalf("tag-only status = %s", got[1].Capabilities[0].Status)
	}
}

func TestCapabilityProposalRequiresTwoNonDuplicateSkills(t *testing.T) {
	lexicon := testLexicon(t)
	got, proposals := ClassifyWritingCandidates([]SkillRecord{{ID: "one", Name: "世界观规则"}}, lexicon)
	if len(got) != 1 || len(proposals) != 0 {
		t.Fatalf("got candidates=%d proposals=%d", len(got), len(proposals))
	}
}

func TestBuildCapabilityProposalsRejectsDuplicateMetadataSignals(t *testing.T) {
	lexicon := testLexicon(t)
	candidates := []CandidateRecord{
		{Skill: SkillRecord{ID: "one", Name: "世界观规则"}, Capabilities: []CapabilityMatch{{CapabilityID: "worldbuilding.build-rules", Status: MatchMatched}}},
		{Skill: SkillRecord{ID: "two", Name: "世界观规则"}, Capabilities: []CapabilityMatch{{CapabilityID: "worldbuilding.build-rules", Status: MatchMatched}}},
	}
	if got := BuildCapabilityProposals(candidates, lexicon); len(got) != 0 {
		t.Fatalf("duplicate metadata emitted proposals=%v", got)
	}
}

func TestBuildCapabilityProposalsRequiresDistinctSkillIDs(t *testing.T) {
	lexicon := testLexicon(t)
	candidates := []CandidateRecord{
		{Skill: SkillRecord{ID: "same", Name: "世界观规则"}, Capabilities: []CapabilityMatch{{CapabilityID: "worldbuilding.build-rules", Status: MatchMatched}}},
		{Skill: SkillRecord{ID: "same", Name: "设定规则"}, Capabilities: []CapabilityMatch{{CapabilityID: "worldbuilding.build-rules", Status: MatchMatched}}},
	}
	if got := BuildCapabilityProposals(candidates, lexicon); len(got) != 0 {
		t.Fatalf("repeated ID emitted proposals=%v", got)
	}
}

func TestLoadLexiconRejectsMalformedAndCoreProposalOverlap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lexicon.json")
	if err := os.WriteFile(path, []byte(`{"contract":"wrong","version":"v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLexicon(path); err == nil {
		t.Fatal("LoadLexicon accepted malformed lexicon")
	}
	if err := os.WriteFile(path, []byte(`{"contract":"denova.xiaping-capability-lexicon","version":"v1","include_terms":["小说"],"exclude_terms":[],"core_capabilities":[{"capability_id":"character.build-dialogue-voice","terms":["台词"]}],"proposal_rules":[{"capability_id":"character.build-dialogue-voice","terms":["台词"],"name_zh":"x","name_en":"x","inputs":["x"],"outputs":["x"],"lifecycle_stage":"x","minimum_permission":"x","evaluation_method":"x"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLexicon(path); err == nil {
		t.Fatal("LoadLexicon accepted core/proposal overlap")
	}
}

func TestLoadLexiconLoadsApprovedBoundaries(t *testing.T) {
	path := filepath.Join("..", "..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-capability-lexicon-v1.json")
	lexicon, err := LoadLexicon(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lexicon.CoreCapabilities) != len(CoreCapabilityIDs) || len(lexicon.ProposalRules) != len(proposalCapabilityIDs) {
		t.Fatalf("core=%d proposals=%d", len(lexicon.CoreCapabilities), len(lexicon.ProposalRules))
	}
}

func testLexicon(t *testing.T) Lexicon {
	t.Helper()
	return Lexicon{
		Contract: "denova.xiaping-capability-lexicon", Version: "v1",
		IncludeTerms:     []string{"小说", "对白", "台词", "世界观", "设定", "规则"},
		ExcludeTerms:     []string{"转视频", "视频提示词"},
		CoreCapabilities: []CapabilityRule{{CapabilityID: "character.build-dialogue-voice", Terms: []string{"对白", "台词"}}},
		ProposalRules:    []CapabilityRule{{CapabilityID: "worldbuilding.build-rules", Terms: []string{"世界观", "设定", "规则"}, NameZH: "世界规则构建", NameEN: "Worldbuilding rules", Inputs: []string{"premise"}, Outputs: []string{"world_rules"}, LifecycleStage: "planning", MinimumPermission: "read-bounded-input", EvaluationMethod: "rule-consistency paired review"}},
	}
}

func idsOf(records []CandidateRecord) string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.Skill.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func proposalIDs(records []CapabilityProposal) string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.CapabilityID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}
