package harness

import (
	"context"
	"fmt"
)

// StateReadRequest asks durable state authority to resolve an optional stored event ID.
type StateReadRequest struct {
	RunID       string
	LastEventID string
}

// StateReadReceipt is a bounded reconnect cursor, not a Run state model.
type StateReadReceipt struct {
	RunID               string
	StateRevision       uint64
	LastSequence        uint64
	ResumeAfterSequence uint64
}

// DurableStateReader is implemented only by the future owning durable Run store.
type DurableStateReader interface {
	ReadState(context.Context, StateReadRequest) (StateReadReceipt, error)
}

func ValidateStateReadContract(request StateReadRequest, receipt StateReadReceipt) error {
	if err := validateOpaqueID("request.run_id", request.RunID); err != nil {
		return err
	}
	if request.LastEventID != "" {
		if err := validateOpaqueID("request.last_event_id", request.LastEventID); err != nil {
			return err
		}
	}
	if receipt.RunID != request.RunID {
		return fmt.Errorf("receipt.run_id must match request.run_id")
	}
	if receipt.StateRevision == 0 {
		return fmt.Errorf("receipt.state_revision must be positive")
	}
	if receipt.ResumeAfterSequence > receipt.LastSequence {
		return fmt.Errorf("receipt.resume_after_sequence exceeds durable last_sequence")
	}
	if request.LastEventID == "" && receipt.ResumeAfterSequence != 0 {
		return fmt.Errorf("receipt.resume_after_sequence must be zero without Last-Event-ID")
	}
	if request.LastEventID != "" && receipt.ResumeAfterSequence == 0 {
		return fmt.Errorf("receipt.resume_after_sequence must resolve the supplied Last-Event-ID")
	}
	return nil
}
