package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"denova/internal/quality/domain"
)

func TestRecordDecoderCompilesNormativeSchemasAndParsesExactV1(t *testing.T) {
	decoder, err := NewRecordDecoder(RecordDecoderConfig{
		CandidateSetSchema:     recordSchemaBytes(t, "candidate-set-v1.schema.json"),
		ReviewIssueSchema:      recordSchemaBytes(t, "review-issue-v1.schema.json"),
		PreferenceMemorySchema: recordSchemaBytes(t, "preference-memory-v1.schema.json"),
	})
	if err != nil {
		t.Fatalf("NewRecordDecoder: %v", err)
	}

	candidateRaw := marshalRecordFixture(t, candidateSetFixture())
	candidate, err := decoder.ParseCandidateSet(candidateRaw)
	if err != nil {
		t.Fatalf("ParseCandidateSet: %v", err)
	}
	if candidate.AccessMode() != domain.AccessManagedV1 || !candidate.CanManagedMutate() {
		t.Fatalf("CandidateSet access=%q managed=%v", candidate.AccessMode(), candidate.CanManagedMutate())
	}
	managedCandidate, err := candidate.Managed()
	if err != nil || managedCandidate.CandidateSetID != "candidate-set-001" {
		t.Fatalf("managed CandidateSet=%#v err=%v", managedCandidate, err)
	}

	issueRaw := marshalRecordFixture(t, reviewIssueFixture())
	issue, err := decoder.ParseReviewIssue(issueRaw)
	if err != nil {
		t.Fatalf("ParseReviewIssue: %v", err)
	}
	managedIssue, err := issue.Managed()
	if err != nil || managedIssue.IssueID != "review-issue-001" {
		t.Fatalf("managed ReviewIssue=%#v err=%v", managedIssue, err)
	}

	preferenceRaw := marshalRecordFixture(t, preferenceSignalFixture())
	preference, err := decoder.ParsePreferenceSignal(preferenceRaw)
	if err != nil {
		t.Fatalf("ParsePreferenceSignal: %v", err)
	}
	managedPreference, err := preference.Managed()
	if err != nil || managedPreference.SignalID != "preference-signal-001" {
		t.Fatalf("managed PreferenceSignal=%#v err=%v", managedPreference, err)
	}
}

func TestRecordDecoderPreservesUnknownVersionsAsExactReadOnlyBytes(t *testing.T) {
	decoder := newRecordDecoderForTest(t)
	tests := []struct {
		name  string
		parse func([]byte) (domain.AccessMode, bool, []byte, error)
		raw   []byte
	}{
		{
			name: "CandidateSet",
			raw:  []byte("{\n  \"contract\": {\"kind\": \"denova.candidate-set\", \"version\": \"v2\", \"schema\": \"candidate-set-v2.schema.json\"},\n  \"future\": true\n}\n"),
			parse: func(raw []byte) (domain.AccessMode, bool, []byte, error) {
				record, err := decoder.ParseCandidateSet(raw)
				return record.AccessMode(), record.CanManagedMutate(), record.RawBytes(), err
			},
		},
		{
			name: "ReviewIssue",
			raw:  []byte("{\n  \"contract\": {\"kind\": \"denova.review-issue\", \"version\": \"v9\", \"schema\": \"review-issue-v9.schema.json\"},\n  \"future\": true\n}\n"),
			parse: func(raw []byte) (domain.AccessMode, bool, []byte, error) {
				record, err := decoder.ParseReviewIssue(raw)
				return record.AccessMode(), record.CanManagedMutate(), record.RawBytes(), err
			},
		},
		{
			name: "PreferenceSignal",
			raw:  []byte("{\n  \"contract\": {\"kind\": \"denova.preference-signal\", \"version\": \"future\", \"schema_id\": \"preference-memory-future.schema.json\"},\n  \"future\": true\n}\n"),
			parse: func(raw []byte) (domain.AccessMode, bool, []byte, error) {
				record, err := decoder.ParsePreferenceSignal(raw)
				return record.AccessMode(), record.CanManagedMutate(), record.RawBytes(), err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mode, managed, raw, err := test.parse(test.raw)
			if err != nil {
				t.Fatalf("parse unknown version: %v", err)
			}
			if mode != domain.AccessReadOnly || managed {
				t.Fatalf("access=%q managed=%v", mode, managed)
			}
			if !bytes.Equal(raw, test.raw) {
				t.Fatalf("raw bytes changed:\n got: %q\nwant: %q", raw, test.raw)
			}
		})
	}
}

