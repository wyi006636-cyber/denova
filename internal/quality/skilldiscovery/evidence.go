package skilldiscovery

import "sort"

func DownloadPercentiles(candidates []CandidateRecord) map[string]map[string]float64 {
	result := map[string]map[string]float64{}
	for capability, members := range capabilityMembers(candidates, nil) {
		sort.Slice(members, func(i, j int) bool {
			if members[i].Skill.Downloads != members[j].Skill.Downloads {
				return members[i].Skill.Downloads < members[j].Skill.Downloads
			}
			return members[i].Skill.ID < members[j].Skill.ID
		})
		result[capability] = map[string]float64{}
		for start := 0; start < len(members); {
			end := start + 1
			for end < len(members) && members[end].Skill.Downloads == members[start].Skill.Downloads {
				end++
			}
			percentile := 1.0
			if len(members) > 1 {
				percentile = float64(start+end-1) / float64(2*(len(members)-1))
			}
			for _, member := range members[start:end] {
				result[capability][member.Skill.ID] = percentile
			}
			start = end
		}
	}
	return result
}

func BayesianAdjustedStars(averageX100 int, effectiveRaters int, poolMeanX100 float64, priorStrength int) float64 {
	if priorStrength <= 0 {
		priorStrength = 1
	}
	return (float64(effectiveRaters)*float64(averageX100) + float64(priorStrength)*poolMeanX100) / (float64(effectiveRaters) + float64(priorStrength))
}

func BuildEvidenceVectors(candidates []CandidateRecord, reviews map[string]ReviewEvidence, clusters []DuplicateCluster) []EvidenceVector {
	representatives := map[string]bool{}
	for _, cluster := range clusters {
		representatives[cluster.RepresentativeID] = true
		for _, id := range cluster.MemberIDs {
			if id != cluster.RepresentativeID {
				representatives[id] = false
			}
		}
	}
	percentiles := DownloadPercentiles(filterRepresentatives(candidates, representatives))
	members := capabilityMembers(filterRepresentatives(candidates, representatives), nil)
	result := make([]EvidenceVector, 0)
	for capability, pool := range members {
		mean, strength := reviewPrior(pool, reviews)
		for _, candidate := range pool {
			review := reviews[candidate.Skill.ID]
			if countMismatch(candidate.Skill.StarCount, candidate.Skill.CommentCount) {
				review.AnomalyFlags = appendUniqueFlag(review.AnomalyFlags, "RATING-COMMENT-COUNT-MISMATCH")
			}
			if review.AverageStarsX100 == 0 {
				review.AverageStarsX100 = candidate.Skill.AverageStars
			}
			vector := EvidenceVector{SkillID: candidate.Skill.ID, CapabilityID: capability, DownloadPercentile: percentiles[capability][candidate.Skill.ID], BayesianStarsX100: BayesianAdjustedStars(review.AverageStarsX100, review.EffectiveRaters, mean, strength), Review: review, PlatformDataRich: review.EffectiveRaters >= 10 && review.SubstantiveComments >= 5 && percentiles[capability][candidate.Skill.ID] >= .75 && !severeFlag(review.AnomalyFlags), MaturityVersionCount: candidate.Skill.VersionCount, EvidenceCacheStatus: review.EvidenceCacheStatus}
			if vector.EvidenceCacheStatus == "" {
				vector.EvidenceCacheStatus = "EVIDENCE-CACHE-AVAILABLE"
			}
			if vector.EvidenceCacheStatus != "EVIDENCE-CACHE-AVAILABLE" {
				vector.PlatformDataRich = false
			}
			result = append(result, vector)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CapabilityID != result[j].CapabilityID {
			return result[i].CapabilityID < result[j].CapabilityID
		}
		return result[i].SkillID < result[j].SkillID
	})
	return result
}
func capabilityMembers(candidates []CandidateRecord, ignored map[string]bool) map[string][]CandidateRecord {
	out := map[string][]CandidateRecord{}
	for _, candidate := range candidates {
		if ignored != nil && !ignored[candidate.Skill.ID] {
			continue
		}
		for _, match := range candidate.Capabilities {
			if match.Status == MatchMatched {
				out[match.CapabilityID] = append(out[match.CapabilityID], candidate)
			}
		}
	}
	return out
}
func filterRepresentatives(candidates []CandidateRecord, reps map[string]bool) []CandidateRecord {
	if len(reps) == 0 {
		return candidates
	}
	out := make([]CandidateRecord, 0, len(candidates))
	for _, candidate := range candidates {
		if reps[candidate.Skill.ID] {
			out = append(out, candidate)
		}
	}
	return out
}
func reviewPrior(pool []CandidateRecord, reviews map[string]ReviewEvidence) (float64, int) {
	weighted, total := 0.0, 0
	counts := []int{}
	for _, candidate := range pool {
		review := reviews[candidate.Skill.ID]
		if review.EffectiveRaters > 0 {
			weighted += float64(review.AverageStarsX100 * review.EffectiveRaters)
			total += review.EffectiveRaters
			counts = append(counts, review.EffectiveRaters)
		}
	}
	if total == 0 {
		return 0, 1
	}
	sort.Ints(counts)
	return weighted / float64(total), counts[len(counts)/2]
}
func severeFlag(flags []string) bool {
	return containsFlag(flags, "REVIEW-BURST") || containsFlag(flags, "DUPLICATE-COMMENT-CONCENTRATION") || containsFlag(flags, "LOW-SUBSTANTIVE-RATIO")
}
func countMismatch(left, right int) bool {
	difference := abs(left - right)
	return difference > 5 && difference*100 > 20*maxInt(left, right)
}
func appendUniqueFlag(flags []string, flag string) []string {
	if containsFlag(flags, flag) {
		return flags
	}
	flags = append(flags, flag)
	sort.Strings(flags)
	return flags
}
