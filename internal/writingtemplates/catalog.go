package writingtemplates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"denova/internal/presetlayout"
	"denova/internal/revisionfile"
	"denova/internal/revisionjson"
)

// Catalog is a user-level catalog. DataRoot is intentionally the only host
// path it stores; item identity is always the stable Template.ID.
type Catalog struct {
	DataRoot string
}

func NewCatalog(dataRoot string) *Catalog {
	return &Catalog{DataRoot: strings.TrimSpace(dataRoot)}
}

// New is kept as a concise constructor for application services and tests.
func New(dataRoot string) *Catalog { return NewCatalog(dataRoot) }

func (c *Catalog) List(ctx context.Context, filter ListFilter) ([]Template, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if err := c.ensureMaterialized(); err != nil {
		return nil, err
	}
	categories, err := c.categoriesForValidation()
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(c.dir(), "*.json"))
	if err != nil {
		return nil, err
	}
	items := make([]Template, 0, len(files))
	for _, path := range files {
		if filepath.Base(path) == "categories.json" {
			continue
		}
		item, readErr := c.readTemplate(path, categories)
		if readErr != nil {
			id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			invalid := Template{ID: id, Path: path, Invalid: true, Error: readErr.Error()}
			invalid = c.applyOwnership(invalid)
			if matchesFilter(invalid, filter) {
				items = append(items, invalid)
			}
			continue
		}
		item = c.applyOwnership(item)
		if matchesFilter(item, filter) {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftBuiltin, rightBuiltin := items[i].Source == "builtin", items[j].Source == "builtin"
		if leftBuiltin != rightBuiltin {
			return leftBuiltin
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

// ListAll is a convenience for callers that do not need filters.
func (c *Catalog) ListAll(ctx context.Context) ([]Template, error) {
	return c.List(ctx, ListFilter{})
}

func (c *Catalog) Get(ctx context.Context, id string) (Template, error) {
	if err := contextErr(ctx); err != nil {
		return Template{}, err
	}
	id = strings.TrimSpace(id)
	if err := validateTemplateID(id); err != nil {
		return Template{}, err
	}
	if err := c.ensureMaterialized(); err != nil {
		return Template{}, err
	}
	categories, err := c.categoriesForValidation()
	if err != nil {
		return Template{}, err
	}
	item, err := c.readTemplate(filepath.Join(c.dir(), id+".json"), categories)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Template{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return Template{}, err
	}
	return c.applyOwnership(item), nil
}

func (c *Catalog) Create(ctx context.Context, item Template) (Template, error) {
	if err := contextErr(ctx); err != nil {
		return Template{}, err
	}
	if err := c.ensureMaterialized(); err != nil {
		return Template{}, err
	}
	categories, err := c.categoriesForValidation()
	if err != nil {
		return Template{}, err
	}
	item = normalizeTemplate(item)
	if item.ID == "" {
		item.ID = newTemplateID(item.Title.EN, item.Title.ZH)
	}
	if err := validateTemplateID(item.ID); err != nil {
		return Template{}, err
	}
	if isBuiltinTemplateID(item.ID) {
		return Template{}, fmt.Errorf("built-in writing templates are updated as user overrides: %s", item.ID)
	}
	item.Version = SchemaVersion
	if item.ContentVersion == 0 {
		item.ContentVersion = 1
	}
	item.Source, item.Revision, item.Path = "", "", ""
	item.Invalid, item.Error, item.BuiltinOverridden = false, "", false
	if err := validateTemplate(item, categories); err != nil {
		return Template{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if item.CreatedAt == "" {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	document, err := c.templateStore(filepath.Join(c.dir(), item.ID+".json"), categories).Create(ctx, item)
	if errors.Is(err, revisionjson.ErrAlreadyExists) {
		return Template{}, fmt.Errorf("writing template already exists: %s", item.ID)
	}
	if err != nil {
		return Template{}, err
	}
	item = document.Value
	item.Path, item.Revision = filepath.Join(c.dir(), item.ID+".json"), document.Revision
	return c.applyOwnership(item), nil
}

func (c *Catalog) Update(ctx context.Context, id string, item Template, baseRevision string) (Template, error) {
	if err := contextErr(ctx); err != nil {
		return Template{}, err
	}
	id = strings.TrimSpace(id)
	if err := validateTemplateID(id); err != nil {
		return Template{}, err
	}
	if strings.TrimSpace(baseRevision) == "" {
		return Template{}, ErrRevisionConflict
	}
	if err := c.ensureMaterialized(); err != nil {
		return Template{}, err
	}
	categories, err := c.categoriesForValidation()
	if err != nil {
		return Template{}, err
	}
	path := filepath.Join(c.dir(), id+".json")
	item = normalizeTemplate(item)
	item.ID = id
	item.Version = SchemaVersion
	document, err := c.templateStore(path, categories).Update(ctx, baseRevision, func(current Template) (Template, error) {
		if item.ContentVersion == 0 {
			item.ContentVersion = current.ContentVersion
		}
		if item.Body != current.Body && item.ContentVersion <= current.ContentVersion {
			return Template{}, fmt.Errorf("writing template %q body changes require content_version > %d", id, current.ContentVersion)
		}
		item.CreatedAt = firstNonEmpty(current.CreatedAt, item.CreatedAt, time.Now().UTC().Format(time.RFC3339Nano))
		item.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		item.Source, item.Revision, item.Path = "", "", ""
		item.Invalid, item.Error, item.BuiltinOverridden = false, "", false
		return item, validateTemplate(item, categories)
	})
	if err != nil {
		if errors.Is(err, revisionfile.ErrRevisionConflict) || errors.Is(err, revisionjson.ErrRevisionRequired) {
			return Template{}, fmt.Errorf("%w: %v", ErrRevisionConflict, err)
		}
		return Template{}, err
	}
	document.Value.Path, document.Value.Revision = path, document.Revision
	return c.applyOwnership(document.Value), nil
}

func (c *Catalog) Delete(ctx context.Context, id string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if err := validateTemplateID(id); err != nil {
		return err
	}
	if err := c.ensureMaterialized(); err != nil {
		return err
	}
	path := filepath.Join(c.dir(), id+".json")
	if isBuiltinTemplateID(id) {
		item, ok := builtinTemplateByID(id)
		if !ok {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		_, err := c.templateStore(path, mustBuiltinCategories()).Replace(ctx, item)
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return err
	}
	return nil
}

func (c *Catalog) dir() string {
	return presetlayout.WritingTemplates(strings.TrimSpace(c.DataRoot))
}

func (c *Catalog) ensureMaterialized() error {
	if strings.TrimSpace(c.DataRoot) == "" {
		return ErrDataRootRequired
	}
	if err := os.MkdirAll(c.dir(), 0o755); err != nil {
		return err
	}
	categoryPath := filepath.Join(c.dir(), "categories.json")
	if _, err := os.Stat(categoryPath); errors.Is(err, os.ErrNotExist) {
		data, readErr := builtinFS.ReadFile("builtin/categories.json")
		if readErr != nil {
			return readErr
		}
		if writeErr := os.WriteFile(categoryPath, data, 0o644); writeErr != nil {
			return writeErr
		}
	} else if err != nil {
		return err
	}
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "categories.json" || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, readErr := builtinFS.ReadFile(filepath.ToSlash(filepath.Join("builtin", entry.Name())))
		if readErr != nil {
			return readErr
		}
		var item Template
		if unmarshalErr := json.Unmarshal(data, &item); unmarshalErr != nil {
			return fmt.Errorf("parse embedded writing template %s: %w", entry.Name(), unmarshalErr)
		}
		item = normalizeTemplate(item)
		if item.Version == 0 {
			item.Version = SchemaVersion
		}
		if item.ContentVersion == 0 {
			item.ContentVersion = 1
		}
		categories, categoryErr := c.categoriesForValidation()
		if categoryErr != nil {
			return categoryErr
		}
		if validateErr := validateTemplate(item, categories); validateErr != nil {
			return fmt.Errorf("embedded writing template %s is invalid: %w", entry.Name(), validateErr)
		}
		path := filepath.Join(c.dir(), item.ID+".json")
		if _, statErr := os.Stat(path); statErr == nil {
			continue
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if _, writeErr := c.templateStore(path, categories).Replace(context.Background(), item); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func (c *Catalog) readTemplate(path string, categories []Category) (Template, error) {
	document, err := c.templateStore(path, categories).Read(context.Background())
	if err != nil {
		return Template{}, err
	}
	item := document.Value
	item.Path, item.Revision = path, document.Revision
	return item, nil
}

func (c *Catalog) templateStore(path string, categories []Category) revisionjson.Store[Template] {
	return revisionjson.NewStore(path, revisionjson.Codec[Template]{
		Decode: func(data []byte) (Template, error) {
			var item Template
			if err := json.Unmarshal(data, &item); err != nil {
				return Template{}, fmt.Errorf("parse writing template JSON: %w", err)
			}
			item = normalizeTemplate(item)
			if err := validateTemplate(item, categories); err != nil {
				return Template{}, err
			}
			return item, nil
		},
		Encode: func(item Template) ([]byte, error) {
			item = normalizeTemplate(item)
			if err := validateTemplate(item, categories); err != nil {
				return nil, err
			}
			item.Source, item.Revision, item.Path = "", "", ""
			item.Invalid, item.Error, item.BuiltinOverridden = false, "", false
			data, err := json.MarshalIndent(item, "", "  ")
			if err != nil {
				return nil, err
			}
			return append(data, '\n'), nil
		},
	})
}

func (c *Catalog) applyOwnership(item Template) Template {
	item = normalizeTemplate(item)
	item.Path = filepath.Join(c.dir(), item.ID+".json")
	builtin, ok := builtinTemplateByID(item.ID)
	if !ok {
		item.Source = "user"
		item.BuiltinOverridden = false
		return item
	}
	if templateComparable(item) == templateComparable(builtin) {
		item.Source = "builtin"
		item.BuiltinOverridden = false
	} else {
		item.Source = "user"
		item.BuiltinOverridden = true
	}
	return item
}

func templateComparable(item Template) string {
	item.Source, item.Revision, item.Path = "", "", ""
	item.Invalid, item.Error, item.BuiltinOverridden = false, "", false
	item.CreatedAt, item.UpdatedAt = "", ""
	data, _ := json.Marshal(item)
	return string(data)
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
		if path != filter.Category {
			found := false
			for _, category := range item.Category {
				if category == filter.Category {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	if query == "" {
		return true
	}
	text := strings.ToLower(strings.Join([]string{item.ID, item.Title.ZH, item.Title.EN, item.Summary.ZH, item.Summary.EN, item.Body}, "\n"))
	return strings.Contains(text, query)
}

func validateTemplateID(id string) error {
	if !templateIDPattern.MatchString(id) {
		return fmt.Errorf("invalid writing template ID %q", id)
	}
	return nil
}

func newTemplateID(title ...string) string {
	base := "writing-template"
	for _, value := range title {
		candidate := strings.ToLower(strings.TrimSpace(value))
		if candidate == "" {
			continue
		}
		var b strings.Builder
		for _, r := range candidate {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
				b.WriteRune(r)
			case r == '-' || r == '_':
				b.WriteByte('-')
			}
		}
		candidate = strings.Trim(b.String(), "-")
		if candidate != "" {
			base = candidate
			break
		}
	}
	return fmt.Sprintf("%s.%d", base, time.Now().UTC().UnixNano())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isBuiltinTemplateID(id string) bool {
	_, ok := builtinTemplateByID(id)
	return ok
}

func builtinTemplateByID(id string) (Template, bool) {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return Template{}, false
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "categories.json" || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := builtinFS.ReadFile(filepath.ToSlash(filepath.Join("builtin", entry.Name())))
		if err != nil {
			continue
		}
		var item Template
		if json.Unmarshal(data, &item) != nil {
			continue
		}
		item = normalizeTemplate(item)
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
