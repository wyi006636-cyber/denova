package yanzhouadapter

import "testing"

func TestCleanWritingCandidateContent(t *testing.T) {
	plain := "林照没有说话，转身走向警车。"
	if got, ok := cleanWritingCandidateContent(plain); !ok || got != plain {
		t.Fatalf("plain prose changed: ok=%v got=%q", ok, got)
	}

	leaked := `<tool_calls>
<invoke name="novel_diff">
<parameter name="action" string="true">preview</parameter>
<parameter name="diffContent" string="true">林照抬起头。</parameter>
</invoke>
</tool_calls>`
	want := "林照抬起头。"
	if got, ok := cleanWritingCandidateContent(leaked); !ok || got != want {
		t.Fatalf("dsml leak not unwrapped: ok=%v got=%q want=%q", ok, got, want)
	}

	if _, ok := cleanWritingCandidateContent(`<tool_calls>
<invoke name="novel_diff">
</invoke>
</tool_calls>`); ok {
		t.Fatal("empty DSML shell must be rejected")
	}
}
