package agent

import (
	"strings"

	"denova/config"
)

type WritingTurnKind string

const (
	WritingTurnPlanning WritingTurnKind = "planning"
	WritingTurnQuestion WritingTurnKind = "question"
	WritingTurnReview   WritingTurnKind = "review"
	WritingTurnDraft    WritingTurnKind = "draft"
	WritingTurnContinue WritingTurnKind = "continue"
	WritingTurnRewrite  WritingTurnKind = "rewrite"
	WritingTurnPolish   WritingTurnKind = "polish"
)

type WritingSkillLoadRequest struct {
	Name                 string
	RequireLoadedReceipt bool
}

// ResolveWritingSkillName selects the effective Writing Skill name for this IDE
// turn without reading SKILL.md. The model decides whether to load it through
// the skill tool based on the dynamic turn hint.
func ResolveWritingSkillName(cfg *config.Config, selected string) string {
	name := strings.TrimSpace(selected)
	if name == "" && cfg != nil {
		name = strings.TrimSpace(cfg.WritingSkillDefault)
	}
	if name == "" {
		name = config.DefaultWritingSkillName
	}
	return name
}

// ResolveWritingSkillForTurn keeps planning, Q&A, and review rounds outside the
// prose-writing Skill boundary. A name is only a load request; callers must
// still obtain a matching revision/checksum receipt before claiming it loaded.
func ResolveWritingSkillForTurn(cfg *config.Config, selected string, turn WritingTurnKind) (WritingSkillLoadRequest, bool) {
	switch turn {
	case WritingTurnDraft, WritingTurnContinue, WritingTurnRewrite, WritingTurnPolish:
		return WritingSkillLoadRequest{
			Name: ResolveWritingSkillName(cfg, selected), RequireLoadedReceipt: true,
		}, true
	case WritingTurnPlanning, WritingTurnQuestion, WritingTurnReview:
		return WritingSkillLoadRequest{}, false
	default:
		return WritingSkillLoadRequest{}, false
	}
}
