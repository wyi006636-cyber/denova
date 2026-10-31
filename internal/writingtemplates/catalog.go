package writingtemplates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"denova/internal/presetlayout"
	"denova/internal/revisionfile"
	"denova/internal/revisionjson"
)

// Catalog is a user-level catalog. Built-in templates stay in the release
// embed; only author-created files are persisted below the data root.
type Catalog struct {
	DataRoot string
}

func NewCatalog(dataRoot string) *Catalog { return &Catalog{DataRoot: strings.TrimSpace(dataRoot)} }

func (c *Catalog) List(ctx context.Context, filter ListFilter) ([]Template, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if err := c.ensureDir(ctx); err != nil {
		return nil, err
	}
	categories, err := c.categoriesForValidation(ctx)
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(c.dir(), "*.json"))
	if err != nil {
		return nil, err
	}
	userFiles := make(map[string]string, len(files))
	for _, path := range files {
		if filepath.Base(path) != "categories.json" {
			userFiles[strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))] = path
		}
	}
	items := make([]Template, 0, len(files)+5)
	for _, builtin := range builtinTemplates() {
		if path, ok := userFiles[builtin.ID]; ok {
			item, readErr := c.readTemplate(ctx, path, categories)
			if readErr != nil {
				if stale, staleErr := c.readTemplate(ctx, path, mustBuiltinCategories()); staleErr == nil && isMaterializedBuiltin(stale, builtin) {
					slog.Info("ignored stale materialized writing template copy", "id", builtin.ID, "path", path)
					items = appendFiltered(items, c.builtinView(builtin, categories), filter)
				} else {
					items = appendFiltered(items, invalidTemplate(path, readErr), filter)
				}
			} else if isMaterializedBuiltin(item, builtin) {
				slog.Info("ignored stale materialized writing template copy", "id", builtin.ID, "path", path)
				items = appendFiltered(items, c.builtinView(builtin, categories), filter)
			} else {
				items = appendFiltered(items, c.applyOwnership(item), filter)
			}
			delete(userFiles, builtin.ID)
			continue
		}
		items = appendFiltered(items, c.builtinView(builtin, categories), filter)
	}
	for _, path := range userFiles {
		item, readErr := c.readTemplate(ctx, path, categories)
		if readErr != nil {
			item = invalidTemplate(path, readErr)
		} else {
			item = c.applyOwnership(item)
		}
		items = appendFiltered(items, item, filter)
	}
	for index := range items {
		items[index].Body = ""
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

func appendFiltered(items []Template, item Template, filter ListFilter) []Template {
	if matchesFilter(item, filter) {
		return append(items, item)
	}
	return items
}

func (c *Catalog) Get(ctx context.Context, id string) (Template, error) {
	if err := contextErr(ctx); err != nil {
		return Template{}, err
	}
	id = strings.TrimSpace(id)
	if err := validateTemplateID(id); err != nil {
		return Template{}, err
	}
	if err := c.ensureDir(ctx); err != nil {
		return Template{}, err
	}
	categories, err := c.categoriesForValidation(ctx)
	if err != nil {
		return Template{}, err
	}
	builtin, builtinOK := builtinTemplateByID(id)
	path := filepath.Join(c.dir(), id+".json")
	if _, statErr := os.Stat(path); statErr == nil {
		item, readErr := c.readTemplate(ctx, path, categories)
		if readErr != nil {
			if errors.Is(readErr, ErrCategoryMissing) {
				if stale, staleErr := c.readTemplate(ctx, path, mustBuiltinCategories()); staleErr == nil && isMaterializedBuiltin(stale, builtin) {
					slog.Info("ignored stale materialized writing template copy", "id", id, "path", path)
					return c.builtinView(builtin, categories), nil
				}
				return invalidTemplate(path, readErr), nil
			}
			return Template{}, readErr
		}
		if builtinOK && isMaterializedBuiltin(item, builtin) {
			slog.Info("ignored stale materialized writing template copy", "id", id, "path", path)
			return c.builtinView(builtin, categories), nil
		}
		return c.applyOwnership(item), nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Template{}, statErr
	}
	if !builtinOK {
		return Template{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return c.builtinView(builtin, categories), nil
}

func (c *Catalog) Create(ctx context.Context, item Template) (Template, error) {
	if err := contextErr(ctx); err != nil {
		return Template{}, err
	}
	if err := c.ensureDir(ctx); err != nil {
		return Template{}, err
	}
	categories, err := c.categoriesForValidation(ctx)
	if err != nil {
		return Template{}, err
	}
	item = normalizeTemplate(item)
	autoID := item.ID == ""
	if autoID {
		item.ID = newTemplateID(item.Title.EN)
	} else if !strings.HasPrefix(item.ID, "user.") {
		return Template{}, fmt.Errorf("%w: user writing template ID must start with user", ErrInvalidTemplate)
	}
	item.Version = SchemaVersion
	if item.ContentVersion == 0 {
		item.ContentVersion = 1
	}
	resetDerived(&item)
	if err := validateTemplate(item, categories); err != nil {
		return Template{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if item.CreatedAt == "" {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	for attempts := 0; attempts < 64; attempts++ {
		if attempts > 0 && autoID {
			item.ID = newTemplateID(item.Title.EN)
		}
		path := filepath.Join(c.dir(), item.ID+".json")
		document, createErr := c.templateStore(path, categories).Create(ctx, item)
		if errors.Is(createErr, revisionjson.ErrAlreadyExists) && autoID {
			continue
		}
		if errors.Is(createErr, revisionjson.ErrAlreadyExists) {
			return Template{}, fmt.Errorf("writing template already exists: %s", item.ID)
		}
		if createErr != nil {
			return Template{}, createErr
		}
		document.Value.Path, document.Value.Revision = path, document.Revision
		return c.applyOwnership(document.Value), nil
	}
	return Template{}, fmt.Errorf("could not allocate a unique writing template ID")
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
	if err := c.ensureDir(ctx); err != nil {
		return Template{}, err
	}
	categories, err := c.categoriesForValidation(ctx)
	if err != nil {
		return Template{}, err
	}
	path := filepath.Join(c.dir(), id+".json")
	item = normalizeTemplate(item)
	item.ID, item.Version = id, SchemaVersion
	prepare := func(current Template) (Template, error) {
		if item.ContentVersion == 0 {
			item.ContentVersion = current.ContentVersion
		}
		if item.ContentVersion < current.ContentVersion {
			return Template{}, fmt.Errorf("%w: writing template %q content_version cannot decrease below %d", ErrInvalidTemplate, id, current.ContentVersion)
		}
		if item.Body != current.Body && item.ContentVersion <= current.ContentVersion {
			return Template{}, fmt.Errorf("%w: writing template %q body changes require content_version > %d", ErrInvalidTemplate, id, current.ContentVersion)
		}
		item.CreatedAt = firstNonEmpty(current.CreatedAt, item.CreatedAt, time.Now().UTC().Format(time.RFC3339Nano))
		item.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		resetDerived(&item)
		return item, validateTemplate(item, categories)
	}
	var document revisionjson.Document[Template]
	var updateErr error
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		builtin, builtinOK := builtinTemplateByID(id)
		if !builtinOK {
			return Template{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		if baseRevision != builtinRevision(builtin) {
			return Template{}, ErrRevisionConflict
		}
		candidate, prepareErr := prepare(builtin)
		if prepareErr != nil {
			return Template{}, prepareErr
		}
		document, updateErr = c.templateStore(path, categories).Create(ctx, candidate)
	} else if statErr != nil {
		return Template{}, statErr
	} else {
		document, updateErr = c.templateStore(path, categories).Update(ctx, baseRevision, prepare)
	}
	if updateErr != nil {
		if errors.Is(updateErr, os.ErrNotExist) {
			return Template{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		if errors.Is(updateErr, revisionfile.ErrRevisionConflict) || errors.Is(updateErr, revisionjson.ErrRevisionRequired) || errors.Is(updateErr, revisionjson.ErrAlreadyExists) {
			return Template{}, fmt.Errorf("%w: %v", ErrRevisionConflict, updateErr)
		}
		return Template{}, updateErr
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
	if err := c.ensureDir(ctx); err != nil {
		return err
	}
	path := filepath.Join(c.dir(), id+".json")
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return err
	}
	return nil
}

func (c *Catalog) dir() string { return presetlayout.WritingTemplates(strings.TrimSpace(c.DataRoot)) }

func (c *Catalog) ensureDir(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(c.DataRoot) == "" {
		return ErrDataRootRequired
	}
	if err := os.MkdirAll(c.dir(), 0o755); err != nil {
		return err
	}
	return contextErr(ctx)
}

func (c *Catalog) readTemplate(ctx context.Context, path string, categories []Category) (Template, error) {
	document, err := c.templateStore(path, categories).Read(ctx)
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
			resetDerived(&item)
			return canonicalTemplateBytes(item)
		},
	})
}
