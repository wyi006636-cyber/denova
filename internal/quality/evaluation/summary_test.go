package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlindPackageContainsOnlySelectedCompletePairs(t *testing.T) {
	fixture := writeReadyCohortRun(t, SplitRegression)
	index, err := PackageBlind(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Samples) != 12 {
		t.Fatalf("samples=%d want=12", len(index.Samples))
	}
	if index.Selection == nil || index.HarnessPolicyID == "" || index.HarnessPolicySHA256 == "" || index.BaselineTemplateSHA256 == "" || len(index.ModelConfigSHA256) == 0 {
		t.Fatalf("blind reproducibility metadata=%#v", index)
	}
	for _, sample := range index.Samples {
		if sample.DataSplit != SplitRegression || sample.Status != StatusReady {
			t.Fatalf("unexpected sample=%#v", sample)
		}
	}
}

func TestCommittedMetadataRejectsHarnessSecretsAndReasoning(t *testing.T) {
	for _, field := range []string{"api_key", "authorization", "thinking_content", "reasoning_content", "reviewer_id", "raw_comments"} {
		if err := rejectSensitiveJSON([]byte(`{"` + field + `":"not-allowed"}`)); err == nil {
			t.Fatalf("field %s was accepted", field)
		}
	}
}

func TestReviewRecordsRemainInPrivateEvidence(t *testing.T) {
	runRoot, run := readyPackagedRun(t)
	review := validReview(firstReadySample(t, runRoot, run.RunID), "private-reviewer", "A")
	if err := SaveReview(runRoot, run.RunID, review); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runRoot, run.RunID, "private", "reviews", review.ReviewID+".json")); err != nil {
		t.Fatalf("private review record: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runRoot, run.RunID, "blind", "reviews", review.ReviewID+".json")); !os.IsNotExist(err) {
		t.Fatalf("reviewer identity escaped private evidence: %v", err)
	}
}

func TestSummarizeRejectsDuplicateIndependentReviewer(t *testing.T) {
	runRoot, run := readyPackagedRun(t)
	sample := firstReadySample(t, runRoot, run.RunID)
	review := validReview(sample, "reviewer-1", "A")
	if err := SaveReview(runRoot, run.RunID, review); err != nil {
		t.Fatal(err)
	}
	review.ReviewID = "review-duplicate"
	if err := SaveReview(runRoot, run.RunID, review); err == nil || !strings.Contains(strings.ToLower(err.Error()), "duplicate reviewer") {
		t.Fatalf("SaveReview() error = %v, want duplicate reviewer", err)
	}
}

