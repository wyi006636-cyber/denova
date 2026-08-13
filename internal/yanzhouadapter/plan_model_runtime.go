package yanzhouadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func validatePlanAnswers(group PlanQuestionGroup, answers map[string]any) error {
	questions := map[string]PlanQuestion{}
	for _, question := range group.Questions {
		questions[question.ID] = question
	}
	for id, answer := range answers {
		if _, exists := questions[id]; !exists || !boundedPlanJSON(answer) {
			return invalidPlanPayload()
		}
	}
	for _, question := range group.Questions {
		active := true
		for _, dependency := range question.DependsOn {
			answer, exists := answers[dependency.QuestionID]
			if !exists || !equalPlanJSON(answer, dependency.Answer) {
				active = false
				break
			}
		}
		answer, answered := answers[question.ID]
		if !active && answered {
			return invalidPlanPayload()
		}
		if active && question.Required && !answered {
			return invalidPlanPayload()
		}
		if active && answered && !validPlanAnswer(question, answer) {
			return invalidPlanPayload()
		}
	}
	return nil
}

func equalPlanJSON(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func validPlanAnswer(question PlanQuestion, answer any) bool {
	optionIDs := map[string]bool{}
	for _, option := range question.Options {
		optionIDs[option.ID] = true
	}
	validChoice := func(value any) bool {
		choice, ok := value.(string)
		return ok && boundedPlanText(choice, 4096) && (optionIDs[choice] || question.AllowCustom)
	}
	switch question.Mode {
	case PlanQuestionSingle:
		return validChoice(answer)
	case PlanQuestionMulti, PlanQuestionRank:
		values, ok := answer.([]any)
		if !ok || len(values) > 32 || (len(values) == 0 && question.Required) {
			return false
		}
		seen := map[string]bool{}
		for _, value := range values {
			choice, ok := value.(string)
			if !ok || seen[choice] || !validChoice(choice) {
				return false
			}
			seen[choice] = true
		}
		if question.Mode == PlanQuestionRank && !question.AllowCustom {
			if len(values) != len(optionIDs) {
				return false
			}
			for optionID := range optionIDs {
				if !seen[optionID] {
					return false
				}
			}
		}
		return true
	case PlanQuestionFreeform:
		value, ok := answer.(string)
		return ok && boundedPlanText(value, 4096)
	case PlanQuestionScale:
		value, ok := planInteger(answer)
		return ok && question.Scale != nil && value >= question.Scale.Min && value <= question.Scale.Max && (value-question.Scale.Min)%question.Scale.Step == 0
	default:
		return false
	}
}

func planInteger(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case float64:
		integer := int(number)
		return integer, float64(integer) == number
	case json.Number:
		integer, err := number.Int64()
		return int(integer), err == nil && int64(int(integer)) == integer
	default:
		return 0, false
	}
}

