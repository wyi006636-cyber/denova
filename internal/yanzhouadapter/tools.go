package yanzhouadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

type ToolCapabilityMode string

const (
	ToolCapabilityRead    ToolCapabilityMode = "read"
	ToolCapabilityPropose ToolCapabilityMode = "propose"
	ToolCapabilityExecute ToolCapabilityMode = "execute"
)

var yanzhouReadToolIDs = []string{
	"story.get_target",
	"story.get_adjacent_chapters",
	"story.search_chapters",
	"story.get_outline",
	"story.get_characters",
	"story.get_open_threads",
	"story.get_settings",
	"story.get_relationships",
	"story.get_timeline",
	"story.get_threads",
	"story.search_knowledge",
	"story.get_style_assets",
	"review.resolve_findings",
	"game.get_committed_context",
}

var yanzhouProposeToolIDs = []string{
	"writing.create_artifact",
	"writing.create_proposal",
	"setting.create_patch_proposal",
	"game.submit_turn",
	"director.submit_patch",
	"image.generate",
}

var defaultForbiddenToolIDs = []string{
	"filesystem.write",
	"filesystem.delete",
	"filesystem.edit",
	"workspace.replace",
	"shell.exec",
	"path.read",
}

type ToolTarget struct {
	SchemaVersion string `json:"schemaVersion"`
	Kind          string `json:"kind"`
	BookID        string `json:"bookId"`
	TargetID      string `json:"targetId"`
}

type ToolCapability struct {
	ID             string             `json:"id"`
	Mode           ToolCapabilityMode `json:"mode"`
	MaxCalls       int                `json:"maxCalls"`
	MaxResultBytes int                `json:"maxResultBytes"`
}

type ToolCapabilityManifest struct {
	SchemaVersion   string           `json:"schemaVersion"`
	RunID           string           `json:"runId"`
	AgentID         string           `json:"agentId"`
	Target          ToolTarget       `json:"target"`
	DeniedByDefault bool             `json:"deniedByDefault"`
	Capabilities    []ToolCapability `json:"capabilities"`
}

type ToolCall struct {
	RunID     string     `json:"runId"`
	AgentID   string     `json:"agentId"`
	Target    ToolTarget `json:"target"`
	ToolID    string     `json:"toolId"`
	Arguments string     `json:"arguments"`
}

type ToolResultKind string

const (
	ToolResultRead     ToolResultKind = "read-result"
	ToolResultProposal ToolResultKind = "proposal"
	ToolResultReceipt  ToolResultKind = "receipt"
)

type ToolExecutionAccounting struct {
	ToolAttempts        int `json:"toolAttempts"`
	ProviderAttempts    int `json:"providerAttempts"`
	OutputGuardAttempts int `json:"outputGuardAttempts"`
}

type ToolResult struct {
	Kind              ToolResultKind          `json:"kind"`
	Data              map[string]any          `json:"data,omitempty"`
	ProposalID        string                  `json:"proposalId,omitempty"`
	ReceiptID         string                  `json:"receiptId,omitempty"`
	Status            string                  `json:"status,omitempty"`
	Target            ToolTarget              `json:"target,omitempty"`
	MutationPerformed bool                    `json:"mutationPerformed"`
	Accounting        ToolExecutionAccounting `json:"accounting"`
}

type ToolHandler func(context.Context, map[string]any) (ToolResult, error)

type ToolExecutionOptions struct {
	MaxToolRetries   int
	ProviderAttempts int
	Timeout          time.Duration
}

type ToolHarness struct {
	manifest   ToolCapabilityManifest
	handlers   map[string]ToolHandler
	registered []string
	readSlots  chan struct{}
	proposalMu sync.Mutex
	callsMu    sync.Mutex
	calls      map[string]int
}

func YanzhouReadToolIDs() []string      { return append([]string(nil), yanzhouReadToolIDs...) }
func YanzhouProposeToolIDs() []string   { return append([]string(nil), yanzhouProposeToolIDs...) }
func DefaultForbiddenToolIDs() []string { return append([]string(nil), defaultForbiddenToolIDs...) }
func ToolCapabilityModes() []ToolCapabilityMode {
	return []ToolCapabilityMode{ToolCapabilityRead, ToolCapabilityPropose, ToolCapabilityExecute}
}

