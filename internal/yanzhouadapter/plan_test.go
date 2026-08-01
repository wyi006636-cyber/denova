package yanzhouadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"denova/internal/yanzhouprotocol"
)

func validPlanQuestionGroup() PlanQuestionGroup {
	return PlanQuestionGroup{
		SchemaVersion:          "1",
		ID:                     "group-1",
		Round:                  1,
		Goal:                   "确认范围",
		RemainingUncertainties: []string{"主角欲望"},
		Questions: []PlanQuestion{
			{
				ID: "genre", Topic: "genre", Prompt: "题材", Mode: PlanQuestionSingle,
				Options:              []PlanQuestionOption{{ID: "fantasy", Label: "奇幻"}, {ID: "realism", Label: "现实"}},
				RecommendedOptionIDs: []string{"fantasy"}, AllowCustom: true, Required: true,
			},
			{
				ID: "promise", Topic: "reader_promise", Prompt: "读者承诺", Mode: PlanQuestionMulti,
				Options:     []PlanQuestionOption{{ID: "mystery", Label: "谜团"}, {ID: "growth", Label: "成长"}},
				AllowCustom: true, Required: true,
				DependsOn: []PlanQuestionDependency{{QuestionID: "genre", Answer: "fantasy"}},
			},
			{ID: "conflict", Topic: "conflict", Prompt: "冲突", Mode: PlanQuestionFreeform, AllowCustom: true},
			{
				ID: "priority", Topic: "structure", Prompt: "排序", Mode: PlanQuestionRank,
				Options: []PlanQuestionOption{{ID: "pace", Label: "节奏"}, {ID: "depth", Label: "深度"}}, Required: true,
			},
			{ID: "tone", Topic: "tone", Prompt: "强度", Mode: PlanQuestionScale, Required: true, Scale: &PlanScale{Min: 1, Max: 5, Step: 1}},
		},
	}
}

func TestPlanQuestionGroupAcceptsFiveModesAndDependencies(t *testing.T) {
	group := validPlanQuestionGroup()
	if err := group.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(PlanQuestionTopics()) != 13 || len(PlanQuestionModes()) != 5 {
		t.Fatalf("topics=%d modes=%d", len(PlanQuestionTopics()), len(PlanQuestionModes()))
	}
}

func TestPlanQuestionGroupRejectsUnknownModeDuplicatesAndBadDependency(t *testing.T) {
	tests := map[string]func(*PlanQuestionGroup){
		"mode":               func(group *PlanQuestionGroup) { group.Questions[0].Mode = "checkbox" },
		"question duplicate": func(group *PlanQuestionGroup) { group.Questions[1].ID = "genre" },
		"option duplicate":   func(group *PlanQuestionGroup) { group.Questions[0].Options[1].ID = "fantasy" },
		"dependency":         func(group *PlanQuestionGroup) { group.Questions[1].DependsOn[0].QuestionID = "missing" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			group := validPlanQuestionGroup()
			mutate(&group)
			if err := group.Validate(); err == nil {
				t.Fatal("invalid question group must fail closed")
			}
		})
	}
}

