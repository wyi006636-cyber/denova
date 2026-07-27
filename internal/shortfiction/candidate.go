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
	var err error
	source, err = validateAuthority(source, generation.PreviewMarkdown, false)
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

func validateAuthority(source SourcePacket, previewMarkdown string, requireCanonical bool) (SourcePacket, error) {
	if strings.TrimSpace(source.Brief) == "" {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "brief is required", nil)
	}
	if !validBaseRevision(source.BaseRevision) {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "base revision must use the workspace change format", nil)
	}
	if len(source.Brief) > MaxBriefBytes {
		return SourcePacket{}, NewError(ErrorCodeOversized, "brief exceeds the maximum size", map[string]any{"max_bytes": MaxBriefBytes})
	}
	if len(source.Source) > MaxSourceBytes {
		return SourcePacket{}, NewError(ErrorCodeOversized, "source exceeds the maximum size", map[string]any{"max_bytes": MaxSourceBytes})
	}
	if len(previewMarkdown) > MaxCandidateBytes {
		return SourcePacket{}, NewError(ErrorCodeOversized, "candidate preview exceeds the maximum size", map[string]any{"max_bytes": MaxCandidateBytes})
	}
	normalized, err := normalizeSourcePacket(source)
	if err != nil {
		return SourcePacket{}, err
	}
	if requireCanonical && (source.Workspace != normalized.Workspace || source.TargetPath != normalized.TargetPath) {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "candidate authority is not canonical", nil)
	}
	return normalized, nil
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

	rawTarget := source.TargetPath
	hasDrivePrefix := len(rawTarget) >= 2 && rawTarget[1] == ':' &&
		((rawTarget[0] >= 'A' && rawTarget[0] <= 'Z') || (rawTarget[0] >= 'a' && rawTarget[0] <= 'z'))
	if strings.Contains(rawTarget, `\`) || hasDrivePrefix {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "target path uses unsupported platform syntax", nil)
	}
	target := filepath.FromSlash(rawTarget)
	if filepath.IsAbs(target) {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "target path must be relative", nil)
	}
	if hasForbiddenTargetSegment(target) {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "target path contains a forbidden segment", nil)
	}

	target = filepath.ToSlash(filepath.Clean(target))
	if filepath.IsAbs(filepath.FromSlash(target)) || hasForbiddenTargetSegment(filepath.FromSlash(target)) {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "target path is not workspace-relative", nil)
	}
	if filepath.Ext(target) != ".md" {
		return SourcePacket{}, NewError(ErrorCodeInvalidSource, "target path must be a Markdown file", nil)
	}
	source.TargetPath = target
	return source, nil
}

func hasForbiddenTargetSegment(target string) bool {
	for _, segment := range strings.Split(target, string(filepath.Separator)) {
		if segment == ".." || strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func ValidateCandidate(candidate GeneratedCandidate) error {
	if candidate.ProfileID != ProfileFanqieShort || candidate.ProfileVersion != FanqieProfileVersion {
		return NewError(ErrorCodeInvalidProfile, "candidate profile is not supported", nil)
	}
	_, err := validateAuthority(SourcePacket{
		Workspace:    candidate.Workspace,
		TargetPath:   candidate.TargetPath,
		BaseRevision: candidate.BaseRevision,
		Brief:        candidate.Brief,
		Source:       candidate.Source,
		Locale:       candidate.Locale,
	}, candidate.PreviewMarkdown, true)
	if err != nil {
		return err
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
