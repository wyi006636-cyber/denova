package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"denova/internal/agent"
	novaApp "denova/internal/app"
	"denova/internal/quality/harness"
)

func TestQualityReconnectReadsDurableStateBeforeSubscribe(t *testing.T) {
	readEntered := make(chan struct{})
	releaseRead := make(chan struct{})
	source := &fakeQualityDisplaySource{subscribeCalled: make(chan struct{}), live: make(chan agent.Event)}
	reader := &fakeDurableStateReader{
		responses: []fakeStateReadResponse{{receipt: qualityReceipt(0, 0)}},
		readHook: func() {
			close(readEntered)
			<-releaseRead
		},
	}
	result := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result <- fmt.Errorf("connect panic: %v", recovered)
			}
		}()
		session, err := NewQualityReconnectCoordinator(reader, source).Connect(context.Background(), harness.StateReadRequest{RunID: "run-001"})
		if session != nil {
			session.Close()
		}
		result <- err
	}()

	<-readEntered
	select {
	case <-source.subscribeCalled:
		t.Fatal("display source subscribed before durable state read returned")
	default:
	}
	close(releaseRead)
	if err := <-result; err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if source.unsubscribeCount.Load() != 1 {
		t.Fatalf("unsubscribe count = %d, want 1", source.unsubscribeCount.Load())
	}
}

func TestQualityReconnectStateFailureNeverSubscribes(t *testing.T) {
	tests := []struct {
		name    string
		request harness.StateReadRequest
		reader  *fakeDurableStateReader
	}{
		{
			name:    "durable read error",
			request: harness.StateReadRequest{RunID: "run-001"},
			reader:  &fakeDurableStateReader{responses: []fakeStateReadResponse{{err: errors.New("store unavailable")}}},
		},
		{
			name:    "unknown Last-Event-ID",
			request: harness.StateReadRequest{RunID: "run-001", LastEventID: "event-404"},
			reader:  &fakeDurableStateReader{responses: []fakeStateReadResponse{{err: errors.New("unknown event id")}}},
		},
		{
			name:    "invalid request rejected before read",
			request: harness.StateReadRequest{RunID: "bad\nrun"},
			reader:  &fakeDurableStateReader{},
		},
		{
			name:    "invalid receipt",
			request: harness.StateReadRequest{RunID: "run-001"},
			reader:  &fakeDurableStateReader{responses: []fakeStateReadResponse{{receipt: harness.StateReadReceipt{RunID: "other-run", StateRevision: 1}}}},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			source := &fakeQualityDisplaySource{live: make(chan agent.Event)}
			_, err := NewQualityReconnectCoordinator(test.reader, source).Connect(context.Background(), test.request)
			var reconnectErr *QualityReconnectError
			if !errors.As(err, &reconnectErr) {
				t.Fatalf("Connect() error = %v, want *QualityReconnectError", err)
			}
			if source.subscribeCount.Load() != 0 {
				t.Fatalf("subscriptions = %d, want 0", source.subscribeCount.Load())
			}
			if test.name == "invalid request rejected before read" && test.reader.callCount() != 0 {
				t.Fatalf("durable reads = %d, want 0", test.reader.callCount())
			}
		})
	}
}

func TestQualityReconnectNilTaskAdapterReadsDurableStateThenReturnsDisplayUnavailable(t *testing.T) {
	reader := &fakeDurableStateReader{responses: []fakeStateReadResponse{{receipt: qualityReceipt(0, 0)}}}
	var (
		session    *QualityReconnectSession
		err        error
		panicValue any
	)
	func() {
		defer func() { panicValue = recover() }()
		session, err = NewQualityReconnectCoordinator(reader, NewQualityTaskDisplaySource(nil)).Connect(
			context.Background(), harness.StateReadRequest{RunID: "run-001"},
		)
	}()
	if panicValue != nil {
		t.Fatalf("Connect() panicked after durable read: %v", panicValue)
	}
	if session != nil {
		t.Fatalf("Connect() session = %#v, want nil", session)
	}
	var reconnectErr *QualityReconnectError
	if !errors.As(err, &reconnectErr) || reconnectErr.Code != QualityReconnectCodeDisplaySource {
		t.Fatalf("Connect() error = %#v, want code %q", err, QualityReconnectCodeDisplaySource)
	}
	if err.Error() != "quality reconnect error: quality_reconnect_display_unavailable" {
		t.Fatalf("Connect() public error = %q", err)
	}
	if reader.callCount() != 1 || reconnectErr.Receipt != qualityReceipt(0, 0) {
		t.Fatalf("durable read evidence = calls %d receipt %#v", reader.callCount(), reconnectErr.Receipt)
	}
}

