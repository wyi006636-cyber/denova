package skilldiscovery

import (
	"fmt"
	"sort"
	"strings"
)

const shortlistContract = "denova.xiaping-evidence-shortlist"

// BuildShortlist selects inspectable, credible writing candidates from a single snapshot.
func BuildShortlist(snapshotID string, candidates []CandidateRecord, vectors []EvidenceVector, clusters []DuplicateCluster) (Shortlist, error) {
	if strings.TrimSpace(snapshotID) == "" {
		return Shortlist{}, fmt.Errorf("snapshot id is required")
	}
	byID := make(map[string]CandidateRecord, len(candidates))
	for _, candidate := range candidates {
		if candidate.Skill.ID == "" {
			return Shortlist{}, fmt.Errorf("candidate id is required")
		}
		if _, exists := byID[candidate.Skill.ID]; exists {
			return Shortlist{}, fmt.Errorf("duplicate candidate id %q", candidate.Skill.ID)
		}
		byID[candidate.Skill.ID] = candidate
	}
	if err := validateVectors(vectors, byID); err != nil {
		return Shortlist{}, err
	}
	if err := validateClusters(clusters, byID); err != nil {
		return Shortlist{}, err
	}
	clusterByID := clusterMembership(clusters)
	pool := candidatesByCapability(byID, vectors, clusterByID)
	shortlist := Shortlist{Contract: shortlistContract, Version: "v1", SnapshotID: snapshotID, Entries: []ShortlistEntry{}, Gaps: []CapabilityGap{}}
	capabilities := capabilityUniverse(candidates)
	for _, capabilityID := range capabilities {
		entries, gap := selectCapabilityShortlist(capabilityID, pool[capabilityID], clusterByID)
		shortlist.Entries = append(shortlist.Entries, entries...)
		if gap != nil {
			shortlist.Gaps = append(shortlist.Gaps, *gap)
		}
	}
	return shortlist, nil
}

