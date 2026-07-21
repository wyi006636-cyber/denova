package skilldiscovery

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"unicode"
)

const duplicateSimilarityThreshold = 0.90

// MetadataSignature identifies an exact duplicate after canonical metadata normalization.
func MetadataSignature(candidate CandidateRecord) string {
	skill := candidate.Skill
	parts := []string{
		"name=" + normalizeText(skill.Name),
		"description=" + normalizeText(skill.Description),
		"triggers=" + strings.Join(normalizedTokens(skill.Triggers), ","),
		"tags=" + strings.Join(normalizedTokens(skill.Tags), ","),
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// TokenJaccard compares description bigrams and normalized trigger/tag tokens.
func TokenJaccard(left, right CandidateRecord) float64 {
	leftTokens := similarityTokens(left)
	rightTokens := similarityTokens(right)
	if len(leftTokens) == 0 && len(rightTokens) == 0 {
		return 0
	}
	intersection := 0
	for token := range leftTokens {
		if _, exists := rightTokens[token]; exists {
			intersection++
		}
	}
	return float64(intersection) / float64(len(leftTokens)+len(rightTokens)-intersection)
}

// ClusterCandidates returns deterministic, explainable metadata duplicate groups without dropping members.
func ClusterCandidates(candidates []CandidateRecord, threshold float64) []DuplicateCluster {
	ordered := append([]CandidateRecord(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Skill.ID < ordered[j].Skill.ID })
	groups := newDisjointSet(len(ordered))
	edges := make([]duplicateEdge, 0)
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			exact := MetadataSignature(ordered[i]) == MetadataSignature(ordered[j])
			similarity := TokenJaccard(ordered[i], ordered[j])
			if !exact && similarity < threshold {
				continue
			}
			groups.union(i, j)
			edges = append(edges, duplicateEdge{left: i, right: j, exact: exact, sameOwner: ordered[i].Skill.OwnerID != "" && ordered[i].Skill.OwnerID == ordered[j].Skill.OwnerID})
		}
	}

	membersByRoot := make(map[int][]int)
	for i := range ordered {
		root := groups.find(i)
		membersByRoot[root] = append(membersByRoot[root], i)
	}
	clusters := make([]DuplicateCluster, 0, len(membersByRoot))
	for _, memberIndexes := range membersByRoot {
		if len(memberIndexes) < 2 {
			continue
		}
		memberIDs := make([]string, len(memberIndexes))
		memberSet := make(map[int]struct{}, len(memberIndexes))
		for i, index := range memberIndexes {
			memberIDs[i] = ordered[index].Skill.ID
			memberSet[index] = struct{}{}
		}
		reasons, kind := clusterReasons(edges, memberSet)
		clusters = append(clusters, DuplicateCluster{
			ClusterID:        "duplicate:" + memberIDs[0],
			Kind:             kind,
			RepresentativeID: representativeID(ordered, memberIndexes),
			MemberIDs:        memberIDs,
			Reasons:          reasons,
		})
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].ClusterID < clusters[j].ClusterID })
	return clusters
}

type duplicateEdge struct {
	left, right      int
	exact, sameOwner bool
}

func clusterReasons(edges []duplicateEdge, members map[int]struct{}) ([]string, string) {
	reasonSet := make(map[string]struct{})
	kind := "near_metadata"
	for _, edge := range edges {
		if _, exists := members[edge.left]; !exists {
			continue
		}
		if _, exists := members[edge.right]; !exists {
			continue
		}
		if edge.exact {
			kind = "exact_metadata"
			reasonSet["exact_metadata_signature"] = struct{}{}
		} else {
			reasonSet["token_jaccard>=0.90"] = struct{}{}
		}
		if edge.sameOwner {
			reasonSet["same_author"] = struct{}{}
		}
	}
	reasons := make([]string, 0, len(reasonSet))
	for reason := range reasonSet {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return reasons, kind
}

func representativeID(candidates []CandidateRecord, memberIndexes []int) string {
	best := memberIndexes[0]
	for _, index := range memberIndexes[1:] {
		if candidatePreferred(candidates[index].Skill, candidates[best].Skill) {
			best = index
		}
	}
	return candidates[best].Skill.ID
}

func candidatePreferred(left, right SkillRecord) bool {
	if left.Downloads != right.Downloads {
		return left.Downloads > right.Downloads
	}
	if left.StarCount != right.StarCount {
		return left.StarCount > right.StarCount
	}
	if left.VersionCount != right.VersionCount {
		return left.VersionCount > right.VersionCount
	}
	return left.ID < right.ID
}

func similarityTokens(candidate CandidateRecord) map[string]struct{} {
	tokens := unicodeBigrams(normalizedComparableText(candidate.Skill.Description))
	for _, token := range append(normalizedTokens(candidate.Skill.Triggers), normalizedTokens(candidate.Skill.Tags)...) {
		tokens["token:"+token] = struct{}{}
	}
	return tokens
}

func normalizedTokens(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if normalized := normalizeText(value); normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizedComparableText(value string) string {
	var builder strings.Builder
	for _, r := range normalizeText(value) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func unicodeBigrams(value string) map[string]struct{} {
	tokens := make(map[string]struct{})
	runes := []rune(value)
	for i := 0; i+1 < len(runes); i++ {
		tokens[string(runes[i:i+2])] = struct{}{}
	}
	return tokens
}

type disjointSet struct{ parents, ranks []int }

func newDisjointSet(size int) disjointSet {
	parents := make([]int, size)
	for i := range parents {
		parents[i] = i
	}
	return disjointSet{parents: parents, ranks: make([]int, size)}
}

func (set *disjointSet) find(index int) int {
	if set.parents[index] != index {
		set.parents[index] = set.find(set.parents[index])
	}
	return set.parents[index]
}

func (set *disjointSet) union(left, right int) {
	leftRoot, rightRoot := set.find(left), set.find(right)
	if leftRoot == rightRoot {
		return
	}
	if set.ranks[leftRoot] < set.ranks[rightRoot] {
		leftRoot, rightRoot = rightRoot, leftRoot
	}
	set.parents[rightRoot] = leftRoot
	if set.ranks[leftRoot] == set.ranks[rightRoot] {
		set.ranks[leftRoot]++
	}
}
