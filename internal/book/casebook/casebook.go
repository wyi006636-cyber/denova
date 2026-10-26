// Package casebook owns the small, file-backed writing case format used by
// the book workspace. Cases are deliberately plain Markdown so authors and
// Agents can inspect and edit them with the existing file tools.
package casebook

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxQuoteRunes = 120
	CaseIndexPath = "setting/case-index.md"
	CaseDirectory = "setting/cases/"
)

type Status string

const (
	StatusPending   Status = "待确认"
	StatusCollected Status = "已收藏"
)

// Case is the author-facing shape of one setting/cases/*.md document.
// Unknown Markdown is intentionally ignored by the parser so authors can add
// notes without making the file unreadable.
type Case struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    Status `json:"status"`
	Direction string `json:"direction"`
	Scenario  string `json:"scenario"`
	Method    string `json:"method"`
	Avoid     string `json:"avoid"`
	Source    string `json:"source"`
	Location  string `json:"location"`
	Quote     string `json:"quote"`
	Prefix    string `json:"prefix,omitempty"`
	Suffix    string `json:"suffix,omitempty"`
	Path      string `json:"path,omitempty"`
	Revision  string `json:"revision,omitempty"`
	Raw       string `json:"-"`
}

// IndexEntry is one compact line from setting/case-index.md.
type IndexEntry struct {
	ID        string `json:"id"`
	Direction string `json:"direction"`
	Method    string `json:"method"`
	Status    Status `json:"status"`
	RawLine   string `json:"-"`
}

type QuoteLocation struct {
	Line      int `json:"line"`
	StartByte int `json:"start_byte"`
	EndByte   int `json:"end_byte"`
}

var caseHeadingPattern = regexp.MustCompile(`^\s*#\s+([^\s]+)(?:\s+(.*?))?\s*$`)

var fieldAliases = map[string]string{
	"状态":        "status",
	"status":    "status",
	"方向":        "direction",
	"direction": "direction",
	"适用场景":      "scenario",
	"scenario":  "scenario",
	"方法":        "method",
	"method":    "method",
	"不要搬用":      "avoid",
	"avoid":     "avoid",
	"来源":        "source",
	"source":    "source",
	"定位":        "location",
	"location":  "location",
	"原文摘录":      "quote",
	"quote":     "quote",
	"原文前文":      "prefix",
	"prefix":    "prefix",
	"原文后文":      "suffix",
	"suffix":    "suffix",
}

// ParseCaseMarkdown parses the intentionally small key/value Markdown case
// format. Status is pending unless the exact collected value is present.
func ParseCaseMarkdown(raw string) (Case, error) {
	if !utf8.ValidString(raw) {
		return Case{}, errors.New("case markdown is not valid UTF-8")
	}
	result := Case{Status: StatusPending, Raw: raw}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, line := range lines {
		if result.ID == "" {
			if match := caseHeadingPattern.FindStringSubmatch(line); match != nil {
				result.ID = strings.TrimSpace(match[1])
				result.Title = strings.TrimSpace(match[2])
				continue
			}
		}
		key, value, ok := parseFieldLine(line)
		if !ok {
			continue
		}
		switch key {
		case "status":
			if strings.TrimSpace(value) == string(StatusCollected) {
				result.Status = StatusCollected
			} else {
				result.Status = StatusPending
			}
		case "direction":
			result.Direction = value
		case "scenario":
			result.Scenario = value
		case "method":
			result.Method = value
		case "avoid":
			result.Avoid = value
		case "source":
			result.Source = normalizeWorkspacePath(value)
		case "location":
			result.Location = value
		case "quote":
			result.Quote = strings.TrimSpace(value)
		case "prefix":
			result.Prefix = value
		case "suffix":
			result.Suffix = value
		}
	}
	if result.ID == "" {
		return Case{}, errors.New("case markdown is missing an id heading")
	}
	return result, nil
}

func parseFieldLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "-") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
	}
	separator := strings.IndexRune(line, ':')
	separatorWidth := 1
	if separator < 0 {
		separator = strings.IndexRune(line, '：')
		separatorWidth = len("：")
	}
	if separator < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:separator])
	value := strings.TrimSpace(line[separator+separatorWidth:])
	canonical, ok := fieldAliases[key]
	return canonical, value, ok
}

// ParseIndexMarkdown returns compact case entries and preserves only the
// status section and the supported pipe-delimited line shape.
func ParseIndexMarkdown(raw string) []IndexEntry {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	section := StatusPending
	entries := make([]IndexEntry, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "##") {
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))
			switch title {
			case string(StatusCollected):
				section = StatusCollected
			case string(StatusPending):
				section = StatusPending
			default:
				section = StatusPending
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")), "|", 3)
		if len(parts) != 3 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		if id == "" {
			continue
		}
		entries = append(entries, IndexEntry{ID: id, Direction: strings.TrimSpace(parts[1]), Method: strings.TrimSpace(parts[2]), Status: section, RawLine: line})
	}
	return entries
}

// MethodOnly produces the only case material that should be placed in an
// Agent draft. It intentionally omits source, location, and the original
// quote. Pending cases are never usable methods.
func MethodOnly(input Case) string {
	if input.Status != StatusCollected {
		return ""
	}
	parts := make([]string, 0, 4)
	for _, part := range []string{
		formatMethodField("方向", input.Direction),
		formatMethodField("适用场景", input.Scenario),
		formatMethodField("方法", input.Method),
		formatMethodField("不要搬用", input.Avoid),
	} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n")
}

