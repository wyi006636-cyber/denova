package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"denova/internal/quality/domain"
)

func TestPreferenceRepositoryAppendsCompleteLinesPreservesPrefixAndResolves(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	references := preferenceRepositoryReferences()
	provider := &repositoryReferenceProvider{preference: references}
	repository := newPreferenceRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})

	selection := preferenceRepositoryRaw(t, references)
	first, err := repository.Append(context.Background(), selection, PreferenceAppendExpectation{PriorRawSHA256: recordSHA256(nil)})
	if err != nil {
		t.Fatalf("append selection: %v", err)
	}
	wantPrefix := append(append([]byte(nil), selection...), '\n')
	if !bytes.Equal(first.RawBytes(), wantPrefix) || first.RawSHA256 != recordSHA256(wantPrefix) || len(first.Entries) != 1 {
		t.Fatalf("first journal snapshot=%#v raw=%q", first, first.RawBytes())
	}

	correction := preferenceMaintenanceRaw(t, selection, "preference-signal-002", "correction", "preference-signal-001", references)
	second, err := repository.Append(context.Background(), correction, PreferenceAppendExpectation{PriorRawSHA256: first.RawSHA256})
	if err != nil {
		t.Fatalf("append correction: %v", err)
	}
	if !bytes.HasPrefix(second.RawBytes(), first.RawBytes()) || len(second.Entries) != 2 {
		t.Fatalf("correction rewrote journal prefix: first=%q second=%q", first.RawBytes(), second.RawBytes())
	}

	revocation := preferenceMaintenanceRaw(t, selection, "preference-signal-003", "revocation", "preference-signal-002", references)
	third, err := repository.Append(context.Background(), revocation, PreferenceAppendExpectation{PriorRawSHA256: second.RawSHA256})
	if err != nil {
		t.Fatalf("append revocation: %v", err)
	}
	if !bytes.HasPrefix(third.RawBytes(), second.RawBytes()) || len(third.Entries) != 3 {
		t.Fatalf("revocation rewrote journal prefix: second=%q third=%q", second.RawBytes(), third.RawBytes())
	}
	resolution, err := repository.Resolve(context.Background(), domain.PreferenceQuery{
		AuthorID: "author-001", ProjectID: "project-001", WorkspaceID: "workspace-001", Dimension: "opening_dialogue",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolution.Effective != nil || resolution.Reason != "no_applicable_explicit_author_signal" {
		t.Fatalf("resolution=%#v", resolution)
	}

	before := third.RawBytes()
	_, err = repository.Append(context.Background(), selection, PreferenceAppendExpectation{PriorRawSHA256: first.RawSHA256})
	if !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("stale append error=%v, want ErrRecordConflict", err)
	}
	after, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(preferenceJournalPath)))
	if readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("stale append changed journal: got=%q err=%v", after, readErr)
	}
}

func TestPreferenceRepositoryRejectsPartialTailDuplicateAndUnknownVersionAppend(t *testing.T) {
	references := preferenceRepositoryReferences()
	provider := &repositoryReferenceProvider{preference: references}
	selection := preferenceRepositoryRaw(t, references)

	t.Run("partial tail", func(t *testing.T) {
		workspace := managedRepositoryWorkspace(t)
		writeWorkspaceTestFile(t, workspace, preferenceJournalPath, string(selection))
		repository := newPreferenceRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
		if _, err := repository.Read(context.Background()); !errors.Is(err, ErrPreferencePartialTail) {
			t.Fatalf("Read error=%v, want ErrPreferencePartialTail", err)
		}
	})

	t.Run("duplicate identity", func(t *testing.T) {
		workspace := managedRepositoryWorkspace(t)
		repository := newPreferenceRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
		first, err := repository.Append(context.Background(), selection, PreferenceAppendExpectation{PriorRawSHA256: recordSHA256(nil)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.Append(context.Background(), selection, PreferenceAppendExpectation{PriorRawSHA256: first.RawSHA256}); err == nil {
			t.Fatal("duplicate signal ID append succeeded")
		}
	})

	t.Run("unknown prior version", func(t *testing.T) {
		workspace := managedRepositoryWorkspace(t)
		unknown := []byte(`{"contract":{"kind":"denova.preference-signal","version":"v2","schema_id":"preference-memory-v2.schema.json"},"future":true}` + "\n")
		writeWorkspaceTestFile(t, workspace, preferenceJournalPath, string(unknown))
		repository := newPreferenceRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
		read, err := repository.Read(context.Background())
		if err != nil || len(read.Entries) != 1 || read.Entries[0].CanManagedMutate() || !bytes.Equal(read.RawBytes(), unknown) {
			t.Fatalf("unknown Read=%#v err=%v", read, err)
		}
		if _, err := repository.Append(context.Background(), selection, PreferenceAppendExpectation{PriorRawSHA256: read.RawSHA256}); !errors.Is(err, ErrUnknownRecordVersion) {
			t.Fatalf("append after unknown version error=%v, want ErrUnknownRecordVersion", err)
		}
		got, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(preferenceJournalPath)))
		if readErr != nil || !bytes.Equal(got, unknown) {
			t.Fatalf("unknown bytes changed: got=%q err=%v", got, readErr)
		}
	})
}

