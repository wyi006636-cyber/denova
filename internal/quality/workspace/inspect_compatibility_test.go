package workspace

import (
	"bytes"
	"reflect"
	"testing"
)

func TestInspectAllowsExactV1AndPreservesUnknownOptionalFeatureBytes(t *testing.T) {
	workspace := t.TempDir()
	raw := newSchemaV1Marker(t, func(marker map[string]any) {
		features := marker["features"].(map[string]any)
		features["future_projection"] = map[string]any{
			"version":  "9.4.0+vendor.7",
			"required": false,
			"extension_payload": map[string]any{
				"opaque": []any{"keep", 7.0},
			},
		}
		marker["future_top_level"] = map[string]any{"preserve": true}
	})
	writeSchemaMarker(t, workspace, raw)

	inspection, err := newSchemaV1Inspector(t, "1.8.0+local.5").Inspect(workspace)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if inspection.Mode != ModeManagedV1 || inspection.ManagedMutation != MutationAllowed || !inspection.CanOpen() || !inspection.CanManagedMutate() {
		t.Fatalf("managed compatibility = %#v", inspection)
	}
	if inspection.ActiveRoot != ".denova" || !inspection.RootResolution.Consistent() {
		t.Fatalf("root resolution = %#v", inspection.RootResolution)
	}
	if !inspection.Marker.Present || inspection.Marker.Contract.SchemaVersion != 1 {
		t.Fatalf("marker = %#v", inspection.Marker)
	}
	if !reflect.DeepEqual(inspection.UnknownOptionalFeatures, []string{"future_projection"}) {
		t.Fatalf("unknown optional features = %#v", inspection.UnknownOptionalFeatures)
	}
	if got := inspection.Marker.RawBytes(); !bytes.Equal(got, raw) {
		t.Fatalf("raw marker changed:\nwant=%q\n got=%q", raw, got)
	} else {
		got[0] ^= 0xff
		if bytes.Equal(got, inspection.Marker.RawBytes()) {
			t.Fatal("RawBytes must return a defensive copy")
		}
	}
	issue := requireIssue(t, inspection, CodeFeatureOptionalUnknown)
	if issue.Blocking || issue.Field != "features.future_projection" || issue.Value != "9.4.0+vendor.7" {
		t.Fatalf("optional feature issue = %#v", issue)
	}
	if err := inspection.RequireManagedMutation(); err != nil {
		t.Fatalf("RequireManagedMutation: %v", err)
	}
}

