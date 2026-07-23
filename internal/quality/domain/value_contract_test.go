package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"denova/internal/quality/domain"
)

func TestResolveQualitySpecSupportsEveryV1ValueType(t *testing.T) {
	tests := []struct {
		name     string
		kind     domain.ValueType
		allowed  []any
		base     any
		override any
	}{
		{"boolean", domain.ValueTypeBoolean, []any{false, true}, false, true},
		{"integer", domain.ValueTypeInteger, []any{json.Number("1"), json.Number("2")}, json.Number("1"), json.Number("2")},
		{"number", domain.ValueTypeNumber, []any{json.Number("1.5"), json.Number("2.5")}, json.Number("1.5"), json.Number("2.5")},
		{"string", domain.ValueTypeString, []any{"normal", "strict"}, "normal", "strict"},
		{"enum", domain.ValueTypeEnum, []any{"normal", "strict"}, "normal", "strict"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := loadExampleQualitySpec(t, "long_serial.json")
			spec.GoalCatalog[0].ValueContract.Type = test.kind
			spec.GoalCatalog[0].ValueContract.AllowedValues = test.allowed
			spec.Layers.ProfileDefaults[0].Value = test.base
			spec.Layers.ProjectOverrides[0].Value = test.override

			resolved, err := domain.ResolveQualitySpec(spec)
			if err != nil {
				t.Fatalf("ResolveQualitySpec(%s): %v", test.name, err)
			}
			if got := resolved.ResolvedGoals[0].Value; !reflect.DeepEqual(got, test.override) {
				t.Fatalf("resolved value = %#v, want %#v", got, test.override)
			}
		})
	}
}

func TestResolveQualitySpecRejectsFractionalInteger(t *testing.T) {
	spec := loadExampleQualitySpec(t, "long_serial.json")
	spec.GoalCatalog[0].ValueContract.Type = domain.ValueTypeInteger
	spec.GoalCatalog[0].ValueContract.AllowedValues = []any{json.Number("1"), json.Number("2")}
	spec.Layers.ProfileDefaults[0].Value = json.Number("1")
	spec.Layers.ProjectOverrides[0].Value = json.Number("1.5")

	_, err := domain.ResolveQualitySpec(spec)
	assertContractErrorLocation(t, err, domain.CodeInvalidValueType, "layers.project_overrides[0].value", "qg.serial.continuity", domain.LayerProjectOverrides)
}

func TestResolveQualitySpecAcceptsEveryJSONSchemaIntegerRepresentation(t *testing.T) {
	for _, value := range []json.Number{
		json.Number("1.0"),
		json.Number("1e3"),
		json.Number("9223372036854775808"),
	} {
		t.Run(value.String(), func(t *testing.T) {
			spec := loadExampleQualitySpec(t, "long_serial.json")
			spec.GoalCatalog[0].ValueContract.Type = domain.ValueTypeInteger
			spec.GoalCatalog[0].ValueContract.AllowedValues = []any{value}
			spec.Layers.ProfileDefaults[0].Value = value
			spec.Layers.ProjectOverrides[0].Value = value

			resolved, err := domain.ResolveQualitySpec(spec)
			if err != nil {
				t.Fatalf("ResolveQualitySpec(%q): %v", value, err)
			}
			if got := resolved.ResolvedGoals[0].Value; !reflect.DeepEqual(got, value) {
				t.Fatalf("resolved value = %#v, want %#v", got, value)
			}
		})
	}
}