func TestPreferenceRepositoryAppendFaultsPreserveOldPrefixAndCompleteLines(t *testing.T) {
	references := preferenceRepositoryReferences()
	provider := &repositoryReferenceProvider{preference: references}
	selection := preferenceRepositoryRaw(t, references)
	for _, point := range []RepositoryFaultPoint{
		RepositoryFaultAfterWrite,
		RepositoryFaultBeforeFileSync,
		RepositoryFaultBeforePublish,
		RepositoryFaultAfterCASBeforePublish,
		RepositoryFaultBeforeParentSync,
	} {
		t.Run(string(point), func(t *testing.T) {
			workspace := managedRepositoryWorkspace(t)
			baseRepository := newPreferenceRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
			first, err := baseRepository.Append(context.Background(), selection, PreferenceAppendExpectation{PriorRawSHA256: recordSHA256(nil)})
			if err != nil {
				t.Fatal(err)
			}
			correction := preferenceMaintenanceRaw(t, selection, "preference-signal-002", "correction", "preference-signal-001", references)
			faulted := newPreferenceRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{Fail: func(got RepositoryFaultPoint) error {
				if got == point {
					return errors.New("injected fault")
				}
				return nil
			}})
			_, err = faulted.Append(context.Background(), correction, PreferenceAppendExpectation{PriorRawSHA256: first.RawSHA256})
			if !errors.Is(err, ErrRecordDurability) {
				t.Fatalf("Append error=%v, want ErrRecordDurability", err)
			}
			got, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(preferenceJournalPath)))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.HasPrefix(got, first.RawBytes()) || got[len(got)-1] != '\n' {
				t.Fatalf("fault lost old prefix or left partial line: %q", got)
			}
			if !bytes.Equal(got, first.RawBytes()) {
				t.Fatalf("pre-publish fault changed journal: %q", got)
			}
			assertNoRepositoryTemps(t, workspace)
		})
	}
}

func TestPreferenceRepositoryBoundsInputBeforeDecodeOrReferenceResolution(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{preference: preferenceRepositoryReferences()}
	repository := newPreferenceRepositoryForTestLimits(t, workspace, provider, RecordRepositoryLimits{MaxJournalBytes: 16})
	if _, err := repository.Append(context.Background(), bytes.Repeat([]byte("x"), 17), PreferenceAppendExpectation{PriorRawSHA256: recordSHA256(nil)}); !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("Append error=%v, want ErrRecordTooLarge", err)
	}
	if provider.preferenceCalls != 0 {
		t.Fatalf("oversize input reached reference provider: calls=%d", provider.preferenceCalls)
	}
}

func newPreferenceRepositoryForTest(t *testing.T, workspace string, provider RecordReferenceProvider, hooks RecordRepositoryHooks) *PreferenceMemoryRepository {
	return newPreferenceRepositoryForTestAuthority(t, workspace, provider, hooks, &repositoryAuthorityVerifier{})
}