func TestRecordDecoderRejectsMalformedAndNonExactV1Shapes(t *testing.T) {
	decoder := newRecordDecoderForTest(t)
	candidateMissing := candidateSetFixture()
	delete(candidateMissing, "candidates")
	candidateUnknown := candidateSetFixture()
	candidateUnknown["future"] = true
	candidateEnum := candidateSetFixture()
	candidateEnum["current_state"] = "silently_accepted"
	issueCapability := reviewIssueFixture()
	issueCapability["capability_routing"].(map[string]any)["capability_id"] = "revision.generic"
	preferenceActor := preferenceSignalFixture()
	preferenceActor["author"].(map[string]any)["actor_type"] = "model"
	preferenceEvent := preferenceSignalFixture()
	preferenceEvent["event"] = "telemetry"
	preferenceCombination := preferenceSignalFixture()
	preferenceCombination["event"] = "revocation"
	preferenceCombination["provenance"].(map[string]any)["source_kind"] = "author_revocation"
	preferenceCombination["confirmation"].(map[string]any)["method"] = "revocation"
	preferenceCombination["event_reference"] = map[string]any{"kind": "revocation", "revocation_reason_hash": fixtureHash('a')}

	tests := []struct {
		name  string
		raw   []byte
		parse func([]byte) error
	}{
		{"CandidateSet missing required", marshalRecordFixture(t, candidateMissing), func(raw []byte) error { _, err := decoder.ParseCandidateSet(raw); return err }},
		{"CandidateSet unknown field", marshalRecordFixture(t, candidateUnknown), func(raw []byte) error { _, err := decoder.ParseCandidateSet(raw); return err }},
		{"CandidateSet unknown enum", marshalRecordFixture(t, candidateEnum), func(raw []byte) error { _, err := decoder.ParseCandidateSet(raw); return err }},
		{"ReviewIssue unknown capability", marshalRecordFixture(t, issueCapability), func(raw []byte) error { _, err := decoder.ParseReviewIssue(raw); return err }},
		{"PreferenceSignal wrong actor", marshalRecordFixture(t, preferenceActor), func(raw []byte) error { _, err := decoder.ParsePreferenceSignal(raw); return err }},
		{"PreferenceSignal unknown event", marshalRecordFixture(t, preferenceEvent), func(raw []byte) error { _, err := decoder.ParsePreferenceSignal(raw); return err }},
		{"PreferenceSignal illegal combination", marshalRecordFixture(t, preferenceCombination), func(raw []byte) error { _, err := decoder.ParsePreferenceSignal(raw); return err }},
		{"malformed exact v1", []byte(`{"contract":{"kind":"denova.candidate-set","version":"v1","schema":"candidate-set-v1.schema.json"}`), func(raw []byte) error { _, err := decoder.ParseCandidateSet(raw); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.parse(test.raw)
			if err == nil {
				t.Fatal("non-exact v1 record was accepted")
			}
			var contractErr *domain.ContractError
			if !errors.As(err, &contractErr) || contractErr.Path == "" {
				t.Fatalf("error=%T %v, want located ContractError", err, err)
			}
		})
	}
}

func newRecordDecoderForTest(t *testing.T) *RecordDecoder {
	t.Helper()
	decoder, err := NewRecordDecoder(RecordDecoderConfig{
		CandidateSetSchema:     recordSchemaBytes(t, "candidate-set-v1.schema.json"),
		ReviewIssueSchema:      recordSchemaBytes(t, "review-issue-v1.schema.json"),
		PreferenceMemorySchema: recordSchemaBytes(t, "preference-memory-v1.schema.json"),
	})
	if err != nil {
		t.Fatalf("NewRecordDecoder: %v", err)
	}
	return decoder
}

