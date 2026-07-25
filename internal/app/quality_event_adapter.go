package app

import (
	"encoding/json"
	"errors"
	"sync"

	"denova/internal/agent"
	"denova/internal/quality/harness"
)

type QualityEventErrorCode string

const QualityEventCodeInvalidEnvelope QualityEventErrorCode = "quality_event_invalid_envelope"

// QualityEventAppError exposes only a bounded application code while retaining
// the validation cause for local diagnostics.
type QualityEventAppError struct {
	Code  QualityEventErrorCode
	Cause error
}

func (err *QualityEventAppError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return "quality event application error: " + string(err.Code)
}

func (err *QualityEventAppError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

var (
	qualityEventValidatorOnce sync.Once
	qualityEventValidator     *harness.Validator
	qualityEventValidatorErr  error
)

// AdaptQualityEvent admits one exact-v1 Quality envelope and maps it without
// granting agent.Event.Data authority over the Quality contract.
func AdaptQualityEvent(event harness.Event) (agent.Event, error) {
	validator, err := appQualityEventValidator()
	if err != nil {
		return agent.Event{}, invalidQualityEventError(err)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return agent.Event{}, invalidQualityEventError(err)
	}
	validated, err := validator.ValidateJSON(raw)
	if err != nil {
		return agent.Event{}, invalidQualityEventError(err)
	}
	return agent.Event{Type: string(validated.EventType), Data: validated}, nil
}

func appQualityEventValidator() (*harness.Validator, error) {
	qualityEventValidatorOnce.Do(func() {
		qualityEventValidator, qualityEventValidatorErr = harness.NewValidator()
	})
	return qualityEventValidator, qualityEventValidatorErr
}

func invalidQualityEventError(cause error) error {
	if cause == nil {
		cause = errors.New("invalid Quality event")
	}
	return &QualityEventAppError{Code: QualityEventCodeInvalidEnvelope, Cause: cause}
}
