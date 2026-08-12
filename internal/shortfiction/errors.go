package shortfiction

import "errors"

const (
	ErrorCodeCandidateMismatch = "candidate_mismatch"
	ErrorCodeInvalidSource     = "invalid_source"
	ErrorCodeInvalidProfile    = "invalid_profile"
	ErrorCodeOversized         = "oversized"
	ErrorCodeRevisionConflict  = "revision_conflict"
)

// Error is a stable, transport-neutral domain failure.
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func NewError(code, message string, details map[string]any) *Error {
	return &Error{Code: code, Message: message, Details: details}
}

func IsCode(err error, code string) bool {
	var typed *Error
	return errors.As(err, &typed) && typed.Code == code
}
