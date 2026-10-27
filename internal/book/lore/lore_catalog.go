package lore

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// NameCatalogOptions controls the compact, model-visible name catalog.
// The catalog is derived from LoreItemsRelativePath and is never persisted separately.
type NameCatalogOptions struct {
	Offset          int
	MaxBytes        int
	ExcludeResident bool
	OmitWhenEmpty   bool
	Cursor          string
	Scope           string
}

// NameCatalogMarkdown returns a bounded catalog for candidate discovery.
// Entries are added as complete lines so the result is never cut mid-entry.
func (s *Store) NameCatalogMarkdown(options NameCatalogOptions) (string, error) {
	items, err := s.List()
	if err != nil {
		return "", err
	}
	categories, err := s.Categories()
	if err != nil {
		return "", err
	}
	var categoryCatalog strings.Builder
	categoryCatalog.WriteString("Category catalog (project reference data; use exact IDs):\n")
	for _, category := range categories {
		fmt.Fprintf(&categoryCatalog, "- %s: %s\n", category.ID, category.DisplayName())
	}
	categoryCatalog.WriteByte('\n')
	prefix := categoryCatalog.String()
	visibleItems := VisibleItems(items, options.Cursor, options.Scope)
	visibleIDs := make(map[string]struct{}, len(visibleItems))
	for _, item := range visibleItems {
		visibleIDs[item.ID] = struct{}{}
	}
	hiddenIDs := make([]string, 0)
	if !strings.EqualFold(strings.TrimSpace(options.Scope), LoreScopeBook) {
		for _, item := range items {
			if options.ExcludeResident && item.LoadMode == LoadModeResident {
				continue
			}
			if len(item.Spans) == 0 {
				continue
			}
			if _, ok := visibleIDs[item.ID]; !ok {
				hiddenIDs = append(hiddenIDs, item.ID)
			}
		}
		sort.Strings(hiddenIDs)
	}
	filtered := make([]Item, 0, len(visibleItems))
	for _, item := range visibleItems {
		if options.ExcludeResident && item.LoadMode == LoadModeResident {
			continue
		}
		filtered = append(filtered, item)
	}
	if options.OmitWhenEmpty && len(filtered) == 0 && len(hiddenIDs) == 0 {
		return "", nil
	}
	entries := make([]loreIndexEntry, 0, len(filtered))
	for _, item := range filtered {
		entries = append(entries, loreIndexEntry{Item: item})
	}
	sortLoreIndexEntries(entries, false)

	maxBytes := options.MaxBytes
	if maxBytes <= 0 || maxBytes > IndexDefaultMaxBytes {
		maxBytes = IndexDefaultMaxBytes
	}
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(entries) {
		offset = len(entries)
	}
	revision, err := s.Revision()
	if err != nil {
		return "", err
	}

	lines := make([]string, 0, len(entries)-offset)
	for _, entry := range entries[offset:] {
		item := entry.Item
		name := boundedLoreCatalogName(item.Name, 512)
		lines = append(lines, fmt.Sprintf("- [%s/%s] %s\n", item.Type, item.Importance, name))
	}
	hiddenNotice := ""
	if len(hiddenIDs) > 0 {
		const hiddenIDLimit = 30
		shownHiddenIDs := hiddenIDs
		if len(shownHiddenIDs) > hiddenIDLimit {
			shownHiddenIDs = shownHiddenIDs[:hiddenIDLimit]
		}
		notice := "not visible at the current chapter cursor"
		if strings.TrimSpace(options.Cursor) == "" {
			notice = "not visible because there is no chapter cursor for this request"
		}
		extra := ""
		if omitted := len(hiddenIDs) - len(shownHiddenIDs); omitted > 0 {
			extra = fmt.Sprintf(", +%d more", omitted)
		}
		hiddenNotice = fmt.Sprintf("\n%d lore item(s) are %s; query or update by exact id: %s%s\n", len(hiddenIDs), notice, strings.Join(shownHiddenIDs, ", "), extra)
	}
	hiddenNoticeBytes := len([]byte(hiddenNotice))

	shown := 0
	shownLineBytes := 0
	for shown < len(lines) {
		candidateShown := shown + 1
		candidateBytes := len(prefix) + len([]byte(renderLoreNameCatalogHeader(revision, len(entries), offset, candidateShown))) + shownLineBytes + len([]byte(lines[shown])) + hiddenNoticeBytes
		if candidateBytes > maxBytes {
			break
		}
		shownLineBytes += len([]byte(lines[shown]))
		shown++
	}
	result := prefix + renderLoreNameCatalog(revision, len(entries), offset, shown, lines[:shown]) + hiddenNotice
	if len([]byte(result)) <= maxBytes {
		return strings.TrimSpace(result), nil
	}

	// Very small caller-provided budgets still receive useful source metadata.
	// The hidden-id list is only a lookup hint, so fall back to a count-only
	// notice before refusing the whole catalog.
	notices := []string{hiddenNotice}
	if len(hiddenIDs) > 0 {
		notices = append(notices, fmt.Sprintf("\n%d lore item(s) are hidden for this request.", len(hiddenIDs)))
	}
	var minimal string
	for index, notice := range notices {
		candidate := fmt.Sprintf("# Lore Name Catalog\nsource: %s\nrevision: %s\ntotal: %d\nshown: 0\nomitted: %d%s", ItemsRelativePath, revision, len(entries), len(entries)-offset, notice)
		if index == 0 {
			minimal = candidate
		}
		if len([]byte(candidate)) <= maxBytes {
			return candidate, nil
		}
		compact := fmt.Sprintf("# Lore Name Catalog\nTotal: %d; omitted: %d. Use query_lore_items.%s", len(entries), len(entries)-offset, notice)
		if len([]byte(compact)) <= maxBytes {
			return compact, nil
		}
	}
	return "", fmt.Errorf("lore name catalog limit is too small; at least %d bytes are required", len([]byte(minimal)))
}

