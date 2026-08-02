package yanzhouadapter

import (
	"fmt"
	"sort"
)

type SubAgentDefinition struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	SystemPrompt string           `json:"systemPrompt"`
	Enabled      bool             `json:"enabled"`
	Capabilities []ToolCapability `json:"capabilities"`
}

type DelegationRequest struct {
	TaskID              string     `json:"taskId"`
	ParentRunID         string     `json:"parentRunId"`
	SubAgentID          string     `json:"subAgentId"`
	Objective           string     `json:"objective"`
	Target              ToolTarget `json:"target"`
	InputArtifactRefs   []string   `json:"inputArtifactRefs"`
	AllowedCapabilities []string   `json:"allowedCapabilities"`
	OutputContract      string     `json:"outputContract"`
	MayProposeWrite     bool       `json:"mayProposeWrite"`
	TokenBudget         int        `json:"tokenBudget,omitempty"`
	WallTimeMS          int        `json:"wallTimeMs,omitempty"`
}

type DelegationAuthorization struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type RunSubAgentConfig struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Prompt       string   `json:"prompt"`
	ProfileID    *string  `json:"profileId"`
	Enabled      bool     `json:"enabled"`
	Capabilities []string `json:"capabilities"`
}

type RunSubAgentSnapshot struct {
	SchemaVersion string              `json:"schemaVersion"`
	Revision      int                 `json:"revision"`
	Agents        []RunSubAgentConfig `json:"agents"`
}

func (snapshot RunSubAgentSnapshot) resolve(id string) (SubAgentDefinition, error) {
	builtins := BuiltinSubAgentDefinitions()
	if snapshot.SchemaVersion != "1" || snapshot.Revision < 1 || len(snapshot.Agents) != len(builtins) {
		return SubAgentDefinition{}, fmt.Errorf("SubAgent snapshot is invalid")
	}
	for index, builtin := range builtins {
		config := snapshot.Agents[index]
		if config.ID != builtin.ID || !boundedPlanText(config.Name, 256) || !boundedPlanText(config.Prompt, 16*1024) || len(config.Capabilities) > 256 {
			return SubAgentDefinition{}, fmt.Errorf("SubAgent snapshot is invalid")
		}
		allowed := map[string]ToolCapability{}
		for _, capability := range builtin.Capabilities {
			allowed[capability.ID] = capability
		}
		seen := map[string]bool{}
		capabilities := make([]ToolCapability, 0, len(config.Capabilities))
		for _, capabilityID := range config.Capabilities {
			capability, ok := allowed[capabilityID]
			if !ok || seen[capabilityID] {
				return SubAgentDefinition{}, fmt.Errorf("SubAgent snapshot capability is invalid")
			}
			seen[capabilityID] = true
			capabilities = append(capabilities, capability)
		}
		if config.ID == id {
			return SubAgentDefinition{
				ID: config.ID, Name: config.Name, Description: builtin.Description,
				SystemPrompt: config.Prompt, Enabled: config.Enabled, Capabilities: capabilities,
			}, nil
		}
	}
	return SubAgentDefinition{}, fmt.Errorf("SubAgent is unavailable")
}

func BuiltinSubAgentDefinitions() []SubAgentDefinition {
	read := ToolCapability{ID: "story.get_target", Mode: ToolCapabilityRead, MaxCalls: 32, MaxResultBytes: 64 * 1024}
	artifact := ToolCapability{ID: "writing.create_artifact", Mode: ToolCapabilityPropose, MaxCalls: 8, MaxResultBytes: 64 * 1024}
	definitions := []SubAgentDefinition{
		{ID: "general", Name: "General", Description: "General bounded delegated task", SystemPrompt: "Do not expand capabilities.", Enabled: true, Capabilities: []ToolCapability{read}},
		{ID: "context-planner", Name: "上下文规划", Description: "Prepare bounded context references", SystemPrompt: "Return only bounded context references.", Enabled: true, Capabilities: []ToolCapability{read}},
		{ID: "writer", Name: "正文作者", Description: "Create candidate prose", SystemPrompt: "Create candidates only; never write canonical files.", Enabled: true, Capabilities: []ToolCapability{read, artifact}},
		{ID: "reviewer", Name: "审阅者", Description: "Review candidate artifacts", SystemPrompt: "Return evidence-backed findings.", Enabled: true, Capabilities: []ToolCapability{read}},
		{ID: "fixer", Name: "修订者", Description: "Revise candidate artifacts", SystemPrompt: "Revise candidates only.", Enabled: true, Capabilities: []ToolCapability{read, artifact}},
		{ID: "final-gate", Name: "终检", Description: "Perform final checks", SystemPrompt: "Do not commit writes.", Enabled: true, Capabilities: []ToolCapability{read}},
		{ID: "memory-patcher", Name: "设定建议", Description: "Propose setting patches", SystemPrompt: "Return typed patch proposals only.", Enabled: true, Capabilities: []ToolCapability{read}},
	}
	return cloneSubAgentDefinitions(definitions)
}

