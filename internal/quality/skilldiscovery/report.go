package skilldiscovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	candidateIndexContract = "denova.xiaping-writing-candidates"
	proposalIndexContract  = "denova.xiaping-capability-proposals"
	clusterIndexContract   = "denova.xiaping-duplicate-clusters"
)
const artifactMaxBytes = 262144

var artifactNames = []string{"xiaping-snapshot-manifest-v1.json", "xiaping-writing-candidates-v1.json", "xiaping-capability-proposals-v1.json", "xiaping-duplicate-clusters-v1.json", "xiaping-evidence-shortlist-v1.json", "XIAPING_EVIDENCE_REPORT.md"}
var artifactRename = os.Rename

var forbiddenDiscoveryContent = regexp.MustCompile(`(?i)(?:"(?:reviewer|user|avatar)[^"\n]{0,32}"\s*:|"(?:raw_?comments?|package_?contents?|packages?)"\s*:|(?:sign|signature|token|credential|private[ _-]?key)=)`)

// DiscoveryArtifacts is the bounded, evidence-only input for offline committed artifacts.
type DiscoveryArtifacts struct {
	Manifest   SnapshotManifest
	Candidates []CandidateRecord
	Proposals  []CapabilityProposal
	Clusters   []DuplicateCluster
	Evidence   []EvidenceVector
	Shortlist  Shortlist
}

// WriteDiscoveryArtifacts validates and atomically writes the six Xiaping discovery outputs.
func WriteDiscoveryArtifacts(root string, artifacts DiscoveryArtifacts) error {
	if err := validateDiscoveryArtifacts(artifacts); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create artifact root: %w", err)
	}
	manifest := artifacts.Manifest
	if manifest.Pages == nil {
		manifest.Pages = []PageReceipt{}
	}
	if manifest.Failures == nil {
		manifest.Failures = []SnapshotFailure{}
	}
	writes := []struct {
		name  string
		value any
	}{
		{"xiaping-snapshot-manifest-v1.json", withProvenance(manifest, artifacts.Manifest, "snapshot manifest")},
		{"xiaping-writing-candidates-v1.json", CandidateIndex{Contract: candidateIndexContract, Version: "v1", SnapshotID: artifacts.Manifest.SnapshotID, Candidates: normalizedCandidates(artifacts.Candidates), Provenance: artifactProvenance(artifacts.Manifest, "candidate index")}},
		{"xiaping-capability-proposals-v1.json", CapabilityProposalIndex{Contract: proposalIndexContract, Version: "v1", SnapshotID: artifacts.Manifest.SnapshotID, Proposals: artifacts.Proposals, Provenance: artifactProvenance(artifacts.Manifest, "capability proposals")}},
		{"xiaping-duplicate-clusters-v1.json", DuplicateClusterIndex{Contract: clusterIndexContract, Version: "v1", SnapshotID: artifacts.Manifest.SnapshotID, Clusters: artifacts.Clusters, Provenance: artifactProvenance(artifacts.Manifest, "duplicate clusters")}},
		{"xiaping-evidence-shortlist-v1.json", withProvenance(artifacts.Shortlist, artifacts.Manifest, "evidence shortlist")},
	}
	report, err := RenderEvidenceReport(artifacts)
	if err != nil {
		return err
	}
	payloads := map[string][]byte{}
	for _, write := range writes {
		b, e := json.MarshalIndent(write.value, "", "  ")
		if e != nil {
			return e
		}
		payloads[write.name] = append(b, '\n')
	}
	payloads[artifactNames[5]] = report
	return publishArtifactTransaction(root, payloads)
}
func normalizedCandidates(input []CandidateRecord) []CandidateRecord {
	out := append([]CandidateRecord(nil), input...)
	for i := range out {
		s := &out[i].Skill
		if s.Triggers == nil {
			s.Triggers = []string{}
		}
		if s.Categories == nil {
			s.Categories = []string{}
		}
		if s.Tags == nil {
			s.Tags = []string{}
		}
		if out[i].Profiles == nil {
			out[i].Profiles = []string{}
		}
		if out[i].Capabilities == nil {
			out[i].Capabilities = []CapabilityMatch{}
		}
		for j := range out[i].Capabilities {
			if out[i].Capabilities[j].Evidence == nil {
				out[i].Capabilities[j].Evidence = []FieldEvidence{}
			}
		}
	}
	return out
}

