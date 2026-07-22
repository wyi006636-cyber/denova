package evaluation

type ProfileID string

const (
	ProfileLongSerial     ProfileID = "long_serial"
	ProfileFanqieShort    ProfileID = "fanqie_short"
	ProfileZhihuSaltShort ProfileID = "zhihu_salt_short"
)

var AllProfileIDs = []ProfileID{ProfileLongSerial, ProfileFanqieShort, ProfileZhihuSaltShort}

type TaskType string

const (
	TaskOpening         TaskType = "opening"
	TaskCharacterChoice TaskType = "character_choice"
	TaskDialogue        TaskType = "dialogue"
	TaskStructureTurn   TaskType = "structure_turn"
	TaskEnding          TaskType = "ending"
	TaskContinuity      TaskType = "continuity"
)

type LengthBucket string

const (
	LengthScene      LengthBucket = "scene"
	LengthChapter    LengthBucket = "chapter"
	LengthShortStory LengthBucket = "short_story"
)

type DataSplit string

const (
	SplitTuning         DataSplit = "tuning"
	SplitRegression     DataSplit = "regression"
	SplitReleaseHoldout DataSplit = "release_holdout"
)

type RunSelection struct {
	DataSplits []DataSplit `json:"data_splits"`
	TaskIDs    []string    `json:"task_ids,omitempty"`
}

type ResultStatus string

const (
	StatusReady              ResultStatus = "READY"
	StatusEnvironmentBlocked ResultStatus = "ENVIRONMENT-BLOCKED"
	StatusNotReady           ResultStatus = "NOT-READY"
	StatusNotEnoughData      ResultStatus = "NOT-ENOUGH-DATA"
	StatusValid              ResultStatus = "VALID"
	StatusFailed             ResultStatus = "FAILED"
)

type CorpusManifest struct {
	Contract     string           `json:"contract"`
	Version      string           `json:"version"`
	InputRoot    string           `json:"input_root"`
	ContractRoot string           `json:"contract_root"`
	RunRoot      string           `json:"run_root"`
	Baseline     BaselineProtocol `json:"baseline"`
	Tasks        []EvaluationTask `json:"tasks"`
}

type BaselineProtocol struct {
	Arm                     string `json:"arm"`
	TemplateVersion         string `json:"template_version"`
	TemplateFile            string `json:"template_file"`
	TemplateSHA256          string `json:"template_sha256"`
	ModelCallLimit          int    `json:"model_call_limit"`
	HarnessArtifactsAllowed bool   `json:"harness_artifacts_allowed"`
	ThinkingPersisted       bool   `json:"thinking_persisted"`
}

type EvaluationTask struct {
	ID                  string              `json:"task_id"`
	ProfileID           ProfileID           `json:"profile_id"`
	Genre               string              `json:"genre"`
	TaskType            TaskType            `json:"task_type"`
	Purpose             string              `json:"purpose"`
	LengthBucket        LengthBucket        `json:"length_bucket"`
	DataSplit           DataSplit           `json:"data_split"`
	AllowedInputs       []string            `json:"allowed_inputs"`
	InputFile           string              `json:"input_file"`
	InputSHA256         string              `json:"input_sha256"`
	Source              SourceRecord        `json:"source"`
	QualitySpec         QualitySpecSnapshot `json:"quality_spec"`
	ModelConfigSnapshot ModelConfigSnapshot `json:"model_config_snapshot"`
	ActualCostRecord    string              `json:"actual_cost_record"`
}

type SourceRecord struct {
	Kind                string `json:"kind"`
	Reference           string `json:"reference"`
	LicenseStatus       string `json:"license_status"`
	AnonymizationStatus string `json:"anonymization_status"`
}

type QualitySpecSnapshot struct {
	ContractFile   string   `json:"contract_file"`
	ContractSHA256 string   `json:"contract_sha256"`
	Goals          []string `json:"goals"`
}

type ModelConfigSnapshot struct {
	Provider         string          `json:"provider"`
	BaseURL          string          `json:"base_url"`
	ModelProfileID   string          `json:"model_profile_id"`
	Model            string          `json:"model"`
	CredentialSource string          `json:"credential_source"`
	Parameters       ModelParameters `json:"parameters"`
	SHA256           string          `json:"sha256"`
}

type ModelParameters struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"max_output_tokens"`
	ThinkingEnabled bool    `json:"thinking_enabled"`
}

func (snapshot ModelConfigSnapshot) hashPayload() any {
	return struct {
		Provider         string          `json:"provider"`
		BaseURL          string          `json:"base_url"`
		ModelProfileID   string          `json:"model_profile_id"`
		Model            string          `json:"model"`
		CredentialSource string          `json:"credential_source"`
		Parameters       ModelParameters `json:"parameters"`
	}{snapshot.Provider, snapshot.BaseURL, snapshot.ModelProfileID, snapshot.Model, snapshot.CredentialSource, snapshot.Parameters}
}

type UsageRecord struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens"`
	ModelCalls       int `json:"model_calls"`
}

type CostRecord struct {
	Status   string   `json:"status"`
	Currency string   `json:"currency,omitempty"`
	Amount   *float64 `json:"amount,omitempty"`
	Note     string   `json:"note,omitempty"`
}
