package skilldiscovery

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const capabilityLexiconContract = "denova.xiaping-capability-lexicon"

var proposalCapabilityIDs = []string{
	"worldbuilding.build-rules",
	"research.verify-story-facts",
	"emotion.manage-character-arc",
	"scene.draft-from-brief",
	"platform.adapt-writing-style",
}

// Lexicon is the versioned, reviewable vocabulary for deterministic capability recall.
type Lexicon struct {
	Contract         string           `json:"contract"`
	Version          string           `json:"version"`
	IncludeTerms     []string         `json:"include_terms"`
	ExcludeTerms     []string         `json:"exclude_terms"`
	CoreCapabilities []CapabilityRule `json:"core_capabilities"`
	ProposalRules    []CapabilityRule `json:"proposal_rules"`
}

// CapabilityRule describes either an approved capability or a non-routable proposal.
type CapabilityRule struct {
	CapabilityID      string   `json:"capability_id"`
	Terms             []string `json:"terms"`
	NameZH            string   `json:"name_zh,omitempty"`
	NameEN            string   `json:"name_en,omitempty"`
	Inputs            []string `json:"inputs,omitempty"`
	Outputs           []string `json:"outputs,omitempty"`
	LifecycleStage    string   `json:"lifecycle_stage,omitempty"`
	MinimumPermission string   `json:"minimum_permission,omitempty"`
	EvaluationMethod  string   `json:"evaluation_method,omitempty"`
}

// LoadLexicon reads and validates a committed capability vocabulary.
func LoadLexicon(path string) (Lexicon, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Lexicon{}, fmt.Errorf("read capability lexicon: %w", err)
	}
	var lexicon Lexicon
	if err := json.Unmarshal(payload, &lexicon); err != nil {
		return Lexicon{}, fmt.Errorf("decode capability lexicon: %w", err)
	}
	if err := validateLexicon(lexicon); err != nil {
		return Lexicon{}, err
	}
	return lexicon, nil
}

// ClassifyWritingCandidates returns sorted recall candidates and non-routable proposals.
func ClassifyWritingCandidates(records []SkillRecord, lexicon Lexicon) ([]CandidateRecord, []CapabilityProposal) {
	ordered := append([]SkillRecord(nil), records...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	rules := append(append([]CapabilityRule(nil), lexicon.CoreCapabilities...), lexicon.ProposalRules...)
	candidates := make([]CandidateRecord, 0, len(ordered))
	for _, record := range ordered {
		fields := skillFields(record)
		includeEvidence := evidenceForTerms(fields, lexicon.IncludeTerms)
		if isExcludedMediaOnly(fields, lexicon.ExcludeTerms, includeEvidence) {
			continue
		}
		matches := make([]CapabilityMatch, 0, len(rules))
		for _, rule := range rules {
			evidence := evidenceForTerms(fields, rule.Terms)
			if len(evidence) == 0 {
				continue
			}
			match := CapabilityMatch{CapabilityID: rule.CapabilityID, Status: matchStatus(evidence), Evidence: evidence}
			matches = append(matches, match)
		}
		if len(includeEvidence) == 0 && len(matches) == 0 {
			continue
		}
		sort.Slice(matches, func(i, j int) bool { return matches[i].CapabilityID < matches[j].CapabilityID })
		candidates = append(candidates, CandidateRecord{Skill: record, Capabilities: matches})
	}
	return candidates, BuildCapabilityProposals(candidates, lexicon)
}

// BuildCapabilityProposals emits proposals only when independently described by two skills.
func BuildCapabilityProposals(candidates []CandidateRecord, lexicon Lexicon) []CapabilityProposal {
	provisional := make(map[string][]proposalSignal, len(lexicon.ProposalRules))
	proposalIDs := make(map[string]struct{}, len(lexicon.ProposalRules))
	for _, rule := range lexicon.ProposalRules {
		proposalIDs[rule.CapabilityID] = struct{}{}
	}
	for _, candidate := range candidates {
		for _, capability := range candidate.Capabilities {
			if _, isProposal := proposalIDs[capability.CapabilityID]; isProposal {
				provisional[capability.CapabilityID] = append(provisional[capability.CapabilityID], proposalSignal{skillID: candidate.Skill.ID, signature: metadataSignature(skillFields(candidate.Skill))})
			}
		}
	}
	proposals := make([]CapabilityProposal, 0, len(lexicon.ProposalRules))
	for _, rule := range lexicon.ProposalRules {
		bySignature := make(map[string]string)
		for _, signal := range provisional[rule.CapabilityID] {
			if previous, exists := bySignature[signal.signature]; !exists || signal.skillID < previous {
				bySignature[signal.signature] = signal.skillID
			}
		}
		if len(bySignature) < 2 {
			continue
		}
		idSet := make(map[string]struct{}, len(bySignature))
		for _, id := range bySignature {
			idSet[id] = struct{}{}
		}
		if len(idSet) < 2 {
			continue
		}
		ids := make([]string, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		proposals = append(proposals, CapabilityProposal{CapabilityID: rule.CapabilityID, NameZH: rule.NameZH, NameEN: rule.NameEN, Inputs: append([]string(nil), rule.Inputs...), Outputs: append([]string(nil), rule.Outputs...), LifecycleStage: rule.LifecycleStage, MinimumPermission: rule.MinimumPermission, EvaluationMethod: rule.EvaluationMethod, CandidateIDs: ids})
	}
	sort.Slice(proposals, func(i, j int) bool { return proposals[i].CapabilityID < proposals[j].CapabilityID })
	return proposals
}

type normalizedField struct{ name, value string }
type proposalSignal struct{ skillID, signature string }

func skillFields(record SkillRecord) []normalizedField {
	return []normalizedField{
		{name: "name", value: normalizeText(record.Name)},
		{name: "description", value: normalizeText(record.Description)},
		{name: "triggers", value: normalizeText(strings.Join(record.Triggers, " "))},
		{name: "categories", value: normalizeText(strings.Join(record.Categories, " "))},
		{name: "tags", value: normalizeText(strings.Join(record.Tags, " "))},
	}
}

func normalizeText(value string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(norm.NFKC.String(value)), unicode.IsSpace), " ")
}

