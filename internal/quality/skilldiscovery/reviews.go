package skilldiscovery

import (
	"sort"
	"strings"
	"time"
)

// ReviewRecord is transient review data. It must never be serialized into an evidence artifact.
type ReviewRecord struct {
	ID                   string  `json:"-"`
	UserID               string  `json:"-"`
	Stars                int     `json:"-"`
	Content              string  `json:"-"`
	Pros                 string  `json:"-"`
	Cons                 string  `json:"-"`
	UseCase              string  `json:"-"`
	ReviewerQualityScore float64 `json:"-"`
	PlatformQualityTotal float64 `json:"-"`
	CreatedAt            string  `json:"-"`
}

// apiReview is cache-only transport data; identity and avatar fields are discarded during normalization.
type apiReview struct {
	ID            string `json:"id"`
	UserID        string `json:"user_id"`
	UserName      string `json:"user_name"`
	UserAvatarURL string `json:"user_avatar_url"`
	Stars         int    `json:"stars"`
	Content       string `json:"content"`
	Pros          string `json:"pros"`
	Cons          string `json:"cons"`
	UseCase       string `json:"use_case"`
	CreatedAt     string `json:"created_at"`
}

func normalizeAPIReview(raw apiReview) ReviewRecord {
	return ReviewRecord{ID: raw.ID, UserID: raw.UserID, Stars: raw.Stars, Content: raw.Content, Pros: raw.Pros, Cons: raw.Cons, UseCase: raw.UseCase, CreatedAt: raw.CreatedAt}
}

type ReviewPolicy struct {
	MinimumRunes         int
	NearDuplicateJaccard float64
	ObservationTerms     []string
	GenericPhrases       []string
}

func defaultReviewPolicy() ReviewPolicy {
	return ReviewPolicy{MinimumRunes: 40, NearDuplicateJaccard: .90, ObservationTerms: []string{"输入", "输出", "行为", "失败", "对比", "结果", "续写", "人物", "时间线", "稳定", "漂移"}, GenericPhrases: []string{"很好用", "非常好用", "效果不错", "推荐使用"}}
}

func SummarizeReviews(ownerID string, reviews []ReviewRecord, policy ReviewPolicy) ReviewEvidence {
	if policy.MinimumRunes <= 0 {
		policy.MinimumRunes = 40
	}
	if policy.NearDuplicateJaccard <= 0 {
		policy.NearDuplicateJaccard = .90
	}
	if len(policy.ObservationTerms) == 0 {
		policy.ObservationTerms = defaultReviewPolicy().ObservationTerms
	}
	if len(policy.GenericPhrases) == 0 {
		policy.GenericPhrases = defaultReviewPolicy().GenericPhrases
	}
	result := ReviewEvidence{AnomalyFlags: []string{}}
	nonOwner := make([]ReviewRecord, 0, len(reviews))
	starTotal := 0
	for _, review := range reviews {
		if review.UserID != "" && review.UserID == ownerID {
			result.OwnerSelfReviews++
			continue
		}
		nonOwner = append(nonOwner, review)
	}
	sort.Slice(nonOwner, func(i, j int) bool {
		left, right := reviewerKey(nonOwner[i]), reviewerKey(nonOwner[j])
		if left != right {
			return left < right
		}
		return nonOwner[i].ID < nonOwner[j].ID
	})
	unique := make([]ReviewRecord, 0, len(nonOwner))
	seenReviewers := map[string]struct{}{}
	for _, review := range nonOwner {
		key := reviewerKey(review)
		if _, seen := seenReviewers[key]; seen {
			continue
		}
		seenReviewers[key] = struct{}{}
		if duplicateReview(review, unique, policy.NearDuplicateJaccard) {
			result.DuplicateComments++
			continue
		}
		unique = append(unique, review)
	}
	substantive := make([]ReviewRecord, 0, len(unique))
	for _, review := range unique {
		if reviewSubstantive(review, policy) {
			substantive = append(substantive, review)
			starTotal += review.Stars
		}
	}
	result.EffectiveRaters, result.SubstantiveComments = len(substantive), len(substantive)
	if result.EffectiveRaters > 0 {
		result.AverageStarsX100 = starTotal * 100 / result.EffectiveRaters
	}
	if len(nonOwner) > 0 && result.DuplicateComments*100 >= 30*len(nonOwner) {
		result.AnomalyFlags = append(result.AnomalyFlags, "DUPLICATE-COMMENT-CONCENTRATION")
	}
	if len(reviews) >= 10 && result.SubstantiveComments*100 < 20*len(reviews) {
		result.AnomalyFlags = append(result.AnomalyFlags, "LOW-SUBSTANTIVE-RATIO")
	}
	if result.EffectiveRaters >= 10 && hasReviewBurst(substantive) {
		result.AnomalyFlags = append(result.AnomalyFlags, "REVIEW-BURST")
	}
	sort.Strings(result.AnomalyFlags)
	return result
}

func reviewSubstantive(review ReviewRecord, policy ReviewPolicy) bool {
	text := normalizedComparableText(strings.Join([]string{review.Content, review.Pros, review.Cons, review.UseCase}, " "))
	minimum := policy.MinimumRunes
	if minimum < 40 {
		minimum = 40
	}
	if utfRunes(text) < minimum || text == "" {
		return false
	}
	for _, generic := range policy.GenericPhrases {
		if text == normalizedComparableText(generic) {
			return false
		}
	}
	for _, term := range policy.ObservationTerms {
		if strings.Contains(text, normalizedComparableText(term)) {
			return true
		}
	}
	return false
}
func utfRunes(text string) int { return len([]rune(text)) }
func reviewerKey(review ReviewRecord) string {
	if review.UserID != "" {
		return "user:" + review.UserID
	}
	return "review:" + review.ID
}
func duplicateReview(review ReviewRecord, existing []ReviewRecord, threshold float64) bool {
	for _, prior := range existing {
		if reviewJaccard(review, prior) >= threshold {
			return true
		}
	}
	return false
}
func reviewJaccard(left, right ReviewRecord) float64 {
	a, b := reviewTokens(left), reviewTokens(right)
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := 0
	for x := range a {
		if _, ok := b[x]; ok {
			n++
		}
	}
	return float64(n) / float64(len(a)+len(b)-n)
}
func reviewTokens(review ReviewRecord) map[string]struct{} {
	text := normalizedComparableText(strings.Join([]string{review.Content, review.Pros, review.Cons, review.UseCase}, " "))
	tokens := map[string]struct{}{}
	r := []rune(text)
	for i := 0; i+1 < len(r); i++ {
		tokens[string(r[i:i+2])] = struct{}{}
	}
	return tokens
}
func hasReviewBurst(reviews []ReviewRecord) bool {
	times := make([]time.Time, 0, len(reviews))
	for _, r := range reviews {
		if value, err := time.Parse(time.RFC3339, r.CreatedAt); err == nil {
			times = append(times, value)
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	for start := range times {
		end := start
		for end < len(times) && times[end].Sub(times[start]) <= 24*time.Hour {
			end++
		}
		if (end-start)*100 >= 60*len(reviews) {
			return true
		}
	}
	return false
}
func containsFlag(flags []string, wanted string) bool {
	for _, flag := range flags {
		if flag == wanted {
			return true
		}
	}
	return false
}
func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
