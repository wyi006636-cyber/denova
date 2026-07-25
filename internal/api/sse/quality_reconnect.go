package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"denova/internal/agent"
	novaApp "denova/internal/app"
	"denova/internal/quality/harness"
)

type QualityReconnectErrorCode string

const (
	QualityReconnectCodeInvalidRequest QualityReconnectErrorCode = "quality_reconnect_invalid_request"
	QualityReconnectCodeStateRead      QualityReconnectErrorCode = "quality_reconnect_state_read_failed"
	QualityReconnectCodeInvalidReceipt QualityReconnectErrorCode = "quality_reconnect_invalid_receipt"
	QualityReconnectCodeDisplaySource  QualityReconnectErrorCode = "quality_reconnect_display_unavailable"
	QualityReconnectCodeInvalidEvent   QualityReconnectErrorCode = "quality_reconnect_invalid_event"
	QualityReconnectCodeSequence       QualityReconnectErrorCode = "quality_reconnect_sequence_discontinuity"
	QualityReconnectCodeDisplayDrop    QualityReconnectErrorCode = "quality_reconnect_display_drop"
)

// QualityReconnectError reports a bounded reconnect classification and the
// latest validated durable receipt available to the caller.
type QualityReconnectError struct {
	Code    QualityReconnectErrorCode
	Receipt harness.StateReadReceipt
	Cause   error
}

func (err *QualityReconnectError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return "quality reconnect error: " + string(err.Code)
}

