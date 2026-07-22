package evaluation

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ReviewKind string

const (
	ReviewKindIndependent  ReviewKind = "independent"
	ReviewKindAdjudication ReviewKind = "adjudication"
)

type ReviewRecord struct {
	Contract          string             `json:"contract"`
	Version           string             `json:"version"`
	ReviewID          string             `json:"review_id"`
	SampleID          string             `json:"sample_id"`
	ReviewerID        string             `json:"reviewer_id"`
	Kind              ReviewKind         `json:"kind"`
	ConflictReviewIDs []string           `json:"conflict_review_ids,omitempty"`
	Restatement       StoryRestatement   `json:"restatement"`
	Decision          string             `json:"decision"`
	Evidence          []EvidenceCitation `json:"evidence"`
	FactErrors        OptionIntMetrics   `json:"fact_errors"`
	AuthorEditRatio   OptionFloatMetrics `json:"author_edit_ratio"`
	Notes             string             `json:"notes,omitempty"`
}

type StoryRestatement struct {
	CharacterGoal string `json:"character_goal"`
	Obstacle      string `json:"obstacle"`
	Choice        string `json:"choice"`
	Cost          string `json:"cost"`
	TextChange    string `json:"text_change"`
}

type EvidenceCitation struct {
	Option string `json:"option"`
	Quote  string `json:"quote"`
	Reason string `json:"reason"`
}

type OptionIntMetrics struct {
	A float64 `json:"A"`
	B float64 `json:"B"`
}

type OptionFloatMetrics struct {
	A float64 `json:"A"`
	B float64 `json:"B"`
}

func SaveReview(runRoot, runID string, review ReviewRecord) error {
	lock, err := acquireReviewLock(runRoot, runID)
	if err != nil {
		return ReviewPersistenceError{}
	}
	defer lock.Close()
	index, err := LoadBlindIndex(runRoot, runID)
	if err != nil {
		return ReviewPersistenceError{}
	}
	sampleReady := false
	for _, sample := range index.Samples {
		if sample.SampleID == review.SampleID {
			sampleReady = sample.Status == StatusReady
			break
		}
	}
	if !sampleReady {
		return fmt.Errorf("sample %s is missing or not ready for review", review.SampleID)
	}
	existing, err := loadReviews(runRoot, runID)
	if err != nil {
		return ReviewPersistenceError{}
	}
	if err := validateReview(review, existing); err != nil {
		return err
	}
	if err := writePrivateReview(filepath.Join(runRoot, runID, "private", "reviews", review.ReviewID+".json"), review); err != nil {
		return ReviewPersistenceError{}
	}
	return nil
}

// ReviewPersistenceError means durable private review state could not be read or written.
type ReviewPersistenceError struct{}

func (ReviewPersistenceError) Error() string { return "review persistence failed" }

// DecodeReviewRecord strictly reads one human-submitted review record from an already-open input.
func DecodeReviewRecord(reader io.Reader) (ReviewRecord, error) {
	var review ReviewRecord
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil {
		return ReviewRecord{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ReviewRecord{}, fmt.Errorf("trailing JSON value")
		}
		return ReviewRecord{}, err
	}
	return review, nil
}

func writePrivateReview(path string, review ReviewRecord) error {
	payload, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateHarnessOutput(path, payload)
}

func validateReview(review ReviewRecord, existing []ReviewRecord) error {
	if review.Contract != "denova.quality-evaluation-review" || review.Version != "v1" {
		return fmt.Errorf("review contract/version invalid")
	}
	if !idPattern.MatchString(review.ReviewID) || !idPattern.MatchString(review.SampleID) || !idPattern.MatchString(review.ReviewerID) {
		return fmt.Errorf("review, sample, and reviewer IDs must be stable safe IDs")
	}
	if review.Kind != ReviewKindIndependent && review.Kind != ReviewKindAdjudication {
		return fmt.Errorf("review %s has unknown kind %q", review.ReviewID, review.Kind)
	}
	if review.Decision != "A" && review.Decision != "B" && review.Decision != "tie" {
		return fmt.Errorf("review %s has invalid decision %q", review.ReviewID, review.Decision)
	}
	for name, value := range map[string]string{
		"character_goal": review.Restatement.CharacterGoal,
		"obstacle":       review.Restatement.Obstacle,
		"choice":         review.Restatement.Choice,
		"cost":           review.Restatement.Cost,
		"text_change":    review.Restatement.TextChange,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("review %s missing restatement %s", review.ReviewID, name)
		}
	}
	if len(review.Evidence) == 0 {
		return fmt.Errorf("review %s requires quoted prose evidence", review.ReviewID)
	}
	for _, evidence := range review.Evidence {
		if (evidence.Option != "A" && evidence.Option != "B") || strings.TrimSpace(evidence.Quote) == "" || strings.TrimSpace(evidence.Reason) == "" {
			return fmt.Errorf("review %s has invalid evidence citation", review.ReviewID)
		}
	}
	if review.FactErrors.A < 0 || review.FactErrors.B < 0 {
		return fmt.Errorf("review %s fact errors cannot be negative", review.ReviewID)
	}
	if review.AuthorEditRatio.A < 0 || review.AuthorEditRatio.A > 1 || review.AuthorEditRatio.B < 0 || review.AuthorEditRatio.B > 1 {
		return fmt.Errorf("review %s author edit ratios must be between 0 and 1", review.ReviewID)
	}
	for _, prior := range existing {
		if prior.ReviewID == review.ReviewID {
			return fmt.Errorf("duplicate review id %s", review.ReviewID)
		}
		if prior.SampleID == review.SampleID && prior.ReviewerID == review.ReviewerID {
			return fmt.Errorf("duplicate reviewer %s for sample %s", review.ReviewerID, review.SampleID)
		}
	}
	if review.Kind == ReviewKindIndependent {
		if len(review.ConflictReviewIDs) != 0 {
			return fmt.Errorf("independent review %s cannot name conflict reviews", review.ReviewID)
		}
		independentCount := 0
		for _, prior := range existing {
			if prior.SampleID == review.SampleID && prior.Kind == ReviewKindIndependent {
				independentCount++
			}
		}
		if independentCount >= 2 {
			return fmt.Errorf("sample %s already has two independent reviews", review.SampleID)
		}
		return nil
	}
	if len(review.ConflictReviewIDs) != 2 || review.ConflictReviewIDs[0] == review.ConflictReviewIDs[1] {
		return fmt.Errorf("adjudication %s must name two distinct conflict reviews", review.ReviewID)
	}
	byID := make(map[string]ReviewRecord, len(existing))
	for _, prior := range existing {
		byID[prior.ReviewID] = prior
	}
	left, leftOK := byID[review.ConflictReviewIDs[0]]
	right, rightOK := byID[review.ConflictReviewIDs[1]]
	if !leftOK || !rightOK || left.Kind != ReviewKindIndependent || right.Kind != ReviewKindIndependent || left.SampleID != review.SampleID || right.SampleID != review.SampleID {
		return fmt.Errorf("adjudication %s references invalid independent reviews", review.ReviewID)
	}
	if left.Decision == right.Decision {
		return fmt.Errorf("adjudication %s is unnecessary because independent reviews agree", review.ReviewID)
	}
	return nil
}

func loadReviews(runRoot, runID string) ([]ReviewRecord, error) {
	dir := filepath.Join(runRoot, runID, "private", "reviews")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	reviews := make([]ReviewRecord, 0, len(files))
	for _, file := range files {
		var review ReviewRecord
		if err := readStrictJSON(file, &review); err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}
