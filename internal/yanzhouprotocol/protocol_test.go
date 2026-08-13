package yanzhouprotocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestJSONLFramingRoundTripAndBoundaries(t *testing.T) {
	frame := Envelope{
		Kind:            KindRuntimeError,
		ProtocolVersion: ProtocolVersion,
		RequestID:       "request-1",
		Payload:         []byte(`{"code":"fixture","message":"中文诊断"}`),
	}
	var output bytes.Buffer
	if err := WriteFrame(&output, frame); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(output.Bytes(), []byte{'\n'}) != 1 {
		t.Fatalf("frame must be one JSONL record: %q", output.Bytes())
	}
	decoded, err := NewReader(&output, DefaultMaxFrameBytes).ReadFrame()
	if err != nil || decoded.Kind != KindRuntimeError || decoded.RequestID != "request-1" {
		t.Fatalf("round trip = %#v, %v", decoded, err)
	}

	for name, input := range map[string]string{
		"partial": strings.TrimSuffix(string(mustFrame(t, frame)), "\n"),
		"noise":   "sidecar starting\n",
		"unknown": `{"kind":"filesystem.write","protocolVersion":"1.0","requestId":"r","payload":{}}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewReader(strings.NewReader(input), DefaultMaxFrameBytes).ReadFrame()
			var protocolErr *ProtocolError
			if !errors.As(err, &protocolErr) {
				t.Fatalf("expected protocol error, got %v", err)
			}
		})
	}
	if _, err := NewReader(strings.NewReader(""), DefaultMaxFrameBytes).ReadFrame(); !errors.Is(err, io.EOF) {
		t.Fatalf("clean EOF = %v", err)
	}
}

func mustFrame(t *testing.T, frame Envelope) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := WriteFrame(&output, frame); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