func newPreferenceRepositoryForTestAuthority(t *testing.T, workspace string, provider RecordReferenceProvider, hooks RecordRepositoryHooks, authority RecordAuthorityVerifier) *PreferenceMemoryRepository {
	return newPreferenceRepositoryForTestConfig(t, workspace, provider, hooks, authority, RecordRepositoryLimits{})
}

func newPreferenceRepositoryForTestLimits(t *testing.T, workspace string, provider RecordReferenceProvider, limits RecordRepositoryLimits) *PreferenceMemoryRepository {
	t.Helper()
	return newPreferenceRepositoryForTestConfig(t, workspace, provider, RecordRepositoryHooks{}, &repositoryAuthorityVerifier{}, limits)
}

func newPreferenceRepositoryForTestConfig(t *testing.T, workspace string, provider RecordReferenceProvider, hooks RecordRepositoryHooks, authority RecordAuthorityVerifier, limits RecordRepositoryLimits) *PreferenceMemoryRepository {
	t.Helper()
	repository, err := NewPreferenceMemoryRepository(RecordRepositoryConfig{
		Workspace:  workspace,
		Inspector:  InspectorOptions{ApplicationVersion: "1.6.2", SupportedFeatures: map[string]string{"quality_harness": ">=1.0.0 <2.0.0"}},
		Decoder:    newRecordDecoderForTest(t),
		Lease:      &migrationTestLease{invoke: true},
		References: provider,
		Authority:  authority,
		Limits:     limits,
		Hooks:      hooks,
	})
	if err != nil {
		t.Fatalf("NewPreferenceMemoryRepository: %v", err)
	}
	return repository
}

func preferenceRepositoryReferences() domain.PreferenceSignalReferences {
	return domain.PreferenceSignalReferences{
		Workspace:            []byte("workspace"),
		Profile:              []byte("profile"),
		QualitySpec:          []byte("quality-spec"),
		Source:               []byte("candidate"),
		ConfirmationEvidence: []byte("confirmation"),
		CandidateSetID:       "candidate-set-001",
		CandidateID:          "candidate-001",
		Candidate:            []byte("candidate"),
		ReplacementEvidence:  []byte("replacement"),
		RevocationReason:     []byte("revocation"),
	}
}

func preferenceRepositoryRaw(t *testing.T, references domain.PreferenceSignalReferences) []byte {
	t.Helper()
	document := preferenceSignalFixture()
	document["workspace"].(map[string]any)["content_hash"] = recordSHA256(references.Workspace)
	document["provenance"].(map[string]any)["profile"].(map[string]any)["hash"] = recordSHA256(references.Profile)
	document["provenance"].(map[string]any)["quality_spec"].(map[string]any)["hash"] = recordSHA256(references.QualitySpec)
	document["provenance"].(map[string]any)["content_hash"] = recordSHA256(references.Source)
	document["event_reference"].(map[string]any)["candidate_hash"] = recordSHA256(references.Candidate)
	document["confirmation"].(map[string]any)["evidence_hash"] = recordSHA256(references.ConfirmationEvidence)
	return marshalRecordFixture(t, document)
}

func preferenceMaintenanceRaw(t *testing.T, base []byte, id, event, target string, references domain.PreferenceSignalReferences) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(base, &document); err != nil {
		t.Fatal(err)
	}
	document["signal_id"] = id
	document["event"] = event
	document["recorded_at"] = "2026-07-21T00:00:01Z"
	document["confirmation"].(map[string]any)["method"] = event
	switch event {
	case "correction":
		document["event_reference"] = map[string]any{"kind": "correction", "corrected_signal_id": target, "replacement_evidence_hash": recordSHA256(references.ReplacementEvidence)}
		document["supersedes_signal_ids"] = []any{target}
	case "revocation":
		document["provenance"].(map[string]any)["source_kind"] = "author_revocation"
		document["provenance"].(map[string]any)["content_hash"] = recordSHA256(references.RevocationReason)
		document["event_reference"] = map[string]any{"kind": "revocation", "revocation_reason_hash": recordSHA256(references.RevocationReason)}
		document["revokes_signal_ids"] = []any{target}
	default:
		t.Fatalf("unsupported maintenance event %q", event)
	}
	return marshalRecordFixture(t, document)
}
