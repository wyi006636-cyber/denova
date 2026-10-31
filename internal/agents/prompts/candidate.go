package prompts

import (
	"fmt"
	"strings"

	"denova/internal/book"
)

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

// candidateEmptyWorkspaceHint replaces the interactive new-work onboarding
// for read-only candidates, which can neither ask the author nor write files.
const candidateEmptyWorkspaceHint = "This work has no outline, lore, or indexed writing sources yet. This is a read-only draft: write the chapter from the chapter task and the candidate instructions alone. Do not ask the author questions, propose a setup plan, or create or modify files; output only the chapter prose."

// CandidateWorkspaceRuntimeContexts builds the frozen writing context for one
// comparison run. Candidates run without tools and receive selected file
// bodies inlined in the same snapshot, so this variant describes inlined
// sources instead of file/Lore tool navigation and never injects the
// interactive new-work onboarding. The ordinary writing Agent keeps using
// IDEWorkspaceRuntimeContextsForContextWithLore, whose text is unchanged.
// Callers must inline the target and workspace files they freeze under
// sections titled with each file path.
func CandidateWorkspaceRuntimeContexts(state *book.State, targetPath, loreAsOf, loreScope string) IDEWorkspaceRuntimeContexts {
	contexts := workspaceRuntimeContexts(state, book.WorkspaceContextReadOnlySnapshot, candidateEmptyWorkspaceHint)
	if target := boundedIDEContextPath(targetPath); target != "" {
		section := fmt.Sprintf("## Candidate Target\n\n- Target chapter path: %s\n- Existing target text, if any, is inlined later in this snapshot under the same path.", target)
		contexts.Dynamic = strings.TrimSpace(strings.Join(nonEmptyStrings(contexts.Dynamic, section), "\n\n"))
	}
	contexts.Lore = chapterLoreContext(state, targetPath, loreAsOf, loreScope)
	return contexts
}
