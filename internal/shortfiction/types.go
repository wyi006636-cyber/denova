package shortfiction

type ProfileID string

const (
	ProfileFanqieShort   ProfileID = "fanqie_short"
	FanqieProfileVersion           = "fanqie-short-v1"
	MissingRevision                = "missing"
	MaxBriefBytes                  = 256 * 1024
	MaxSourceBytes                 = 256 * 1024
	MaxCandidateBytes              = 1024 * 1024
)

// SourcePacket is the bounded, auditable source for one candidate preview.
type SourcePacket struct {
	Workspace    string `json:"workspace"`
	TargetPath   string `json:"target_path"`
	BaseRevision string `json:"base_revision"`
	Brief        string `json:"brief"`
	Source       string `json:"source"`
	Locale       string `json:"locale"`
}

// Generation is the transport-neutral result returned by a preview generator.
type Generation struct {
	PreviewMarkdown string `json:"preview_markdown"`
	ModelProfileID  string `json:"model_profile_id"`
	Model           string `json:"model"`
}

// GeneratedCandidate is an integrity-bound preview snapshot until explicit confirmation.
type GeneratedCandidate struct {
	ProfileID       ProfileID `json:"profile_id"`
	ProfileVersion  string    `json:"profile_version"`
	CandidateID     string    `json:"candidate_id"`
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

// ConfirmRequest carries the candidate selected by an explicit user action.
type ConfirmRequest struct {
	Candidate GeneratedCandidate `json:"candidate"`
}

// ConfirmationResult reports the checkpoint outcome without implying a workspace write.
type ConfirmationResult struct {
	CandidateID string `json:"candidate_id"`
	Confirmed   bool   `json:"confirmed"`
	Checkpoint  string `json:"checkpoint"`
}