func evidenceForTerms(fields []normalizedField, terms []string) []FieldEvidence {
	evidence := make([]FieldEvidence, 0)
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		for _, term := range terms {
			normalizedTerm := normalizeText(term)
			if normalizedTerm != "" && strings.Contains(field.value, normalizedTerm) {
				evidence = append(evidence, FieldEvidence{Field: field.name, Term: normalizedTerm})
			}
		}
	}
	return evidence
}

func isExcludedMediaOnly(fields []normalizedField, excluded []string, includeEvidence []FieldEvidence) bool {
	if len(evidenceForTerms(fields, excluded)) == 0 {
		return false
	}
	if hasExplicitWritingTransformation(fields) {
		return false
	}
	// A bare work-type label (for example "小说转视频") is not direct writing work.
	for _, evidence := range includeEvidence {
		if evidence.Term != "小说" && evidence.Term != "故事" {
			return false
		}
	}
	return true
}

func hasExplicitWritingTransformation(fields []normalizedField) bool {
	for _, field := range fields {
		if strings.Contains(field.value, "改写") && (strings.Contains(field.value, "小说") || strings.Contains(field.value, "故事")) {
			return true
		}
	}
	return false
}

func matchStatus(evidence []FieldEvidence) MatchStatus {
	fields := make(map[string]struct{})
	for _, item := range evidence {
		if item.Field == "name" || item.Field == "triggers" {
			return MatchMatched
		}
		fields[item.Field] = struct{}{}
	}
	if len(fields) >= 2 {
		return MatchMatched
	}
	return MatchAmbiguous
}

func metadataSignature(fields []normalizedField) string {
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		values = append(values, field.name+"="+field.value)
	}
	return strings.Join(values, "|")
}

func validateLexicon(lexicon Lexicon) error {
	if lexicon.Contract != capabilityLexiconContract || strings.TrimSpace(lexicon.Version) == "" || len(lexicon.IncludeTerms) == 0 {
		return fmt.Errorf("invalid capability lexicon contract, version, or include terms")
	}
	if err := uniqueNonEmptyTerms("include", lexicon.IncludeTerms); err != nil {
		return err
	}
	if err := uniqueNonEmptyTerms("exclude", lexicon.ExcludeTerms); err != nil {
		return err
	}
	core := make(map[string]struct{}, len(lexicon.CoreCapabilities))
	if len(lexicon.CoreCapabilities) != len(CoreCapabilityIDs) {
		return fmt.Errorf("core capability count = %d, want %d", len(lexicon.CoreCapabilities), len(CoreCapabilityIDs))
	}
	for _, rule := range lexicon.CoreCapabilities {
		if err := validateRule(rule, false); err != nil {
			return err
		}
		if _, exists := core[rule.CapabilityID]; exists {
			return fmt.Errorf("duplicate core capability %q", rule.CapabilityID)
		}
		core[rule.CapabilityID] = struct{}{}
	}
	for _, id := range CoreCapabilityIDs {
		if _, exists := core[id]; !exists {
			return fmt.Errorf("missing approved core capability %q", id)
		}
	}
	proposals := make(map[string]struct{}, len(lexicon.ProposalRules))
	for _, rule := range lexicon.ProposalRules {
		if err := validateRule(rule, true); err != nil {
			return err
		}
		if _, exists := core[rule.CapabilityID]; exists {
			return fmt.Errorf("proposal overlaps core capability %q", rule.CapabilityID)
		}
		if _, exists := proposals[rule.CapabilityID]; exists {
			return fmt.Errorf("duplicate proposal capability %q", rule.CapabilityID)
		}
		proposals[rule.CapabilityID] = struct{}{}
	}
	if len(proposals) != len(proposalCapabilityIDs) {
		return fmt.Errorf("proposal capability count = %d, want %d", len(proposals), len(proposalCapabilityIDs))
	}
	for _, id := range proposalCapabilityIDs {
		if _, exists := proposals[id]; !exists {
			return fmt.Errorf("missing approved proposal capability %q", id)
		}
	}
	return nil
}

func uniqueNonEmptyTerms(label string, terms []string) error {
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		normalized := normalizeText(term)
		if normalized == "" {
			return fmt.Errorf("%s term is empty", label)
		}
		if _, exists := seen[normalized]; exists {
			return fmt.Errorf("duplicate %s term %q", label, normalized)
		}
		seen[normalized] = struct{}{}
	}
	return nil
}

func validateRule(rule CapabilityRule, proposal bool) error {
	if strings.TrimSpace(rule.CapabilityID) == "" {
		return fmt.Errorf("capability rule ID is empty")
	}
	if err := uniqueNonEmptyTerms("capability", rule.Terms); err != nil {
		return err
	}
	if proposal && (strings.TrimSpace(rule.NameZH) == "" || strings.TrimSpace(rule.NameEN) == "" || len(rule.Inputs) == 0 || len(rule.Outputs) == 0 || strings.TrimSpace(rule.LifecycleStage) == "" || strings.TrimSpace(rule.MinimumPermission) == "" || strings.TrimSpace(rule.EvaluationMethod) == "") {
		return fmt.Errorf("proposal %q lacks required metadata", rule.CapabilityID)
	}
	return nil
}
