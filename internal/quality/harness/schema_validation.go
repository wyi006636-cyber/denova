package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"denova/docs/project-design/implementation/contracts"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const qualityEventSchemaURL = "https://denova.example/schemas/quality-event-v1.schema.json"

// Validator combines the normative JSON shape with exact-v1 semantic validation.
type Validator struct {
	schema *jsonschema.Schema
}

func NewValidator() (*Validator, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(contracts.QualityEventV1Schema()))
	if err != nil {
		return nil, fmt.Errorf("decode Quality event schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	if err := compiler.AddResource(qualityEventSchemaURL, document); err != nil {
		return nil, fmt.Errorf("add Quality event schema: %w", err)
	}
	schema, err := compiler.Compile(qualityEventSchemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile Quality event schema: %w", err)
	}
	return &Validator{schema: schema}, nil
}

// InspectedEvent keeps unsupported event bytes opaque while exposing only its routing header.
type InspectedEvent struct {
	raw       []byte
	contract  Contract
	eventType EventType
}

func (event InspectedEvent) RawBytes() []byte     { return bytes.Clone(event.raw) }
func (event InspectedEvent) Contract() Contract   { return event.contract }
func (event InspectedEvent) EventType() EventType { return event.eventType }

func InspectEvent(raw []byte) (InspectedEvent, error) {
	var header struct {
		Contract  Contract  `json:"contract"`
		EventType EventType `json:"event_type"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return InspectedEvent{}, fmt.Errorf("inspect Quality event: %w", err)
	}
	if header.Contract.Kind == "" || header.Contract.Version == "" || header.EventType == "" {
		return InspectedEvent{}, fmt.Errorf("inspect Quality event: contract kind, version, and event_type are required")
	}
	return InspectedEvent{
		raw:       bytes.Clone(raw),
		contract:  header.Contract,
		eventType: header.EventType,
	}, nil
}

func (validator *Validator) ValidateJSON(raw []byte) (Event, error) {
	inspection, err := InspectEvent(raw)
	if err != nil {
		return Event{}, err
	}
	if inspection.contract.Kind != ContractKind || inspection.contract.Version != ContractVersionV1 {
		return Event{}, fmt.Errorf("Quality event is not exact %s %s", ContractKind, ContractVersionV1)
	}
	if validator == nil || validator.schema == nil {
		return Event{}, fmt.Errorf("Quality event schema is unavailable")
	}
	if err := validateUnambiguousJSON(raw); err != nil {
		return Event{}, fmt.Errorf("Quality event JSON is ambiguous: %w", err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Event{}, fmt.Errorf("decode Quality event JSON: %w", err)
	}
	if err := validator.schema.Validate(document); err != nil {
		return Event{}, fmt.Errorf("Quality event does not satisfy exact v1 schema: %w", err)
	}
	var event Event
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return Event{}, fmt.Errorf("decode exact Quality event v1: %w", err)
	}
	if err := ValidateEvent(event); err != nil {
		return Event{}, fmt.Errorf("validate exact Quality event v1 semantics: %w", err)
	}
	return event, nil
}

func validateUnambiguousJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := readUniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func readUniqueJSONValue(decoder *json.Decoder) error {
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
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := readUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("object is not closed")
		}
		return nil
	case '[':
		for decoder.More() {
			if err := readUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("array is not closed")
		}
		return nil
	}
	return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
}
