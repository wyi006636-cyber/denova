package yanzhouadapter

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"denova/internal/yanzhouprotocol"
)

func TestBootstrapPolicyMatchesProductSpecAllowlistAndDefaultsToNoTools(t *testing.T) {
	want := map[string]CapabilityMode{
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
	if got := DomainToolAllowlist(); !reflect.DeepEqual(got, want) {
		t.Fatalf("domain tool allowlist mismatch\n got: %#v\nwant: %#v", got, want)
	}

	policy := NewBootstrapPolicy()
	if !policy.DeniedByDefault || len(policy.Capabilities) != 0 {
		t.Fatalf("bootstrap must default deny with no granted tools: %#v", policy)
	}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}

	receipt, err := SmokeBuildBootstrap(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.CandidateRegistrationCount != len(want)+11 {
		t.Fatalf("bootstrap smoke candidates = %d, want %d", receipt.CandidateRegistrationCount, len(want)+11)
	}
	if receipt.RegisteredToolCount != 0 || receipt.Filesystem || receipt.Shell || receipt.DirectWrite {
		t.Fatalf("unsafe bootstrap builder receipt: %#v", receipt)
	}
}

func TestBootstrapPolicyRejectsUnknownWrongModeAndDangerousCapabilities(t *testing.T) {
	tests := []Capability{
		{ID: "filesystem.write", Mode: ModeExecute},
		{ID: "path.read", Mode: ModeRead},
		{ID: "shell.exec", Mode: ModeExecute},
		{ID: "writing.create_proposal", Mode: ModeRead},
		{ID: "story.get_target", Mode: ModePropose},
		{ID: "unknown.tool", Mode: ModeRead},
	}
	for _, capability := range tests {
		policy := NewBootstrapPolicy()
		policy.Capabilities = []Capability{capability}
		if err := policy.Validate(); err == nil {
			t.Fatalf("unsafe capability accepted: %#v", capability)
		}
	}
}

func TestBootstrapTokenIsSingleUseAndHandshakeReturnsNineKinds(t *testing.T) {
	gate, err := NewBootstrapTokenGate("one-time-secret")
	if err != nil {
		t.Fatal(err)
	}
	request := yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: ProtocolVersion,
		ClientBuild:     "yanzhou-test",
		WorkspaceSchema: "yanzhou-book/1",
		BootstrapToken:  "one-time-secret",
		RequestedFeatures: []string{
			"handshake",
		},
	}
	provenance := yanzhouprotocol.Provenance{
		SchemaVersion:      "1",
		UpstreamRepository: "denova",
		UpstreamBaseSHA:    "a111111111111111111111111111111111111111",
		AdapterCommitSHA:   "b222222222222222222222222222222222222222",
		SourceTreeSHA:      "c333333333333333333333333333333333333333",
		BinarySHA256:       strings.Repeat("d", 64),
		SkillsManifestSHA:  strings.Repeat("e", 64),
		GoVersion:          "go1.26.5",
		TargetOS:           "darwin",
		TargetArch:         "arm64",
		BuiltAt:            "2026-07-23T00:00:00Z",
	}

	response, err := Handshake(request, gate, provenance, "test-sidecar")
	if err != nil {
		t.Fatal(err)
	}
	if len(response.AgentKinds) != 9 || response.ProtocolVersion != ProtocolVersion {
		t.Fatalf("unexpected handshake response: %#v", response)
	}
	if _, err := Handshake(request, gate, provenance, "test-sidecar"); err == nil {
		t.Fatal("bootstrap token must be invalid immediately after the first handshake")
	}
}
