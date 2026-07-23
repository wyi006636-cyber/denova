package profile_test

import (
	"reflect"
	"testing"

	"denova/internal/quality/domain"
	"denova/internal/quality/profile"
)

func TestRegistryRejectsUnknownMissingAndDuplicateProfiles(t *testing.T) {
	decoder := newDecoder(t)
	profiles := loadCommittedProfiles(t, decoder)

	registry, err := profile.NewRegistry(profiles)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	_, err = registry.Lookup(domain.ProfileID("unknown_profile"))
	unknown := assertProfileContractError(t, err, domain.CodeUnknownProfile, "profile_id")
	if unknown.Value != "unknown_profile" {
		t.Fatalf("unknown Profile error lost value: %#v", unknown)
	}

	_, err = profile.NewRegistry(profiles[:2])
	missing := assertProfileContractError(t, err, domain.CodeRegistryIncomplete, "registry.profile_ids")
	if !reflect.DeepEqual(missing.Value, []domain.ProfileID{domain.ProfileZhihuSaltShort}) {
		t.Fatalf("missing registry value = %#v", missing.Value)
	}

	duplicate := append(append([]profile.ParsedProfile(nil), profiles...), profiles[0])
	_, err = profile.NewRegistry(duplicate)
	assertProfileContractError(t, err, domain.CodeDuplicateProfile, "registry.profile_ids")
}
