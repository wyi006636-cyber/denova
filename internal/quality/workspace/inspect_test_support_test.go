package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newSchemaV1Marker(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	marker := map[string]any{
		"schema_version": 1,
		"reader": map[string]any{
			"min_schema_version": 1,
			"max_schema_version": 1,
			"min_denova_version": "1.0.0",
		},
		"writer": map[string]any{
			"schema_version":      1,
			"min_denova_version":  "1.0.0",
			"compatibility_range": WriterCompatibilityRangeV1,
			"version":             "1.6.2+test.1",
		},
		"features": map[string]any{
			"quality_harness": map[string]any{
				"version":  "1.1.0",
				"required": true,
			},
		},
		"migration": map[string]any{
			"state": "not_required",
		},
	}
	if mutate != nil {
		mutate(marker)
	}
	raw, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func writeSchemaMarker(t *testing.T, workspace string, raw []byte) {
	t.Helper()
	path := filepath.Join(workspace, filepath.FromSlash(MarkerRelativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func newSchemaV1Inspector(t *testing.T, applicationVersion string) *Inspector {
	t.Helper()
	inspector, err := NewInspector(InspectorOptions{
		ApplicationVersion: applicationVersion,
		SupportedFeatures: map[string]string{
			"quality_harness": ">=1.0.0 <2.0.0",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return inspector
}

func requireIssue(t *testing.T, inspection Inspection, code ErrorCode) CompatibilityIssue {
	t.Helper()
	for _, issue := range inspection.Issues {
		if issue.Code == code {
			return issue
		}
	}
	t.Fatalf("inspection issues %#v do not contain %q", inspection.Issues, code)
	return CompatibilityIssue{}
}

func requireMutationBlockedError(t *testing.T, err error, code ErrorCode) *MutationBlockedError {
	t.Helper()
	var blocked *MutationBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("error = %T %v, want *MutationBlockedError", err, err)
	}
	found := false
	for _, issue := range blocked.Issues {
		if issue.Code == code {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("blocked issues %#v do not contain %q", blocked.Issues, code)
	}
	return blocked
}
