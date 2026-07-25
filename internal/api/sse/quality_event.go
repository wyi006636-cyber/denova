package sse

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"denova/internal/agent"
	"denova/internal/quality/harness"
)

type QualityFrameErrorCode string

const (
	QualityFrameCodeInvalidEvent QualityFrameErrorCode = "quality_sse_invalid_event"
	QualityFrameCodeTypeMismatch QualityFrameErrorCode = "quality_sse_type_mismatch"
	QualityFrameCodeWriteFailed  QualityFrameErrorCode = "quality_sse_write_failed"
)

// QualityFrameError keeps validation and I/O details out of the public error
// string while retaining the cause for diagnostics.
type QualityFrameError struct {
	Code  QualityFrameErrorCode
	Cause error
}

func (err *QualityFrameError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return "quality SSE error: " + string(err.Code)
}

func (err *QualityFrameError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

var (
	qualityFrameValidatorOnce sync.Once
	qualityFrameValidator     *harness.Validator
	qualityFrameValidatorErr  error
)

// DecodeQualityEvent admits agent.Event only as a transport wrapper around an
// exact-v1 Quality envelope.
func DecodeQualityEvent(event agent.Event) (harness.Event, error) {
	validator, err := sseQualityEventValidator()
	if err != nil {
		return harness.Event{}, qualityFrameError(QualityFrameCodeInvalidEvent, err)
	}
	raw, err := json.Marshal(event.Data)
	if err != nil {
		return harness.Event{}, qualityFrameError(QualityFrameCodeInvalidEvent, err)
	}
	validated, err := validator.ValidateJSON(raw)
	if err != nil {
		return harness.Event{}, qualityFrameError(QualityFrameCodeInvalidEvent, err)
	}
	if event.Type != string(validated.EventType) {
		return harness.Event{}, qualityFrameError(QualityFrameCodeTypeMismatch, errors.New("agent event type does not match Quality envelope"))
	}
	return validated, nil
}

// WriteQualityEventFrame emits one standard SSE frame without synthesizing or
// rewriting producer-owned event identity, sequence, or time.
func WriteQualityEventFrame(w io.Writer, event agent.Event) error {
	validated, err := DecodeQualityEvent(event)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(validated)
	if err != nil {
		return qualityFrameError(QualityFrameCodeInvalidEvent, err)
	}
	if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", validated.EventID, validated.EventType, raw); err != nil {
		return qualityFrameError(QualityFrameCodeWriteFailed, err)
	}
	return nil
}

func sseQualityEventValidator() (*harness.Validator, error) {
	qualityFrameValidatorOnce.Do(func() {
		qualityFrameValidator, qualityFrameValidatorErr = harness.NewValidator()
	})
	return qualityFrameValidator, qualityFrameValidatorErr
}

func qualityFrameError(code QualityFrameErrorCode, cause error) error {
	return &QualityFrameError{Code: code, Cause: cause}
}
