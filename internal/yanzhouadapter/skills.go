package yanzhouadapter

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"denova/internal/yanzhouprotocol"
)

type SkillSource string

const (
	SkillSourceBuiltin   SkillSource = "builtin"
	SkillSourceUser      SkillSource = "user"
	SkillSourceWorkspace SkillSource = "workspace"
)

var skillChecksumPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type SkillManifest struct {
	SchemaVersion          string      `json:"schemaVersion"`
	ID                     string      `json:"id"`
	Name                   string      `json:"name"`
	Revision               int         `json:"revision"`
	Source                 SkillSource `json:"source"`
	AgentKinds             []string    `json:"agentKinds"`
	CompatibleCapabilities []string    `json:"compatibleCapabilities"`
	Categories             []string    `json:"categories"`
	Summary                string      `json:"summary"`
	EntryResource          string      `json:"entryResource"`
	SupportResources       []string    `json:"supportResources,omitempty"`
	RequestedCapabilities  []string    `json:"requestedCapabilities,omitempty"`
	Checksum               string      `json:"checksum"`
}

type SkillCatalogSummary struct {
	ID                     string      `json:"id"`
	Name                   string      `json:"name"`
	Revision               int         `json:"revision"`
	Source                 SkillSource `json:"source"`
	AgentKinds             []string    `json:"agentKinds"`
	CompatibleCapabilities []string    `json:"compatibleCapabilities"`
	Categories             []string    `json:"categories"`
	Summary                string      `json:"summary"`
}

type SkillCatalogFilter struct {
	AgentKind    string
	Capabilities []string
	Overrides    map[string]bool
}

type SkillLoadRequest struct {
	SchemaVersion    string          `json:"schemaVersion"`
	RunID            string          `json:"runId"`
	ID               string          `json:"id"`
	ExpectedRevision int             `json:"expectedRevision"`
	AgentKind        string          `json:"agentKind"`
	Capabilities     []string        `json:"capabilities"`
	Overrides        map[string]bool `json:"overrides"`
}

type SkillResourceReceipt struct {
	Path     string `json:"path"`
	Checksum string `json:"checksum"`
	Size     int64  `json:"size"`
}

type SkillLoadReceipt struct {
	SchemaVersion string                 `json:"schemaVersion"`
	ID            string                 `json:"id"`
	Revision      int                    `json:"revision"`
	Checksum      string                 `json:"checksum"`
	Source        SkillSource            `json:"source"`
	Resources     []SkillResourceReceipt `json:"resources"`
}

type SelectedSkill struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Checksum string `json:"checksum"`
}

type SkillSnapshot struct {
	SchemaVersion string             `json:"schemaVersion"`
	RunID         string             `json:"runId"`
	Skills        []SkillLoadReceipt `json:"skills"`
}

func BuildSkillCatalog(manifests []SkillManifest, filter SkillCatalogFilter) ([]SkillCatalogSummary, error) {
	resolved, err := resolveSkillManifests(manifests)
	if err != nil {
		return nil, err
	}
	if !knownAgentKind(filter.AgentKind) || !validSkillStringList(filter.Capabilities, 256, false) || !validSkillOverrides(filter.Overrides) {
		return nil, fmt.Errorf("Skill Catalog filter is invalid")
	}
	output := make([]SkillCatalogSummary, 0, len(resolved))
	for _, manifest := range resolved {
		if !skillManifestAllowed(manifest, filter) {
			continue
		}
		output = append(output, SkillCatalogSummary{
			ID: manifest.ID, Name: manifest.Name, Revision: manifest.Revision, Source: manifest.Source,
			AgentKinds:             append([]string(nil), manifest.AgentKinds...),
			CompatibleCapabilities: append([]string(nil), manifest.CompatibleCapabilities...),
			Categories:             append([]string(nil), manifest.Categories...), Summary: manifest.Summary,
		})
	}
	sort.Slice(output, func(i, j int) bool { return output[i].ID < output[j].ID })
	return output, nil
}

