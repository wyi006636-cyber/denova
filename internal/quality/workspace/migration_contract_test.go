package workspace

import (
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildMigrationAuthorizationBindsCompletePreviewAndAuthorEvidence(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, "ideas.md", "first idea\n")
	preview, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
	if err != nil {
		t.Fatal(err)
	}

	confirmation := AuthorConfirmation{
		ID:       "author-confirmation-01",
		Evidence: "author explicitly approved preview 01",
	}
	authorization, err := BuildMigrationAuthorization(preview, "migration-01", confirmation)
	if err != nil {
		t.Fatal(err)
	}

	if authorization.Version != MigrationAuthorizationVersionV1 {
		t.Fatalf("authorization version = %d, want %d", authorization.Version, MigrationAuthorizationVersionV1)
	}
	if authorization.MigrationID != "migration-01" {
		t.Fatalf("migration id = %q", authorization.MigrationID)
	}
	if authorization.CanonicalWorkspace != preview.Workspace {
		t.Fatalf("workspace = %q, want %q", authorization.CanonicalWorkspace, preview.Workspace)
	}
	assertSHA256String(t, authorization.PreviewSHA256)
	assertSHA256String(t, authorization.PayloadSHA256)
	if authorization.TargetSchemaVersion != 1 {
		t.Fatalf("target schema = %d, want 1", authorization.TargetSchemaVersion)
	}
	if !reflect.DeepEqual(authorization.TargetFeatures, preview.Features) {
		t.Fatalf("target features = %#v, want %#v", authorization.TargetFeatures, preview.Features)
	}
	if len(authorization.Sources) == 0 {
		t.Fatal("authorization must bind the complete source snapshot")
	}
	if authorization.Confirmation != confirmation {
		t.Fatalf("confirmation = %#v, want %#v", authorization.Confirmation, confirmation)
	}

	changed := confirmation
	changed.Evidence += " changed"
	changedAuthorization, err := BuildMigrationAuthorization(preview, "migration-01", changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedAuthorization.PayloadSHA256 == authorization.PayloadSHA256 {
		t.Fatal("changing confirmation evidence must change the bound payload hash")
	}
	changedPreview := preview
	changedPreview.Features = append([]PreviewFeature(nil), preview.Features...)
	changedPreview.Features[0].Version = "1.1.1"
	changedAuthorization, err = BuildMigrationAuthorization(changedPreview, "migration-01", confirmation)
	if err != nil {
		t.Fatal(err)
	}
	if changedAuthorization.PreviewSHA256 == authorization.PreviewSHA256 {
		t.Fatal("changing any public preview field must change the preview digest")
	}
}

func TestBuildMigrationAuthorizationRejectsMissingConfirmation(t *testing.T) {
	workspace := t.TempDir()
	preview, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		confirmation AuthorConfirmation
	}{
		{name: "missing id", confirmation: AuthorConfirmation{Evidence: "approved"}},
		{name: "missing evidence", confirmation: AuthorConfirmation{ID: "confirmation-01"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildMigrationAuthorization(preview, "migration-01", test.confirmation)
			assertMigrationError(t, err, CodeMigrationAuthorizationRequired, MigrationStepAuthorize)
		})
	}
}

func TestValidateMigrationIDRejectsUnsafePortableSegments(t *testing.T) {
	tests := []string{
		"",
		".",
		"..",
		"a/b",
		`a\b`,
		"/absolute",
		`C:\drive`,
		`\\server\share`,
		"nul\x00byte",
		"CON",
		"nul.json",
		"trailing.",
		"trailing ",
		"stream:name",
		"decomposed-e\u0301",
		strings.Repeat("x", MaxMigrationIDBytes+1),
	}
	for _, migrationID := range tests {
		t.Run(filepath.ToSlash(strings.ReplaceAll(migrationID, "\x00", "NUL")), func(t *testing.T) {
			if err := ValidateMigrationID(migrationID); err == nil {
				t.Fatalf("ValidateMigrationID(%q) succeeded", migrationID)
			}
		})
	}

	for _, migrationID := range []string{"migration-01", "迁移-甲", "draft 01"} {
		if err := ValidateMigrationID(migrationID); err != nil {
			t.Fatalf("ValidateMigrationID(%q): %v", migrationID, err)
		}
	}
}

func TestMigrationRecordStrictRoundTripAndExhaustiveNextAction(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "idea\n")
	preview, err := BuildMigrationPreview(workspace, schemaV1PreviewOptions())
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := BuildMigrationAuthorization(preview, "migration-01", AuthorConfirmation{ID: "confirmation-01", Evidence: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := newMigrationRecord(preview, authorization)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encodeMigrationRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMigrationRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, record) {
		t.Fatalf("decoded record differs\n got: %#v\nwant: %#v", decoded, record)
	}

	states := AllMigrationStates()
	if len(states) != 12 {
		t.Fatalf("migration states = %d, want 12", len(states))
	}
	for _, state := range states {
		next, err := nextActionForState(state)
		if err != nil {
			t.Fatalf("state %q: %v", state, err)
		}
		if next == "" {
			t.Fatalf("state %q returned an empty next action", state)
		}
	}
	if _, err := nextActionForState(MigrationState("future_state")); err == nil {
		t.Fatal("unknown migration state must be rejected")
	}
}