func TestQualityReconnectFiltersCursorAndPreservesPartialCompletionReplay(t *testing.T) {
	snapshot := []agent.Event{
		mustQualityAgentEvent(t, harness.EventWorkflowRunCreated, 1),
		mustQualityAgentEvent(t, harness.EventWorkflowStageCompleted, 2),
		mustQualityAgentEvent(t, harness.EventFinalizationCompleted, 3),
	}
	source := &fakeQualityDisplaySource{snapshot: snapshot, live: make(chan agent.Event)}
	reader := &fakeDurableStateReader{responses: []fakeStateReadResponse{{receipt: qualityReceipt(1, 3)}}}

	session, err := NewQualityReconnectCoordinator(reader, source).Connect(context.Background(), harness.StateReadRequest{
		RunID: "run-001", LastEventID: "event-001",
	})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	replay := session.Replay()
	if len(replay) != 2 {
		t.Fatalf("replay length = %d, want 2", len(replay))
	}
	wantTypes := []string{string(harness.EventWorkflowStageCompleted), string(harness.EventFinalizationCompleted)}
	if got := []string{replay[0].Type, replay[1].Type}; !reflect.DeepEqual(got, wantTypes) {
		t.Fatalf("replay types = %v, want %v", got, wantTypes)
	}
	for index, event := range replay {
		envelope, err := DecodeQualityEvent(event)
		if err != nil {
			t.Fatalf("decode replay %d: %v", index, err)
		}
		wantSequence := uint64(index + 2)
		if envelope.Sequence != wantSequence || envelope.EventID != fmt.Sprintf("event-%03d", wantSequence) || envelope.OccurredAt != "2026-07-24T12:34:56Z" {
			t.Fatalf("replay identity changed: %#v", envelope)
		}
	}
}

func TestQualityReconnectSnapshotLiveBoundaryHasNoDuplicate(t *testing.T) {
	live := make(chan agent.Event, 1)
	live <- mustQualityAgentEvent(t, harness.EventWorkflowStageStarted, 2)
	source := &fakeQualityDisplaySource{
		snapshot: []agent.Event{mustQualityAgentEvent(t, harness.EventWorkflowRunCreated, 1)},
		live:     live,
	}
	reader := &fakeDurableStateReader{responses: []fakeStateReadResponse{{receipt: qualityReceipt(0, 1)}}}
	session, err := NewQualityReconnectCoordinator(reader, source).Connect(context.Background(), harness.StateReadRequest{RunID: "run-001"})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	if replay := session.Replay(); len(replay) != 1 || replay[0].Type != string(harness.EventWorkflowRunCreated) {
		t.Fatalf("unexpected replay: %#v", replay)
	}
	event, err := session.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	envelope, err := DecodeQualityEvent(event)
	if err != nil {
		t.Fatalf("decode live event: %v", err)
	}
	if envelope.Sequence != 2 || envelope.EventID != "event-002" {
		t.Fatalf("unexpected live identity: %#v", envelope)
	}
}