func AcceptSkillLoad(manifests []SkillManifest, request SkillLoadRequest, receipt SkillLoadReceipt) (SkillLoadReceipt, error) {
	if request.SchemaVersion != "1" || !validPlanSchemaID(request.RunID) || !validPlanSchemaID(request.ID) || request.ExpectedRevision < 1 {
		return SkillLoadReceipt{}, fmt.Errorf("Skill load request is invalid")
	}
	resolved, err := resolveSkillManifests(manifests)
	if err != nil {
		return SkillLoadReceipt{}, err
	}
	manifest, ok := resolved[request.ID]
	filter := SkillCatalogFilter{AgentKind: request.AgentKind, Capabilities: request.Capabilities, Overrides: request.Overrides}
	if !ok || !knownAgentKind(filter.AgentKind) || !validSkillStringList(filter.Capabilities, 256, false) || !validSkillOverrides(filter.Overrides) || !skillManifestAllowed(manifest, filter) {
		return SkillLoadReceipt{}, fmt.Errorf("Skill load is not authorized")
	}
	if request.ExpectedRevision != manifest.Revision || receipt.Validate() != nil || receipt.ID != manifest.ID || receipt.Revision != manifest.Revision || receipt.Source != manifest.Source || receipt.Checksum != manifest.Checksum {
		return SkillLoadReceipt{}, fmt.Errorf("Skill load receipt does not match the authorized manifest")
	}
	return cloneSkillReceipt(receipt), nil
}

func NewSkillSnapshot(runID string, selected []SelectedSkill, receipts []SkillLoadReceipt) (SkillSnapshot, error) {
	if !validPlanSchemaID(runID) || len(selected) > 64 || len(receipts) > 64 {
		return SkillSnapshot{}, fmt.Errorf("Skill snapshot input is invalid")
	}
	receiptByID := make(map[string]SkillLoadReceipt, len(receipts))
	for _, receipt := range receipts {
		if receipt.Validate() != nil {
			return SkillSnapshot{}, fmt.Errorf("Skill snapshot receipt is invalid")
		}
		if _, duplicate := receiptByID[receipt.ID]; duplicate {
			return SkillSnapshot{}, fmt.Errorf("Skill snapshot receipt is duplicated")
		}
		receiptByID[receipt.ID] = receipt
	}
	skills := make([]SkillLoadReceipt, 0, len(selected))
	seen := map[string]bool{}
	for _, item := range selected {
		if !validPlanSchemaID(item.ID) || item.Revision < 1 || !skillChecksumPattern.MatchString(item.Checksum) || seen[item.ID] {
			return SkillSnapshot{}, fmt.Errorf("Skill snapshot selection is invalid")
		}
		receipt, ok := receiptByID[item.ID]
		if !ok || receipt.Revision != item.Revision || receipt.Checksum != item.Checksum {
			return SkillSnapshot{}, fmt.Errorf("explicit Skill requires an exact loaded receipt")
		}
		seen[item.ID] = true
		skills = append(skills, cloneSkillReceipt(receipt))
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].ID < skills[j].ID })
	return SkillSnapshot{SchemaVersion: "1", RunID: runID, Skills: skills}, nil
}

func (receipt SkillLoadReceipt) Validate() error {
	if receipt.SchemaVersion != "1" || !validPlanSchemaID(receipt.ID) || receipt.Revision < 1 || !skillChecksumPattern.MatchString(receipt.Checksum) || skillSourceRank(receipt.Source) == 0 || len(receipt.Resources) > 256 {
		return fmt.Errorf("Skill load receipt is invalid")
	}
	seen := map[string]bool{}
	for _, resource := range receipt.Resources {
		clean := path.Clean(resource.Path)
		if clean != resource.Path || clean == "." || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") || resource.Size < 0 || resource.Size > 512*1024 || !skillChecksumPattern.MatchString(resource.Checksum) || seen[clean] {
			return fmt.Errorf("Skill resource receipt is invalid")
		}
		seen[clean] = true
	}
	return nil
}

