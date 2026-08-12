package yanzhouadapter

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	planQuestionsOpenTag  = "<plan_questions>"
	planQuestionsCloseTag = "</plan_questions>"
	proposedPlanOpenTag   = "<proposed_plan>"
	proposedPlanCloseTag  = "</proposed_plan>"
)

type PlanBlockKind string

const (
	PlanBlockQuestions PlanBlockKind = "questions"
	PlanBlockProposal  PlanBlockKind = "proposal"
)

type PlanBlock struct {
	Kind    PlanBlockKind
	Content string
}

type PlanParseResult struct {
	Visible string
	Blocks  []PlanBlock
	Stop    bool
}

type PlanStreamParser struct {
	buffer      strings.Builder
	block       PlanBlockKind
	blockBuffer strings.Builder
	stopped     bool
}

func NewPlanStreamParser() *PlanStreamParser {
	return &PlanStreamParser{}
}

func (p *PlanStreamParser) Stopped() bool {
	return p != nil && p.stopped
}

func (p *PlanStreamParser) Push(content string) (PlanParseResult, error) {
	if p == nil {
		return PlanParseResult{}, errors.New("plan stream parser is required")
	}
	if p.stopped || content == "" {
		return PlanParseResult{Stop: p.stopped}, nil
	}
	p.buffer.WriteString(content)
	return p.drain(false)
}

func (p *PlanStreamParser) Flush() (PlanParseResult, error) {
	if p == nil {
		return PlanParseResult{}, errors.New("plan stream parser is required")
	}
	if p.stopped {
		return PlanParseResult{Stop: true}, nil
	}
	return p.drain(true)
}

func (p *PlanStreamParser) drain(flush bool) (PlanParseResult, error) {
	var visible strings.Builder
	for {
		buffer := p.buffer.String()
		if p.block != "" {
			closeTag := planCloseTag(p.block)
			if _, nestedIndex, _ := nextPlanOpenTag(buffer); nestedIndex >= 0 {
				if closeIndex := strings.Index(buffer, closeTag); closeIndex < 0 || nestedIndex < closeIndex {
					p.resetBlock()
					return PlanParseResult{Visible: visible.String()}, errors.New("nested plan control block is invalid")
				}
			}
			if index := strings.Index(buffer, closeTag); index >= 0 {
				p.blockBuffer.WriteString(buffer[:index])
				content := strings.TrimSpace(p.blockBuffer.String())
				kind := p.block
				p.buffer.Reset()
				p.resetBlock()
				if content == "" {
					return PlanParseResult{Visible: visible.String()}, errors.New("plan control block content is empty")
				}
				p.stopped = true
				return PlanParseResult{Visible: visible.String(), Blocks: []PlanBlock{{Kind: kind, Content: content}}, Stop: true}, nil
			}
			if flush {
				p.buffer.Reset()
				p.resetBlock()
				return PlanParseResult{Visible: visible.String()}, errors.New("plan control block is not closed")
			}
			retain := longestPlanTagPrefixSuffix(buffer, []string{closeTag, planQuestionsOpenTag, proposedPlanOpenTag})
			if len(buffer) > retain {
				p.blockBuffer.WriteString(buffer[:len(buffer)-retain])
				p.buffer.Reset()
				p.buffer.WriteString(buffer[len(buffer)-retain:])
			}
			return PlanParseResult{Visible: visible.String()}, nil
		}

		if buffer == "" {
			return PlanParseResult{Visible: visible.String()}, nil
		}
		kind, index, openTag := nextPlanOpenTag(buffer)
		if index >= 0 {
			visible.WriteString(buffer[:index])
			p.buffer.Reset()
			p.buffer.WriteString(buffer[index+len(openTag):])
			p.block = kind
			continue
		}
		if flush {
			visible.WriteString(buffer)
			p.buffer.Reset()
			return PlanParseResult{Visible: visible.String()}, nil
		}
		retain := longestPlanTagPrefixSuffix(buffer, []string{planQuestionsOpenTag, proposedPlanOpenTag})
		if len(buffer) > retain {
			visible.WriteString(buffer[:len(buffer)-retain])
			p.buffer.Reset()
			p.buffer.WriteString(buffer[len(buffer)-retain:])
		}
		return PlanParseResult{Visible: visible.String()}, nil
	}
}

func (p *PlanStreamParser) resetBlock() {
	p.block = ""
	p.blockBuffer.Reset()
}

func nextPlanOpenTag(content string) (PlanBlockKind, int, string) {
	bestKind, bestIndex, bestTag := PlanBlockKind(""), -1, ""
	for _, candidate := range []struct {
		kind PlanBlockKind
		tag  string
	}{
		{PlanBlockQuestions, planQuestionsOpenTag},
		{PlanBlockProposal, proposedPlanOpenTag},
	} {
		index := strings.Index(content, candidate.tag)
		if index >= 0 && (bestIndex < 0 || index < bestIndex) {
			bestKind, bestIndex, bestTag = candidate.kind, index, candidate.tag
		}
	}
	return bestKind, bestIndex, bestTag
}

func planCloseTag(kind PlanBlockKind) string {
	if kind == PlanBlockQuestions {
		return planQuestionsCloseTag
	}
	if kind == PlanBlockProposal {
		return proposedPlanCloseTag
	}
	return ""
}

func longestPlanTagPrefixSuffix(content string, tags []string) int {
	longest := 0
	for _, tag := range tags {
		limit := min(len(content), len(tag)-1)
		for length := limit; length > longest; length-- {
			if strings.HasSuffix(content, tag[:length]) {
				longest = length
				break
			}
		}
	}
	return longest
}

func ParsePlanToolCall(name, args string) (PlanBlock, bool, error) {
	var kind PlanBlockKind
	switch strings.TrimSpace(name) {
	case "plan_questions", "plan_question":
		kind = PlanBlockQuestions
	case "proposed_plan":
		kind = PlanBlockProposal
	default:
		return PlanBlock{}, false, nil
	}

	args = strings.TrimSpace(args)
	var object map[string]any
	if args == "" || json.Unmarshal([]byte(args), &object) != nil || object == nil {
		return PlanBlock{}, true, errors.New("plan tool arguments must be a complete JSON object")
	}
	content := args
	if kind == PlanBlockProposal {
		if _, structured := object["schemaVersion"]; !structured {
			content = extractPlanToolContent(object)
		}
		if content == "" {
			return PlanBlock{}, true, errors.New("proposed plan tool content is empty")
		}
	}
	return PlanBlock{Kind: kind, Content: content}, true, nil
}

func extractPlanToolContent(object map[string]any) string {
	for _, key := range []string{"content", "plan", "markdown", "proposal", "summary"} {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