func formatMethodField(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return fmt.Sprintf("%s：%s", label, value)
}

// QuoteLocated finds one exact quote and reports its 1-based source line.
// Duplicate matches fail closed; callers needing a disambiguator can supply a
// prefix or suffix through QuoteLocatedWithContext.
func QuoteLocated(source, quote string) (QuoteLocation, error) {
	return QuoteLocatedWithContext(source, quote, "", "")
}

func QuoteLocatedWithContext(source, quote, prefix, suffix string) (QuoteLocation, error) {
	if !utf8.ValidString(source) || !utf8.ValidString(quote) || !utf8.ValidString(prefix) || !utf8.ValidString(suffix) {
		return QuoteLocation{}, errors.New("source and quote must be valid UTF-8")
	}
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return QuoteLocation{}, errors.New("quote is empty")
	}
	if utf8.RuneCountInString(quote) > MaxQuoteRunes {
		return QuoteLocation{}, fmt.Errorf("quote exceeds %d Unicode characters", MaxQuoteRunes)
	}
	prefix = strings.TrimSpace(prefix)
	suffix = strings.TrimSpace(suffix)
	match := -1
	for offset := 0; offset <= len(source); {
		index := strings.Index(source[offset:], quote)
		if index < 0 {
			break
		}
		start := offset + index
		end := start + len(quote)
		if contextMatches(source, start, end, prefix, suffix) {
			if match >= 0 {
				return QuoteLocation{}, errors.New("quote matches multiple passages")
			}
			match = start
		}
		offset = start + 1
	}
	if match < 0 {
		return QuoteLocation{}, errors.New("quote was not found")
	}
	line := 1 + strings.Count(source[:match], "\n")
	return QuoteLocation{Line: line, StartByte: match, EndByte: match + len(quote)}, nil
}

func contextMatches(source string, start, end int, prefix, suffix string) bool {
	return (prefix == "" || (start >= len(prefix) && source[start-len(prefix):start] == prefix)) &&
		(suffix == "" || (end+len(suffix) <= len(source) && source[end:end+len(suffix)] == suffix))
}

// PatchCaseStatus replaces only the status line and keeps author notes and
// unknown fields byte-for-byte unchanged.
func PatchCaseStatus(raw string, status Status, location int) (string, error) {
	if status != StatusPending && status != StatusCollected {
		return "", errors.New("case status is invalid")
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	foundStatus := false
	foundLocation := false
	for index, line := range lines {
		key, _, ok := parseFieldLine(line)
		if !ok {
			continue
		}
		switch key {
		case "status":
			separator := strings.IndexRune(line, ':')
			separatorWidth := 1
			if separator < 0 {
				separator = strings.IndexRune(line, '：')
				separatorWidth = len("：")
			}
			label := strings.TrimSpace(line[:separator])
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			if strings.HasPrefix(strings.TrimLeft(line, " \t"), "-") {
				label = "- " + strings.TrimSpace(strings.TrimPrefix(label, "-"))
			}
			lines[index] = indent + label + line[separator:separator+separatorWidth] + string(status)
			foundStatus = true
		case "location":
			if location > 0 {
				separator := strings.IndexRune(line, ':')
				separatorWidth := 1
				if separator < 0 {
					separator = strings.IndexRune(line, '：')
					separatorWidth = len("：")
				}
				label := strings.TrimSpace(line[:separator])
				indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				if strings.HasPrefix(strings.TrimLeft(line, " \t"), "-") {
					label = "- " + strings.TrimSpace(strings.TrimPrefix(label, "-"))
				}
				lines[index] = indent + label + line[separator:separator+separatorWidth] + "第 " + strconv.Itoa(location) + " 行"
				foundLocation = true
			}
		}
	}
	if !foundStatus {
		return "", errors.New("case markdown is missing a status field")
	}
	if location > 0 && !foundLocation {
		return "", errors.New("case markdown is missing a location field")
	}
	return strings.Join(lines, "\n"), nil
}

// PatchIndexStatus moves one compact line between the two status sections.
// If an index has no matching line, the operation fails closed instead of
// silently creating an entry whose method may not match the case file.
func PatchIndexStatus(raw, id string, status Status) (string, error) {
	if status != StatusPending && status != StatusCollected {
		return "", errors.New("case status is invalid")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("case id is empty")
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	lineIndex := -1
	lineText := ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")), "|", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == id {
			lineIndex = index
			lineText = line
			break
		}
	}
	if lineIndex < 0 {
		return "", fmt.Errorf("case %q is missing from the case index", id)
	}
	lines = append(lines[:lineIndex], lines[lineIndex+1:]...)
	if !strings.HasSuffix(strings.Join(lines, "\n"), "\n") {
		// Keep insertion logic independent of whether the input had a final LF.
	}
	section := "## " + string(status)
	insertAt := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == section {
			insertAt = index + 1
			for insertAt < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[insertAt]), "## ") {
				insertAt++
			}
			break
		}
	}
	if insertAt < 0 {
		lines = append(lines, "", section, lineText)
	} else {
		lines = append(lines[:insertAt], append([]string{lineText}, lines[insertAt:]...)...)
	}
	return strings.Join(lines, "\n"), nil
}

func normalizeWorkspacePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	return strings.TrimPrefix(value, "./")
}
