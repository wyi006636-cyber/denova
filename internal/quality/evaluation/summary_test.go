package evaluation

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

func TestSaveReviewSerializesConcurrentIndependentLimits(t *testing.T) {
	runRoot, run := readyPackagedRun(t)
	sample := firstReadySample(t, runRoot, run.RunID)
	start := make(chan struct{})
	results := make(chan error, 3)
	var group sync.WaitGroup
	for index := 0; index < 3; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results <- SaveReview(runRoot, run.RunID, validReview(sample, "concurrent-reviewer-"+string(rune('1'+index)), "A"))
		}()
	}
	close(start)
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 2 {
		t.Fatalf("successful independent reviews=%d, want 2", successes)
	}
	reviews, err := loadReviews(runRoot, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 2 {
		t.Fatalf("persisted reviews=%d, want 2", len(reviews))
	}

	index, err := LoadBlindIndex(runRoot, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	duplicateSample := index.Samples[1]
	duplicateResults := make(chan error, 2)
	start = make(chan struct{})
	for index := 0; index < 2; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			review := validReview(duplicateSample, "same-reviewer", "A")
			review.ReviewID = "same-id-race-" + string(rune('1'+index))
			duplicateResults <- SaveReview(runRoot, run.RunID, review)
		}()
	}
	close(start)
	group.Wait()
	close(duplicateResults)
	if successes := countSuccessfulReviews(duplicateResults); successes != 1 {
		t.Fatalf("duplicate reviewer successes=%d, want 1", successes)
	}
}

func TestSaveReviewSerializesSameIDRace(t *testing.T) {
	runRoot, run := readyPackagedRun(t)
	sample := firstReadySample(t, runRoot, run.RunID)
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			review := validReview(sample, "same-id-reviewer-"+string(rune('1'+index)), "A")
			review.ReviewID = "same-review-id"
			results <- SaveReview(runRoot, run.RunID, review)
		}()
	}
	close(start)
	group.Wait()
	close(results)
	if successes := countSuccessfulReviews(results); successes != 1 {
		t.Fatalf("same review ID successes=%d, want 1", successes)
	}
	reviews, err := loadReviews(runRoot, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].ReviewID != "same-review-id" {
		t.Fatalf("reviews=%#v", reviews)
	}
}

func TestSaveReviewSerializesSeparateProcesses(t *testing.T) {
	runRoot, run := readyPackagedRun(t)
	sample := firstReadySample(t, runRoot, run.RunID)
	reviews := []ReviewRecord{
		validReview(sample, "process-reviewer-one", "A"),
		validReview(sample, "process-reviewer-two", "A"),
		validReview(sample, "process-reviewer-three", "A"),
	}
	commands := make([]*exec.Cmd, 0, len(reviews))
	for _, review := range reviews {
		payload, err := json.Marshal(review)
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(os.Args[0], "-test.run=TestSaveReviewProcessHelper")
		command.Env = append(os.Environ(), "DENOVA_REVIEW_PROCESS_HELPER=1", "DENOVA_REVIEW_RUN_ROOT="+runRoot, "DENOVA_REVIEW_RUN_ID="+run.RunID, "DENOVA_REVIEW_RECORD="+string(payload))
		commands = append(commands, command)
	}
	for _, command := range commands {
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	successes := 0
	for _, command := range commands {
		if err := command.Wait(); err == nil {
			successes++
		}
	}
	if successes != 2 {
		t.Fatalf("separate process successes=%d, want 2", successes)
	}
	recorded, err := loadReviews(runRoot, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded) != 2 {
		t.Fatalf("persisted separate process reviews=%d, want 2", len(recorded))
	}
}

func TestSaveReviewProcessHelper(t *testing.T) {
	if os.Getenv("DENOVA_REVIEW_PROCESS_HELPER") != "1" {
		return
	}
	var review ReviewRecord
	if err := json.Unmarshal([]byte(os.Getenv("DENOVA_REVIEW_RECORD")), &review); err != nil {
		os.Exit(2)
	}
	if err := SaveReview(os.Getenv("DENOVA_REVIEW_RUN_ROOT"), os.Getenv("DENOVA_REVIEW_RUN_ID"), review); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func countSuccessfulReviews(results <-chan error) int {
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	return successes
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
	fixture := writeReadyCohortRunForSelection(t, []DataSplit{SplitTuning, SplitRegression}, []string{
		"long_serial-01", "long_serial-02", "fanqie_short-04", "zhihu_salt_short-05",
	})
	if _, err := PackageBlind(fixture.RunRoot, fixture.RunID); err != nil {
		t.Fatal(err)
	}
	index, err := LoadBlindIndex(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range index.Samples {
		for n := 1; n <= 2; n++ {
			review := validReview(sample, sample.SampleID+"-reviewer-"+string(rune('0'+n)), "A")
			if err := writeJSONFile(filepath.Join(fixture.RunRoot, fixture.RunID, "private", "reviews", review.ReviewID+".json"), review); err != nil {
				t.Fatal(err)
			}
		}
	}
	summary, err := Summarize(fixture.RunRoot, fixture.RunID)
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
	return writeReadyCohortRunForSelection(t, []DataSplit{split}, nil)
}

func writeReadyCohortRunForSelection(t *testing.T, dataSplits []DataSplit, taskIDs []string) readyCohortFixture {
	t.Helper()
	manifestPath, manifest := writeValidManifest(t)
	policyPath := writeHarnessPolicyFixture(t, nil)
	policy, err := LoadHarnessPolicy(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewRunSelection(dataSplits, taskIDs)
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