func TestInspectCompatibilityFailuresStayReadableAndBlockManagedMutation(t *testing.T) {
	tests := []struct {
		name        string
		raw         func(*testing.T) []byte
		application string
		code        ErrorCode
		field       string
	}{
		{
			name: "malformed marker",
			raw: func(*testing.T) []byte {
				return []byte(`{"schema_version":`)
			},
			application: "1.4.0",
			code:        CodeMarkerMalformed,
			field:       "$",
		},
		{
			name: "duplicate marker field",
			raw: func(t *testing.T) []byte {
				raw := newSchemaV1Marker(t, nil)
				return bytes.Replace(raw, []byte(`"schema_version": 1,`), []byte(`"schema_version": 1, "schema_version": 1,`), 1)
			},
			application: "1.4.0",
			code:        CodeMarkerMalformed,
			field:       "schema_version",
		},
		{
			name: "invalid UTF-8 marker",
			raw: func(*testing.T) []byte {
				return []byte("{\"schema_version\":\"\xff\"}")
			},
			application: "1.4.0",
			code:        CodeMarkerMalformed,
			field:       "$",
		},
		{
			name: "newer schema",
			raw: func(t *testing.T) []byte {
				return newSchemaV1Marker(t, func(marker map[string]any) { marker["schema_version"] = 2 })
			},
			application: "1.4.0",
			code:        CodeSchemaNewer,
			field:       "schema_version",
		},
		{
			name: "unknown required feature",
			raw: func(t *testing.T) []byte {
				return newSchemaV1Marker(t, func(marker map[string]any) {
					marker["features"].(map[string]any)["future_required"] = map[string]any{"version": "1.0.0", "required": true}
				})
			},
			application: "1.4.0",
			code:        CodeFeatureRequiredUnsupported,
			field:       "features.future_required",
		},
		{
			name: "writer version missing",
			raw: func(t *testing.T) []byte {
				return newSchemaV1Marker(t, func(marker map[string]any) { delete(marker["writer"].(map[string]any), "version") })
			},
			application: "1.4.0",
			code:        CodeMarkerFieldMissing,
			field:       "writer.version",
		},
		{
			name: "writer version invalid",
			raw: func(t *testing.T) []byte {
				return newSchemaV1Marker(t, func(marker map[string]any) { marker["writer"].(map[string]any)["version"] = "v1.4.0" })
			},
			application: "1.4.0",
			code:        CodeWriterVersionInvalid,
			field:       "writer.version",
		},
		{
			name: "writer version outside range",
			raw: func(t *testing.T) []byte {
				return newSchemaV1Marker(t, func(marker map[string]any) { marker["writer"].(map[string]any)["version"] = "2.0.0" })
			},
			application: "1.4.0",
			code:        CodeWriterVersionUnsupported,
			field:       "writer.version",
		},
		{
			name: "writer prerelease below range",
			raw: func(t *testing.T) []byte {
				return newSchemaV1Marker(t, func(marker map[string]any) { marker["writer"].(map[string]any)["version"] = "1.0.0-alpha.1" })
			},
			application: "1.4.0",
			code:        CodeWriterVersionUnsupported,
			field:       "writer.version",
		},
		{
			name: "marker widens writer range",
			raw: func(t *testing.T) []byte {
				return newSchemaV1Marker(t, func(marker map[string]any) {
					marker["writer"].(map[string]any)["compatibility_range"] = ">=1.0.0 <3.0.0"
				})
			},
			application: "1.4.0",
			code:        CodeWriterRangeMismatch,
			field:       "writer.compatibility_range",
		},
		{
			name:        "running application version missing",
			raw:         func(t *testing.T) []byte { return newSchemaV1Marker(t, nil) },
			application: "",
			code:        CodeApplicationVersionInvalid,
			field:       "application_version",
		},
		{
			name:        "running application outside writer range",
			raw:         func(t *testing.T) []byte { return newSchemaV1Marker(t, nil) },
			application: "2.0.0",
			code:        CodeApplicationVersionUnsupported,
			field:       "application_version",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			raw := test.raw(t)
			writeSchemaMarker(t, workspace, raw)
			inspection, err := newSchemaV1Inspector(t, test.application).Inspect(workspace)
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if inspection.Mode != ModeSafeReadOpen || inspection.ManagedMutation != MutationBlocked || !inspection.CanOpen() || inspection.CanManagedMutate() {
				t.Fatalf("blocked compatibility = %#v", inspection)
			}
			issue := requireIssue(t, inspection, test.code)
			if !issue.Blocking || issue.Path != MarkerRelativePath && test.field != "application_version" || issue.Field != test.field || issue.Value == nil {
				t.Fatalf("blocking issue = %#v", issue)
			}
			requireMutationBlockedError(t, inspection.RequireManagedMutation(), test.code)
			if got := inspection.Marker.RawBytes(); !bytes.Equal(got, raw) {
				t.Fatalf("blocked marker bytes changed: want=%q got=%q", raw, got)
			}
		})
	}
}

func TestInspectUsesSemVerPrecedenceForPrereleasesInsideV1Ranges(t *testing.T) {
	workspace := t.TempDir()
	raw := newSchemaV1Marker(t, func(marker map[string]any) {
		marker["writer"].(map[string]any)["version"] = "1.5.0-rc.1"
		marker["features"].(map[string]any)["quality_harness"].(map[string]any)["version"] = "1.4.0-beta.2"
	})
	writeSchemaMarker(t, workspace, raw)

	inspection, err := newSchemaV1Inspector(t, "1.8.0-rc.3").Inspect(workspace)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !inspection.CanManagedMutate() || inspection.Mode != ModeManagedV1 {
		t.Fatalf("SemVer-ordered prereleases inside the v1 range were rejected: %#v", inspection.Issues)
	}
}
