package profile

import (
	"bytes"
	"encoding/json"
	"fmt"

	"denova/internal/quality/domain"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	profileSchemaURL     = "https://denova.example/schemas/profile-v1.schema.json"
	qualitySpecSchemaURL = "https://denova.example/schemas/quality-spec-v1.schema.json"
)

// Decoder validates exact v1 Profile records against both normative schemas.
type Decoder struct {
	profileSchema     *jsonschema.Schema
	qualitySpecSchema *jsonschema.Schema
}

func NewDecoder(profileSchemaBytes, qualitySpecSchemaBytes []byte) (*Decoder, error) {
	profileDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(profileSchemaBytes))
	if err != nil {
		return nil, fmt.Errorf("decode Profile schema: %w", err)
	}
	qualitySpecDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(qualitySpecSchemaBytes))
	if err != nil {
		return nil, fmt.Errorf("decode QualitySpec schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	if err := compiler.AddResource(qualitySpecSchemaURL, qualitySpecDocument); err != nil {
		return nil, fmt.Errorf("add QualitySpec schema: %w", err)
	}
	if err := compiler.AddResource(profileSchemaURL, profileDocument); err != nil {
		return nil, fmt.Errorf("add Profile schema: %w", err)
	}
	qualitySpecSchema, err := compiler.Compile(qualitySpecSchemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile QualitySpec schema: %w", err)
	}
	profileSchema, err := compiler.Compile(profileSchemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile Profile schema: %w", err)
	}
	return &Decoder{profileSchema: profileSchema, qualitySpecSchema: qualitySpecSchema}, nil
}

type ParsedQualitySpec struct {
	raw      []byte
	contract domain.Contract
	mode     domain.AccessMode
	spec     *domain.QualitySpec
}

func (record ParsedQualitySpec) AccessMode() domain.AccessMode { return record.mode }

func (record ParsedQualitySpec) CanManagedMutate() bool {
	return record.mode == domain.AccessManagedV1 && record.spec != nil
}

func (record ParsedQualitySpec) RawBytes() []byte { return bytes.Clone(record.raw) }

func (record ParsedQualitySpec) Managed() (*domain.QualitySpec, error) {
	if record.CanManagedMutate() {
		clone, err := cloneQualitySpec(*record.spec)
		if err != nil {
			return nil, err
		}
		return &clone, nil
	}
	return nil, &domain.ContractError{
		Code:    domain.CodeUnsupportedVersion,
		Path:    "contract.version",
		Value:   record.contract.Version,
		Message: "unsupported QualitySpec versions are read-only until controlled migration",
	}
}

// ParseQualitySpec applies the shared semantic resolver and the normative v1 shape schema.
func (decoder *Decoder) ParseQualitySpec(raw []byte) (ParsedQualitySpec, error) {
	record := ParsedQualitySpec{raw: bytes.Clone(raw), mode: domain.AccessReadOnly}
	contract, err := domain.ParseContractHeader(raw)
	record.contract = contract
	if err != nil {
		return record, err
	}
	if contract.Kind != domain.QualitySpecContractKind {
		return record, &domain.ContractError{Code: domain.CodeContractKind, Path: "contract.kind", Value: contract.Kind, Message: "record is not a QualitySpec contract"}
	}
	if contract.Version != domain.ContractVersionV1 {
		return record, nil
	}
	if decoder == nil || decoder.qualitySpecSchema == nil {
		return record, &domain.ContractError{Code: domain.CodeSchemaViolation, Path: "$", Message: "QualitySpec schema is unavailable"}
	}

	var spec domain.QualitySpec
	jsonDecoder := json.NewDecoder(bytes.NewReader(raw))
	jsonDecoder.UseNumber()
	jsonDecoder.DisallowUnknownFields()
	if err := jsonDecoder.Decode(&spec); err != nil {
		return record, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "$", Message: "decode exact QualitySpec v1", Err: err}
	}
	if err := domain.ValidateQualitySpec(spec); err != nil {
		return record, err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return record, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "$", Message: "decode QualitySpec JSON", Err: err}
	}
	if err := decoder.qualitySpecSchema.Validate(document); err != nil {
		return record, &domain.ContractError{Code: domain.CodeSchemaViolation, Path: "$", Message: "QualitySpec does not satisfy v1 schema", Err: err}
	}
	record.mode = domain.AccessManagedV1
	record.spec = &spec
	return record, nil
}

type ParsedProfile struct {
	raw      []byte
	contract domain.Contract
	mode     domain.AccessMode
	profile  *Profile
}

func (record ParsedProfile) AccessMode() domain.AccessMode { return record.mode }

func (record ParsedProfile) CanManagedMutate() bool {
	return record.mode == domain.AccessManagedV1 && record.profile != nil
}

func (record ParsedProfile) RawBytes() []byte { return bytes.Clone(record.raw) }

func (record ParsedProfile) Managed() (*Profile, error) {
	if record.CanManagedMutate() {
		clone, err := cloneProfile(*record.profile)
		if err != nil {
			return nil, err
		}
		return &clone, nil
	}
	return nil, &domain.ContractError{
		Code:    domain.CodeUnsupportedVersion,
		Path:    "contract.version",
		Value:   record.contract.Version,
		Message: "unsupported Profile versions are read-only until controlled migration",
	}
}

// Parse keeps unsupported versions opaque and only admits exact v1 to managed operations.
func (decoder *Decoder) Parse(raw []byte) (ParsedProfile, error) {
	record := ParsedProfile{raw: bytes.Clone(raw), mode: domain.AccessReadOnly}
	contract, err := domain.ParseContractHeader(raw)
	record.contract = contract
	if err != nil {
		return record, err
	}
	if contract.Kind != ProfileContractKind {
		return record, &domain.ContractError{Code: domain.CodeContractKind, Path: "contract.kind", Value: contract.Kind, Message: "record is not a Quality Profile contract"}
	}
	if contract.Version != domain.ContractVersionV1 {
		return record, nil
	}
	if decoder == nil || decoder.profileSchema == nil {
		return record, &domain.ContractError{Code: domain.CodeSchemaViolation, Path: "$", Message: "Profile schema is unavailable"}
	}

	var item Profile
	jsonDecoder := json.NewDecoder(bytes.NewReader(raw))
	jsonDecoder.UseNumber()
	jsonDecoder.DisallowUnknownFields()
	if err := jsonDecoder.Decode(&item); err != nil {
		return record, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "$", Message: "decode exact Profile v1", Err: err}
	}
	if err := Validate(item); err != nil {
		return record, err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return record, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "$", Message: "decode Profile JSON", Err: err}
	}
	if err := decoder.profileSchema.Validate(document); err != nil {
		return record, &domain.ContractError{Code: domain.CodeSchemaViolation, Path: "$", Message: "Profile does not satisfy v1 schema", Err: err}
	}
	record.mode = domain.AccessManagedV1
	record.profile = &item
	return record, nil
}
