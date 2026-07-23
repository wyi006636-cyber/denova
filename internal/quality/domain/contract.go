package domain

import (
	"encoding/json"
)

const (
	QualitySpecContractKind = "denova.quality-spec"
	ContractVersionV1       = "v1"
)

// Layer is one QualitySpec v1 resolution layer.
type Layer string

// AccessMode separates exact-v1 managed records from preserved unsupported input.
type AccessMode string

const (
	AccessManagedV1 AccessMode = "managed_v1"
	AccessReadOnly  AccessMode = "read_only"
)

// Contract identifies one versioned contract record.
type Contract struct {
	Kind     string `json:"kind"`
	Version  string `json:"version"`
	IssuedAt string `json:"issued_at"`
}

// ParseContractHeader reads only the version envelope so unsupported records can stay opaque.
func ParseContractHeader(raw []byte) (Contract, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return Contract{}, &ContractError{Code: CodeMalformedRecord, Path: "$", Message: "decode contract record", Err: err}
	}
	contractRaw, ok := root["contract"]
	if !ok {
		return Contract{}, &ContractError{Code: CodeMalformedRecord, Path: "contract", Message: "contract is required"}
	}
	var contract Contract
	if err := json.Unmarshal(contractRaw, &contract); err != nil {
		return Contract{}, &ContractError{Code: CodeMalformedRecord, Path: "contract", Message: "decode contract header", Err: err}
	}
	if contract.Kind == "" {
		return contract, &ContractError{Code: CodeMalformedRecord, Path: "contract.kind", Message: "contract kind is required"}
	}
	if contract.Version == "" {
		return contract, &ContractError{Code: CodeMalformedRecord, Path: "contract.version", Message: "contract version is required"}
	}
	return contract, nil
}
