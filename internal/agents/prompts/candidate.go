package prompts

import "strings"

// CandidateContractText and TargetHintText are the single source for the
// candidate prompt contract. The template preview endpoint imports these same
// constants, so its three segments cannot drift from the model system prompt.
const CandidateContractText = `Produce one complete chapter as prose only. Do not output an explanation, title, outline, JSON, tool call, or author note. If the target chapter already exists, produce the complete replacement. Follow the chapter task and frozen context, and never invent story facts absent from that context.`

const TargetHintText = `Target hint: use the supplied target path, chapter cursor, existing-text state, and word target when present. Keep the target chapter as the only unit of work.`

// ComposeCandidateDraftInstruction deterministically builds the exact system
// instruction sent to a candidate model. The source body is one frozen
// template or Skill body; targetHint is task-derived and may be empty.
func ComposeCandidateDraftInstruction(sourceBody, targetHint string) string {
	if strings.TrimSpace(targetHint) == "" {
		targetHint = TargetHintText
	}
	parts := []string{CandidateContractText, strings.TrimSpace(sourceBody), strings.TrimSpace(targetHint)}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, strings.TrimSpace(part))
		}
	}
	return strings.Join(result, "\n\n")
}
