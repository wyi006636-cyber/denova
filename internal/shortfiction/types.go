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

type ConfirmationStatus string

const (
	ConfirmationWritten                 ConfirmationStatus = "written"
	ConfirmationWrittenCheckpointFailed ConfirmationStatus = "written_checkpoint_failed"
)

type CheckpointStatus string

const (
	CheckpointCreated CheckpointStatus = "created"
	CheckpointFailed  CheckpointStatus = "failed"
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

// ConfirmationCheckpoint identifies the exact manual version created for a confirmed write.
type ConfirmationCheckpoint struct {
	VersionID string `json:"version_id"`
	Source    string `json:"source"`
	Path      string `json:"path"`
	Revision  string `json:"revision"`
}

// ConfirmationResult distinguishes a complete confirmation from a durable
// workspace write whose exact version checkpoint failed.
type ConfirmationResult struct {
	Status           ConfirmationStatus      `json:"status"`
	CandidateID      string                  `json:"candidate_id"`
	WriteRevision    string                  `json:"write_revision"`
	ChangeGroupID    string                  `json:"change_group_id"`
	ChangeSetID      string                  `json:"change_set_id"`
	WorkspaceMutated bool                    `json:"workspace_mutated"`
	CheckpointStatus CheckpointStatus        `json:"checkpoint_status"`
	Checkpoint       *ConfirmationCheckpoint `json:"checkpoint,omitempty"`
	Retryable        bool                    `json:"retryable"`
}
