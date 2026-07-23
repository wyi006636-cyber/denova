package yanzhouprotocol

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestEnvelopeAcceptsHandshakeAndExactAgentKinds(t *testing.T) {
	payload, err := json.Marshal(HandshakeResponse{
		ProtocolVersion: ProtocolVersion,
		SidecarBuild:    "test",
		Provenance: Provenance{
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
		},
		SupportedFeatures: []string{"handshake", "jsonl", "bootstrap-policy"},
		AgentKinds:        AgentKinds(),
		MaxFrameBytes:     DefaultMaxFrameBytes,
	})
	if err != nil {
		t.Fatal(err)
	}

	frame := Envelope{
		Kind:            KindHandshakeResponse,
		ProtocolVersion: ProtocolVersion,
		RequestID:       "request-1",
		Payload:         payload,
	}
	if err := frame.Validate(); err != nil {
		t.Fatalf("valid handshake response rejected: %v", err)
	}

	want := []string{
		"ide", "interactive_story", "config_manager", "interactive_director",
		"version_summary", "tool_agent", "image", "automation", "context_compaction",
	}
	if got := AgentKinds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("agent kinds mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEnvelopeFailsClosedForUnknownMajorKindAndIdentity(t *testing.T) {
	tests := []struct {
		name string
		in   Envelope
		code ErrorCode
	}{
		{
			name: "unknown protocol major",
			in:   Envelope{Kind: KindHandshakeRequest, ProtocolVersion: "2.0", RequestID: "r", Payload: json.RawMessage(`{}`)},
			code: CodeIncompatibleProtocol,
		},
		{
			name: "unknown frame kind",
			in:   Envelope{Kind: "filesystem.write", ProtocolVersion: ProtocolVersion, RequestID: "r", Payload: json.RawMessage(`{}`)},
			code: CodeUnknownKind,
		},
		{
			name: "missing request id",
			in:   Envelope{Kind: KindHandshakeRequest, ProtocolVersion: ProtocolVersion, Payload: json.RawMessage(`{}`)},
			code: CodeInvalidFrame,
		},
		{
			name: "missing run sequence",
			in:   Envelope{Kind: KindRunEvent, ProtocolVersion: ProtocolVersion, RunID: "run-1", Payload: json.RawMessage(`{}`)},
			code: CodeInvalidFrame,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Validate()
			var protocolErr *ProtocolError
			if !errors.As(err, &protocolErr) {
				t.Fatalf("expected ProtocolError, got %T: %v", err, err)
			}
			if protocolErr.Code != tt.code {
				t.Fatalf("got code %q, want %q", protocolErr.Code, tt.code)
			}
		})
	}
}

func TestProvenanceRequiresFiveIndependentIdentities(t *testing.T) {
	valid := Provenance{
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
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid provenance rejected: %v", err)
	}

	duplicateCommit := valid
	duplicateCommit.AdapterCommitSHA = duplicateCommit.UpstreamBaseSHA
	if err := duplicateCommit.Validate(); err == nil {
		t.Fatal("duplicate commit identities must be rejected")
	}

	duplicateSHA256 := valid
	duplicateSHA256.BinarySHA256 = duplicateSHA256.SkillsManifestSHA
	if err := duplicateSHA256.Validate(); err == nil {
		t.Fatal("duplicate sha256 identities must be rejected")
	}
}
