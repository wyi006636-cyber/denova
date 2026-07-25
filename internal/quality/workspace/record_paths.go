package workspace

import (
	"fmt"
	"path"
	"strings"
)

const (
	candidateSetRecordRoot = ".denova/quality/artifacts/candidate-sets"
	reviewIssueRecordRoot  = ".denova/quality/artifacts/review-issues"
	preferenceJournalPath  = ".denova/quality/preferences.jsonl"
)

func CandidateSetRelativePath(id string) (string, error) {
	return artifactRecordRelativePath(candidateSetRecordRoot, id)
}

func ReviewIssueRelativePath(id string) (string, error) {
	return artifactRecordRelativePath(reviewIssueRecordRoot, id)
}

func artifactRecordRelativePath(root, id string) (string, error) {
	if id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("record ID must be one portable segment: %q", id)
	}
	validated, err := ValidateRelativePath(id, PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows, Limits: PathLimits{MaxPathBytes: artifactIDMaxBytes, MaxSegmentBytes: artifactIDMaxBytes}})
	if err != nil {
		return "", err
	}
	return path.Join(root, validated+".json"), nil
}
