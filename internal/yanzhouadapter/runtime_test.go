package yanzhouadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"denova/config"
	"denova/internal/agent"
	"denova/internal/book"
	"denova/internal/session"
	"denova/internal/yanzhouprotocol"
)

func TestDurableResumeFinishPanicMakesOnePanickedFallback(t *testing.T) {
	const sentinel = "sk-finishPanicSentinel-DoNotReflect-7x9q"
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		finishPanicRemaining: 1,
		finishPanicValue:     sentinel,
	}
	var doneEvents int
	var successStates int
	var publicErrors []string
	var escaped any

	func() {
		defer func() { escaped = recover() }()
		agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
			context.Background(),
			nil,
			conversation,
			nil,
			agent.ChatRequest{Message: "继续"},
			agent.RunOptions{TaskID: "resume-request-1"},
			func(event agent.Event) {
				data, _ := event.Data.(map[string]string)
				if event.Type == "done" {
					doneEvents++
				}
				if event.Type == "run_state" && data["phase"] == "finished" && data["status"] == "success" {
					successStates++
				}
				if event.Type == "error" {
					publicErrors = append(publicErrors, data["message"])
				}
			},
		)
	}()

	if escaped != nil {
		t.Fatalf("finish panic escaped Runtime.Run: %T", escaped)
	}
	wantOutcomes := []agent.ResumeAttemptFinishOutcome{
		agent.ResumeAttemptOutcomePrepareFailed,
		agent.ResumeAttemptOutcomePanicked,
	}
	if conversation.finishCalls != 2 || !reflect.DeepEqual(conversation.finishOutcomes, wantOutcomes) {
		t.Fatalf("finish panic lifecycle = calls %d outcomes %#v, want 2/%#v", conversation.finishCalls, conversation.finishOutcomes, wantOutcomes)
	}
	if len(conversation.finishContextCancelledHistory) != 2 || conversation.finishContextCancelledHistory[0] || conversation.finishContextCancelledHistory[1] {
		t.Fatalf("finish panic fallback contexts were canceled: %#v", conversation.finishContextCancelledHistory)
	}
	if conversation.resolveCalls != 0 || doneEvents != 0 || successStates != 0 {
		t.Fatalf("finish panic claimed success: resolve=%d done=%d success=%d", conversation.resolveCalls, doneEvents, successStates)
	}
	for _, message := range publicErrors {
		if strings.Contains(message, sentinel) {
			t.Fatalf("finish panic leaked through public error: %q", message)
		}
	}
}

func TestDurableResumeExecutionIdentityOwnsRunLedgerFileAndRecords(t *testing.T) {
	workspace := t.TempDir()
	agent.SetTraceRuntimeConfig(agent.TraceCaptureSummary, agent.TraceExporterLocal, 100)
	defer agent.SetTraceRuntimeConfig(agent.TraceCaptureSummary, agent.TraceExporterLocal, 100)
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
	}
	var startedRunID string

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		nil,
		conversation,
		book.NewService(workspace),
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(event agent.Event) {
			data, _ := event.Data.(map[string]string)
			if event.Type == "run_state" && data["phase"] == "started" {
				startedRunID = data["run_id"]
			}
		},
	)

	if startedRunID != "execution-run-1" || conversation.finishInput.Attempt.ExecutionRunID != "execution-run-1" {
		t.Fatalf("current runtime identities = event %q attempt %q", startedRunID, conversation.finishInput.Attempt.ExecutionRunID)
	}
	wantPath := filepath.Join(workspace, ".denova", "runs", "execution-run-1.jsonl")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("authoritative ledger file missing: %v", err)
	}
	trace, err := agent.ReadRunTrace(workspace, "execution-run-1")
	if err != nil {
		t.Fatalf("ReadRunTrace(execution-run-1): %v", err)
	}
	if trace.Summary.ID != "execution-run-1" || trace.Summary.Path != wantPath {
		t.Fatalf("trace summary identity = id %q path %q", trace.Summary.ID, trace.Summary.Path)
	}
	if len(trace.Records) == 0 {
		t.Fatal("authoritative run ledger has no records")
	}
	for index, record := range trace.Records {
		if record.RunID != "execution-run-1" {
			t.Fatalf("record %d run_id = %q, want execution-run-1", index, record.RunID)
		}
	}
}

func TestDurableResumeAttemptBeginsBeforePrepareMessages(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
	}

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		nil,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(agent.Event) {},
	)

	if conversation.beginCalls != 1 {
		t.Fatalf("BeginResumeAttempt calls = %d, want 1", conversation.beginCalls)
	}
	if conversation.prepareCalls != 1 {
		t.Fatalf("PrepareMessages calls = %d, want 1", conversation.prepareCalls)
	}
	if !conversation.prepareObservedBegin {
		t.Fatal("PrepareMessages ran before the durable resume attempt began")
	}
	if conversation.beginBasis.InterruptionID != conversation.pending.ID {
		t.Fatalf("begin interruption = %q, want %q", conversation.beginBasis.InterruptionID, conversation.pending.ID)
	}
}

func TestDurableResumePrepareFailureFinishesOnceAndLeavesInterruptionPending(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
	}

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		nil,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(agent.Event) {},
	)

	if conversation.finishCalls != 1 {
		t.Fatalf("FinishResumeAttempt calls = %d, want 1", conversation.finishCalls)
	}
	if conversation.finishInput.Attempt.AttemptID != "attempt-1" {
		t.Fatalf("finished attempt = %q, want attempt-1", conversation.finishInput.Attempt.AttemptID)
	}
	if conversation.finishInput.Outcome != agent.ResumeAttemptOutcomePrepareFailed {
		t.Fatalf("finish outcome = %q, want %q", conversation.finishInput.Outcome, agent.ResumeAttemptOutcomePrepareFailed)
	}
	if conversation.resolveCalls != 0 {
		t.Fatalf("legacy ResolveInterruption calls = %d, want 0", conversation.resolveCalls)
	}
	pending := conversation.PendingInterruption()
	if pending == nil || pending.ID != "interruption-origin-1" || pending.Status != session.InterruptionPending {
		t.Fatalf("original interruption was not left pending: %#v", pending)
	}
}

func TestDurableResumeUsesNewExecutionIdentityNotOrigin(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
	}
	var started map[string]string

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		nil,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(event agent.Event) {
			data, ok := event.Data.(map[string]string)
			if event.Type == "run_state" && ok && data["phase"] == "started" {
				started = data
			}
		},
	)

	if started == nil {
		t.Fatal("started run_state was not emitted")
	}
	if started["run_id"] != "execution-run-1" {
		t.Fatalf("runtime run_id = %q, want new execution-run-1", started["run_id"])
	}
	if started["run_id"] == "origin-run-1" {
		t.Fatal("origin run identity was reused as the resumed execution")
	}
}

func TestDurableResumeUserCommitFailureFinishesOnce(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		prepareSucceeds: true,
	}

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		nil,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{
			TaskID: "resume-request-1",
			OnUserMessageCommitted: func(context.Context) error {
				return errors.New("injected user commit failure")
			},
		},
		func(agent.Event) {},
	)

	if conversation.finishCalls != 1 {
		t.Fatalf("FinishResumeAttempt calls = %d, want 1", conversation.finishCalls)
	}
	if conversation.finishInput.Outcome != agent.ResumeAttemptOutcomeUserCommitFailed {
		t.Fatalf("finish outcome = %q, want %q", conversation.finishInput.Outcome, agent.ResumeAttemptOutcomeUserCommitFailed)
	}
	if conversation.resolveCalls != 0 {
		t.Fatalf("legacy ResolveInterruption calls = %d, want 0", conversation.resolveCalls)
	}
}

func TestDurableResumeCompactionFailureFinishesOnce(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		prepareSucceeds: true,
		compactionErr:   errors.New("injected compaction failure"),
	}

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		nil,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(agent.Event) {},
	)

	if conversation.compactionCalls != 1 {
		t.Fatalf("CompactContextIfNeeded calls = %d, want 1", conversation.compactionCalls)
	}
	if conversation.finishCalls != 1 {
		t.Fatalf("FinishResumeAttempt calls = %d, want 1", conversation.finishCalls)
	}
	if conversation.finishInput.Outcome != agent.ResumeAttemptOutcomeCompactionFailed {
		t.Fatalf("finish outcome = %q, want %q", conversation.finishInput.Outcome, agent.ResumeAttemptOutcomeCompactionFailed)
	}
}

func TestDurableResumeSuccessFinishesAfterAssistantPersistenceWithoutLegacyResolve(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		prepareSucceeds: true,
	}
	runnerAgent := &resumeTestAgent{event: &adk.AgentEvent{
		AgentName: "resume-test-agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Role:    schema.Assistant,
			Message: schema.AssistantMessage("durable assistant output", nil),
		}},
	}}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})
	var doneEvents int

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(event agent.Event) {
			if event.Type == "done" {
				doneEvents++
			}
		},
	)

	if conversation.appendCalls != 1 || conversation.appended != "durable assistant output" {
		t.Fatalf("assistant persistence = calls %d content %q", conversation.appendCalls, conversation.appended)
	}
	if conversation.finishCalls != 1 {
		t.Fatalf("FinishResumeAttempt calls = %d, want 1", conversation.finishCalls)
	}
	if !conversation.finishObservedPersist {
		t.Fatal("success lifecycle finish ran before assistant persistence")
	}
	if conversation.finishInput.Outcome != agent.ResumeAttemptOutcomeSucceeded {
		t.Fatalf("finish outcome = %q, want %q", conversation.finishInput.Outcome, agent.ResumeAttemptOutcomeSucceeded)
	}
	if conversation.resolveCalls != 0 {
		t.Fatalf("legacy ResolveInterruption calls = %d, want 0", conversation.resolveCalls)
	}
	if doneEvents != 1 {
		t.Fatalf("done events = %d, want 1", doneEvents)
	}
}

func TestDurableResumeAssistantPersistenceFailureFinishesNonSuccessAndStaysPending(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		prepareSucceeds: true,
		appendErr:       errors.New("injected assistant persistence failure"),
	}
	runnerAgent := &resumeTestAgent{event: &adk.AgentEvent{
		AgentName: "resume-test-agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Role:    schema.Assistant,
			Message: schema.AssistantMessage("assistant output that cannot persist", nil),
		}},
	}}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})
	var doneEvents int

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(event agent.Event) {
			if event.Type == "done" {
				doneEvents++
			}
		},
	)

	if conversation.appendCalls != 1 {
		t.Fatalf("AppendAssistant calls = %d, want 1", conversation.appendCalls)
	}
	if conversation.finishCalls != 1 {
		t.Fatalf("FinishResumeAttempt calls = %d, want 1", conversation.finishCalls)
	}
	if conversation.finishInput.Outcome != agent.ResumeAttemptOutcomeAssistantPersistFailed {
		t.Fatalf("finish outcome = %q, want %q", conversation.finishInput.Outcome, agent.ResumeAttemptOutcomeAssistantPersistFailed)
	}
	if conversation.resolveCalls != 0 || doneEvents != 0 {
		t.Fatalf("failed persistence resolved or completed: resolve=%d done=%d", conversation.resolveCalls, doneEvents)
	}
	pending := conversation.PendingInterruption()
	if pending == nil || pending.Status != session.InterruptionPending {
		t.Fatalf("original interruption was not left pending: %#v", pending)
	}
}

func TestDurableResumeSuccessFinishMustConfirmInterruptionResolution(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		prepareSucceeds:     true,
		finishLeavesPending: true,
	}
	runnerAgent := &resumeTestAgent{event: &adk.AgentEvent{
		AgentName: "resume-test-agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Role:    schema.Assistant,
			Message: schema.AssistantMessage("persisted but unresolved", nil),
		}},
	}}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})
	var doneEvents int
	var successFinishedStates int

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(event agent.Event) {
			data, _ := event.Data.(map[string]string)
			if event.Type == "done" {
				doneEvents++
			}
			if event.Type == "run_state" && data["phase"] == "finished" && data["status"] == "success" {
				successFinishedStates++
			}
		},
	)

	if conversation.finishCalls != 1 {
		t.Fatalf("FinishResumeAttempt calls = %d, want 1", conversation.finishCalls)
	}
	if doneEvents != 0 || successFinishedStates != 0 {
		t.Fatalf("unresolved success was claimed: done=%d success_states=%d", doneEvents, successFinishedStates)
	}
	if conversation.resolveCalls != 0 {
		t.Fatalf("legacy ResolveInterruption calls = %d, want 0", conversation.resolveCalls)
	}
}

func TestDurableResumePanicFinishesOnceWithoutLegacyResolve(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		preparePanics: true,
	}

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		nil,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(agent.Event) {},
	)

	if conversation.finishCalls != 1 {
		t.Fatalf("FinishResumeAttempt calls = %d, want 1", conversation.finishCalls)
	}
	if conversation.finishInput.Outcome != agent.ResumeAttemptOutcomePanicked {
		t.Fatalf("finish outcome = %q, want %q", conversation.finishInput.Outcome, agent.ResumeAttemptOutcomePanicked)
	}
	if conversation.resolveCalls != 0 {
		t.Fatalf("legacy ResolveInterruption calls = %d, want 0", conversation.resolveCalls)
	}
}

func TestDurableResumeRunnerErrorFinishesOnceWithoutLegacyResolve(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		prepareSucceeds: true,
	}
	runnerAgent := &resumeTestAgent{event: &adk.AgentEvent{Err: errors.New("injected runner failure")}}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(agent.Event) {},
	)

	if conversation.finishCalls != 1 {
		t.Fatalf("FinishResumeAttempt calls = %d, want 1", conversation.finishCalls)
	}
	if conversation.finishInput.Outcome != agent.ResumeAttemptOutcomeProviderFailed {
		t.Fatalf("finish outcome = %q, want %q", conversation.finishInput.Outcome, agent.ResumeAttemptOutcomeProviderFailed)
	}
	if conversation.resolveCalls != 0 {
		t.Fatalf("legacy ResolveInterruption calls = %d, want 0", conversation.resolveCalls)
	}
}

func TestDurableRuntimeConversationBridgesLifecyclePortAcrossRestart(t *testing.T) {
	basis := agent.ResumeAttemptBasis{
		OperationID:    "resume-operation-1",
		InterruptionID: "interruption-origin-1",
	}
	attempt := agent.ResumeAttemptIdentity{
		AttemptID:      "attempt-1",
		ExecutionRunID: "execution-run-1",
		InterruptionID: basis.InterruptionID,
		OriginRunID:    "origin-run-1",
		AttemptNumber:  1,
		Status:         agent.ResumeAttemptStatusRunning,
		Validation: agent.ResumeValidationReceipt{
			OperationID:    basis.OperationID,
			InterruptionID: basis.InterruptionID,
			OriginRunID:    "origin-run-1",
		},
	}
	port := &resumeLifecyclePortProbe{attempt: attempt}
	base := &resumeOrderingConversation{pending: session.Interruption{ID: basis.InterruptionID, Status: session.InterruptionPending}}

	first, err := NewDurableRuntimeConversation(base, port)
	if err != nil {
		t.Fatal(err)
	}
	gotAttempt, err := first.BeginResumeAttempt(context.Background(), basis)
	if err != nil || !reflect.DeepEqual(gotAttempt, attempt) {
		t.Fatalf("first begin = %#v, %v", gotAttempt, err)
	}
	finish := agent.ResumeAttemptFinish{Attempt: gotAttempt, Outcome: agent.ResumeAttemptOutcomePrepareFailed}
	wantReceipt := agent.ResumeAttemptFinishReceipt{Attempt: gotAttempt, Outcome: finish.Outcome}
	wantReceipt.Attempt.Status = agent.ResumeAttemptStatusFailed
	port.receipt = wantReceipt
	gotReceipt, err := first.FinishResumeAttempt(context.Background(), finish)
	if err != nil || !reflect.DeepEqual(gotReceipt, wantReceipt) {
		t.Fatalf("first finish = %#v, %v", gotReceipt, err)
	}

	restarted, err := NewDurableRuntimeConversation(base, port)
	if err != nil {
		t.Fatal(err)
	}
	retriedAttempt, err := restarted.BeginResumeAttempt(context.Background(), basis)
	if err != nil || !reflect.DeepEqual(retriedAttempt, attempt) {
		t.Fatalf("restart retry begin = %#v, %v", retriedAttempt, err)
	}
	if port.beginCalls != 2 || port.finishCalls != 1 {
		t.Fatalf("port calls after restart = begin %d finish %d", port.beginCalls, port.finishCalls)
	}
}

func TestDurableRuntimeConversationRejectsSensitiveOrUnboundedResumeBasis(t *testing.T) {
	const sentinel = "sk-resumeLifecycleSentinel-DoNotReflect-7x9q"
	cases := []struct {
		name  string
		basis agent.ResumeAttemptBasis
	}{
		{
			name: "sensitive operation identity",
			basis: agent.ResumeAttemptBasis{
				OperationID:    sentinel,
				InterruptionID: "interruption-origin-1",
			},
		},
		{
			name: "sensitive interruption identity",
			basis: agent.ResumeAttemptBasis{
				OperationID:    "resume-operation-1",
				InterruptionID: "Bearer " + sentinel,
			},
		},
		{
			name: "unbounded operation identity",
			basis: agent.ResumeAttemptBasis{
				OperationID:    strings.Repeat("x", 129),
				InterruptionID: "interruption-origin-1",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := &resumeLifecyclePortProbe{}
			base := &resumeOrderingConversation{pending: session.Interruption{ID: "interruption-origin-1", Status: session.InterruptionPending}}
			conversation, err := NewDurableRuntimeConversation(base, port)
			if err != nil {
				t.Fatal(err)
			}
			_, err = conversation.BeginResumeAttempt(context.Background(), tc.basis)
			if err == nil {
				t.Fatal("unsafe resume basis was accepted")
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Fatalf("resume validation reflected sensitive identity: %v", err)
			}
			if port.beginCalls != 0 {
				t.Fatalf("unsafe resume basis reached durable port: %d calls", port.beginCalls)
			}
		})
	}
}

func TestDurableRuntimeConversationRejectsInvalidPortIdentityWithoutReflection(t *testing.T) {
	const sentinel = "sk-resumePortSentinel-DoNotReflect-7x9q"
	basis := agent.ResumeAttemptBasis{OperationID: "resume-operation-1", InterruptionID: "interruption-origin-1"}
	valid := agent.ResumeAttemptIdentity{
		AttemptID:      "attempt-1",
		ExecutionRunID: "execution-run-1",
		InterruptionID: basis.InterruptionID,
		OriginRunID:    "origin-run-1",
		AttemptNumber:  1,
		Status:         agent.ResumeAttemptStatusRunning,
		Validation: agent.ResumeValidationReceipt{
			OperationID:    basis.OperationID,
			InterruptionID: basis.InterruptionID,
			OriginRunID:    "origin-run-1",
		},
	}
	cases := []struct {
		name    string
		attempt agent.ResumeAttemptIdentity
		portErr error
	}{
		{name: "sensitive attempt identity", attempt: func() agent.ResumeAttemptIdentity {
			attempt := valid
			attempt.AttemptID = sentinel
			return attempt
		}()},
		{name: "origin reused as execution", attempt: func() agent.ResumeAttemptIdentity {
			attempt := valid
			attempt.ExecutionRunID = attempt.OriginRunID
			return attempt
		}()},
		{name: "mismatched interruption", attempt: func() agent.ResumeAttemptIdentity {
			attempt := valid
			attempt.InterruptionID = "interruption-other"
			return attempt
		}()},
		{name: "raw port error", attempt: valid, portErr: errors.New("authority failed: " + sentinel)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := &resumeLifecyclePortProbe{attempt: tc.attempt, beginErr: tc.portErr}
			base := &resumeOrderingConversation{pending: session.Interruption{ID: basis.InterruptionID, Status: session.InterruptionPending}}
			conversation, err := NewDurableRuntimeConversation(base, port)
			if err != nil {
				t.Fatal(err)
			}
			_, err = conversation.BeginResumeAttempt(context.Background(), basis)
			if err == nil {
				t.Fatal("invalid durable port result was accepted")
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Fatalf("durable port value was reflected: %v", err)
			}
			if port.beginCalls != 1 {
				t.Fatalf("durable port calls = %d, want 1", port.beginCalls)
			}
		})
	}
}

func TestDurableRuntimeConversationRejectsInvalidFinishBeforePort(t *testing.T) {
	const sentinel = "sk-resumeFinishSentinel-DoNotReflect-7x9q"
	attempt := agent.ResumeAttemptIdentity{
		AttemptID:      "attempt-1",
		ExecutionRunID: "execution-run-1",
		InterruptionID: "interruption-origin-1",
		OriginRunID:    "origin-run-1",
		AttemptNumber:  1,
		Status:         agent.ResumeAttemptStatusRunning,
		Validation: agent.ResumeValidationReceipt{
			OperationID:    "resume-operation-1",
			InterruptionID: "interruption-origin-1",
			OriginRunID:    "origin-run-1",
		},
	}
	cases := []struct {
		name  string
		input agent.ResumeAttemptFinish
	}{
		{
			name:  "unknown outcome",
			input: agent.ResumeAttemptFinish{Attempt: attempt, Outcome: agent.ResumeAttemptFinishOutcome("unexpected")},
		},
		{
			name: "sensitive attempt identity",
			input: func() agent.ResumeAttemptFinish {
				unsafe := attempt
				unsafe.AttemptID = sentinel
				return agent.ResumeAttemptFinish{Attempt: unsafe, Outcome: agent.ResumeAttemptOutcomeRunnerFailed}
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := &resumeLifecyclePortProbe{}
			base := &resumeOrderingConversation{pending: session.Interruption{ID: attempt.InterruptionID, Status: session.InterruptionPending}}
			conversation, err := NewDurableRuntimeConversation(base, port)
			if err != nil {
				t.Fatal(err)
			}
			_, err = conversation.FinishResumeAttempt(context.Background(), tc.input)
			if err == nil {
				t.Fatal("invalid finish input was accepted")
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Fatalf("finish validation reflected sensitive identity: %v", err)
			}
			if port.finishCalls != 0 {
				t.Fatalf("invalid finish reached durable port: %d calls", port.finishCalls)
			}
		})
	}
}

func TestDurableRuntimeConversationRejectsInvalidFinishReceiptWithoutReflection(t *testing.T) {
	const sentinel = "sk-resumeFinishPortSentinel-DoNotReflect-7x9q"
	attempt := agent.ResumeAttemptIdentity{
		AttemptID:      "attempt-1",
		ExecutionRunID: "execution-run-1",
		InterruptionID: "interruption-origin-1",
		OriginRunID:    "origin-run-1",
		AttemptNumber:  1,
		Status:         agent.ResumeAttemptStatusRunning,
		Validation: agent.ResumeValidationReceipt{
			OperationID:    "resume-operation-1",
			InterruptionID: "interruption-origin-1",
			OriginRunID:    "origin-run-1",
		},
	}
	input := agent.ResumeAttemptFinish{Attempt: attempt, Outcome: agent.ResumeAttemptOutcomeSucceeded}
	validReceipt := agent.ResumeAttemptFinishReceipt{Attempt: attempt, Outcome: input.Outcome, InterruptionResolved: true}
	validReceipt.Attempt.Status = agent.ResumeAttemptStatusSucceeded
	cases := []struct {
		name      string
		receipt   agent.ResumeAttemptFinishReceipt
		finishErr error
	}{
		{name: "mismatched attempt", receipt: func() agent.ResumeAttemptFinishReceipt {
			receipt := validReceipt
			receipt.Attempt.AttemptID = "attempt-other"
			return receipt
		}()},
		{name: "success remains pending", receipt: func() agent.ResumeAttemptFinishReceipt {
			receipt := validReceipt
			receipt.InterruptionResolved = false
			return receipt
		}()},
		{name: "wrong terminal status", receipt: func() agent.ResumeAttemptFinishReceipt {
			receipt := validReceipt
			receipt.Attempt.Status = agent.ResumeAttemptStatusFailed
			return receipt
		}()},
		{name: "raw port error", receipt: validReceipt, finishErr: errors.New("authority finish failed: " + sentinel)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := &resumeLifecyclePortProbe{receipt: tc.receipt, finishErr: tc.finishErr}
			base := &resumeOrderingConversation{pending: session.Interruption{ID: attempt.InterruptionID, Status: session.InterruptionPending}}
			conversation, err := NewDurableRuntimeConversation(base, port)
			if err != nil {
				t.Fatal(err)
			}
			_, err = conversation.FinishResumeAttempt(context.Background(), input)
			if err == nil {
				t.Fatal("invalid finish receipt was accepted")
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Fatalf("finish port value was reflected: %v", err)
			}
			if port.finishCalls != 1 {
				t.Fatalf("finish port calls = %d, want 1", port.finishCalls)
			}
		})
	}
}

