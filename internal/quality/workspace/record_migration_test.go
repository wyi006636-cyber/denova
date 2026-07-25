package workspace

import (
	"bytes"
	"errors"
	"testing"
)

func TestRecordMigrationPreviewExactV1IsNoOp(t *testing.T) {
	planner, err := NewRecordMigrationPlanner(newRecordDecoderForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	previews := []RecordMigrationPreview{}
	candidate, err := planner.PreviewCandidateSet(marshalRecordFixture(t, candidateSetFixture()))
	if err != nil {
		t.Fatal(err)
	}
	previews = append(previews, candidate)
	issue, err := planner.PreviewReviewIssue(marshalRecordFixture(t, reviewIssueFixture()))
	if err != nil {
		t.Fatal(err)
	}
	previews = append(previews, issue)
	preference, err := planner.PreviewPreferenceSignal(marshalRecordFixture(t, preferenceSignalFixture()))
	if err != nil {
		t.Fatal(err)
	}
	previews = append(previews, preference)

	for _, preview := range previews {
		if preview.Action != RecordMigrationNoOp || preview.Reason != "exact_v1" || preview.WritesPlanned != 0 {
			t.Fatalf("preview=%#v", preview)
		}
		if err := planner.Execute(preview); err != nil {
			t.Fatalf("execute exact-v1 no-op: %v", err)
		}
	}
}

func TestRecordMigrationPreviewPreservesUnknownBytesAndRefusesConversion(t *testing.T) {
	planner, err := NewRecordMigrationPlanner(newRecordDecoderForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("{\n  \"contract\": {\"kind\": \"denova.candidate-set\", \"version\": \"v2\", \"schema\": \"candidate-set-v2.schema.json\"},\n  \"future\": true\n}\n")
	preview, err := planner.PreviewCandidateSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Action != RecordMigrationUnavailable || preview.Reason != "no_accepted_migration_path" || preview.WritesPlanned != 0 || !bytes.Equal(preview.RawBytes(), raw) {
		t.Fatalf("preview=%#v raw=%q", preview, preview.RawBytes())
	}
	if err := planner.Execute(preview); !errors.Is(err, ErrNoAcceptedRecordMigration) {
		t.Fatalf("Execute error=%v, want ErrNoAcceptedRecordMigration", err)
	}
	forged := preview
	forged.SourceVersion = "v1"
	forged.Action = RecordMigrationNoOp
	forged.Reason = "exact_v1"
	if err := planner.Execute(forged); !errors.Is(err, ErrNoAcceptedRecordMigration) {
		t.Fatalf("forged Execute error=%v, want ErrNoAcceptedRecordMigration", err)
	}
	if !bytes.Equal(preview.RawBytes(), raw) {
		t.Fatal("refused execution changed preserved raw bytes")
	}
}

func TestRecordMigrationPreviewRejectsMalformedV1(t *testing.T) {
	planner, err := NewRecordMigrationPlanner(newRecordDecoderForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"contract":{"kind":"denova.review-issue","version":"v1","schema":"review-issue-v1.schema.json"}}`)
	if _, err := planner.PreviewReviewIssue(raw); err == nil {
		t.Fatal("malformed exact v1 produced a migration preview")
	}
}
