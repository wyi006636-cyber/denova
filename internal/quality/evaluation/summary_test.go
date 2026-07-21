package evaluation

import (
	"path/filepath"
	"strings"
	"testing"
)

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
			if err := writeJSONFile(filepath.Join(runRoot, run.RunID, "blind", "reviews", review.ReviewID+".json"), review); err != nil {
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