func TestQualityReconnectInvalidReplayRereadsDurableState(t *testing.T) {
	tests := []struct {
		name     string
		snapshot []agent.Event
		wantCode QualityReconnectErrorCode
	}{
		{
			name: "gap",
			snapshot: []agent.Event{
				mustQualityAgentEvent(t, harness.EventWorkflowRunCreated, 1),
				mustQualityAgentEvent(t, harness.EventWorkflowStageCompleted, 3),
			},
			wantCode: QualityReconnectCodeSequence,
		},
		{
			name: "duplicate",
			snapshot: []agent.Event{
				mustQualityAgentEvent(t, harness.EventWorkflowRunCreated, 1),
				mustQualityAgentEvent(t, harness.EventWorkflowStageCompleted, 1),
			},
			wantCode: QualityReconnectCodeSequence,
		},
		{
			name: "regression",
			snapshot: []agent.Event{
				mustQualityAgentEvent(t, harness.EventWorkflowStageCompleted, 2),
				mustQualityAgentEvent(t, harness.EventWorkflowRunCreated, 1),
			},
			wantCode: QualityReconnectCodeSequence,
		},
		{
			name: "invalid quality data",
			snapshot: []agent.Event{{
				Type: string(harness.EventWorkflowRunCreated),
				Data: map[string]any{"contract": map[string]any{"kind": harness.ContractKind, "version": "v2"}},
			}},
			wantCode: QualityReconnectCodeInvalidEvent,
		},
		{
			name: "replay beyond durable state",
			snapshot: []agent.Event{
				mustQualityAgentEvent(t, harness.EventWorkflowRunCreated, 1),
				mustQualityAgentEvent(t, harness.EventWorkflowStageCompleted, 2),
				mustQualityAgentEvent(t, harness.EventFinalizationCompleted, 3),
			},
			wantCode: QualityReconnectCodeDisplayDrop,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			reader := &fakeDurableStateReader{responses: []fakeStateReadResponse{
				{receipt: qualityReceipt(0, 2)},
				{receipt: harness.StateReadReceipt{RunID: "run-001", StateRevision: 8, LastSequence: 3}},
			}}
			source := &fakeQualityDisplaySource{snapshot: test.snapshot, live: make(chan agent.Event)}

			_, err := NewQualityReconnectCoordinator(reader, source).Connect(context.Background(), harness.StateReadRequest{RunID: "run-001"})
			var reconnectErr *QualityReconnectError
			if !errors.As(err, &reconnectErr) {
				t.Fatalf("Connect() error = %v, want *QualityReconnectError", err)
			}
			if reconnectErr.Code != test.wantCode {
				t.Fatalf("error code = %q, want %q", reconnectErr.Code, test.wantCode)
			}
			if reconnectErr.Receipt.StateRevision != 8 || reader.callCount() != 2 {
				t.Fatalf("reread evidence = receipt %#v, calls %d", reconnectErr.Receipt, reader.callCount())
			}
			if source.unsubscribeCount.Load() != 1 {
				t.Fatalf("unsubscribe count = %d, want 1", source.unsubscribeCount.Load())
			}
		})
	}
}

func TestQualityReconnectLiveDiscontinuityAndDropRereadDurableState(t *testing.T) {
	tests := []struct {
		name     string
		live     func() <-chan agent.Event
		wantCode QualityReconnectErrorCode
	}{
		{
			name: "live gap",
			live: func() <-chan agent.Event {
				ch := make(chan agent.Event, 1)
				ch <- mustQualityAgentEvent(t, harness.EventWorkflowStageCompleted, 3)
				return ch
			},
			wantCode: QualityReconnectCodeSequence,
		},
		{
			name: "closed display channel with durable progress",
			live: func() <-chan agent.Event {
				ch := make(chan agent.Event)
				close(ch)
				return ch
			},
			wantCode: QualityReconnectCodeDisplayDrop,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			reader := &fakeDurableStateReader{responses: []fakeStateReadResponse{
				{receipt: qualityReceipt(0, 1)},
				{receipt: harness.StateReadReceipt{RunID: "run-001", StateRevision: 2, LastSequence: 3}},
			}}
			source := &fakeQualityDisplaySource{
				snapshot: []agent.Event{mustQualityAgentEvent(t, harness.EventWorkflowRunCreated, 1)},
				live:     test.live(),
			}
			session, err := NewQualityReconnectCoordinator(reader, source).Connect(context.Background(), harness.StateReadRequest{RunID: "run-001"})
			if err != nil {
				t.Fatalf("Connect() error = %v", err)
			}
			defer session.Close()

			_, err = session.Next(context.Background())
			var reconnectErr *QualityReconnectError
			if !errors.As(err, &reconnectErr) || reconnectErr.Code != test.wantCode {
				t.Fatalf("Next() error = %#v, want code %q", err, test.wantCode)
			}
			if reconnectErr.Receipt.LastSequence != 3 || reader.callCount() != 2 {
				t.Fatalf("reread evidence = receipt %#v, calls %d", reconnectErr.Receipt, reader.callCount())
			}
		})
	}
}