func TestMigrationRecordCodecRejectsAmbiguousOrUnknownBytes(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "duplicate", raw: []byte(`{"record_version":1,"record_version":1}`)},
		{name: "unknown version", raw: []byte(`{"record_version":2,"migration_id":"migration-01"}`)},
		{name: "invalid utf8", raw: []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}},
		{name: "trailing value", raw: []byte(`{"record_version":1} {}`)},
		{name: "malformed", raw: []byte(`{"record_version":`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeMigrationRecord(test.raw)
			assertMigrationError(t, err, CodeMigrationRecordInvalid, MigrationStepLoadRecord)
		})
	}
}

func TestMigrationRecordRejectsStatePayloadThatSkipsDurableBoundaries(t *testing.T) {
	workspace := t.TempDir()
	writeWorkspaceTestFile(t, workspace, ".denova/ideas.md", "idea\n")
	preview, authorization := newMigrationTestRequest(t, workspace, "migration-01")
	base, err := newMigrationRecord(preview, authorization)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*MigrationRecord)
	}{
		{name: "backed up without manifest", mutate: func(record *MigrationRecord) {
			record.State = MigrationBackedUp
			record.NextAction = MigrationNextStage
		}},
		{name: "staged without stage", mutate: func(record *MigrationRecord) {
			record.State = MigrationStaged
			record.NextAction = MigrationNextPrepareSwitch
			record.Backup = &MigrationArtifactRef{RelativePath: backupManifestRelativePath(record.MigrationID), SHA256: strings.Repeat("a", 64)}
		}},
		{name: "switch pending without intent", mutate: func(record *MigrationRecord) {
			record.State = MigrationSwitchPending
			record.NextAction = MigrationNextSwitch
			record.Backup = &MigrationArtifactRef{RelativePath: backupManifestRelativePath(record.MigrationID), SHA256: strings.Repeat("a", 64)}
			record.Stage = &MigrationArtifactRef{RelativePath: stageManifestRelativePath(record.MigrationID), SHA256: strings.Repeat("b", 64)}
		}},
		{name: "completed without terminal receipt", mutate: func(record *MigrationRecord) {
			record.State = MigrationCompleted
			record.NextAction = MigrationNextNone
		}},
		{name: "rollback pending without author recovery binding", mutate: func(record *MigrationRecord) {
			record.State = MigrationRollbackPending
			record.NextAction = MigrationNextRollback
			record.RollbackFromState = MigrationStaged
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := base
			test.mutate(&record)
			if _, err := encodeMigrationRecord(record); err == nil {
				t.Fatalf("state payload was accepted: %#v", record)
			}
		})
	}
}

func TestMigrationErrorCarriesRecoveryEvidence(t *testing.T) {
	cause := errors.New("disk failure")
	err := &MigrationError{
		Code:             CodeMigrationDurability,
		MigrationID:      "migration-01",
		State:            MigrationBackedUp,
		Step:             MigrationStepBackup,
		Path:             ".denova-migration/migration-01/backup/manifest.json",
		ExpectedSHA256:   strings.Repeat("a", 64),
		ActualSHA256:     strings.Repeat("b", 64),
		WorkspaceMutated: true,
		Durability:       DurabilityPending,
		Recovery:         RecoveryAvailable,
		NextAction:       MigrationNextResume,
		Message:          "backup manifest publication failed",
		Err:              cause,
	}
	if !errors.Is(err, cause) {
		t.Fatal("MigrationError must unwrap its cause")
	}
	message := err.Error()
	for _, expected := range []string{"migration-01", "backed_up", "backup", "workspace_mutated=true", "resume"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("error %q does not contain %q", message, expected)
		}
	}
}

func assertSHA256String(t *testing.T, value string) {
	t.Helper()
	if len(value) != 64 {
		t.Fatalf("SHA-256 = %q, want 64 hex characters", value)
	}
	if _, err := hex.DecodeString(value); err != nil {
		t.Fatalf("SHA-256 = %q: %v", value, err)
	}
}

func assertMigrationError(t *testing.T, err error, code ErrorCode, step MigrationStep) *MigrationError {
	t.Helper()
	var migrationErr *MigrationError
	if !errors.As(err, &migrationErr) {
		t.Fatalf("error = %T %v, want *MigrationError", err, err)
	}
	if migrationErr.Code != code {
		t.Fatalf("error code = %q, want %q", migrationErr.Code, code)
	}
	if migrationErr.Step != step {
		t.Fatalf("error step = %q, want %q", migrationErr.Step, step)
	}
	return migrationErr
}
