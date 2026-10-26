package lore

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	LoreScopeAsOf = "as_of"
	LoreScopeBook = "book"
)

var chapterPrefixPattern = regexp.MustCompile(`(?i)\bch\d{5}\b`)
var chapterPrefixExactPattern = regexp.MustCompile(`(?i)^ch\d{5}$`)

// SpanVisibility describes the request-scoped lore projection used by writing,
// game turns, and lore tools. ScopeBook is deliberately request-only; it is
// never persisted on an Item or StoryMeta.
type SpanVisibility struct {
	Cursor string
	Scope  string
}

type spanVisibilityContextKey struct{}

// ContextWithSpanVisibility carries one request's lore cursor into tool calls.
// The value is process-local and is never serialized into journals or files.
func ContextWithSpanVisibility(ctx context.Context, visibility SpanVisibility) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	visibility.Cursor = normalizeChapterPrefix(visibility.Cursor)
	visibility.Scope = normalizeLoreScope(visibility.Scope)
	return context.WithValue(ctx, spanVisibilityContextKey{}, visibility)
}

// SpanVisibilityFromContext returns a request's lore projection. The zero value
// means no chapter cursor and the safe as_of scope.
func SpanVisibilityFromContext(ctx context.Context) SpanVisibility {
	if ctx == nil {
		return SpanVisibility{}
	}
	visibility, _ := ctx.Value(spanVisibilityContextKey{}).(SpanVisibility)
	visibility.Cursor = normalizeChapterPrefix(visibility.Cursor)
	visibility.Scope = normalizeLoreScope(visibility.Scope)
	return visibility
}

// ExtractChapterPrefix returns the first ch##### anchor in a filename or path.
// It intentionally does not infer anchors from human chapter titles such as
// "第三章"; those files have no lore cursor.
func ExtractChapterPrefix(value string) string {
	match := chapterPrefixPattern.FindString(value)
	return normalizeChapterPrefix(match)
}

// NormalizeChapterPrefix validates and canonicalizes a stored anchor.
func NormalizeChapterPrefix(value string) (string, bool) {
	normalized := normalizeChapterPrefix(value)
	return normalized, normalized != ""
}

func normalizeChapterPrefix(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !chapterPrefixExactPattern.MatchString(value) {
		return ""
	}
	return value
}

func chapterNumber(prefix string) (int, bool) {
	prefix = normalizeChapterPrefix(prefix)
	if prefix == "" {
		return 0, false
	}
	number, err := strconv.Atoi(prefix[2:])
	return number, err == nil
}

func normalizeLoreScope(scope string) string {
	if strings.EqualFold(strings.TrimSpace(scope), LoreScopeBook) {
		return LoreScopeBook
	}
	return LoreScopeAsOf
}

// validateLoreSpans enforces the persisted Span contract. It is called before
// writing a collection, so invalid updates leave the source file untouched.
func validateLoreSpans(item Item) error {
	seen := make(map[string]bool, len(item.Spans))
	for index, span := range item.Spans {
		id := strings.TrimSpace(span.ID)
		if id == "" {
			return fmt.Errorf("lore item %q span %d id must not be empty", item.ID, index)
		}
		if seen[id] {
			return fmt.Errorf("lore item %q contains duplicate span id %q", item.ID, id)
		}
		seen[id] = true
		from := normalizeChapterPrefix(span.From)
		until := normalizeChapterPrefix(span.Until)
		if strings.TrimSpace(span.From) != "" && from == "" {
			return fmt.Errorf("lore item %q span %q has invalid from chapter prefix", item.ID, id)
		}
		if strings.TrimSpace(span.Until) != "" && until == "" {
			return fmt.Errorf("lore item %q span %q has invalid until chapter prefix", item.ID, id)
		}
		if from == "" && until == "" {
			return fmt.Errorf("lore item %q span %q must define from or until", item.ID, id)
		}
		if from != "" && until != "" {
			fromNumber, _ := chapterNumber(from)
			untilNumber, _ := chapterNumber(until)
			if fromNumber >= untilNumber {
				return fmt.Errorf("lore item %q span %q has an empty or reversed chapter window", item.ID, id)
			}
		}
		if strings.TrimSpace(span.Content) == "" {
			return fmt.Errorf("lore item %q span %q content must not be empty", item.ID, id)
		}
	}
	return nil
}

