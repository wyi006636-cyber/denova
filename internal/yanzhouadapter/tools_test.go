package yanzhouadapter

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testToolTarget() ToolTarget {
	return ToolTarget{SchemaVersion: "1", Kind: "chapter", BookID: "book-1", TargetID: "chapter-1"}
}

func TestToolHarnessBuilderAndOrchestratorDefaultDeny(t *testing.T) {
	if len(YanzhouReadToolIDs()) != 15 || len(YanzhouProposeToolIDs()) != 6 || len(YanzhouExecuteToolIDs()) != 1 || len(DefaultForbiddenToolIDs()) != 6 || len(ToolCapabilityModes()) != 3 {
		t.Fatalf("tool counts read=%d propose=%d execute=%d forbidden=%d modes=%d", len(YanzhouReadToolIDs()), len(YanzhouProposeToolIDs()), len(YanzhouExecuteToolIDs()), len(DefaultForbiddenToolIDs()), len(ToolCapabilityModes()))
	}
	var calls atomic.Int32
	manifest := ToolCapabilityManifest{
		SchemaVersion: "1", RunID: "run-1", AgentID: "writer", Target: testToolTarget(), DeniedByDefault: true,
		Capabilities: []ToolCapability{
			{ID: "story.get_target", Mode: ToolCapabilityRead, MaxCalls: 2, MaxResultBytes: 4096},
			{ID: "writing.create_proposal", Mode: ToolCapabilityPropose, MaxCalls: 2, MaxResultBytes: 4096},
		},
	}
	harness, err := NewToolHarness(manifest, map[string]ToolHandler{
		"story.get_target": func(context.Context, map[string]any) (ToolResult, error) {
			calls.Add(1)
			return ToolResult{Kind: ToolResultRead, Data: map[string]any{"title": "Chapter"}}, nil
		},
		"writing.create_proposal": func(context.Context, map[string]any) (ToolResult, error) {
			calls.Add(1)
			return ToolResult{Kind: ToolResultProposal, ProposalID: "proposal-1", Target: testToolTarget(), MutationPerformed: false}, nil
		},
		"filesystem.write": func(context.Context, map[string]any) (ToolResult, error) {
			calls.Add(1)
			return ToolResult{}, nil
		},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := harness.RegisteredTools(); len(got) != 2 || got[0] != "story.get_target" || got[1] != "writing.create_proposal" {
		t.Fatalf("registered=%v", got)
	}
	for _, call := range []ToolCall{
		{RunID: "run-1", AgentID: "writer", Target: testToolTarget(), ToolID: "unknown.tool", Arguments: `{}`},
		{RunID: "run-1", AgentID: "writer", Target: testToolTarget(), ToolID: "story.get_target", Arguments: `{"x":`},
		{RunID: "other", AgentID: "writer", Target: testToolTarget(), ToolID: "story.get_target", Arguments: `{}`},
		{RunID: "run-1", AgentID: "reviewer", Target: testToolTarget(), ToolID: "story.get_target", Arguments: `{}`},
	} {
		if _, err := harness.Execute(context.Background(), call, ToolExecutionOptions{}); err == nil {
			t.Fatalf("unsafe call accepted: %#v", call)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("unsafe calls reached handlers: %d", calls.Load())
	}
	result, err := harness.Execute(context.Background(), ToolCall{
		RunID: "run-1", AgentID: "writer", Target: testToolTarget(), ToolID: "story.get_target", Arguments: `{}`,
	}, ToolExecutionOptions{})
	if err != nil || result.Kind != ToolResultRead || result.MutationPerformed {
		t.Fatalf("read result=%#v err=%v", result, err)
	}
	proposal, err := harness.Execute(context.Background(), ToolCall{
		RunID: "run-1", AgentID: "writer", Target: testToolTarget(), ToolID: "writing.create_proposal", Arguments: `{}`,
	}, ToolExecutionOptions{})
	if err != nil || proposal.Kind != ToolResultProposal || proposal.MutationPerformed {
		t.Fatalf("proposal result=%#v err=%v", proposal, err)
	}
}

func TestTask10ForegroundCommandUsesTheExistingRunBoundToolHarness(t *testing.T) {
	manifest := ToolCapabilityManifest{
		SchemaVersion: "1", RunID: "run-command", AgentID: "primary-writer", Target: testToolTarget(), DeniedByDefault: true,
		Capabilities: []ToolCapability{{ID: "command.run", Mode: ToolCapabilityExecute, MaxCalls: 1, MaxResultBytes: 4096}},
	}
	harness, err := NewToolHarness(manifest, map[string]ToolHandler{
		"command.run": func(context.Context, map[string]any) (ToolResult, error) {
			return ToolResult{Kind: ToolResultRead, Data: map[string]any{"stdout": "book-command-output", "exitCode": 0}}, nil
		},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err := harness.Execute(context.Background(), ToolCall{
		RunID: "run-command", AgentID: "primary-writer", Target: testToolTarget(), ToolID: "command.run", Arguments: `{"command":"pwd"}`,
	}, ToolExecutionOptions{})
	if err != nil || result.Kind != ToolResultRead || result.Data["stdout"] != "book-command-output" || result.MutationPerformed {
		t.Fatalf("command result=%#v err=%v", result, err)
	}
}

func TestToolHarnessBoundsReadConcurrencyAndSerializesProposal(t *testing.T) {
	manifest := ToolCapabilityManifest{
		SchemaVersion: "1", RunID: "run-1", AgentID: "writer", Target: testToolTarget(), DeniedByDefault: true,
		Capabilities: []ToolCapability{
			{ID: "story.get_target", Mode: ToolCapabilityRead, MaxCalls: 8, MaxResultBytes: 4096},
			{ID: "writing.create_proposal", Mode: ToolCapabilityPropose, MaxCalls: 8, MaxResultBytes: 4096},
		},
	}
	var activeReads, maxReads, activeWrites, maxWrites atomic.Int32
	track := func(active, maximum *atomic.Int32) func() {
		value := active.Add(1)
		for {
			current := maximum.Load()
			if value <= current || maximum.CompareAndSwap(current, value) {
				break
			}
		}
		return func() { active.Add(-1) }
	}
	harness, err := NewToolHarness(manifest, map[string]ToolHandler{
		"story.get_target": func(context.Context, map[string]any) (ToolResult, error) {
			done := track(&activeReads, &maxReads)
			defer done()
			time.Sleep(15 * time.Millisecond)
			return ToolResult{Kind: ToolResultRead, Data: map[string]any{}}, nil
		},
		"writing.create_proposal": func(context.Context, map[string]any) (ToolResult, error) {
			done := track(&activeWrites, &maxWrites)
			defer done()
			time.Sleep(10 * time.Millisecond)
			return ToolResult{Kind: ToolResultReceipt, ReceiptID: "receipt-1", Status: "proposed"}, nil
		},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	runConcurrent := func(toolID string, count int) {
		t.Helper()
		var group sync.WaitGroup
		for index := 0; index < count; index++ {
			group.Add(1)
			go func() {
				defer group.Done()
				if _, err := harness.Execute(context.Background(), ToolCall{RunID: "run-1", AgentID: "writer", Target: testToolTarget(), ToolID: toolID, Arguments: `{}`}, ToolExecutionOptions{}); err != nil {
					t.Errorf("Execute(%s): %v", toolID, err)
				}
			}()
		}
		group.Wait()
	}
	runConcurrent("story.get_target", 5)
	runConcurrent("writing.create_proposal", 3)
	if maxReads.Load() != 2 || maxWrites.Load() != 1 {
		t.Fatalf("max reads=%d writes=%d", maxReads.Load(), maxWrites.Load())
	}
}

func TestSubAgentDefinitionsCapabilityIntersectionAndDelegationValidation(t *testing.T) {
	want := []string{"general", "context-planner", "writer", "reviewer", "fixer", "final-gate", "memory-patcher"}
	definitions := BuiltinSubAgentDefinitions()
	if len(definitions) != len(want) {
		t.Fatalf("definitions=%d", len(definitions))
	}
	for index, definition := range definitions {
		if definition.ID != want[index] {
			t.Fatalf("definition %d id=%q", index, definition.ID)
		}
	}
	read := ToolCapability{ID: "story.get_target", Mode: ToolCapabilityRead, MaxCalls: 1, MaxResultBytes: 4096}
	propose := ToolCapability{ID: "writing.create_artifact", Mode: ToolCapabilityPropose, MaxCalls: 1, MaxResultBytes: 4096}
	effective, err := EffectiveChildCapabilities([]ToolCapability{read, propose}, []ToolCapability{read, propose}, []ToolCapability{read})
	if err != nil || len(effective) != 1 || effective[0].ID != read.ID {
		t.Fatalf("effective=%#v err=%v", effective, err)
	}
	for index := 0; index < 3; index++ {
		layers := [][]ToolCapability{{read}, {read}, {read}}
		layers[index] = nil
		if got, err := EffectiveChildCapabilities(layers[0], layers[1], layers[2]); err != nil || len(got) != 0 {
			t.Fatalf("deny layer %d got=%#v err=%v", index, got, err)
		}
	}
	request := DelegationRequest{
		TaskID: "task-1", ParentRunID: "run-1", SubAgentID: "writer", Objective: "生成候选开篇",
		Target: testToolTarget(), InputArtifactRefs: []string{"context-1"},
		AllowedCapabilities: []string{read.ID, propose.ID}, OutputContract: "candidate-artifact-v1",
		MayProposeWrite: true, TokenBudget: 2048, WallTimeMS: 5000,
	}
	if _, err := ValidateDelegation(request, definitions[2], []ToolCapability{read, propose}, []ToolCapability{read, propose}, DelegationAuthorization{}); err == nil {
		t.Fatal("automatic delegation must be rejected")
	}
	grant, err := ValidateDelegation(request, definitions[2], []ToolCapability{read, propose}, []ToolCapability{read, propose}, DelegationAuthorization{Kind: "user", Ref: "author-choice-1"})
	if err != nil || len(grant) != 2 {
		t.Fatalf("grant=%#v err=%v", grant, err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" {
		t.Fatal("delegation request must encode")
	}
}
