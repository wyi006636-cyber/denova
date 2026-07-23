package yanzhouprotocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func validHandshakeLine() string {
	return `{"kind":"handshake.request","protocolVersion":"1.0","requestId":"request-1","payload":{"protocolVersion":"1.0","clientBuild":"test","workspaceSchema":"yanzhou-book/1","bootstrapToken":"secret","requestedFeatures":[]}}` + "\n"
}

func TestReaderReadsOneUTF8JSONLFrame(t *testing.T) {
	reader := NewReader(strings.NewReader(validHandshakeLine()), DefaultMaxFrameBytes)
	frame, err := reader.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if frame.Kind != KindHandshakeRequest || frame.RequestID != "request-1" {
		t.Fatalf("unexpected frame: %#v", frame)
	}
}

func TestReaderFailsClosedForMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		max  int
		code ErrorCode
	}{
		{name: "partial line", data: []byte(strings.TrimSuffix(validHandshakeLine(), "\n")), max: DefaultMaxFrameBytes, code: CodeIncompleteFrame},
		{name: "oversized line", data: []byte(validHandshakeLine()), max: 32, code: CodeFrameTooLarge},
		{name: "bad json", data: []byte("{bad json}\n"), max: DefaultMaxFrameBytes, code: CodeBadJSON},
		{name: "stdout noise", data: []byte("starting sidecar\n"), max: DefaultMaxFrameBytes, code: CodeBadJSON},
		{name: "unknown kind", data: []byte(`{"kind":"future.kind","protocolVersion":"1.0","requestId":"r","payload":{}}` + "\n"), max: DefaultMaxFrameBytes, code: CodeUnknownKind},
		{name: "invalid utf8", data: append([]byte{'{'}, []byte{0xff, '}', '\n'}...), max: DefaultMaxFrameBytes, code: CodeInvalidUTF8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewReader(bytes.NewReader(tt.data), tt.max)
			_, err := reader.ReadFrame()
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

func TestReaderReturnsCleanEOFOnlyWithoutResidualBytes(t *testing.T) {
	reader := NewReader(strings.NewReader(""), DefaultMaxFrameBytes)
	_, err := reader.ReadFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean EOF, got %v", err)
	}
}

func TestWriteFrameEmitsExactlyOneJSONLine(t *testing.T) {
	frame := Envelope{
		Kind:            KindRuntimeError,
		ProtocolVersion: ProtocolVersion,
		RequestID:       "request-1",
		Payload:         []byte(`{"code":"test","message":"诊断"}`),
	}
	var output bytes.Buffer
	if err := WriteFrame(&output, frame); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(output.Bytes(), []byte("\n")) != 1 || output.Bytes()[output.Len()-1] != '\n' {
		t.Fatalf("expected exactly one newline-terminated frame, got %q", output.Bytes())
	}
	if !bytes.Contains(output.Bytes(), []byte("诊断")) {
		t.Fatalf("expected UTF-8 payload, got %q", output.Bytes())
	}
}
