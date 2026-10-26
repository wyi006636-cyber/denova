package casebook

import (
	"strings"
	"testing"
)

func TestParseCaseMarkdownRequiresExactCollectedStatus(t *testing.T) {
	base := "# c-opening Hook\n\n- 状态：待确认\n- 方向：开篇\n- 适用场景：章首\n- 方法：先写异常\n- 不要搬用：不使用原作事实\n- 来源：references/sources/tide.txt\n- 定位：第 1 行\n- 原文摘录：灯亮了。\n"
	parsed, err := ParseCaseMarkdown(base)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Status != StatusPending || parsed.ID != "c-opening" || parsed.Source != "references/sources/tide.txt" {
		t.Fatalf("parsed = %#v", parsed)
	}
	parsed, err = ParseCaseMarkdown(strings.Replace(base, "待确认", "已收藏", 1))
	if err != nil || parsed.Status != StatusCollected {
		t.Fatalf("collected parse = %#v, err=%v", parsed, err)
	}
	parsed, err = ParseCaseMarkdown(strings.Replace(base, "待确认", "收藏", 1))
	if err != nil || parsed.Status != StatusPending {
		t.Fatalf("non-exact status must stay pending: %#v, err=%v", parsed, err)
	}
}

func TestQuoteLocatedUniqueAndLine(t *testing.T) {
	location, err := QuoteLocated("第一行\n灯亮了。\n第三行", "灯亮了。")
	if err != nil || location.Line != 2 {
		t.Fatalf("location = %#v, err=%v", location, err)
	}
	if _, err := QuoteLocated("相同\n相同", "相同"); err == nil {
		t.Fatal("duplicate quote must fail")
	}
	location, err = QuoteLocatedWithContext("相同\n甲相同乙\n相同", "相同", "甲", "乙")
	if err != nil || location.Line != 2 {
		t.Fatalf("context location = %#v, err=%v", location, err)
	}
	if _, err := QuoteLocated("没有", "找不到"); err == nil {
		t.Fatal("missing quote must fail")
	}
	if _, err := QuoteLocated("短", strings.Repeat("字", MaxQuoteRunes+1)); err == nil {
		t.Fatal("long quote must fail")
	}
	if _, err := QuoteLocated("短", "  "); err == nil {
		t.Fatal("empty quote must fail")
	}
}

func TestPatchCaseAndIndexStatus(t *testing.T) {
	caseMarkdown := "# c-one One\n- 状态：待确认\n- 定位：未知\n"
	updated, err := PatchCaseStatus(caseMarkdown, StatusCollected, 12)
	if err != nil || !strings.Contains(updated, "状态：已收藏") || !strings.Contains(updated, "定位：第 12 行") {
		t.Fatalf("updated case = %q, err=%v", updated, err)
	}
	index := "# 写法案例\n\n## 已收藏\n\n## 待确认\n\n- c-one | 开篇 | 先写异常\n"
	updated, err = PatchIndexStatus(index, "c-one", StatusCollected)
	if err != nil || !strings.Contains(updated, "## 已收藏\n\n- c-one") || strings.Contains(updated, "## 待确认\n\n- c-one") {
		t.Fatalf("updated index = %q, err=%v", updated, err)
	}
}

func TestMethodOnlyOmitsSourceAndQuote(t *testing.T) {
	input := Case{Status: StatusCollected, Direction: "开篇", Scenario: "章首", Method: "先写异常", Avoid: "不使用原作", Source: "references/sources/tide.txt", Quote: "灯亮了"}
	method := MethodOnly(input)
	if !strings.Contains(method, "方法：先写异常") || strings.Contains(method, "灯亮了") || strings.Contains(method, "references/sources") {
		t.Fatalf("method = %q", method)
	}
	if got := MethodOnly(Case{Status: StatusPending, Method: "pending"}); got != "" {
		t.Fatalf("pending method = %q", got)
	}
}
