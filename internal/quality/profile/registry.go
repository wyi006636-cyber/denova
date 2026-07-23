package profile

import (
	"bytes"
	"encoding/json"

	"denova/internal/quality/domain"
)

// Registry is an exhaustive, queryable v1 Profile set with no default Profile.
type Registry struct {
	profiles map[domain.ProfileID]Profile
}

func NewRegistry(records []ParsedProfile) (*Registry, error) {
	profiles := make(map[domain.ProfileID]Profile, len(records))
	for _, record := range records {
		managed, err := record.Managed()
		if err != nil {
			return nil, err
		}
		item := *managed
		if err := Validate(item); err != nil {
			return nil, err
		}
		if _, exists := profiles[item.ProfileID]; exists {
			return nil, &domain.ContractError{Code: domain.CodeDuplicateProfile, Path: "registry.profile_ids", Value: item.ProfileID, Message: "Profile registry IDs must be unique"}
		}
		clone, err := cloneProfile(item)
		if err != nil {
			return nil, err
		}
		profiles[item.ProfileID] = clone
	}

	missing := make([]domain.ProfileID, 0, 3)
	for _, id := range domain.AllProfileIDs() {
		if _, ok := profiles[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) != 0 {
		return nil, &domain.ContractError{Code: domain.CodeRegistryIncomplete, Path: "registry.profile_ids", Value: missing, Message: "Profile v1 registry must contain every exhaustive ID"}
	}
	return &Registry{profiles: profiles}, nil
}

func (registry *Registry) IDs() []domain.ProfileID {
	if registry == nil {
		return nil
	}
	return domain.AllProfileIDs()
}

func (registry *Registry) Lookup(id domain.ProfileID) (Profile, error) {
	if _, err := domain.ParseProfileID(string(id)); err != nil {
		return Profile{}, err
	}
	if registry == nil {
		return Profile{}, &domain.ContractError{Code: domain.CodeRegistryIncomplete, Path: "registry.profile_ids", Value: []domain.ProfileID{id}, Message: "Profile registry is unavailable"}
	}
	item, ok := registry.profiles[id]
	if !ok {
		return Profile{}, &domain.ContractError{Code: domain.CodeRegistryIncomplete, Path: "registry.profile_ids", Value: []domain.ProfileID{id}, Message: "Profile registry is incomplete"}
	}
	return cloneProfile(item)
}

func cloneProfile(item Profile) (Profile, error) {
	payload, err := json.Marshal(item)
	if err != nil {
		return Profile{}, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "registry", Message: "encode Profile registry value", Err: err}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var clone Profile
	if err := decoder.Decode(&clone); err != nil {
		return Profile{}, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "registry", Message: "clone Profile registry value", Err: err}
	}
	return clone, nil
}

func cloneQualitySpec(item domain.QualitySpec) (domain.QualitySpec, error) {
	payload, err := json.Marshal(item)
	if err != nil {
		return domain.QualitySpec{}, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "quality_spec", Message: "encode managed QualitySpec snapshot", Err: err}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var clone domain.QualitySpec
	if err := decoder.Decode(&clone); err != nil {
		return domain.QualitySpec{}, &domain.ContractError{Code: domain.CodeMalformedRecord, Path: "quality_spec", Message: "clone managed QualitySpec snapshot", Err: err}
	}
	return clone, nil
}