func artifactProvenance(manifest SnapshotManifest, purpose string) ArtifactProvenance {
	return ArtifactProvenance{Source: "completed snapshot manifest receipts", Purpose: purpose, InputSHA256: manifest.SkillRecordsSHA256, MaxBytes: artifactMaxBytes}
}
func withProvenance[T any](value T, manifest SnapshotManifest, purpose string) T {
	switch v := any(&value).(type) {
	case *SnapshotManifest:
		v.Provenance = artifactProvenance(manifest, purpose)
	case *Shortlist:
		v.Provenance = artifactProvenance(manifest, purpose)
	}
	return value
}
func publishArtifactTransaction(root string, payloads map[string][]byte) error {
	parent := filepath.Dir(root)
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(root)+"-xiaping-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, name := range artifactNames {
		b := payloads[name]
		if len(b) == 0 || len(b) > artifactMaxBytes {
			return fmt.Errorf("artifact %s exceeds bounded size", name)
		}
		if err := rejectDiscoveryBytes(b); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(stage, name), b, 0o600); err != nil {
			return err
		}
	}
	backups := map[string]string{}
	published := []string{}
	rollback := func() {
		for _, name := range published {
			_ = os.Remove(filepath.Join(root, name))
		}
		for name, b := range backups {
			_ = artifactRename(b, filepath.Join(root, name))
		}
	}
	for _, name := range artifactNames {
		target := filepath.Join(root, name)
		if _, err := os.Stat(target); err == nil {
			backup := filepath.Join(stage, "backup-"+name)
			if err := artifactRename(target, backup); err != nil {
				rollback()
				return fmt.Errorf("backup %s: %w", name, err)
			}
			backups[name] = backup
		}
		if err := artifactRename(filepath.Join(stage, name), target); err != nil {
			rollback()
			return fmt.Errorf("publish %s: %w", name, err)
		}
		published = append(published, name)
	}
	return nil
}

func validateDiscoveryArtifacts(artifacts DiscoveryArtifacts) error {
	if err := ValidateSnapshotManifest(artifacts.Manifest); err != nil {
		return fmt.Errorf("validate manifest provenance: %w", err)
	}
	if artifacts.Manifest.Status != SnapshotComplete {
		return fmt.Errorf("complete snapshot required for artifacts")
	}
	if artifacts.Shortlist.Contract != shortlistContract || artifacts.Shortlist.Version != "v1" || artifacts.Shortlist.SnapshotID != artifacts.Manifest.SnapshotID {
		return fmt.Errorf("shortlist snapshot identity does not match manifest")
	}
	if err := ValidateSkillRecords(candidateSkills(artifacts.Candidates)); err != nil {
		return fmt.Errorf("validate candidate source linkage: %w", err)
	}
	vectors := map[string]EvidenceVector{}
	for _, vector := range artifacts.Evidence {
		if vector.SkillID == "" || vector.CapabilityID == "" {
			return fmt.Errorf("evidence vector has required empty field")
		}
		key := vector.SkillID + "\x00" + vector.CapabilityID
		candidate, exists := candidateByArtifactID(artifacts.Candidates, vector.SkillID)
		if !exists || !credibleWritingMatch(candidate, vector.CapabilityID) {
			return fmt.Errorf("evidence vector capability is not a credible candidate match")
		}
		if _, ok := vectors[key]; ok {
			return fmt.Errorf("duplicate evidence vector")
		}
		vectors[key] = vector
	}
	entries := map[string]struct{}{}
	for _, entry := range artifacts.Shortlist.Entries {
		if entry.Evidence.SkillID != entry.SkillID || entry.Evidence.CapabilityID != entry.CapabilityID {
			return fmt.Errorf("shortlist entry evidence does not match identity")
		}
		key := entry.SkillID + "\x00" + entry.CapabilityID
		candidate, exists := candidateByArtifactID(artifacts.Candidates, entry.SkillID)
		if !exists || !credibleWritingMatch(candidate, entry.CapabilityID) {
			return fmt.Errorf("shortlist entry capability is not a credible candidate match")
		}
		if _, ok := entries[key]; ok {
			return fmt.Errorf("duplicate shortlist entry")
		}
		entries[key] = struct{}{}
		vector, ok := vectors[key]
		if !ok {
			return fmt.Errorf("shortlist entry has missing vector")
		}
		if !equalEvidence(vector, entry.Evidence) {
			return fmt.Errorf("shortlist entry evidence is not exact supplied vector")
		}
	}
	encoded, err := json.Marshal(artifacts)
	if err != nil {
		return fmt.Errorf("marshal artifact bounds: %w", err)
	}
	if err := rejectSensitiveArtifact(string(encoded)); err != nil {
		return err
	}
	if forbiddenDiscoveryContent.Match(encoded) {
		return fmt.Errorf("refusing artifact containing raw review or package content")
	}
	return nil
}
func candidateByArtifactID(candidates []CandidateRecord, id string) (CandidateRecord, bool) {
	for _, candidate := range candidates {
		if candidate.Skill.ID == id {
			return candidate, true
		}
	}
	return CandidateRecord{}, false
}
func equalEvidence(left, right EvidenceVector) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return bytes.Equal(a, b)
}
func rejectDiscoveryBytes(b []byte) error {
	if err := rejectSensitiveArtifact(string(b)); err != nil {
		return err
	}
	if forbiddenDiscoveryContent.Match(b) {
		return fmt.Errorf("refusing artifact containing forbidden review/package/identity content")
	}
	return nil
}

