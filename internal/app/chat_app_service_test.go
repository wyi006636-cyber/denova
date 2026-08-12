package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/config"
	"denova/internal/agent"
)

func TestApplyWritingSkillRuntimePolicyResolvesDefaultNameOnly(t *testing.T) {
	runtime := &ideChatRuntime{cfg: config.Config{
		WritingSkillDefault: "novel-heavy",
		SubAgents: []config.SubAgentConfig{{
			ID:           "researcher",
			Description:  "Reads context.",
			SystemPrompt: "Return notes.",
		}},
	}}
	req := &agent.ChatRequest{Message: "帮我分析一下 progress.md 有没有问题"}

	if err := applyWritingSkillRuntimePolicy(context.Background(), runtime, req); err != nil {
		t.Fatal(err)
	}
	if req.WritingSkill != "novel-heavy" {
		t.Fatalf("writing skill = %s, want novel-heavy", req.WritingSkill)
	}
	if len(runtime.cfg.SubAgents) != 1 || runtime.cfg.SubAgents[0].ID != "researcher" {
		t.Fatalf("writing skill selection should not mutate subagents: %+v", runtime.cfg.SubAgents)
	}
}

func TestApplyWritingSkillRuntimePolicyKeepsCustomSkillAsDynamicHintOnly(t *testing.T) {
	runtime := &ideChatRuntime{cfg: config.Config{WritingSkillDefault: "novel-standard"}}
	req := &agent.ChatRequest{Message: "写一个雨夜重逢的场景", WritingSkill: "slow-burn"}

	if err := applyWritingSkillRuntimePolicy(context.Background(), runtime, req); err != nil {
		t.Fatal(err)
	}
	if req.WritingSkill != "slow-burn" {
		t.Fatalf("writing skill = %s, want slow-burn", req.WritingSkill)
	}
	if runtime.cfg.GeneralSubAgents.IDE != nil || len(runtime.cfg.SubAgents) != 0 {
		t.Fatalf("writing skill selection should not mutate agent config: %+v", runtime.cfg)
	}
}

func TestApplyWritingSkillRuntimePolicyPreloadsFanqieMainEntry(t *testing.T) {
	builtinDir := t.TempDir()
	skillDir := filepath.Join(builtinDir, "fanqie-short")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: fanqie-short\ndescription: test\nagent: ide\n---\n\n# Test entry\n\nPRELOADED_SKILL_MARKER\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime := &ideChatRuntime{cfg: config.Config{SkillsDir: builtinDir}}
	req := &agent.ChatRequest{Message: "剧情太平了", WritingSkill: "fanqie-short"}

	if err := applyWritingSkillRuntimePolicy(context.Background(), runtime, req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.WritingSkillContent, "PRELOADED_SKILL_MARKER") {
		t.Fatalf("fanqie-short main entry was not preloaded: %q", req.WritingSkillContent)
	}
	if req.WritingSkillBaseDirectory != skillDir {
		t.Fatalf("fanqie-short base directory = %q, want %q", req.WritingSkillBaseDirectory, skillDir)
	}
}