func recordSchemaBytes(t *testing.T, name string) []byte {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "docs", "project-design", "implementation", "contracts", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	return raw
}

func marshalRecordFixture(t *testing.T, value map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return raw
}

func fixtureHash(char byte) string { return string(bytes.Repeat([]byte{char}, 64)) }

func fixtureContract(kind, schema string) map[string]any {
	return map[string]any{"kind": kind, "version": "v1", "schema": schema}
}

func candidateSetFixture() map[string]any {
	hash := fixtureHash('a')
	timestamp := "2026-07-21T00:00:00Z"
	profile := map[string]any{"profile_id": "long_serial", "contract_version": "v1", "hash": hash}
	qualitySpec := map[string]any{"spec_id": "quality-spec-001", "revision": 1, "contract_version": "v1", "hash": hash}
	artifact := map[string]any{"artifact_id": "artifact-001", "artifact_type": "chapter", "contract_version": "v1", "hash": hash}
	checks := make([]any, 0, 7)
	for _, kind := range []string{"workspace", "artifact", "source_manifest", "candidate", "profile", "quality_spec", "candidate_policy"} {
		checks = append(checks, map[string]any{"binding_kind": kind, "expected_hash": hash, "observed_hash": hash, "status": "valid", "checked_at": timestamp})
	}
	return map[string]any{
		"contract":         fixtureContract("denova.candidate-set", "candidate-set-v1.schema.json"),
		"candidate_set_id": "candidate-set-001",
		"workspace":        map[string]any{"workspace_id": "workspace-001", "revision": 1, "hash": hash},
		"run":              map[string]any{"run_id": "run-001", "contract_version": "v1"},
		"stage":            map[string]any{"stage_id": "stage-001", "stage_type": "draft", "contract_version": "v1"},
		"artifact":         artifact,
		"source_manifest":  map[string]any{"id": "source-manifest-001", "version": "v1", "hash": hash},
		"profile":          profile,
		"quality_spec":     qualitySpec,
		"candidate_policy": map[string]any{"policy_id": "candidate-policy-001", "version": "v1", "requested_count": 1, "key_node": false, "rationale": "ordinary stage"},
		"candidates": []any{map[string]any{
			"candidate_id": "candidate-001", "artifact": artifact, "content_hash": hash,
			"source_manifest": map[string]any{"id": "source-manifest-001", "version": "v1", "hash": hash},
			"model":           map[string]any{"provider": "provider", "model_id": "model-001", "version": "v1"},
			"skill":           map[string]any{"skill_id": "skill-001", "version": "v1", "hash": hash, "capability_id": "draft.chapter"},
			"profile":         profile, "quality_spec": qualitySpec, "created_at": timestamp,
		}},
		"current_state":        "open",
		"transition_history":   []any{},
		"evaluation":           nil,
		"author_decision":      nil,
		"mixed_output":         nil,
		"binding_validation":   checks,
		"finalization_handoff": map[string]any{"status": "not_ready", "content_hash": nil},
	}
}

