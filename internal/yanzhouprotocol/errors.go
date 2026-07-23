package yanzhouprotocol

import "fmt"

type ErrorCode string

const (
	CodeBadJSON              ErrorCode = "bad_json"
	CodeFrameTooLarge        ErrorCode = "frame_too_large"
	CodeIncompleteFrame      ErrorCode = "incomplete_frame"
	CodeInvalidFrame         ErrorCode = "invalid_frame"
	CodeInvalidUTF8          ErrorCode = "invalid_utf8"
	CodeUnknownKind          ErrorCode = "unknown_kind"
	CodeIncompatibleProtocol ErrorCode = "incompatible_protocol"
)

// ProtocolError classifies transport failures without exposing input payloads.
type ProtocolError struct {
	Code   ErrorCode
	Detail string
	Cause  error
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Detail == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Detail)
}

func (e *ProtocolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func protocolError(code ErrorCode, detail string, cause error) error {
	return &ProtocolError{Code: code, Detail: detail, Cause: cause}
}
