package skilldiscovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSummarizeReviewsExcludesOwnerGenericAndDuplicates(t *testing.T) {
	reviews := []ReviewRecord{
		{ID: "1", UserID: "owner", Stars: 5, Content: "非常好用", CreatedAt: "2026-07-01T00:00:00Z"},
		{ID: "2", UserID: "u2", Stars: 5, Content: "实际续写三章后，人物声线保持稳定，时间线没有漂移。", CreatedAt: "2026-07-02T00:00:00Z"},
		{ID: "3", UserID: "u3", Stars: 5, Content: "实际续写三章后人物声线保持稳定，时间线没有漂移", CreatedAt: "2026-07-02T00:01:00Z"},
	}
	policy := ReviewPolicy{MinimumRunes: 40, NearDuplicateJaccard: 0.90, ObservationTerms: []string{"续写", "人物", "时间线", "稳定", "漂移"}, GenericPhrases: []string{"非常好用", "效果不错", "推荐使用"}}
	got := SummarizeReviews("owner", reviews, policy)
	if got.EffectiveRaters != 1 || got.SubstantiveComments != 1 || got.OwnerSelfReviews != 1 || got.DuplicateComments != 1 {
		t.Fatalf("evidence = %#v", got)
	}
}

func TestCommittedEvidenceContainsNoReviewerOrSignedAvatar(t *testing.T) {
	raw := apiReview{ID: "review-1", UserID: "reviewer-1", UserName: "评审者", UserAvatarURL: "https://example.test/a.png?sign=ephemeral", Stars: 5, Content: "实际续写三章后，人物声线保持稳定，时间线没有漂移。"}
	review := normalizeAPIReview(raw)
	policy := ReviewPolicy{MinimumRunes: 20, NearDuplicateJaccard: 0.90, ObservationTerms: []string{"续写", "人物", "时间线"}}
	vector := EvidenceVector{SkillID: "skill-1", CapabilityID: "continuity.review-facts", Review: SummarizeReviews("owner", []ReviewRecord{review}, policy)}
	data, err := json.Marshal(vector)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"user_id", "user_name", "avatar", "?sign="} {
		if bytes.Contains(bytes.ToLower(data), []byte(forbidden)) {
			t.Fatalf("committed evidence leaked %q: %s", forbidden, data)
		}
	}
}

func TestSummarizeReviewsReportsDefinedAnomalies(t *testing.T) {
	reviews := make([]ReviewRecord, 0, 10)
	for index := 0; index < 10; index++ {
		reviews = append(reviews, ReviewRecord{ID: string(rune('a' + index)), UserID: string(rune('a' + index)), Stars: 5, Content: fmt.Sprintf("续写%s结果可复核", strings.Repeat(string(rune('甲'+index)), 50)), CreatedAt: "2026-07-02T12:00:00Z"})
	}
	got := SummarizeReviews("owner", reviews, defaultReviewPolicy())
	if !containsFlag(got.AnomalyFlags, "REVIEW-BURST") {
		t.Fatalf("anomaly flags = %v", got.AnomalyFlags)
	}
}