func (runtime *PlanFrameRuntime) runModelRound(ctx context.Context, state *planRuntimeState, output io.Writer) error {
	if state.modelCalls >= state.request.Budgets.MaxModelCalls {
		return errors.New("plan model call budget is exhausted")
	}
	state.modelCalls++
	request := ModelRequest{
		Messages: state.messages,
		Tools: []ModelTool{
			{Name: "plan_questions", Description: "Ask one bounded group of planning questions", InputSchema: map[string]any{"type": "object"}},
			{Name: "proposed_plan", Description: "Propose a discussable plan after uncertainties are resolved", InputSchema: map[string]any{"type": "object"}},
		},
		MaxOutputTokens: 4096,
	}
	native, err := state.adapter.BuildRequest(request, false)
	if err != nil {
		return errors.New("plan model request could not be built")
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(state.request.EffectiveModelProfile.TimeoutMS)*time.Millisecond)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(callCtx, native.Method, native.URL, bytes.NewReader(native.Body))
	if err != nil {
		return errors.New("plan model request is invalid")
	}
	for key, value := range native.Headers {
		httpRequest.Header.Set(key, value)
	}
	response, err := runtime.client.Do(httpRequest)
	if err != nil {
		return errors.New("plan model request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(body) > 1024*1024 {
		return errors.New("plan model response is invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("plan model request failed")
	}
	modelResponse, err := state.adapter.NormalizeResponse(body)
	if err != nil {
		return errors.New("plan model response is invalid")
	}
	block, err := parsePlanModelResponse(modelResponse)
	if err != nil {
		return err
	}
	return runtime.acceptPlanBlock(ctx, state, block, output)
}

func parsePlanModelResponse(response ModelResponse) (PlanBlock, error) {
	if len(response.ToolCalls) > 0 {
		if len(response.ToolCalls) != 1 || strings.TrimSpace(response.Content) != "" {
			return PlanBlock{}, errors.New("plan model response is ambiguous")
		}
		block, handled, err := ParsePlanToolCall(response.ToolCalls[0].Name, response.ToolCalls[0].Arguments)
		if err != nil || !handled {
			return PlanBlock{}, errors.New("plan model tool call is invalid")
		}
		return block, nil
	}
	parser := NewPlanStreamParser()
	result, err := parser.Push(response.Content)
	if err != nil {
		return PlanBlock{}, errors.New("plan model block is invalid")
	}
	blocks := append([]PlanBlock{}, result.Blocks...)
	if !result.Stop {
		flushed, flushErr := parser.Flush()
		if flushErr != nil {
			return PlanBlock{}, errors.New("plan model block is invalid")
		}
		blocks = append(blocks, flushed.Blocks...)
	}
	if len(blocks) != 1 {
		return PlanBlock{}, errors.New("plan model response must contain one plan block")
	}
	return blocks[0], nil
}

func (runtime *PlanFrameRuntime) acceptPlanBlock(ctx context.Context, state *planRuntimeState, block PlanBlock, output io.Writer) error {
	switch block.Kind {
	case PlanBlockQuestions:
		group, err := DecodePlanQuestionGroup([]byte(block.Content))
		if err != nil || group.Round != state.modelCalls {
			return errors.New("plan question group is invalid")
		}
		for _, question := range group.Questions {
			if state.asked[question.ID] {
				return errors.New("plan question cannot be repeated")
			}
		}
		payload, err := publicPlanPayload(group)
		if err != nil {
			return err
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{Type: RunEventTypePlanQuestions, Payload: payload}); err != nil {
			return err
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{
			Type: RunEventTypeRunWaitingAuthor, Payload: map[string]any{"reason": "plan_questions", "groupId": group.ID},
		}); err != nil {
			return err
		}
		for _, question := range group.Questions {
			state.asked[question.ID] = true
		}
		state.lastGroup = &group
		encoded, _ := json.Marshal(group)
		state.messages = append(state.messages, ModelMessage{Role: "assistant", Content: string(encoded)})
		return nil
	case PlanBlockProposal:
		proposal, err := DecodeProposedPlan([]byte(block.Content))
		if err != nil || state.lastGroup != nil {
			return errors.New("proposed plan is invalid")
		}
		if state.expectedProposalRevision > 0 {
			if proposal.Revision != state.expectedProposalRevision || (state.proposal != nil && proposal.ID != state.proposal.ID) {
				return errors.New("proposed plan revision is invalid")
			}
		} else if proposal.Revision != 1 {
			return errors.New("initial proposed plan revision is invalid")
		}
		payload, err := publicPlanPayload(proposal)
		if err != nil {
			return err
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{Type: RunEventTypePlanProposed, Payload: payload}); err != nil {
			return err
		}
		if _, err := EmitRunEvent(ctx, runtime.store, output, state.request.RunID, RuntimeEventInput{
			Type: RunEventTypeRunWaitingAuthor, Payload: map[string]any{"reason": "plan_proposed", "planId": proposal.ID, "revision": proposal.Revision},
		}); err != nil {
			return err
		}
		state.proposal = &proposal
		state.expectedProposalRevision = 0
		return nil
	default:
		return errors.New("plan block kind is invalid")
	}
}

func publicPlanPayload(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("plan payload is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("plan payload is invalid")
	}
	delete(payload, "schemaVersion")
	// The durable event spine deliberately rejects generic `prompt` keys. Plan
	// questions expose the same author-facing value under a projection-safe key;
	// main reconstructs the InterviewQuestion DTO for the Renderer.
	if questions, ok := payload["questions"].([]any); ok {
		for _, item := range questions {
			question, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("plan payload is invalid")
			}
			question["questionText"] = question["prompt"]
			delete(question, "prompt")
		}
	}
	return payload, nil
}

func planModeSystemInstruction() string {
	return "Stay in Plan Mode. Ask a new plan_questions group while critical uncertainty remains; otherwise return exactly one proposed_plan. Never imply plan approval, execution approval, or write approval. Do not repeat question ids."
}