func TestSummarizeRequiresAdjudicationForConflict(t *testing.T) {
	runRoot, run := readyPackagedRun(t)
	index, err := LoadBlindIndex(runRoot, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for i, sample := range index.Samples[:2] {
		left := validReview(sample, "reviewer-left-"+sample.SampleID, "A")
		right := validReview(sample, "reviewer-right-"+sample.SampleID, "B")
		if err := SaveReview(runRoot, run.RunID, left); err != nil {
			t.Fatal(err)
		}
		if err := SaveReview(runRoot, run.RunID, right); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			adjudication := validReview(sample, "adjudicator-"+sample.SampleID, "tie")
			adjudication.Kind = ReviewKindAdjudication
			adjudication.ConflictReviewIDs = []string{left.ReviewID, right.ReviewID}
			if err := SaveReview(runRoot, run.RunID, adjudication); err != nil {
				t.Fatal(err)
			}
		}
	}
	summary, err := Summarize(runRoot, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != StatusNotEnoughData || summary.PendingAdjudications != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestSummarizeReportsStrataAndValidPairedResults(t *testing.T) {
	runRoot, run := readyPackagedRun(t)
	index, err := LoadBlindIndex(runRoot, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range index.Samples {
		for n := 1; n <= 2; n++ {
			review := validReview(sample, sample.SampleID+"-reviewer-"+string(rune('0'+n)), "A")
			if err := writeJSONFile(filepath.Join(runRoot, run.RunID, "private", "reviews", review.ReviewID+".json"), review); err != nil {
				t.Fatal(err)
			}
		}
	}
	summary, err := Summarize(runRoot, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != StatusValid || summary.Paired.Total != len(index.Samples) {
		t.Fatalf("summary status/total = %s/%d", summary.Status, summary.Paired.Total)
	}
	if len(summary.ByProfile) != 3 || len(summary.ByGenre) < 2 || len(summary.ByTaskType) < 4 || len(summary.ByLengthBucket) < 2 {
		t.Fatalf("missing strata: profiles=%d genres=%d task_types=%d lengths=%d", len(summary.ByProfile), len(summary.ByGenre), len(summary.ByTaskType), len(summary.ByLengthBucket))
	}
	if summary.Paired.CI95.High < summary.Paired.CI95.Low {
		t.Fatalf("invalid CI: %#v", summary.Paired.CI95)
	}
}

func TestSummarizeMarksIncompleteReviewsNotEnoughData(t *testing.T) {
	runRoot, run := readyPackagedRun(t)
	index, err := LoadBlindIndex(runRoot, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	sample := firstReadySample(t, runRoot, run.RunID)
	for n := 1; n <= 2; n++ {
		review := validReview(sample, "only-sample-reviewer-"+string(rune('0'+n)), "tie")
		if err := SaveReview(runRoot, run.RunID, review); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := Summarize(runRoot, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != StatusNotEnoughData || summary.Paired.Total != 1 || summary.MissingReviews != len(index.Samples)-1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func readyPackagedRun(t *testing.T) (string, RunRecord) {
	t.Helper()
	manifestPath, manifest := writeValidManifest(t)
	runRoot := filepath.Join(filepath.Dir(manifestPath), manifest.RunRoot)
	run := newTestRun(t, manifestPath, manifest, runRoot, true)
	if _, err := PackageBlind(runRoot, run.RunID); err != nil {
		t.Fatal(err)
	}
	return runRoot, run
}

type readyCohortFixture struct {
	RunRoot string
	RunID   string
}

func writeReadyCohortRun(t *testing.T, split DataSplit) readyCohortFixture {
	t.Helper()
	manifestPath, manifest := writeValidManifest(t)
	policyPath := writeHarnessPolicyFixture(t, nil)
	policy, err := LoadHarnessPolicy(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewRunSelection([]DataSplit{split}, nil)
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(filepath.Dir(manifestPath), manifest.RunRoot)
	run, err := CreateRun(manifestPath, CreateRunOptions{
		RunRoot: runRoot, Selection: &selection, HarnessPolicyID: policy.PolicyID,
		HarnessPolicySHA256: HarnessPolicySHA256(policy),
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range run.Tasks {
		taskDir := filepath.Join(runRoot, run.RunID, "private", "outputs", run.Tasks[i].TaskID)
		if err := os.MkdirAll(taskDir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, arm := range []string{"S", "H"} {
			path := filepath.Join(taskDir, strings.ToLower(arm)+".txt")
			payload := []byte("fiction output " + arm + " for " + run.Tasks[i].TaskID)
			if err := os.WriteFile(path, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			record := run.Tasks[i].Arms[arm]
			record.Status = StatusReady
			record.OutputFile = filepath.ToSlash(filepath.Join("private", "outputs", run.Tasks[i].TaskID, strings.ToLower(arm)+".txt"))
			record.OutputSHA256 = bytesSHA256(payload)
			record.Usage = UsageRecord{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, ModelCalls: 1}
			record.Cost = CostRecord{Status: "recorded", Currency: "USD", Amount: floatPtr64(0.01)}
			run.Tasks[i].Arms[arm] = record
		}
	}
	if err := SaveRun(runRoot, run); err != nil {
		t.Fatal(err)
	}
	return readyCohortFixture{RunRoot: runRoot, RunID: run.RunID}
}

func firstReadySample(t *testing.T, runRoot, runID string) BlindSample {
	t.Helper()
	index, err := LoadBlindIndex(runRoot, runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range index.Samples {
		if sample.Status == StatusReady {
			return sample
		}
	}
	t.Fatal("no ready sample")
	return BlindSample{}
}

func validReview(sample BlindSample, reviewer, decision string) ReviewRecord {
	return ReviewRecord{
		Contract: "denova.quality-evaluation-review", Version: "v1",
		ReviewID: "review-" + reviewer + "-" + sample.SampleID,
		SampleID: sample.SampleID, ReviewerID: reviewer, Kind: ReviewKindIndependent,
		Restatement: StoryRestatement{
			CharacterGoal: "protect the relationship", Obstacle: "conflicting evidence", Choice: "tell the truth",
			Cost: "loss of trust", TextChange: "the relationship becomes irreversible",
		},
		Decision:        decision,
		Evidence:        []EvidenceCitation{{Option: "A", Quote: "fiction output", Reason: "the choice changes the relationship"}},
		FactErrors:      OptionIntMetrics{A: 0, B: 1},
		AuthorEditRatio: OptionFloatMetrics{A: 0.2, B: 0.4},
	}
}
