package yanzhouadapter

import (
	"encoding/json"
	"errors"
)

type writingDelegation struct {
	request       DelegationRequest
	authorization DelegationAuthorization
	agent         SubAgentConfig
	capabilities  []ToolCapability
}

func prepareWritingDelegation(request planRunRequest, stage WritingHarnessStage, previous []writingRuntimeArtifact) (writingDelegation, error) {
	if !stage.Delegated {
		return writingDelegation{}, errors.New("writing delegation is unavailable")
	}
	agent, found := runSubAgentSnapshot(request).Agent(string(stage.RoleID))
	if !found || !agent.Enabled || (agent.ProfileID != nil && *agent.ProfileID != request.EffectiveModelProfile.ProfileID) {
		return writingDelegation{}, errors.New("writing delegated model profile is unavailable")
	}
	var target ToolTarget
	if err := json.Unmarshal(request.Target, &target); err != nil || target.Validate() != nil {
		return writingDelegation{}, errors.New("writing delegation target is invalid")
	}
	var manifest struct {
		Capabilities []ToolCapability `json:"capabilities"`
	}
	if err := json.Unmarshal(request.ToolManifest, &manifest); err != nil {
		return writingDelegation{}, errors.New("writing delegation tool manifest is invalid")
	}
	runByID := make(map[string]ToolCapability, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		if capability.Validate() == nil {
			runByID[capability.ID] = capability
		}
	}
	childAllows := make(map[string]bool, len(agent.Capabilities))
	for _, id := range agent.Capabilities {
		childAllows[id] = true
	}
	parent := make([]ToolCapability, 0, len(stage.Permissions))
	child := make([]ToolCapability, 0, len(stage.Permissions))
	run := make([]ToolCapability, 0, len(stage.Permissions))
	allowed := make([]string, 0, len(stage.Permissions))
	for _, id := range stage.Permissions {
		capability, runAllows := runByID[id]
		if !runAllows {
			continue
		}
		parent = append(parent, capability)
		run = append(run, capability)
		if childAllows[id] {
			child = append(child, capability)
			allowed = append(allowed, id)
		}
	}
	if len(allowed) == 0 {
		return writingDelegation{}, errors.New("writing delegation has no effective capability")
	}
	inputRefs := writingArtifactIDs(previous)
	objective := request.UserIntent
	if request.CapabilityID == "book.conceive" {
		objective = "Review the complete prior conception Artifact. Return concrete structure or content suggestions; do not rewrite the candidate."
	}
	delegationRequest := DelegationRequest{
		TaskID:              "task-" + request.RunID + "-" + stage.ID,
		ParentRunID:         request.RunID,
		SubAgentID:          string(stage.RoleID),
		Objective:           objective,
		Target:              target,
		InputArtifactRefs:   inputRefs,
		AllowedCapabilities: allowed,
		OutputContract:      "harness-" + stage.OutputKind + "-v1",
		TokenBudget:         positiveInt(request.Budgets.MaxOutputTokens),
		WallTimeMS:          request.Budgets.MaxWallTimeMS,
	}
	authorization := DelegationAuthorization{Kind: "user", Ref: "harness-" + request.HarnessProfile}
	definition := SubAgentDefinition{
		ID: agent.ID, Name: agent.Name, Description: agent.Name,
		SystemPrompt: agent.Prompt, Enabled: agent.Enabled, Capabilities: child,
	}
	capabilities, err := ValidateDelegation(delegationRequest, definition, parent, run, authorization)
	if err != nil {
		return writingDelegation{}, err
	}
	return writingDelegation{
		request: delegationRequest, authorization: authorization,
		agent: agent, capabilities: capabilities,
	}, nil
}

func positiveInt(value *int) int {
	if value == nil || *value < 0 {
		return 0
	}
	return *value
}

func writingDelegationStartedPayload(delegation writingDelegation) map[string]any {
	capabilityIDs := make([]string, len(delegation.capabilities))
	for index, capability := range delegation.capabilities {
		capabilityIDs[index] = capability.ID
	}
	return map[string]any{
		"taskId": delegation.request.TaskID, "parentRunId": delegation.request.ParentRunID,
		"subAgentId": delegation.request.SubAgentID, "inputArtifactRefs": delegation.request.InputArtifactRefs,
		"outputContract": delegation.request.OutputContract, "capabilityIds": capabilityIDs,
		"authorizationKind": delegation.authorization.Kind, "authorizationRef": delegation.authorization.Ref,
		"status": "running",
	}
}

func writingDelegationCompletedPayload(delegation writingDelegation, outputArtifactRefs []string, status string) map[string]any {
	return map[string]any{
		"taskId": delegation.request.TaskID, "parentRunId": delegation.request.ParentRunID,
		"subAgentId": delegation.request.SubAgentID, "inputArtifactRefs": delegation.request.InputArtifactRefs,
		"outputArtifactRefs": outputArtifactRefs, "outputContract": delegation.request.OutputContract,
		"status": status,
	}
}
