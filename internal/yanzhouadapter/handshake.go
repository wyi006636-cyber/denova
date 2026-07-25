package yanzhouadapter

import (
	"fmt"
	"strings"

	"denova/config"
	"denova/internal/yanzhouprotocol"
)

var sidecarSupportedFeatures = []string{
	"handshake",
	"jsonl",
	"bootstrap-policy",
	"plan-mode",
	"skills-v2",
	"sub-agents",
	"tool-harness",
}

func Handshake(request yanzhouprotocol.HandshakeRequest, gate *BootstrapTokenGate, provenance yanzhouprotocol.Provenance, sidecarBuild string) (yanzhouprotocol.HandshakeResponse, error) {
	if err := request.Validate(); err != nil {
		return yanzhouprotocol.HandshakeResponse{}, err
	}
	if gate == nil {
		return yanzhouprotocol.HandshakeResponse{}, fmt.Errorf("bootstrap token gate is required")
	}
	if err := gate.consume(request.BootstrapToken); err != nil {
		return yanzhouprotocol.HandshakeResponse{}, err
	}
	if err := provenance.Validate(); err != nil {
		return yanzhouprotocol.HandshakeResponse{}, err
	}
	if strings.TrimSpace(sidecarBuild) == "" {
		return yanzhouprotocol.HandshakeResponse{}, fmt.Errorf("sidecar build is required")
	}

	definitions := config.AgentKindDefinitions()
	kinds := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		kinds = append(kinds, definition.Kind)
	}
	response := yanzhouprotocol.HandshakeResponse{
		ProtocolVersion:   ProtocolVersion,
		SidecarBuild:      sidecarBuild,
		Provenance:        provenance,
		SupportedFeatures: negotiateSidecarFeatures(request.RequestedFeatures),
		AgentKinds:        kinds,
		MaxFrameBytes:     yanzhouprotocol.DefaultMaxFrameBytes,
	}
	if err := response.Validate(); err != nil {
		return yanzhouprotocol.HandshakeResponse{}, err
	}
	return response, nil
}

func negotiateSidecarFeatures(requested []string) []string {
	requestedSet := make(map[string]struct{}, len(requested))
	for _, feature := range requested {
		requestedSet[feature] = struct{}{}
	}
	negotiated := make([]string, 0, len(sidecarSupportedFeatures))
	for _, feature := range sidecarSupportedFeatures {
		if _, ok := requestedSet[feature]; ok {
			negotiated = append(negotiated, feature)
		}
	}
	return negotiated
}
