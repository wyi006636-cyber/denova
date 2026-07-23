package profile_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"denova/internal/quality/domain"
	"denova/internal/quality/profile"
)

func TestCommittedProfilesPassDraft202012AndSemanticValidation(t *testing.T) {
	decoder := newDecoder(t)
	profiles := loadCommittedProfiles(t, decoder)
	registry, err := profile.NewRegistry(profiles)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	wantIDs := domain.AllProfileIDs()
	if got := registry.IDs(); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("registry IDs = %#v, want %#v", got, wantIDs)
	}
	for _, id := range wantIDs {
		item, err := registry.Lookup(id)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", id, err)
		}
		if item.ProfileID != id || item.Contract.Version != domain.ContractVersionV1 {
			t.Fatalf("Lookup(%q) returned wrong identity/version: %#v", id, item)
		}
		if item.EngineContract.EngineID != profile.SharedEngineID ||
			item.EngineContract.ContractVersion != domain.ContractVersionV1 ||
			item.EngineContract.ImplementationBranching != profile.ProfileDataOnly {
			t.Fatalf("Lookup(%q) returned non-shared engine: %#v", id, item.EngineContract)
		}
	}
}

func TestProfileDecoderPreservesUnsupportedVersionsAsReadOnly(t *testing.T) {
	decoder := newDecoder(t)
	for _, version := range []string{"v2", "future-version"} {
		t.Run(version, func(t *testing.T) {
			raw := []byte("{\n  \"contract\": {\"kind\": \"denova.quality-profile\", \"version\": \"" + version + "\", \"issued_at\": \"2030-01-01T00:00:00Z\"},\n  \"future_profile\": true\n}\n")
			record, err := decoder.Parse(raw)
			if err != nil {
				t.Fatalf("Parse unsupported Profile: %v", err)
			}
			if record.AccessMode() != domain.AccessReadOnly || record.CanManagedMutate() {
				t.Fatalf("unsupported Profile access = %q managed=%v", record.AccessMode(), record.CanManagedMutate())
			}
			if got := record.RawBytes(); !bytes.Equal(got, raw) {
				t.Fatalf("RawBytes() changed input:\n got: %q\nwant: %q", got, raw)
			}
			managed, err := record.Managed()
			if managed != nil {
				t.Fatalf("Managed returned unsupported Profile: %#v", managed)
			}
			assertProfileContractError(t, err, domain.CodeUnsupportedVersion, "contract.version")
		})
	}
}

func TestQualitySpecDecoderPreservesUnsupportedVersionsAsReadOnly(t *testing.T) {
	decoder := newDecoder(t)
	for _, version := range []string{"v2", "future-version"} {
		t.Run(version, func(t *testing.T) {
			raw := []byte("{\n  \"contract\": {\"kind\": \"denova.quality-spec\", \"version\": \"" + version + "\", \"issued_at\": \"2030-01-01T00:00:00Z\"},\n  \"future_field\": {\"must_stay\": true}\n}\n")
			record, err := decoder.ParseQualitySpec(raw)
			if err != nil {
				t.Fatalf("Parse unsupported QualitySpec: %v", err)
			}
			if record.AccessMode() != domain.AccessReadOnly || record.CanManagedMutate() {
				t.Fatalf("unsupported QualitySpec access = %q managed=%v", record.AccessMode(), record.CanManagedMutate())
			}
			if got := record.RawBytes(); !bytes.Equal(got, raw) {
				t.Fatalf("RawBytes() changed input:\n got: %q\nwant: %q", got, raw)
			}
			managed, err := record.Managed()
			if managed != nil {
				t.Fatalf("Managed returned unsupported QualitySpec: %#v", managed)
			}
			assertProfileContractError(t, err, domain.CodeUnsupportedVersion, "contract.version")
		})
	}
}

