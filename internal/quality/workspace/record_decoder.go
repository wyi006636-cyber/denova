package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"denova/internal/quality/domain"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	candidateSetSchemaURL     = "https://denova.example/schemas/candidate-set-v1.schema.json"
	reviewIssueSchemaURL      = "https://denova.example/schemas/review-issue-v1.schema.json"
	preferenceMemorySchemaURL = "https://denova.example/schemas/preference-memory-v1.schema.json"
)

// RecordDecoderConfig injects normative schema bytes explicitly. Production
// parsing never resolves schemas through the process working directory.
type RecordDecoderConfig struct {
	CandidateSetSchema     []byte
	ReviewIssueSchema      []byte
	PreferenceMemorySchema []byte
}

// RecordDecoder owns the exact-v1 shape boundary for P1-T04 records.
type RecordDecoder struct {
	candidateSet     *jsonschema.Schema
	reviewIssue      *jsonschema.Schema
	preferenceMemory *jsonschema.Schema
}

func NewRecordDecoder(config RecordDecoderConfig) (*RecordDecoder, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	inputs := []struct {
		name string
		url  string
		raw  []byte
	}{
		{"CandidateSet", candidateSetSchemaURL, config.CandidateSetSchema},
		{"ReviewIssue", reviewIssueSchemaURL, config.ReviewIssueSchema},
		{"PreferenceMemory", preferenceMemorySchemaURL, config.PreferenceMemorySchema},
	}
	for _, input := range inputs {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(input.raw))
		if err != nil {
			return nil, fmt.Errorf("decode %s schema: %w", input.name, err)
		}
		if err := compiler.AddResource(input.url, document); err != nil {
			return nil, fmt.Errorf("add %s schema: %w", input.name, err)
		}
	}
	candidate, err := compiler.Compile(candidateSetSchemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile CandidateSet schema: %w", err)
	}
	issue, err := compiler.Compile(reviewIssueSchemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile ReviewIssue schema: %w", err)
	}
	preference, err := compiler.Compile(preferenceMemorySchemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile PreferenceMemory schema: %w", err)
	}
	return &RecordDecoder{candidateSet: candidate, reviewIssue: issue, preferenceMemory: preference}, nil
}

type ParsedCandidateSet struct {
	raw      []byte
	contract domain.Contract
	mode     domain.AccessMode
	record   *domain.CandidateSet
}

func (record ParsedCandidateSet) AccessMode() domain.AccessMode { return record.mode }
func (record ParsedCandidateSet) CanManagedMutate() bool {
	return record.mode == domain.AccessManagedV1 && record.record != nil
}
func (record ParsedCandidateSet) RawBytes() []byte { return bytes.Clone(record.raw) }
func (record ParsedCandidateSet) Managed() (*domain.CandidateSet, error) {
	if !record.CanManagedMutate() {
		return nil, unsupportedRecordError(record.contract.Version, "CandidateSet")
	}
	return cloneJSON(record.record)
}

func (decoder *RecordDecoder) ParseCandidateSet(raw []byte) (ParsedCandidateSet, error) {
	parsed := ParsedCandidateSet{raw: bytes.Clone(raw), mode: domain.AccessReadOnly}
	contract, err := parseRecordHeader(raw, domain.CandidateSetContractKind)
	parsed.contract = contract
	if err != nil || contract.Version != domain.ContractVersionV1 {
		return parsed, err
	}
	var record domain.CandidateSet
	if err := decoder.parseExactV1(raw, decoder.candidateSet, "CandidateSet", &record); err != nil {
		return parsed, err
	}
	parsed.mode = domain.AccessManagedV1
	parsed.record = &record
	return parsed, nil
}

type ParsedReviewIssue struct {
	raw      []byte
	contract domain.Contract
	mode     domain.AccessMode
	record   *domain.ReviewIssue
}

func (record ParsedReviewIssue) AccessMode() domain.AccessMode { return record.mode }
func (record ParsedReviewIssue) CanManagedMutate() bool {
	return record.mode == domain.AccessManagedV1 && record.record != nil
}
func (record ParsedReviewIssue) RawBytes() []byte { return bytes.Clone(record.raw) }
func (record ParsedReviewIssue) Managed() (*domain.ReviewIssue, error) {
	if !record.CanManagedMutate() {
		return nil, unsupportedRecordError(record.contract.Version, "ReviewIssue")
	}
	return cloneJSON(record.record)
}

