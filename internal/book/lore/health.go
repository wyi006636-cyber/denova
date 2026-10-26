package lore

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	DefaultHealthChapterLimit = 5000
	DefaultHealthIssueLimit   = 500
	maxHealthChapterBytes     = 4 * 1024 * 1024
)

type HealthIssue struct {
	Kind      string `json:"kind"`
	ItemID    string `json:"item_id,omitempty"`
	Name      string `json:"name,omitempty"`
	OtherName string `json:"other_name,omitempty"`
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line,omitempty"`
	Reference string `json:"reference,omitempty"`
	// DetailKey is a stable UI lookup key. Clients localize it instead of
	// rendering the diagnostic prose produced by the scanner.
	DetailKey string `json:"detail_key,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type HealthReport struct {
	DuplicateNames   []HealthIssue `json:"duplicate_names"`
	EmptyItems       []HealthIssue `json:"empty_items"`
	BrokenReferences []HealthIssue `json:"broken_references"`
	UnusedItems      []HealthIssue `json:"unused_items"`
	ValidityRanges   []HealthIssue `json:"validity_ranges"`
	ChaptersScanned  int           `json:"chapters_scanned"`
	ChaptersTotal    int           `json:"chapters_total"`
	IssuesTotal      int           `json:"issues_total"`
	ResultsTruncated bool          `json:"results_truncated"`
	IssueLimit       int           `json:"issue_limit"`
	ChapterLimit     int           `json:"chapter_limit"`
}

var healthReferencePattern = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// Health performs a read-only bounded scan of lore identities and chapter references.
// Keywords serve as aliases because the released lore schema has no separate alias field.
func (s *Store) Health(chapterLimit, issueLimit int) (HealthReport, error) {
	if chapterLimit <= 0 {
		chapterLimit = DefaultHealthChapterLimit
	}
	if issueLimit <= 0 {
		issueLimit = DefaultHealthIssueLimit
	}
	report := HealthReport{IssueLimit: issueLimit, ChapterLimit: chapterLimit}
	items, err := s.ListAll()
	if err != nil {
		return report, err
	}
	nameOwners := make(map[string]Item)
	for _, item := range items {
		terms := append([]string{item.Name}, item.Keywords...)
		for _, term := range terms {
			key := healthTermKey(term)
			if key == "" {
				continue
			}
			if owner, exists := nameOwners[key]; exists && owner.ID != item.ID {
				appendHealthIssue(&report.DuplicateNames, HealthIssue{
					Kind: "duplicate_name_or_alias", ItemID: item.ID, Name: item.Name, Reference: strings.TrimSpace(term),
					OtherName: owner.Name, DetailKey: "lore.health.detail.duplicate",
				}, issueLimit, &report.IssuesTotal, &report.ResultsTruncated)
				continue
			}
			nameOwners[key] = item
		}
		if loreBodyEmpty(item.Content) {
			appendHealthIssue(&report.EmptyItems, HealthIssue{Kind: "empty_content", ItemID: item.ID, Name: item.Name, DetailKey: "lore.health.detail.empty"}, issueLimit, &report.IssuesTotal, &report.ResultsTruncated)
		}
	}

	chapterPaths := make([]string, 0)
	root := filepath.Join(s.workspace, "chapters")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry == nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".md" || ext == ".txt" {
			chapterPaths = append(chapterPaths, path)
		}
		return nil
	})
	sort.Strings(chapterPaths)
	chapterMatches := make(map[string][]string)
	for _, path := range chapterPaths {
		if prefix := ExtractChapterPrefix(filepath.Base(path)); prefix != "" {
			rel, relErr := filepath.Rel(s.workspace, path)
			if relErr == nil {
				chapterMatches[prefix] = append(chapterMatches[prefix], filepath.ToSlash(rel))
			}
		}
	}
	for _, item := range items {
		for _, span := range item.Spans {
			spanID := strings.TrimSpace(span.ID)
			validateRangeBoundary := func(boundary, field string) {
				boundary = strings.TrimSpace(boundary)
				if boundary == "" {
					return
				}
				prefix, ok := NormalizeChapterPrefix(boundary)
				if !ok {
					appendHealthIssue(&report.ValidityRanges, HealthIssue{Kind: "invalid_lore_range_prefix", ItemID: item.ID, Name: item.Name, Reference: spanID, DetailKey: "lore.health.detail.range.invalid_prefix", Detail: field}, issueLimit, &report.IssuesTotal, &report.ResultsTruncated)
					return
				}
				matches := chapterMatches[prefix]
				switch len(matches) {
				case 0:
					appendHealthIssue(&report.ValidityRanges, HealthIssue{Kind: "unmatched_lore_range_prefix", ItemID: item.ID, Name: item.Name, Reference: prefix, DetailKey: "lore.health.detail.range.unmatched", Detail: field}, issueLimit, &report.IssuesTotal, &report.ResultsTruncated)
				case 1:
					// A unique match is healthy; retain no diagnostic record.
				default:
					appendHealthIssue(&report.ValidityRanges, HealthIssue{Kind: "ambiguous_lore_range_prefix", ItemID: item.ID, Name: item.Name, Reference: prefix, Path: strings.Join(matches, ", "), DetailKey: "lore.health.detail.range.ambiguous", Detail: field}, issueLimit, &report.IssuesTotal, &report.ResultsTruncated)
				}
			}
			validateRangeBoundary(span.From, "from")
			validateRangeBoundary(span.Until, "until")
			if from, fromOK := chapterNumber(span.From); fromOK {
				if until, untilOK := chapterNumber(span.Until); untilOK && from >= until {
					appendHealthIssue(&report.ValidityRanges, HealthIssue{Kind: "reversed_lore_range", ItemID: item.ID, Name: item.Name, Reference: spanID, DetailKey: "lore.health.detail.range.reversed"}, issueLimit, &report.IssuesTotal, &report.ResultsTruncated)
				}
			}
		}
	}
	referenced := make(map[string]bool)
	for index, path := range chapterPaths {
		report.ChaptersTotal++
		if index >= chapterLimit {
			report.ResultsTruncated = true
			continue
		}
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() > maxHealthChapterBytes {
			report.ResultsTruncated = true
			continue
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		report.ChaptersScanned++
		rel, relErr := filepath.Rel(s.workspace, path)
		if relErr != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		lineStarts := lineOffsets(data)
		for _, match := range healthReferencePattern.FindAllSubmatchIndex(data, -1) {
			if len(match) < 4 {
				continue
			}
			name := strings.TrimSpace(string(data[match[2]:match[3]]))
			key := healthTermKey(name)
			owner, exists := nameOwners[key]
			if exists {
				referenced[owner.ID] = true
				continue
			}
			line := sort.SearchInts(lineStarts, match[0])
			if line >= len(lineStarts) || lineStarts[line] > match[0] {
				line--
			}
			appendHealthIssue(&report.BrokenReferences, HealthIssue{Kind: "broken_reference", Path: rel, Line: line + 1, Reference: name, DetailKey: "lore.health.detail.broken"}, issueLimit, &report.IssuesTotal, &report.ResultsTruncated)
		}
	}
	for _, item := range items {
		if !referenced[item.ID] {
			appendHealthIssue(&report.UnusedItems, HealthIssue{Kind: "unused_item", ItemID: item.ID, Name: item.Name, DetailKey: "lore.health.detail.unused"}, issueLimit, &report.IssuesTotal, &report.ResultsTruncated)
		}
	}
	return report, nil
}

func appendHealthIssue(target *[]HealthIssue, issue HealthIssue, limit int, total *int, truncated *bool) {
	(*total)++
	if *total > limit {
		*truncated = true
		return
	}
	*target = append(*target, issue)
}

func healthTermKey(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func loreBodyEmpty(content string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return false
	}
	return true
}

func lineOffsets(data []byte) []int {
	starts := []int{0}
	for index, value := range data {
		if value == '\n' {
			starts = append(starts, index+1)
		}
	}
	return starts
}
