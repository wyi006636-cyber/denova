package writingtemplates

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func (c *Catalog) Categories(ctx context.Context) ([]Category, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if err := c.ensureMaterialized(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(c.dir(), "categories.json"))
	if err != nil {
		return nil, err
	}
	categories, err := decodeCategories(data)
	if err != nil {
		return nil, err
	}
	return sortedCategories(categories), nil
}

func decodeCategories(data []byte) ([]Category, error) {
	var categories []Category
	if err := json.Unmarshal(data, &categories); err != nil {
		return nil, fmt.Errorf("parse writing template categories JSON: %w", err)
	}
	for index := range categories {
		categories[index] = normalizeCategory(categories[index])
	}
	if err := ValidateCategories(categories); err != nil {
		return nil, err
	}
	return categories, nil
}

func (c *Catalog) categoriesForValidation() ([]Category, error) {
	data, err := os.ReadFile(filepath.Join(c.dir(), "categories.json"))
	if err == nil {
		if categories, decodeErr := decodeCategories(data); decodeErr == nil {
			return categories, nil
		}
		// A malformed full-tree override must not make every template unreadable.
		// Fall back to the release tree for per-template validation; the Categories
		// endpoint still reports the user's malformed replacement.
	}
	return builtinCategories()
}

func builtinCategories() ([]Category, error) {
	data, err := builtinFS.ReadFile("builtin/categories.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded writing template categories: %w", err)
	}
	return decodeCategories(data)
}
