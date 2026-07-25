package harness

import (
	"context"
	"errors"
	"testing"
)

type testDurableStateReader struct {
	receipt StateReadReceipt
	err     error
}

func (reader testDurableStateReader) ReadState(context.Context, StateReadRequest) (StateReadReceipt, error) {
	return reader.receipt, reader.err
}

func TestDurableStateReaderIsANarrowReadOnlyPort(t *testing.T) {
	var reader DurableStateReader = testDurableStateReader{
		receipt: StateReadReceipt{RunID: "run-001", StateRevision: 3, LastSequence: 7, ResumeAfterSequence: 4},
	}
	receipt, err := reader.ReadState(context.Background(), StateReadRequest{RunID: "run-001", LastEventID: "event-004"})
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if receipt != (StateReadReceipt{RunID: "run-001", StateRevision: 3, LastSequence: 7, ResumeAfterSequence: 4}) {
		t.Fatalf("unexpected bounded receipt: %#v", receipt)
	}

	wantErr := errors.New("unknown event ID")
	reader = testDurableStateReader{err: wantErr}
	if _, err := reader.ReadState(context.Background(), StateReadRequest{RunID: "run-001", LastEventID: "event-999"}); !errors.Is(err, wantErr) {
		t.Fatalf("unknown cursor error was not preserved: %v", err)
	}
}

func TestValidateStateReadContractRejectsInvalidRequestOrReceipt(t *testing.T) {
	validRequest := StateReadRequest{RunID: "run-001", LastEventID: "event-004"}
	validReceipt := StateReadReceipt{RunID: "run-001", StateRevision: 3, LastSequence: 7, ResumeAfterSequence: 4}
	if err := ValidateStateReadContract(validRequest, validReceipt); err != nil {
		t.Fatalf("valid read contract rejected: %v", err)
	}
	if err := ValidateStateReadContract(StateReadRequest{RunID: "run-001"}, StateReadReceipt{RunID: "run-001", StateRevision: 1, LastSequence: 0, ResumeAfterSequence: 0}); err != nil {
		t.Fatalf("valid no-cursor receipt rejected: %v", err)
	}

	tests := []struct {
		name    string
		request StateReadRequest
		receipt StateReadReceipt
	}{
		{"invalid request run", StateReadRequest{RunID: "x"}, validReceipt},
		{"invalid last event id", StateReadRequest{RunID: "run-001", LastEventID: "e1"}, validReceipt},
		{"different receipt run", validRequest, StateReadReceipt{RunID: "run-002", StateRevision: 3, LastSequence: 7, ResumeAfterSequence: 4}},
		{"zero state revision", validRequest, StateReadReceipt{RunID: "run-001", StateRevision: 0, LastSequence: 7, ResumeAfterSequence: 4}},
		{"cursor beyond durable sequence", validRequest, StateReadReceipt{RunID: "run-001", StateRevision: 3, LastSequence: 7, ResumeAfterSequence: 8}},
		{"provided cursor unresolved", validRequest, StateReadReceipt{RunID: "run-001", StateRevision: 3, LastSequence: 7, ResumeAfterSequence: 0}},
		{"absent cursor resolves nonzero", StateReadRequest{RunID: "run-001"}, StateReadReceipt{RunID: "run-001", StateRevision: 3, LastSequence: 7, ResumeAfterSequence: 4}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateStateReadContract(test.request, test.receipt); err == nil {
				t.Fatal("invalid read contract accepted")
			}
		})
	}
}