func resolveSkillManifests(manifests []SkillManifest) (map[string]SkillManifest, error) {
	resolved := map[string]SkillManifest{}
	seenSource := map[string]bool{}
	for _, manifest := range manifests {
		if err := manifest.Validate(); err != nil {
			return nil, err
		}
		key := string(manifest.Source) + "\x00" + manifest.ID
		if seenSource[key] {
			return nil, fmt.Errorf("Skill manifest source identity is duplicated")
		}
		seenSource[key] = true
		current, exists := resolved[manifest.ID]
		if !exists || skillSourceRank(manifest.Source) > skillSourceRank(current.Source) {
			resolved[manifest.ID] = manifest
		}
	}
	return resolved, nil
}

func (manifest SkillManifest) Validate() error {
	if manifest.SchemaVersion != "1" || !validPlanSchemaID(manifest.ID) || manifest.Name != manifest.ID || manifest.Revision < 1 || skillSourceRank(manifest.Source) == 0 || !boundedPlanText(manifest.Summary, 2048) || manifest.EntryResource != "SKILL.md" || !skillChecksumPattern.MatchString(manifest.Checksum) {
		return fmt.Errorf("Skill manifest is invalid")
	}
	if !validSkillStringList(manifest.AgentKinds, 9, true) || !validSkillStringList(manifest.CompatibleCapabilities, 256, false) || !validSkillStringList(manifest.RequestedCapabilities, 256, false) || !validSkillStringList(manifest.Categories, 7, false) || !validSkillStringList(manifest.SupportResources, 256, false) {
		return fmt.Errorf("Skill manifest lists are invalid")
	}
	for _, kind := range manifest.AgentKinds {
		if !knownAgentKind(kind) {
			return fmt.Errorf("Skill manifest Agent Kind is invalid")
		}
	}
	return nil
}

func skillManifestAllowed(manifest SkillManifest, filter SkillCatalogFilter) bool {
	agentAllowed := false
	if override, exists := filter.Overrides[manifest.ID]; exists {
		agentAllowed = override
	} else if len(manifest.AgentKinds) == 0 {
		agentAllowed = true
	} else {
		for _, kind := range manifest.AgentKinds {
			if kind == filter.AgentKind {
				agentAllowed = true
				break
			}
		}
	}
	if !agentAllowed {
		return false
	}
	granted := map[string]bool{}
	for _, capability := range filter.Capabilities {
		granted[capability] = true
	}
	for _, capability := range manifest.CompatibleCapabilities {
		if !granted[capability] {
			return false
		}
	}
	return true
}

func validSkillOverrides(overrides map[string]bool) bool {
	if len(overrides) > 256 {
		return false
	}
	for id := range overrides {
		if !validPlanSchemaID(id) {
			return false
		}
	}
	return true
}

func validSkillStringList(values []string, max int, _ bool) bool {
	if len(values) > max {
		return false
	}
	seen := map[string]bool{}
	for _, value := range values {
		if !validPlanSchemaID(value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func knownAgentKind(kind string) bool {
	for _, candidate := range yanzhouprotocol.AgentKinds() {
		if candidate == kind {
			return true
		}
	}
	return false
}

func skillSourceRank(source SkillSource) int {
	switch source {
	case SkillSourceBuiltin:
		return 1
	case SkillSourceUser:
		return 2
	case SkillSourceWorkspace:
		return 3
	default:
		return 0
	}
}

func cloneSkillReceipt(receipt SkillLoadReceipt) SkillLoadReceipt {
	cloned := receipt
	cloned.Resources = append([]SkillResourceReceipt(nil), receipt.Resources...)
	return cloned
}