func TestQualityReconnectClosedDisplayAtDurableCursorEndsNormally(t *testing.T) {
	live := make(chan agent.Event)
	close(live)
	reader := &fakeDurableStateReader{responses: []fakeStateReadResponse{
		{receipt: qualityReceipt(0, 1)},
		{receipt: harness.StateReadReceipt{RunID: "run-001", StateRevision: 2, LastSequence: 1}},
	}}
	source := &fakeQualityDisplaySource{snapshot: []agent.Event{mustQualityAgentEvent(t, harness.EventWorkflowRunCreated, 1)}, live: live}
	session, err := NewQualityReconnectCoordinator(reader, source).Connect(context.Background(), harness.StateReadRequest{RunID: "run-001"})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()
	if _, err := session.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("Next() error = %v, want io.EOF", err)
	}
}

func TestQualityReconnectRejectsLiveEventIDDuplicatedBeforeCursor(t *testing.T) {
	duplicate := validSSEQualityEvent(harness.EventWorkflowStageCompleted, 2)
	duplicate.EventID = "event-001"
	adapted, err := novaApp.AdaptQualityEvent(duplicate)
	if err != nil {
		t.Fatalf("adapt duplicate fixture: %v", err)
	}
	live := make(chan agent.Event, 1)
	live <- adapted
	reader := &fakeDurableStateReader{responses: []fakeStateReadResponse{
		{receipt: qualityReceipt(1, 1)},
		{receipt: harness.StateReadReceipt{RunID: "run-001", StateRevision: 2, LastSequence: 2, ResumeAfterSequence: 1}},
	}}
	source := &fakeQualityDisplaySource{
		snapshot: []agent.Event{mustQualityAgentEvent(t, harness.EventWorkflowRunCreated, 1)},
		live:     live,
	}
	session, err := NewQualityReconnectCoordinator(reader, source).Connect(context.Background(), harness.StateReadRequest{RunID: "run-001", LastEventID: "event-001"})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	_, err = session.Next(context.Background())
	var reconnectErr *QualityReconnectError
	if !errors.As(err, &reconnectErr) || reconnectErr.Code != QualityReconnectCodeSequence {
		t.Fatalf("Next() error = %#v, want duplicate event ID sequence error", err)
	}
	if reader.callCount() != 2 {
		t.Fatalf("durable reads = %d, want 2", reader.callCount())
	}
}

func TestQualityReconnectCloseMakesNextEndImmediately(t *testing.T) {
	reader := &fakeDurableStateReader{responses: []fakeStateReadResponse{{receipt: qualityReceipt(0, 0)}}}
	source := &fakeQualityDisplaySource{live: make(chan agent.Event)}
	session, err := NewQualityReconnectCoordinator(reader, source).Connect(context.Background(), harness.StateReadRequest{RunID: "run-001"})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	session.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := session.Next(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("Next() after Close error = %v, want io.EOF", err)
	}
	if source.unsubscribeCount.Load() != 1 {
		t.Fatalf("unsubscribe count = %d, want 1", source.unsubscribeCount.Load())
	}
}

