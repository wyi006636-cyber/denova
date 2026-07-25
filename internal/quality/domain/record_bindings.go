package domain

// ArtifactRecordContract is the exact envelope shared by CandidateSet and
// ReviewIssue. PreferenceSignal deliberately uses a distinct schema_id field.
type ArtifactRecordContract struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Schema  string `json:"schema"`
}

type WorkspaceBinding struct {
	WorkspaceID string `json:"workspace_id"`
	Revision    int    `json:"revision"`
	Hash        string `json:"hash"`
}

type RunBinding struct {
	RunID           string `json:"run_id"`
	ContractVersion string `json:"contract_version"`
}

type StageBinding struct {
	StageID         string `json:"stage_id"`
	StageType       string `json:"stage_type"`
	ContractVersion string `json:"contract_version"`
}

type ArtifactBinding struct {
	ArtifactID      string `json:"artifact_id"`
	ArtifactType    string `json:"artifact_type"`
	ContractVersion string `json:"contract_version"`
	Hash            string `json:"hash"`
}

type VersionedHashBinding struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

type ProfileBinding struct {
	ProfileID       ProfileID `json:"profile_id"`
	ContractVersion string    `json:"contract_version"`
	Hash            string    `json:"hash"`
}

type QualitySpecBinding struct {
	SpecID          string `json:"spec_id"`
	Revision        int    `json:"revision"`
	ContractVersion string `json:"contract_version"`
	Hash            string `json:"hash"`
}

type ByteRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type RecordActor struct {
	ActorID   string `json:"actor_id"`
	ActorType string `json:"actor_type"`
}
