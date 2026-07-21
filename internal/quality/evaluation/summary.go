package evaluation

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
)

type Summary struct {
	Contract             string                  `json:"contract"`
	Version              string                  `json:"version"`
	RunID                string                  `json:"run_id"`
	Status               ResultStatus            `json:"status"`
	MissingArms          int                     `json:"missing_arms"`
	MissingReviews       int                     `json:"missing_reviews"`
	PendingAdjudications int                     `json:"pending_adjudications"`
	Paired               PairedMetric            `json:"paired"`
	ByProfile            map[string]PairedMetric `json:"by_profile"`
	ByGenre              map[string]PairedMetric `json:"by_genre"`
	ByTaskType           map[string]PairedMetric `json:"by_task_type"`
	ByLengthBucket       map[string]PairedMetric `json:"by_length_bucket"`
	FactErrors           ComparativeMetric       `json:"fact_errors"`
	AuthorEditRatio      ComparativeMetric       `json:"author_edit_ratio"`
	Cost                 CostSummary             `json:"cost"`
	Interpretation       string                  `json:"interpretation"`
}

type PairedMetric struct {
	Wins   int                `json:"harness_wins"`
	Losses int                `json:"harness_losses"`
	Ties   int                `json:"ties"`
	Total  int                `json:"total"`
	Score  float64            `json:"paired_score"`
	CI95   ConfidenceInterval `json:"bootstrap_95_ci"`
}

type ConfidenceInterval struct {
	Low  float64 `json:"low"`
	High float64 `json:"high"`
}

type ComparativeMetric struct {
	Harness float64 `json:"harness"`
	Single  float64 `json:"single"`
	Delta   float64 `json:"delta"`
	Samples int     `json:"samples"`
}

type CostSummary struct {
	Status               string   `json:"status"`
	Currency             string   `json:"currency,omitempty"`
	HarnessAmount        *float64 `json:"harness_amount,omitempty"`
	SingleAmount         *float64 `json:"single_amount,omitempty"`
	HarnessTotalTokens   int      `json:"harness_total_tokens"`
	SingleTotalTokens    int      `json:"single_total_tokens"`
	HarnessToSingleRatio *float64 `json:"harness_to_single_ratio,omitempty"`
}

type evaluatedSample struct {
	sample       BlindSample
	outcome      float64
	harnessFacts float64
	singleFacts  float64
	harnessEdit  float64
	singleEdit   float64
}

