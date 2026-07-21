package skilldiscovery

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestClusterCandidatesGroupsMetadataCopiesButNotSameAuthorAlone(t *testing.T) {
	candidates := []CandidateRecord{
		{Skill: SkillRecord{ID: "a", OwnerID: "owner", Name: "长篇小说助手", Description: "维护人物时间线伏笔并续写章节", Downloads: 10}},
		{Skill: SkillRecord{ID: "b", OwnerID: "other", Name: "长篇小说写作助手", Description: "维护人物、时间线、伏笔并续写章节", Downloads: 20}},
		{Skill: SkillRecord{ID: "c", OwnerID: "owner", Name: "对白助手", Description: "塑造人物独特声线", Downloads: 30}},
	}
	clusters := ClusterCandidates(candidates, duplicateSimilarityThreshold)
	if len(clusters) != 1 || strings.Join(clusters[0].MemberIDs, ",") != "a,b" || clusters[0].RepresentativeID != "b" {
		t.Fatalf("clusters = %#v", clusters)
	}
}

func TestMetadataSignatureNormalizesNFKCAndSortsMetadataTokens(t *testing.T) {
	left := CandidateRecord{Skill: SkillRecord{Name: "Ｄｒａｆｔ", Description: "  Story\tPlan ", Triggers: []string{"续写", "  Plan"}, Tags: []string{"人物", "Ｄｒａｆｔ"}}}
	right := CandidateRecord{Skill: SkillRecord{Name: "draft", Description: "story plan", Triggers: []string{"plan", "续写"}, Tags: []string{"draft", "人物"}}}
	if got, want := MetadataSignature(left), MetadataSignature(right); got != want {
		t.Fatalf("MetadataSignature() = %q, want %q", got, want)
	}
}

func TestMetadataSignatureSeparatesCommaDelimitedMetadataValues(t *testing.T) {
	left := CandidateRecord{Skill: SkillRecord{Triggers: []string{"a,b", "c"}}}
	right := CandidateRecord{Skill: SkillRecord{Triggers: []string{"a", "b,c"}}}
	if got, want := MetadataSignature(left), MetadataSignature(right); got == want {
		t.Fatalf("MetadataSignature collision: both signatures = %q", got)
	}
}

func TestTokenJaccardIncludesNormalizedTriggerAndTagTokens(t *testing.T) {
	left := CandidateRecord{Skill: SkillRecord{Name: "", Description: "", Triggers: []string{"Ｄｒａｆｔ"}, Tags: []string{"人物"}}}
	right := CandidateRecord{Skill: SkillRecord{Name: "", Description: "", Triggers: []string{"draft"}, Tags: []string{"人物"}}}
	if got := TokenJaccard(left, right); got != 1 {
		t.Fatalf("TokenJaccard() = %v, want 1", got)
	}
}

func TestClusterCandidatesUsesInclusiveThresholdAndTransitiveGroups(t *testing.T) {
	candidates := []CandidateRecord{
		{Skill: SkillRecord{ID: "c", Triggers: append(clusterTokens(2, 19), "t21", "t22")}},
		{Skill: SkillRecord{ID: "a", Triggers: clusterTokens(1, 20)}},
		{Skill: SkillRecord{ID: "b", Triggers: append(clusterTokens(1, 19), "t21")}},
	}
	clusters := ClusterCandidates(candidates, duplicateSimilarityThreshold)
	if len(clusters) != 1 || !reflect.DeepEqual(clusters[0].MemberIDs, []string{"a", "b", "c"}) {
		t.Fatalf("clusters = %#v, want one transitive a,b,c cluster", clusters)
	}
}

func clusterTokens(start, end int) []string {
	tokens := make([]string, 0, end-start+1)
	for value := start; value <= end; value++ {
		tokens = append(tokens, "t"+string(rune('a'+value)))
	}
	return tokens
}

func TestClusterCandidatesRepresentativeUsesOrderedTieBreakers(t *testing.T) {
	candidates := []CandidateRecord{
		{Skill: SkillRecord{ID: "z", Name: "copy", Description: "same", Downloads: 10, StarCount: 4, VersionCount: 2}},
		{Skill: SkillRecord{ID: "b", Name: "copy", Description: "same", Downloads: 10, StarCount: 5, VersionCount: 1}},
		{Skill: SkillRecord{ID: "a", Name: "copy", Description: "same", Downloads: 10, StarCount: 5, VersionCount: 3}},
	}
	clusters := ClusterCandidates(candidates, 0.90)
	if len(clusters) != 1 || clusters[0].RepresentativeID != "a" {
		t.Fatalf("clusters = %#v, want representative a", clusters)
	}
}

func TestClusterCandidatesCoalescesDuplicateIDsDeterministically(t *testing.T) {
	inputs := []CandidateRecord{
		{Skill: SkillRecord{ID: "x", Name: "copy", Description: "same", Downloads: 1}},
		{Skill: SkillRecord{ID: "y", Name: "copy", Description: "same", Downloads: 5}},
		{Skill: SkillRecord{ID: "x", Name: "copy", Description: "same", Downloads: 10}},
	}
	original := append([]CandidateRecord(nil), inputs...)
	forward := ClusterCandidates(inputs, duplicateSimilarityThreshold)
	reversed := ClusterCandidates([]CandidateRecord{inputs[2], inputs[1], inputs[0]}, duplicateSimilarityThreshold)
	forwardJSON, _ := json.Marshal(forward)
	reversedJSON, _ := json.Marshal(reversed)
	if !reflect.DeepEqual(inputs, original) {
		t.Fatalf("ClusterCandidates mutated input: %#v", inputs)
	}
	if string(forwardJSON) != string(reversedJSON) {
		t.Fatalf("clusters differ after duplicate-ID shuffle: %s != %s", forwardJSON, reversedJSON)
	}
	if len(forward) != 1 || !reflect.DeepEqual(forward[0].MemberIDs, []string{"x", "y"}) || forward[0].RepresentativeID != "x" {
		t.Fatalf("clusters = %#v, want coalesced x,y with x representative", forward)
	}
}

func TestClusterCandidatesRecordsSameAuthorAcrossTransitiveCluster(t *testing.T) {
	candidates := []CandidateRecord{
		{Skill: SkillRecord{ID: "a", OwnerID: "shared", Triggers: clusterTokens(1, 20)}},
		{Skill: SkillRecord{ID: "b", OwnerID: "other", Triggers: append(clusterTokens(1, 19), "t21")}},
		{Skill: SkillRecord{ID: "c", OwnerID: "shared", Triggers: append(clusterTokens(2, 19), "t21", "t22")}},
	}
	if got := TokenJaccard(candidates[0], candidates[2]); got >= duplicateSimilarityThreshold {
		t.Fatalf("A/C similarity = %v, want below %v", got, duplicateSimilarityThreshold)
	}
	clusters := ClusterCandidates(candidates, duplicateSimilarityThreshold)
	if len(clusters) != 1 || !reflect.DeepEqual(clusters[0].MemberIDs, []string{"a", "b", "c"}) || !strings.Contains(strings.Join(clusters[0].Reasons, ","), "same_author") {
		t.Fatalf("clusters = %#v, want transitive same_author evidence", clusters)
	}
}