func TestManagedV1RecordsRoundTripThroughNormativeSchemas(t *testing.T) {
	decoder := newDecoder(t)
	profileRecord, err := decoder.Parse(profileExampleBytes(t, "long_serial.json"))
	if err != nil {
		t.Fatalf("Parse Profile: %v", err)
	}
	managedProfile, err := profileRecord.Managed()
	if err != nil {
		t.Fatalf("Managed Profile: %v", err)
	}
	profileJSON, err := json.Marshal(managedProfile)
	if err != nil {
		t.Fatalf("marshal managed Profile: %v", err)
	}
	if _, err := decoder.Parse(profileJSON); err != nil {
		t.Fatalf("reparse marshaled Profile: %v", err)
	}

	qualitySpecJSON, err := json.Marshal(managedProfile.QualitySpec)
	if err != nil {
		t.Fatalf("marshal managed QualitySpec: %v", err)
	}
	if _, err := decoder.ParseQualitySpec(qualitySpecJSON); err != nil {
		t.Fatalf("reparse marshaled QualitySpec: %v", err)
	}

	registry, err := profile.NewRegistry([]profile.ParsedProfile{profileRecord})
	if err == nil {
		t.Fatal("one-record registry unexpectedly satisfied the exhaustive registry contract")
	}
	profiles := loadCommittedProfiles(t, decoder)
	registry, err = profile.NewRegistry(profiles)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	lookup, err := registry.Lookup(domain.ProfileLongSerial)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	lookupJSON, err := json.Marshal(lookup)
	if err != nil {
		t.Fatalf("marshal registry Profile: %v", err)
	}
	if _, err := decoder.Parse(lookupJSON); err != nil {
		t.Fatalf("reparse registry Profile: %v", err)
	}
}

func TestParsedRecordsAndRegistryReturnIsolatedSnapshots(t *testing.T) {
	decoder := newDecoder(t)
	profileRecord, err := decoder.Parse(profileExampleBytes(t, "long_serial.json"))
	if err != nil {
		t.Fatalf("Parse Profile: %v", err)
	}
	firstProfile, err := profileRecord.Managed()
	if err != nil {
		t.Fatalf("first Managed Profile: %v", err)
	}
	firstProfile.DisplayName.En = ""
	firstProfile.QualitySpec.Revision = 0
	firstProfile.QualitySpec.CandidateChanges[0].ProposedBy = "author"
	secondProfile, err := profileRecord.Managed()
	if err != nil {
		t.Fatalf("second Managed Profile: %v", err)
	}
	if secondProfile.DisplayName.En == "" || secondProfile.QualitySpec.Revision == 0 || secondProfile.QualitySpec.CandidateChanges[0].ProposedBy != "model" {
		t.Fatalf("ParsedProfile leaked a mutable alias: %#v", secondProfile)
	}

	var envelope struct {
		QualitySpec json.RawMessage `json:"quality_spec"`
	}
	if err := json.Unmarshal(profileExampleBytes(t, "long_serial.json"), &envelope); err != nil {
		t.Fatalf("decode QualitySpec envelope: %v", err)
	}
	specRecord, err := decoder.ParseQualitySpec(envelope.QualitySpec)
	if err != nil {
		t.Fatalf("Parse QualitySpec: %v", err)
	}
	firstSpec, err := specRecord.Managed()
	if err != nil {
		t.Fatalf("first Managed QualitySpec: %v", err)
	}
	firstSpec.Revision = 0
	secondSpec, err := specRecord.Managed()
	if err != nil {
		t.Fatalf("second Managed QualitySpec: %v", err)
	}
	if secondSpec.Revision == 0 {
		t.Fatal("ParsedQualitySpec leaked a mutable alias")
	}

	profiles := loadCommittedProfiles(t, decoder)
	profiles[0] = profileRecord
	registry, err := profile.NewRegistry(profiles)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	lookup, err := registry.Lookup(domain.ProfileLongSerial)
	if err != nil {
		t.Fatalf("first Lookup: %v", err)
	}
	lookup.DisplayName.En = ""
	lookup.QualitySpec.Revision = 0
	lookupAgain, err := registry.Lookup(domain.ProfileLongSerial)
	if err != nil {
		t.Fatalf("second Lookup: %v", err)
	}
	if lookupAgain.DisplayName.En == "" || lookupAgain.QualitySpec.Revision == 0 {
		t.Fatalf("Registry Lookup leaked a mutable alias: %#v", lookupAgain)
	}
	ids := registry.IDs()
	ids[0] = domain.ProfileZhihuSaltShort
	if got := registry.IDs(); !reflect.DeepEqual(got, domain.AllProfileIDs()) {
		t.Fatalf("Registry IDs leaked mutable state: %#v", got)
	}
}

