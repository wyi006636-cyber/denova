package shortfiction

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
)

type candidateHashInput struct {
	ProfileID       ProfileID `json:"profile_id"`
	ProfileVersion  string    `json:"profile_version"`
	Workspace       string    `json:"workspace"`
	TargetPath      string    `json:"target_path"`
	BaseRevision    string    `json:"base_revision"`
	Brief           string    `json:"brief"`
	Source          string    `json:"source"`
	Locale          string    `json:"locale"`
	PreviewMarkdown string    `json:"preview_markdown"`
	ModelProfileID  string    `json:"model_profile_id"`
	Model           string    `json:"model"`
}

func NewCandidate(source SourcePacket, generation Generation) (GeneratedCandidate, error) {
	if strings.TrimSpace(source.Brief) == "" {
		return GeneratedCandidate{}, NewError(ErrorCodeInvalidSource, "brief is required", nil)
	}
	if !validBaseRevision(source.BaseRevision) {
		return GeneratedCandidate{}, NewError(ErrorCodeInvalidSource, "base revision must use the workspace change format", nil)
	}
	if len(source.Brief) > MaxBriefBytes {
		return GeneratedCandidate{}, NewError(ErrorCodeOversized, "brief exceeds the maximum size", map[string]any{"max_bytes": MaxBriefBytes})
	}
	if len(source.Source) > MaxSourceBytes {
		return GeneratedCandidate{}, NewError(ErrorCodeOversized, "source exceeds the maximum size", map[string]any{"max_bytes": MaxSourceBytes})
	}
	if len(generation.PreviewMarkdown) > MaxCandidateBytes {
		return GeneratedCandidate{}, NewError(ErrorCodeOversized, "candidate preview exceeds the maximum size", map[string]any{"max_bytes": MaxCandidateBytes})
	}
	var err error
	source, err = normalizeSourcePacket(source)
	if err != nil {
		return GeneratedCandidate{}, err
	}
	candidate := GeneratedCandidate{
		ProfileID:       ProfileFanqieShort,
		ProfileVersion:  FanqieProfileVersion,
		Workspace:       source.Workspace,
		TargetPath:      source.TargetPath,
		BaseRevision:    source.BaseRevision,
		Brief:           source.Brief,
		Source:          source.Source,
		Locale:          source.Locale,
		PreviewMarkdown: generation.PreviewMarkdown,
		ModelProfileID:  generation.ModelProfileID,
		Model:           generation.Model,
	}
	candidate.CandidateID, err = candidateID(candidate)
	if err != nil {
		return GeneratedCandidate{}, err
	}
	return candidate, nil
}

func validBaseRevision(revision string) bool {
	if revision == MissingRevision {
		return true
	}
	const prefix = "sha256:"
	if !strings.HasPrefix(revision, prefix) || len(revision) != len(prefix)+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(revision[len(prefix):])
	return err == nil
}

func normalizeSourcePacket(source SourcePacket) (SourcePacket, error) {
	workspace, err := filepath.Abs(source.Workspace)
	if err != nil {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "workspace cannot be resolved", nil)
	}
	source.Workspace = filepath.Clean(workspace)

	path := filepath.FromSlash(source.TargetPath)
	if filepath.IsAbs(path) {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "target path must be relative", nil)
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if filepath.Ext(path) != ".md" {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "target path must be a Markdown file", nil)
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." || strings.HasPrefix(segment, ".") {
			return SourcePacket{}, NewError(ErrorCodeInvalidSource, "target path contains a forbidden segment", nil)
		}
	}
	source.TargetPath = path
	return source, nil
}

func ValidateCandidate(candidate GeneratedCandidate) error {
	if candidate.ProfileID != ProfileFanqieShort || candidate.ProfileVersion != FanqieProfileVersion {
		return NewError(ErrorCodeInvalidProfile, "candidate profile is not supported", nil)
	}
	id, err := candidateID(candidate)
	if err != nil {
		return err
	}
	if candidate.CandidateID != id {
		return NewError(ErrorCodeCandidateMismatch, "candidate content does not match candidate_id", nil)
	}
	return nil
}

func candidateID(candidate GeneratedCandidate) (string, error) {
	data, err := json.Marshal(candidateHashInput{
		ProfileID:       candidate.ProfileID,
		ProfileVersion:  candidate.ProfileVersion,
		Workspace:       candidate.Workspace,
		TargetPath:      candidate.TargetPath,
		BaseRevision:    candidate.BaseRevision,
		Brief:           candidate.Brief,
		Source:          candidate.Source,
		Locale:          candidate.Locale,
		PreviewMarkdown: candidate.PreviewMarkdown,
		ModelProfileID:  candidate.ModelProfileID,
		Model:           candidate.Model,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