func TestDurableRuntimeConversationAcceptsAtomicLifecycleFinishFailureReceipt(t *testing.T) {
	attempt := agent.ResumeAttemptIdentity{
		AttemptID:      "attempt-1",
		ExecutionRunID: "execution-run-1",
		InterruptionID: "interruption-origin-1",
		OriginRunID:    "origin-run-1",
		AttemptNumber:  1,
		Status:         agent.ResumeAttemptStatusRunning,
		Validation: agent.ResumeValidationReceipt{
			OperationID:    "resume-operation-1",
			InterruptionID: "interruption-origin-1",
			OriginRunID:    "origin-run-1",
		},
	}
	input := agent.ResumeAttemptFinish{Attempt: attempt, Outcome: agent.ResumeAttemptOutcomeSucceeded}
	want := agent.ResumeAttemptFinishReceipt{
		Attempt: attempt,
		Outcome: agent.ResumeAttemptFinishOutcome("lifecycle_finish_failed"),
	}
	want.Attempt.Status = agent.ResumeAttemptStatusFailed
	port := &resumeLifecyclePortProbe{receipt: want}
	base := &resumeOrderingConversation{pending: session.Interruption{ID: attempt.InterruptionID, Status: session.InterruptionPending}}
	conversation, err := NewDurableRuntimeConversation(base, port)
	if err != nil {
		t.Fatal(err)
	}

	got, err := conversation.FinishResumeAttempt(context.Background(), input)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("atomic lifecycle failure receipt = %#v, %v", got, err)
	}
}

func TestDurableResumeCancellationFinishesOnce(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		prepareSucceeds: true,
	}
	runnerAgent := &resumeTestAgent{}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})
	runContext, cancel := context.WithCancel(context.Background())
	cancel()

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		runContext,
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(agent.Event) {},
	)

	if conversation.finishCalls != 1 {
		t.Fatalf("FinishResumeAttempt calls = %d, want 1", conversation.finishCalls)
	}
	if conversation.finishInput.Outcome != agent.ResumeAttemptOutcomeCancelled {
		t.Fatalf("finish outcome = %q, want %q", conversation.finishInput.Outcome, agent.ResumeAttemptOutcomeCancelled)
	}
	if conversation.finishContextCancelled {
		t.Fatal("canceled run context was propagated to durable finalization")
	}
}

func TestDurableResumeBeginFailureStopsPrepareAndModelWithBoundedError(t *testing.T) {
	const sentinel = "sk-resumeBeginFailureSentinel-DoNotReflect-7x9q"
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		beginErr: errors.New("authority unavailable: " + sentinel),
	}
	runnerAgent := &resumeTestAgent{}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})
	var publicErrors []string

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(event agent.Event) {
			if event.Type == "error" {
				data, _ := event.Data.(map[string]string)
				publicErrors = append(publicErrors, data["message"])
			}
		},
	)

	if conversation.beginCalls != 1 || conversation.prepareCalls != 0 || runnerAgent.runCalls != 0 {
		t.Fatalf("begin failure crossed execution boundary: begin=%d prepare=%d model=%d", conversation.beginCalls, conversation.prepareCalls, runnerAgent.runCalls)
	}
	if conversation.finishCalls != 0 {
		t.Fatalf("unallocated attempt was finished: %d calls", conversation.finishCalls)
	}
	if len(publicErrors) != 1 || strings.Contains(publicErrors[0], sentinel) {
		t.Fatalf("begin failure was not bounded: %#v", publicErrors)
	}
}

func TestLegacyConversationResumeFallbackRemainsCompatible(t *testing.T) {
	conversation := &legacyResumeConversation{pending: session.Interruption{ID: "legacy-interruption-1", Status: session.InterruptionPending}}
	if _, durable := any(conversation).(agent.ResumeAttemptLifecycle); durable {
		t.Fatal("legacy conversation unexpectedly implements durable lifecycle")
	}
	runnerAgent := &resumeTestAgent{event: &adk.AgentEvent{
		AgentName: "legacy-test-agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Role:    schema.Assistant,
			Message: schema.AssistantMessage("legacy output", nil),
		}},
	}}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "legacy-resume-request"},
		func(agent.Event) {},
	)

	if conversation.prepareCalls != 1 || runnerAgent.runCalls != 1 || conversation.appended != "legacy output" {
		t.Fatalf("legacy execution changed: prepare=%d model=%d output=%q", conversation.prepareCalls, runnerAgent.runCalls, conversation.appended)
	}
	if conversation.resolveCalls != 1 || conversation.resolvedID != "legacy-interruption-1" {
		t.Fatalf("legacy resolution changed: calls=%d id=%q", conversation.resolveCalls, conversation.resolvedID)
	}
}

func TestDurableRuntimePortFailedAttemptLinksNextAttemptAndIsIdempotentAcrossRestart(t *testing.T) {
	authority := newDurableResumeAuthorityFake("interruption-origin-1", "origin-run-1")
	base := &legacyResumeConversation{pending: session.Interruption{ID: "interruption-origin-1", Status: session.InterruptionPending}}
	firstAdapter, err := NewDurableRuntimeConversation(base, authority)
	if err != nil {
		t.Fatal(err)
	}
	basis1 := agent.ResumeAttemptBasis{OperationID: "resume-operation-1", InterruptionID: "interruption-origin-1"}
	attempt1, err := firstAdapter.BeginResumeAttempt(context.Background(), basis1)
	if err != nil {
		t.Fatal(err)
	}
	retried1, err := firstAdapter.BeginResumeAttempt(context.Background(), basis1)
	if err != nil || !reflect.DeepEqual(retried1, attempt1) || authority.beginMutations != 1 {
		t.Fatalf("idempotent begin = %#v, %v mutations=%d", retried1, err, authority.beginMutations)
	}
	failedInput := agent.ResumeAttemptFinish{Attempt: attempt1, Outcome: agent.ResumeAttemptOutcomeRunnerFailed}
	failedReceipt, err := firstAdapter.FinishResumeAttempt(context.Background(), failedInput)
	if err != nil || failedReceipt.Attempt.Status != agent.ResumeAttemptStatusFailed || failedReceipt.InterruptionResolved {
		t.Fatalf("failed finish = %#v, %v", failedReceipt, err)
	}
	retriedFailure, err := firstAdapter.FinishResumeAttempt(context.Background(), failedInput)
	if err != nil || !reflect.DeepEqual(retriedFailure, failedReceipt) || authority.finishMutations != 1 {
		t.Fatalf("idempotent failed finish = %#v, %v mutations=%d", retriedFailure, err, authority.finishMutations)
	}
	conflict := failedInput
	conflict.Outcome = agent.ResumeAttemptOutcomeSucceeded
	if _, err := firstAdapter.FinishResumeAttempt(context.Background(), conflict); err == nil {
		t.Fatal("conflicting repeated finish was accepted")
	}

	restartedAdapter, err := NewDurableRuntimeConversation(base, authority)
	if err != nil {
		t.Fatal(err)
	}
	basis2 := agent.ResumeAttemptBasis{OperationID: "resume-operation-2", InterruptionID: "interruption-origin-1"}
	attempt2, err := restartedAdapter.BeginResumeAttempt(context.Background(), basis2)
	if err != nil {
		t.Fatal(err)
	}
	if attempt2.AttemptID == attempt1.AttemptID || attempt2.ExecutionRunID == attempt1.ExecutionRunID || attempt2.ExecutionRunID == attempt2.OriginRunID {
		t.Fatalf("next attempt reused durable identity: first=%#v next=%#v", attempt1, attempt2)
	}
	if attempt2.ParentAttemptID != attempt1.AttemptID || attempt2.AttemptNumber != 2 {
		t.Fatalf("next attempt lineage = parent %q number %d", attempt2.ParentAttemptID, attempt2.AttemptNumber)
	}
	successInput := agent.ResumeAttemptFinish{Attempt: attempt2, Outcome: agent.ResumeAttemptOutcomeSucceeded}
	successReceipt, err := restartedAdapter.FinishResumeAttempt(context.Background(), successInput)
	if err != nil || !successReceipt.InterruptionResolved || successReceipt.Attempt.Status != agent.ResumeAttemptStatusSucceeded {
		t.Fatalf("success finish = %#v, %v", successReceipt, err)
	}
	retriedSuccess, err := restartedAdapter.FinishResumeAttempt(context.Background(), successInput)
	if err != nil || !reflect.DeepEqual(retriedSuccess, successReceipt) || authority.resolveCount != 1 || authority.finishMutations != 2 {
		t.Fatalf("idempotent success = %#v, %v resolves=%d mutations=%d", retriedSuccess, err, authority.resolveCount, authority.finishMutations)
	}
	if authority.pending {
		t.Fatal("successful durable finish left interruption pending")
	}
}

func TestDurableResumeSuccessFinishErrorCannotClaimSuccess(t *testing.T) {
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		prepareSucceeds: true,
		finishErr:       errors.New("injected finish failure"),
	}
	runnerAgent := &resumeTestAgent{event: &adk.AgentEvent{
		AgentName: "resume-test-agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Role:    schema.Assistant,
			Message: schema.AssistantMessage("persisted before finish failure", nil),
		}},
	}}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})
	var doneEvents int
	var successStates int

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-request-1"},
		func(event agent.Event) {
			data, _ := event.Data.(map[string]string)
			if event.Type == "done" {
				doneEvents++
			}
			if event.Type == "run_state" && data["phase"] == "finished" && data["status"] == "success" {
				successStates++
			}
		},
	)

	if conversation.finishCalls != 1 || conversation.finishInput.Outcome != agent.ResumeAttemptOutcomeSucceeded {
		t.Fatalf("finish failure calls/outcome = %d/%q", conversation.finishCalls, conversation.finishInput.Outcome)
	}
	if conversation.resolveCalls != 0 || doneEvents != 0 || successStates != 0 {
		t.Fatalf("finish failure claimed success: resolve=%d done=%d success=%d", conversation.resolveCalls, doneEvents, successStates)
	}
}

func TestResumeLifecycleDTOFieldAllowlistsExcludeContentPathsAndSecrets(t *testing.T) {
	cases := []struct {
		name   string
		typeOf reflect.Type
		want   []string
	}{
		{name: "basis", typeOf: reflect.TypeOf(agent.ResumeAttemptBasis{}), want: []string{"interruption_id", "operation_id"}},
		{name: "validation receipt", typeOf: reflect.TypeOf(agent.ResumeValidationReceipt{}), want: []string{"interruption_id", "operation_id", "origin_run_id"}},
		{name: "attempt", typeOf: reflect.TypeOf(agent.ResumeAttemptIdentity{}), want: []string{"attempt_id", "attempt_number", "execution_run_id", "interruption_id", "origin_run_id", "parent_attempt_id", "status", "validation"}},
		{name: "finish", typeOf: reflect.TypeOf(agent.ResumeAttemptFinish{}), want: []string{"attempt", "outcome"}},
		{name: "finish receipt", typeOf: reflect.TypeOf(agent.ResumeAttemptFinishReceipt{}), want: []string{"attempt", "interruption_resolved", "outcome"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for index := 0; index < tc.typeOf.NumField(); index++ {
				tag := strings.Split(tc.typeOf.Field(index).Tag.Get("json"), ",")[0]
				got = append(got, tag)
			}
			sort.Strings(got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("public fields = %#v, want %#v", got, tc.want)
			}
			joined := strings.ToLower(strings.Join(got, ""))
			for _, forbidden := range []string{"path", "workspace", "bookroot", "request", "prose", "tool", "frame", "stderr", "credential", "provider", "profile", "secret"} {
				if strings.Contains(joined, forbidden) {
					t.Fatalf("public fields include forbidden class %q: %#v", forbidden, got)
				}
			}
		})
	}
}

type resumeOrderingConversation struct {
	pending                       session.Interruption
	beginBasis                    agent.ResumeAttemptBasis
	beginErr                      error
	beginCalls                    int
	finishCalls                   int
	finishInput                   agent.ResumeAttemptFinish
	finishErr                     error
	resolveCalls                  int
	prepareCalls                  int
	prepareObservedBegin          bool
	prepareSucceeds               bool
	preparePanics                 bool
	compactionCalls               int
	compactionErr                 error
	appendCalls                   int
	appended                      string
	appendErr                     error
	finishObservedPersist         bool
	finishLeavesPending           bool
	finishContextCancelled        bool
	finishPanicRemaining          int
	finishPanicValue              string
	finishOutcomes                []agent.ResumeAttemptFinishOutcome
	finishContextCancelledHistory []bool
	interruptionReason            string
}

func (c *resumeOrderingConversation) BeginResumeAttempt(_ context.Context, basis agent.ResumeAttemptBasis) (agent.ResumeAttemptIdentity, error) {
	c.beginCalls++
	c.beginBasis = basis
	if c.beginErr != nil {
		return agent.ResumeAttemptIdentity{}, c.beginErr
	}
	return agent.ResumeAttemptIdentity{
		AttemptID:      "attempt-1",
		ExecutionRunID: "execution-run-1",
		InterruptionID: basis.InterruptionID,
		OriginRunID:    "origin-run-1",
		AttemptNumber:  1,
		Status:         agent.ResumeAttemptStatusRunning,
	}, nil
}

func (c *resumeOrderingConversation) FinishResumeAttempt(ctx context.Context, input agent.ResumeAttemptFinish) (agent.ResumeAttemptFinishReceipt, error) {
	c.finishCalls++
	c.finishInput = input
	c.finishOutcomes = append(c.finishOutcomes, input.Outcome)
	c.finishObservedPersist = c.appendCalls == 1
	c.finishContextCancelled = ctx.Err() != nil
	c.finishContextCancelledHistory = append(c.finishContextCancelledHistory, c.finishContextCancelled)
	if c.finishPanicRemaining > 0 {
		c.finishPanicRemaining--
		panic(c.finishPanicValue)
	}
	if c.finishErr != nil {
		return agent.ResumeAttemptFinishReceipt{}, c.finishErr
	}
	finishedAttempt := input.Attempt
	if input.Outcome == agent.ResumeAttemptOutcomeSucceeded {
		finishedAttempt.Status = agent.ResumeAttemptStatusSucceeded
	} else {
		finishedAttempt.Status = agent.ResumeAttemptStatusFailed
	}
	return agent.ResumeAttemptFinishReceipt{
		Attempt:              finishedAttempt,
		Outcome:              input.Outcome,
		InterruptionResolved: input.Outcome == agent.ResumeAttemptOutcomeSucceeded && !c.finishLeavesPending,
	}, nil
}

func (c *resumeOrderingConversation) PrepareMessages(string, string) ([]*schema.Message, error) {
	c.prepareCalls++
	c.prepareObservedBegin = c.beginCalls == 1
	if c.preparePanics {
		panic("injected prepare panic")
	}
	if c.prepareSucceeds {
		return []*schema.Message{schema.UserMessage("resume")}, nil
	}
	return nil, errors.New("stop after observing resume ordering")
}

func (c *resumeOrderingConversation) CompactContextIfNeeded(_ context.Context, input agent.ContextCompactionInput) ([]*schema.Message, agent.ContextCompactionResult, error) {
	c.compactionCalls++
	return input.Messages, agent.ContextCompactionResult{}, c.compactionErr
}

func (c *resumeOrderingConversation) AppendAssistant(content string) error {
	c.appendCalls++
	c.appended = content
	return c.appendErr
}

func (c *resumeOrderingConversation) MarkInterrupted(_, _, reason string) error {
	c.interruptionReason = reason
	return nil
}

func (c *resumeOrderingConversation) PendingInterruption() *session.Interruption {
	pending := c.pending
	return &pending
}

func (c *resumeOrderingConversation) ResolveInterruption(string) error {
	c.resolveCalls++
	return nil
}

type resumeTestAgent struct {
	event    *adk.AgentEvent
	events   []*adk.AgentEvent
	runCalls int
}

type resumeLifecyclePortProbe struct {
	attempt     agent.ResumeAttemptIdentity
	receipt     agent.ResumeAttemptFinishReceipt
	beginErr    error
	finishErr   error
	beginCalls  int
	finishCalls int
}

type legacyResumeConversation struct {
	pending      session.Interruption
	prepareCalls int
	appended     string
	resolveCalls int
	resolvedID   string
}

func (c *legacyResumeConversation) PrepareMessages(string, string) ([]*schema.Message, error) {
	c.prepareCalls++
	return []*schema.Message{schema.UserMessage("legacy resume")}, nil
}

func (c *legacyResumeConversation) AppendAssistant(content string) error {
	c.appended = content
	return nil
}

func (c *legacyResumeConversation) MarkInterrupted(string, string, string) error { return nil }

func (c *legacyResumeConversation) PendingInterruption() *session.Interruption {
	pending := c.pending
	return &pending
}

func (c *legacyResumeConversation) ResolveInterruption(id string) error {
	c.resolveCalls++
	c.resolvedID = id
	return nil
}

type durableResumeFinishRecord struct {
	input   agent.ResumeAttemptFinish
	receipt agent.ResumeAttemptFinishReceipt
}

type durableResumeBeginRecord struct {
	basis   agent.ResumeAttemptBasis
	attempt agent.ResumeAttemptIdentity
}

type durableResumeAuthorityFake struct {
	mu              sync.Mutex
	interruptionID  string
	originRunID     string
	pending         bool
	attempts        []agent.ResumeAttemptIdentity
	begins          map[string]durableResumeBeginRecord
	finishes        map[string]durableResumeFinishRecord
	beginMutations  int
	finishMutations int
	resolveCount    int
}

func newDurableResumeAuthorityFake(interruptionID, originRunID string) *durableResumeAuthorityFake {
	return &durableResumeAuthorityFake{
		interruptionID: interruptionID,
		originRunID:    originRunID,
		pending:        true,
		begins:         make(map[string]durableResumeBeginRecord),
		finishes:       make(map[string]durableResumeFinishRecord),
	}
}

func (p *durableResumeAuthorityFake) BeginResumeAttempt(_ context.Context, basis agent.ResumeAttemptBasis) (agent.ResumeAttemptIdentity, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.begins[basis.OperationID]; ok {
		if existing.basis != basis {
			return agent.ResumeAttemptIdentity{}, errors.New("begin operation conflict")
		}
		return existing.attempt, nil
	}
	if !p.pending || basis.InterruptionID != p.interruptionID {
		return agent.ResumeAttemptIdentity{}, errors.New("interruption is not pending")
	}
	number := len(p.attempts) + 1
	parent := ""
	if len(p.attempts) > 0 {
		parent = p.attempts[len(p.attempts)-1].AttemptID
	}
	attempt := agent.ResumeAttemptIdentity{
		AttemptID:       fmt.Sprintf("attempt-%d", number),
		ExecutionRunID:  fmt.Sprintf("execution-run-%d", number),
		InterruptionID:  p.interruptionID,
		OriginRunID:     p.originRunID,
		ParentAttemptID: parent,
		AttemptNumber:   number,
		Status:          agent.ResumeAttemptStatusRunning,
		Validation: agent.ResumeValidationReceipt{
			OperationID:    basis.OperationID,
			InterruptionID: p.interruptionID,
			OriginRunID:    p.originRunID,
		},
	}
	p.attempts = append(p.attempts, attempt)
	p.begins[basis.OperationID] = durableResumeBeginRecord{basis: basis, attempt: attempt}
	p.beginMutations++
	return attempt, nil
}

func (p *durableResumeAuthorityFake) FinishResumeAttempt(_ context.Context, input agent.ResumeAttemptFinish) (agent.ResumeAttemptFinishReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.finishes[input.Attempt.AttemptID]; ok {
		if existing.input != input {
			return agent.ResumeAttemptFinishReceipt{}, errors.New("finish operation conflict")
		}
		return existing.receipt, nil
	}
	finished := input.Attempt
	resolved := input.Outcome == agent.ResumeAttemptOutcomeSucceeded
	if resolved {
		finished.Status = agent.ResumeAttemptStatusSucceeded
		p.pending = false
		p.resolveCount++
	} else {
		finished.Status = agent.ResumeAttemptStatusFailed
	}
	receipt := agent.ResumeAttemptFinishReceipt{Attempt: finished, Outcome: input.Outcome, InterruptionResolved: resolved}
	p.finishes[input.Attempt.AttemptID] = durableResumeFinishRecord{input: input, receipt: receipt}
	p.finishMutations++
	return receipt, nil
}

func (p *resumeLifecyclePortProbe) BeginResumeAttempt(_ context.Context, _ agent.ResumeAttemptBasis) (agent.ResumeAttemptIdentity, error) {
	p.beginCalls++
	return p.attempt, p.beginErr
}

func (p *resumeLifecyclePortProbe) FinishResumeAttempt(_ context.Context, _ agent.ResumeAttemptFinish) (agent.ResumeAttemptFinishReceipt, error) {
	p.finishCalls++
	return p.receipt, p.finishErr
}

func (a *resumeTestAgent) Name(context.Context) string { return "resume-test-agent" }

func (a *resumeTestAgent) Description(context.Context) string { return "durable resume test" }

func (a *resumeTestAgent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	a.runCalls++
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()
		for _, event := range a.events {
			generator.Send(event)
		}
		if a.event != nil {
			generator.Send(a.event)
		}
	}()
	return iterator
}

func (a *resumeTestAgent) Resume(context.Context, *adk.ResumeInfo, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	generator.Close()
	return iterator
}

