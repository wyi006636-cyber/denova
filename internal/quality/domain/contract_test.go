package domain_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"denova/internal/quality/domain"
)

func TestProfileIDsAreExhaustiveAndUnknownIDsFailExplicitly(t *testing.T) {
	want := []domain.ProfileID{
		domain.ProfileLongSerial,
		domain.ProfileFanqieShort,
		domain.ProfileZhihuSaltShort,
	}
	if got := domain.AllProfileIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllProfileIDs() = %#v, want %#v", got, want)
	}

	for _, id := range want {
		got, err := domain.ParseProfileID(string(id))
		if err != nil {
			t.Fatalf("ParseProfileID(%q): %v", id, err)
		}
		if got != id {
			t.Fatalf("ParseProfileID(%q) = %q", id, got)
		}
	}

	_, err := domain.ParseProfileID("other_profile")
	assertContractError(t, err, domain.CodeUnknownProfile, "profile_id", "other_profile")
}

func contractBytes(t *testing.T, name string) []byte {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "docs", "project-design", "implementation", "contracts", name)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract %s: %v", path, err)
	}
	return payload
}

func assertContractError(t *testing.T, err error, code domain.ErrorCode, path string, value any) *domain.ContractError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected contract error %q", code)
	}
	var contractErr *domain.ContractError
	if !errors.As(err, &contractErr) {
		t.Fatalf("error %T is not *domain.ContractError: %v", err, err)
	}
	if contractErr.Code != code || contractErr.Path != path || !reflect.DeepEqual(contractErr.Value, value) {
		t.Fatalf("contract error = %#v, want code=%q path=%q value=%#v", contractErr, code, path, value)
	}
	return contractErr
}