func (err *QualityReconnectError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// QualityDisplaySource is the display-only snapshot/live boundary consumed by
// reconnect coordination. It deliberately has no execution or persistence API.
type QualityDisplaySource interface {
	Subscribe() ([]agent.Event, <-chan agent.Event)
	Unsubscribe(<-chan agent.Event)
}

type qualityTaskDisplaySource struct {
	task *novaApp.Task
}

// NewQualityTaskDisplaySource adapts the existing Task transport without
// changing its best-effort buffering or subscriber semantics.
func NewQualityTaskDisplaySource(task *novaApp.Task) QualityDisplaySource {
	if task == nil {
		return nil
	}
	return &qualityTaskDisplaySource{task: task}
}

func (source *qualityTaskDisplaySource) Subscribe() ([]agent.Event, <-chan agent.Event) {
	return source.task.Subscribe()
}

func (source *qualityTaskDisplaySource) Unsubscribe(ch <-chan agent.Event) {
	source.task.Unsubscribe(ch)
}

type QualityReconnectCoordinator struct {
	reader harness.DurableStateReader
	source QualityDisplaySource
}

func NewQualityReconnectCoordinator(reader harness.DurableStateReader, source QualityDisplaySource) *QualityReconnectCoordinator {
	return &QualityReconnectCoordinator{reader: reader, source: source}
}

// Connect performs durable-read-first admission and atomically acquires the
// existing display snapshot/live subscription only after the receipt is valid.
func (coordinator *QualityReconnectCoordinator) Connect(ctx context.Context, request harness.StateReadRequest) (*QualityReconnectSession, error) {
	if err := validateQualityReconnectRequest(request); err != nil {
		return nil, reconnectError(QualityReconnectCodeInvalidRequest, harness.StateReadReceipt{}, err)
	}
	if coordinator == nil || coordinator.reader == nil {
		return nil, reconnectError(QualityReconnectCodeStateRead, harness.StateReadReceipt{}, errors.New("durable state reader is unavailable"))
	}
	receipt, err := coordinator.reader.ReadState(ctx, request)
	if err != nil {
		return nil, reconnectError(QualityReconnectCodeStateRead, harness.StateReadReceipt{}, err)
	}
	if err := harness.ValidateStateReadContract(request, receipt); err != nil {
		return nil, reconnectError(QualityReconnectCodeInvalidReceipt, harness.StateReadReceipt{}, err)
	}
	if coordinator.source == nil {
		return nil, reconnectError(QualityReconnectCodeDisplaySource, receipt, errors.New("display source is unavailable"))
	}

	snapshot, live := coordinator.source.Subscribe()
	if live == nil {
		return nil, coordinator.failAfterSubscribe(ctx, request, receipt, nil, QualityReconnectCodeDisplaySource, errors.New("display live subscription is unavailable"))
	}
	replay, cursor, seen, code, err := normalizeQualityReplay(receipt, snapshot)
	if err != nil {
		coordinator.source.Unsubscribe(live)
		return nil, coordinator.rereadError(ctx, request, receipt, code, err)
	}
	return &QualityReconnectSession{
		reader:  coordinator.reader,
		source:  coordinator.source,
		request: request,
		receipt: receipt,
		replay:  replay,
		live:    live,
		cursor:  cursor,
		seen:    seen,
		done:    make(chan struct{}),
	}, nil
}

func (coordinator *QualityReconnectCoordinator) failAfterSubscribe(ctx context.Context, request harness.StateReadRequest, receipt harness.StateReadReceipt, live <-chan agent.Event, code QualityReconnectErrorCode, cause error) error {
	if live != nil {
		coordinator.source.Unsubscribe(live)
	}
	return coordinator.rereadError(ctx, request, receipt, code, cause)
}

func (coordinator *QualityReconnectCoordinator) rereadError(ctx context.Context, request harness.StateReadRequest, prior harness.StateReadReceipt, code QualityReconnectErrorCode, cause error) error {
	current, err := coordinator.reader.ReadState(ctx, request)
	if err != nil {
		return reconnectError(code, prior, errors.Join(cause, err))
	}
	if err := harness.ValidateStateReadContract(request, current); err != nil {
		return reconnectError(code, prior, errors.Join(cause, err))
	}
	return reconnectError(code, current, cause)
}

// QualityReconnectSession separates replay from the existing live channel and
// owns exactly one Task-style subscription until Close.
type QualityReconnectSession struct {
	reader  harness.DurableStateReader
	source  QualityDisplaySource
	request harness.StateReadRequest
	receipt harness.StateReadReceipt
	replay  []agent.Event
	live    <-chan agent.Event
	cursor  uint64
	seen    map[string]struct{}
	close   sync.Once
	done    chan struct{}
}

func (session *QualityReconnectSession) Replay() []agent.Event {
	if session == nil {
		return nil
	}
	return append([]agent.Event(nil), session.replay...)
}

func (session *QualityReconnectSession) Receipt() harness.StateReadReceipt {
	if session == nil {
		return harness.StateReadReceipt{}
	}
	return session.receipt
}

// Next validates live display delivery against the replay cursor. A detected
// drop only requests a fresh durable receipt; it never restarts execution.
func (session *QualityReconnectSession) Next(ctx context.Context) (agent.Event, error) {
	if session == nil || session.live == nil {
		return agent.Event{}, io.EOF
	}
	select {
	case <-session.done:
		return agent.Event{}, io.EOF
	default:
	}
	select {
	case <-session.done:
		return agent.Event{}, io.EOF
	case <-ctx.Done():
		return agent.Event{}, ctx.Err()
	case event, ok := <-session.live:
		if !ok {
			current, err := session.reread(ctx)
			session.Close()
			if err != nil {
				return agent.Event{}, reconnectError(QualityReconnectCodeStateRead, session.receipt, err)
			}
			if current.LastSequence > session.cursor {
				return agent.Event{}, reconnectError(QualityReconnectCodeDisplayDrop, current, errors.New("display delivery ended before durable state"))
			}
			return agent.Event{}, io.EOF
		}
		envelope, err := DecodeQualityEvent(event)
		if err != nil {
			return agent.Event{}, session.failLive(ctx, QualityReconnectCodeInvalidEvent, err)
		}
		if envelope.RunID != session.request.RunID {
			return agent.Event{}, session.failLive(ctx, QualityReconnectCodeInvalidEvent, errors.New("live event belongs to another Run"))
		}
		if envelope.Sequence != session.cursor+1 {
			return agent.Event{}, session.failLive(ctx, QualityReconnectCodeSequence, fmt.Errorf("live sequence %d is not contiguous after %d", envelope.Sequence, session.cursor))
		}
		if _, duplicate := session.seen[envelope.EventID]; duplicate {
			return agent.Event{}, session.failLive(ctx, QualityReconnectCodeSequence, errors.New("live event_id is duplicated"))
		}
		session.cursor = envelope.Sequence
		session.seen[envelope.EventID] = struct{}{}
		return agent.Event{Type: string(envelope.EventType), Data: envelope}, nil
	}
}

func (session *QualityReconnectSession) Close() {
	if session == nil {
		return
	}
	session.close.Do(func() {
		close(session.done)
		if session.source != nil && session.live != nil {
			session.source.Unsubscribe(session.live)
		}
	})
}

func (session *QualityReconnectSession) failLive(ctx context.Context, code QualityReconnectErrorCode, cause error) error {
	current, err := session.reread(ctx)
	session.Close()
	if err != nil {
		return reconnectError(code, session.receipt, errors.Join(cause, err))
	}
	return reconnectError(code, current, cause)
}

func (session *QualityReconnectSession) reread(ctx context.Context) (harness.StateReadReceipt, error) {
	current, err := session.reader.ReadState(ctx, session.request)
	if err != nil {
		return harness.StateReadReceipt{}, err
	}
	if err := harness.ValidateStateReadContract(session.request, current); err != nil {
		return harness.StateReadReceipt{}, err
	}
	return current, nil
}

func normalizeQualityReplay(receipt harness.StateReadReceipt, snapshot []agent.Event) ([]agent.Event, uint64, map[string]struct{}, QualityReconnectErrorCode, error) {
	replay := make([]agent.Event, 0, len(snapshot))
	validated := make([]harness.Event, 0, len(snapshot))
	seen := make(map[string]struct{}, len(snapshot))
	expected := receipt.ResumeAfterSequence + 1
	for index, transportEvent := range snapshot {
		envelope, err := DecodeQualityEvent(transportEvent)
		if err != nil {
			return nil, receipt.ResumeAfterSequence, nil, QualityReconnectCodeInvalidEvent, fmt.Errorf("snapshot event %d: %w", index, err)
		}
		if envelope.RunID != receipt.RunID {
			return nil, receipt.ResumeAfterSequence, nil, QualityReconnectCodeInvalidEvent, fmt.Errorf("snapshot event %d belongs to another Run", index)
		}
		if _, duplicate := seen[envelope.EventID]; duplicate {
			return nil, receipt.ResumeAfterSequence, nil, QualityReconnectCodeSequence, fmt.Errorf("snapshot event %d duplicates event_id", index)
		}
		seen[envelope.EventID] = struct{}{}
		if envelope.Sequence <= receipt.ResumeAfterSequence {
			continue
		}
		if envelope.Sequence != expected {
			return nil, receipt.ResumeAfterSequence, nil, QualityReconnectCodeSequence, fmt.Errorf("snapshot event %d sequence %d is not contiguous after %d", index, envelope.Sequence, expected-1)
		}
		if envelope.Sequence > receipt.LastSequence {
			return nil, receipt.ResumeAfterSequence, nil, QualityReconnectCodeDisplayDrop, fmt.Errorf("snapshot event %d exceeds durable last_sequence", index)
		}
		validated = append(validated, envelope)
		replay = append(replay, agent.Event{Type: string(envelope.EventType), Data: envelope})
		expected++
	}
	if expected-1 < receipt.LastSequence {
		return nil, receipt.ResumeAfterSequence, nil, QualityReconnectCodeDisplayDrop, errors.New("display snapshot ends before durable last_sequence")
	}
	if err := harness.ValidateReplay(harness.ReplayCursor{RunID: receipt.RunID, Sequence: receipt.ResumeAfterSequence}, validated); err != nil {
		return nil, receipt.ResumeAfterSequence, nil, QualityReconnectCodeSequence, err
	}
	return replay, expected - 1, seen, "", nil
}

func validateQualityReconnectRequest(request harness.StateReadRequest) error {
	receipt := harness.StateReadReceipt{RunID: request.RunID, StateRevision: 1}
	if request.LastEventID != "" {
		receipt.LastSequence = 1
		receipt.ResumeAfterSequence = 1
	}
	return harness.ValidateStateReadContract(request, receipt)
}

func reconnectError(code QualityReconnectErrorCode, receipt harness.StateReadReceipt, cause error) error {
	return &QualityReconnectError{Code: code, Receipt: receipt, Cause: cause}
}