func EffectiveChildCapabilities(parent, child, run []ToolCapability) ([]ToolCapability, error) {
	for _, layer := range [][]ToolCapability{parent, child, run} {
		seen := map[string]bool{}
		for _, capability := range layer {
			if capability.Validate() != nil || seen[capability.ID] {
				return nil, fmt.Errorf("SubAgent capability layer is invalid")
			}
			seen[capability.ID] = true
		}
	}
	childByID := map[string]ToolCapability{}
	runByID := map[string]ToolCapability{}
	for _, capability := range child {
		childByID[capability.ID] = capability
	}
	for _, capability := range run {
		runByID[capability.ID] = capability
	}
	effective := make([]ToolCapability, 0)
	for _, capability := range parent {
		childCapability, childOK := childByID[capability.ID]
		runCapability, runOK := runByID[capability.ID]
		if !childOK || !runOK || childCapability.Mode != capability.Mode || runCapability.Mode != capability.Mode {
			continue
		}
		capability.MaxCalls = minPositive(capability.MaxCalls, childCapability.MaxCalls, runCapability.MaxCalls)
		capability.MaxResultBytes = minPositive(capability.MaxResultBytes, childCapability.MaxResultBytes, runCapability.MaxResultBytes)
		effective = append(effective, capability)
	}
	sort.Slice(effective, func(i, j int) bool { return effective[i].ID < effective[j].ID })
	return effective, nil
}

func ValidateDelegation(request DelegationRequest, child SubAgentDefinition, parent, run []ToolCapability, authorization DelegationAuthorization) ([]ToolCapability, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if child.ID != request.SubAgentID || !child.Enabled || !validDelegationAuthorization(authorization) {
		return nil, fmt.Errorf("delegation is not explicitly authorized")
	}
	effective, err := EffectiveChildCapabilities(parent, child.Capabilities, run)
	if err != nil {
		return nil, err
	}
	effectiveByID := map[string]ToolCapability{}
	for _, capability := range effective {
		effectiveByID[capability.ID] = capability
	}
	grant := make([]ToolCapability, 0, len(request.AllowedCapabilities))
	mayPropose := false
	for _, id := range request.AllowedCapabilities {
		capability, ok := effectiveByID[id]
		if !ok {
			return nil, fmt.Errorf("delegation capability escalation is denied")
		}
		grant = append(grant, capability)
		if capability.Mode == ToolCapabilityPropose && len(id) > len("writing.") && id[:len("writing.")] == "writing." {
			mayPropose = true
		}
	}
	if request.MayProposeWrite && !mayPropose {
		return nil, fmt.Errorf("delegation write proposal intent is not authorized")
	}
	return grant, nil
}

func (request DelegationRequest) Validate() error {
	if !validPlanSchemaID(request.TaskID) || !validPlanSchemaID(request.ParentRunID) || !builtinSubAgentID(request.SubAgentID) || !boundedPlanText(request.Objective, 16*1024) || request.Target.Validate() != nil || !validPlanSchemaID(request.OutputContract) || len(request.InputArtifactRefs) > 64 || len(request.AllowedCapabilities) > 256 {
		return fmt.Errorf("delegation request is invalid")
	}
	if request.TokenBudget < 0 || request.TokenBudget > 10_000_000 || request.WallTimeMS < 0 || request.WallTimeMS > 3_600_000 {
		return fmt.Errorf("delegation budget is invalid")
	}
	seenRefs := map[string]bool{}
	for _, ref := range request.InputArtifactRefs {
		if !validPlanSchemaID(ref) || seenRefs[ref] {
			return fmt.Errorf("delegation input reference is invalid")
		}
		seenRefs[ref] = true
	}
	seenCapabilities := map[string]bool{}
	for _, id := range request.AllowedCapabilities {
		if _, ok := knownToolMode(id); !ok || seenCapabilities[id] {
			return fmt.Errorf("delegation capability is invalid")
		}
		seenCapabilities[id] = true
	}
	return nil
}

func builtinSubAgentID(id string) bool {
	for _, definition := range BuiltinSubAgentDefinitions() {
		if definition.ID == id {
			return true
		}
	}
	return false
}

func validDelegationAuthorization(value DelegationAuthorization) bool {
	return (value.Kind == "user" || value.Kind == "skill") && validPlanSchemaID(value.Ref)
}

func minPositive(values ...int) int {
	minimum := 0
	for _, value := range values {
		if value > 0 && (minimum == 0 || value < minimum) {
			minimum = value
		}
	}
	return minimum
}

func cloneSubAgentDefinitions(definitions []SubAgentDefinition) []SubAgentDefinition {
	cloned := make([]SubAgentDefinition, len(definitions))
	for index, definition := range definitions {
		cloned[index] = definition
		cloned[index].Capabilities = append([]ToolCapability(nil), definition.Capabilities...)
	}
	return cloned
}
