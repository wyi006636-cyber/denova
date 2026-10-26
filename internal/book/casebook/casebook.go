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

// QuoteError is a stable classification for quote validation failures. The
// application layer maps these codes to localized API errors instead of
// depending on the English wording of this package's diagnostics.
type QuoteError struct {
	Code string
	Err  error
}

const (
	QuoteErrorEmpty     = "empty"
	QuoteErrorMissing   = "missing"
	QuoteErrorAmbiguous = "ambiguous"
	QuoteErrorTooLong   = "too_long"
)

func (e *QuoteError) Error() string {
	if e == nil || e.Err == nil {
		return "quote validation failed"
	}
	return e.Err.Error()
}

func (e *QuoteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// QuoteErrorCode returns the machine-readable quote validation kind.
func QuoteErrorCode(err error) string {
	var typed *QuoteError
	if errors.As(err, &typed) && typed != nil {
		return typed.Code
	}
	return ""
}

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
	"状态":          "status",
	"status":      "status",
	"方向":          "direction",
	"direction":   "direction",
	"适用场景":        "scenario",
	"scenario":    "scenario",
	"use when":    "scenario",
	"方法":          "method",
	"method":      "method",
	"不要搬用":        "avoid",
	"avoid":       "avoid",
	"do not copy": "avoid",
	"来源":          "source",
	"source":      "source",
	"定位":          "location",
	"location":    "location",
	"原文摘录":        "quote",
	"quote":       "quote",
	"excerpt":     "quote",
	"原文前文":        "prefix",
	"prefix":      "prefix",
	"原文后文":        "suffix",
	"suffix":      "suffix",
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
			if isCollectedStatus(value) {
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
	if !ok {
		canonical, ok = fieldAliases[strings.ToLower(key)]
	}
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
			case string(StatusCollected), "Collected":
				section = StatusCollected
			case string(StatusPending), "Pending":
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
		return QuoteLocation{}, &QuoteError{Code: QuoteErrorEmpty, Err: errors.New("quote is empty")}
	}
	if utf8.RuneCountInString(quote) > MaxQuoteRunes {
		return QuoteLocation{}, &QuoteError{Code: QuoteErrorTooLong, Err: fmt.Errorf("quote exceeds %d Unicode characters", MaxQuoteRunes)}
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
				return QuoteLocation{}, &QuoteError{Code: QuoteErrorAmbiguous, Err: errors.New("quote matches multiple passages")}
			}
			match = start
		}
		offset = start + 1
	}
	if match < 0 {
		return QuoteLocation{}, &QuoteError{Code: QuoteErrorMissing, Err: errors.New("quote was not found")}
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
	newline := detectNewline(raw)
	lines := splitMarkdownLines(raw)
	foundStatus := false
	foundLocation := false
	locationEnglish := false
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
			value := strings.TrimSpace(parseFieldValue(line))
			statusValue := string(status)
			if isEnglishStatusValue(value) || (strings.EqualFold(strings.TrimSpace(label), "- status") || strings.EqualFold(strings.TrimSpace(label), "status")) {
				statusValue = englishStatus(status)
			}
			valuePrefix := line[separator+separatorWidth:]
			valueIndent := valuePrefix[:len(valuePrefix)-len(strings.TrimLeft(valuePrefix, " \t"))]
			lines[index] = indent + label + line[separator:separator+separatorWidth] + valueIndent + statusValue
			foundStatus = true
		case "location":
			locationSeparator := strings.IndexRune(line, ':')
			if locationSeparator < 0 {
				locationSeparator = strings.IndexRune(line, '：')
			}
			locationLabel := strings.TrimSpace(line[:locationSeparator])
			locationEnglish = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(locationLabel, "-")), "location")
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
				locationValue := "第 " + strconv.Itoa(location) + " 行"
				if locationEnglish || isEnglishLocationValue(parseFieldValue(line)) {
					locationValue = "line " + strconv.Itoa(location)
				}
				valuePrefix := line[separator+separatorWidth:]
				valueIndent := valuePrefix[:len(valuePrefix)-len(strings.TrimLeft(valuePrefix, " \t"))]
				lines[index] = indent + label + line[separator:separator+separatorWidth] + valueIndent + locationValue
				foundLocation = true
			}
		}
	}
	if !foundStatus {
		return "", errors.New("case markdown is missing a status field")
	}
	if location > 0 && !foundLocation {
		insertAt := len(lines)
		if insertAt > 0 && lines[insertAt-1] == "" {
			insertAt--
		}
		if prefersEnglishFields(raw) {
			lines = append(lines[:insertAt], append([]string{"- Location: line " + strconv.Itoa(location)}, lines[insertAt:]...)...)
		} else {
			lines = append(lines[:insertAt], append([]string{"- 定位：第 " + strconv.Itoa(location) + " 行"}, lines[insertAt:]...)...)
		}
	}
	return strings.Join(lines, newline), nil
}