func validateVectors(vectors []EvidenceVector, candidates map[string]CandidateRecord) error {
	seen := map[string]struct{}{}
	for _, vector := range vectors {
		if vector.SkillID == "" || vector.CapabilityID == "" {
			return fmt.Errorf("evidence vector identity is required")
		}
		if _, ok := candidates[vector.SkillID]; !ok {
			return fmt.Errorf("evidence vector has nonexistent candidate %q", vector.SkillID)
		}
		key := vector.SkillID + "\x00" + vector.CapabilityID
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate evidence vector %q", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}
func validateClusters(clusters []DuplicateCluster, candidates map[string]CandidateRecord) error {
	ids, members := map[string]struct{}{}, map[string]struct{}{}
	for _, c := range clusters {
		if c.ClusterID == "" {
			return fmt.Errorf("cluster id is required")
		}
		if _, ok := ids[c.ClusterID]; ok {
			return fmt.Errorf("duplicate cluster id")
		}
		ids[c.ClusterID] = struct{}{}
		if len(c.MemberIDs) == 0 {
			return fmt.Errorf("cluster members required")
		}
		prior := ""
		representative := false
		for _, id := range c.MemberIDs {
			if id == "" || id <= prior {
				return fmt.Errorf("cluster members must be sorted unique")
			}
			prior = id
			if _, ok := candidates[id]; !ok {
				return fmt.Errorf("cluster has unknown candidate")
			}
			if _, ok := members[id]; ok {
				return fmt.Errorf("overlapping cluster member")
			}
			members[id] = struct{}{}
			if id == c.RepresentativeID {
				representative = true
			}
		}
		if !representative {
			return fmt.Errorf("cluster representative must be a member")
		}
	}
	return nil
}
func capabilityUniverse(candidates []CandidateRecord) []string {
	set := map[string]struct{}{}
	for _, id := range CoreCapabilityIDs {
		set[id] = struct{}{}
	}
	for _, c := range candidates {
		for _, m := range c.Capabilities {
			if credibleWritingMatch(c, m.CapabilityID) {
				set[m.CapabilityID] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// BuildShortlistFromSnapshot permits selection only from a completed, identified snapshot.
func BuildShortlistFromSnapshot(snapshot LocalSnapshot, candidates []CandidateRecord, vectors []EvidenceVector, clusters []DuplicateCluster) (Shortlist, error) {
	if snapshot.Manifest.Status != SnapshotComplete {
		return Shortlist{}, fmt.Errorf("complete snapshot required")
	}
	if strings.TrimSpace(snapshot.Manifest.SnapshotID) == "" {
		return Shortlist{}, fmt.Errorf("snapshot identity is required")
	}
	return BuildShortlist(snapshot.Manifest.SnapshotID, candidates, vectors, clusters)
}

type shortlistedCandidate struct {
	candidate CandidateRecord
	vector    EvidenceVector
	cluster   clusterRef
}
type clusterRef struct {
	id, representative string
	members, reasons   []string
}

func clusterMembership(clusters []DuplicateCluster) map[string]clusterRef {
	result := make(map[string]clusterRef)
	for _, cluster := range clusters {
		members := append([]string(nil), cluster.MemberIDs...)
		sort.Strings(members)
		for _, id := range members {
			result[id] = clusterRef{id: cluster.ClusterID, representative: cluster.RepresentativeID, members: members, reasons: append([]string(nil), cluster.Reasons...)}
		}
	}
	return result
}

func candidatesByCapability(candidates map[string]CandidateRecord, vectors []EvidenceVector, clusters map[string]clusterRef) map[string][]shortlistedCandidate {
	result := make(map[string][]shortlistedCandidate)
	for _, vector := range vectors {
		candidate, exists := candidates[vector.SkillID]
		if !exists || !credibleWritingMatch(candidate, vector.CapabilityID) || !isClusterRepresentative(vector.SkillID, clusters, candidates) {
			continue
		}
		result[vector.CapabilityID] = append(result[vector.CapabilityID], shortlistedCandidate{candidate: candidate, vector: vector, cluster: clusters[vector.SkillID]})
	}
	return result
}

func credibleWritingMatch(candidate CandidateRecord, capabilityID string) bool {
	if mediaOnlyCandidate(candidate) {
		return false
	}
	for _, match := range candidate.Capabilities {
		if match.CapabilityID == capabilityID && (match.Status == MatchMatched || (match.Status == MatchAmbiguous && len(match.Evidence) > 0)) {
			return true
		}
	}
	return false
}

func mediaOnlyCandidate(candidate CandidateRecord) bool {
	text := normalizeText(strings.Join(append([]string{candidate.Skill.Name, candidate.Skill.Description}, append(candidate.Skill.Categories, candidate.Skill.Tags...)...), " "))
	mediaTerms := []string{"视频", "video", "漫画", "comic", "分镜", "storyboard", "配图", "插画", "illustration", "有声书", "audiobook", "配音", "voiceover"}
	for _, term := range mediaTerms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func isClusterRepresentative(id string, clusters map[string]clusterRef, candidates map[string]CandidateRecord) bool {
	cluster, found := clusters[id]
	if !found {
		return true
	}
	if cluster.representative != "" {
		return cluster.representative == id
	}
	for _, member := range cluster.members {
		if _, exists := candidates[member]; exists && member < id {
			return false
		}
	}
	return true
}

func sortedCapabilityIDs(pool map[string][]shortlistedCandidate) []string {
	ids := make([]string, 0, len(pool))
	for id := range pool {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func selectCapabilityShortlist(capabilityID string, pool []shortlistedCandidate, clusters map[string]clusterRef) ([]ShortlistEntry, *CapabilityGap) {
	dataRich := append([]shortlistedCandidate(nil), pool...)
	sort.Slice(dataRich, func(i, j int) bool { return dataRichLess(dataRich[i], dataRich[j]) })
	dataRich = filterDataRich(dataRich)
	selected := chooseDiverse(dataRich, 3, nil, nil)
	selectedIDs, owners, clusterIDs := selectionSets(selected)
	exploration := make([]shortlistedCandidate, 0, len(pool))
	for _, item := range pool {
		if _, exists := selectedIDs[item.candidate.Skill.ID]; !exists {
			exploration = append(exploration, item)
		}
	}
	exploration = chooseExploration(exploration, 2, selected, owners, clusterIDs)
	entries := make([]ShortlistEntry, 0, len(selected)+len(exploration))
	for index, item := range selected {
		entries = append(entries, entryFor(item, LaneDataRich, index+1))
	}
	for index, item := range exploration {
		entries = append(entries, entryFor(item, LaneExploration, index+1))
	}
	if len(pool) >= 5 {
		return entries, nil
	}
	return entries, &CapabilityGap{CapabilityID: capabilityID, Wanted: 5, Selected: len(entries), Reason: "fewer than five credible writing candidates"}
}

func chooseExploration(pool []shortlistedCandidate, limit int, selected []shortlistedCandidate, owners, clusters map[string]struct{}) []shortlistedCandidate {
	out := make([]shortlistedCandidate, 0, limit)
	for len(out) < limit && len(pool) > 0 {
		best := -1
		for pass := 0; pass < 3 && best < 0; pass++ {
			for i, item := range pool {
				ownerUsed := false
				_, ownerUsed = owners[ownerIdentity(item.candidate)]
				clusterUsed := false
				if item.cluster.id != "" {
					_, clusterUsed = clusters[item.cluster.id]
				}
				if (pass == 0 && (ownerUsed || clusterUsed)) || (pass == 1 && clusterUsed) {
					continue
				}
				if best < 0 || explorationBetter(item, pool[best], append(selected, out...)) {
					best = i
				}
			}
		}
		if best < 0 {
			break
		}
		item := pool[best]
		out = append(out, item)
		owners[ownerIdentity(item.candidate)] = struct{}{}
		if item.cluster.id != "" {
			clusters[item.cluster.id] = struct{}{}
		}
		pool = append(pool[:best], pool[best+1:]...)
	}
	return out
}
func explorationBetter(left, right shortlistedCandidate, selected []shortlistedCandidate) bool {
	ld, rd := methodDistance(left, selected), methodDistance(right, selected)
	if ld != rd {
		return ld > rd
	}
	le, re := matchedEvidenceCount(left.candidate, left.vector.CapabilityID), matchedEvidenceCount(right.candidate, right.vector.CapabilityID)
	if le != re {
		return le > re
	}
	ln, rn := profileNovelty(left, selected), profileNovelty(right, selected)
	if ln != rn {
		return ln > rn
	}
	if metadataCompleteness(left.candidate) != metadataCompleteness(right.candidate) {
		return metadataCompleteness(left.candidate) > metadataCompleteness(right.candidate)
	}
	return left.candidate.Skill.ID < right.candidate.Skill.ID
}
func methodDistance(item shortlistedCandidate, selected []shortlistedCandidate) float64 {
	if len(selected) == 0 {
		return 1
	}
	min := 1.0
	for _, other := range selected {
		sim := TokenJaccard(item.candidate, other.candidate)
		if 1-sim < min {
			min = 1 - sim
		}
	}
	return min
}
func profileNovelty(item shortlistedCandidate, selected []shortlistedCandidate) int {
	seen := map[string]struct{}{}
	for _, other := range selected {
		for _, p := range other.candidate.Profiles {
			seen[normalizeText(p)] = struct{}{}
		}
	}
	n := 0
	for _, p := range item.candidate.Profiles {
		if _, ok := seen[normalizeText(p)]; !ok && normalizeText(p) != "" {
			n++
		}
	}
	return n
}

func filterDataRich(items []shortlistedCandidate) []shortlistedCandidate {
	result := make([]shortlistedCandidate, 0, len(items))
	for _, item := range items {
		if item.vector.PlatformDataRich {
			result = append(result, item)
		}
	}
	return result
}

func dataRichLess(left, right shortlistedCandidate) bool {
	if severeFlag(left.vector.Review.AnomalyFlags) != severeFlag(right.vector.Review.AnomalyFlags) {
		return !severeFlag(left.vector.Review.AnomalyFlags)
	}
	if left.vector.PlatformDataRich != right.vector.PlatformDataRich {
		return left.vector.PlatformDataRich
	}
	if left.vector.BayesianStarsX100 != right.vector.BayesianStarsX100 {
		return left.vector.BayesianStarsX100 > right.vector.BayesianStarsX100
	}
	if left.vector.Review.EffectiveRaters != right.vector.Review.EffectiveRaters {
		return left.vector.Review.EffectiveRaters > right.vector.Review.EffectiveRaters
	}
	if left.vector.DownloadPercentile != right.vector.DownloadPercentile {
		return left.vector.DownloadPercentile > right.vector.DownloadPercentile
	}
	if left.vector.MaturityVersionCount != right.vector.MaturityVersionCount {
		return left.vector.MaturityVersionCount > right.vector.MaturityVersionCount
	}
	return left.candidate.Skill.ID < right.candidate.Skill.ID
}

func explorationLess(left, right shortlistedCandidate) bool {
	if left.cluster.id == "" != (right.cluster.id == "") {
		return left.cluster.id == ""
	}
	if matchedEvidenceCount(left.candidate, left.vector.CapabilityID) != matchedEvidenceCount(right.candidate, right.vector.CapabilityID) {
		return matchedEvidenceCount(left.candidate, left.vector.CapabilityID) > matchedEvidenceCount(right.candidate, right.vector.CapabilityID)
	}
	if len(left.candidate.Profiles) != len(right.candidate.Profiles) {
		return len(left.candidate.Profiles) > len(right.candidate.Profiles)
	}
	if metadataCompleteness(left.candidate) != metadataCompleteness(right.candidate) {
		return metadataCompleteness(left.candidate) > metadataCompleteness(right.candidate)
	}
	return left.candidate.Skill.ID < right.candidate.Skill.ID
}

func matchedEvidenceCount(candidate CandidateRecord, capabilityID string) int {
	for _, match := range candidate.Capabilities {
		if match.CapabilityID == capabilityID {
			return len(match.Evidence)
		}
	}
	return 0
}
func metadataCompleteness(candidate CandidateRecord) int {
	score := 0
	for _, value := range []string{candidate.Skill.Name, candidate.Skill.Description, candidate.Skill.OwnerID, candidate.Skill.CurrentVersion} {
		if strings.TrimSpace(value) != "" {
			score++
		}
	}
	return score
}

func chooseDiverse(pool []shortlistedCandidate, limit int, usedOwners map[string]struct{}, usedClusters map[string]struct{}) []shortlistedCandidate {
	if usedOwners == nil {
		usedOwners = map[string]struct{}{}
	}
	if usedClusters == nil {
		usedClusters = map[string]struct{}{}
	}
	selected := make([]shortlistedCandidate, 0, limit)
	for len(selected) < limit {
		found := diverseIndex(pool, usedOwners, usedClusters, true, true)
		if found < 0 {
			found = diverseIndex(pool, usedOwners, usedClusters, false, true)
		}
		if found < 0 {
			found = diverseIndex(pool, usedOwners, usedClusters, false, false)
		}
		if found < 0 {
			break
		}
		item := pool[found]
		selected = append(selected, item)
		usedOwners[ownerIdentity(item.candidate)] = struct{}{}
		if item.cluster.id != "" {
			usedClusters[item.cluster.id] = struct{}{}
		}
		pool = append(pool[:found], pool[found+1:]...)
	}
	return selected
}

func diverseIndex(pool []shortlistedCandidate, owners, clusters map[string]struct{}, requireNewOwner, requireNewCluster bool) int {
	for index, item := range pool {
		if item.candidate.Skill.ID == "" {
			continue
		}
		if requireNewOwner {
			if _, used := owners[ownerIdentity(item.candidate)]; used {
				continue
			}
		}
		if requireNewCluster && item.cluster.id != "" {
			if _, used := clusters[item.cluster.id]; used {
				continue
			}
		}
		return index
	}
	return -1
}

func selectionSets(items []shortlistedCandidate) (map[string]struct{}, map[string]struct{}, map[string]struct{}) {
	ids, owners, clusters := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for _, item := range items {
		ids[item.candidate.Skill.ID] = struct{}{}
		owners[ownerIdentity(item.candidate)] = struct{}{}
		if item.cluster.id != "" {
			clusters[item.cluster.id] = struct{}{}
		}
	}
	return ids, owners, clusters
}
func ownerIdentity(candidate CandidateRecord) string {
	if id := normalizeText(candidate.Skill.OwnerID); id != "" {
		return "id:" + id
	}
	if name := normalizeText(candidate.Skill.OwnerName); name != "" {
		return "name:" + name
	}
	return "unknown:" + candidate.Skill.ID
}
func entryFor(item shortlistedCandidate, lane ShortlistLane, rank int) ShortlistEntry {
	reasons := []string{"capability evidence retained", "selection evidence vector embedded"}
	if item.cluster.id != "" {
		reasons = append(reasons, "cluster:"+item.cluster.id, "cluster_members:"+strings.Join(item.cluster.members, ","))
		for _, reason := range item.cluster.reasons {
			reasons = append(reasons, "cluster_reason:"+reason)
		}
	}
	return ShortlistEntry{SkillID: item.candidate.Skill.ID, CapabilityID: item.vector.CapabilityID, Lane: lane, Rank: rank, Reasons: reasons, Evidence: item.vector}
}
