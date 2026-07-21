package skilldiscovery

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestClassifyWritingCandidatesKeepsProposalEvidenceOutOfStableCapabilities(t *testing.T) {
	lexicon := testLexicon(t)
	records := []SkillRecord{
		{ID: "world-a", Name: "世界观规则库", Description: "维护魔法规则和设定约束"},
		{ID: "world-b", Name: "设定一致性助手", Description: "检查世界规则与能力体系约束"},
		{ID: "dialogue", Name: "对白声线", Tags: []string{"人物", "台词"}},
		{ID: "video", Name: "小说转视频", Description: "生成分镜和视频提示词"},
	}
	got, proposals := ClassifyWritingCandidates(records, lexicon)
	if idsOf(got) != "dialogue,world-a,world-b" || proposalIDs(proposals) != "worldbuilding.build-rules" {
		t.Fatalf("candidates=%s proposals=%s", idsOf(got), proposalIDs(proposals))
	}
	if proposalCandidateIDs(proposals) != "world-a,world-b" {
		t.Fatalf("proposal candidates=%s", proposalCandidateIDs(proposals))
	}
	for _, candidate := range got {
		for _, match := range candidate.Capabilities {
			if match.CapabilityID == "worldbuilding.build-rules" {
				t.Fatalf("proposal leaked into stable capabilities: %+v", candidate)
			}
		}
	}
}

func TestClassifyWritingCandidatesNeverLeaksUnapprovedCoreRuleIDs(t *testing.T) {
	lexicon := testLexicon(t)
	lexicon.CoreCapabilities = []CapabilityRule{{CapabilityID: "worldbuilding.build-rules", Terms: []string{"世界观"}}}
	got, _ := ClassifyWritingCandidates([]SkillRecord{{ID: "world", Name: "世界观"}}, lexicon)
	if len(got) != 1 || len(got[0].Capabilities) != 0 {
		t.Fatalf("unapproved stable capabilities=%+v", got)
	}
}

func TestBuildCapabilityProposalsRequiresDistinctSkillIDsAndMetadata(t *testing.T) {
	lexicon := testLexicon(t)
	for _, records := range [][]SkillRecord{
		{{ID: "one", Name: "世界观规则"}},
		{{ID: "one", Name: "世界观规则"}, {ID: "two", Name: "世界观规则"}},
		{{ID: "same", Name: "世界观规则"}, {ID: "same", Name: "设定规则"}},
	} {
		if got := BuildCapabilityProposals(records, lexicon); len(got) != 0 {
			t.Fatalf("unexpected proposals for %#v: %#v", records, got)
		}
	}
}

func TestClassifyWritingCandidatesKeepsAmbiguousAndExplicitProseTransformations(t *testing.T) {
	lexicon := testLexicon(t)
	got, _ := ClassifyWritingCandidates([]SkillRecord{
		{ID: "tag-only", Tags: []string{"台词"}},
		{ID: "rewrite", Name: "把视频剧本改写成小说", Description: "视频提示词"},
		{ID: "write-as", Name: "把视频内容写成小说", Description: "视频提示词"},
		{ID: "write-story", Name: "把视频内容写为故事", Description: "视频提示词"},
		{ID: "convert", Name: "把视频内容转成小说", Description: "视频提示词"},
		{ID: "convert-story", Name: "把视频内容转换为故事", Description: "视频提示词"},
		{ID: "media-only", Name: "小说转视频"},
	}, lexicon)
	if idsOf(got) != "convert,convert-story,rewrite,tag-only,write-as,write-story" {
		t.Fatalf("candidate ids = %s", idsOf(got))
	}
	if candidateByID(got, "tag-only").Capabilities[0].Status != MatchAmbiguous {
		t.Fatalf("tag-only status = %s", candidateByID(got, "tag-only").Capabilities[0].Status)
	}
}