func TestExactV1DecodersRejectClosedSchemaViolations(t *testing.T) {
	decoder := newDecoder(t)
	tests := []struct {
		name  string
		parse func([]byte) error
		raw   []byte
		code  domain.ErrorCode
	}{
		{
			name: "Profile invalid date-time format",
			parse: func(raw []byte) error {
				_, err := decoder.Parse(raw)
				return err
			},
			raw: mutateJSON(t, profileExampleBytes(t, "long_serial.json"), func(document map[string]any) {
				document["contract"].(map[string]any)["issued_at"] = "not-a-date-time"
			}),
			code: domain.CodeSchemaViolation,
		},
		{
			name: "QualitySpec closed priority enum",
			parse: func(raw []byte) error {
				_, err := decoder.ParseQualitySpec(raw)
				return err
			},
			raw: mutateQualitySpecJSON(t, "long_serial.json", func(document map[string]any) {
				document["goal_catalog"].([]any)[0].(map[string]any)["priority"] = "critical"
			}),
			code: domain.CodeSchemaViolation,
		},
		{
			name: "Profile additional property",
			parse: func(raw []byte) error {
				_, err := decoder.Parse(raw)
				return err
			},
			raw: mutateJSON(t, profileExampleBytes(t, "long_serial.json"), func(document map[string]any) {
				document["unexpected"] = true
			}),
			code: domain.CodeMalformedRecord,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertProfileContractError(t, test.parse(test.raw), test.code, "$")
		})
	}
}

func TestProfileSchemaRejectsModelProposalInAuthoritativeProvenance(t *testing.T) {
	decoder := newDecoder(t)
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "Profile provenance",
			mutate: func(document map[string]any) {
				document["profile_provenance"].(map[string]any)["source_kind"] = "model_proposal"
			},
		},
		{
			name: "mutable setting provenance",
			mutate: func(document map[string]any) {
				settings := document["settings"].(map[string]any)
				setting := settings["required_artifacts"].([]any)[0].(map[string]any)
				setting["provenance"].(map[string]any)["source_kind"] = "model_proposal"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := mutateJSON(t, profileExampleBytes(t, "long_serial.json"), test.mutate)
			_, err := decoder.Parse(raw)
			assertProfileContractError(t, err, domain.CodeSchemaViolation, "$")
		})
	}
}

func mutateQualitySpecJSON(t *testing.T, name string, mutate func(map[string]any)) []byte {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(profileExampleBytes(t, name), &envelope); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	qualitySpec := envelope["quality_spec"].(map[string]any)
	mutate(qualitySpec)
	payload, err := json.Marshal(qualitySpec)
	if err != nil {
		t.Fatalf("encode mutated QualitySpec: %v", err)
	}
	return payload
}

func mutateJSON(t *testing.T, raw []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode JSON for mutation: %v", err)
	}
	mutate(document)
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode mutated JSON: %v", err)
	}
	return payload
}

func newDecoder(t *testing.T) *profile.Decoder {
	t.Helper()
	decoder, err := profile.NewDecoder(
		profileContractBytes(t, "profile-v1.schema.json"),
		profileContractBytes(t, "quality-spec-v1.schema.json"),
	)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	return decoder
}

func loadCommittedProfiles(t *testing.T, decoder *profile.Decoder) []profile.ParsedProfile {
	t.Helper()
	result := make([]profile.ParsedProfile, 0, 3)
	for _, name := range []string{"long_serial.json", "fanqie_short.json", "zhihu_salt_short.json"} {
		record, err := decoder.Parse(profileExampleBytes(t, name))
		if err != nil {
			t.Fatalf("Parse(%s): %v", name, err)
		}
		if _, err := record.Managed(); err != nil {
			t.Fatalf("Managed(%s): %v", name, err)
		}
		result = append(result, record)
	}
	return result
}

func profileContractBytes(t *testing.T, name string) []byte {
	t.Helper()
	return readRepositoryFile(t, "docs", "project-design", "implementation", "contracts", name)
}

func profileExampleBytes(t *testing.T, name string) []byte {
	t.Helper()
	return readRepositoryFile(t, "docs", "project-design", "implementation", "contracts", "examples", name)
}

func readRepositoryFile(t *testing.T, elements ...string) []byte {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	root := filepath.Join(filepath.Dir(currentFile), "..", "..", "..")
	path := filepath.Join(append([]string{root}, elements...)...)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return payload
}

func assertProfileContractError(t *testing.T, err error, code domain.ErrorCode, path string) *domain.ContractError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected contract error %q", code)
	}
	var contractErr *domain.ContractError
	if !errors.As(err, &contractErr) {
		t.Fatalf("error %T is not *domain.ContractError: %v", err, err)
	}
	if contractErr.Code != code || contractErr.Path != path {
		t.Fatalf("contract error = %#v, want code=%q path=%q", contractErr, code, path)
	}
	return contractErr
}
