package writingcomparison

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"

	projectdomain "denova/internal/project"
)

// CandidateContent is one immutable candidate revision. Content is always the
// complete verified asset; the API never returns a partial body.
type CandidateContent struct {
	CandidateID string `json:"candidate_id"`
	Revision    int    `json:"revision"`
	SHA256      string `json:"sha256"`
	Content     string `json:"content"`
}

func (service *Service) CandidateContent(ctx context.Context, layout projectdomain.Layout, taskID, candidateID string, revision int) (CandidateContent, error) {
	task, err := service.Get(ctx, layout, taskID)
	if err != nil {
		return CandidateContent{}, err
	}
	var candidate *Candidate
	for index := range task.Candidates {
		if task.Candidates[index].ID == strings.TrimSpace(candidateID) {
			candidate = &task.Candidates[index]
			break
		}
	}
	if candidate == nil || candidate.Revision == nil {
		return CandidateContent{}, ErrNotFound
	}
	if revision > 0 && candidate.Revision.Number != revision {
		return CandidateContent{}, ErrNotFound
	}
	store := NewStore(layout)
	path, err := store.AssetPath(task.ID, candidate.Revision.Path)
	if err != nil {
		return CandidateContent{}, err
	}
	data, err := ReadVerifiedAsset(path, candidate.Revision.SHA256)
	if err != nil {
		if errors.Is(err, ErrAssetCorrupt) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			return CandidateContent{}, &Error{Code: CodeAssetCorrupt, Message: "candidate asset checksum mismatch", Cause: ErrAssetCorrupt}
		}
		return CandidateContent{}, err
	}
	return CandidateContent{CandidateID: candidate.ID, Revision: candidate.Revision.Number, SHA256: candidate.Revision.SHA256, Content: string(data)}, nil
}
