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

var forbiddenDiscoveryContent = regexp.MustCompile(`(?i)"(?:reviewer_?id|review_?id|raw_?comments?|comments?|packages?|package_?contents?)"\s*:`)

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
		{"xiaping-snapshot-manifest-v1.json", manifest},
		{"xiaping-writing-candidates-v1.json", CandidateIndex{Contract: candidateIndexContract, Version: "v1", SnapshotID: artifacts.Manifest.SnapshotID, Candidates: artifacts.Candidates}},
		{"xiaping-capability-proposals-v1.json", CapabilityProposalIndex{Contract: proposalIndexContract, Version: "v1", SnapshotID: artifacts.Manifest.SnapshotID, Proposals: artifacts.Proposals}},
		{"xiaping-duplicate-clusters-v1.json", DuplicateClusterIndex{Contract: clusterIndexContract, Version: "v1", SnapshotID: artifacts.Manifest.SnapshotID, Clusters: artifacts.Clusters}},
		{"xiaping-evidence-shortlist-v1.json", artifacts.Shortlist},
	}
	for _, write := range writes {
		if err := WriteJSONArtifact(filepath.Join(root, write.name), write.value); err != nil {
			return fmt.Errorf("write %s: %w", write.name, err)
		}
	}
	report, err := RenderEvidenceReport(artifacts)
	if err != nil {
		return err
	}
	if err := writeDiscoveryReport(filepath.Join(root, "XIAPING_EVIDENCE_REPORT.md"), report); err != nil {
		return err
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
	for _, vector := range artifacts.Evidence {
		if vector.SkillID == "" || vector.CapabilityID == "" {
			return fmt.Errorf("evidence vector has required empty field")
		}
	}
	for _, entry := range artifacts.Shortlist.Entries {
		if entry.Evidence.SkillID != entry.SkillID || entry.Evidence.CapabilityID != entry.CapabilityID {
			return fmt.Errorf("shortlist entry evidence does not match identity")
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
	if regexp.MustCompile(`"\$ref"\s*:\s*"https?://`).Match(schemaBytes) {
		return fmt.Errorf("schema must not contain remote refs")
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
		anomalies += len(entry.Evidence.Review.AnomalyFlags)
	}
	var builder strings.Builder
	builder.WriteString("# Xiaping Evidence Discovery Report / 霞萍证据发现报告\n\n")
	fmt.Fprintf(&builder, "- Snapshot / 快照: `%s`\n- Completeness / 完整性: `%s`\n- Candidate count / 候选数量: %d\n- Proposal count / 提案数量: %d\n- Duplicate clusters / 重复簇数量: %d\n- Anomaly facts / 异常事实: %d\n- DATA-RICH lane / 数据充足通道: %d\n- EXPLORATION lane / 探索通道: %d\n\n", artifacts.Manifest.SnapshotID, artifacts.Manifest.Status, len(artifacts.Candidates), len(artifacts.Proposals), len(artifacts.Clusters), anomalies, dataRich, exploration)
	builder.WriteString("## Coverage and gaps / 覆盖与缺口\n\n")
	for _, capability := range capabilities {
		fmt.Fprintf(&builder, "- `%s`: %d selected / 已选 %d\n", capability.id, capability.count, capability.count)
	}
	if len(artifacts.Shortlist.Gaps) == 0 {
		builder.WriteString("- No recorded gaps / 无记录缺口\n")
	} else {
		for _, gap := range artifacts.Shortlist.Gaps {
			fmt.Fprintf(&builder, "- `%s`: %d/%d — %s / %d/%d — %s\n", gap.CapabilityID, gap.Selected, gap.Wanted, gap.Reason, gap.Selected, gap.Wanted, gap.Reason)
		}
	}
	builder.WriteString("\n## Evidence boundaries and limitations / 证据边界与限制\n\n")
	builder.WriteString("- Source linkage is the completed snapshot manifest, its page receipts, and existing SHA-256 receipts; no artifact hashes itself. / 来源关联为完整快照清单、页面回执及既有 SHA-256 回执；工件不对自身哈希。\n")
	builder.WriteString("- Bounded inputs are candidate metadata, proposals, duplicate clusters, evidence vectors, and shortlist entries only; raw review content, reviewer identifiers, signed URLs, and package contents are excluded. / 有界输入仅包括候选元数据、提案、重复簇、证据向量和短名单条目；不包含原始评论、评审者标识、签名 URL 或软件包内容。\n")
	builder.WriteString("- Platform evidence is not a writing-quality result.\n- 平台证据不是写作质量结果。\n")
	return []byte(builder.String()), nil
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