func NewToolHarness(manifest ToolCapabilityManifest, handlers map[string]ToolHandler, maxReadConcurrency int) (*ToolHarness, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	if maxReadConcurrency < 1 || maxReadConcurrency > 32 {
		return nil, fmt.Errorf("read concurrency is invalid")
	}
	registered := make([]string, 0, len(manifest.Capabilities))
	filteredHandlers := map[string]ToolHandler{}
	for _, capability := range manifest.Capabilities {
		if handler := handlers[capability.ID]; handler != nil {
			registered = append(registered, capability.ID)
			filteredHandlers[capability.ID] = handler
		}
	}
	sort.Strings(registered)
	return &ToolHarness{
		manifest: manifest, handlers: filteredHandlers, registered: registered,
		readSlots: make(chan struct{}, maxReadConcurrency), calls: map[string]int{},
	}, nil
}

func (h *ToolHarness) RegisteredTools() []string {
	if h == nil {
		return nil
	}
	return append([]string(nil), h.registered...)
}

func (h *ToolHarness) Execute(ctx context.Context, call ToolCall, options ToolExecutionOptions) (ToolResult, error) {
	if h == nil {
		return ToolResult{}, fmt.Errorf("tool harness is unavailable")
	}
	capability, err := h.authorize(call)
	if err != nil {
		return ToolResult{}, err
	}
	arguments, err := decodeToolArguments(call.Arguments)
	if err != nil {
		return ToolResult{}, err
	}
	h.callsMu.Lock()
	h.calls[call.ToolID]++
	callCount := h.calls[call.ToolID]
	h.callsMu.Unlock()
	if callCount > capability.MaxCalls {
		return ToolResult{}, fmt.Errorf("tool call budget is exhausted")
	}
	operation := func() (ToolResult, error) {
		return h.invoke(ctx, call.ToolID, arguments, capability, options)
	}
	if capability.Mode == ToolCapabilityRead {
		select {
		case h.readSlots <- struct{}{}:
			defer func() { <-h.readSlots }()
		case <-ctx.Done():
			return ToolResult{}, ctx.Err()
		}
		return operation()
	}
	h.proposalMu.Lock()
	defer h.proposalMu.Unlock()
	return operation()
}

func (h *ToolHarness) authorize(call ToolCall) (ToolCapability, error) {
	if call.RunID != h.manifest.RunID || call.AgentID != h.manifest.AgentID || call.Target != h.manifest.Target {
		return ToolCapability{}, fmt.Errorf("tool execution identity mismatch")
	}
	if h.handlers[call.ToolID] == nil {
		return ToolCapability{}, fmt.Errorf("tool execution denied by default")
	}
	for _, capability := range h.manifest.Capabilities {
		if capability.ID == call.ToolID {
			return capability, nil
		}
	}
	return ToolCapability{}, fmt.Errorf("tool execution denied by default")
}

func (h *ToolHarness) invoke(ctx context.Context, toolID string, arguments map[string]any, capability ToolCapability, options ToolExecutionOptions) (ToolResult, error) {
	if options.MaxToolRetries < 0 || options.MaxToolRetries > 3 || options.ProviderAttempts < 0 {
		return ToolResult{}, fmt.Errorf("tool execution accounting is invalid")
	}
	accounting := ToolExecutionAccounting{ProviderAttempts: options.ProviderAttempts}
	var lastErr error
	for attempt := 0; attempt <= options.MaxToolRetries; attempt++ {
		accounting.ToolAttempts++
		result, err := invokeToolHandler(ctx, h.handlers[toolID], arguments, options.Timeout)
		if err != nil {
			lastErr = err
			continue
		}
		if err := validateToolResult(capability.Mode, &result, capability.MaxResultBytes); err != nil {
			accounting.OutputGuardAttempts++
			return ToolResult{}, fmt.Errorf("tool output guard rejected result")
		}
		result.MutationPerformed = false
		result.Accounting = accounting
		return result, nil
	}
	return ToolResult{}, fmt.Errorf("tool retry budget exhausted: %w", lastErr)
}