func reviewIssueFixture() map[string]any {
	hash := fixtureHash('a')
	timestamp := "2026-07-21T00:00:00Z"
	return map[string]any{
		"contract":           fixtureContract("denova.review-issue", "review-issue-v1.schema.json"),
		"issue_id":           "review-issue-001",
		"capability_routing": map[string]any{"capability_id": "revision.scene", "contract_version": "v1", "unknown_capability_id": "reject_explicitly"},
		"binding": map[string]any{
			"workspace":        map[string]any{"workspace_id": "workspace-001", "revision": 1, "hash": hash},
			"run":              map[string]any{"run_id": "run-001", "contract_version": "v1"},
			"stage":            map[string]any{"stage_id": "stage-001", "stage_type": "review", "contract_version": "v1"},
			"artifact":         map[string]any{"artifact_id": "artifact-001", "artifact_type": "chapter", "contract_version": "v1", "hash": hash},
			"candidate_set_id": "candidate-set-001", "candidate_set_hash": hash,
			"candidate_id": "candidate-001", "candidate_content_hash": hash,
			"source_manifest":       map[string]any{"id": "source-manifest-001", "version": "v1", "hash": hash},
			"profile":               map[string]any{"profile_id": "long_serial", "contract_version": "v1", "hash": hash},
			"quality_spec":          map[string]any{"spec_id": "quality-spec-001", "revision": 1, "contract_version": "v1", "hash": hash},
			"reviewed_content_hash": hash,
		},
		"attachment":      map[string]any{"kind": "candidate", "target_id": "candidate-001", "target_hash": hash},
		"location":        map[string]any{"artifact_path": "chapters/chapter-001.md", "byte_range": map[string]any{"start": 0, "end": 1}, "anchor_hash": hash, "quoted_text_hash": hash},
		"reader_evidence": map[string]any{"observable_effect": "reader loses orientation", "summary": "scene location is unclear", "excerpts": []any{map[string]any{"quote": "x", "location": map[string]any{"start": 0, "end": 1}, "hash": hash}}},
		"cause":           map[string]any{"category": "scene", "explanation": "location cue is missing"},
		"severity":        "major", "revision_layer": "scene",
		"recommendation":         map[string]any{"minimum_impact_change": "add one location cue", "affected_range": map[string]any{"start": 0, "end": 1}, "dimensions_to_recheck": []any{"scene_orientation"}},
		"reviewer_provenance":    map[string]any{"source_id": "reviewer-001", "source_kind": "reviewer", "source_version": "v1", "source_hash": hash, "created_at": timestamp},
		"reviewer_output_policy": map[string]any{"output": "evidence_and_findings_only", "writer_chain_of_thought": "forbidden", "formal_mutation_authority": false},
		"status":                 "open",
		"status_history":         []any{map[string]any{"transition_id": "transition-001", "from": nil, "to": "open", "actor": map[string]any{"actor_id": "reviewer-001", "actor_type": "reviewer"}, "reason": "issue created", "at": timestamp, "reviewed_content_hash": hash}},
		"reverification_history": []any{},
	}
}

func preferenceSignalFixture() map[string]any {
	hash := fixtureHash('a')
	timestamp := "2026-07-21T00:00:00Z"
	return map[string]any{
		"contract":  map[string]any{"kind": "denova.preference-signal", "version": "v1", "schema_id": "preference-memory-v1.schema.json"},
		"signal_id": "preference-signal-001", "event": "selection",
		"scope":           map[string]any{"kind": "workspace", "author_id": "author-001", "project_id": "project-001", "workspace_id": "workspace-001"},
		"author":          map[string]any{"actor_id": "author-001", "actor_type": "author"},
		"workspace":       map[string]any{"workspace_id": "workspace-001", "project_id": "project-001", "canonical_path": "/workspace", "revision": "revision-001", "content_hash": hash},
		"provenance":      map[string]any{"source_kind": "candidate", "operation_id": "operation-001", "profile": map[string]any{"id": "long_serial", "version": "v1", "hash": hash}, "quality_spec": map[string]any{"id": "quality-spec-001", "revision": "1", "version": "v1", "hash": hash}, "content_hash": hash},
		"event_reference": map[string]any{"kind": "selection", "candidate_set_id": "candidate-set-001", "candidate_id": "candidate-001", "candidate_hash": hash},
		"preference":      map[string]any{"dimension": "opening_dialogue", "value": "shorter", "reason": "faster entry", "strength": "normal", "confidence": 1.0},
		"confirmation":    map[string]any{"explicit": true, "method": "selection", "confirmed_at": timestamp, "evidence_hash": hash},
		"recorded_at":     timestamp,
	}
}
