package yanzhouadapter

import (
	"context"
	"crypto/subtle"
	"fmt"
	"sync"

	agenttools "denova/internal/agent/tools"
	"denova/internal/yanzhouprotocol"
)

const ProtocolVersion = yanzhouprotocol.ProtocolVersion

type CapabilityMode string

const (
	ModeRead    CapabilityMode = "read"
	ModePropose CapabilityMode = "propose"
	ModeExecute CapabilityMode = "execute"
)

type Capability struct {
	ID   string         `json:"id"`
	Mode CapabilityMode `json:"mode"`
}

type BootstrapPolicy struct {
	SchemaVersion       string       `json:"schemaVersion"`
	DeniedByDefault     bool         `json:"deniedByDefault"`
	Capabilities        []Capability `json:"capabilities"`
	TargetAccess        string       `json:"targetAccess"`
	ArbitraryPathAccess bool         `json:"arbitraryPathAccess"`
	DirectMutation      bool         `json:"directMutation"`
}

type BootstrapBuildReceipt struct {
	RegisteredToolCount int  `json:"registeredToolCount"`
	Filesystem          bool `json:"filesystem"`
	Shell               bool `json:"shell"`
	DirectWrite         bool `json:"directWrite"`
}

var domainToolAllowlist = map[string]CapabilityMode{
	"story.get_target":              ModeRead,
	"story.get_adjacent_chapters":   ModeRead,
	"story.search_chapters":         ModeRead,
	"story.get_outline":             ModeRead,
	"story.get_characters":          ModeRead,
	"story.get_settings":            ModeRead,
	"story.get_relationships":       ModeRead,
	"story.get_timeline":            ModeRead,
	"story.get_threads":             ModeRead,
	"story.search_knowledge":        ModeRead,
	"story.get_style_assets":        ModeRead,
	"review.resolve_findings":       ModeRead,
	"game.get_committed_context":    ModeRead,
	"writing.create_artifact":       ModePropose,
	"writing.create_proposal":       ModePropose,
	"setting.create_patch_proposal": ModePropose,
	"game.submit_turn":              ModePropose,
	"director.submit_patch":         ModePropose,
	"image.generate":                ModePropose,
}

func DomainToolAllowlist() map[string]CapabilityMode {
	out := make(map[string]CapabilityMode, len(domainToolAllowlist))
	for id, mode := range domainToolAllowlist {
		out[id] = mode
	}
	return out
}

func NewBootstrapPolicy() BootstrapPolicy {
	return BootstrapPolicy{
		SchemaVersion:       "1",
		DeniedByDefault:     true,
		Capabilities:        []Capability{},
		TargetAccess:        "none",
		ArbitraryPathAccess: false,
		DirectMutation:      false,
	}
}

func (p BootstrapPolicy) Validate() error {
	if p.SchemaVersion != "1" || !p.DeniedByDefault || p.TargetAccess != "none" || p.ArbitraryPathAccess || p.DirectMutation {
		return fmt.Errorf("bootstrap policy must be default-deny without path or mutation authority")
	}
	seen := make(map[string]bool, len(p.Capabilities))
	for _, capability := range p.Capabilities {
		mode, ok := domainToolAllowlist[capability.ID]
		if !ok || mode != capability.Mode {
			return fmt.Errorf("bootstrap capability rejected: %s:%s", capability.ID, capability.Mode)
		}
		if seen[capability.ID] {
			return fmt.Errorf("duplicate bootstrap capability: %s", capability.ID)
		}
		seen[capability.ID] = true
	}
	return nil
}

// SmokeBuildBootstrap exercises Denova's real tool assembler with every legacy
// file, shell, lore-write, and configuration-write flag left false.
func SmokeBuildBootstrap(ctx context.Context, policy BootstrapPolicy) (BootstrapBuildReceipt, error) {
	if err := policy.Validate(); err != nil {
		return BootstrapBuildReceipt{}, err
	}
	result, err := agenttools.Build(ctx, agenttools.BuildRequest{})
	if err != nil {
		return BootstrapBuildReceipt{}, err
	}
	return BootstrapBuildReceipt{RegisteredToolCount: len(result.Tools)}, nil
}

type BootstrapTokenGate struct {
	mu       sync.Mutex
	token    []byte
	consumed bool
}

func NewBootstrapTokenGate(token string) (*BootstrapTokenGate, error) {
	if token == "" {
		return nil, fmt.Errorf("bootstrap token is required")
	}
	return &BootstrapTokenGate{token: []byte(token)}, nil
}

func (g *BootstrapTokenGate) consume(candidate string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.consumed || len(g.token) == 0 || len(candidate) != len(g.token) || subtle.ConstantTimeCompare([]byte(candidate), g.token) != 1 {
		return fmt.Errorf("bootstrap token rejected")
	}
	for index := range g.token {
		g.token[index] = 0
	}
	g.token = nil
	g.consumed = true
	return nil
}