func invokeToolHandler(ctx context.Context, handler ToolHandler, arguments map[string]any, timeout time.Duration) (ToolResult, error) {
	if timeout <= 0 {
		return handler(ctx, arguments)
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type outcome struct {
		result ToolResult
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		result, err := handler(callCtx, arguments)
		completed <- outcome{result: result, err: err}
	}()
	select {
	case result := <-completed:
		return result.result, result.err
	case <-callCtx.Done():
		return ToolResult{}, fmt.Errorf("tool timeout")
	}
}

func decodeToolArguments(raw string) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > 64*1024 {
		return nil, fmt.Errorf("tool arguments contain invalid JSON")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var arguments map[string]any
	if err := decoder.Decode(&arguments); err != nil || arguments == nil {
		return nil, fmt.Errorf("tool arguments contain invalid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("tool arguments contain invalid JSON")
	}
	return arguments, nil
}

func validateToolResult(mode ToolCapabilityMode, result *ToolResult, maxBytes int) error {
	if result == nil || result.MutationPerformed {
		return fmt.Errorf("tool output is invalid")
	}
	switch mode {
	case ToolCapabilityRead:
		if result.Kind != ToolResultRead || result.Data == nil {
			return fmt.Errorf("read tool output is invalid")
		}
	case ToolCapabilityPropose:
		switch result.Kind {
		case ToolResultProposal:
			if !validPlanSchemaID(result.ProposalID) || result.Target.Validate() != nil {
				return fmt.Errorf("proposal tool output is invalid")
			}
		case ToolResultReceipt:
			if !validPlanSchemaID(result.ReceiptID) || result.Status != "proposed" {
				return fmt.Errorf("tool receipt is invalid")
			}
		default:
			return fmt.Errorf("proposal tool output is invalid")
		}
	default:
		return fmt.Errorf("execute tools are not available in WP4")
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maxBytes {
		return fmt.Errorf("tool output exceeds limit")
	}
	return nil
}

func (manifest ToolCapabilityManifest) Validate() error {
	if manifest.SchemaVersion != "1" || !validPlanSchemaID(manifest.RunID) || !validPlanSchemaID(manifest.AgentID) || manifest.Target.Validate() != nil || !manifest.DeniedByDefault || len(manifest.Capabilities) > 256 {
		return fmt.Errorf("tool capability manifest is invalid")
	}
	seen := map[string]bool{}
	for _, capability := range manifest.Capabilities {
		if capability.Validate() != nil || seen[capability.ID] {
			return fmt.Errorf("tool capability manifest is invalid")
		}
		seen[capability.ID] = true
	}
	return nil
}

func (capability ToolCapability) Validate() error {
	expected, known := knownToolMode(capability.ID)
	if !known || expected != capability.Mode || capability.MaxCalls < 1 || capability.MaxCalls > 1_000_000 || capability.MaxResultBytes < 1 || capability.MaxResultBytes > 1024*1024 {
		return fmt.Errorf("tool capability is forbidden or invalid")
	}
	return nil
}

func (target ToolTarget) Validate() error {
	if target.SchemaVersion != "1" || !knownTargetKind(target.Kind) || !validPlanSchemaID(target.BookID) || !validPlanSchemaID(target.TargetID) {
		return fmt.Errorf("tool target is invalid")
	}
	return nil
}

func knownToolMode(id string) (ToolCapabilityMode, bool) {
	for _, candidate := range yanzhouReadToolIDs {
		if id == candidate {
			return ToolCapabilityRead, true
		}
	}
	for _, candidate := range yanzhouProposeToolIDs {
		if id == candidate {
			return ToolCapabilityPropose, true
		}
	}
	return "", false
}

func knownTargetKind(kind string) bool {
	for _, candidate := range []string{"book", "main_outline", "volume", "volume_outline", "chapter", "chapter_outline", "text_selection", "character", "setting", "relationship", "thread", "review_finding", "game_story", "game_branch", "game_turn"} {
		if strings.TrimSpace(kind) == candidate {
			return true
		}
	}
	return false
}
