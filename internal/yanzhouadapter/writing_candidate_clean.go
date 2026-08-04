package yanzhouadapter

import (
	"regexp"
	"strings"
)

// Candidate models occasionally wrap the prose they were asked to produce
// directly inside DSML/XML tool-call shells (for example an invoke block
// named novel_diff with a diffContent parameter). The runtime must never
// surface that markup as candidate prose, so we unwrap the payload when it
// is clearly a single tool-call shell and reject the stage when no prose
// can be recovered.

var (
	writingInvokeOpenRe  = regexp.MustCompile(`(?s)<invoke\s+name="([^"]+)"\s*>`)
	writingInvokeCloseRe = regexp.MustCompile(`(?s)</invoke\s*>`)
	writingToolCallWrap  = regexp.MustCompile(`(?s)<tool_calls>.*</tool_calls>`)
	writingParamNameRe   = regexp.MustCompile(`(?s)<parameter\s+name="([^"]+)"[^>]*>`)
	writingParamCloseRe  = regexp.MustCompile(`(?s)</parameter\s*>`)
	writingAnyTagRe      = regexp.MustCompile(`(?s)<\s*/?[a-zA-Z][^>]*>`)
)

// unwrapWritingMarkup extracts the visible payload from a DSML/XML tool-call
// shell. It returns the cleaned text and whether a meaningful payload was
// found. Plain prose without any tool-call shell is returned unchanged.
func unwrapWritingMarkup(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", false
	}
	open := writingInvokeOpenRe.FindStringSubmatchIndex(trimmed)
	close := writingInvokeCloseRe.FindStringSubmatchIndex(trimmed)
	if open == nil || close == nil || close[0] <= open[1] {
		// No shell: if the text is still polluted with stray tags treat it as
		// unusable only when it contains no prose outside the tags.
		stripped := writingAnyTagRe.ReplaceAllString(trimmed, "")
		if strings.TrimSpace(stripped) == "" && writingAnyTagRe.MatchString(trimmed) {
			return "", false
		}
		return trimmed, true
	}
	inner := trimmed[open[1]:close[0]]
	// Prefer a parameter literally named diffContent or content.
	names := writingParamNameRe.FindAllStringSubmatchIndex(inner, -1)
	closes := writingParamCloseRe.FindAllStringSubmatchIndex(inner, -1)
	for i, n := range names {
		name := strings.ToLower(strings.TrimSpace(inner[n[2]:n[3]]))
		if name != "diffcontent" && name != "content" {
			continue
		}
		if i >= len(closes) {
			continue
		}
		payload := strings.TrimSpace(inner[n[1]:closes[i][0]])
		if payload == "" {
			continue
		}
		return payload, true
	}
	// Fall back to the whole inner body with tags removed.
	stripped := writingAnyTagRe.ReplaceAllString(inner, "")
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		return "", false
	}
	return stripped, true
}

// cleanWritingCandidateContent is the public entry used by the harness
// before an artifact is recorded. It returns the content to record and
// whether the stage output is usable.
func cleanWritingCandidateContent(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", false
	}
	if !writingToolCallWrap.MatchString(trimmed) && !writingInvokeOpenRe.MatchString(trimmed) {
		return trimmed, true
	}
	return unwrapWritingMarkup(trimmed)
}