func Summarize(runRoot, runID string) (Summary, error) {
	run, err := LoadRun(runRoot, runID)
	if err != nil {
		return Summary{}, err
	}
	index, err := LoadBlindIndex(runRoot, runID)
	if err != nil {
		return Summary{}, err
	}
	mapping, err := loadBlindMap(runRoot, runID)
	if err != nil {
		return Summary{}, err
	}
	reviews, err := loadReviews(runRoot, runID)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{
		Contract: "denova.quality-evaluation-summary", Version: "v1", RunID: runID,
		ByProfile: map[string]PairedMetric{}, ByGenre: map[string]PairedMetric{},
		ByTaskType: map[string]PairedMetric{}, ByLengthBucket: map[string]PairedMetric{},
		Cost: summarizeCosts(run),
	}
	mapBySample := make(map[string]blindMapSample, len(mapping.Samples))
	for _, item := range mapping.Samples {
		mapBySample[item.SampleID] = item
	}
	reviewsBySample := make(map[string][]ReviewRecord)
	for _, review := range reviews {
		reviewsBySample[review.SampleID] = append(reviewsBySample[review.SampleID], review)
	}
	var evaluated []evaluatedSample
	for _, sample := range index.Samples {
		if sample.Status != StatusReady {
			summary.MissingArms++
			continue
		}
		item, ok := mapBySample[sample.SampleID]
		if !ok {
			return Summary{}, fmt.Errorf("sample %s missing private blind mapping", sample.SampleID)
		}
		finalReview, state, err := resolveSampleReview(sample.SampleID, reviewsBySample[sample.SampleID])
		if err != nil {
			return Summary{}, err
		}
		switch state {
		case "missing":
			summary.MissingReviews++
			continue
		case "conflict":
			summary.PendingAdjudications++
			continue
		}
		outcome := decisionOutcome(finalReview.Decision, item)
		hFacts, sFacts := optionMetrics(finalReview.FactErrors.A, finalReview.FactErrors.B, item)
		hEdit, sEdit := optionMetrics(finalReview.AuthorEditRatio.A, finalReview.AuthorEditRatio.B, item)
		evaluated = append(evaluated, evaluatedSample{
			sample: sample, outcome: outcome, harnessFacts: hFacts, singleFacts: sFacts,
			harnessEdit: hEdit, singleEdit: sEdit,
		})
	}
	summary.Paired = pairedMetric(outcomes(evaluated), runID+":all")
	summary.ByProfile = groupMetrics(evaluated, func(item evaluatedSample) string { return string(item.sample.ProfileID) }, runID+":profile")
	summary.ByGenre = groupMetrics(evaluated, func(item evaluatedSample) string { return item.sample.Genre }, runID+":genre")
	summary.ByTaskType = groupMetrics(evaluated, func(item evaluatedSample) string { return string(item.sample.TaskType) }, runID+":task-type")
	summary.ByLengthBucket = groupMetrics(evaluated, func(item evaluatedSample) string { return string(item.sample.LengthBucket) }, runID+":length")
	summary.FactErrors = comparativeMetric(evaluated, func(item evaluatedSample) (float64, float64) { return item.harnessFacts, item.singleFacts })
	summary.AuthorEditRatio = comparativeMetric(evaluated, func(item evaluatedSample) (float64, float64) { return item.harnessEdit, item.singleEdit })
	switch {
	case summary.MissingArms > 0:
		summary.Status = StatusNotReady
		summary.Interpretation = "At least one paired arm is unavailable; no quality conclusion is valid."
	case summary.MissingReviews > 0 || summary.PendingAdjudications > 0 || summary.Paired.Total < 2:
		summary.Status = StatusNotEnoughData
		summary.Interpretation = "Paired outputs exist, but independent review coverage or adjudication is incomplete."
	default:
		summary.Status = StatusValid
		summary.Interpretation = "The paired calculation is valid evaluation data; Phase 0 does not claim that Harness wins."
	}
	if err := writeJSONFile(filepath.Join(runRoot, runID, "summary.json"), summary); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func resolveSampleReview(sampleID string, reviews []ReviewRecord) (ReviewRecord, string, error) {
	var independent []ReviewRecord
	var adjudications []ReviewRecord
	seenReviewers := map[string]bool{}
	for _, review := range reviews {
		if seenReviewers[review.ReviewerID] {
			return ReviewRecord{}, "", fmt.Errorf("duplicate reviewer %s for sample %s", review.ReviewerID, sampleID)
		}
		seenReviewers[review.ReviewerID] = true
		switch review.Kind {
		case ReviewKindIndependent:
			independent = append(independent, review)
		case ReviewKindAdjudication:
			adjudications = append(adjudications, review)
		default:
			return ReviewRecord{}, "", fmt.Errorf("unknown review kind %q for sample %s", review.Kind, sampleID)
		}
	}
	if len(independent) < 2 {
		return ReviewRecord{}, "missing", nil
	}
	if len(independent) > 2 {
		return ReviewRecord{}, "", fmt.Errorf("sample %s has more than two independent reviews", sampleID)
	}
	if independent[0].Decision == independent[1].Decision {
		return averageReviewMetrics(independent[0], independent[1]), "resolved", nil
	}
	leftID, rightID := independent[0].ReviewID, independent[1].ReviewID
	for _, adjudication := range adjudications {
		if sameIDPair(adjudication.ConflictReviewIDs, leftID, rightID) {
			return adjudication, "resolved", nil
		}
	}
	return ReviewRecord{}, "conflict", nil
}

func averageReviewMetrics(left, right ReviewRecord) ReviewRecord {
	left.FactErrors.A = (left.FactErrors.A + right.FactErrors.A) / 2
	left.FactErrors.B = (left.FactErrors.B + right.FactErrors.B) / 2
	left.AuthorEditRatio.A = (left.AuthorEditRatio.A + right.AuthorEditRatio.A) / 2
	left.AuthorEditRatio.B = (left.AuthorEditRatio.B + right.AuthorEditRatio.B) / 2
	return left
}

func sameIDPair(values []string, left, right string) bool {
	return len(values) == 2 && ((values[0] == left && values[1] == right) || (values[0] == right && values[1] == left))
}

func decisionOutcome(decision string, mapping blindMapSample) float64 {
	if decision == "tie" {
		return 0.5
	}
	chosenArm := mapping.OptionAArm
	if decision == "B" {
		chosenArm = mapping.OptionBArm
	}
	if chosenArm == "H" {
		return 1
	}
	return 0
}

func optionMetrics(optionA, optionB float64, mapping blindMapSample) (harness, single float64) {
	if mapping.OptionAArm == "H" {
		return optionA, optionB
	}
	return optionB, optionA
}

func outcomes(items []evaluatedSample) []float64 {
	values := make([]float64, 0, len(items))
	for _, item := range items {
		values = append(values, item.outcome)
	}
	return values
}

func groupMetrics(items []evaluatedSample, key func(evaluatedSample) string, seedPrefix string) map[string]PairedMetric {
	groups := map[string][]float64{}
	for _, item := range items {
		groups[key(item)] = append(groups[key(item)], item.outcome)
	}
	result := make(map[string]PairedMetric, len(groups))
	for name, values := range groups {
		result[name] = pairedMetric(values, seedPrefix+":"+name)
	}
	return result
}

func pairedMetric(values []float64, seed string) PairedMetric {
	metric := PairedMetric{Total: len(values)}
	for _, value := range values {
		switch value {
		case 1:
			metric.Wins++
		case 0:
			metric.Losses++
		default:
			metric.Ties++
		}
		metric.Score += value
	}
	if metric.Total > 0 {
		metric.Score /= float64(metric.Total)
	}
	metric.CI95 = bootstrapCI(values, 2000, seed)
	return metric
}

func bootstrapCI(values []float64, iterations int, seed string) ConfidenceInterval {
	if len(values) == 0 {
		return ConfidenceInterval{}
	}
	if len(values) == 1 {
		return ConfidenceInterval{Low: values[0], High: values[0]}
	}
	sum := sha256.Sum256([]byte(seed))
	rng := rand.New(rand.NewSource(int64(binary.LittleEndian.Uint64(sum[:8]))))
	means := make([]float64, iterations)
	for i := range means {
		var total float64
		for range values {
			total += values[rng.Intn(len(values))]
		}
		means[i] = total / float64(len(values))
	}
	sort.Float64s(means)
	return ConfidenceInterval{
		Low:  means[int(float64(len(means)-1)*0.025)],
		High: means[int(float64(len(means)-1)*0.975)],
	}
}

func comparativeMetric(items []evaluatedSample, values func(evaluatedSample) (float64, float64)) ComparativeMetric {
	metric := ComparativeMetric{Samples: len(items)}
	for _, item := range items {
		harness, single := values(item)
		metric.Harness += harness
		metric.Single += single
	}
	if metric.Samples > 0 {
		metric.Harness /= float64(metric.Samples)
		metric.Single /= float64(metric.Samples)
		metric.Delta = metric.Harness - metric.Single
	}
	return metric
}

func summarizeCosts(run RunRecord) CostSummary {
	summary := CostSummary{Status: "recorded"}
	var harnessAmount, singleAmount float64
	allHarnessAmounts, allSingleAmounts := true, true
	for _, task := range run.Tasks {
		h := task.Arms["H"]
		s := task.Arms["S"]
		summary.HarnessTotalTokens += h.Usage.TotalTokens
		summary.SingleTotalTokens += s.Usage.TotalTokens
		if h.Cost.Amount == nil {
			allHarnessAmounts = false
		} else {
			harnessAmount += *h.Cost.Amount
		}
		if s.Cost.Amount == nil {
			allSingleAmounts = false
		} else {
			singleAmount += *s.Cost.Amount
		}
		if summary.Currency == "" {
			if h.Cost.Currency != "" {
				summary.Currency = h.Cost.Currency
			} else if s.Cost.Currency != "" {
				summary.Currency = s.Cost.Currency
			}
		}
	}
	if !allHarnessAmounts || !allSingleAmounts {
		summary.Status = "NOT-AVAILABLE"
		return summary
	}
	summary.HarnessAmount = &harnessAmount
	summary.SingleAmount = &singleAmount
	if singleAmount > 0 {
		ratio := harnessAmount / singleAmount
		summary.HarnessToSingleRatio = &ratio
	}
	return summary
}
