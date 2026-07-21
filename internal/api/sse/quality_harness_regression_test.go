package sse

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	hertzapp "github.com/cloudwego/hertz/pkg/app"

	"denova/internal/agent"
	novaApp "denova/internal/app"
)

func TestQualityHarnessSSEReconnectReplaysCompletionWithoutRerunningTask(t *testing.T) {
	firstEventEmitted := make(chan struct{})
	releaseLiveEvents := make(chan struct{})
	var executions atomic.Int32
	task := novaApp.NewTask(func(_ context.Context, _ *novaApp.Task, emit func(agent.Event)) {
		executions.Add(1)
		emit(agent.Event{Type: "snapshot", Data: map[string]string{"sequence": "one"}})
		close(firstEventEmitted)
		<-releaseLiveEvents
		emit(agent.Event{Type: "live", Data: map[string]string{"sequence": "two"}})
		emit(agent.Event{Type: "done", Data: map[string]string{"sequence": "three"}})
	})

	<-firstEventEmitted
	var firstContext hertzapp.RequestContext
	StreamTask(&firstContext, task)
	reader := bufio.NewReader(firstContext.Response.BodyStream())
	prefix := readSSEFrame(t, reader)
	close(releaseLiveEvents)
	remainder, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read first SSE stream: %v", err)
	}
	firstStream := prefix + string(remainder)
	assertQualityHarnessSSESequence(t, "initial stream", firstStream, []string{"snapshot", "live", "done"})

	var reconnectContext hertzapp.RequestContext
	StreamTask(&reconnectContext, task)
	replayed, err := io.ReadAll(reconnectContext.Response.BodyStream())
	if err != nil {
		t.Fatalf("read reconnect SSE stream: %v", err)
	}
	assertQualityHarnessSSESequence(t, "reconnect replay", string(replayed), []string{"snapshot", "live", "done"})
	if executions.Load() != 1 {
		t.Fatalf("SSE reconnect task executions = %d, want 1", executions.Load())
	}
}

func readSSEFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE snapshot frame: %v", err)
		}
		frame.WriteString(line)
		if line == "\n" {
			return frame.String()
		}
	}
}

func assertQualityHarnessSSESequence(t *testing.T, delivery string, stream string, events []string) {
	t.Helper()
	previous := -1
	for _, eventType := range events {
		marker := "event: " + eventType + "\n"
		if count := strings.Count(stream, marker); count != 1 {
			t.Fatalf("%s SSE event %q count = %d, want 1; stream=%q", delivery, eventType, count, stream)
		}
		index := strings.Index(stream, marker)
		if index <= previous {
			t.Fatalf("%s SSE events out of order at %q; stream=%q", delivery, eventType, stream)
		}
		previous = index
	}
	if !strings.Contains(stream, `"sequence":"three"`) {
		t.Fatalf("%s SSE completion payload was not replayable: %q", delivery, stream)
	}
}
