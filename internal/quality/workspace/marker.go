package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"unicode/utf8"
)

const (
	// MarkerRelativePath is the only Workspace Schema v1 authority marker.
	MarkerRelativePath = ".denova/workspace-schema.json"
	// WriterCompatibilityRangeV1 is local-reader policy, not a marker-defined
	// permission that a future writer may widen.
	WriterCompatibilityRangeV1 = ">=1.0.0 <2.0.0"
)

// ReaderContract is the reader portion of a parsed v1 marker.
type ReaderContract struct {
	MinSchemaVersion int
	MaxSchemaVersion int
	MinDenovaVersion string
}

// WriterContract is the writer portion of a parsed v1 marker.
type WriterContract struct {
	SchemaVersion    int
	MinDenovaVersion string
	Compatibility    string
	Version          string
}

// FeatureContract describes one independently versioned marker feature.
type FeatureContract struct {
	Version  string
	Required bool
}

// MigrationState is the accepted Workspace Schema v1 migration state.
type MigrationState string

const (
	MigrationNotRequired     MigrationState = "not_required"
	MigrationPreviewed       MigrationState = "previewed"
	MigrationValidated       MigrationState = "validated"
	MigrationBackedUp        MigrationState = "backed_up"
	MigrationStaged          MigrationState = "staged"
	MigrationSwitchPending   MigrationState = "switch_pending"
	MigrationSwitched        MigrationState = "switched"
	MigrationVerifying       MigrationState = "verifying"
	MigrationCompleted       MigrationState = "completed"
	MigrationRollbackPending MigrationState = "rollback_pending"
	MigrationRolledBack      MigrationState = "rolled_back"
	MigrationNeedsRecovery   MigrationState = "needs_recovery"
)

var migrationStates = [...]MigrationState{
	MigrationNotRequired,
	MigrationPreviewed,
	MigrationValidated,
	MigrationBackedUp,
	MigrationStaged,
	MigrationSwitchPending,
	MigrationSwitched,
	MigrationVerifying,
	MigrationCompleted,
	MigrationRollbackPending,
	MigrationRolledBack,
	MigrationNeedsRecovery,
}

// AllMigrationStates returns a defensive copy in protocol order.
func AllMigrationStates() []MigrationState {
	states := make([]MigrationState, len(migrationStates))
	copy(states, migrationStates[:])
	return states
}

// Marker is the understood v1 subset. Unknown fields remain preserved in the
// enclosing MarkerRecord raw bytes and are never synthesized on read.
type Marker struct {
	SchemaVersion int
	Reader        ReaderContract
	Writer        WriterContract
	Features      map[string]FeatureContract
	Migration     MigrationState
}

// MarkerRecord preserves the exact source bytes even when the marker is newer
// or malformed. Contract contains only fields understood by this adapter.
type MarkerRecord struct {
	Present  bool
	Contract Marker
	raw      []byte
}

// RawBytes returns a defensive copy of the authoritative marker bytes.
func (record MarkerRecord) RawBytes() []byte {
	return append([]byte(nil), record.raw...)
}

type markerWire struct {
	SchemaVersion *int                    `json:"schema_version"`
	Reader        *markerReaderWire       `json:"reader"`
	Writer        *markerWriterWire       `json:"writer"`
	Features      *map[string]featureWire `json:"features"`
	Migration     *markerMigrationWire    `json:"migration"`
}

type markerReaderWire struct {
	MinSchemaVersion *int    `json:"min_schema_version"`
	MaxSchemaVersion *int    `json:"max_schema_version"`
	MinDenovaVersion *string `json:"min_denova_version"`
}

type markerWriterWire struct {
	SchemaVersion    *int    `json:"schema_version"`
	MinDenovaVersion *string `json:"min_denova_version"`
	Compatibility    *string `json:"compatibility_range"`
	Version          *string `json:"version"`
}

type featureWire struct {
	Version  *string `json:"version"`
	Required *bool   `json:"required"`
}

