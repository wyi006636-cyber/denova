package yanzhouprotocol

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ProtocolVersion      = "1.0"
	SupportedMajor       = 1
	DefaultMaxFrameBytes = 1024 * 1024

	KindHandshakeRequest  = "handshake.request"
	KindHandshakeResponse = "handshake.response"
	KindRunStart          = "run.start"
	KindRunCancel         = "run.cancel"
	KindRunResume         = "run.resume"
	KindRunEvent          = "run.event"
	KindToolRequest       = "tool.request"
	KindToolResponse      = "tool.response"
	KindRuntimeError      = "runtime.error"
)

var (
	knownKinds = map[string]struct{}{
		KindHandshakeRequest: {}, KindHandshakeResponse: {}, KindRunStart: {},
		KindRunCancel: {}, KindRunResume: {}, KindRunEvent: {}, KindToolRequest: {},
		KindToolResponse: {}, KindRuntimeError: {},
	}
	agentKinds = []string{
		"ide", "interactive_story", "config_manager", "interactive_director",
		"version_summary", "tool_agent", "image", "automation", "context_compaction",
	}
	sha1Pattern   = regexp.MustCompile(`^[a-f0-9]{40}$`)
	sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Envelope is the only value allowed on sidecar stdout and stdin.
type Envelope struct {
	Kind            string          `json:"kind"`
	ProtocolVersion string          `json:"protocolVersion"`
	RequestID       string          `json:"requestId,omitempty"`
	RunID           string          `json:"runId,omitempty"`
	Seq             uint64          `json:"seq,omitempty"`
	Payload         json.RawMessage `json:"payload"`
}

type HandshakeRequest struct {
	ProtocolVersion   string   `json:"protocolVersion"`
	ClientBuild       string   `json:"clientBuild"`
	WorkspaceSchema   string   `json:"workspaceSchema"`
	BootstrapToken    string   `json:"bootstrapToken"`
	RequestedFeatures []string `json:"requestedFeatures"`
}

type HandshakeResponse struct {
	ProtocolVersion   string     `json:"protocolVersion"`
	SidecarBuild      string     `json:"sidecarBuild"`
	Provenance        Provenance `json:"provenance"`
	SupportedFeatures []string   `json:"supportedFeatures"`
	AgentKinds        []string   `json:"agentKinds"`
	MaxFrameBytes     int        `json:"maxFrameBytes"`
}

type Provenance struct {
	SchemaVersion      string `json:"schemaVersion"`
	UpstreamRepository string `json:"upstreamRepository"`
	UpstreamBaseSHA    string `json:"upstreamBaseSha"`
	AdapterCommitSHA   string `json:"adapterCommitSha"`
	SourceTreeSHA      string `json:"sourceTreeSha"`
	BinarySHA256       string `json:"binarySha256"`
	SkillsManifestSHA  string `json:"skillsManifestSha"`
	GoVersion          string `json:"goVersion"`
	TargetOS           string `json:"targetOs"`
	TargetArch         string `json:"targetArch"`
	BuiltAt            string `json:"builtAt"`
}

func AgentKinds() []string {
	return append([]string(nil), agentKinds...)
}

func (e Envelope) Validate() error {
	major, err := protocolMajor(e.ProtocolVersion)
	if err != nil || major != SupportedMajor {
		return protocolError(CodeIncompatibleProtocol, "unsupported protocol major", err)
	}
	if _, ok := knownKinds[e.Kind]; !ok {
		return protocolError(CodeUnknownKind, fmt.Sprintf("unknown frame kind %q", e.Kind), nil)
	}
	if len(e.Payload) == 0 || !json.Valid(e.Payload) || strings.TrimSpace(string(e.Payload)) == "null" {
		return protocolError(CodeInvalidFrame, "payload must be a JSON value", nil)
	}
	firstPayloadByte := strings.TrimSpace(string(e.Payload))[0]
	if firstPayloadByte != '{' {
		return protocolError(CodeInvalidFrame, "payload must be a JSON object", nil)
	}

	switch e.Kind {
	case KindHandshakeRequest, KindHandshakeResponse, KindRunStart, KindRunCancel, KindRunResume:
		if strings.TrimSpace(e.RequestID) == "" {
			return protocolError(CodeInvalidFrame, "requestId is required", nil)
		}
	case KindRunEvent, KindToolRequest, KindToolResponse:
		if strings.TrimSpace(e.RunID) == "" || e.Seq == 0 {
			return protocolError(CodeInvalidFrame, "runId and positive seq are required", nil)
		}
	case KindRuntimeError:
		if strings.TrimSpace(e.RequestID) == "" && strings.TrimSpace(e.RunID) == "" {
			return protocolError(CodeInvalidFrame, "runtime.error requires requestId or runId", nil)
		}
	}

	if e.Kind == KindHandshakeRequest {
		var payload HandshakeRequest
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return protocolError(CodeInvalidFrame, "invalid handshake request", err)
		}
		if err := payload.Validate(); err != nil {
			return err
		}
	}
	if e.Kind == KindHandshakeResponse {
		var payload HandshakeResponse
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return protocolError(CodeInvalidFrame, "invalid handshake response", err)
		}
		if err := payload.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (r HandshakeRequest) Validate() error {
	major, err := protocolMajor(r.ProtocolVersion)
	if err != nil || major != SupportedMajor {
		return protocolError(CodeIncompatibleProtocol, "handshake protocol is incompatible", err)
	}
	if strings.TrimSpace(r.ClientBuild) == "" || r.WorkspaceSchema != "yanzhou-book/1" || r.BootstrapToken == "" {
		return protocolError(CodeInvalidFrame, "handshake clientBuild, workspaceSchema, and bootstrapToken are required", nil)
	}
	for _, feature := range r.RequestedFeatures {
		if strings.TrimSpace(feature) == "" {
			return protocolError(CodeInvalidFrame, "requested features must be non-empty strings", nil)
		}
	}
	return nil
}

func (r HandshakeResponse) Validate() error {
	major, err := protocolMajor(r.ProtocolVersion)
	if err != nil || major != SupportedMajor {
		return protocolError(CodeIncompatibleProtocol, "handshake protocol is incompatible", err)
	}
	if strings.TrimSpace(r.SidecarBuild) == "" || r.MaxFrameBytes < 1 {
		return protocolError(CodeInvalidFrame, "sidecarBuild and maxFrameBytes are required", nil)
	}
	if err := r.Provenance.Validate(); err != nil {
		return err
	}
	if len(r.AgentKinds) != len(agentKinds) {
		return protocolError(CodeInvalidFrame, "handshake must expose the exact nine Agent Kinds", nil)
	}
	seen := make(map[string]bool, len(r.AgentKinds))
	for index, kind := range r.AgentKinds {
		if kind != agentKinds[index] || seen[kind] {
			return protocolError(CodeInvalidFrame, "handshake must expose the exact ordered nine Agent Kinds", nil)
		}
		seen[kind] = true
	}
	return nil
}

func (p Provenance) Validate() error {
	if p.SchemaVersion != "1" || strings.TrimSpace(p.UpstreamRepository) == "" {
		return protocolError(CodeInvalidFrame, "provenance schema and repository are required", nil)
	}
	commitDigests := []string{p.UpstreamBaseSHA, p.AdapterCommitSHA, p.SourceTreeSHA}
	for _, digest := range commitDigests {
		if !sha1Pattern.MatchString(digest) {
			return protocolError(CodeInvalidFrame, "provenance commit/tree digest is invalid", nil)
		}
	}
	if commitDigests[0] == commitDigests[1] || commitDigests[0] == commitDigests[2] || commitDigests[1] == commitDigests[2] {
		return protocolError(CodeInvalidFrame, "provenance commit/tree identities must be distinct", nil)
	}
	if !sha256Pattern.MatchString(p.BinarySHA256) || !sha256Pattern.MatchString(p.SkillsManifestSHA) {
		return protocolError(CodeInvalidFrame, "provenance SHA-256 digest is invalid", nil)
	}
	if p.BinarySHA256 == p.SkillsManifestSHA {
		return protocolError(CodeInvalidFrame, "provenance SHA-256 identities must be distinct", nil)
	}
	if strings.TrimSpace(p.GoVersion) == "" || strings.TrimSpace(p.TargetOS) == "" || strings.TrimSpace(p.TargetArch) == "" {
		return protocolError(CodeInvalidFrame, "provenance toolchain and target are required", nil)
	}
	if _, err := time.Parse(time.RFC3339, p.BuiltAt); err != nil {
		return protocolError(CodeInvalidFrame, "provenance builtAt must be RFC3339", err)
	}
	return nil
}

func protocolMajor(version string) (int, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid protocol version %q", version)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid protocol version %q", version)
	}
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return 0, fmt.Errorf("invalid protocol version %q", version)
	}
	return major, nil
}