func TestQualityReconnectSameTaskExecutesOnceAcrossReconnect(t *testing.T) {
	firstEmitted := make(chan struct{})
	releaseCompletion := make(chan struct{})
	taskFinished := make(chan struct{})
	var executions atomic.Int32
	task := novaApp.NewTask(func(_ context.Context, _ *novaApp.Task, emit func(agent.Event)) {
		executions.Add(1)
		emit(mustQualityAgentEvent(t, harness.EventWorkflowRunCreated, 1))
		close(firstEmitted)
		<-releaseCompletion
		emit(mustQualityAgentEvent(t, harness.EventFinalizationCompleted, 2))
		close(taskFinished)
	})
	<-firstEmitted
	reader := &fakeDurableStateReader{responses: []fakeStateReadResponse{
		{receipt: qualityReceipt(0, 1)},
		{receipt: harness.StateReadReceipt{RunID: "run-001", StateRevision: 2, LastSequence: 2, ResumeAfterSequence: 1}},
	}}
	coordinator := NewQualityReconnectCoordinator(reader, NewQualityTaskDisplaySource(task))
	first, err := coordinator.Connect(context.Background(), harness.StateReadRequest{RunID: "run-001"})
	if err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}
	first.Close()
	close(releaseCompletion)
	<-taskFinished

	second, err := coordinator.Connect(context.Background(), harness.StateReadRequest{RunID: "run-001", LastEventID: "event-001"})
	if err != nil {
		t.Fatalf("second Connect() error = %v", err)
	}
	defer second.Close()
	if replay := second.Replay(); len(replay) != 1 || replay[0].Type != string(harness.EventFinalizationCompleted) {
		t.Fatalf("completion replay = %#v", replay)
	}
	if executions.Load() != 1 {
		t.Fatalf("task executions = %d, want 1", executions.Load())
	}
}

type fakeStateReadResponse struct {
	receipt harness.StateReadReceipt
	err     error
}

type fakeDurableStateReader struct {
	mu        sync.Mutex
	requests  []harness.StateReadRequest
	responses []fakeStateReadResponse
	readHook  func()
}

func (reader *fakeDurableStateReader) ReadState(_ context.Context, request harness.StateReadRequest) (harness.StateReadReceipt, error) {
	if reader.readHook != nil {
		reader.readHook()
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.requests = append(reader.requests, request)
	if len(reader.responses) == 0 {
		return harness.StateReadReceipt{}, errors.New("unexpected durable read")
	}
	response := reader.responses[0]
	reader.responses = reader.responses[1:]
	return response.receipt, response.err
}

func (reader *fakeDurableStateReader) callCount() int {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return len(reader.requests)
}

type fakeQualityDisplaySource struct {
	snapshot         []agent.Event
	live             <-chan agent.Event
	subscribeCalled  chan struct{}
	subscribeCount   atomic.Int32
	unsubscribeCount atomic.Int32
}

func (source *fakeQualityDisplaySource) Subscribe() ([]agent.Event, <-chan agent.Event) {
	source.subscribeCount.Add(1)
	if source.subscribeCalled != nil {
		close(source.subscribeCalled)
	}
	return append([]agent.Event(nil), source.snapshot...), source.live
}

func (source *fakeQualityDisplaySource) Unsubscribe(<-chan agent.Event) {
	source.unsubscribeCount.Add(1)
}

func qualityReceipt(resumeAfter, lastSequence uint64) harness.StateReadReceipt {
	return harness.StateReadReceipt{
		RunID:               "run-001",
		StateRevision:       1,
		LastSequence:        lastSequence,
		ResumeAfterSequence: resumeAfter,
	}
}

func mustQualityAgentEvent(t *testing.T, eventType harness.EventType, sequence uint64) agent.Event {
	t.Helper()
	event := validSSEQualityEvent(eventType, sequence)
	adapted, err := novaApp.AdaptQualityEvent(event)
	if err != nil {
		t.Fatalf("adapt Quality event fixture: %v", err)
	}
	return adapted
}