type markerMigrationWire struct {
	State *string `json:"state"`
}

func parseMarker(raw []byte) (Marker, []CompatibilityIssue) {
	if duplicateField, err := validateMarkerJSON(raw); err != nil {
		field := "$"
		value := err.Error()
		if duplicateField != "" {
			field = duplicateField
			value = "duplicate"
		}
		return Marker{}, []CompatibilityIssue{blockingIssue(CodeMarkerMalformed, MarkerRelativePath, field, value, "workspace marker is not unambiguous valid UTF-8 JSON")}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var wire markerWire
	if err := decoder.Decode(&wire); err != nil {
		return Marker{}, []CompatibilityIssue{malformedMarkerIssue(err)}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return Marker{}, []CompatibilityIssue{malformedMarkerIssue(err)}
	}

	marker := Marker{Features: make(map[string]FeatureContract)}
	issues := make([]CompatibilityIssue, 0)
	if wire.SchemaVersion == nil {
		issues = append(issues, blockingIssue(CodeMarkerFieldMissing, MarkerRelativePath, "schema_version", "missing", "schema version is required"))
	} else {
		marker.SchemaVersion = *wire.SchemaVersion
		switch {
		case marker.SchemaVersion > 1:
			issues = append(issues, blockingIssue(CodeSchemaNewer, MarkerRelativePath, "schema_version", marker.SchemaVersion, "workspace schema is newer than this reader"))
		case marker.SchemaVersion != 1:
			issues = append(issues, blockingIssue(CodeSchemaUnsupported, MarkerRelativePath, "schema_version", marker.SchemaVersion, "only Workspace Schema v1 is supported"))
		}
	}

	if wire.Reader == nil {
		issues = append(issues, blockingIssue(CodeMarkerFieldMissing, MarkerRelativePath, "reader", "missing", "reader contract is required"))
	} else {
		marker.Reader.MinSchemaVersion, issues = exactIntField(wire.Reader.MinSchemaVersion, 1, "reader.min_schema_version", issues)
		marker.Reader.MaxSchemaVersion, issues = exactIntField(wire.Reader.MaxSchemaVersion, 1, "reader.max_schema_version", issues)
		marker.Reader.MinDenovaVersion, issues = exactStringField(wire.Reader.MinDenovaVersion, "1.0.0", "reader.min_denova_version", issues)
	}

	if wire.Writer == nil {
		issues = append(issues, blockingIssue(CodeMarkerFieldMissing, MarkerRelativePath, "writer", "missing", "writer contract is required"))
	} else {
		marker.Writer.SchemaVersion, issues = exactIntField(wire.Writer.SchemaVersion, 1, "writer.schema_version", issues)
		marker.Writer.MinDenovaVersion, issues = exactStringField(wire.Writer.MinDenovaVersion, "1.0.0", "writer.min_denova_version", issues)
		if wire.Writer.Compatibility == nil {
			issues = append(issues, blockingIssue(CodeMarkerFieldMissing, MarkerRelativePath, "writer.compatibility_range", "missing", "writer compatibility range is required"))
		} else {
			marker.Writer.Compatibility = *wire.Writer.Compatibility
			if marker.Writer.Compatibility != WriterCompatibilityRangeV1 {
				issues = append(issues, blockingIssue(CodeWriterRangeMismatch, MarkerRelativePath, "writer.compatibility_range", marker.Writer.Compatibility, "marker range must exactly match the local v1 writer contract"))
			}
		}
		if wire.Writer.Version == nil || *wire.Writer.Version == "" {
			issues = append(issues, blockingIssue(CodeMarkerFieldMissing, MarkerRelativePath, "writer.version", "missing", "last writer version is required"))
		} else {
			marker.Writer.Version = *wire.Writer.Version
		}
	}

	if wire.Features == nil {
		issues = append(issues, blockingIssue(CodeMarkerFieldMissing, MarkerRelativePath, "features", "missing", "feature map is required"))
	} else {
		featureIDs := make([]string, 0, len(*wire.Features))
		for id := range *wire.Features {
			featureIDs = append(featureIDs, id)
		}
		sort.Strings(featureIDs)
		for _, id := range featureIDs {
			feature := (*wire.Features)[id]
			field := "features." + id
			if id == "" || feature.Version == nil || *feature.Version == "" || feature.Required == nil {
				issues = append(issues, blockingIssue(CodeFeatureMalformed, MarkerRelativePath, field, "missing_or_invalid", "feature version and required flag are required"))
				continue
			}
			marker.Features[id] = FeatureContract{Version: *feature.Version, Required: *feature.Required}
		}
	}

	if wire.Migration == nil || wire.Migration.State == nil || *wire.Migration.State == "" {
		issues = append(issues, blockingIssue(CodeMarkerFieldMissing, MarkerRelativePath, "migration.state", "missing", "migration state is required"))
	} else {
		marker.Migration = MigrationState(*wire.Migration.State)
		if !validMigrationState(marker.Migration) {
			issues = append(issues, blockingIssue(CodeMigrationStateInvalid, MarkerRelativePath, "migration.state", marker.Migration, "unknown Workspace Schema v1 migration state"))
		}
	}
	return marker, issues
}

type duplicateJSONFieldError struct {
	field string
}

func (err *duplicateJSONFieldError) Error() string {
	return fmt.Sprintf("duplicate JSON field %s", err.field)
}

func validateMarkerJSON(raw []byte) (string, error) {
	if !utf8.Valid(raw) {
		return "", errors.New("marker is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := walkUniqueJSONValue(decoder, "$", 0); err != nil {
		var duplicate *duplicateJSONFieldError
		if errors.As(err, &duplicate) {
			return duplicate.field, err
		}
		return "", err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return "", err
	}
	return "", nil
}

func walkUniqueJSONValue(decoder *json.Decoder, field string, depth int) error {
	if depth > 128 {
		return errors.New("marker JSON nesting exceeds 128 levels")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			childField := key
			if field != "$" {
				childField = field + "." + key
			}
			if _, exists := seen[key]; exists {
				return &duplicateJSONFieldError{field: childField}
			}
			seen[key] = struct{}{}
			if err := walkUniqueJSONValue(decoder, childField, depth+1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		index := 0
		for decoder.More() {
			if err := walkUniqueJSONValue(decoder, fmt.Sprintf("%s[%d]", field, index), depth+1); err != nil {
				return err
			}
			index++
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}

func exactIntField(value *int, expected int, field string, issues []CompatibilityIssue) (int, []CompatibilityIssue) {
	if value == nil {
		return 0, append(issues, blockingIssue(CodeMarkerFieldMissing, MarkerRelativePath, field, "missing", "required integer field is missing"))
	}
	if *value != expected {
		issues = append(issues, blockingIssue(CodeMarkerFieldMismatch, MarkerRelativePath, field, *value, "field does not match the Workspace Schema v1 contract"))
	}
	return *value, issues
}

func exactStringField(value *string, expected, field string, issues []CompatibilityIssue) (string, []CompatibilityIssue) {
	if value == nil || *value == "" {
		return "", append(issues, blockingIssue(CodeMarkerFieldMissing, MarkerRelativePath, field, "missing", "required string field is missing"))
	}
	if *value != expected {
		issues = append(issues, blockingIssue(CodeMarkerFieldMismatch, MarkerRelativePath, field, *value, "field does not match the Workspace Schema v1 contract"))
	}
	return *value, issues
}

func malformedMarkerIssue(err error) CompatibilityIssue {
	field := "$"
	value := err.Error()
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		field = typeErr.Field
		value = typeErr.Value
	}
	return blockingIssue(CodeMarkerMalformed, MarkerRelativePath, field, value, "workspace marker is not valid understood JSON")
}

func validMigrationState(state MigrationState) bool {
	for _, known := range migrationStates {
		if state == known {
			return true
		}
	}
	return false
}
