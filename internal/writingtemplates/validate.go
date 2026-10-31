package writingtemplates

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	categoryIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
	templateIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)
)

var allowedTasks = map[string]struct{}{
	TaskChapterDraft:      {},
	TaskChapterOpening:    {},
	TaskLocalRewrite:      {},
	TaskConsistencyReview: {},
	TaskComposition:       {},
	TaskPlanning:          {},
}

// ValidateCategories checks the flat category tree before it is exposed to a
// selector. A malformed user replacement is rejected as one tree, never
// partially merged with the embedded tree.
func ValidateCategories(categories []Category) error {
	if len(categories) == 0 {
		return fmt.Errorf("writing template category tree is empty")
	}
	byID := make(map[string]Category, len(categories))
	for _, category := range categories {
		if !categoryIDPattern.MatchString(category.ID) {
			return fmt.Errorf("invalid category ID %q", category.ID)
		}
		if _, exists := byID[category.ID]; exists {
			return fmt.Errorf("duplicate category ID %q", category.ID)
		}
		if category.Parent != "" && !categoryIDPattern.MatchString(category.Parent) {
			return fmt.Errorf("invalid parent category ID %q", category.Parent)
		}
		if category.TaskHint != "" {
			if _, ok := allowedTasks[category.TaskHint]; !ok {
				return fmt.Errorf("category %q has unknown task hint %q", category.ID, category.TaskHint)
			}
		}
		byID[category.ID] = category
	}
	for _, category := range categories {
		if category.Parent == "" {
			continue
		}
		parent, ok := byID[category.Parent]
		if !ok {
			return fmt.Errorf("category %q refers to missing parent %q", category.ID, category.Parent)
		}
		if parent.Parent != "" {
			return fmt.Errorf("category %q is deeper than two levels", category.ID)
		}
	}
	return nil
}

func validateTemplate(item Template, categories []Category) error {
	if item.Version != SchemaVersion {
		return fmt.Errorf("unsupported writing template schema version %d", item.Version)
	}
	if !templateIDPattern.MatchString(item.ID) {
		return fmt.Errorf("invalid writing template ID %q", item.ID)
	}
	if item.ContentVersion < 1 {
		return fmt.Errorf("writing template %q content_version must be positive", item.ID)
	}
	if item.Kind != KindTemplate && item.Kind != KindTechnique {
		return fmt.Errorf("writing template %q has unknown kind %q", item.ID, item.Kind)
	}
	if strings.TrimSpace(item.Body) == "" {
		return fmt.Errorf("writing template %q body is required", item.ID)
	}
	if len(item.Category) == 0 || len(item.Category) > 2 {
		return fmt.Errorf("writing template %q category must contain one or two IDs", item.ID)
	}
	categoryMap := make(map[string]Category, len(categories))
	for _, category := range categories {
		categoryMap[category.ID] = category
	}
	for index, categoryID := range item.Category {
		if !categoryIDPattern.MatchString(categoryID) {
			return fmt.Errorf("writing template %q has invalid category ID %q", item.ID, categoryID)
		}
		category, ok := categoryMap[categoryID]
		if !ok {
			return fmt.Errorf("%w: template %q refers to missing category %q", ErrCategoryMissing, item.ID, categoryID)
		}
		if index == 0 && category.Parent != "" {
			return fmt.Errorf("writing template %q category must start at a root: %q", item.ID, categoryID)
		}
		if index == 1 {
			if category.Parent != item.Category[0] {
				return fmt.Errorf("writing template %q category path does not match parent %q", item.ID, item.Category[0])
			}
		}
	}
	last := categoryMap[item.Category[len(item.Category)-1]]
	for _, category := range categories {
		if category.Parent == last.ID {
			return fmt.Errorf("writing template %q category %q is not a leaf", item.ID, last.ID)
		}
	}
	seenTasks := make(map[string]struct{}, len(item.Tasks))
	if len(item.Tasks) == 0 {
		return fmt.Errorf("writing template %q must declare at least one task", item.ID)
	}
	for _, task := range item.Tasks {
		if _, ok := allowedTasks[task]; !ok {
			return fmt.Errorf("writing template %q has unknown task %q", item.ID, task)
		}
		if _, duplicate := seenTasks[task]; duplicate {
			return fmt.Errorf("writing template %q repeats task %q", item.ID, task)
		}
		seenTasks[task] = struct{}{}
	}
	root := item.Category[0]
	if (root == "draft") != (item.Kind == KindTemplate) {
		return fmt.Errorf("writing template %q kind %q does not match root category %q", item.ID, item.Kind, root)
	}
	return nil
}

func normalizeTemplate(item Template) Template {
	item.Version = SchemaVersion
	item.ID = strings.TrimSpace(item.ID)
	item.Kind = strings.TrimSpace(item.Kind)
	item.Body = strings.TrimSpace(item.Body)
	item.Preview = strings.TrimSpace(item.Preview)
	item.Title.ZH = strings.TrimSpace(item.Title.ZH)
	item.Title.EN = strings.TrimSpace(item.Title.EN)
	item.Summary.ZH = strings.TrimSpace(item.Summary.ZH)
	item.Summary.EN = strings.TrimSpace(item.Summary.EN)
	item.Category = normalizeStrings(item.Category)
	item.Tasks = normalizeStrings(item.Tasks)
	item.RecommendedFor = normalizeStrings(item.RecommendedFor)
	return item
}

func normalizeCategory(category Category) Category {
	category.ID = strings.TrimSpace(category.ID)
	category.Parent = strings.TrimSpace(category.Parent)
	category.Title.ZH = strings.TrimSpace(category.Title.ZH)
	category.Title.EN = strings.TrimSpace(category.Title.EN)
	category.TaskHint = strings.TrimSpace(category.TaskHint)
	return category
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sortedCategories(categories []Category) []Category {
	out := append([]Category(nil), categories...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Parent != out[j].Parent {
			return out[i].Parent < out[j].Parent
		}
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}
