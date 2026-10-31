package lore

import (
	"fmt"
	"strings"
)

// Index prose addresses entries by their unique names. Storage identity and
// loading metadata belong to the editor, not to the model's discovery text.
func indexItemMarkdown(item Item, category, detail string) string {
	metadata := fmt.Sprintf("(%s / %s / %s)", category, item.Importance, detail)
	switch detail {
	case IndexDetailFull:
		return fmt.Sprintf("### %s %s\n\n%s", item.Name, metadata, strings.TrimSpace(item.Content))
	case IndexDetailBrief:
		return fmt.Sprintf("- **%s** %s: %s", item.Name, metadata, strings.Join(strings.Fields(item.BriefDescription), " "))
	default:
		return fmt.Sprintf("- %s %s", item.Name, metadata)
	}
}

// loreIndexToolPreamble is the ordinary Agent contract: bodies omitted from
// the index are loaded on demand with the Lore query tool.
const loreIndexToolPreamble = "Entry metadata is category / importance / detail. Importance indicates narrative prominence, not authority or required use. name and brief omit the body; full includes the complete body. Read missing bodies with `query_lore_items` using names and detail=full. Search with `query_lore_items` keywords; `group_names` filters by heading. Explore existing material before inventing new entities."

// loreIndexReadOnlyPreamble describes the same index to a model that has no
// tools, such as a frozen comparison candidate. Omitted bodies are simply
// unavailable, so the text must not point at any loading mechanism.
const loreIndexReadOnlyPreamble = "Entry metadata is category / importance / detail. Importance indicates narrative prominence, not authority or required use. Entries marked full include the complete body. Entries marked name or brief have no body in this read-only snapshot and cannot be loaded; treat their unstated details as unknown and do not invent them."

func renderIndexGuide(collection Collection) (string, error) {
	return renderIndexGuideWithPreamble(collection, loreIndexToolPreamble)
}

func renderIndexGuideWithPreamble(collection Collection, preamble string) (string, error) {
	guide := collection.IndexGuide
	groups := resolveIndexGroups(collection)
	if len(groups) == 0 && strings.TrimSpace(guide.IntroMarkdown) == "" {
		return "", nil
	}
	var sb strings.Builder
	sb.WriteString("# Lore Index\n\n")
	sb.WriteString(preamble)
	sb.WriteString("\n\n")
	if strings.TrimSpace(guide.IntroMarkdown) != "" {
		sb.WriteString(guide.IntroMarkdown)
		sb.WriteString("\n\n")
	}
	categoryNames := make(map[string]string, len(collection.Categories))
	for _, category := range collection.Categories {
		categoryNames[category.ID] = category.DisplayName()
	}
	emitted := map[string]bool{}
	for _, group := range groups {
		fmt.Fprintf(&sb, "## %s\n\n", group.Name)
		if strings.TrimSpace(group.Purpose) != "" {
			sb.WriteString(group.Purpose)
			sb.WriteString("\n\n")
		}
		if strings.TrimSpace(group.BodyMarkdown) != "" {
			sb.WriteString(group.BodyMarkdown)
			sb.WriteString("\n\n")
		}
		for _, item := range group.items {
			if !item.Enabled || (strings.TrimSpace(item.Content) == "" && len(item.Spans) > 0) {
				continue
			}
			if emitted[item.ID] {
				fmt.Fprintf(&sb, "- %s (included above)\n", item.Name)
				continue
			}
			sb.WriteString(indexItemMarkdown(item, categoryNames[item.Type], guide.itemDetail(item)))
			sb.WriteString("\n\n")
			emitted[item.ID] = true
		}
	}
	if sb.Len() > IndexContextMaxBytes {
		return "", fmt.Errorf("%w: limit %d bytes; lower group detail or shorten guide prose (no entries were truncated)", ErrIndexContextTooLarge, IndexContextMaxBytes)
	}
	return strings.TrimSpace(sb.String()), nil
}