func TestCheckpointVersionRestartIdentityAndPermissions(t *testing.T) {
	root := t.TempDir()
	ref := checkpointTestRef("checkpoint-main", "session-a", "task-a", "run-a", "attempt-1")
	store := newCheckpointAdapter(t, root)

	input := []byte(`{"cursor":1}`)
	first := store.Set(context.Background(), ref, input)
	if first.Status != CheckpointStatusOK || first.Version != 1 || first.Resumable {
		t.Fatalf("first Set result = %#v", first)
	}
	input[0] = 'X'
	second := store.Set(context.Background(), ref, []byte(`{"cursor":2}`))
	if second.Status != CheckpointStatusOK || second.Version != 2 {
		t.Fatalf("second Set result = %#v", second)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted := newCheckpointAdapter(t, root)
	defer restarted.Close()
	got := restarted.Get(context.Background(), ref)
	if got.Status != CheckpointStatusOK || got.Version != 2 || string(got.Value) != `{"cursor":2}` || !got.Resumable {
		t.Fatalf("restart Get result = %#v", got)
	}
	got.Value[0] = 'X'
	again := restarted.Get(context.Background(), ref)
	if string(again.Value) != `{"cursor":2}` {
		t.Fatalf("Get did not return a copy: %q", again.Value)
	}

	other := ref
	other.SessionID = "session-b"
	if result := restarted.Get(context.Background(), other); result.Status != CheckpointStatusMissing {
		t.Fatalf("different lineage crossed checkpoint identity: %#v", result)
	}

	target := checkpointTargetPathForTest(root, ref)
	if filepath.Base(target) != fmt.Sprintf("%x.json", sha256.Sum256([]byte(checkpointLogicalKeyForTest(ref)))) {
		t.Fatalf("target is not the logical key hash: %q", filepath.Base(target))
	}
	if strings.Contains(target, ref.CheckpointID) || strings.Contains(target, ref.SessionID) {
		t.Fatalf("logical identity leaked into target path: %q", target)
	}
	fileInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("checkpoint mode = %o, want 600", fileInfo.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("checkpoint directory mode = %o, want 700", dirInfo.Mode().Perm())
	}

	orphan := filepath.Join(filepath.Dir(target), ".orphan-checkpoint.tmp")
	if err := os.WriteFile(orphan, []byte(`{"value":"not-active"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if result := restarted.Get(context.Background(), ref); result.Status != CheckpointStatusOK || string(result.Value) != `{"cursor":2}` {
		t.Fatalf("orphan temp became active: %#v", result)
	}
}

func TestCheckpointAtomicOldOrNewFailureMatrix(t *testing.T) {
	for _, phase := range []checkpointWritePhase{
		checkpointPhaseTempCreate,
		checkpointPhaseTempWrite,
		checkpointPhaseTempSync,
		checkpointPhaseReplace,
	} {
		t.Run(string(phase), func(t *testing.T) {
			root := t.TempDir()
			ref := checkpointTestRef("checkpoint-atomic", "session-a", "task-a", "run-a", "attempt-1")
			seed := newCheckpointAdapter(t, root)
			if result := seed.Set(context.Background(), ref, []byte("old")); result.Status != CheckpointStatusOK {
				t.Fatal(result)
			}
			_ = seed.Close()

			faulted, err := newCheckpointAdapterWithBarrier(root, func(got checkpointWritePhase) error {
				if got == phase {
					return errors.New("injected checkpoint phase failure")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			result := faulted.Set(context.Background(), ref, []byte("new"))
			_ = faulted.Close()
			if result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeWriteFailed {
				t.Fatalf("fault result = %#v", result)
			}
			fresh := newCheckpointAdapter(t, root)
			defer fresh.Close()
			read := fresh.Get(context.Background(), ref)
			if read.Status != CheckpointStatusOK || string(read.Value) != "old" || read.Version != 1 {
				t.Fatalf("phase %q did not preserve old target: %#v", phase, read)
			}
		})
	}

	t.Run("directory sync after replace is durability uncertain", func(t *testing.T) {
		root := t.TempDir()
		ref := checkpointTestRef("checkpoint-uncertain", "session-a", "task-a", "run-a", "attempt-1")
		seed := newCheckpointAdapter(t, root)
		if result := seed.Set(context.Background(), ref, []byte("old")); result.Status != CheckpointStatusOK {
			t.Fatal(result)
		}
		_ = seed.Close()
		faulted, err := newCheckpointAdapterWithBarrier(root, func(got checkpointWritePhase) error {
			if got == checkpointPhaseDirectorySync {
				return errors.New("injected directory sync failure")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		result := faulted.Set(context.Background(), ref, []byte("new"))
		_ = faulted.Close()
		if result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeDurabilityUncertain || result.Resumable {
			t.Fatalf("post-replace sync result = %#v", result)
		}
		fresh := newCheckpointAdapter(t, root)
		defer fresh.Close()
		if read := fresh.Get(context.Background(), ref); read.Status != CheckpointStatusOK || string(read.Value) != "new" || read.Version != 2 {
			t.Fatalf("replace did not atomically expose new target: %#v", read)
		}
	})

	t.Run("temp identity swap before replace preserves old target", func(t *testing.T) {
		root := t.TempDir()
		ref := checkpointTestRef("checkpoint-temp-swap", "session-a", "task-a", "run-a", "attempt-1")
		seed := newCheckpointAdapter(t, root)
		if result := seed.Set(context.Background(), ref, []byte("old")); result.Status != CheckpointStatusOK {
			t.Fatal(result)
		}
		_ = seed.Close()
		outside := filepath.Join(t.TempDir(), "outside.json")
		if err := os.WriteFile(outside, []byte("outside-safe"), 0o600); err != nil {
			t.Fatal(err)
		}
		faulted, err := newCheckpointAdapterWithBarrier(root, func(phase checkpointWritePhase) error {
			if phase != checkpointPhaseReplace {
				return nil
			}
			entries, err := os.ReadDir(filepath.Join(root, "checkpoints"))
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".tmp") {
					tempPath := filepath.Join(root, "checkpoints", entry.Name())
					if err := os.Remove(tempPath); err != nil {
						return err
					}
					return os.Symlink(outside, tempPath)
				}
			}
			return errors.New("checkpoint temp was not found")
		})
		if err != nil {
			t.Fatal(err)
		}
		result := faulted.Set(context.Background(), ref, []byte("new"))
		_ = faulted.Close()
		if result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeUnsafeFile {
			t.Fatalf("temp identity swap result = %#v", result)
		}
		fresh := newCheckpointAdapter(t, root)
		defer fresh.Close()
		if read := fresh.Get(context.Background(), ref); read.Status != CheckpointStatusOK || string(read.Value) != "old" || read.Version != 1 {
			t.Fatalf("temp identity swap did not preserve old target: %#v", read)
		}
		if got, _ := os.ReadFile(outside); string(got) != "outside-safe" {
			t.Fatalf("temp identity swap modified outside target: %q", got)
		}
	})
}

func TestCheckpointCorruptionCodesAreStableAndFilesRemainUntouched(t *testing.T) {
	cases := []struct {
		name string
		code string
		edit func([]byte) []byte
	}{
		{name: "malformed", code: CheckpointCodeMalformed, edit: func([]byte) []byte { return []byte("{") }},
		{name: "future schema", code: CheckpointCodeSchemaUnsupported, edit: checkpointEditField("schemaVersion", "2")},
		{name: "key mismatch", code: CheckpointCodeKeyMismatch, edit: checkpointEditField("key", "different-logical-key")},
		{name: "key hash mismatch", code: CheckpointCodeKeyHashMismatch, edit: checkpointEditField("keyHash", strings.Repeat("0", 64))},
		{name: "zero version", code: CheckpointCodeVersionInvalid, edit: checkpointEditField("version", float64(0))},
		{name: "bad timestamp", code: CheckpointCodeTimestampInvalid, edit: checkpointEditField("updatedAt", "yesterday")},
		{name: "value hash mismatch", code: CheckpointCodeValueHashMismatch, edit: checkpointEditField("valueHash", strings.Repeat("0", 64))},
		{name: "checksum mismatch", code: CheckpointCodeChecksumMismatch, edit: checkpointEditField("checksum", strings.Repeat("0", 64))},
		{name: "unknown field", code: CheckpointCodeMalformed, edit: checkpointEditField("surprise", true)},
		{name: "duplicate field", code: CheckpointCodeMalformed, edit: func(data []byte) []byte {
			return bytes.Replace(data, []byte(`{"schemaVersion":"1"`), []byte(`{"schemaVersion":"1","schemaVersion":"1"`), 1)
		}},
		{name: "trailing garbage", code: CheckpointCodeMalformed, edit: func(data []byte) []byte { return append(data, []byte("garbage")...) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			ref := checkpointTestRef("checkpoint-corrupt", "session-a", "task-a", "run-a", "attempt-1")
			store := newCheckpointAdapter(t, root)
			defer store.Close()
			if result := store.Set(context.Background(), ref, []byte("safe-value")); result.Status != CheckpointStatusOK {
				t.Fatal(result)
			}
			target := checkpointTargetPathForTest(root, ref)
			original, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			corrupt := tc.edit(original)
			if err := os.WriteFile(target, corrupt, 0o600); err != nil {
				t.Fatal(err)
			}
			fixedTime := time.Unix(1_700_000_000, 123_000_000)
			if err := os.Chtimes(target, fixedTime, fixedTime); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(target)
			beforeInfo, _ := os.Stat(target)
			result := store.Get(context.Background(), ref)
			if result.Status != CheckpointStatusDegraded || result.Code != tc.code || result.Resumable || len(result.Value) != 0 {
				t.Fatalf("degraded result = %#v, want code %q", result, tc.code)
			}
			after, _ := os.ReadFile(target)
			afterInfo, _ := os.Stat(target)
			if !bytes.Equal(after, before) || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
				t.Fatalf("corrupt checkpoint was modified during fail-closed read")
			}
		})
	}

	t.Run("oversize", func(t *testing.T) {
		root := t.TempDir()
		ref := checkpointTestRef("checkpoint-oversize", "session-a", "task-a", "run-a", "attempt-1")
		store := newCheckpointAdapter(t, root)
		defer store.Close()
		target := checkpointTargetPathForTest(root, ref)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, bytes.Repeat([]byte{'x'}, checkpointMaxEnvelopeBytes+1), 0o600); err != nil {
			t.Fatal(err)
		}
		if result := store.Get(context.Background(), ref); result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeOversize {
			t.Fatalf("oversize result = %#v", result)
		}
	})
}

func TestCheckpointRejectsSensitiveMaterialAndBoundsWithoutLeakage(t *testing.T) {
	root := t.TempDir()
	store := newCheckpointAdapter(t, root)
	defer store.Close()
	ref := checkpointTestRef("checkpoint-secret", "session-a", "task-a", "run-a", "attempt-1")
	const sentinel = "sk-yanzhouCheckpointSentinel-DoNotPersist-7x9q"
	for _, value := range [][]byte{
		[]byte(sentinel),
		[]byte(`{"apiKey":"` + sentinel + `"}`),
		[]byte(`{"runtimeAuth":"` + sentinel + `"}`),
		[]byte("Authorization: Bearer " + sentinel),
		[]byte("Bearer " + sentinel),
		[]byte(`{"apiKey":"opaque-checkpoint-credential"}`),
		[]byte(`{"runtimeAuth":"opaque-checkpoint-auth"}`),
		[]byte(`{"Authorization":"Basic opaque-checkpoint-auth"}`),
	} {
		result := store.Set(context.Background(), ref, value)
		encoded, _ := json.Marshal(result)
		if result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeSensitiveMaterial || bytes.Contains(encoded, []byte(sentinel)) {
			t.Fatalf("sensitive Set result leaked or accepted: %s", encoded)
		}
	}
	if result := store.Set(context.Background(), ref, bytes.Repeat([]byte{'v'}, checkpointMaxValueBytes+1)); result.Code != CheckpointCodeOversize {
		t.Fatalf("oversized value result = %#v", result)
	}
	badRef := ref
	badRef.CheckpointID = strings.Repeat("x", checkpointMaxIdentityBytes+1)
	if result := store.Get(context.Background(), badRef); result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeInvalidRef {
		t.Fatalf("unbounded ref result = %#v", result)
	}
	secretRef := ref
	secretRef.CheckpointID = sentinel
	secretResult := store.Get(context.Background(), secretRef)
	secretJSON, _ := json.Marshal(secretResult)
	if secretResult.Status != CheckpointStatusDegraded || secretResult.Code != CheckpointCodeInvalidRef || bytes.Contains(secretJSON, []byte(sentinel)) {
		t.Fatalf("sensitive ref result leaked or was accepted: %s", secretJSON)
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(sentinel)) {
			t.Fatalf("secret sentinel persisted in runtime tree")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointSecretShapesNeverPersistOrReflect(t *testing.T) {
	shapes := checkpointSensitiveShapeCases()

	for index, shape := range shapes {
		t.Run("value/"+shape.name, func(t *testing.T) {
			root := t.TempDir()
			store := newCheckpointAdapter(t, root)
			defer store.Close()
			ref := checkpointTestRef(fmt.Sprintf("checkpoint-value-%d", index), "session-a", "task-a", "run-a", "attempt-1")
			result := store.Set(context.Background(), ref, []byte(shape.value))
			encoded, _ := json.Marshal(result)
			if result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeSensitiveMaterial {
				t.Fatalf("secret-shaped value result = %s", encoded)
			}
			if bytes.Contains(encoded, []byte(shape.sentinel)) {
				t.Fatalf("secret-shaped value reflected in public result: %s", encoded)
			}
			assertRuntimeTreeExcludes(t, root, shape.sentinel)
		})
	}

	refFields := []struct {
		name string
		set  func(*CheckpointRef, string)
	}{
		{name: "checkpointId", set: func(ref *CheckpointRef, value string) { ref.CheckpointID = value }},
		{name: "namespace", set: func(ref *CheckpointRef, value string) { ref.Namespace = value }},
		{name: "agentKind", set: func(ref *CheckpointRef, value string) { ref.AgentKind = value }},
		{name: "sessionId", set: func(ref *CheckpointRef, value string) { ref.SessionID = value }},
		{name: "taskId", set: func(ref *CheckpointRef, value string) { ref.TaskID = value }},
		{name: "runId", set: func(ref *CheckpointRef, value string) { ref.RunID = value }},
		{name: "attemptId", set: func(ref *CheckpointRef, value string) { ref.AttemptID = value }},
	}
	root := t.TempDir()
	store := newCheckpointAdapter(t, root)
	defer store.Close()
	for _, field := range refFields {
		for _, shape := range shapes {
			t.Run("ref/"+field.name+"/"+shape.name, func(t *testing.T) {
				ref := checkpointTestRef("checkpoint-ref", "session-a", "task-a", "run-a", "attempt-1")
				field.set(&ref, shape.value)
				for operationName, operation := range map[string]func() CheckpointResult{
					"put":    func() CheckpointResult { return store.Set(context.Background(), ref, []byte("safe")) },
					"read":   func() CheckpointResult { return store.Get(context.Background(), ref) },
					"remove": func() CheckpointResult { return store.Remove(context.Background(), ref) },
				} {
					result := operation()
					encoded, _ := json.Marshal(result)
					if result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeInvalidRef {
						t.Fatalf("%s secret ref result = %s", operationName, encoded)
					}
					if bytes.Contains(encoded, []byte(shape.sentinel)) {
						t.Fatalf("%s reflected secret ref: %s", operationName, encoded)
					}
				}
				assertRuntimeTreeExcludes(t, root, shape.sentinel)
			})
		}
	}

	t.Run("normal Chinese is accepted", func(t *testing.T) {
		root := t.TempDir()
		store := newCheckpointAdapter(t, root)
		defer store.Close()
		ref := checkpointTestRef("checkpoint-chinese", "session-chinese", "task-chinese", "run-chinese", "attempt-1")
		value := []byte("这是正常中文正文游标，不是凭证")
		if result := store.Set(context.Background(), ref, value); result.Status != CheckpointStatusOK {
			t.Fatalf("normal Chinese checkpoint was rejected: %#v", result)
		}
		if result := store.Get(context.Background(), ref); result.Status != CheckpointStatusOK || !bytes.Equal(result.Value, value) {
			t.Fatalf("normal Chinese checkpoint round-trip failed: %#v", result)
		}
	})
}

func TestCheckpointRejectsSymlinksAndRootReplacement(t *testing.T) {
	t.Run("symlink target", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside.json")
		if err := os.WriteFile(outside, []byte("outside-safe"), 0o600); err != nil {
			t.Fatal(err)
		}
		ref := checkpointTestRef("checkpoint-link", "session-a", "task-a", "run-a", "attempt-1")
		target := checkpointTargetPathForTest(root, ref)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		store := newCheckpointAdapter(t, root)
		defer store.Close()
		if result := store.Get(context.Background(), ref); result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeUnsafeFile {
			t.Fatalf("symlink Get result = %#v", result)
		}
		if result := store.Set(context.Background(), ref, []byte("new")); result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeUnsafeFile {
			t.Fatalf("symlink Set result = %#v", result)
		}
		if got, _ := os.ReadFile(outside); string(got) != "outside-safe" {
			t.Fatalf("symlink target modified: %q", got)
		}
	})

	t.Run("root replacement", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "runtime")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		store := newCheckpointAdapter(t, root)
		defer store.Close()
		moved := filepath.Join(parent, "runtime-moved")
		if err := os.Rename(root, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, root); err != nil {
			t.Fatal(err)
		}
		ref := checkpointTestRef("checkpoint-swap", "session-a", "task-a", "run-a", "attempt-1")
		if result := store.Set(context.Background(), ref, []byte("new")); result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeUnsafeRoot {
			t.Fatalf("root swap Set result = %#v", result)
		}
		entries, err := os.ReadDir(outside)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("root replacement escaped into outside tree: %v", entries)
		}
	})

	t.Run("root replacement during write", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "runtime")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		moved := filepath.Join(parent, "runtime-moved")
		store, err := newCheckpointAdapterWithBarrier(root, func(phase checkpointWritePhase) error {
			if phase != checkpointPhaseReplace {
				return nil
			}
			if err := os.Rename(root, moved); err != nil {
				return err
			}
			return os.Symlink(outside, root)
		})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		ref := checkpointTestRef("checkpoint-mid-swap", "session-a", "task-a", "run-a", "attempt-1")
		if result := store.Set(context.Background(), ref, []byte("new")); result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeUnsafeRoot {
			t.Fatalf("mid-write root swap Set result = %#v", result)
		}
		entries, err := os.ReadDir(outside)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("mid-write root replacement escaped into outside tree: %v", entries)
		}
	})
}

func TestCheckpointConcurrentSetIsGaplessAndCloseIsExplicit(t *testing.T) {
	root := t.TempDir()
	store := newCheckpointAdapter(t, root)
	ref := checkpointTestRef("checkpoint-concurrent", "session-a", "task-a", "run-a", "attempt-1")
	const writers = 24
	versions := make(chan uint64, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result := store.Set(context.Background(), ref, []byte(fmt.Sprintf("value-%d", index)))
			if result.Status != CheckpointStatusOK {
				t.Errorf("concurrent Set result = %#v", result)
				return
			}
			versions <- result.Version
		}(i)
	}
	wg.Wait()
	close(versions)
	seen := make(map[uint64]bool, writers)
	for version := range versions {
		seen[version] = true
	}
	for version := uint64(1); version <= writers; version++ {
		if !seen[version] {
			t.Fatalf("version gap at %d: %#v", version, seen)
		}
	}
	if result := store.Get(context.Background(), ref); result.Status != CheckpointStatusOK || result.Version != writers {
		t.Fatalf("final checkpoint = %#v", result)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if result := store.Get(context.Background(), ref); result.Status != CheckpointStatusDegraded || result.Code != CheckpointCodeClosed {
		t.Fatalf("Get after Close = %#v", result)
	}
}

func TestCheckpointRemoveNeverTouchesManuscriptFixture(t *testing.T) {
	root := t.TempDir()
	bookRoot := t.TempDir()
	manuscript := filepath.Join(bookRoot, "第一章-书稿.txt")
	fixture := []byte("用户书稿绝不能被 checkpoint 删除")
	if err := os.WriteFile(manuscript, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	store := newCheckpointAdapter(t, root)
	defer store.Close()
	ref := checkpointTestRef("checkpoint-remove", "session-a", "task-a", "run-a", "attempt-1")
	if result := store.Set(context.Background(), ref, []byte("runtime-only")); result.Status != CheckpointStatusOK {
		t.Fatal(result)
	}
	if result := store.Remove(context.Background(), ref); result.Status != CheckpointStatusMissing {
		t.Fatalf("Remove result = %#v", result)
	}
	if result := store.Get(context.Background(), ref); result.Status != CheckpointStatusMissing {
		t.Fatalf("removed checkpoint remained: %#v", result)
	}
	if got, err := os.ReadFile(manuscript); err != nil || !bytes.Equal(got, fixture) {
		t.Fatalf("manuscript fixture changed: %q err=%v", got, err)
	}
}

func checkpointTestRef(checkpointID, sessionID, taskID, runID, attemptID string) CheckpointRef {
	return CheckpointRef{
		CheckpointID: checkpointID,
		Namespace:    "writing-agent",
		AgentKind:    "ide",
		SessionID:    sessionID,
		TaskID:       taskID,
		RunID:        runID,
		AttemptID:    attemptID,
	}
}

type checkpointSensitiveShape struct {
	name     string
	value    string
	sentinel string
}

func checkpointSensitiveShapeCases() []checkpointSensitiveShape {
	return []checkpointSensitiveShape{
		{name: "sk underscore", value: "sk_WP3SecretSentinelA01", sentinel: "WP3SecretSentinelA01"},
		{name: "sk dash", value: "sk-WP3SecretSentinelB02", sentinel: "WP3SecretSentinelB02"},
		{name: "api key spaced", value: "api key: WP3SecretSentinelC03", sentinel: "WP3SecretSentinelC03"},
		{name: "api key mixed dash", value: "ApI-KeY=WP3SecretSentinelD04", sentinel: "WP3SecretSentinelD04"},
		{name: "api key underscore", value: "API_KEY:WP3SecretSentinelE05", sentinel: "WP3SecretSentinelE05"},
		{name: "runtime auth", value: "runtimeAuth: WP3SecretSentinelF06", sentinel: "WP3SecretSentinelF06"},
		{name: "authorization bearer", value: "Authorization: Bearer WP3SecretSentinelG07", sentinel: "WP3SecretSentinelG07"},
		{name: "authorization basic", value: "Authorization: Basic WP3SecretSentinelH08", sentinel: "WP3SecretSentinelH08"},
		{name: "nested json bytes", value: `{"outer":[{"api_key":"WP3SecretSentinelI09"}]}`, sentinel: "WP3SecretSentinelI09"},
	}
}

func assertRuntimeTreeExcludes(t *testing.T, root, sentinel string) {
	t.Helper()
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(sentinel)) {
			t.Fatalf("runtime tree persisted sentinel %q", sentinel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func checkpointEditField(key string, value any) func([]byte) []byte {
	return func(data []byte) []byte {
		var record map[string]any
		if err := json.Unmarshal(data, &record); err != nil {
			panic(err)
		}
		record[key] = value
		encoded, err := json.Marshal(record)
		if err != nil {
			panic(err)
		}
		return append(encoded, '\n')
	}
}

func newCheckpointAdapter(t *testing.T, root string) CheckpointStore {
	t.Helper()
	store, err := NewCheckpointStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestRuntimeEventTypeContractIsClosedAndExact(t *testing.T) {
	want := []RunEventType{
		"run.started",
		"context.accepted",
		"plan.questions",
		"plan.proposed",
		"plan.approved",
		"skill.load.requested",
		"skill.loaded",
		"delegation.started",
		"delegation.completed",
		"model.delta",
		"model.reasoning.delta",
		"tool.requested",
		"tool.started",
		"tool.completed",
		"artifact.created",
		"check.completed",
		"review.completed",
		"revision.requested",
		"proposal.ready",
		"run.interrupted",
		"run.waiting_author",
		"run.budget_exhausted",
		"run.completed",
		"run.failed",
		"run.aborted",
	}
	wantTerminal := []RunEventType{
		"run.interrupted",
		"run.budget_exhausted",
		"run.completed",
		"run.failed",
		"run.aborted",
	}
	if got := RunEventTypes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RunEvent types mismatch\n got: %#v\nwant: %#v", got, want)
	}
	if got := TerminalRunEventTypes(); !reflect.DeepEqual(got, wantTerminal) {
		t.Fatalf("terminal RunEvent types mismatch\n got: %#v\nwant: %#v", got, wantTerminal)
	}
	for _, eventType := range wantTerminal {
		if !IsTerminalRunEventType(eventType) {
			t.Fatalf("terminal event %q not recognized", eventType)
		}
	}
	if IsTerminalRunEventType(RunEventType("run.waiting_author")) {
		t.Fatal("run.waiting_author is not terminal")
	}

	store := newRuntimeEventStore(t)
	defer store.Close()
	if _, err := store.Append(context.Background(), "run-enums", RuntimeEventInput{
		Type: RunEventType("run.succeeded"),
	}); err == nil {
		t.Fatal("unknown RunEvent type must fail closed")
	}
}

func TestRuntimeEventSensitiveStringValuesFailBeforeDurableAppend(t *testing.T) {
	for index, shape := range checkpointSensitiveShapeCases() {
		for _, path := range []string{"append", "emit"} {
			t.Run(path+"/"+shape.name, func(t *testing.T) {
				root := t.TempDir()
				store, err := NewFileRuntimeEventStore(root)
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				input := RuntimeEventInput{
					Type: RunEventType("model.delta"),
					Payload: map[string]any{
						"message": shape.value,
						"nested":  []any{map[string]any{"note": shape.value}},
					},
				}
				var output bytes.Buffer
				if path == "append" {
					_, err = store.Append(context.Background(), fmt.Sprintf("run-secret-%d", index), input)
				} else {
					_, err = EmitRunEvent(context.Background(), store, &output, fmt.Sprintf("run-secret-%d", index), input)
				}
				if err == nil {
					t.Fatal("secret-shaped runtime event string was accepted")
				}
				if strings.Contains(err.Error(), shape.sentinel) {
					t.Fatalf("runtime event error reflected secret: %v", err)
				}
				if output.Len() != 0 || bytes.Contains(output.Bytes(), []byte(shape.sentinel)) {
					t.Fatalf("runtime event stdout changed or leaked: %q", output.Bytes())
				}
				assertRuntimeTreeExcludes(t, root, shape.sentinel)
			})
		}
	}

	t.Run("normal Chinese is accepted", func(t *testing.T) {
		store := newRuntimeEventStore(t)
		defer store.Close()
		if _, err := store.Append(context.Background(), "run-normal-chinese", RuntimeEventInput{
			Type:    RunEventType("model.delta"),
			Payload: map[string]any{"message": "这是正常中文事件摘要，不是凭证"},
		}); err != nil {
			t.Fatalf("normal Chinese runtime event was rejected: %v", err)
		}
	})
}

func TestRuntimeEventEmitAppendsBeforeStdoutAndSuppressesAppendOrSyncFailure(t *testing.T) {
	ctx := context.Background()

	t.Run("append happens before stdout", func(t *testing.T) {
		var output bytes.Buffer
		store := &runtimeEventProbeStore{output: &output}
		event, err := EmitRunEvent(ctx, store, &output, "run-order", RuntimeEventInput{
			Type:    RunEventType("run.started"),
			Payload: map[string]any{"source": "test"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !store.appended || !store.stdoutWasEmpty || event.Seq != 1 {
			t.Fatalf("append-before-output contract violated: store=%#v event=%#v", store, event)
		}
		frame, err := yanzhouprotocol.NewReader(bytes.NewReader(output.Bytes()), 0).ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		if frame.Kind != yanzhouprotocol.KindRunEvent || frame.RunID != event.RunID || frame.Seq != event.Seq {
			t.Fatalf("unexpected run.event frame: %#v", frame)
		}
		var wireFields map[string]any
		if err := json.Unmarshal(frame.Payload, &wireFields); err != nil {
			t.Fatal(err)
		}
		gotFields := make([]string, 0, len(wireFields))
		for field := range wireFields {
			gotFields = append(gotFields, field)
		}
		sort.Strings(gotFields)
		wantFields := []string{"payload", "runId", "schemaVersion", "seq", "timestamp", "type"}
		if !reflect.DeepEqual(gotFields, wantFields) {
			t.Fatalf("run.event wire fields = %v, want exactly %v", gotFields, wantFields)
		}
		var emitted RunEvent
		if err := json.Unmarshal(frame.Payload, &emitted); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(emitted, event) {
			t.Fatalf("emitted event mismatch\n got: %#v\nwant: %#v", emitted, event)
		}
	})

	for _, failure := range []string{"append failed", "sync failed"} {
		t.Run(failure, func(t *testing.T) {
			var output bytes.Buffer
			store := &runtimeEventFailStore{err: errors.New(failure)}
			if _, err := EmitRunEvent(ctx, store, &output, "run-failure", RuntimeEventInput{
				Type: RunEventType("run.started"),
			}); err == nil || !strings.Contains(err.Error(), failure) {
				t.Fatalf("EmitRunEvent error = %v, want %q", err, failure)
			}
			if output.Len() != 0 {
				t.Fatalf("stdout changed after %s: %q", failure, output.Bytes())
			}
		})
	}

	t.Run("file append failure", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		runDir := filepath.Join(root, "runs", "run-blocked")
		if err := os.MkdirAll(filepath.Join(runDir, "events.jsonl"), 0o700); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if _, err := EmitRunEvent(ctx, store, &output, "run-blocked", RuntimeEventInput{
			Type: RunEventType("run.started"),
		}); err == nil {
			t.Fatal("directory in place of event ledger must reject append")
		}
		if output.Len() != 0 {
			t.Fatalf("stdout changed after durable append failure: %q", output.Bytes())
		}
	})
}

func TestRuntimeEventFileStoreAllocatesSeqPersistsChecksumsAndEnforcesTerminal(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileRuntimeEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := store.Append(context.Background(), "run-ledger", RuntimeEventInput{
		Type:    RunEventType("run.started"),
		Payload: map[string]any{"phase": "start"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Append(context.Background(), "run-ledger", RuntimeEventInput{
		Type:    RunEventType("run.waiting_author"),
		Payload: map[string]any{"reason": "approval"},
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := store.Append(context.Background(), "run-ledger", RuntimeEventInput{
		Type: RunEventType("run.completed"),
		Payload: map[string]any{
			"schemaVersion":       "1",
			"reason":              "completed",
			"resumable":           false,
			"partialArtifactRefs": []any{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Seq != 1 || second.Seq != 2 || terminal.Seq != 3 {
		t.Fatalf("seq must start at 1 and increase: %d %d %d", first.Seq, second.Seq, terminal.Seq)
	}
	for _, event := range []RunEvent{first, second, terminal} {
		if event.SchemaVersion != "1" || event.RunID != "run-ledger" || event.Timestamp == "" {
			t.Fatalf("incomplete public event: %#v", event)
		}
	}
	if _, err := store.Append(context.Background(), "run-ledger", RuntimeEventInput{
		Type: RunEventType("run.failed"),
	}); err == nil {
		t.Fatal("a second terminal event must fail closed")
	}
	if _, err := store.Append(context.Background(), "run-ledger", RuntimeEventInput{
		Type: RunEventType("model.delta"),
	}); err == nil {
		t.Fatal("an event after terminal must fail closed")
	}

	ledgerPath := filepath.Join(root, "runs", "run-ledger", "events.jsonl")
	ledger, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := nonEmptyRuntimeEventLines(ledger)
	if len(lines) != 3 {
		t.Fatalf("ledger line count = %d, want 3", len(lines))
	}
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"schemaVersion", "runId", "seq", "timestamp", "type", "payload", "checksum"} {
			if _, ok := record[field]; !ok {
				t.Fatalf("JSONL record missing %q: %s", field, line)
			}
		}
		checksum, _ := record["checksum"].(string)
		if len(checksum) != 64 {
			t.Fatalf("durable checksum length = %d, want 64: %s", len(checksum), line)
		}
	}
}

func TestRuntimeEventCrashDurabilityOrderBeforeStdout(t *testing.T) {
	root := t.TempDir()
	ops := newRuntimeEventRecordingOps(root)
	store, err := newFileRuntimeEventStoreWithOps(root, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var output bytes.Buffer
	writer := &runtimeEventPhaseWriter{output: &output, ops: ops}
	if _, err := EmitRunEvent(context.Background(), store, writer, "run-ordering", RuntimeEventInput{
		Type:    RunEventType("run.started"),
		Payload: map[string]any{"phase": "start"},
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"dir-sync:.",
		"mkdir:runs",
		"dir-sync:.",
		"mkdir:runs/run-ordering",
		"dir-sync:runs",
		"create:runs/run-ordering/events.jsonl",
		"write",
		"file-sync",
		"close",
		"dir-sync:runs/run-ordering",
		"stdout",
	}
	if got := ops.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("durability/output phase order mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestRuntimeEventConstructorDurablyCreatesNestedRuntimeRoot(t *testing.T) {
	ancestor := t.TempDir()
	runtimeRoot := filepath.Join(ancestor, "level-one", "level-two", "agent-runtime")
	ops := newRuntimeEventRecordingOps(runtimeRoot)
	store, err := newFileRuntimeEventStoreWithOps(runtimeRoot, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	wantConstructor := []string{
		"mkdir:level-one",
		"dir-sync:.",
		"mkdir:level-one/level-two",
		"dir-sync:level-one",
		"mkdir:level-one/level-two/agent-runtime",
		"dir-sync:level-one/level-two",
		"mkdir:runs",
		"dir-sync:.",
	}
	if got := ops.snapshot(); !reflect.DeepEqual(got, wantConstructor) {
		t.Fatalf("nested runtime root durability phases mismatch\n got: %v\nwant: %v", got, wantConstructor)
	}
	if info, err := os.Stat(filepath.Join(runtimeRoot, "runs")); err != nil || !info.IsDir() {
		t.Fatalf("durably created runtime runs directory is unavailable: info=%v err=%v", info, err)
	}

	var output bytes.Buffer
	writer := &runtimeEventPhaseWriter{output: &output, ops: ops}
	if _, err := EmitRunEvent(context.Background(), store, writer, "run-nested-root", RuntimeEventInput{
		Type: RunEventType("run.started"),
	}); err != nil {
		t.Fatal(err)
	}
	phases := ops.snapshot()
	stdoutIndex := -1
	for index, phase := range phases {
		if phase == "stdout" {
			stdoutIndex = index
			break
		}
	}
	if stdoutIndex < len(wantConstructor) {
		t.Fatalf("stdout occurred before constructor durability prefix: phases=%v", phases)
	}
	if !reflect.DeepEqual(phases[:len(wantConstructor)], wantConstructor) {
		t.Fatalf("constructor durability prefix changed before stdout: %v", phases)
	}
}

func TestRuntimeEventUncertainAppendQuarantinesRunAndCloseAfterSyncCommits(t *testing.T) {
	for _, fault := range []string{"write-after-bytes", "short-write", "file-sync", "ledger-dir-sync"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			ops := newRuntimeEventRecordingOps(root)
			store, err := newFileRuntimeEventStoreWithOps(root, ops)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ops.setFault(fault)
			var output bytes.Buffer
			if _, err := EmitRunEvent(context.Background(), store, &output, "run-uncertain", RuntimeEventInput{
				Type: RunEventType("model.delta"),
			}); err == nil {
				t.Fatalf("fault %q did not fail append", fault)
			}
			if output.Len() != 0 {
				t.Fatalf("fault %q changed stdout: %q", fault, output.Bytes())
			}
			writesBefore := ops.phaseCount("write")
			ops.setFault("")
			if _, err := store.Append(context.Background(), "run-uncertain", RuntimeEventInput{
				Type: RunEventType("model.delta"),
			}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "quarantin") {
				t.Fatalf("append after uncertain %q error = %v", fault, err)
			}
			if got := ops.phaseCount("write"); got != writesBefore {
				t.Fatalf("quarantined run wrote again after %q: writes %d -> %d", fault, writesBefore, got)
			}
		})
	}

	t.Run("close error after file sync is committed", func(t *testing.T) {
		root := t.TempDir()
		ops := newRuntimeEventRecordingOps(root)
		store, err := newFileRuntimeEventStoreWithOps(root, ops)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		ops.setFault("close-after-sync")
		var output bytes.Buffer
		event, err := EmitRunEvent(context.Background(), store, &output, "run-close-commit", RuntimeEventInput{
			Type: RunEventType("run.started"),
		})
		if err != nil {
			t.Fatalf("close error after successful fsync created ghost event: %v", err)
		}
		if event.Seq != 1 || output.Len() == 0 {
			t.Fatalf("committed close-error event was not emitted: event=%#v output=%q", event, output.Bytes())
		}
		ops.setFault("")
		next, err := store.Append(context.Background(), "run-close-commit", RuntimeEventInput{
			Type: RunEventType("model.delta"),
		})
		if err != nil || next.Seq != 2 {
			t.Fatalf("append after committed close error = event %#v, error %v", next, err)
		}
	})
}

func TestRuntimeEventAppendRefusesLedgerPolicyOverflowBeforeWrite(t *testing.T) {
	root := t.TempDir()
	ops := newRuntimeEventRecordingOps(root)
	store, err := newFileRuntimeEventStoreWithOps(root, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ops.setReportedAppendSize(runtimeEventMaxLedgerBytes)
	if _, err := store.Append(context.Background(), "run-size-policy", RuntimeEventInput{
		Type: RunEventType("run.started"),
	}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "ledger") {
		t.Fatalf("ledger policy overflow error = %v", err)
	}
	if writes := ops.phaseCount("write"); writes != 0 {
		t.Fatalf("ledger policy overflow wrote %d times, want 0", writes)
	}
}

func TestRuntimeReplayCursorOrderBoundsGapAndChecksumCorruption(t *testing.T) {
	t.Run("cursor order and bounds", func(t *testing.T) {
		store := newRuntimeEventStore(t)
		defer store.Close()
		for index := 1; index <= 4; index++ {
			if _, err := store.Append(context.Background(), "run-replay", RuntimeEventInput{
				Type:    RunEventType("model.delta"),
				Payload: map[string]any{"step": index},
			}); err != nil {
				t.Fatal(err)
			}
		}
		page, err := store.ReplayAfter(context.Background(), "run-replay", 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) != 2 || page[0].Seq != 2 || page[1].Seq != 3 {
			t.Fatalf("unexpected replay page: %#v", page)
		}
		for _, limit := range []int{0, -1, 101} {
			if _, err := store.ReplayAfter(context.Background(), "run-replay", 0, limit); err == nil {
				t.Fatalf("unsafe replay limit %d accepted", limit)
			}
		}
	})

	t.Run("checksum corruption", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Append(context.Background(), "run-corrupt", RuntimeEventInput{
			Type:    RunEventType("model.delta"),
			Payload: map[string]any{"step": 1},
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		ledgerPath := filepath.Join(root, "runs", "run-corrupt", "events.jsonl")
		ledger, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		corrupt := bytes.Replace(ledger, []byte(`"step":1`), []byte(`"step":9`), 1)
		if bytes.Equal(corrupt, ledger) {
			t.Fatal("test failed to mutate ledger payload")
		}
		if err := os.WriteFile(ledgerPath, corrupt, 0o600); err != nil {
			t.Fatal(err)
		}
		store, err = NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if _, err := store.ReplayAfter(context.Background(), "run-corrupt", 0, 10); err == nil || !strings.Contains(strings.ToLower(err.Error()), "checksum") {
			t.Fatalf("checksum corruption error = %v", err)
		}
	})

	t.Run("sequence gap", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		for index := 1; index <= 3; index++ {
			if _, err := store.Append(context.Background(), "run-gap", RuntimeEventInput{
				Type:    RunEventType("model.delta"),
				Payload: map[string]any{"step": index},
			}); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		ledgerPath := filepath.Join(root, "runs", "run-gap", "events.jsonl")
		ledger, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		lines := nonEmptyRuntimeEventLines(ledger)
		if len(lines) != 3 {
			t.Fatalf("ledger line count = %d, want 3", len(lines))
		}
		gapped := append(append(append([]byte(nil), lines[0]...), '\n'), lines[2]...)
		gapped = append(gapped, '\n')
		if err := os.WriteFile(ledgerPath, gapped, 0o600); err != nil {
			t.Fatal(err)
		}
		store, err = NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if _, err := store.ReplayAfter(context.Background(), "run-gap", 0, 10); err == nil || !strings.Contains(strings.ToLower(err.Error()), "gap") {
			t.Fatalf("sequence gap error = %v", err)
		}
	})
}

func TestRuntimeEventTruncatedTailRecoveryIsPrefixSafeAndGapless(t *testing.T) {
	const partial = `{"partial":"WP3TailSentinelNoNewline"`

	t.Run("replay returns valid prefix and reports issue without mutation", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		ledgerPath := seedRuntimeEventLedger(t, store, root, "run-tail-replay", 2)
		appendRuntimeTail(t, ledgerPath, []byte(partial))
		before, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		page, err := store.ReplayAfter(context.Background(), "run-tail-replay", 0, 10)
		if err != nil {
			t.Fatalf("truncated-tail replay rejected valid prefix: %v", err)
		}
		if len(page) != 2 || page[0].Seq != 1 || page[1].Seq != 2 {
			t.Fatalf("truncated-tail replay prefix = %#v", page)
		}
		after, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("ReplayAfter modified truncated-tail ledger")
		}
		reporter, ok := store.(interface{ RecoveryIssueCodes() []string })
		if !ok {
			t.Fatal("runtime event store does not expose recovery issue codes")
		}
		codes := reporter.RecoveryIssueCodes()
		if !containsString(codes, "truncated_tail") {
			t.Fatalf("recovery issue codes = %v, want truncated_tail", codes)
		}
		encoded, _ := json.Marshal(codes)
		if bytes.Contains(encoded, []byte("WP3TailSentinelNoNewline")) {
			t.Fatalf("recovery issue exposed partial bytes: %s", encoded)
		}
	})

	t.Run("append truncates tail before writing next gapless record", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		ledgerPath := seedRuntimeEventLedger(t, store, root, "run-tail-repair", 2)
		appendRuntimeTail(t, ledgerPath, []byte(partial))
		third, err := store.Append(context.Background(), "run-tail-repair", RuntimeEventInput{
			Type:    RunEventType("model.delta"),
			Payload: map[string]any{"step": 3},
		})
		if err != nil {
			t.Fatalf("append after truncated tail failed: %v", err)
		}
		if third.Seq != 3 {
			t.Fatalf("repaired append seq = %d, want 3", third.Seq)
		}
		ledger, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(ledger, []byte("WP3TailSentinelNoNewline")) {
			t.Fatalf("partial tail remained or was joined to seq 3: %q", ledger)
		}
		lines := nonEmptyRuntimeEventLines(ledger)
		if len(lines) != 3 {
			t.Fatalf("repaired ledger lines = %d, want 3", len(lines))
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		page, err := reopened.ReplayAfter(context.Background(), "run-tail-repair", 0, 10)
		if err != nil || len(page) != 3 || page[2].Seq != 3 {
			t.Fatalf("reopened repaired replay = %#v, err=%v", page, err)
		}
	})

	for _, fault := range []string{"truncate", "truncate-sync"} {
		t.Run(fault+" failure quarantines without third record", func(t *testing.T) {
			root := t.TempDir()
			ops := newRuntimeEventRecordingOps(root)
			store, err := newFileRuntimeEventStoreWithOps(root, ops)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ledgerPath := seedRuntimeEventLedger(t, store, root, "run-tail-fault", 2)
			appendRuntimeTail(t, ledgerPath, []byte(partial))
			before, err := os.ReadFile(ledgerPath)
			if err != nil {
				t.Fatal(err)
			}
			ops.setFault(fault)
			if _, err := store.Append(context.Background(), "run-tail-fault", RuntimeEventInput{
				Type:    RunEventType("model.delta"),
				Payload: map[string]any{"step": 3},
			}); err == nil || strings.Contains(err.Error(), "WP3TailSentinelNoNewline") {
				t.Fatalf("%s recovery error = %v", fault, err)
			}
			afterFailure, err := os.ReadFile(ledgerPath)
			if err != nil {
				t.Fatal(err)
			}
			if fault == "truncate" && !bytes.Equal(afterFailure, before) {
				t.Fatal("truncate failure modified ledger")
			}
			if bytes.Contains(afterFailure, []byte(`"seq":3`)) {
				t.Fatalf("%s failure wrote third record", fault)
			}
			ops.setFault("")
			if _, err := store.Append(context.Background(), "run-tail-fault", RuntimeEventInput{
				Type:    RunEventType("model.delta"),
				Payload: map[string]any{"step": 3},
			}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "quarantin") {
				t.Fatalf("append after %s recovery failure = %v, want quarantine", fault, err)
			}
			afterQuarantine, err := os.ReadFile(ledgerPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(afterQuarantine, afterFailure) {
				t.Fatalf("quarantined append after %s changed ledger", fault)
			}
		})
	}

	t.Run("middle corruption remains an error and is never truncated", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		ledgerPath := seedRuntimeEventLedger(t, store, root, "run-middle-corrupt", 3)
		ledger, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		corrupt := bytes.Replace(ledger, []byte(`"step":2`), []byte(`"step":9`), 1)
		if bytes.Equal(corrupt, ledger) {
			t.Fatal("test did not corrupt middle record")
		}
		if err := os.WriteFile(ledgerPath, corrupt, 0o600); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(ledgerPath)
		if _, err := store.ReplayAfter(context.Background(), "run-middle-corrupt", 0, 10); err == nil {
			t.Fatal("middle corruption replay did not fail closed")
		}
		if _, err := store.Append(context.Background(), "run-middle-corrupt", RuntimeEventInput{
			Type: RunEventType("model.delta"),
		}); err == nil {
			t.Fatal("middle corruption append did not fail closed")
		}
		after, _ := os.ReadFile(ledgerPath)
		if !bytes.Equal(after, before) {
			t.Fatal("middle corruption was automatically truncated or rewritten")
		}
	})
}

func TestRuntimeReplayRejectsOverlongRecordAndOversizedLedger(t *testing.T) {
	t.Run("overlong record", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		runDir := filepath.Join(root, "runs", "run-overlong")
		if err := os.Mkdir(runDir, 0o700); err != nil {
			t.Fatal(err)
		}
		line := append(bytes.Repeat([]byte{'x'}, runtimeEventMaxRecordBytes+1), '\n')
		if err := os.WriteFile(filepath.Join(runDir, "events.jsonl"), line, 0o600); err != nil {
			t.Fatal(err)
		}
		if page, err := store.ReplayAfter(context.Background(), "run-overlong", 0, 1); err == nil || !strings.Contains(strings.ToLower(err.Error()), "record") {
			t.Fatalf("overlong record replay = page %#v, error %v", page, err)
		}
	})

	t.Run("oversized ledger", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		runDir := filepath.Join(root, "runs", "run-oversized")
		if err := os.Mkdir(runDir, 0o700); err != nil {
			t.Fatal(err)
		}
		ledgerPath := filepath.Join(runDir, "events.jsonl")
		file, err := os.Create(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(runtimeEventMaxLedgerBytes + 1); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if page, err := store.ReplayAfter(context.Background(), "run-oversized", 0, 1); err == nil || !strings.Contains(strings.ToLower(err.Error()), "ledger") {
			t.Fatalf("oversized ledger replay = page %#v, error %v", page, err)
		}
	})
}

func TestRuntimeEventAppendFailsClosedAfterLedgerCorruption(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileRuntimeEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Append(context.Background(), "run-corrupt-append", RuntimeEventInput{
		Type:    RunEventType("model.delta"),
		Payload: map[string]any{"step": 1},
	}); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(root, "runs", "run-corrupt-append", "events.jsonl")
	ledger, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.Replace(ledger, []byte(`"step":1`), []byte(`"step":9`), 1)
	if bytes.Equal(corrupt, ledger) {
		t.Fatal("test failed to mutate ledger payload")
	}
	if err := os.WriteFile(ledgerPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), "run-corrupt-append", RuntimeEventInput{
		Type:    RunEventType("model.delta"),
		Payload: map[string]any{"step": 2},
	}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "checksum") {
		t.Fatalf("append after ledger corruption error = %v", err)
	}
}

func TestRuntimeEventConcurrentAppendHasUniqueGaplessSequence(t *testing.T) {
	store := newRuntimeEventStore(t)
	defer store.Close()

	const count = 32
	results := make(chan RunEvent, count)
	errorsFound := make(chan error, count)
	var wait sync.WaitGroup
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			event, err := store.Append(context.Background(), "run-concurrent", RuntimeEventInput{
				Type:    RunEventType("model.delta"),
				Payload: map[string]any{"worker": index},
			})
			if err != nil {
				errorsFound <- err
				return
			}
			results <- event
		}(index)
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}

	sequences := make([]int, 0, count)
	for event := range results {
		sequences = append(sequences, int(event.Seq))
	}
	sort.Ints(sequences)
	if len(sequences) != count {
		t.Fatalf("append result count = %d, want %d", len(sequences), count)
	}
	for index, seq := range sequences {
		if want := index + 1; seq != want {
			t.Fatalf("sequence[%d] = %d, want %d; all=%v", index, seq, want, sequences)
		}
	}
	replayed, err := store.ReplayAfter(context.Background(), "run-concurrent", 0, count)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != count {
		t.Fatalf("replayed event count = %d, want %d", len(replayed), count)
	}
}

func TestRuntimeEventRejectsForbiddenPayloadUnsafeRootRunIDAndFilename(t *testing.T) {
	t.Run("unsafe runtime roots", func(t *testing.T) {
		for _, root := range []string{"", "relative/runtime", string(filepath.Separator)} {
			if store, err := NewFileRuntimeEventStore(root); err == nil {
				store.Close()
				t.Fatalf("unsafe runtime root %q accepted", root)
			}
		}
		base := t.TempDir()
		escaping := base + string(filepath.Separator) + "child" + string(filepath.Separator) + ".." + string(filepath.Separator) + "other"
		if store, err := NewFileRuntimeEventStore(escaping); err == nil {
			store.Close()
			t.Fatalf("runtime root with .. accepted: %q", escaping)
		}
		target := filepath.Join(base, "real-root")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(base, "linked-root")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if store, err := NewFileRuntimeEventStore(link); err == nil {
			store.Close()
			t.Fatal("symlink runtime root accepted")
		}
	})

	t.Run("unsafe run ids and system identities", func(t *testing.T) {
		store := newRuntimeEventStore(t)
		defer store.Close()
		for _, runID := range []string{"", ".", "..", "../escape", "nested/run", `nested\\run`, "run id", strings.Repeat("r", 129)} {
			if _, err := store.Append(context.Background(), runID, RuntimeEventInput{
				Type: RunEventType("run.started"),
			}); err == nil {
				t.Fatalf("unsafe run id %q accepted", runID)
			}
		}
		for _, key := range []string{"seq", "schemaVersion", "runId", "timestamp", "checksum", "eventId"} {
			if _, err := store.Append(context.Background(), "run-owned-fields", RuntimeEventInput{
				Type:    RunEventType("run.started"),
				Payload: map[string]any{key: 1},
			}); err == nil {
				t.Fatalf("caller-supplied event identity field %q accepted", key)
			}
		}
	})

	t.Run("forbidden payload keys recursively", func(t *testing.T) {
		store := newRuntimeEventStore(t)
		defer store.Close()
		for index, key := range []string{
			"apiKey", "runtimeAuth", "credentials", "rawRequest", "rawResponse", "rawTool",
			"stderr", "prompt", "bookPath", "workspacePath", "effectiveModelProfile",
		} {
			payload := map[string]any{"safe": map[string]any{"nested": map[string]any{key: "secret-sentinel"}}}
			if _, err := store.Append(context.Background(), fmt.Sprintf("run-forbidden-%d", index), RuntimeEventInput{
				Type:    RunEventType("model.delta"),
				Payload: payload,
			}); err == nil {
				t.Fatalf("forbidden recursive payload key %q accepted", key)
			}
		}
		if _, err := store.Append(context.Background(), "run-large-string", RuntimeEventInput{
			Type:    RunEventType("model.delta"),
			Payload: map[string]any{"text": strings.Repeat("x", 9000)},
		}); err == nil {
			t.Fatal("oversized payload string accepted")
		}
	})

	t.Run("unsafe event filename symlink", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewFileRuntimeEventStore(root)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		runDir := filepath.Join(root, "runs", "run-symlink-file")
		if err := os.MkdirAll(runDir, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "outside.jsonl")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(runDir, "events.jsonl")); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Append(context.Background(), "run-symlink-file", RuntimeEventInput{
			Type: RunEventType("run.started"),
		}); err == nil {
			t.Fatal("symlink event filename accepted")
		}
		outside, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if len(outside) != 0 {
			t.Fatalf("symlink target was modified: %q", outside)
		}
	})

	t.Run("os root contains symlink swap", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		ops := newRuntimeEventRecordingOps(root)
		store, err := newFileRuntimeEventStoreWithOps(root, ops)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		ops.setSwap("run-swap", outside)
		if _, err := store.Append(context.Background(), "run-swap", RuntimeEventInput{
			Type: RunEventType("run.started"),
		}); err == nil {
			t.Fatal("symlink swap escaped the held os.Root")
		}
		if _, err := os.Stat(filepath.Join(outside, "events.jsonl")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("outside target was touched: %v", err)
		}
	})
}

func TestRuntimeEventCloseIsIdempotentAndAppendAfterCloseFails(t *testing.T) {
	store := newRuntimeEventStore(t)
	rootHandle := store.(*fileRuntimeEventStore).root
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
	if _, err := store.Append(context.Background(), "run-closed", RuntimeEventInput{
		Type: RunEventType("run.started"),
	}); err == nil {
		t.Fatal("append after close must fail")
	}
	if _, err := store.ReplayAfter(context.Background(), "run-closed", 0, 1); err == nil {
		t.Fatal("replay after close must fail")
	}
	if _, err := rootHandle.Stat("."); err == nil {
		t.Fatal("Close did not close the held os.Root")
	}
}

func newRuntimeEventStore(t *testing.T) RuntimeEventStore {
	t.Helper()
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func seedRuntimeEventLedger(t *testing.T, store RuntimeEventStore, root, runID string, count int) string {
	t.Helper()
	for index := 1; index <= count; index++ {
		if _, err := store.Append(context.Background(), runID, RuntimeEventInput{
			Type:    RunEventType("model.delta"),
			Payload: map[string]any{"step": index},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(root, "runs", runID, runtimeEventLedgerFilename)
}

func appendRuntimeTail(t *testing.T, ledgerPath string, tail []byte) {
	t.Helper()
	file, err := os.OpenFile(ledgerPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(tail); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func nonEmptyRuntimeEventLines(data []byte) [][]byte {
	var lines [][]byte
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) != 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

type runtimeEventProbeStore struct {
	output         *bytes.Buffer
	appended       bool
	stdoutWasEmpty bool
}

func (s *runtimeEventProbeStore) Append(_ context.Context, runID string, input RuntimeEventInput) (RunEvent, error) {
	s.appended = true
	s.stdoutWasEmpty = s.output.Len() == 0
	return RunEvent{
		SchemaVersion: "1",
		RunID:         runID,
		Seq:           1,
		Timestamp:     "2026-07-23T00:00:00Z",
		Type:          input.Type,
		Payload:       input.Payload,
	}, nil
}

func (s *runtimeEventProbeStore) ReplayAfter(context.Context, string, uint64, int) ([]RunEvent, error) {
	return nil, nil
}

func (s *runtimeEventProbeStore) Close() error { return nil }

type runtimeEventFailStore struct{ err error }

func (s *runtimeEventFailStore) Append(context.Context, string, RuntimeEventInput) (RunEvent, error) {
	return RunEvent{}, s.err
}

func (s *runtimeEventFailStore) ReplayAfter(context.Context, string, uint64, int) ([]RunEvent, error) {
	return nil, s.err
}

func (s *runtimeEventFailStore) Close() error { return nil }

type runtimeEventRecordingOps struct {
	mu                 sync.Mutex
	base               runtimeEventFileOps
	rootPath           string
	phases             []string
	fault              string
	swapRunID          string
	swapOutside        string
	reportedAppendSize int64
}

func newRuntimeEventRecordingOps(rootPath string) *runtimeEventRecordingOps {
	return &runtimeEventRecordingOps{
		base:               newOSRuntimeEventFileOps(),
		rootPath:           rootPath,
		reportedAppendSize: -1,
	}
}

func (o *runtimeEventRecordingOps) mkdir(root *os.Root, name string, perm os.FileMode) (bool, error) {
	created, err := o.base.mkdir(root, name, perm)
	if created {
		o.record("mkdir:" + name)
	}
	return created, err
}

func (o *runtimeEventRecordingOps) openAppend(root *os.Root, name string, perm os.FileMode) (runtimeEventFile, bool, error) {
	o.mu.Lock()
	swapRunID, swapOutside := o.swapRunID, o.swapOutside
	if swapRunID != "" && name == filepath.Join("runs", swapRunID, runtimeEventLedgerFilename) {
		o.swapRunID = ""
	}
	o.mu.Unlock()
	if swapRunID != "" && name == filepath.Join("runs", swapRunID, runtimeEventLedgerFilename) {
		runDir := filepath.Join(o.rootPath, "runs", swapRunID)
		if err := os.Remove(runDir); err != nil {
			return nil, false, err
		}
		if err := os.Symlink(swapOutside, runDir); err != nil {
			return nil, false, err
		}
	}
	file, created, err := o.base.openAppend(root, name, perm)
	if err != nil {
		return nil, created, err
	}
	if created {
		o.record("create:" + name)
	} else {
		o.record("open-append:" + name)
	}
	return &runtimeEventRecordingFile{runtimeEventFile: file, ops: o}, created, nil
}

func (o *runtimeEventRecordingOps) openRepair(root *os.Root, name string) (runtimeEventFile, error) {
	file, err := o.base.openRepair(root, name)
	if err != nil {
		return nil, err
	}
	o.record("open-repair:" + name)
	return &runtimeEventRecordingFile{runtimeEventFile: file, ops: o}, nil
}

func (o *runtimeEventRecordingOps) openRead(root *os.Root, name string) (runtimeEventFile, error) {
	return o.base.openRead(root, name)
}

func (o *runtimeEventRecordingOps) syncDirectory(root *os.Root, name string) error {
	o.record("dir-sync:" + name)
	if o.currentFault() == "ledger-dir-sync" && strings.HasPrefix(name, filepath.Join("runs", "run-uncertain")) {
		return errors.New("injected ledger directory sync failure")
	}
	return o.base.syncDirectory(root, name)
}

func (o *runtimeEventRecordingOps) setFault(fault string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.fault = fault
}

func (o *runtimeEventRecordingOps) currentFault() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.fault
}

func (o *runtimeEventRecordingOps) setSwap(runID, outside string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.swapRunID = runID
	o.swapOutside = outside
}

func (o *runtimeEventRecordingOps) setReportedAppendSize(size int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reportedAppendSize = size
}

func (o *runtimeEventRecordingOps) record(phase string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.phases = append(o.phases, phase)
}

func (o *runtimeEventRecordingOps) snapshot() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.phases...)
}

func (o *runtimeEventRecordingOps) phaseCount(phase string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	count := 0
	for _, got := range o.phases {
		if got == phase {
			count++
		}
	}
	return count
}

type runtimeEventRecordingFile struct {
	runtimeEventFile
	ops *runtimeEventRecordingOps
}

func (f *runtimeEventRecordingFile) Stat() (os.FileInfo, error) {
	info, err := f.runtimeEventFile.Stat()
	if err != nil {
		return nil, err
	}
	f.ops.mu.Lock()
	reportedSize := f.ops.reportedAppendSize
	f.ops.mu.Unlock()
	if reportedSize < 0 {
		return info, nil
	}
	return runtimeEventSizedFileInfo{FileInfo: info, size: reportedSize}, nil
}

func (f *runtimeEventRecordingFile) Write(data []byte) (int, error) {
	f.ops.record("write")
	switch f.ops.currentFault() {
	case "write-after-bytes":
		written, _ := f.runtimeEventFile.Write(data)
		return written, errors.New("injected write error after bytes")
	case "short-write":
		if len(data) == 0 {
			return 0, nil
		}
		return f.runtimeEventFile.Write(data[:len(data)-1])
	default:
		return f.runtimeEventFile.Write(data)
	}
}

func (f *runtimeEventRecordingFile) Truncate(size int64) error {
	f.ops.record("truncate")
	if f.ops.currentFault() == "truncate" {
		return errors.New("injected truncate failure")
	}
	return f.runtimeEventFile.Truncate(size)
}

func (f *runtimeEventRecordingFile) Sync() error {
	f.ops.record("file-sync")
	if fault := f.ops.currentFault(); fault == "file-sync" || fault == "truncate-sync" {
		return errors.New("injected file sync failure")
	}
	return f.runtimeEventFile.Sync()
}

func (f *runtimeEventRecordingFile) Close() error {
	f.ops.record("close")
	err := f.runtimeEventFile.Close()
	if f.ops.currentFault() == "close-after-sync" {
		return errors.New("injected close error after sync")
	}
	return err
}

type runtimeEventPhaseWriter struct {
	output *bytes.Buffer
	ops    *runtimeEventRecordingOps
}

type runtimeEventSizedFileInfo struct {
	os.FileInfo
	size int64
}

func (i runtimeEventSizedFileInfo) Size() int64 { return i.size }

func (w *runtimeEventPhaseWriter) Write(data []byte) (int, error) {
	w.ops.record("stdout")
	return w.output.Write(data)
}

func TestRuntimeTerminationEnumsAndMatrixAreClosedAndExact(t *testing.T) {
	wantTimeouts := []RuntimeTimeoutType{
		"startup_timeout",
		"handshake_timeout",
		"provider_connect_timeout",
		"provider_idle_timeout",
		"tool_timeout",
		"run_wall_timeout",
		"cancel_grace_timeout",
		"display_consumer_timeout",
	}
	wantCauses := []TerminationCause{
		"provider_idle_timeout",
		"user_cancelled",
		"provider_error",
		"panic",
		"budget_exhausted",
		"run_wall_timeout",
	}
	wantStates := []TerminalRunState{
		"interrupted",
		"budget_exhausted",
		"completed",
		"failed",
		"aborted",
	}
	if got := RuntimeTimeoutTypes(); !reflect.DeepEqual(got, wantTimeouts) {
		t.Fatalf("runtime timeout types mismatch\n got: %#v\nwant: %#v", got, wantTimeouts)
	}
	if got := TerminationCauses(); !reflect.DeepEqual(got, wantCauses) {
		t.Fatalf("termination causes mismatch\n got: %#v\nwant: %#v", got, wantCauses)
	}
	if got := TerminalRunStates(); !reflect.DeepEqual(got, wantStates) {
		t.Fatalf("terminal run states mismatch\n got: %#v\nwant: %#v", got, wantStates)
	}

	resumableFalse := false
	resumableTrue := true
	cases := []struct {
		name  string
		input TerminationInput
		want  TerminationDecision
	}{
		{
			name:  "provider idle",
			input: TerminationInput{Cause: "provider_idle_timeout"},
			want: TerminationDecision{
				EventType:           RunEventTypeRunInterrupted,
				State:               "interrupted",
				Reason:              "provider_idle_timeout",
				Resumable:           true,
				PartialArtifactRefs: []string{},
				TimeoutType:         "provider_idle_timeout",
			},
		},
		{
			name:  "cancel",
			input: TerminationInput{Cause: "user_cancelled"},
			want: TerminationDecision{
				EventType:           RunEventTypeRunAborted,
				State:               "aborted",
				Reason:              "cancelled",
				Resumable:           false,
				PartialArtifactRefs: []string{},
			},
		},
		{
			name:  "provider error",
			input: TerminationInput{Cause: "provider_error", Resumable: &resumableFalse},
			want: TerminationDecision{
				EventType:           RunEventTypeRunFailed,
				State:               "failed",
				Reason:              "provider_error",
				Resumable:           false,
				PartialArtifactRefs: []string{},
			},
		},
		{
			name: "panic",
			input: TerminationInput{
				Cause:               "panic",
				Resumable:           &resumableTrue,
				CheckpointID:        "checkpoint-7",
				PartialArtifactRefs: []string{"artifact-7"},
			},
			want: TerminationDecision{
				EventType:           RunEventTypeRunFailed,
				State:               "failed",
				Reason:              "panic",
				Resumable:           true,
				CheckpointID:        "checkpoint-7",
				PartialArtifactRefs: []string{"artifact-7"},
			},
		},
		{
			name:  "budget",
			input: TerminationInput{Cause: "budget_exhausted", PartialArtifactRefs: []string{"artifact-budget"}},
			want: TerminationDecision{
				EventType:           RunEventTypeRunBudgetExhausted,
				State:               "budget_exhausted",
				Reason:              "budget_exhausted",
				Resumable:           true,
				PartialArtifactRefs: []string{"artifact-budget"},
			},
		},
		{
			name:  "wall budget",
			input: TerminationInput{Cause: "run_wall_timeout"},
			want: TerminationDecision{
				EventType:           RunEventTypeRunBudgetExhausted,
				State:               "budget_exhausted",
				Reason:              "run_wall_timeout",
				Resumable:           true,
				PartialArtifactRefs: []string{},
				TimeoutType:         "run_wall_timeout",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ClassifyTermination(tc.input)
			if err != nil {
				t.Fatalf("ClassifyTermination() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("termination decision mismatch\n got: %#v\nwant: %#v", got, tc.want)
			}
			finish, ok := agent.ClassifyRunTermination(agent.RunTerminationCause(tc.input.Cause))
			if !ok || finish.Status != string(tc.want.State) || finish.Reason != tc.want.Reason {
				t.Fatalf("agent finish mapping contradicts adapter: %#v ok=%v", finish, ok)
			}
		})
	}
}

func TestRuntimeTerminationInputFailsClosedWithoutSecretReflection(t *testing.T) {
	const sentinel = "sk-step9-denova-secret-never-reflect-123456789"
	resumableFalse := false
	resumableTrue := true
	tooManyRefs := make([]string, 33)
	for index := range tooManyRefs {
		tooManyRefs[index] = fmt.Sprintf("artifact-%d", index)
	}
	invalid := []TerminationInput{
		{Cause: "startup_timeout"},
		{Cause: "provider_idle_timeout", TimeoutType: "tool_timeout"},
		{Cause: "user_cancelled", TimeoutType: "cancel_grace_timeout"},
		{Cause: "provider_error"},
		{Cause: "panic"},
		{Cause: "provider_error", Resumable: &resumableTrue},
		{Cause: "panic", Resumable: &resumableTrue, PartialArtifactRefs: []string{}},
		{Cause: "provider_error", Resumable: &resumableFalse, CheckpointID: "/Users/writer/private/checkpoint"},
		{Cause: "provider_error", Resumable: &resumableFalse, PartialArtifactRefs: []string{sentinel}},
		{Cause: "budget_exhausted", PartialArtifactRefs: tooManyRefs},
		{Cause: "budget_exhausted", PartialArtifactRefs: []string{strings.Repeat("x", 129)}},
	}
	for index, input := range invalid {
		if decision, err := ClassifyTermination(input); err == nil {
			t.Fatalf("invalid termination %d accepted: %#v", index, decision)
		} else if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), "/Users/writer") {
			t.Fatalf("invalid termination %d reflected private input: %q", index, err)
		}
	}

	for index, raw := range [][]byte{
		[]byte(`{"cause":"user_cancelled","apiKey":"` + sentinel + `"}`),
		[]byte(`{"cause":"panic","resumable":false,"rawResponse":"` + sentinel + `"}`),
		[]byte(`{"cause":"budget_exhausted","reason":"invented"}`),
		[]byte(`{"cause":"user_cancelled"} trailing`),
	} {
		if input, err := DecodeTerminationInput(raw); err == nil {
			t.Fatalf("unsafe termination JSON %d accepted: %#v", index, input)
		} else if strings.Contains(err.Error(), sentinel) {
			t.Fatalf("unsafe termination JSON %d reflected sentinel: %q", index, err)
		}
	}
}

func TestRuntimeEventTerminalPayloadContractIsClosedAndExact(t *testing.T) {
	valid := []struct {
		name      string
		eventType RunEventType
		payload   map[string]any
	}{
		{
			name:      "provider idle",
			eventType: RunEventTypeRunInterrupted,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "provider_idle_timeout",
				"resumable":           true,
				"partialArtifactRefs": []any{},
				"timeoutType":         "provider_idle_timeout",
			},
		},
		{
			name:      "cancel",
			eventType: RunEventTypeRunAborted,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "cancelled",
				"resumable":           false,
				"partialArtifactRefs": []any{},
			},
		},
		{
			name:      "provider error",
			eventType: RunEventTypeRunFailed,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "provider_error",
				"resumable":           false,
				"partialArtifactRefs": []any{},
			},
		},
		{
			name:      "panic with durable refs",
			eventType: RunEventTypeRunFailed,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "panic",
				"resumable":           true,
				"partialArtifactRefs": []any{"artifact-7"},
				"checkpointId":        "checkpoint-7",
			},
		},
		{
			name:      "budget",
			eventType: RunEventTypeRunBudgetExhausted,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "budget_exhausted",
				"resumable":           true,
				"partialArtifactRefs": []any{"artifact-budget"},
			},
		},
		{
			name:      "wall timeout budget",
			eventType: RunEventTypeRunBudgetExhausted,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "run_wall_timeout",
				"resumable":           true,
				"partialArtifactRefs": []any{},
				"timeoutType":         "run_wall_timeout",
			},
		},
		{
			name:      "completed",
			eventType: RunEventTypeRunCompleted,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "completed",
				"resumable":           false,
				"partialArtifactRefs": []any{},
			},
		},
	}
	for index, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			store := newRuntimeEventStore(t)
			defer store.Close()
			runID := fmt.Sprintf("run-terminal-valid-%d", index)
			event, err := store.Append(context.Background(), runID, RuntimeEventInput{
				Type:    tc.eventType,
				Payload: tc.payload,
			})
			if err != nil {
				t.Fatalf("valid terminal payload rejected: %v", err)
			}
			replayed, err := store.ReplayAfter(context.Background(), runID, 0, 1)
			if err != nil || len(replayed) != 1 || !reflect.DeepEqual(replayed[0], event) {
				t.Fatalf("terminal replay mismatch: event=%#v replay=%#v err=%v", event, replayed, err)
			}
		})
	}

	const sentinel = "sk-step9-terminal-payload-never-reflect-123456789"
	invalid := []struct {
		name      string
		eventType RunEventType
		payload   map[string]any
	}{
		{"schemaless", RunEventTypeRunFailed, map[string]any{"reason": "provider_error", "resumable": false, "partialArtifactRefs": []any{}}},
		{"invented reason", RunEventTypeRunFailed, map[string]any{"schemaVersion": "1", "reason": "invented", "resumable": false, "partialArtifactRefs": []any{}}},
		{"event reason mismatch", RunEventTypeRunAborted, map[string]any{"schemaVersion": "1", "reason": "provider_error", "resumable": false, "partialArtifactRefs": []any{}}},
		{"resumable mismatch", RunEventTypeRunInterrupted, map[string]any{"schemaVersion": "1", "reason": "provider_idle_timeout", "resumable": false, "partialArtifactRefs": []any{}, "timeoutType": "provider_idle_timeout"}},
		{"timeout mismatch", RunEventTypeRunBudgetExhausted, map[string]any{"schemaVersion": "1", "reason": "run_wall_timeout", "resumable": true, "partialArtifactRefs": []any{}, "timeoutType": "provider_idle_timeout"}},
		{"missing durable refs", RunEventTypeRunFailed, map[string]any{"schemaVersion": "1", "reason": "panic", "resumable": true, "partialArtifactRefs": []any{}}},
		{"extra raw", RunEventTypeRunFailed, map[string]any{"schemaVersion": "1", "reason": "provider_error", "resumable": false, "partialArtifactRefs": []any{}, "rawResponse": sentinel}},
		{"path ref", RunEventTypeRunFailed, map[string]any{"schemaVersion": "1", "reason": "provider_error", "resumable": false, "partialArtifactRefs": []any{"/Users/writer/private"}}},
		{"credential ref", RunEventTypeRunFailed, map[string]any{"schemaVersion": "1", "reason": "provider_error", "resumable": false, "partialArtifactRefs": []any{sentinel}}},
		{"credential key", RunEventTypeRunFailed, map[string]any{"schemaVersion": "1", "reason": "provider_error", "resumable": false, "partialArtifactRefs": []any{}, "apiKey": sentinel}},
	}
	for index, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewFileRuntimeEventStore(root)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			runID := fmt.Sprintf("run-terminal-invalid-%d", index)
			if event, err := store.Append(context.Background(), runID, RuntimeEventInput{Type: tc.eventType, Payload: tc.payload}); err == nil {
				t.Fatalf("invalid terminal payload accepted: %#v", event)
			} else if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), "/Users/writer") {
				t.Fatalf("terminal validation reflected private input: %q", err)
			}
			if _, err := os.Stat(filepath.Join(root, "runs", runID)); !os.IsNotExist(err) {
				t.Fatalf("invalid terminal payload reached durable append: %v", err)
			}
			assertRuntimeTreeExcludes(t, root, sentinel)
		})
	}
}

func TestRuntimeEventTerminalPayloadRejectsExplicitNullOptionalFields(t *testing.T) {
	const genericError = "terminal runtime event payload is invalid"
	testCases := []struct {
		name      string
		eventType RunEventType
		payload   map[string]any
	}{
		{
			name:      "failed checkpointId null",
			eventType: RunEventTypeRunFailed,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "provider_error",
				"resumable":           false,
				"partialArtifactRefs": []any{},
				"checkpointId":        nil,
			},
		},
		{
			name:      "completed checkpointId null",
			eventType: RunEventTypeRunCompleted,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "completed",
				"resumable":           false,
				"partialArtifactRefs": []any{},
				"checkpointId":        nil,
			},
		},
		{
			name:      "failed timeoutType null",
			eventType: RunEventTypeRunFailed,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "provider_error",
				"resumable":           false,
				"partialArtifactRefs": []any{},
				"timeoutType":         nil,
			},
		},
		{
			name:      "budget timeoutType null",
			eventType: RunEventTypeRunBudgetExhausted,
			payload: map[string]any{
				"schemaVersion":       "1",
				"reason":              "budget_exhausted",
				"resumable":           true,
				"partialArtifactRefs": []any{"artifact-budget"},
				"timeoutType":         nil,
			},
		},
	}

	assertGenericRejection := func(t *testing.T, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), genericError) {
			t.Fatalf("explicit null did not produce the generic terminal error: %v", err)
		}
		for _, reflected := range []string{"checkpointId", "timeoutType", "null"} {
			if strings.Contains(err.Error(), reflected) {
				t.Fatalf("terminal validation reflected rejected payload details: %q", err)
			}
		}
	}

	for index, tc := range testCases {
		t.Run(tc.name+" append", func(t *testing.T) {
			root := t.TempDir()
			store, err := NewFileRuntimeEventStore(root)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			runID := fmt.Sprintf("run-terminal-null-append-%d", index)
			event, err := store.Append(context.Background(), runID, RuntimeEventInput{
				Type:    tc.eventType,
				Payload: tc.payload,
			})
			if err == nil {
				t.Fatalf("explicit null terminal payload reached append projection: %#v", event)
			}
			assertGenericRejection(t, err)
			if _, statErr := os.Stat(filepath.Join(root, "runs", runID)); !os.IsNotExist(statErr) {
				t.Fatalf("explicit null terminal payload reached durable append: %v", statErr)
			}
		})

		t.Run(tc.name+" replay", func(t *testing.T) {
			root := t.TempDir()
			runID := fmt.Sprintf("run-terminal-null-replay-%d", index)
			record, err := newRuntimeEventRecord(RunEvent{
				SchemaVersion: runtimeEventSchemaVersion,
				RunID:         runID,
				Seq:           1,
				Timestamp:     "2026-07-24T12:00:00Z",
				Type:          tc.eventType,
				Payload:       tc.payload,
			})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			runDirectory := filepath.Join(root, "runs", runID)
			if err := os.MkdirAll(runDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(runDirectory, runtimeEventLedgerFilename), append(encoded, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := NewFileRuntimeEventStore(root)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			page, err := store.ReplayAfter(context.Background(), runID, 0, 1)
			if err == nil {
				t.Fatalf("explicit null terminal payload reached replay projection: %#v", page)
			}
			if len(page) != 0 {
				t.Fatalf("explicit null terminal payload returned replay events: %#v", page)
			}
			assertGenericRejection(t, err)
		})
	}
}

func TestRuntimeProviderIdleTerminationUsesStableBoundedLifecycleOutcome(t *testing.T) {
	workspace := t.TempDir()
	conversation := &resumeOrderingConversation{
		pending: session.Interruption{
			ID:     "interruption-origin-1",
			Status: session.InterruptionPending,
		},
		prepareSucceeds: true,
	}
	runnerAgent := &blockingResumeAgent{}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})
	var publicErrors []string

	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		book.NewService(workspace),
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{
			TaskID:      "resume-request-1",
			Workspace:   workspace,
			IdleTimeout: 5 * time.Millisecond,
		},
		func(event agent.Event) {
			if event.Type == "error" {
				data, _ := event.Data.(map[string]string)
				publicErrors = append(publicErrors, data["message"])
			}
		},
	)

	if conversation.finishCalls != 1 || conversation.finishInput.Outcome != agent.ResumeAttemptOutcomeProviderIdleTimeout {
		t.Fatalf("provider idle finish = calls %d outcome %q", conversation.finishCalls, conversation.finishInput.Outcome)
	}
	for _, message := range publicErrors {
		if strings.Contains(message, "主循环") || strings.Contains(message, workspace) {
			t.Fatalf("provider idle public error exposed runtime detail: %q", message)
		}
	}

	ledgerPath := filepath.Join(workspace, ".denova", "runs", "execution-run-1.jsonl")
	raw, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(workspace)) {
		t.Fatal("run ledger persisted the runtime workspace path")
	}
	var finish map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		if record["type"] == "run_finished" {
			finish, _ = record["data"].(map[string]any)
		}
	}
	if finish == nil || finish["status"] != "interrupted" || finish["reason"] != "provider_idle_timeout" {
		t.Fatalf("stable provider idle finish record missing: %#v", finish)
	}

	freshConversation := &resumeOrderingConversation{prepareSucceeds: true}
	freshRunner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &blockingResumeAgent{}})
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		freshRunner,
		freshConversation,
		nil,
		agent.ChatRequest{Message: "开始"},
		agent.RunOptions{TaskID: "fresh-request-1", IdleTimeout: 5 * time.Millisecond},
		func(agent.Event) {},
	)
	if freshConversation.interruptionReason != "provider_idle_timeout" {
		t.Fatalf("fresh interruption reason = %q, want provider_idle_timeout", freshConversation.interruptionReason)
	}
}

func TestRuntimeProviderAndStreamingErrorsUseStableBoundedTermination(t *testing.T) {
	const sentinel = "sk-step9-provider-error-never-reflect-123456789"
	assertStableFailure := func(t *testing.T, event *adk.AgentEvent, idleTimeout time.Duration, want agent.ResumeAttemptFinishOutcome, reason string) {
		t.Helper()
		workspace := t.TempDir()
		conversation := &resumeOrderingConversation{
			pending:         session.Interruption{ID: "interruption-origin-1", Status: session.InterruptionPending},
			prepareSucceeds: true,
		}
		runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{event: event}})
		var public bytes.Buffer
		agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
			context.Background(),
			runner,
			conversation,
			book.NewService(workspace),
			agent.ChatRequest{Message: "继续"},
			agent.RunOptions{TaskID: "resume-request-1", IdleTimeout: idleTimeout},
			func(event agent.Event) {
				fmt.Fprint(&public, event.Type, event.Data)
			},
		)
		if conversation.finishCalls != 1 || conversation.finishInput.Outcome != want {
			t.Fatalf("finish = calls %d outcome %q, want %q", conversation.finishCalls, conversation.finishInput.Outcome, want)
		}
		ledgerPath := filepath.Join(workspace, ".denova", "runs", "execution-run-1.jsonl")
		raw, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		for label, output := range map[string][]byte{"ledger": raw, "events": public.Bytes()} {
			if bytes.Contains(output, []byte(sentinel)) {
				t.Fatalf("%s reflected provider sentinel: %s", label, output)
			}
		}
		if !bytes.Contains(raw, []byte(`"status":"`+map[bool]string{true: "interrupted", false: "failed"}[reason == "provider_idle_timeout"]+`"`)) ||
			!bytes.Contains(raw, []byte(`"reason":"`+reason+`"`)) {
			t.Fatalf("stable finish missing for %q: %s", reason, raw)
		}
	}

	t.Run("ADK event error", func(t *testing.T) {
		assertStableFailure(t, &adk.AgentEvent{Err: errors.New(sentinel)}, 0, agent.ResumeAttemptOutcomeProviderFailed, "provider_error")

		fresh := &resumeOrderingConversation{prepareSucceeds: true}
		runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{event: &adk.AgentEvent{Err: errors.New(sentinel)}}})
		var public bytes.Buffer
		agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
			context.Background(), runner, fresh, nil,
			agent.ChatRequest{Message: "开始"}, agent.RunOptions{TaskID: "fresh-provider-error"},
			func(event agent.Event) { fmt.Fprint(&public, event.Data) },
		)
		if fresh.interruptionReason != "provider_error" || strings.Contains(public.String(), sentinel) {
			t.Fatalf("fresh provider error was not bounded: reason=%q events=%q", fresh.interruptionReason, public.String())
		}
	})

	t.Run("stream error", func(t *testing.T) {
		reader, writer := schema.Pipe[*schema.Message](1)
		writer.Send(nil, errors.New(sentinel))
		writer.Close()
		assertStableFailure(t, &adk.AgentEvent{
			Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
				Role: schema.Assistant, IsStreaming: true, MessageStream: reader,
			}},
		}, 0, agent.ResumeAttemptOutcomeProviderFailed, "provider_error")
	})

	t.Run("stream idle remains idle", func(t *testing.T) {
		reader, writer := schema.Pipe[*schema.Message](1)
		writer.Send(nil, errors.New("Agent 流式响应超过 5ms 没有收到任何输出，已中断本次运行"))
		writer.Close()
		assertStableFailure(t, &adk.AgentEvent{
			Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
				Role: schema.Assistant, IsStreaming: true, MessageStream: reader,
			}},
		}, 0, agent.ResumeAttemptOutcomeProviderIdleTimeout, "provider_idle_timeout")
	})
}

type blockingResumeAgent struct{}

func (*blockingResumeAgent) Name(context.Context) string { return "blocking-resume-agent" }

func (*blockingResumeAgent) Description(context.Context) string { return "blocks until idle timeout" }

func (*blockingResumeAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		<-ctx.Done()
		generator.Close()
	}()
	return iterator
}

func (*blockingResumeAgent) Resume(ctx context.Context, _ *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	return (&blockingResumeAgent{}).Run(ctx, nil)
}

type contextRefSpy struct {
	blobs map[string][]byte
	errs  map[string]error
	calls []string
}

func (s *contextRefSpy) Get(_ context.Context, ref string) ([]byte, error) {
	s.calls = append(s.calls, ref)
	if err := s.errs[ref]; err != nil {
		return nil, err
	}
	value, ok := s.blobs[ref]
	if !ok {
		return nil, errors.New("PRIVATE_CONTEXT_SOURCE_MISSING")
	}
	return append([]byte(nil), value...), nil
}

type contextFixture struct {
	manifest   map[string]any
	request    AuthorizedContextRequest
	source     *contextRefSpy
	sectionRef []string
	content    [][]byte
}

func newContextFixture() *contextFixture {
	bookContent := []byte("书名：雾港收潮人。")
	chapterContent := []byte("铜钟在退潮后的第三个刻度重新响起。")
	bookRef := contextTestRef(bookContent)
	chapterRef := contextTestRef(chapterContent)
	bookSource := map[string]any{
		"schemaVersion": "1",
		"kind":          "book",
		"bookId":        "book-tide-7",
		"targetId":      "context-source-bookMeta-" + strings.Repeat("1", 64),
	}
	chapterSource := map[string]any{
		"schemaVersion": "1",
		"kind":          "chapter",
		"bookId":        "book-tide-7",
		"targetId":      "context-source-currentChapter-" + strings.Repeat("2", 64),
	}
	settingSource := map[string]any{
		"schemaVersion": "1",
		"kind":          "setting",
		"bookId":        "book-tide-7",
		"targetId":      "context-source-settings-" + strings.Repeat("3", 64),
	}
	bookTokens := (len(bookContent) + 2) / 3
	chapterTokens := (len(chapterContent) + 2) / 3
	manifest := map[string]any{
		"schemaVersion": "1",
		"bookId":        "book-tide-7",
		"target": map[string]any{
			"schemaVersion": "1",
			"kind":          "chapter",
			"bookId":        "book-tide-7",
			"targetId":      "chapter-7",
			"parentIds":     []any{"volume-1", "outline-node-7"},
		},
		"capabilityId": "writing.create_artifact",
		"policyId":     "writing.default.v1",
		"sections": []any{
			map[string]any{
				"id":              "context-section-bookMeta-" + strings.Repeat("4", 64),
				"kind":            "book_meta",
				"source":          bookSource,
				"revision":        contextTestRef([]byte("revision-book-meta")),
				"contentHash":     bookRef,
				"contentRef":      bookRef,
				"chars":           len([]rune(string(bookContent))),
				"estimatedTokens": bookTokens,
				"truncated":       false,
				"reasonIncluded":  "requested:bookMeta",
			},
			map[string]any{
				"id":              "context-section-currentChapter-" + strings.Repeat("5", 64),
				"kind":            "chapter_text",
				"source":          chapterSource,
				"revision":        contextTestRef([]byte("revision-current-chapter")),
				"contentHash":     chapterRef,
				"contentRef":      chapterRef,
				"chars":           len([]rune(string(chapterContent))),
				"estimatedTokens": chapterTokens,
				"truncated":       false,
				"reasonIncluded":  "requested:currentChapter",
			},
		},
		"exclusions": []any{
			map[string]any{"source": settingSource, "reason": "not_requested"},
		},
		"budget": map[string]any{
			"maxTokens":            12000,
			"estimatedTokens":      bookTokens + chapterTokens,
			"reservedOutputTokens": 4096,
		},
	}
	fixture := &contextFixture{
		manifest:   manifest,
		sectionRef: []string{bookRef, chapterRef},
		content:    [][]byte{bookContent, chapterContent},
		source: &contextRefSpy{
			blobs: map[string][]byte{
				bookRef:    append([]byte(nil), bookContent...),
				chapterRef: append([]byte(nil), chapterContent...),
			},
			errs: map[string]error{},
		},
		request: AuthorizedContextRequest{
			BookID: "book-tide-7",
			Target: ContextTargetRef{
				SchemaVersion: "1",
				Kind:          "chapter",
				BookID:        "book-tide-7",
				TargetID:      "chapter-7",
				ParentIDs:     []string{"volume-1", "outline-node-7"},
			},
		},
	}
	fixture.installManifest(contextTestCanonicalJSON(manifest))
	return fixture
}

func (f *contextFixture) installManifest(data []byte) {
	ref := contextTestRef(data)
	f.request.ContextPackRef = ref
	f.source.blobs[ref] = append([]byte(nil), data...)
}

func (f *contextFixture) cloneManifest() map[string]any {
	raw, err := json.Marshal(f.manifest)
	if err != nil {
		panic(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var clone map[string]any
	if err := decoder.Decode(&clone); err != nil {
		panic(err)
	}
	return clone
}

func (f *contextFixture) installFirstSectionContent(content []byte) {
	manifest := f.cloneManifest()
	section := manifest["sections"].([]any)[0].(map[string]any)
	oldTokens, _ := section["estimatedTokens"].(json.Number).Int64()
	ref := contextTestRef(content)
	newTokens := (len(content) + 2) / 3
	section["contentRef"], section["contentHash"] = ref, ref
	section["chars"] = json.Number(fmt.Sprint(len([]rune(string(content)))))
	section["estimatedTokens"] = json.Number(fmt.Sprint(newTokens))
	budget := manifest["budget"].(map[string]any)
	total, _ := budget["estimatedTokens"].(json.Number).Int64()
	budget["estimatedTokens"] = json.Number(fmt.Sprint(total - oldTokens + int64(newTokens)))
	f.source.blobs[ref] = append([]byte(nil), content...)
	f.sectionRef[0] = ref
	f.installManifest(contextTestCanonicalJSON(manifest))
}

func contextTestRef(value []byte) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("sha256:%x", digest[:])
}

func contextTestCanonicalJSON(value any) []byte {
	var output bytes.Buffer
	var encode func(any)
	encode = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			output.WriteByte('{')
			for index, key := range keys {
				if index > 0 {
					output.WriteByte(',')
				}
				encodedKey, _ := json.Marshal(key)
				output.Write(encodedKey)
				output.WriteByte(':')
				encode(typed[key])
			}
			output.WriteByte('}')
		case []any:
			output.WriteByte('[')
			for index, child := range typed {
				if index > 0 {
					output.WriteByte(',')
				}
				encode(child)
			}
			output.WriteByte(']')
		default:
			encoded, err := json.Marshal(typed)
			if err != nil {
				panic(err)
			}
			output.Write(encoded)
		}
	}
	encode(value)
	return output.Bytes()
}

func assertContextLoadFailsClosed(t *testing.T, source ContentRefSource, request AuthorizedContextRequest, sentinels ...string) {
	t.Helper()
	loaded, err := LoadAuthorizedContext(context.Background(), source, request)
	if err == nil {
		t.Fatal("context load unexpectedly succeeded")
	}
	if err.Error() != ContextLoadErrorCode {
		t.Fatalf("context load error = %q, want %q", err, ContextLoadErrorCode)
	}
	if !reflect.DeepEqual(loaded, AuthorizedContext{}) {
		t.Fatalf("context load returned partial data: %#v", loaded)
	}
	for _, sentinel := range sentinels {
		if sentinel != "" && strings.Contains(err.Error(), sentinel) {
			t.Fatalf("context load reflected sentinel %q: %v", sentinel, err)
		}
	}
}

func TestContextAdapterLoadsExactCanonicalManifestAndOrderedSectionBlobs(t *testing.T) {
	fixture := newContextFixture()
	loaded, err := LoadAuthorizedContext(context.Background(), fixture.source, fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContextPackRef != fixture.request.ContextPackRef || loaded.Manifest.BookID != fixture.request.BookID {
		t.Fatalf("loaded identity mismatch: %#v", loaded)
	}
	if !reflect.DeepEqual(loaded.Manifest.Target, fixture.request.Target) {
		t.Fatalf("loaded target = %#v, want %#v", loaded.Manifest.Target, fixture.request.Target)
	}
	if len(loaded.Sections) != 2 || loaded.Sections[0].Content != string(fixture.content[0]) || loaded.Sections[1].Content != string(fixture.content[1]) {
		t.Fatalf("loaded ordered content mismatch: %#v", loaded.Sections)
	}
	wantCalls := append([]string{fixture.request.ContextPackRef}, fixture.sectionRef...)
	if !reflect.DeepEqual(fixture.source.calls, wantCalls) {
		t.Fatalf("content refs read = %#v, want %#v", fixture.source.calls, wantCalls)
	}
	if len(loaded.Manifest.Exclusions) != 1 || loaded.Manifest.Exclusions[0].Reason != ContextExclusionNotRequested {
		t.Fatalf("exclusions not preserved: %#v", loaded.Manifest.Exclusions)
	}

	fixture.source.blobs[fixture.sectionRef[0]][0] = 'X'
	loaded.Manifest.Target.ParentIDs[0] = "mutated-parent"
	loaded.Sections[0].Metadata.Source.TargetID = "mutated-source"
	if loaded.Sections[0].Content != string(fixture.content[0]) || fixture.request.Target.ParentIDs[0] != "volume-1" {
		t.Fatal("authorized context was not detached by Go copy semantics")
	}
}

func TestRuntimeContextReceiptMatchesExactLoadedManifestWithoutContent(t *testing.T) {
	fixture := newContextFixture()
	loaded, err := LoadAuthorizedContext(context.Background(), fixture.source, fixture.request)
	if err != nil {
		t.Fatal(err)
	}

	receipt := loaded.Receipt()
	if receipt.SchemaVersion != "1" || receipt.ContextPackRef != fixture.request.ContextPackRef || receipt.BookID != fixture.request.BookID {
		t.Fatalf("receipt identity mismatch: %#v", receipt)
	}
	if !reflect.DeepEqual(receipt.Target, loaded.Manifest.Target) || !reflect.DeepEqual(receipt.Sections, loaded.Manifest.Sections) || !reflect.DeepEqual(receipt.Exclusions, loaded.Manifest.Exclusions) || receipt.Budget != loaded.Manifest.Budget {
		t.Fatalf("receipt did not preserve exact public manifest projection: %#v", receipt)
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{string(fixture.content[0]), string(fixture.content[1]), `"content"`, "bookRoot", "workspace", "/Users/"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("receipt exposed forbidden value %q: %s", forbidden, encoded)
		}
	}

	receipt.Target.ParentIDs[0] = "changed-parent"
	receipt.Sections[0].Source.TargetID = "changed-source"
	if loaded.Manifest.Target.ParentIDs[0] != "volume-1" || strings.Contains(loaded.Sections[0].Metadata.Source.TargetID, "changed") {
		t.Fatal("receipt shares mutable authority state with loaded context")
	}
}

func TestRuntimeCompactionReceiptSurvivesRestartWithoutReplacingTranscript(t *testing.T) {
	root := t.TempDir()
	store, err := session.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("receipt-session")
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []*schema.Message{
		schema.UserMessage("旧用户目标：守住潮票。"),
		schema.AssistantMessage("旧助手结果：已确认钟塔线索。", nil),
		schema.UserMessage("最近用户回合：沿石阶下行。"),
		schema.AssistantMessage("最近助手回合：潮声逼近。", nil),
	} {
		if err := sess.Append(message); err != nil {
			t.Fatal(err)
		}
	}
	const summary = "压缩摘要：目标、约束和钟塔线索保持有效。"
	if _, err := sess.AppendContextCompaction(session.ContextCompaction{
		AgentKind:          config.AgentKindIDE,
		Epoch:              4,
		Summary:            summary,
		SourceStartIndex:   0,
		SourceEndIndex:     2,
		SourceMessageCount: 2,
		RetainedTurns:      1,
		TokensBefore:       6400,
		TokensAfter:        1700,
		Phase:              "pre_run",
	}); err != nil {
		t.Fatal(err)
	}

	newConversation := func(current *session.Session) *agent.SessionConversation {
		return agent.NewSessionConversationForAgentWithRuntimeContexts(
			current,
			&config.Config{},
			config.AgentKindIDE,
			"稳定作品上下文",
			"书名：雾港收潮人；正式纲要 revision=42。",
			"本轮动态上下文",
			"仅用于这一轮。",
		)
	}
	first, ok := newConversation(sess).LatestContextCompactionReceipt()
	if !ok {
		t.Fatal("latest compaction receipt missing")
	}
	if first.SchemaVersion != "1" || first.Epoch != 4 || first.Phase != "pre_run" || first.RetainedRecentTurns != 1 {
		t.Fatalf("compaction receipt identity mismatch: %#v", first)
	}
	if first.CompressedMessageRange.StartIndex != 0 || first.CompressedMessageRange.EndIndex != 2 || first.CompressedMessageRange.Count != 2 {
		t.Fatalf("compressed range mismatch: %#v", first.CompressedMessageRange)
	}
	if first.TokensBefore != 6400 || first.TokensAfter != 1700 {
		t.Fatalf("token estimates mismatch: %#v", first)
	}
	if first.SummaryArtifactRef != contextTestRef([]byte(summary)) {
		t.Fatalf("summary artifact ref = %q", first.SummaryArtifactRef)
	}
	for name, value := range map[string]string{
		"stable context": first.StableContextHash,
		"message range":  first.CompressedMessageRange.Hash,
		"receipt":        first.Hash,
	} {
		if !contextSHA256Ref.MatchString(value) {
			t.Fatalf("%s hash = %q", name, value)
		}
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(summary)) || bytes.Contains(encoded, []byte("旧用户目标")) || bytes.Contains(encoded, []byte(`"summary":`)) || bytes.Contains(encoded, []byte(`"transcript":`)) {
		t.Fatalf("bounded compaction receipt exposed summary or transcript: %s", encoded)
	}

	restartedStore, err := session.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	restartedSession, err := restartedStore.Get("receipt-session")
	if err != nil {
		t.Fatal(err)
	}
	restarted, ok := newConversation(restartedSession).LatestContextCompactionReceipt()
	if !ok || !reflect.DeepEqual(restarted, first) {
		t.Fatalf("compaction receipt changed after restart: %#v want %#v", restarted, first)
	}
	history := restartedSession.History()
	if len(history) != 4 || history[0].Content != "旧用户目标：守住潮票。" || history[1].Content != "旧助手结果：已确认钟塔线索。" {
		t.Fatalf("compaction replaced the product transcript: %#v", history)
	}
}

func TestRuntimeCompactionReceiptRejectsValuesOutsideMainSafeIntegerContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*session.ContextCompaction)
	}{
		{name: "epoch", mutate: func(record *session.ContextCompaction) { record.Epoch = 1_000_000_001 }},
		{name: "retained turns", mutate: func(record *session.ContextCompaction) { record.RetainedTurns = 1_000_000_001 }},
		{name: "tokens before", mutate: func(record *session.ContextCompaction) { record.TokensBefore = 1_000_000_001 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := session.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			sess, err := store.GetOrCreate("bounded-receipt")
			if err != nil {
				t.Fatal(err)
			}
			if err := sess.Append(schema.UserMessage("bounded source")); err != nil {
				t.Fatal(err)
			}
			record := session.ContextCompaction{
				AgentKind:          config.AgentKindIDE,
				Epoch:              1,
				Summary:            "bounded summary",
				SourceStartIndex:   0,
				SourceEndIndex:     1,
				SourceMessageCount: 1,
				RetainedTurns:      1,
				TokensBefore:       100,
				TokensAfter:        40,
				Phase:              "pre_run",
			}
			test.mutate(&record)
			if _, err := sess.AppendContextCompaction(record); err != nil {
				t.Fatal(err)
			}
			conversation := agent.NewSessionConversationForAgent(sess, &config.Config{}, config.AgentKindIDE)
			if receipt, ok := conversation.LatestContextCompactionReceipt(); ok {
				t.Fatalf("out-of-contract %s produced receipt: %#v", test.name, receipt)
			}
		})
	}
}

func TestRuntimeRunLedgerRecordsBoundedContextReceiptBeforeAndAfterRun(t *testing.T) {
	workspace := t.TempDir()
	conversationRoot := t.TempDir()
	store, err := session.NewStore(conversationRoot)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("ledger-context-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.UserMessage("历史用户请求")); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.AssistantMessage("历史助手结果", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendContextCompaction(session.ContextCompaction{
		AgentKind:          config.AgentKindIDE,
		Epoch:              2,
		Summary:            "已有摘要不得进入 run ledger",
		SourceStartIndex:   0,
		SourceEndIndex:     2,
		SourceMessageCount: 2,
		RetainedTurns:      1,
		TokensBefore:       3000,
		TokensAfter:        900,
		Phase:              "pre_run",
	}); err != nil {
		t.Fatal(err)
	}
	conversation := agent.NewSessionConversationForAgentWithRuntimeContexts(
		sess,
		&config.Config{},
		config.AgentKindIDE,
		"稳定作品上下文",
		"正式设定 hash-only receipt source",
		"本轮动态上下文",
		"只在模型输入中可见",
	)
	runnerAgent := &resumeTestAgent{event: &adk.AgentEvent{
		AgentName: "context-receipt-agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Role:    schema.Assistant,
			Message: schema.AssistantMessage("本轮完成", nil),
		}},
	}}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})
	agent.SetTraceRuntimeConfig(agent.TraceCaptureSummary, agent.TraceExporterLocal, 100)
	defer agent.SetTraceRuntimeConfig(agent.TraceCaptureSummary, agent.TraceExporterLocal, 100)
	var runID string
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		book.NewService(workspace),
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "context-receipt-run", SessionID: "ledger-context-session", AgentKind: config.AgentKindIDE},
		func(event agent.Event) {
			data, _ := event.Data.(map[string]string)
			if event.Type == "run_state" && data["phase"] == "started" {
				runID = data["run_id"]
			}
		},
	)
	if runID == "" {
		t.Fatal("run id missing")
	}
	trace, err := agent.ReadRunTrace(workspace, runID)
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	for _, record := range trace.Records {
		if record.Type != "context_receipt" {
			continue
		}
		phase, _ := record.Data["phase"].(string)
		phases = append(phases, phase)
		encoded, _ := json.Marshal(record)
		for _, forbidden := range []string{"已有摘要不得进入 run ledger", "历史用户请求", "本轮动态上下文", `"preview"`} {
			if bytes.Contains(encoded, []byte(forbidden)) {
				t.Fatalf("context receipt ledger exposed %q: %s", forbidden, encoded)
			}
		}
	}
	if !reflect.DeepEqual(phases, []string{"before_run", "after_run"}) {
		t.Fatalf("context receipt phases = %#v", phases)
	}
}

func TestRuntimeCompactionEventsExposeArtifactRefsInsteadOfSummaryOrDeltaBodies(t *testing.T) {
	const summarySentinel = "RAW_COMPACTION_SUMMARY_SENTINEL"
	const deltaSentinel = "RAW_COMPACTION_DELTA_SENTINEL"
	const unknownSentinel = "RAW_COMPACTION_UNKNOWN_SENTINEL"
	conversation := &compactionEventConversation{summary: summarySentinel, delta: deltaSentinel, unknown: unknownSentinel}
	runnerAgent := &resumeTestAgent{event: &adk.AgentEvent{
		AgentName: "bounded-compaction-event-agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Role:    schema.Assistant,
			Message: schema.AssistantMessage("完成", nil),
		}},
	}}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: runnerAgent})
	var received []agent.Event
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "压缩上下文"},
		agent.RunOptions{TaskID: "bounded-compaction-event"},
		func(event agent.Event) { received = append(received, event) },
	)

	var bounded map[string]any
	for _, event := range received {
		if event.Type != "context_compaction" {
			continue
		}
		data, _ := event.Data.(map[string]any)
		encoded, _ := json.Marshal(data)
		if bytes.Contains(encoded, []byte(summarySentinel)) || bytes.Contains(encoded, []byte(deltaSentinel)) || bytes.Contains(encoded, []byte(unknownSentinel)) {
			t.Fatalf("raw compaction body reached ordinary event projection: %s", encoded)
		}
		if _, exists := data["summary"]; exists {
			t.Fatalf("summary field survived ordinary projection: %#v", data)
		}
		if _, exists := data["delta"]; exists {
			t.Fatalf("delta field survived ordinary projection: %#v", data)
		}
		if _, exists := data["raw"]; exists {
			t.Fatalf("unknown raw field survived ordinary projection: %#v", data)
		}
		bounded = data
	}
	if bounded == nil {
		t.Fatal("bounded compaction event missing")
	}
	if bounded["summary_artifact_ref"] != contextTestRef([]byte(summarySentinel)) || bounded["delta_hash"] != contextTestRef([]byte(deltaSentinel)) {
		t.Fatalf("compaction refs = %#v", bounded)
	}
	if bounded["summary_chars"] != len([]rune(summarySentinel)) || bounded["delta_chars"] != len([]rune(deltaSentinel)) {
		t.Fatalf("compaction sizes = %#v", bounded)
	}
}

func TestRuntimeRunLedgerAndOrdinaryLogsExcludeRawRequestContextToolAndStderr(t *testing.T) {
	const requestSentinel = "WP3_RAW_REQUEST_MUST_ONLY_EXIST_IN_EXPLICIT_DIAGNOSTICS"
	const stableContextSentinel = "WP3_RAW_STABLE_CONTEXT_MUST_ONLY_EXIST_IN_EXPLICIT_DIAGNOSTICS"
	const dynamicContextSentinel = "WP3_RAW_DYNAMIC_CONTEXT_MUST_ONLY_EXIST_IN_EXPLICIT_DIAGNOSTICS"
	const toolStderrSentinel = "WP3_RAW_TOOL_ERROR_STDERR_MUST_ONLY_EXIST_IN_EXPLICIT_DIAGNOSTICS"

	oldLogger := slog.Default()
	oldLogWriter := log.Writer()
	oldLogFlags := log.Flags()
	oldLogPrefix := log.Prefix()
	var ordinaryLogs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&ordinaryLogs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(&ordinaryLogs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
		log.SetOutput(oldLogWriter)
		log.SetFlags(oldLogFlags)
		log.SetPrefix(oldLogPrefix)
	})

	workspace := t.TempDir()
	conversationRoot := t.TempDir()
	store, err := session.NewStore(conversationRoot)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("bounded-ledger-session")
	if err != nil {
		t.Fatal(err)
	}
	conversation := agent.NewSessionConversationForAgentWithRuntimeContexts(
		sess,
		&config.Config{},
		config.AgentKindIDE,
		"stable-context",
		stableContextSentinel,
		"dynamic-context",
		dynamicContextSentinel,
	)
	toolMessage := schema.ToolMessage(
		"error: "+toolStderrSentinel,
		"call-wp3-bounded-ledger",
		schema.WithToolName("read_file"),
	)
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{event: &adk.AgentEvent{
		AgentName: "bounded-ledger-agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Role:    schema.Tool,
			Message: toolMessage,
		}},
	}}})
	agent.SetTraceRuntimeConfig(agent.TraceCaptureSummary, agent.TraceExporterLocal, 100)
	t.Cleanup(func() {
		agent.SetTraceRuntimeConfig(agent.TraceCaptureSummary, agent.TraceExporterLocal, 100)
	})
	var runID string
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		book.NewService(workspace),
		agent.ChatRequest{
			Message: requestSentinel,
			Selections: []agent.TextSelectionRef{{
				FileName:  "chapter-1",
				StartLine: 1,
				EndLine:   1,
				Content:   requestSentinel,
			}},
		},
		agent.RunOptions{
			TaskID:    "bounded-ledger-run",
			SessionID: "bounded-ledger-session",
			AgentKind: config.AgentKindIDE,
		},
		func(event agent.Event) {
			data, _ := event.Data.(map[string]string)
			if event.Type == "run_state" && data["phase"] == "started" {
				runID = data["run_id"]
			}
		},
	)
	if runID == "" {
		t.Fatal("run id missing")
	}
	ledgerPath := filepath.Join(workspace, ".denova", "runs", runID+".jsonl")
	ledger, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	for label, output := range map[string][]byte{
		"run ledger":    ledger,
		"ordinary logs": ordinaryLogs.Bytes(),
	} {
		for _, forbidden := range []string{
			requestSentinel,
			stableContextSentinel,
			dynamicContextSentinel,
			toolStderrSentinel,
		} {
			if bytes.Contains(output, []byte(forbidden)) {
				t.Fatalf("%s exposed raw diagnostic-only payload %q: %s", label, forbidden, output)
			}
		}
	}
	var assertPreviewRedacted func(any)
	assertPreviewRedacted = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, item := range typed {
				if key == "preview" {
					if preview, _ := item.(string); preview != "" && preview != "[omitted: explicit developer diagnostics only]" {
						t.Fatalf("run ledger retained a raw preview: %#v", item)
					}
				}
				assertPreviewRedacted(item)
			}
		case []any:
			for _, item := range typed {
				assertPreviewRedacted(item)
			}
		}
	}
	for _, line := range bytes.Split(bytes.TrimSpace(ledger), []byte{'\n'}) {
		var record any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		assertPreviewRedacted(record)
	}
	for _, required := range [][]byte{[]byte(`"bytes"`), []byte(`"chars"`), []byte(`"hash"`), []byte(`"event_type":"tool_result"`)} {
		if !bytes.Contains(ledger, required) {
			t.Fatalf("run ledger lost bounded metadata %q: %s", required, ledger)
		}
	}
}

func TestRuntimeFailurePayloadIsBoundedAcrossLedgerLogsAndPublicEvents(t *testing.T) {
	const sentinel = "sk-WP3FailurePayloadMustStayInDiagnosticsOnly-7x9q"
	const privatePath = "/Users/test/作品/正文/第一章.txt"

	oldLogger := slog.Default()
	oldLogWriter := log.Writer()
	var ordinaryLogs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&ordinaryLogs, nil)))
	log.SetOutput(&ordinaryLogs)
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
		log.SetOutput(oldLogWriter)
	})

	workspace := t.TempDir()
	conversation := &diagnosticFailureConversation{err: errors.New("prepare failed " + sentinel + " at " + privatePath)}
	var runID string
	var publicEvents bytes.Buffer
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		nil,
		conversation,
		book.NewService(workspace),
		agent.ChatRequest{Message: "safe request"},
		agent.RunOptions{TaskID: "bounded-failure-run", AgentKind: config.AgentKindIDE},
		func(event agent.Event) {
			fmt.Fprint(&publicEvents, event.Type, event.Data)
			data, _ := event.Data.(map[string]string)
			if event.Type == "run_state" && data["phase"] == "started" {
				runID = data["run_id"]
			}
		},
	)
	if runID == "" {
		t.Fatal("run id missing")
	}
	ledger, err := os.ReadFile(filepath.Join(workspace, ".denova", "runs", runID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for label, output := range map[string][]byte{
		"run ledger":    ledger,
		"ordinary logs": ordinaryLogs.Bytes(),
		"public events": publicEvents.Bytes(),
	} {
		for _, forbidden := range []string{sentinel, privatePath} {
			if bytes.Contains(output, []byte(forbidden)) {
				t.Fatalf("%s exposed private failure payload %q: %s", label, forbidden, output)
			}
		}
	}
	if !bytes.Contains(ledger, []byte(`"reason":"prepare_messages_failed"`)) {
		t.Fatalf("run ledger did not retain the stable failure taxonomy: %s", ledger)
	}
}

func TestRuntimeOperationFailureTaxonomyNeverReflectsPrivateErrorBodies(t *testing.T) {
	const sentinel = "sk-WP3OperationFailureMustStayInDiagnosticsOnly-7x9q"
	const privatePath = "/Users/test/作品/runtime-private.txt"
	rawErr := errors.New("operation failed " + sentinel + " at " + privatePath)
	tests := []struct {
		name       string
		wantReason string
		configure  func(*resumeOrderingConversation, *agent.RunOptions) *adk.Runner
	}{
		{
			name:       "user message commit",
			wantReason: "user_message_commit_failed",
			configure: func(conversation *resumeOrderingConversation, options *agent.RunOptions) *adk.Runner {
				conversation.prepareSucceeds = true
				options.OnUserMessageCommitted = func(context.Context) error { return rawErr }
				return nil
			},
		},
		{
			name:       "context compaction",
			wantReason: "context_compaction_failed",
			configure: func(conversation *resumeOrderingConversation, _ *agent.RunOptions) *adk.Runner {
				conversation.prepareSucceeds = true
				conversation.compactionErr = rawErr
				return nil
			},
		},
		{
			name:       "assistant persistence",
			wantReason: "assistant_persistence_failed",
			configure: func(conversation *resumeOrderingConversation, options *agent.RunOptions) *adk.Runner {
				conversation.prepareSucceeds = true
				conversation.appendErr = rawErr
				options.RootAgentName = "resume-test-agent"
				return adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{event: &adk.AgentEvent{
					AgentName: "DenovaAgent",
					Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
						Role:    schema.Assistant,
						Message: schema.AssistantMessage("safe assistant output", nil),
					}},
				}}})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldLogger := slog.Default()
			oldLogWriter := log.Writer()
			var ordinaryLogs bytes.Buffer
			slog.SetDefault(slog.New(slog.NewTextHandler(&ordinaryLogs, nil)))
			log.SetOutput(&ordinaryLogs)
			t.Cleanup(func() {
				slog.SetDefault(oldLogger)
				log.SetOutput(oldLogWriter)
			})

			workspace := t.TempDir()
			conversation := &resumeOrderingConversation{}
			options := agent.RunOptions{TaskID: "operation-failure-run", AgentKind: config.AgentKindIDE}
			runner := tc.configure(conversation, &options)
			var runID string
			var publicEvents bytes.Buffer
			agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
				context.Background(),
				runner,
				conversation,
				book.NewService(workspace),
				agent.ChatRequest{Message: "safe request"},
				options,
				func(event agent.Event) {
					fmt.Fprint(&publicEvents, event.Type, event.Data)
					data, _ := event.Data.(map[string]string)
					if event.Type == "run_state" && data["phase"] == "started" {
						runID = data["run_id"]
					}
				},
			)
			if runID == "" {
				t.Fatal("run id missing")
			}
			ledger, err := os.ReadFile(filepath.Join(workspace, ".denova", "runs", runID+".jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			for label, output := range map[string][]byte{
				"run ledger":    ledger,
				"ordinary logs": ordinaryLogs.Bytes(),
				"public events": publicEvents.Bytes(),
			} {
				for _, forbidden := range []string{sentinel, privatePath} {
					if bytes.Contains(output, []byte(forbidden)) {
						t.Fatalf("%s exposed private operation failure %q: %s", label, forbidden, output)
					}
				}
			}
			if !bytes.Contains(ledger, []byte(`"reason":"`+tc.wantReason+`"`)) {
				t.Fatalf("run ledger reason = %s, want %q", ledger, tc.wantReason)
			}
		})
	}
}

func TestRuntimeToolTargetPathIsMetadataOnlyInLedgerAndOrdinaryLogs(t *testing.T) {
	const sentinel = "sk-WP3ToolPathMustStayInDiagnosticsOnly-7x9q"
	const privatePath = "/Users/test/作品/" + sentinel + ".txt"

	oldLogger := slog.Default()
	oldLogWriter := log.Writer()
	var ordinaryLogs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&ordinaryLogs, nil)))
	log.SetOutput(&ordinaryLogs)
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
		log.SetOutput(oldLogWriter)
	})

	conversation := &resumeOrderingConversation{prepareSucceeds: true}
	toolCall := schema.AssistantMessage("", []schema.ToolCall{{
		ID:   "call-private-tool-path",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "read_file",
			Arguments: `{"path":"` + privatePath + `"}`,
		},
	}})
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{event: &adk.AgentEvent{
		AgentName: "resume-test-agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Role:    schema.Assistant,
			Message: toolCall,
		}},
	}}})
	workspace := t.TempDir()
	var runID string
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		book.NewService(workspace),
		agent.ChatRequest{Message: "safe request"},
		agent.RunOptions{
			TaskID:        "bounded-tool-target-run",
			AgentKind:     config.AgentKindIDE,
			RootAgentName: "resume-test-agent",
		},
		func(event agent.Event) {
			data, _ := event.Data.(map[string]string)
			if event.Type == "run_state" && data["phase"] == "started" {
				runID = data["run_id"]
			}
		},
	)
	if runID == "" {
		t.Fatal("run id missing")
	}
	ledger, err := os.ReadFile(filepath.Join(workspace, ".denova", "runs", runID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for label, output := range map[string][]byte{
		"run ledger":    ledger,
		"ordinary logs": ordinaryLogs.Bytes(),
	} {
		for _, forbidden := range []string{sentinel, privatePath} {
			if bytes.Contains(output, []byte(forbidden)) {
				t.Fatalf("%s exposed a private tool path %q: %s", label, forbidden, output)
			}
		}
	}
	for _, required := range [][]byte{[]byte(`"args":{"bytes"`), []byte(`"target":{"bytes"`)} {
		if !bytes.Contains(ledger, required) {
			t.Fatalf("run ledger lost bounded tool metadata %q: %s", required, ledger)
		}
	}
}

func TestRuntimeSessionDisplayProjectionExcludesRawToolArgsResultsAndPaths(t *testing.T) {
	const argsSentinel = "WP3_DISPLAY_TOOL_ARGS_MUST_NOT_PERSIST"
	const resultSentinel = "WP3_DISPLAY_TOOL_RESULT_MUST_NOT_PERSIST"
	const privatePath = "/Users/test/作品/正文/第一章.txt"
	conversation := &displayCaptureConversation{
		resumeOrderingConversation: resumeOrderingConversation{prepareSucceeds: true},
	}
	toolCall := schema.AssistantMessage("", []schema.ToolCall{{
		ID:   "call-display-boundary",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "read_file",
			Arguments: `{"path":"` + privatePath + `","note":"` + argsSentinel + `"}`,
		},
	}})
	toolResult := schema.ToolMessage(
		resultSentinel+" at "+privatePath,
		"call-display-boundary",
		schema.WithToolName("read_file"),
	)
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{events: []*adk.AgentEvent{
		{
			AgentName: "resume-test-agent",
			Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
				Role:    schema.Assistant,
				Message: toolCall,
			}},
		},
		{
			AgentName: "resume-test-agent",
			Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
				Role:    schema.Tool,
				Message: toolResult,
			}},
		},
	}}})
	var transport bytes.Buffer
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "safe request"},
		agent.RunOptions{TaskID: "display-boundary-run", AgentKind: config.AgentKindIDE, RootAgentName: "resume-test-agent"},
		func(event agent.Event) { _ = json.NewEncoder(&transport).Encode(event) },
	)

	stored, err := json.Marshal(conversation.displayEvents)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{argsSentinel, resultSentinel, privatePath} {
		if bytes.Contains(stored, []byte(forbidden)) {
			t.Fatalf("session display projection exposed raw tool payload %q: %s", forbidden, stored)
		}
	}
	if len(conversation.displayEvents) != 1 || conversation.displayEvents[0].Role != "tool_call" || conversation.displayEvents[0].Name != "read_file" || conversation.displayEvents[0].Status != "success" {
		t.Fatalf("bounded display projection lost reconstructible tool metadata: %#v", conversation.displayEvents)
	}
	for _, required := range []string{argsSentinel, resultSentinel, privatePath} {
		if !strings.Contains(transport.String(), required) {
			t.Fatalf("internal sidecar transport lost raw frame field %q", required)
		}
	}
}

func TestRuntimeDisplayPersistenceErrorsNeverReachOrdinaryLogs(t *testing.T) {
	const sentinel = "sk-WP3DisplayPersistenceErrorMustStayInDiagnosticsOnly-7x9q"
	const privatePath = "/Users/test/作品/display-events.jsonl"

	oldLogger := slog.Default()
	oldLogWriter := log.Writer()
	var ordinaryLogs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&ordinaryLogs, nil)))
	log.SetOutput(&ordinaryLogs)
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
		log.SetOutput(oldLogWriter)
	})

	conversation := &displayFailureConversation{
		resumeOrderingConversation: resumeOrderingConversation{prepareSucceeds: true},
		err:                        errors.New("display store failed " + sentinel + " at " + privatePath),
	}
	message := schema.AssistantMessage("", []schema.ToolCall{{
		ID:       "call-display-failure",
		Type:     "function",
		Function: schema.FunctionCall{Name: "read_file", Arguments: `{}`},
	}})
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{event: &adk.AgentEvent{
		AgentName: "resume-test-agent",
		Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Role:    schema.Assistant,
			Message: message,
		}},
	}}})
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		nil,
		agent.ChatRequest{Message: "safe request"},
		agent.RunOptions{TaskID: "display-failure-run", AgentKind: config.AgentKindIDE, RootAgentName: "resume-test-agent"},
		func(agent.Event) {},
	)
	for _, forbidden := range []string{sentinel, privatePath} {
		if strings.Contains(ordinaryLogs.String(), forbidden) {
			t.Fatalf("ordinary logs exposed display persistence error %q: %s", forbidden, ordinaryLogs.String())
		}
	}
}

func TestRuntimeAuxiliaryPersistenceErrorsNeverReachOrdinaryLogs(t *testing.T) {
	const sentinel = "sk-WP3AuxiliaryPersistenceErrorMustStayInDiagnosticsOnly-7x9q"
	const privatePath = "/Users/test/作品/runtime-auxiliary.jsonl"
	rawErr := errors.New("auxiliary persistence failed " + sentinel + " at " + privatePath)
	tests := []struct {
		name         string
		conversation agent.Conversation
		runner       *adk.Runner
	}{
		{
			name: "interruption",
			conversation: &interruptionFailureConversation{
				resumeOrderingConversation: resumeOrderingConversation{prepareSucceeds: true},
				err:                        rawErr,
			},
			runner: adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{event: &adk.AgentEvent{Err: errors.New("provider failed")}}}),
		},
		{
			name: "retained tool context",
			conversation: &toolContextFailureConversation{
				resumeOrderingConversation: resumeOrderingConversation{prepareSucceeds: true},
				err:                        rawErr,
			},
			runner: adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{event: &adk.AgentEvent{
				AgentName: "resume-test-agent",
				Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
					Role: schema.Assistant,
					Message: schema.AssistantMessage("", []schema.ToolCall{{
						ID:       "call-context-persist",
						Type:     "function",
						Function: schema.FunctionCall{Name: "read_file", Arguments: `{}`},
					}}),
				}},
			}}}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldLogger := slog.Default()
			oldLogWriter := log.Writer()
			var ordinaryLogs bytes.Buffer
			slog.SetDefault(slog.New(slog.NewTextHandler(&ordinaryLogs, nil)))
			log.SetOutput(&ordinaryLogs)
			t.Cleanup(func() {
				slog.SetDefault(oldLogger)
				log.SetOutput(oldLogWriter)
			})

			agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
				context.Background(),
				tc.runner,
				tc.conversation,
				nil,
				agent.ChatRequest{Message: "safe request"},
				agent.RunOptions{TaskID: "auxiliary-persistence-run", AgentKind: config.AgentKindIDE, RootAgentName: "resume-test-agent"},
				func(agent.Event) {},
			)
			for _, forbidden := range []string{sentinel, privatePath} {
				if strings.Contains(ordinaryLogs.String(), forbidden) {
					t.Fatalf("ordinary logs exposed auxiliary persistence error %q: %s", forbidden, ordinaryLogs.String())
				}
			}
		})
	}
}

func TestRuntimeResumeFinalizationErrorsNeverReachOrdinaryLogs(t *testing.T) {
	const sentinel = "sk-WP3ResumeFinishErrorMustStayInDiagnosticsOnly-7x9q"
	const privatePath = "/Users/test/作品/resume-attempt.jsonl"

	oldLogger := slog.Default()
	oldLogWriter := log.Writer()
	var ordinaryLogs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&ordinaryLogs, nil)))
	log.SetOutput(&ordinaryLogs)
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
		log.SetOutput(oldLogWriter)
	})

	conversation := &resumeOrderingConversation{
		pending:   session.Interruption{ID: "interruption-origin-1", Status: session.InterruptionPending},
		finishErr: errors.New("resume finalization failed " + sentinel + " at " + privatePath),
	}
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		nil,
		conversation,
		book.NewService(t.TempDir()),
		agent.ChatRequest{Message: "继续"},
		agent.RunOptions{TaskID: "resume-finalization-failure", AgentKind: config.AgentKindIDE},
		func(agent.Event) {},
	)
	for _, forbidden := range []string{sentinel, privatePath} {
		if strings.Contains(ordinaryLogs.String(), forbidden) {
			t.Fatalf("ordinary logs exposed resume finalization error %q: %s", forbidden, ordinaryLogs.String())
		}
	}
}

func TestRuntimeRunLedgerRejectsCredentialMaterialInMetadataFields(t *testing.T) {
	const sentinel = "sk-WP3MetadataCredentialMustStayInDiagnosticsOnly-7x9q"
	oldLogger := slog.Default()
	oldLogWriter := log.Writer()
	var ordinaryLogs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&ordinaryLogs, nil)))
	log.SetOutput(&ordinaryLogs)
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
		log.SetOutput(oldLogWriter)
	})

	workspace := t.TempDir()
	conversation := &resumeOrderingConversation{prepareSucceeds: true}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{}})
	var runID string
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		book.NewService(workspace),
		agent.ChatRequest{Message: "safe request", WritingSkill: sentinel},
		agent.RunOptions{TaskID: "metadata-credential-run", AgentKind: config.AgentKindIDE},
		func(event agent.Event) {
			data, _ := event.Data.(map[string]string)
			if event.Type == "run_state" && data["phase"] == "started" {
				runID = data["run_id"]
			}
		},
	)
	if runID == "" {
		t.Fatal("run id missing")
	}
	ledger, err := os.ReadFile(filepath.Join(workspace, ".denova", "runs", runID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ledger, []byte(sentinel)) {
		t.Fatalf("run ledger exposed credential-shaped metadata: %s", ledger)
	}
	if bytes.Contains(ordinaryLogs.Bytes(), []byte(sentinel)) {
		t.Fatalf("ordinary logs exposed credential-shaped metadata: %s", ordinaryLogs.Bytes())
	}
	if !bytes.Contains(ledger, []byte(`"writing_skill":{"bytes"`)) {
		t.Fatalf("run ledger did not replace sensitive metadata with a bounded summary: %s", ledger)
	}
}

func TestRuntimeRunLedgerSummarizesFilesystemLikeValuesUnderMetadataFieldNames(t *testing.T) {
	const privatePath = "/Users/test/作品/skills/private-writing-skill.md"
	workspace := t.TempDir()
	conversation := &resumeOrderingConversation{prepareSucceeds: true}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{}})
	var runID string
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		book.NewService(workspace),
		agent.ChatRequest{Message: "safe request", WritingSkill: privatePath},
		agent.RunOptions{TaskID: "metadata-path-run", AgentKind: config.AgentKindIDE},
		func(event agent.Event) {
			data, _ := event.Data.(map[string]string)
			if event.Type == "run_state" && data["phase"] == "started" {
				runID = data["run_id"]
			}
		},
	)
	ledger, err := os.ReadFile(filepath.Join(workspace, ".denova", "runs", runID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ledger, []byte(privatePath)) {
		t.Fatalf("run ledger exposed filesystem-like metadata under a nominally safe field: %s", ledger)
	}
	if !bytes.Contains(ledger, []byte(`"writing_skill":{"bytes"`)) {
		t.Fatalf("run ledger did not retain a bounded summary for filesystem-like metadata: %s", ledger)
	}
}

func TestRuntimeRunLedgerCapsEveryEncodedRecord(t *testing.T) {
	const maxRecordBytes = 64 * 1024
	parts := make([]agent.ContextLedgerPart, 256)
	for index := range parts {
		parts[index] = agent.ContextLedgerPart{
			Source:   strings.Repeat("s", 128),
			Purpose:  strings.Repeat("p", 128),
			Bytes:    1,
			Chars:    1,
			Included: true,
		}
	}
	workspace := t.TempDir()
	conversation := &largeLedgerConversation{
		resumeOrderingConversation: resumeOrderingConversation{prepareSucceeds: true},
		parts:                      parts,
	}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{}})
	var runID string
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		book.NewService(workspace),
		agent.ChatRequest{Message: "safe request"},
		agent.RunOptions{TaskID: "record-cap-run", AgentKind: config.AgentKindIDE},
		func(event agent.Event) {
			data, _ := event.Data.(map[string]string)
			if event.Type == "run_state" && data["phase"] == "started" {
				runID = data["run_id"]
			}
		},
	)
	ledger, err := os.ReadFile(filepath.Join(workspace, ".denova", "runs", runID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for index, line := range bytes.Split(bytes.TrimSpace(ledger), []byte{'\n'}) {
		if len(line) > maxRecordBytes {
			t.Fatalf("run ledger record %d has %d bytes, max %d", index, len(line), maxRecordBytes)
		}
	}
	if !bytes.Contains(ledger, []byte(`"omitted":true`)) {
		t.Fatalf("oversized record did not leave a bounded omission receipt: %s", ledger)
	}
}

func TestRuntimeRunLedgerSummarizesUnknownRecordsAndSensitiveParentPaths(t *testing.T) {
	const parentSentinel = "WP3_REQUEST_CHILD_NAME_MUST_NOT_PERSIST"
	const unknownRecordSentinel = "WP3_UNKNOWN_RECORD_SAFE_NAME_MUST_NOT_PERSIST"
	workspace := t.TempDir()
	conversation := &sensitiveTraceConversation{
		resumeOrderingConversation: resumeOrderingConversation{prepareSucceeds: true},
		parentSentinel:             parentSentinel,
		unknownRecordSentinel:      unknownRecordSentinel,
	}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: &resumeTestAgent{}})
	var runID string
	agent.NewRuntime(agent.DefaultLoopPolicy()).Run(
		context.Background(),
		runner,
		conversation,
		book.NewService(workspace),
		agent.ChatRequest{Message: "safe request"},
		agent.RunOptions{TaskID: "sensitive-parent-run", AgentKind: config.AgentKindIDE},
		func(event agent.Event) {
			data, _ := event.Data.(map[string]string)
			if event.Type == "run_state" && data["phase"] == "started" {
				runID = data["run_id"]
			}
		},
	)
	ledger, err := os.ReadFile(filepath.Join(workspace, ".denova", "runs", runID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{parentSentinel, unknownRecordSentinel} {
		if bytes.Contains(ledger, []byte(forbidden)) {
			t.Fatalf("run ledger exposed generic record content %q: %s", forbidden, ledger)
		}
	}
	if !bytes.Contains(ledger, []byte(`"custom_sensitive_trace"`)) || !bytes.Contains(ledger, []byte(`"bytes"`)) {
		t.Fatalf("run ledger lost the bounded custom trace receipt: %s", ledger)
	}
}

type displayFailureConversation struct {
	resumeOrderingConversation
	err error
}

func (c *displayFailureConversation) AppendDisplayEvent(session.DisplayEvent) error {
	return c.err
}

func (c *displayFailureConversation) UpdateDisplayToolStatus(string, string, string) error {
	return c.err
}

type displayCaptureConversation struct {
	resumeOrderingConversation
	displayEvents []session.DisplayEvent
}

type interruptionFailureConversation struct {
	resumeOrderingConversation
	err error
}

func (c *interruptionFailureConversation) MarkInterrupted(string, string, string) error {
	return c.err
}

type toolContextFailureConversation struct {
	resumeOrderingConversation
	err error
}

func (c *toolContextFailureConversation) AppendContextMessage(*schema.Message) error {
	return c.err
}

func (c *toolContextFailureConversation) ToolResultContextPolicy() agent.ToolResultContextPolicy {
	return agent.ToolResultContextPolicy{AgentKind: config.AgentKindIDE, Enabled: true, MaxResultBytes: 4096}
}

type largeLedgerConversation struct {
	resumeOrderingConversation
	parts []agent.ContextLedgerPart
}

func (c *largeLedgerConversation) ContextLedgerParts() []agent.ContextLedgerPart {
	return append([]agent.ContextLedgerPart(nil), c.parts...)
}

type sensitiveTraceConversation struct {
	resumeOrderingConversation
	parentSentinel        string
	unknownRecordSentinel string
}

func (c *sensitiveTraceConversation) CompactContextIfNeeded(ctx context.Context, input agent.ContextCompactionInput) ([]*schema.Message, agent.ContextCompactionResult, error) {
	agent.RecordCompletedTraceSpan(ctx, "custom_sensitive_trace", time.Now(), "success", map[string]any{
		"name": c.unknownRecordSentinel,
		"request": map[string]any{
			"name": c.parentSentinel,
		},
	})
	return input.Messages, agent.ContextCompactionResult{}, nil
}

func (c *displayCaptureConversation) AppendDisplayEvent(event session.DisplayEvent) error {
	c.displayEvents = append(c.displayEvents, event)
	return nil
}

func (c *displayCaptureConversation) UpdateDisplayToolStatus(id, name, status string) error {
	for index := len(c.displayEvents) - 1; index >= 0; index-- {
		if c.displayEvents[index].ID == id || id == "" && c.displayEvents[index].Name == name {
			c.displayEvents[index].Status = status
			return nil
		}
	}
	return nil
}

func (c *displayCaptureConversation) AppendDisplayToolArgs(id, name, delta string) error {
	for index := len(c.displayEvents) - 1; index >= 0; index-- {
		if c.displayEvents[index].ID == id || id == "" && c.displayEvents[index].Name == name {
			c.displayEvents[index].Args += delta
			return nil
		}
	}
	return nil
}

func (c *displayCaptureConversation) UpdateDisplayToolResult(id, name, status, result string) error {
	for index := len(c.displayEvents) - 1; index >= 0; index-- {
		if c.displayEvents[index].ID == id || id == "" && c.displayEvents[index].Name == name {
			c.displayEvents[index].Status = status
			c.displayEvents[index].Result = result
			return nil
		}
	}
	return nil
}

type diagnosticFailureConversation struct {
	err error
}

func (c *diagnosticFailureConversation) PrepareMessages(string, string) ([]*schema.Message, error) {
	return nil, c.err
}

func (*diagnosticFailureConversation) AppendAssistant(string) error { return nil }

func (*diagnosticFailureConversation) MarkInterrupted(string, string, string) error { return nil }

func (*diagnosticFailureConversation) PendingInterruption() *session.Interruption { return nil }

func (*diagnosticFailureConversation) ResolveInterruption(string) error { return nil }

type compactionEventConversation struct {
	summary string
	delta   string
	unknown string
}

func (c *compactionEventConversation) PrepareMessages(string, string) ([]*schema.Message, error) {
	return []*schema.Message{schema.UserMessage("model input")}, nil
}

func (c *compactionEventConversation) CompactContextIfNeeded(_ context.Context, input agent.ContextCompactionInput) ([]*schema.Message, agent.ContextCompactionResult, error) {
	input.Emit(agent.Event{Type: "context_compaction", Data: map[string]any{
		"phase":   "pre_run",
		"status":  "completed",
		"epoch":   1,
		"summary": c.summary,
		"delta":   c.delta,
		"raw":     c.unknown,
	}})
	return input.Messages, agent.ContextCompactionResult{
		Triggered:    true,
		Phase:        "pre_run",
		Epoch:        1,
		TokensBefore: 100,
		TokensAfter:  40,
	}, nil
}

func (c *compactionEventConversation) AppendAssistant(string) error { return nil }
func (c *compactionEventConversation) MarkInterrupted(string, string, string) error {
	return nil
}
func (c *compactionEventConversation) PendingInterruption() *session.Interruption { return nil }
func (c *compactionEventConversation) ResolveInterruption(string) error           { return nil }

func TestContextAdapterCanonicalManifestMatchesProductJSONStringEscaping(t *testing.T) {
	fixture := newContextFixture()
	manifest := fixture.cloneManifest()
	specialTargetID := "chapter-a\u2028b<>&"
	manifest["target"].(map[string]any)["targetId"] = specialTargetID
	fixture.request.Target.TargetID = specialTargetID
	manifestBytes := contextTestCanonicalJSON(manifest)
	for escaped, literal := range map[string]string{
		`\u2028`: "\u2028",
		`\u003c`: "<",
		`\u003e`: ">",
		`\u0026`: "&",
	} {
		manifestBytes = bytes.ReplaceAll(manifestBytes, []byte(escaped), []byte(literal))
	}
	fixture.installManifest(manifestBytes)

	loaded, err := LoadAuthorizedContext(context.Background(), fixture.source, fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manifest.Target.TargetID != specialTargetID {
		t.Fatalf("target id = %q, want exact Product string", loaded.Manifest.Target.TargetID)
	}
}

func TestContextAdapterAcceptsOnlyProductCanonicalNonnegativeIntegerSpellings(t *testing.T) {
	makeFixture := func() (*contextFixture, []byte) {
		fixture := newContextFixture()
		manifest := fixture.cloneManifest()
		section := manifest["sections"].([]any)[0].(map[string]any)
		oldTokens, _ := section["estimatedTokens"].(json.Number).Int64()
		content := []byte("a")
		ref := contextTestRef(content)
		section["contentRef"], section["contentHash"] = ref, ref
		section["chars"], section["estimatedTokens"] = json.Number("1"), json.Number("1")
		budget := manifest["budget"].(map[string]any)
		total, _ := budget["estimatedTokens"].(json.Number).Int64()
		budget["estimatedTokens"] = json.Number(fmt.Sprint(total - oldTokens + 1))
		budget["reservedOutputTokens"] = json.Number("0")
		fixture.source.blobs[ref] = content
		fixture.sectionRef[0] = ref
		return fixture, contextTestCanonicalJSON(manifest)
	}

	invalidSpellings := []struct {
		name string
		from string
		to   string
	}{
		{name: "negative zero", from: `"reservedOutputTokens":0`, to: `"reservedOutputTokens":-0`},
		{name: "fractional integer", from: `"chars":1`, to: `"chars":1.0`},
		{name: "exponent integer", from: `"chars":1`, to: `"chars":1e0`},
		{name: "leading zero", from: `"chars":1`, to: `"chars":01`},
	}
	for _, test := range invalidSpellings {
		t.Run(test.name, func(t *testing.T) {
			fixture, canonical := makeFixture()
			raw := bytes.Replace(canonical, []byte(test.from), []byte(test.to), 1)
			if bytes.Equal(raw, canonical) {
				t.Fatalf("numeric fixture did not replace %q", test.from)
			}
			fixture.installManifest(raw)
			assertContextLoadFailsClosed(t, fixture.source, fixture.request)
			if want := []string{fixture.request.ContextPackRef}; !reflect.DeepEqual(fixture.source.calls, want) {
				t.Fatalf("noncanonical number expanded refs: got %#v want %#v", fixture.source.calls, want)
			}
		})
	}

	fixture, canonical := makeFixture()
	fixture.installManifest(canonical)
	loaded, err := LoadAuthorizedContext(context.Background(), fixture.source, fixture.request)
	if err != nil {
		t.Fatalf("canonical zero and positive decimal integers were rejected: %v", err)
	}
	if loaded.Manifest.Budget.ReservedOutputTokens != 0 || loaded.Sections[0].Metadata.Chars != 1 {
		t.Fatalf("canonical numeric metadata changed: %#v", loaded)
	}
}

func TestContextAdapterPortAndAuthoritySurfaceAreRefOnlyAndPathless(t *testing.T) {
	portType := reflect.TypeOf((*ContentRefSource)(nil)).Elem()
	if portType.NumMethod() != 1 || portType.Method(0).Name != "Get" {
		t.Fatalf("ContentRefSource methods = %#v", portType)
	}
	method := portType.Method(0).Type
	if method.NumIn() != 2 || method.In(0) != reflect.TypeOf((*context.Context)(nil)).Elem() || method.In(1).Kind() != reflect.String || method.NumOut() != 2 {
		t.Fatalf("ContentRefSource.Get signature widened: %v", method)
	}
	if fields := reflect.VisibleFields(reflect.TypeOf(AuthorizedContextRequest{})); len(fields) != 3 || fields[0].Name != "BookID" || fields[1].Name != "Target" || fields[2].Name != "ContextPackRef" {
		t.Fatalf("AuthorizedContextRequest fields widened: %#v", fields)
	}
	wantTargetFields := []string{"SchemaVersion", "Kind", "BookID", "TargetID", "ParentIDs"}
	targetFields := reflect.VisibleFields(reflect.TypeOf(ContextTargetRef{}))
	for index, name := range wantTargetFields {
		if len(targetFields) != len(wantTargetFields) || targetFields[index].Name != name {
			t.Fatalf("ContextTargetRef fields widened: %#v", targetFields)
		}
	}
	wantManifestFields := []string{"SchemaVersion", "BookID", "Target", "CapabilityID", "PolicyID", "Sections", "Exclusions", "Budget"}
	manifestFields := reflect.VisibleFields(reflect.TypeOf(ContextPackManifest{}))
	if len(manifestFields) != len(wantManifestFields) {
		t.Fatalf("ContextPackManifest fields widened: %#v", manifestFields)
	}
	for index, name := range wantManifestFields {
		if manifestFields[index].Name != name {
			t.Fatalf("ContextPackManifest fields widened: %#v", manifestFields)
		}
	}
	wantSectionFields := []string{"ID", "Kind", "Source", "Revision", "ContentHash", "ContentRef", "Chars", "EstimatedTokens", "Truncated", "ReasonIncluded"}
	sectionFields := reflect.VisibleFields(reflect.TypeOf(ContextSection{}))
	if len(sectionFields) != len(wantSectionFields) {
		t.Fatalf("ContextSection fields widened: %#v", sectionFields)
	}
	for index, name := range wantSectionFields {
		if sectionFields[index].Name != name {
			t.Fatalf("ContextSection fields widened: %#v", sectionFields)
		}
	}
	sourceText, err := os.ReadFile("context.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"os.", "filepath.", "ReadDir", "WalkDir", "Glob(", "bookRoot", "workspacePath"} {
		if bytes.Contains(sourceText, []byte(forbidden)) {
			t.Fatalf("context adapter contains filesystem/root entrypoint %q", forbidden)
		}
	}
}

func TestContextAdapterRejectsMalformedManifestBudgetOrderAndAuthoritySubstitution(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*contextFixture)
	}{
		{name: "missing manifest", mutate: func(f *contextFixture) { delete(f.source.blobs, f.request.ContextPackRef) }},
		{name: "manifest ref mismatch", mutate: func(f *contextFixture) {
			wrongRef := contextTestRef([]byte("different manifest"))
			f.source.blobs[wrongRef] = contextTestCanonicalJSON(f.manifest)
			f.request.ContextPackRef = wrongRef
		}},
		{name: "unknown root field", mutate: func(f *contextFixture) {
			manifest := f.cloneManifest()
			manifest["bookRoot"] = "/private/manuscript"
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "null required field", mutate: func(f *contextFixture) {
			manifest := f.cloneManifest()
			manifest["policyId"] = nil
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "explicit empty optional capability", mutate: func(f *contextFixture) {
			manifest := f.cloneManifest()
			manifest["capabilityId"] = ""
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "missing required field", mutate: func(f *contextFixture) {
			manifest := f.cloneManifest()
			delete(manifest, "exclusions")
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "duplicate field", mutate: func(f *contextFixture) {
			manifest := contextTestCanonicalJSON(f.manifest)
			manifest = bytes.Replace(manifest, []byte(`{"bookId":`), []byte(`{"bookId":"book-tide-7","bookId":`), 1)
			f.installManifest(manifest)
		}},
		{name: "trailing json", mutate: func(f *contextFixture) {
			f.installManifest(append(contextTestCanonicalJSON(f.manifest), []byte(` {}`)...))
		}},
		{name: "oversize manifest", mutate: func(f *contextFixture) {
			f.installManifest(bytes.Repeat([]byte("x"), 256*1024+1))
		}},
		{name: "section ref hash mismatch", mutate: func(f *contextFixture) {
			manifest := f.cloneManifest()
			section := manifest["sections"].([]any)[0].(map[string]any)
			section["contentRef"] = contextTestRef([]byte("other content"))
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "section order", mutate: func(f *contextFixture) {
			manifest := f.cloneManifest()
			sections := manifest["sections"].([]any)
			manifest["sections"] = []any{sections[1], sections[0]}
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "duplicate section source", mutate: func(f *contextFixture) {
			manifest := f.cloneManifest()
			sections := manifest["sections"].([]any)
			sections[1].(map[string]any)["source"] = sections[0].(map[string]any)["source"]
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "budget mismatch", mutate: func(f *contextFixture) {
			manifest := f.cloneManifest()
			manifest["budget"].(map[string]any)["estimatedTokens"] = json.Number("1")
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "target substitution", mutate: func(f *contextFixture) {
			f.request.Target.TargetID = "chapter-8"
		}},
		{name: "book substitution", mutate: func(f *contextFixture) {
			f.request.BookID = "book-harbor-9"
			f.request.Target.BookID = "book-harbor-9"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newContextFixture()
			test.mutate(fixture)
			assertContextLoadFailsClosed(t, fixture.source, fixture.request, "PRIVATE", "/private/manuscript")
			if want := []string{fixture.request.ContextPackRef}; !reflect.DeepEqual(fixture.source.calls, want) {
				t.Fatalf("invalid manifest expanded refs: got %#v want %#v", fixture.source.calls, want)
			}
		})
	}
}

func TestContextAdapterRejectsCorruptUTF8OversizeMetadataAndCredentialSectionContent(t *testing.T) {
	tests := []struct {
		name     string
		sentinel string
		mutate   func(*contextFixture)
	}{
		{name: "missing blob", sentinel: "PRIVATE_SECTION_MISSING", mutate: func(f *contextFixture) {
			delete(f.source.blobs, f.sectionRef[0])
			f.source.errs[f.sectionRef[0]] = errors.New("PRIVATE_SECTION_MISSING")
		}},
		{name: "corrupt hash", mutate: func(f *contextFixture) {
			f.source.blobs[f.sectionRef[0]] = []byte("corrupt content")
		}},
		{name: "invalid utf8", mutate: func(f *contextFixture) {
			invalid := []byte{0xff, 0xfe, 0xfd}
			ref := contextTestRef(invalid)
			manifest := f.cloneManifest()
			section := manifest["sections"].([]any)[0].(map[string]any)
			section["contentRef"], section["contentHash"] = ref, ref
			f.source.blobs[ref] = invalid
			f.sectionRef[0] = ref
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "oversize section", mutate: func(f *contextFixture) {
			large := bytes.Repeat([]byte("a"), 128*1024+1)
			ref := contextTestRef(large)
			manifest := f.cloneManifest()
			section := manifest["sections"].([]any)[0].(map[string]any)
			section["contentRef"], section["contentHash"] = ref, ref
			f.source.blobs[ref] = large
			f.sectionRef[0] = ref
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "chars mismatch", mutate: func(f *contextFixture) {
			manifest := f.cloneManifest()
			section := manifest["sections"].([]any)[0].(map[string]any)
			section["chars"] = json.Number(fmt.Sprint(len([]rune(string(f.content[0]))) + 1))
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "token estimator mismatch", mutate: func(f *contextFixture) {
			manifest := f.cloneManifest()
			section := manifest["sections"].([]any)[0].(map[string]any)
			section["estimatedTokens"] = json.Number("1")
			manifest["budget"].(map[string]any)["estimatedTokens"] = json.Number(fmt.Sprint(1 + (len(f.content[1])+2)/3))
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
		{name: "credential shaped content", sentinel: "WP3ContextCredentialSentinelG07", mutate: func(f *contextFixture) {
			secret := []byte("Authorization: Bearer WP3ContextCredentialSentinelG07")
			ref := contextTestRef(secret)
			manifest := f.cloneManifest()
			section := manifest["sections"].([]any)[0].(map[string]any)
			oldTokens, _ := section["estimatedTokens"].(json.Number).Int64()
			newTokens := (len(secret) + 2) / 3
			section["contentRef"], section["contentHash"] = ref, ref
			section["chars"] = json.Number(fmt.Sprint(len([]rune(string(secret)))))
			section["estimatedTokens"] = json.Number(fmt.Sprint(newTokens))
			budget := manifest["budget"].(map[string]any)
			total, _ := budget["estimatedTokens"].(json.Number).Int64()
			budget["estimatedTokens"] = json.Number(fmt.Sprint(total - oldTokens + int64(newTokens)))
			f.source.blobs[ref] = secret
			f.sectionRef[0] = ref
			f.installManifest(contextTestCanonicalJSON(manifest))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newContextFixture()
			test.mutate(fixture)
			assertContextLoadFailsClosed(t, fixture.source, fixture.request, test.sentinel)
			wantCalls := []string{fixture.request.ContextPackRef, fixture.sectionRef[0]}
			if !reflect.DeepEqual(fixture.source.calls, wantCalls) {
				t.Fatalf("failed section expanded refs: got %#v want %#v", fixture.source.calls, wantCalls)
			}
		})
	}
}

func TestContextAdapterRejectsBareAuthorizationSchemesWithoutRejectingOrdinaryProse(t *testing.T) {
	credentials := []struct {
		name  string
		value string
	}{
		{name: "bearer punctuation", value: "Bearer opaque-token-4821"},
		{name: "basic base64", value: "basic QWxhZGRpbjpvcGVuU2VzYW1lPQ=="},
		{name: "bearer mixed case and tab", value: "bEaReR\tOpaque_Abc123456"},
		{name: "basic mixed case and newline", value: "bAsIc \n QWxhZGRpbjpPcGVuU2VzYW1lPQ=="},
		{name: "bearer lowercase alpha", value: "Bearer abcdefgh"},
		{name: "bearer uppercase alpha", value: "Bearer ABCDEFGH"},
		{name: "basic lowercase alpha", value: "Basic aaaaaaaa"},
		{name: "basic uppercase alpha", value: "Basic AAAAAAAA"},
		{name: "bearer lowercase alpha case whitespace", value: "bEaReR \t abcdefgh"},
		{name: "basic uppercase alpha case whitespace", value: "bAsIc\nAAAAAAAA"},
	}
	for _, credential := range credentials {
		t.Run(credential.name, func(t *testing.T) {
			fixture := newContextFixture()
			fixture.installFirstSectionContent([]byte(credential.value))
			assertContextLoadFailsClosed(t, fixture.source, fixture.request, credential.value)
			wantCalls := []string{fixture.request.ContextPackRef, fixture.sectionRef[0]}
			if !reflect.DeepEqual(fixture.source.calls, wantCalls) {
				t.Fatalf("credential rejection expanded refs: got %#v want %#v", fixture.source.calls, wantCalls)
			}
		})
	}

	for _, benign := range []string{
		"The bearer carried a note. Basic prose kept the scene clear.",
		"Bearer alone; basic words remain ordinary.",
		"Bearer -- and Basic !? are not credential tokens.",
	} {
		fixture := newContextFixture()
		fixture.installFirstSectionContent([]byte(benign))
		loaded, err := LoadAuthorizedContext(context.Background(), fixture.source, fixture.request)
		if err != nil {
			t.Fatalf("ordinary prose was rejected: %q: %v", benign, err)
		}
		if loaded.Sections[0].Content != benign {
			t.Fatalf("ordinary prose changed: %q", loaded.Sections[0].Content)
		}
	}
}

func TestContextAdapterEnumsMatchProductAndProtocolKindsRemainExact(t *testing.T) {
	wantKinds := []ContextSectionKind{
		"book_meta", "main_outline", "volume_outline", "chapter_outline", "chapter_text",
		"adjacent_chapter", "character", "setting", "relationship", "timeline", "thread",
		"knowledge", "style", "selection", "review_feedback", "game_fact",
	}
	wantReasons := []ContextExclusionReason{"not_requested", "budget", "stale", "missing", "permission"}
	if got := ContextSectionKinds(); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("ContextSection kinds = %#v, want %#v", got, wantKinds)
	}
	if got := ContextExclusionReasons(); !reflect.DeepEqual(got, wantReasons) {
		t.Fatalf("ContextExclusion reasons = %#v, want %#v", got, wantReasons)
	}
	wantFrames := []string{
		yanzhouprotocol.KindHandshakeRequest, yanzhouprotocol.KindHandshakeResponse,
		yanzhouprotocol.KindRunStart, yanzhouprotocol.KindRunCancel, yanzhouprotocol.KindRunResume,
		yanzhouprotocol.KindRunEvent, yanzhouprotocol.KindToolRequest,
		yanzhouprotocol.KindToolResponse, yanzhouprotocol.KindRuntimeError,
	}
	if yanzhouprotocol.ProtocolVersion != "1.0" || !reflect.DeepEqual(wantFrames, []string{
		"handshake.request", "handshake.response", "run.start", "run.cancel", "run.resume",
		"run.event", "tool.request", "tool.response", "runtime.error",
	}) {
		t.Fatalf("Product protocol widened: version=%q frames=%#v", yanzhouprotocol.ProtocolVersion, wantFrames)
	}
	unknown := yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart + ".context", ProtocolVersion: "1.0",
		RequestID: "request-context-private", Payload: json.RawMessage(`{}`),
	}
	if err := unknown.Validate(); err == nil {
		t.Fatal("private context protocol kind was accepted")
	}
}
