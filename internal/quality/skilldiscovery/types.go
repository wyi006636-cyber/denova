// Package skilldiscovery defines persisted contracts for public Xiaping catalog discovery.
package skilldiscovery

type SnapshotStatus string

const (
	SnapshotComplete SnapshotStatus = "COMPLETE"
	SnapshotPartial  SnapshotStatus = "PARTIAL"
)

type PageReceipt struct {
	Kind       string `json:"kind"`
	Key        string `json:"key"`
	URL        string `json:"url"`
	HTTPStatus int    `json:"http_status"`
	CapturedAt string `json:"captured_at"`
	SHA256     string `json:"sha256"`
	ItemCount  int    `json:"item_count"`
	Error      string `json:"error,omitempty"`
}
type SnapshotFailure struct {
	Kind        string `json:"kind"`
	Key         string `json:"key"`
	Disposition string `json:"disposition"`
	Message     string `json:"message"`
}
type SnapshotManifest struct {
	Contract               string            `json:"contract"`
	Version                string            `json:"version"`
	SnapshotID             string            `json:"snapshot_id"`
	Status                 SnapshotStatus    `json:"status"`
	StartedAt              string            `json:"started_at"`
	CompletedAt            string            `json:"completed_at"`
	BaseURL                string            `json:"base_url"`
	NormalizationVersion   string            `json:"normalization_version"`
	ReportedTotal          int               `json:"reported_total"`
	UniqueSkills           int               `json:"unique_skills"`
	Pages                  []PageReceipt     `json:"pages"`
	Failures               []SnapshotFailure `json:"failures"`
	PreviousSnapshotSHA256 string            `json:"previous_snapshot_sha256,omitempty"`
	SkillRecordsSHA256     string            `json:"skill_records_sha256"`
}
type SkillRecord struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Triggers       []string `json:"triggers"`
	Categories     []string `json:"categories"`
	Tags           []string `json:"tags"`
	OwnerID        string   `json:"owner_id"`
	OwnerName      string   `json:"owner_name"`
	CurrentVersion string   `json:"current_version"`
	Downloads      int      `json:"downloads"`
	AverageStars   int      `json:"average_stars_x100"`
	StarCount      int      `json:"star_count"`
	CommentCount   int      `json:"comment_count"`
	Featured       bool     `json:"featured"`
	PlatformStatus string   `json:"platform_status"`
	SecurityStatus string   `json:"security_status"`
	VersionCount   int      `json:"version_count"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	DetailURL      string   `json:"detail_url"`
}
type LocalSnapshot struct {
	Manifest SnapshotManifest `json:"manifest"`
	Skills   []SkillRecord    `json:"skills"`
}
type MatchStatus string

const (
	MatchMatched    MatchStatus = "MATCHED"
	MatchAmbiguous  MatchStatus = "AMBIGUOUS"
	MatchNotMatched MatchStatus = "NOT-MATCHED"
)

type FieldEvidence struct {
	Field string `json:"field"`
	Term  string `json:"term"`
}
type CapabilityMatch struct {
	CapabilityID string          `json:"capability_id"`
	Status       MatchStatus     `json:"status"`
	Evidence     []FieldEvidence `json:"evidence"`
}
type CandidateRecord struct {
	Skill        SkillRecord       `json:"skill"`
	Profiles     []string          `json:"profiles"`
	Capabilities []CapabilityMatch `json:"capabilities"`
}
type CandidateIndex struct {
	Contract   string            `json:"contract"`
	Version    string            `json:"version"`
	SnapshotID string            `json:"snapshot_id"`
	Candidates []CandidateRecord `json:"candidates"`
}
type CapabilityProposal struct {
	CapabilityID      string   `json:"capability_id"`
	NameZH            string   `json:"name_zh"`
	NameEN            string   `json:"name_en"`
	Inputs            []string `json:"inputs"`
	Outputs           []string `json:"outputs"`
	LifecycleStage    string   `json:"lifecycle_stage"`
	MinimumPermission string   `json:"minimum_permission"`
	EvaluationMethod  string   `json:"evaluation_method"`
	CandidateIDs      []string `json:"candidate_ids"`
}
type CapabilityProposalIndex struct {
	Contract   string               `json:"contract"`
	Version    string               `json:"version"`
	SnapshotID string               `json:"snapshot_id"`
	Proposals  []CapabilityProposal `json:"proposals"`
}
type DuplicateCluster struct {
	ClusterID        string   `json:"cluster_id"`
	Kind             string   `json:"kind"`
	RepresentativeID string   `json:"representative_id"`
	MemberIDs        []string `json:"member_ids"`
	Reasons          []string `json:"reasons"`
}
type DuplicateClusterIndex struct {
	Contract   string             `json:"contract"`
	Version    string             `json:"version"`
	SnapshotID string             `json:"snapshot_id"`
	Clusters   []DuplicateCluster `json:"clusters"`
}
type ReviewEvidence struct {
	EffectiveRaters     int      `json:"effective_raters"`
	SubstantiveComments int      `json:"substantive_comments"`
	DuplicateComments   int      `json:"duplicate_comments"`
	OwnerSelfReviews    int      `json:"owner_self_reviews"`
	AverageStarsX100    int      `json:"average_stars_x100"`
	PlatformQualityMean *float64 `json:"platform_quality_mean,omitempty"`
	AnomalyFlags        []string `json:"anomaly_flags"`
	EvidenceCacheStatus string   `json:"-"`
}
type EvidenceVector struct {
	SkillID              string         `json:"skill_id"`
	CapabilityID         string         `json:"capability_id"`
	DownloadPercentile   float64        `json:"download_percentile"`
	BayesianStarsX100    float64        `json:"bayesian_stars_x100"`
	Review               ReviewEvidence `json:"review"`
	PlatformDataRich     bool           `json:"platform_data_rich"`
	MaturityVersionCount int            `json:"maturity_version_count"`
	EvidenceCacheStatus  string         `json:"evidence_cache_status"`
}
type ShortlistLane string

const (
	LaneDataRich    ShortlistLane = "DATA-RICH"
	LaneExploration ShortlistLane = "EXPLORATION"
)

type ShortlistEntry struct {
	SkillID      string         `json:"skill_id"`
	CapabilityID string         `json:"capability_id"`
	Lane         ShortlistLane  `json:"lane"`
	Rank         int            `json:"rank"`
	Reasons      []string       `json:"reasons"`
	Evidence     EvidenceVector `json:"evidence"`
}
type CapabilityGap struct {
	CapabilityID string `json:"capability_id"`
	Wanted       int    `json:"wanted"`
	Selected     int    `json:"selected"`
	Reason       string `json:"reason"`
}
type Shortlist struct {
	Contract   string           `json:"contract"`
	Version    string           `json:"version"`
	SnapshotID string           `json:"snapshot_id"`
	Entries    []ShortlistEntry `json:"entries"`
	Gaps       []CapabilityGap  `json:"gaps"`
}

// CoreCapabilityIDs is the approved catalog's closed capability set, in catalog order.
var CoreCapabilityIDs = []string{
	"ideation.generate-premises", "project.bootstrap-genre", "outline.build-long-arc", "outline.build-short-structure", "outline.simulate-multiline", "outline.build-execution-brief", "character.build-profile", "character.build-dialogue-voice", "opening.review-hook", "engagement.review-reading-drive", "plot.manage-foreshadowing", "continuity.review-facts", "editor.review-story", "editor.review-profile-rubric", "style.revise-naturalness", "style.revise-prose",
}