func TestClassifyWritingCandidatesNormalizesAndOrdersEvidenceDeterministically(t *testing.T) {
	lexicon := testLexicon(t)
	lexicon.CoreCapabilities = []CapabilityRule{
		{CapabilityID: "character.build-dialogue-voice", Terms: []string{"ＤＩＡＬＯＧＵＥ", "台词", "台词"}},
		{CapabilityID: "character.build-profile", Terms: []string{"人物 设定"}},
	}
	got, _ := ClassifyWritingCandidates([]SkillRecord{
		{ID: "z", Description: "Ｄｉａｌｏｇｕｅ", Tags: []string{"台词", "人物\t设定"}},
		{ID: "a", Categories: []string{"台词"}, Triggers: []string{" dialogue "}},
	}, lexicon)
	if idsOf(got) != "a,z" || got[0].Skill.ID != "a" || got[1].Skill.ID != "z" {
		t.Fatalf("candidate order=%v", idsOf(got))
	}
	if got[0].Capabilities[0].Status != MatchMatched || got[1].Capabilities[0].Status != MatchMatched {
		t.Fatalf("statuses=%s,%s", got[0].Capabilities[0].Status, got[1].Capabilities[0].Status)
	}
	want := []FieldEvidence{{Field: "description", Term: "dialogue"}, {Field: "tags", Term: "台词"}}
	if !sameEvidence(got[1].Capabilities[0].Evidence, want) {
		t.Fatalf("evidence=%+v", got[1].Capabilities[0].Evidence)
	}
}

func TestLoadLexiconRejectsWrongVersionAndRuleTerms(t *testing.T) {
	lexicon := approvedLexicon(t)
	lexicon.Version = "v2"
	if err := validateLexicon(lexicon); err == nil {
		t.Fatal("accepted v2")
	}
	lexicon = approvedLexicon(t)
	lexicon.CoreCapabilities[0].Terms = []string{" \t "}
	if err := validateLexicon(lexicon); err == nil {
		t.Fatal("accepted whitespace core term")
	}
	lexicon = approvedLexicon(t)
	lexicon.ProposalRules[0].Terms = nil
	if err := validateLexicon(lexicon); err == nil {
		t.Fatal("accepted empty proposal terms")
	}
}

func TestLoadLexiconRejectsMalformedInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lexicon.json")
	if err := os.WriteFile(path, []byte(`{"contract":"wrong","version":"v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLexicon(path); err == nil {
		t.Fatal("LoadLexicon accepted malformed lexicon")
	}
}

func TestLoadLexiconLoadsApprovedBoundaries(t *testing.T) {
	lexicon := approvedLexicon(t)
	if len(lexicon.CoreCapabilities) != len(CoreCapabilityIDs) || len(lexicon.ProposalRules) != len(proposalCapabilityIDs) {
		t.Fatalf("core=%d proposals=%d", len(lexicon.CoreCapabilities), len(lexicon.ProposalRules))
	}
}

func approvedLexicon(t *testing.T) Lexicon {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-capability-lexicon-v1.json")
	lexicon, err := LoadLexicon(path)
	if err != nil {
		t.Fatal(err)
	}
	return lexicon
}

func testLexicon(t *testing.T) Lexicon {
	t.Helper()
	return Lexicon{Contract: capabilityLexiconContract, Version: "v1", IncludeTerms: []string{"小说", "故事", "对白", "台词", "世界观", "设定", "规则", "dialogue", "人物 设定"}, ExcludeTerms: []string{"转视频", "视频提示词"}, CoreCapabilities: []CapabilityRule{{CapabilityID: "character.build-dialogue-voice", Terms: []string{"对白", "台词", "dialogue"}}}, ProposalRules: []CapabilityRule{{CapabilityID: "worldbuilding.build-rules", Terms: []string{"世界观", "设定", "规则"}, NameZH: "世界规则构建", NameEN: "Worldbuilding rules", Inputs: []string{"premise"}, Outputs: []string{"world_rules"}, LifecycleStage: "planning", MinimumPermission: "read-bounded-input", EvaluationMethod: "rule-consistency paired review"}}}
}

func candidateByID(records []CandidateRecord, id string) CandidateRecord {
	for _, record := range records {
		if record.Skill.ID == id {
			return record
		}
	}
	return CandidateRecord{}
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
func proposalCandidateIDs(records []CapabilityProposal) string {
	if len(records) == 0 {
		return ""
	}
	return strings.Join(records[0].CandidateIDs, ",")
}
func sameEvidence(got, want []FieldEvidence) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
