package shortfiction

import (
	"strings"
	"testing"
)

func TestFanqieCandidateIDBindsEveryAuthorityField(t *testing.T) {
	source := SourcePacket{Workspace: "/tmp/book", TargetPath: "chapters/short.md", BaseRevision: "missing", Brief: "一名外卖员发现订单来自明天。", Locale: "zh-CN"}
	generation := Generation{PreviewMarkdown: "# 明日订单\n\n正文", ModelProfileID: "writer", Model: "test-model"}
	first, err := NewCandidate(source, generation)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCandidate(source, generation)
	if err != nil {
		t.Fatal(err)
	}
	if first.CandidateID == "" || first.CandidateID != second.CandidateID {
		t.Fatalf("candidate ids = %q / %q", first.CandidateID, second.CandidateID)
	}
	mutated := first
	mutated.PreviewMarkdown += "被篡改"
	if err := ValidateCandidate(mutated); !IsCode(err, ErrorCodeCandidateMismatch) {
		t.Fatalf("error = %v", err)
	}
}

func TestFanqieCandidateRejectsUnknownProfileWithoutFallback(t *testing.T) {
	candidate, err := NewCandidate(
		SourcePacket{Workspace: "/tmp/book", TargetPath: "chapters/short.md", BaseRevision: MissingRevision, Brief: "一个人等待答案。", Locale: "zh-CN"},
		Generation{PreviewMarkdown: "# 等待\n\n正文", ModelProfileID: "writer", Model: "test-model"},
	)
	if err != nil {
		t.Fatal(err)
	}
	candidate.ProfileID = "unknown"
	if err := ValidateCandidate(candidate); !IsCode(err, ErrorCodeInvalidProfile) {
		t.Fatalf("error = %v", err)
	}
}

func TestFanqieCandidateRejectsInvalidMarkdownTarget(t *testing.T) {
	for _, target := range []string{"chapters/short.txt", "/tmp/short.md", "../short.md", ".draft/short.md", "chapters/.short.md"} {
		t.Run(target, func(t *testing.T) {
			_, err := NewCandidate(
				SourcePacket{Workspace: "/tmp/book", TargetPath: target, BaseRevision: MissingRevision, Brief: "一个人等待答案。", Locale: "zh-CN"},
				Generation{PreviewMarkdown: "# 等待\n\n正文", ModelProfileID: "writer", Model: "test-model"},
			)
			if !IsCode(err, ErrorCodeInvalidSource) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestFanqieCandidateRejectsOversizedBriefSourceAndOutput(t *testing.T) {
	validSource := SourcePacket{Workspace: "/tmp/book", TargetPath: "chapters/short.md", BaseRevision: MissingRevision, Brief: "一个人等待答案。", Locale: "zh-CN"}
	validGeneration := Generation{PreviewMarkdown: "# 等待\n\n正文", ModelProfileID: "writer", Model: "test-model"}
	for _, test := range []struct {
		name       string
		source     SourcePacket
		generation Generation
	}{
		{name: "brief", source: SourcePacket{Workspace: validSource.Workspace, TargetPath: validSource.TargetPath, BaseRevision: validSource.BaseRevision, Brief: strings.Repeat("b", MaxBriefBytes+1), Locale: validSource.Locale}, generation: validGeneration},
		{name: "source", source: SourcePacket{Workspace: validSource.Workspace, TargetPath: validSource.TargetPath, BaseRevision: validSource.BaseRevision, Brief: validSource.Brief, Source: strings.Repeat("s", MaxSourceBytes+1), Locale: validSource.Locale}, generation: validGeneration},
		{name: "output", source: validSource, generation: Generation{PreviewMarkdown: strings.Repeat("m", MaxCandidateBytes+1), ModelProfileID: validGeneration.ModelProfileID, Model: validGeneration.Model}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewCandidate(test.source, test.generation)
			if !IsCode(err, ErrorCodeOversized) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestFanqiePromptRequestsOneCompleteStoryWithoutWriteClaims(t *testing.T) {
	prompt := FanqieSystemPrompt()
	for _, requirement := range []string{
		"one complete Markdown story",
		"clear premise",
		"protagonist desire",
		"immediate pressure",
		"early hook",
		"payoff",
		"Do not use a code fence.",
		"Do not claim to write to a workspace or use tools.",
	} {
		if !strings.Contains(prompt, requirement) {
			t.Fatalf("prompt missing %q: %q", requirement, prompt)
		}
	}
}

func TestFanqieCandidateRejectsBlankBriefWithoutTrimmingContent(t *testing.T) {
	_, err := NewCandidate(
		SourcePacket{Workspace: "/tmp/book", TargetPath: "chapters/short.md", BaseRevision: MissingRevision, Brief: " \n\t ", Locale: "zh-CN"},
		Generation{PreviewMarkdown: "# 等待\n\n正文", ModelProfileID: "writer", Model: "test-model"},
	)
	if !IsCode(err, ErrorCodeInvalidSource) {
		t.Fatalf("error = %v", err)
	}

	candidate, err := NewCandidate(
		SourcePacket{Workspace: "/tmp/book", TargetPath: "chapters/short.md", BaseRevision: MissingRevision, Brief: "  保留空格  ", Locale: "zh-CN"},
		Generation{PreviewMarkdown: "# 等待\n\n正文", ModelProfileID: "writer", Model: "test-model"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Brief != "  保留空格  " {
		t.Fatalf("brief = %q", candidate.Brief)
	}
}

func TestFanqieCandidateRequiresWorkspaceChangeRevisionFormat(t *testing.T) {
	for _, revision := range []string{"", "sha256:not-hex", "sha1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		t.Run(revision, func(t *testing.T) {
			_, err := NewCandidate(
				SourcePacket{Workspace: "/tmp/book", TargetPath: "chapters/short.md", BaseRevision: revision, Brief: "一个人等待答案。", Locale: "zh-CN"},
				Generation{PreviewMarkdown: "# 等待\n\n正文", ModelProfileID: "writer", Model: "test-model"},
			)
			if !IsCode(err, ErrorCodeInvalidSource) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
