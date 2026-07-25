package harness

import (
	"fmt"
	"math"
)

// ReplayCursor is the durable preceding sequence for one Run. Sequence zero
// denotes a complete stream that must begin at one.
type ReplayCursor struct {
	RunID    string
	Sequence uint64
}

// ValidateReplay verifies stored events without rewriting their producer identity or time.
func ValidateReplay(cursor ReplayCursor, events []Event) error {
	if err := validateOpaqueID("cursor.run_id", cursor.RunID); err != nil {
		return err
	}
	seenEventIDs := make(map[string]struct{}, len(events))
	previous := cursor.Sequence
	for index, event := range events {
		if err := ValidateEvent(event); err != nil {
			return fmt.Errorf("replay event %d is invalid: %w", index, err)
		}
		if event.RunID != cursor.RunID {
			return fmt.Errorf("replay event %d belongs to run %q, not %q", index, event.RunID, cursor.RunID)
		}
		if previous == math.MaxUint64 || event.Sequence != previous+1 {
			return fmt.Errorf("replay event %d sequence %d is not contiguous after %d", index, event.Sequence, previous)
		}
		if _, exists := seenEventIDs[event.EventID]; exists {
			return fmt.Errorf("replay event %d duplicates event_id %q", index, event.EventID)
		}
		seenEventIDs[event.EventID] = struct{}{}
		previous = event.Sequence
	}
	return nil
}
