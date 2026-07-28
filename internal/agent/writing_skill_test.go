package agent

import (
	"strings"
	"testing"

	"denova/config"
)

func TestResolveWritingSkillNameDefaultsAndSelection(t *testing.T) {
	if got := ResolveWritingSkillName(&config.Config{WritingSkillDefault: "novel-heavy"}, ""); got != "novel-heavy" {
		t.Fatalf("default writing skill = %s, want novel-heavy", got)
	}
	if got := ResolveWritingSkillName(&config.Config{WritingSkillDefault: "novel-heavy"}, "slow-burn"); got != "slow-burn" {
		t.Fatalf("selected writing skill = %s, want slow-burn", got)
	}
	if got := ResolveWritingSkillName(&config.Config{}, ""); got != config.DefaultWritingSkillName {
		t.Fatalf("fallback writing skill = %s, want %s", got, config.DefaultWritingSkillName)
	}
}

func TestComposeAgentInputAddsWritingSkillLoadHintWithoutSkillBody(t *testing.T) {
	composition := composeAgentInput(ChatRequest{
		Message:      "帮我分析一下 progress.md 有没有问题",
		WritingSkill: "novel-standard",
	}, nil, nil, DefaultLoopPolicy())

	for _, want := range []string{"Writing Skill 按需加载提示", "当前创作 Agent 选中的 Writing Skill 是 `novel-standard`", "当前 Agent 已启用 `skill` 工具", "调用 `skill` 工具加载 `novel-standard`", "不要假装已经读取了该 Skill 的完整说明", "不存在单独的 `writing_scope` 字段"} {
		if !strings.Contains(composition.AgentMessage, want) {
			t.Fatalf("writing skill hint missing %q:\n%s", want, composition.AgentMessage)
		}
	}
	for _, notWant := range []string{"```markdown", "SKILL.md 是本轮 IDE 创作 Agent 必须遵循"} {
		if strings.Contains(composition.AgentMessage, notWant) {
			t.Fatalf("writing skill body should not be injected, found %q:\n%s", notWant, composition.AgentMessage)
		}
	}
}

func TestComposeAgentInputLoadsFanqieSkillForConversationStages(t *testing.T) {
	composition := composeAgentInput(ChatRequest{
		Message:      "我有一个短篇想法，先聊聊故事方案",
		WritingSkill: "fanqie-short",
	}, nil, nil, DefaultLoopPolicy())

	for _, want := range []string{
		"当前创作 Agent 选中的 Writing Skill 是 `fanqie-short`",
		"构思、故事方案、分章大纲、逐章写作或正文修改",
		"先调用 `skill` 工具加载 `fanqie-short`",
	} {
		if !strings.Contains(composition.AgentMessage, want) {
			t.Fatalf("fanqie-short workflow hint missing %q:\n%s", want, composition.AgentMessage)
		}
	}
	if strings.Contains(composition.AgentMessage, "大纲/设定讨论、配置或规划，不要加载 Writing Skill") {
		t.Fatalf("fanqie-short planning turn must load its workflow Skill:\n%s", composition.AgentMessage)
	}
}

func TestComposeAgentInputUsesPreloadedFanqieSkillEntry(t *testing.T) {
	composition := composeAgentInput(ChatRequest{
		Message:                   "这篇故事剧情太平了，先帮我看看怎么改",
		WritingSkill:              "fanqie-short",
		WritingSkillContent:       "# 番茄短篇对话创作\n\nPRELOADED_SKILL_MARKER",
		WritingSkillBaseDirectory: "/skills/fanqie-short",
	}, nil, nil, DefaultLoopPolicy())

	for _, want := range []string{"Writing Skill 主入口", "PRELOADED_SKILL_MARKER", "/skills/fanqie-short"} {
		if !strings.Contains(composition.AgentMessage, want) {
			t.Fatalf("preloaded fanqie-short context missing %q:\n%s", want, composition.AgentMessage)
		}
	}
	if strings.Contains(composition.AgentMessage, "请先调用 `skill` 工具加载 `fanqie-short`") {
		t.Fatalf("preloaded fanqie-short should not rely on a later skill tool call:\n%s", composition.AgentMessage)
	}
}
