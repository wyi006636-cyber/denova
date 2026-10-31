package writingtemplates

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (c *Catalog) applyOwnership(item Template) Template {
	item = normalizeTemplate(item)
	item.Path = filepath.Join(c.dir(), item.ID+".json")
	builtin, ok := builtinTemplateByID(item.ID)
	if !ok {
		item.Source = "user"
		item.BuiltinOverridden = false
		item.BuiltinContentVersion = 0
		return item
	}
	item.Source = "user"
	item.BuiltinOverridden = true
	item.BuiltinContentVersion = builtin.ContentVersion
	return item
}

func (c *Catalog) builtinView(item Template, categories []Category) Template {
	item = normalizeTemplate(item)
	item.Path = ""
	item.Revision = builtinRevision(item)
	item.Source = "builtin"
	item.BuiltinOverridden = false
	item.BuiltinContentVersion = 0
	if err := validateTemplate(item, categories); err != nil {
		item.Invalid, item.Error = true, err.Error()
	}
	return item
}

func invalidTemplate(path string, err error) Template {
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return Template{ID: id, Path: path, Invalid: true, Error: err.Error(), Source: "user"}
}

func isMaterializedBuiltin(item, builtin Template) bool {
	return item.Revision == builtinRevision(builtin) && templateComparable(item) == templateComparable(builtin)
}

func templateComparable(item Template) string {
	resetDerived(&item)
	item.CreatedAt, item.UpdatedAt = "", ""
	data, _ := json.Marshal(item)
	return string(data)
}

func resetDerived(item *Template) {
	item.Source, item.Revision, item.Path = "", "", ""
	item.Invalid, item.Error, item.BuiltinOverridden = false, "", false
	item.BuiltinContentVersion = 0
}

func canonicalTemplateBytes(item Template) ([]byte, error) {
	resetDerived(&item)
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func builtinRevision(item Template) string {
	data, err := canonicalTemplateBytes(item)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func matchesFilter(item Template, filter ListFilter) bool {
	if filter.Kind != "" && item.Kind != filter.Kind {
		return false
	}
	if filter.Task != "" && !hasTask(item.Tasks, filter.Task) {
		return false
	}
	if filter.Category != "" {
		path := strings.Join(item.Category, ".")
		if path != filter.Category && !hasString(item.Category, filter.Category) {
			return false
		}
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	if query == "" {
		return true
	}
	text := strings.ToLower(strings.Join([]string{item.ID, item.Title.ZH, item.Title.EN, item.Summary.ZH, item.Summary.EN, item.Body}, "\n"))
	return strings.Contains(text, query)
}

func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateTemplateID(id string) error {
	if !templateIDPattern.MatchString(id) {
		return fmt.Errorf("invalid writing template ID %q", id)
	}
	return nil
}

func newTemplateID(title string) string {
	slug := asciiSlug(title)
	if slug == "" {
		slug = "template"
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		copy(suffix[:], fmt.Sprintf("%06d", time.Now().UnixNano()%1000000))
	}
	for index := range suffix {
		suffix[index] = alphabet[int(suffix[index])%len(alphabet)]
	}
	return "user." + slug + "-" + string(suffix[:])
}

func asciiSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	separator := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			if separator && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if b.Len() > 0 {
				if separator {
					b.WriteByte('-')
				}
				b.WriteRune(r)
			}
		default:
			if b.Len() > 0 {
				separator = true
			}
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			separator = false
		}
	}
	return strings.Trim(b.String(), "-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func builtinTemplates() []Template {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil
	}
	items := make([]Template, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "categories.json" || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, readErr := builtinFS.ReadFile(filepath.ToSlash(filepath.Join("builtin", entry.Name())))
		if readErr != nil {
			continue
		}
		var item Template
		if json.Unmarshal(data, &item) == nil {
			items = append(items, normalizeTemplate(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func builtinTemplateByID(id string) (Template, bool) {
	for _, item := range builtinTemplates() {
		if item.ID == id {
			return item, true
		}
	}
	return Template{}, false
}

func mustBuiltinCategories() []Category {
	categories, err := builtinCategories()
	if err != nil {
		return nil
	}
	return categories
}