// VisibleItems projects a collection for one model request. A missing cursor
// exposes only always-valid content; book scope exposes all spans. Items whose
// content and visible spans are both empty are omitted entirely.
func VisibleItems(items []Item, cursor, scope string) []Item {
	scope = normalizeLoreScope(scope)
	cursor = normalizeChapterPrefix(cursor)
	cursorNumber, hasCursor := chapterNumber(cursor)
	visible := make([]Item, 0, len(items))
	for _, item := range items {
		projected := item
		projected.Spans = nil
		if scope == LoreScopeBook {
			for _, span := range item.Spans {
				if (span.From != "" && normalizeChapterPrefix(span.From) == "") || (span.Until != "" && normalizeChapterPrefix(span.Until) == "") {
					continue
				}
				projected.Spans = append(projected.Spans, span)
			}
		} else if hasCursor {
			for _, span := range item.Spans {
				from, hasFrom := chapterNumber(span.From)
				until, hasUntil := chapterNumber(span.Until)
				if span.From != "" && !hasFrom {
					continue
				}
				if span.Until != "" && !hasUntil {
					continue
				}
				if span.From != "" && (!hasCursor || from > cursorNumber) {
					continue
				}
				if hasUntil && until <= cursorNumber {
					continue
				}
				projected.Spans = append(projected.Spans, span)
			}
		}
		if strings.TrimSpace(projected.Content) == "" && len(projected.Spans) == 0 {
			continue
		}
		visible = append(visible, projected)
	}
	return visible
}

func cloneSpans(spans []Span) []Span {
	if spans == nil {
		return nil
	}
	return append([]Span(nil), spans...)
}

const VisibleSpanContextMaxBytes = 256 * 1024

// VisibleSpanContextTitle is the stable fragment title used by every model
// context assembler. The title is kept here so the renderer and validation
// share one byte budget instead of validating a body that is later wrapped and
// truncated by the generic assembler.
const VisibleSpanContextTitle = "Chapter-scoped Lore for This Turn"

const visibleSpanContextTitleBytes = len("# " + VisibleSpanContextTitle + "\n\n")

// VisibleSpansMarkdown renders only chapter-scoped canon for a turn-scoped
// model fragment. It deliberately excludes Item.Content, which belongs to the
// cache-stable Lore Index, and fails rather than truncating a partial span.
func VisibleSpansMarkdown(items []Item, cursor, scope string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = VisibleSpanContextMaxBytes
	}
	if !hasLoreSpans(items) {
		return "", nil
	}
	if normalizeLoreScope(scope) != LoreScopeBook && normalizeChapterPrefix(cursor) == "" {
		return "Chapter-scoped lore is omitted because this request has no chapter cursor. Use the focused ch##### chapter or an explicit request cursor.", nil
	}
	// The shared assembler adds "# <title>\\n\\n" around Content. Reserve
	// those bytes here so a valid body can never be truncated after admission.
	if maxBytes <= visibleSpanContextTitleBytes {
		return "", fmt.Errorf("chapter-scoped lore exceeds %d bytes; shorten spans or choose a narrower chapter", maxBytes)
	}
	visible := VisibleItems(items, cursor, scope)
	var sb strings.Builder
	if normalizeLoreScope(scope) == LoreScopeBook {
		sb.WriteString("Chapter-scoped lore (scope=book; these statements are not proof that the current chapter has reached them):\n\n")
	} else {
		fmt.Fprintf(&sb, "Chapter-scoped lore (scope=as_of; cursor=%s):\n\n", normalizeChapterPrefix(cursor))
	}
	for _, item := range visible {
		if len(item.Spans) == 0 {
			continue
		}
		var block strings.Builder
		fmt.Fprintf(&block, "### %s\n", strings.TrimSpace(item.Name))
		for _, span := range item.Spans {
			if strings.TrimSpace(span.Content) == "" {
				continue
			}
			fmt.Fprintf(&block, "\n#### span=%s", span.ID)
			if span.From != "" {
				fmt.Fprintf(&block, " from=%s", span.From)
			}
			if span.Until != "" {
				fmt.Fprintf(&block, " until=%s", span.Until)
			}
			block.WriteString("\n\n")
			block.WriteString(span.Content)
			block.WriteString("\n")
		}
		if block.Len() == 0 {
			continue
		}
		candidate := strings.TrimSpace(sb.String() + block.String() + "\n")
		if visibleSpanContextTitleBytes+len([]byte(candidate)) > maxBytes {
			return "", fmt.Errorf("chapter-scoped lore exceeds %d bytes; shorten spans or choose a narrower chapter", maxBytes)
		}
		sb.WriteString(block.String())
		sb.WriteByte('\n')
	}
	return strings.TrimSpace(sb.String()), nil
}

func hasLoreSpans(items []Item) bool {
	for _, item := range items {
		if len(item.Spans) > 0 {
			return true
		}
	}
	return false
}