// PatchIndexStatus moves one compact line between the two status sections.
// If an index has no matching line, the operation fails closed instead of
// silently creating an entry whose method may not match the case file.
func PatchIndexStatus(raw, id string, status Status) (string, error) {
	return patchIndexStatus(raw, id, status, "", false)
}

// EnsureIndexEntry moves an existing index row or adds an orphan case to the
// appropriate status section before moving it. It is used when an author
// collects a valid case file that was created before the index row existed.
func EnsureIndexEntry(raw, id, direction, method string, status Status) (string, error) {
	return patchIndexStatus(raw, id, status, fmt.Sprintf("- %s | %s | %s", strings.TrimSpace(id), strings.TrimSpace(direction), strings.TrimSpace(method)), true)
}

// IndexHasEntry reports whether an index contains a row for id.
func IndexHasEntry(raw, id string) bool {
	return indexLine(raw, id) >= 0
}

func patchIndexStatus(raw, id string, status Status, newLine string, allowMissing bool) (string, error) {
	if status != StatusPending && status != StatusCollected {
		return "", errors.New("case status is invalid")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("case id is empty")
	}
	newline := detectNewline(raw)
	lines := splitMarkdownLines(raw)
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
		if !allowMissing {
			return "", fmt.Errorf("case %q is missing from the case index", id)
		}
		lineText = newLine
	}
	if lineIndex >= 0 {
		lines = append(lines[:lineIndex], lines[lineIndex+1:]...)
	}
	insertAt := statusSectionInsertAt(lines, status)
	if insertAt < 0 {
		section := "## " + preferredStatusHeading(raw, status)
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, section, "", lineText)
	} else {
		lines = append(lines[:insertAt], append([]string{lineText}, lines[insertAt:]...)...)
	}
	return strings.Join(lines, newline), nil
}

func normalizeWorkspacePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	return strings.TrimPrefix(value, "./")
}

func isCollectedStatus(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == string(StatusCollected) || strings.EqualFold(trimmed, "collected")
}

func isEnglishStatusValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.EqualFold(trimmed, "pending") || strings.EqualFold(trimmed, "collected")
}

func englishStatus(status Status) string {
	if status == StatusCollected {
		return "collected"
	}
	return "pending"
}

func isEnglishLocationValue(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "line ")
}

func prefersEnglishFields(raw string) bool {
	for _, line := range splitMarkdownLines(raw) {
		key, _, ok := parseFieldLine(line)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if strings.HasPrefix(strings.ToLower(trimmed), key+":") || strings.HasPrefix(strings.ToLower(trimmed), "status:") || strings.HasPrefix(strings.ToLower(trimmed), "location:") {
			return true
		}
	}
	return false
}

func parseFieldValue(line string) string {
	_, value, ok := parseFieldLine(line)
	if !ok {
		return ""
	}
	return value
}

func splitMarkdownLines(raw string) []string {
	return strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
}

func detectNewline(raw string) string {
	if strings.Contains(raw, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func indexLine(raw, id string) int {
	for index, line := range splitMarkdownLines(raw) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")), "|", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == strings.TrimSpace(id) {
			return index
		}
	}
	return -1
}

func statusSectionInsertAt(lines []string, status Status) int {
	for index, line := range lines {
		if headingStatus(line) != status {
			continue
		}
		insertAt := index + 1
		for insertAt < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[insertAt]), "## ") {
			insertAt++
		}
		return insertAt
	}
	return -1
}

func headingStatus(line string) Status {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "##") {
		return ""
	}
	switch strings.TrimSpace(strings.TrimPrefix(trimmed, "##")) {
	case string(StatusCollected), "Collected":
		return StatusCollected
	case string(StatusPending), "Pending":
		return StatusPending
	default:
		return ""
	}
}

func preferredStatusHeading(raw string, status Status) string {
	for _, line := range splitMarkdownLines(raw) {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "##"))
		if strings.EqualFold(trimmed, "pending") || strings.EqualFold(trimmed, "collected") {
			if status == StatusCollected {
				return "Collected"
			}
			return "Pending"
		}
	}
	return string(status)
}
