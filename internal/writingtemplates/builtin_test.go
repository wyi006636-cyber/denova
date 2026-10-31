package writingtemplates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinTemplatesLoadAndKinds(t *testing.T) {
	items := builtinTemplates()
	if len(items) != 5 {
		t.Fatalf("embedded templates = %d, want 5", len(items))
	}
	for _, item := range items {
		if item.Source != "" || item.Invalid || strings.TrimSpace(item.Body) == "" {
			t.Fatalf("raw builtin item has derived/empty state: %+v", item)
		}
		if item.Kind == KindTemplate && item.Category[0] != "draft" {
			t.Fatalf("template outside draft root: %s", item.ID)
		}
		if item.Kind == KindTechnique && item.Category[0] == "draft" {
			t.Fatalf("technique inside draft root: %s", item.ID)
		}
	}
}

func TestBuiltinTemplateContentVersionsAreStable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "builtin-hashes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pins map[string]struct {
		ContentVersion int    `json:"content_version"`
		SHA256         string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &pins); err != nil {
		t.Fatal(err)
	}
	items := builtinTemplates()
	if len(pins) != len(items) {
		t.Fatalf("hash pin count = %d, builtin count = %d", len(pins), len(items))
	}
	for _, item := range items {
		pin, ok := pins[item.ID]
		if !ok {
			t.Fatalf("missing hash pin for %s", item.ID)
		}
		sum := sha256.Sum256([]byte(strings.TrimSpace(item.Body)))
		if got := hex.EncodeToString(sum[:]); got != pin.SHA256 {
			t.Errorf("%s body hash = %s, want %s", item.ID, got, pin.SHA256)
		}
		if item.ContentVersion != pin.ContentVersion {
			t.Errorf("%s content_version = %d, want %d", item.ID, item.ContentVersion, pin.ContentVersion)
		}
	}
}