func candidateSkills(candidates []CandidateRecord) []SkillRecord {
	result := make([]SkillRecord, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.Skill)
	}
	return result
}

func writeDiscoveryReport(path string, report []byte) error {
	if err := rejectSensitiveArtifact(string(report)); err != nil {
		return err
	}
	if forbiddenDiscoveryContent.Match(report) {
		return fmt.Errorf("refusing report containing raw review or package content")
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".skilldiscovery-*.md")
	if err != nil {
		return fmt.Errorf("create report temp file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(report); err != nil {
		temporary.Close()
		return fmt.Errorf("write report temp file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close report temp file: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("rename report temp file: %w", err)
	}
	return nil
}

// ValidateArtifactSchema compiles a local schema and validates each artifact without remote references.
func ValidateArtifactSchema(schemaPath string, artifactPaths []string) error {
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(schemaBytes, &decoded); err != nil {
		return fmt.Errorf("decode schema: %w", err)
	}
	if err := validateLocalRefs(decoded); err != nil {
		return err
	}
	schemaDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return fmt.Errorf("decode schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", schemaDocument); err != nil {
		return fmt.Errorf("add schema: %w", err)
	}
	if err := compiler.AddResource(schemaPath, schemaDocument); err != nil {
		return fmt.Errorf("add local schema: %w", err)
	}
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}
	for _, path := range artifactPaths {
		payload, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read artifact %s: %w", path, err)
		}
		var document any
		if err := json.Unmarshal(payload, &document); err != nil {
			return fmt.Errorf("decode artifact %s: %w", path, err)
		}
		if err := schema.Validate(document); err != nil {
			return fmt.Errorf("validate artifact %s: %w", path, err)
		}
	}
	return nil
}
func validateLocalRefs(value any) error {
	switch current := value.(type) {
	case map[string]any:
		for key, item := range current {
			if key == "$ref" {
				ref, ok := item.(string)
				if !ok || !strings.HasPrefix(ref, "#") {
					return fmt.Errorf("schema must not contain remote refs")
				}
			}
			if err := validateLocalRefs(item); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range current {
			if err := validateLocalRefs(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// RenderEvidenceReport returns a deterministic bilingual, evidence-only markdown summary.
func RenderEvidenceReport(artifacts DiscoveryArtifacts) ([]byte, error) {
	if err := validateDiscoveryArtifacts(artifacts); err != nil {
		return nil, err
	}
	capabilities := reportCapabilities(artifacts)
	dataRich, exploration, anomalies := 0, 0, 0
	for _, entry := range artifacts.Shortlist.Entries {
		if entry.Lane == LaneDataRich {
			dataRich++
		}
		if entry.Lane == LaneExploration {
			exploration++
		}
	}
	for _, vector := range artifacts.Evidence {
		anomalies += len(vector.Review.AnomalyFlags)
	}
	var builder strings.Builder
	builder.WriteString("# Xiaping Evidence Discovery Report / 霞萍证据发现报告\n\n")
	fmt.Fprintf(&builder, "- Snapshot / 快照: `%s`\n- Completeness / 完整性: `%s`\n- Candidate count / 候选数量: %d\n- Proposal count / 提案数量: %d\n- Duplicate clusters / 重复簇数量: %d\n- Anomaly facts / 异常事实: %d\n- DATA-RICH lane / 数据充足通道: %d\n- EXPLORATION lane / 探索通道: %d\n\n", markdownSafe(artifacts.Manifest.SnapshotID), markdownSafe(string(artifacts.Manifest.Status)), len(artifacts.Candidates), len(artifacts.Proposals), len(artifacts.Clusters), anomalies, dataRich, exploration)
	builder.WriteString("## Coverage and gaps / 覆盖与缺口\n\n")
	for _, capability := range capabilities {
		fmt.Fprintf(&builder, "- `%s`: %d selected / 已选 %d\n", markdownSafe(capability.id), capability.count, capability.count)
	}
	if len(artifacts.Shortlist.Gaps) == 0 {
		builder.WriteString("- No recorded gaps / 无记录缺口\n")
	} else {
		for _, gap := range artifacts.Shortlist.Gaps {
			fmt.Fprintf(&builder, "- `%s`: %d/%d — %s / %d/%d — %s\n", markdownSafe(gap.CapabilityID), gap.Selected, gap.Wanted, markdownSafe(gap.Reason), gap.Selected, gap.Wanted, markdownSafe(gap.Reason))
		}
	}
	builder.WriteString("\n## Evidence boundaries and limitations / 证据边界与限制\n\n")
	provenance := artifactProvenance(artifacts.Manifest, "committed discovery artifacts")
	fmt.Fprintf(&builder, "- Source / 来源: %s / %s\n- Purpose / 用途: %s / %s\n- Input SHA-256 / 输入 SHA-256: `%s` / `%s`\n- Maximum bytes / 最大字节数: %d / %d\n", provenance.Source, provenance.Source, provenance.Purpose, provenance.Purpose, provenance.InputSHA256, provenance.InputSHA256, provenance.MaxBytes, provenance.MaxBytes)
	builder.WriteString("- Source linkage is the completed snapshot manifest, its page receipts, and existing SHA-256 receipts; no artifact hashes itself. / 来源关联为完整快照清单、页面回执及既有 SHA-256 回执；工件不对自身哈希。\n")
	builder.WriteString("- Bounded inputs are candidate metadata, proposals, duplicate clusters, evidence vectors, and shortlist entries only; raw review content, reviewer identifiers, signed URLs, and package contents are excluded. / 有界输入仅包括候选元数据、提案、重复簇、证据向量和短名单条目；不包含原始评论、评审者标识、签名 URL 或软件包内容。\n")
	builder.WriteString("- Platform evidence is not a writing-quality result.\n- 平台证据不是写作质量结果。\n")
	return []byte(builder.String()), nil
}
func markdownSafe(value string) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "|", "\\|", "`", "'", "<", "&lt;", ">", "&gt;", "&", "&amp;").Replace(value)
	return strings.TrimSpace(value)
}

type capabilityCount struct {
	id    string
	count int
}

func reportCapabilities(artifacts DiscoveryArtifacts) []capabilityCount {
	counts := map[string]int{}
	for _, entry := range artifacts.Shortlist.Entries {
		counts[entry.CapabilityID]++
	}
	for _, gap := range artifacts.Shortlist.Gaps {
		if _, found := counts[gap.CapabilityID]; !found {
			counts[gap.CapabilityID] = 0
		}
	}
	result := make([]capabilityCount, 0, len(counts))
	for id, count := range counts {
		result = append(result, capabilityCount{id, count})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}