func renderLoreNameCatalog(revision string, total, offset, shown int, lines []string) string {
	header := renderLoreNameCatalogHeader(revision, total, offset, shown)
	var sb strings.Builder
	sb.Grow(len(header) + len(lines)*32)
	sb.WriteString(header)
	for _, line := range lines {
		sb.WriteString(line)
	}
	return sb.String()
}

func renderLoreNameCatalogHeader(revision string, total, offset, shown int) string {
	omitted := total - offset - shown
	if omitted < 0 {
		omitted = 0
	}
	nextOffset := "null"
	if omitted > 0 {
		nextOffset = fmt.Sprintf("%d", offset+shown)
	}
	var sb strings.Builder
	sb.WriteString("# Lore Name Catalog\n")
	fmt.Fprintf(&sb, "source: %s\n", ItemsRelativePath)
	fmt.Fprintf(&sb, "revision: %s\n", revision)
	fmt.Fprintf(&sb, "total: %d\n", total)
	fmt.Fprintf(&sb, "shown: %d\n", shown)
	fmt.Fprintf(&sb, "omitted: %d\n", omitted)
	fmt.Fprintf(&sb, "next_offset: %s\n\n", nextOffset)
	fmt.Fprintf(&sb, "There are %d enabled lore items. This catalog is for candidate discovery only; read a complete body directly by unique name or search with filters.\n\n", total)
	return sb.String()
}

func boundedLoreCatalogName(value string, maxBytes int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "(unnamed)"
	}
	if maxBytes <= 0 || len([]byte(value)) <= maxBytes {
		return value
	}
	const suffix = "…"
	limit := maxBytes - len([]byte(suffix))
	if limit <= 0 {
		return suffix
	}
	cut := 0
	for index := range value {
		if index > limit {
			break
		}
		cut = index
	}
	if cut == 0 && utf8.RuneCountInString(value) > 0 {
		_, size := utf8.DecodeRuneInString(value)
		if size <= limit {
			cut = size
		}
	}
	return strings.TrimSpace(value[:cut]) + suffix
}
