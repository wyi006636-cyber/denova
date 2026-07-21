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
		{ID: "2", UserID: "u2", Stars: 5, Content: "实际续写三章后，人物声线保持稳定，时间线没有漂移；逐段对照原大纲，关键伏笔、场景顺序和角色动机都能对应。", CreatedAt: "2026-07-02T00:00:00Z"},
		{ID: "3", UserID: "u3", Stars: 5, Content: "实际续写三章后人物声线保持稳定时间线没有漂移逐段对照原大纲关键伏笔场景顺序和角色动机都能对应", CreatedAt: "2026-07-02T00:01:00Z"},
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

func TestSummarizeReviewsRequiresFortyRunesAndDeduplicatesReviewerDeterministically(t *testing.T) {
	short := ReviewRecord{ID: "short", UserID: "u", Stars: 5, Content: "续写人物时间线稳定"}
	long := ReviewRecord{ID: "long", UserID: "u", Stars: 4, Content: "续写完成后人物行为和时间线都保持稳定，输出章节与输入大纲逐项对比，没有发现设定冲突或事实漂移。"}
	duplicateGeneric := ReviewRecord{ID: "generic", UserID: "other", Stars: 5, Content: "效果不错"}
	got := SummarizeReviews("owner", []ReviewRecord{short, long, duplicateGeneric, {ID: "generic-copy", UserID: "third", Stars: 5, Content: "效果不错"}}, ReviewPolicy{MinimumRunes: 1, ObservationTerms: []string{"续写", "人物", "时间线"}, NearDuplicateJaccard: .9})
	if got.EffectiveRaters != 1 || got.AverageStarsX100 != 400 || got.DuplicateComments != 1 {
		t.Fatalf("evidence=%#v", got)
	}
}