func TestDecodePlanQuestionGroupRejectsUnknownFieldsAndMissingRequiredBooleans(t *testing.T) {
	valid, err := json.Marshal(validPlanQuestionGroup())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePlanQuestionGroup(valid); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"unknown field":       `{"schemaVersion":"1","id":"group-1","round":1,"goal":"范围","questions":[{"id":"q-1","topic":"genre","prompt":"题材","mode":"freeform","allowCustom":true,"required":true,"unexpected":true}],"remainingUncertainties":[]}`,
		"missing allowCustom": `{"schemaVersion":"1","id":"group-1","round":1,"goal":"范围","questions":[{"id":"q-1","topic":"genre","prompt":"题材","mode":"freeform","required":true}],"remainingUncertainties":[]}`,
		"missing required":    `{"schemaVersion":"1","id":"group-1","round":1,"goal":"范围","questions":[{"id":"q-1","topic":"genre","prompt":"题材","mode":"freeform","allowCustom":true}],"remainingUncertainties":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodePlanQuestionGroup([]byte(raw)); err == nil {
				t.Fatal("invalid question group JSON must fail closed")
			}
		})
	}
}

func TestPlanAnswersValidateModesDependenciesAndCustomValues(t *testing.T) {
	group := validPlanQuestionGroup()
	valid := map[string]any{
		"genre": "fantasy", "promise": []any{"mystery", "growth"},
		"conflict": "失踪案迫使主角违背禁忌", "priority": []any{"pace", "depth"}, "tone": float64(3),
	}
	if err := validatePlanAnswers(group, valid); err != nil {
		t.Fatal(err)
	}

	group.Questions[0].AllowCustom = false
	for name, mutate := range map[string]func(map[string]any){
		"single": func(answers map[string]any) { answers["genre"] = "unknown" },
		"multi":  func(answers map[string]any) { answers["promise"] = []any{"mystery", "mystery"} },
		"rank":   func(answers map[string]any) { answers["priority"] = []any{"pace"} },
		"scale":  func(answers map[string]any) { answers["tone"] = float64(6) },
	} {
		t.Run(name, func(t *testing.T) {
			answers := clonePlanAnswers(valid)
			mutate(answers)
			if err := validatePlanAnswers(group, answers); err == nil {
				t.Fatal("invalid mode-specific answer must fail closed")
			}
		})
	}

	inactive := clonePlanAnswers(valid)
	inactive["genre"] = "realism"
	if err := validatePlanAnswers(group, inactive); err == nil {
		t.Fatal("answer for an inactive dependent question must be rejected")
	}
	delete(inactive, "promise")
	if err := validatePlanAnswers(group, inactive); err != nil {
		t.Fatalf("inactive required question must not be required: %v", err)
	}
}

func clonePlanAnswers(value map[string]any) map[string]any {
	encoded, _ := json.Marshal(value)
	var cloned map[string]any
	_ = json.Unmarshal(encoded, &cloned)
	return cloned
}

func TestPlanResumeValidatesExclusiveAnswerSkipAndRecommendedModes(t *testing.T) {
	for name, resume := range map[string]planRunResume{
		"skip": {
			SchemaVersion: "1", RunID: "run-1", GroupID: "group-1", Skip: true,
		},
		"recommended": {
			SchemaVersion: "1", RunID: "run-1", GroupID: "group-1", UseRecommended: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := resume.Validate(); err != nil {
				t.Fatalf("valid resume mode rejected: %v", err)
			}
		})
	}
	for name, resume := range map[string]planRunResume{
		"ambiguous": {
			SchemaVersion: "1", RunID: "run-1", GroupID: "group-1", Skip: true, UseRecommended: true,
		},
		"skip with answers": {
			SchemaVersion: "1", RunID: "run-1", GroupID: "group-1", Skip: true, Answers: map[string]any{"q-1": "x"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := resume.Validate(); err == nil {
				t.Fatal("ambiguous resume mode must fail closed")
			}
		})
	}
}

func TestPlanFrameRuntimeKeepsPlanModeAcrossTwoQuestionGroups(t *testing.T) {
	responses := []map[string]any{
		{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "我先说明一下计划。"}, "finish_reason": "stop"}}},
		planToolResponse("plan_questions", map[string]any{"questions": []map[string]any{{"id": "missing-required-fields"}}}),
		planToolResponse("plan_questions", map[string]any{
			"schemaVersion": "1", "id": "group-1", "round": 1, "goal": "范围",
			"questions": []map[string]any{{
				"id": "q-genre", "topic": "genre", "prompt": "题材", "mode": "freeform",
				"allowCustom": true, "required": true,
			}},
			"remainingUncertainties": []string{"语气"},
		}),
		planToolResponse("plan_questions", map[string]any{
			"schemaVersion": "1", "id": "group-2", "round": 2, "goal": "语气",
			"questions": []map[string]any{{
				"id": "q-tone", "topic": "tone", "prompt": "语气", "mode": "freeform",
				"allowCustom": true, "required": true,
			}},
			"remainingUncertainties": []string{},
		}),
		planToolResponse("proposed_plan", map[string]any{
			"schemaVersion": "1", "id": "plan-1", "revision": 1, "status": "proposed",
			"summary":   "可讨论计划",
			"sections":  []map[string]any{{"id": "opening", "title": "开篇", "objective": "建立悬念"}},
			"approvals": map[string]any{"planApproved": false, "executionApproved": false, "writeApproved": false},
		}),
		planToolResponse("proposed_plan", map[string]any{
			"schemaVersion": "1", "id": "plan-1", "revision": 2, "status": "proposed",
			"summary":   "已解释并修改的计划",
			"sections":  []map[string]any{{"id": "opening", "title": "开篇", "objective": "先建立人物欲望，再揭示悬念"}},
			"approvals": map[string]any{"planApproved": false, "executionApproved": false, "writeApproved": false},
		}),
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if calls >= len(responses) {
			http.Error(writer, "unexpected call", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(responses[calls]); err != nil {
			t.Fatal(err)
		}
		calls++
	}))
	defer server.Close()

	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewPlanFrameRuntime(store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	start := yanzhouprotocol.Envelope{
		Kind:            yanzhouprotocol.KindRunStart,
		ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID:       "request-1",
		Payload:         testPlanRunRequestPayload(t, server.URL),
	}
	if err := runtime.HandleFrame(context.Background(), start, &output); err != nil {
		t.Fatal(err)
	}
	for index, resume := range []map[string]any{
		{"schemaVersion": "1", "runId": "plan-run-1", "groupId": "group-1", "answers": map[string]any{"q-genre": "奇幻"}},
		{"schemaVersion": "1", "runId": "plan-run-1", "groupId": "group-2", "answers": map[string]any{"q-tone": "克制"}},
	} {
		payload, err := json.Marshal(resume)
		if err != nil {
			t.Fatal(err)
		}
		frame := yanzhouprotocol.Envelope{
			Kind: yanzhouprotocol.KindRunResume, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
			RequestID: fmt.Sprintf("resume-%d", index+1), Payload: payload,
		}
		if err := runtime.HandleFrame(context.Background(), frame, &output); err != nil {
			t.Fatal(err)
		}
	}
	for index, resume := range []map[string]any{
		{"schemaVersion": "1", "runId": "plan-run-1", "planId": "plan-1", "revision": 1, "action": "discuss", "message": "解释并调整开篇取舍"},
		{"schemaVersion": "1", "runId": "plan-run-1", "planId": "plan-1", "revision": 2, "action": "approve_plan"},
		{"schemaVersion": "1", "runId": "plan-run-1", "planId": "plan-1", "revision": 2, "action": "approve_execution"},
	} {
		payload, err := json.Marshal(resume)
		if err != nil {
			t.Fatal(err)
		}
		frame := yanzhouprotocol.Envelope{
			Kind: yanzhouprotocol.KindRunResume, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
			RequestID: fmt.Sprintf("plan-command-%d", index+1), Payload: payload,
		}
		if err := runtime.HandleFrame(context.Background(), frame, &output); err != nil {
			t.Fatal(err)
		}
	}

	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	wantTypes := []RunEventType{
		RunEventTypeRunStarted, RunEventTypePlanQuestions, RunEventTypeRunWaitingAuthor,
		RunEventTypePlanQuestions, RunEventTypeRunWaitingAuthor,
		RunEventTypePlanProposed, RunEventTypeRunWaitingAuthor,
		RunEventTypeRevisionRequested, RunEventTypePlanProposed, RunEventTypeRunWaitingAuthor,
		RunEventTypePlanApproved, RunEventTypeRunWaitingAuthor,
		RunEventTypePlanApproved, RunEventTypeRunCompleted,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("events=%d want=%d: %#v", len(events), len(wantTypes), events)
	}
	questionIDs := map[string]bool{}
	for index, event := range events {
		if event.Type != wantTypes[index] {
			t.Fatalf("event %d type=%q want=%q", index, event.Type, wantTypes[index])
		}
		if event.Type == RunEventTypePlanQuestions {
			questions, ok := event.Payload["questions"].([]any)
			if !ok || len(questions) != 1 {
				t.Fatalf("question payload=%#v", event.Payload)
			}
			question := questions[0].(map[string]any)
			id := question["id"].(string)
			if questionIDs[id] {
				t.Fatalf("question repeated across rounds: %s", id)
			}
			questionIDs[id] = true
		}
	}
	if calls != 6 {
		t.Fatalf("model calls=%d want=6", calls)
	}
	if strings.Contains(output.String(), "WP4_FAKE_KEY") || strings.Contains(output.String(), server.URL) {
		t.Fatal("private model profile material leaked to protocol output")
	}
}

func TestPlanFrameRuntimeReadsStoryBeforeQuestionsWithoutArtifacts(t *testing.T) {
	responses := []map[string]any{
		planToolResponse("story.get_outline", map[string]any{}),
		planToolResponse("plan_questions", map[string]any{
			"schemaVersion": "1", "id": "group-after-read", "round": 1, "goal": "根据大纲确认改写范围",
			"questions":              []map[string]any{{"id": "q-scope", "topic": "structure", "prompt": "保留现有结尾吗？", "mode": "freeform", "allowCustom": true, "required": true}},
			"remainingUncertainties": []string{},
		}),
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(responses[calls])
		calls++
	}))
	defer server.Close()
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewPlanFrameRuntime(store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	payload := testPlanRunRequestPayload(t, server.URL)
	var request map[string]any
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatal(err)
	}
	request["toolCapabilityManifest"] = map[string]any{"schemaVersion": "1", "capabilities": []map[string]any{{"id": "story.get_outline", "mode": "read"}}}
	request["budgets"].(map[string]any)["maxToolRounds"] = 1
	payload, _ = json.Marshal(request)
	responsePayload, _ := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_outline", "success": true,
		"result": map[string]any{"kind": "read-result", "mutationPerformed": false, "data": map[string]any{"summary": "卷纲显示结尾尚未收束"}},
	})
	if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "tool-plan-run-1-plan-1", RunID: "plan-run-1", Seq: 1, Payload: responsePayload}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: payload}, &output); err != nil {
		t.Fatal(err)
	}
	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]RunEventType, 0, len(events))
	for _, event := range events {
		got = append(got, event.Type)
		if event.Type == RunEventTypeArtifactCreated || event.Type == RunEventTypeProposalReady {
			t.Fatalf("unapproved Plan Mode created a candidate: %#v", event)
		}
	}
	want := []RunEventType{RunEventTypeRunStarted, RunEventTypeToolRequested, RunEventTypeToolStarted, RunEventTypeToolCompleted, RunEventTypePlanQuestions, RunEventTypeRunWaitingAuthor}
	if fmt.Sprint(got) != fmt.Sprint(want) || calls != 2 || !strings.Contains(output.String(), `"toolId":"story.get_outline"`) {
		t.Fatalf("events=%v calls=%d output=%s", got, calls, output.String())
	}
}

func TestPlanModelToolsPublishTheExactQuestionAndProposalSchemas(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(planToolResponse("plan_questions", map[string]any{
			"schemaVersion": "1", "id": "group-schema", "round": 1, "goal": "确认冲突强度",
			"questions": []map[string]any{{
				"id": "q-conflict", "topic": "conflict", "prompt": "保留冲突吗？", "mode": "freeform",
				"allowCustom": true, "required": true,
			}},
			"remainingUncertainties": []string{},
		}))
	}))
	defer server.Close()
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewPlanFrameRuntime(store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	start := yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: testPlanRunRequestPayload(t, server.URL)}
	if err := runtime.HandleFrame(context.Background(), start, &output); err != nil {
		t.Fatal(err)
	}
	tools, ok := requestBody["tools"].([]any)
	if !ok {
		t.Fatalf("tools=%#v", requestBody["tools"])
	}
	byName := map[string]map[string]any{}
	for _, raw := range tools {
		function := raw.(map[string]any)["function"].(map[string]any)
		byName[function["name"].(string)] = function["parameters"].(map[string]any)
	}
	questions := byName["plan_questions"]
	proposal := byName["proposed_plan"]
	if questions["additionalProperties"] != false || proposal["additionalProperties"] != false {
		t.Fatal("Plan tool schemas must be closed")
	}
	for _, field := range []string{"schemaVersion", "id", "round", "goal", "questions", "remainingUncertainties"} {
		if !jsonStringSliceContains(questions["required"], field) {
			t.Fatalf("plan_questions required field missing: %s in %#v", field, questions)
		}
	}
	questionItems := questions["properties"].(map[string]any)["questions"].(map[string]any)["items"].(map[string]any)
	if variants, ok := questionItems["allOf"].([]any); !ok || len(variants) != 3 {
		t.Fatalf("question mode constraints = %#v, want option/freeform/scale variants", questionItems["allOf"])
	}
	for _, field := range []string{"id", "topic", "prompt", "mode", "allowCustom", "required"} {
		if !jsonStringSliceContains(questionItems["required"], field) {
			t.Fatalf("question required field missing: %s in %#v", field, questionItems)
		}
	}
	for _, field := range []string{"schemaVersion", "id", "revision", "status", "summary", "sections", "approvals"} {
		if !jsonStringSliceContains(proposal["required"], field) {
			t.Fatalf("proposed_plan required field missing: %s in %#v", field, proposal)
		}
	}
}

func TestPlanModelInvalidLegacyPayloadEndsTheRunWithoutCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(planToolResponse("plan_questions", map[string]any{
			"questions": []map[string]any{{"id": "legacy", "question": "保留冲突吗？", "options": []string{"保留", "缓和"}}},
		}))
	}))
	defer server.Close()
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewPlanFrameRuntime(store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	start := yanzhouprotocol.Envelope{Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion, RequestID: "request-1", Payload: testPlanRunRequestPayload(t, server.URL)}
	if err := runtime.HandleFrame(context.Background(), start, &output); err != nil {
		t.Fatalf("a durable Plan failure must settle the frame: %v", err)
	}
	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != RunEventTypeRunStarted || events[1].Type != RunEventTypeRunFailed {
		t.Fatalf("events=%#v", events)
	}
	if events[1].Payload["code"] != "plan_response_invalid" || events[1].Payload["reason"] != "provider_error" {
		t.Fatalf("terminal=%#v", events[1])
	}
	for _, event := range events {
		if event.Type == RunEventTypeArtifactCreated || event.Type == RunEventTypeProposalReady {
			t.Fatalf("failed Plan created a candidate: %#v", event)
		}
	}
}

func TestPlanCancelRunEndsAWaitingPlanOnce(t *testing.T) {
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewPlanFrameRuntime(store, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	runtime.runs["plan-run-1"] = &planRuntimeState{request: planRunRequest{RunID: "plan-run-1", IdempotencyKey: "idem-plan-1"}}
	runtime.idempotency["idem-plan-1"] = "plan-run-1"
	var output bytes.Buffer
	if err := runtime.CancelRun(context.Background(), "plan-run-1", &output); err != nil {
		t.Fatal(err)
	}
	if err := runtime.CancelRun(context.Background(), "plan-run-1", &output); err == nil {
		t.Fatal("second cancel must reject a terminal plan run")
	}
	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != RunEventTypeRunAborted {
		t.Fatalf("events = %#v, want one run.aborted", events)
	}
}

func jsonStringSliceContains(value any, expected string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}

func TestPlanRunRequestDoesNotRejectSelectedSkillDuringTaskFive(t *testing.T) {
	payload := testPlanRunRequestPayload(t, "https://fixture.invalid/v1")
	var request planRunRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatal(err)
	}
	request.SelectedSkillIDs = []string{"outline"}
	if err := request.Validate("request-1"); err != nil {
		t.Fatalf("Task 5 keeps Skills inert rather than rejecting Plan Mode: %v", err)
	}
}

func TestPlanExitWorksBeforeAProposalExists(t *testing.T) {
	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewPlanFrameRuntime(store, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	state := &planRuntimeState{request: planRunRequest{RunID: "plan-run-1"}}
	resume := planRunResume{SchemaVersion: "1", RunID: "plan-run-1", Action: "exit", PlanID: "stale-plan", Revision: 9}
	if err := runtime.handlePlanCommand(context.Background(), state, resume, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := resume.Validate(); err != nil {
		t.Fatalf("question-stage exit must be a valid resume: %v", err)
	}
	events, err := store.ReplayAfter(context.Background(), "plan-run-1", 0, 2)
	if err != nil || len(events) != 1 || events[0].Type != RunEventTypeRunAborted {
		t.Fatalf("exit events=%#v err=%v", events, err)
	}
}

func TestHandshakeNegotiatesPlanModeWithoutEchoingUnknownFeatures(t *testing.T) {
	gate, err := NewBootstrapTokenGate("wp4-token")
	if err != nil {
		t.Fatal(err)
	}
	response, err := Handshake(yanzhouprotocol.HandshakeRequest{
		ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		ClientBuild:     "wp4-test",
		WorkspaceSchema: "yanzhou-book/1",
		BootstrapToken:  "wp4-token",
		RequestedFeatures: []string{
			"handshake", "plan-mode", "future-unknown-feature",
		},
	}, gate, yanzhouprotocol.Provenance{
		SchemaVersion:      "1",
		UpstreamRepository: "denova",
		UpstreamBaseSHA:    "a111111111111111111111111111111111111111",
		AdapterCommitSHA:   "b222222222222222222222222222222222222222",
		SourceTreeSHA:      "c333333333333333333333333333333333333333",
		BinarySHA256:       strings.Repeat("d", 64),
		SkillsManifestSHA:  strings.Repeat("e", 64),
		GoVersion:          "go1.26.5",
		TargetOS:           "darwin",
		TargetArch:         "arm64",
		BuiltAt:            "2026-07-24T00:00:00Z",
	}, "wp4-sidecar")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"handshake", "plan-mode"}
	if fmt.Sprint(response.SupportedFeatures) != fmt.Sprint(want) {
		t.Fatalf("supported features=%v want=%v", response.SupportedFeatures, want)
	}
}

func TestSkillCatalogAndLoadApplyPrecedenceAgentOverrideAndCapabilityTwice(t *testing.T) {
	manifests := []SkillManifest{
		{
			SchemaVersion: "1", ID: "outline", Name: "outline", Revision: 1, Source: SkillSourceBuiltin,
			AgentKinds: []string{"ide"}, CompatibleCapabilities: []string{"story.get_outline"},
			Categories: []string{"workflow"}, Summary: "builtin outline", EntryResource: "SKILL.md",
			Checksum: "sha256:" + strings.Repeat("a", 64),
		},
		{
			SchemaVersion: "1", ID: "outline", Name: "outline", Revision: 7, Source: SkillSourceWorkspace,
			AgentKinds: []string{"interactive_story"}, CompatibleCapabilities: []string{"story.get_outline"},
			Categories: []string{"workflow"}, Summary: "workspace outline", EntryResource: "SKILL.md",
			Checksum: "sha256:" + strings.Repeat("b", 64),
		},
	}
	if got, err := BuildSkillCatalog(manifests, SkillCatalogFilter{
		AgentKind: "ide", Capabilities: []string{"story.get_outline"}, Overrides: map[string]bool{},
	}); err != nil || len(got) != 0 {
		t.Fatalf("inapplicable workspace override must suppress builtin fallback: got=%#v err=%v", got, err)
	}
	catalog, err := BuildSkillCatalog(manifests, SkillCatalogFilter{
		AgentKind: "ide", Capabilities: []string{"story.get_outline"}, Overrides: map[string]bool{"outline": true},
	})
	if err != nil || len(catalog) != 1 || catalog[0].Source != SkillSourceWorkspace || catalog[0].Summary != "workspace outline" {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
	serialized, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"content", "path", "supportResources", "entryResource"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("catalog leaked %q: %s", forbidden, serialized)
		}
	}

	receipt := SkillLoadReceipt{
		SchemaVersion: "1", ID: "outline", Revision: 7,
		Checksum: "sha256:" + strings.Repeat("b", 64), Source: SkillSourceWorkspace,
		Resources: []SkillResourceReceipt{},
	}
	request := SkillLoadRequest{
		SchemaVersion: "1", RunID: "run-1", ID: "outline", ExpectedRevision: 7,
		AgentKind: "ide", Capabilities: []string{"story.get_outline"}, Overrides: map[string]bool{"outline": true},
	}
	accepted, err := AcceptSkillLoad(manifests, request, receipt)
	if err != nil || accepted.Checksum != receipt.Checksum {
		t.Fatalf("accepted=%#v err=%v", accepted, err)
	}
	request.Capabilities = nil
	if _, err := AcceptSkillLoad(manifests, request, receipt); err == nil {
		t.Fatal("load must recheck capability filter independently from Catalog")
	}
	receipt.Revision = 6
	request.Capabilities = []string{"story.get_outline"}
	if _, err := AcceptSkillLoad(manifests, request, receipt); err == nil {
		t.Fatal("stale load receipt must fail closed")
	}
}

func TestSkillSnapshotRequiresExactReceiptAndContainsNoContentOrPaths(t *testing.T) {
	selected := []SelectedSkill{{ID: "outline", Revision: 7, Checksum: "sha256:" + strings.Repeat("a", 64)}}
	if _, err := NewSkillSnapshot("run-1", selected, nil); err == nil {
		t.Fatal("selected Skill without a receipt must fail")
	}
	receipt := SkillLoadReceipt{
		SchemaVersion: "1", ID: "outline", Revision: 7,
		Checksum: "sha256:" + strings.Repeat("a", 64), Source: SkillSourceWorkspace,
		Resources: []SkillResourceReceipt{{Path: "SKILL.md", Checksum: "sha256:" + strings.Repeat("c", 64), Size: 12}},
	}
	snapshot, err := NewSkillSnapshot("run-1", selected, []SkillLoadReceipt{receipt})
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "content") || strings.Contains(string(serialized), "/Users/") {
		t.Fatalf("snapshot leaked content or paths: %s", serialized)
	}
}

func planToolResponse(name string, arguments map[string]any) map[string]any {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		panic(err)
	}
	return map[string]any{
		"choices": []map[string]any{{
			"message": map[string]any{
				"role": "assistant", "content": "",
				"tool_calls": []map[string]any{{
					"id": "call-1", "type": "function",
					"function": map[string]any{"name": name, "arguments": string(encoded)},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	}
}

func testPlanRunRequestPayload(t *testing.T, baseURL string) json.RawMessage {
	t.Helper()
	payload := map[string]any{
		"schemaVersion": "1", "requestId": "request-1", "idempotencyKey": "idempotency-1",
		"runId": "plan-run-1", "sessionId": "session-1", "agentKind": "ide",
		"entrypoint": "agent_chat", "target": map[string]any{
			"schemaVersion": "1", "kind": "book", "bookId": "book-1", "targetId": "book-1",
		},
		"userIntent": "制定故事计划", "planMode": true, "selectedSkillIds": []string{},
		"effectiveModelProfile": map[string]any{
			"profileId": "fake-plan", "providerType": "openai-compatible", "adapterId": "openai-compatible",
			"baseUrl": baseURL, "model": "fake-model", "timeoutMs": 5000,
			"extraHeaders": map[string]string{}, "runtimeAuth": map[string]any{"mode": "inline-stdin", "apiKey": "WP4_FAKE_KEY"},
			"capabilities": map[string]any{"streaming": false},
			"resolution":   map[string]any{"source": "run", "degradedFeatures": []string{}},
		},
		"contextPackRef":         map[string]any{"ref": "sha256:" + strings.Repeat("a", 64)},
		"toolCapabilityManifest": map[string]any{"schemaVersion": "1", "capabilities": []any{}},
		"budgets": map[string]any{
			"maxModelCalls": 6, "maxToolRounds": 0, "maxDelegations": 0,
			"maxRevisionRounds": 2, "maxWallTimeMs": 30000,
		},
		"baseRevisions": map[string]string{"book": "revision-1"}, "displayLocale": "zh-CN",
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestPlanStreamParserCrossChunkHidesControlTagsAndStopsAfterSuccess(t *testing.T) {
	parser := NewPlanStreamParser()

	first, err := parser.Push("先确认<plan_ques")
	if err != nil {
		t.Fatal(err)
	}
	second, err := parser.Push("tions>{\"id\":\"group-1\",\"round\":1,\"goal\":\"范围\",\"questions\":[],\"remainingUncertainties\":[]}</plan_questions>不得显示")
	if err != nil {
		t.Fatal(err)
	}
	last, err := parser.Flush()
	if err != nil {
		t.Fatal(err)
	}

	if got := first.Visible + second.Visible + last.Visible; got != "先确认" {
		t.Fatalf("visible content = %q, want only preamble", got)
	}
	if len(second.Blocks) != 1 || second.Blocks[0].Kind != PlanBlockQuestions {
		t.Fatalf("blocks = %#v, want one question block", second.Blocks)
	}
	if !second.Stop || !parser.Stopped() {
		t.Fatalf("successful block must stop the model round: result=%#v", second)
	}
	serialized := second.Blocks[0].Content + first.Visible + second.Visible + last.Visible
	for _, tag := range []string{"<plan_questions>", "</plan_questions>", "<proposed_plan>", "</proposed_plan>"} {
		if strings.Contains(serialized, tag) {
			t.Fatalf("control tag leaked: %s", tag)
		}
	}
}

func TestPlanStreamParserRejectsNestedAndUnclosedBlocksWithoutLeak(t *testing.T) {
	for name, chunks := range map[string][]string{
		"nested":   {"<plan_questions>{}", "<proposed_plan>bad</proposed_plan></plan_questions>"},
		"unclosed": {"prefix<proposed_plan># incomplete"},
	} {
		t.Run(name, func(t *testing.T) {
			parser := NewPlanStreamParser()
			var visible strings.Builder
			var parseErr error
			for _, chunk := range chunks {
				result, err := parser.Push(chunk)
				visible.WriteString(result.Visible)
				if err != nil {
					parseErr = err
					break
				}
			}
			if parseErr == nil {
				result, err := parser.Flush()
				visible.WriteString(result.Visible)
				parseErr = err
			}
			if parseErr == nil {
				t.Fatal("malformed plan block must fail closed")
			}
			if strings.Contains(visible.String(), "plan_questions") || strings.Contains(visible.String(), "proposed_plan") {
				t.Fatalf("control tag leaked in visible output: %q", visible.String())
			}
		})
	}
}

func TestPlanToolCallParserAcceptsClosedFormAndRejectsBadJSON(t *testing.T) {
	block, handled, err := ParsePlanToolCall("plan_questions", `{"id":"group-1","round":1,"goal":"范围","questions":[],"remainingUncertainties":[]}`)
	if err != nil || !handled {
		t.Fatalf("valid tool call handled=%t err=%v", handled, err)
	}
	if block.Kind != PlanBlockQuestions || !strings.Contains(block.Content, `"group-1"`) {
		t.Fatalf("unexpected block: %#v", block)
	}

	if _, handled, err := ParsePlanToolCall("plan_questions", `{"questions":`); !handled || err == nil {
		t.Fatalf("truncated arguments handled=%t err=%v, want handled error", handled, err)
	}
	if _, handled, err := ParsePlanToolCall("story.get_target", `{}`); handled || err != nil {
		t.Fatalf("non-plan tool handled=%t err=%v", handled, err)
	}
}

func TestPlanToolCallParserPreservesStructuredProposalInsteadOfExtractingSummary(t *testing.T) {
	raw := `{"schemaVersion":"1","id":"plan-1","revision":1,"status":"proposed","summary":"可讨论计划","sections":[{"id":"opening","title":"开篇","objective":"建立悬念"}],"approvals":{"planApproved":false,"executionApproved":false,"writeApproved":false}}`
	block, handled, err := ParsePlanToolCall("proposed_plan", raw)
	if err != nil || !handled {
		t.Fatalf("structured proposal handled=%t err=%v", handled, err)
	}
	if block.Kind != PlanBlockProposal || block.Content != raw {
		t.Fatalf("structured proposal content=%q, want complete JSON", block.Content)
	}
}