func (decoder *RecordDecoder) ParseReviewIssue(raw []byte) (ParsedReviewIssue, error) {
	parsed := ParsedReviewIssue{raw: bytes.Clone(raw), mode: domain.AccessReadOnly}
	contract, err := parseRecordHeader(raw, domain.ReviewIssueContractKind)
	parsed.contract = contract
	if err != nil || contract.Version != domain.ContractVersionV1 {
		return parsed, err
	}
	var record domain.ReviewIssue
	if err := decoder.parseExactV1(raw, decoder.reviewIssue, "ReviewIssue", &record); err != nil {
		return parsed, err
	}
	parsed.mode = domain.AccessManagedV1
	parsed.record = &record
	return parsed, nil
}

type ParsedPreferenceSignal struct {
	raw      []byte
	contract domain.Contract
	mode     domain.AccessMode
	record   *domain.PreferenceSignal
}

func (record ParsedPreferenceSignal) AccessMode() domain.AccessMode { return record.mode }
func (record ParsedPreferenceSignal) CanManagedMutate() bool {
	return record.mode == domain.AccessManagedV1 && record.record != nil
}
func (record ParsedPreferenceSignal) RawBytes() []byte { return bytes.Clone(record.raw) }
func (record ParsedPreferenceSignal) Managed() (*domain.PreferenceSignal, error) {
	if !record.CanManagedMutate() {
		return nil, unsupportedRecordError(record.contract.Version, "PreferenceSignal")
	}
	return cloneJSON(record.record)
}

func (decoder *RecordDecoder) ParsePreferenceSignal(raw []byte) (ParsedPreferenceSignal, error) {
	parsed := ParsedPreferenceSignal{raw: bytes.Clone(raw), mode: domain.AccessReadOnly}
	contract, err := parseRecordHeader(raw, domain.PreferenceSignalContractKind)
	parsed.contract = contract
	if err != nil || contract.Version != domain.ContractVersionV1 {
		return parsed, err
	}
	var record domain.PreferenceSignal
	if err := decoder.parseExactV1(raw, decoder.preferenceMemory, "PreferenceSignal", &record); err != nil {
		return parsed, err
	}
	parsed.mode = domain.AccessManagedV1
	parsed.record = &record
	return parsed, nil
}

func parseRecordHeader(raw []byte, wantKind string) (domain.Contract, error) {
	contract, err := domain.ParseContractHeader(raw)
	if err != nil {
		return contract, err
	}
	if contract.Kind != wantKind {
		return contract, &domain.ContractError{Code: domain.CodeContractKind, Path: "contract.kind", Value: contract.Kind, Message: "record contract kind does not match parser"}
	}
	return contract, nil
}

func (decoder *RecordDecoder) parseExactV1(raw []byte, schema *jsonschema.Schema, name string, target any) error {
	if decoder == nil || schema == nil {
		return &domain.ContractError{Code: domain.CodeSchemaViolation, Path: "$", Message: name + " schema is unavailable"}
	}
	if duplicate, err := validateMarkerJSON(raw); err != nil {
		path := "$"
		if duplicate != "" {
			path = duplicate
		}
		return &domain.ContractError{Code: domain.CodeMalformedRecord, Path: path, Message: "record is not unambiguous valid JSON", Err: err}
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "$", Message: "decode " + name + " JSON", Err: err}
	}
	if err := schema.Validate(document); err != nil {
		return &domain.ContractError{Code: domain.CodeSchemaViolation, Path: validationErrorPath(err), Message: name + " does not satisfy exact v1 schema", Err: err}
	}
	jsonDecoder := json.NewDecoder(bytes.NewReader(raw))
	jsonDecoder.DisallowUnknownFields()
	jsonDecoder.UseNumber()
	if err := jsonDecoder.Decode(target); err != nil {
		return &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "$", Message: "decode exact " + name + " v1", Err: err}
	}
	var trailing any
	if err := jsonDecoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "$", Message: "decode exact " + name + " v1", Err: err}
	}
	return nil
}

func validationErrorPath(err error) string {
	var validationErr *jsonschema.ValidationError
	if !errors.As(err, &validationErr) {
		return "$"
	}
	output := validationErr.BasicOutput()
	for _, item := range output.Errors {
		if item.InstanceLocation != "" {
			return "$" + item.InstanceLocation
		}
	}
	return "$"
}

func unsupportedRecordError(version, name string) error {
	return &domain.ContractError{Code: domain.CodeUnsupportedVersion, Path: "contract.version", Value: version, Message: name + " version is read-only until an accepted migration exists"}
}

func cloneJSON[T any](value *T) (*T, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "$", Message: "clone managed record", Err: err}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var clone T
	if err := decoder.Decode(&clone); err != nil {
		return nil, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "$", Message: "clone managed record", Err: err}
	}
	return &clone, nil
}
