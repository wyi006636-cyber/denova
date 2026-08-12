package yanzhouadapter

import (
	"encoding/json"
	"net/url"
	"strings"
)

type geminiAdapter struct {
	modelAdapterBase
}

func newGeminiAdapter(profile EffectiveModelProfile) ModelAdapter {
	return &geminiAdapter{
		modelAdapterBase: modelAdapterBase{profile: profile, id: "gemini-native"},
	}
}

func (a *geminiAdapter) BuildRequest(request ModelRequest, stream bool) (NativeModelRequest, error) {
	systemParts := make([]string, 0)
	contents := make([]map[string]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		switch message.Role {
		case "system":
			if message.Content != "" {
				systemParts = append(systemParts, message.Content)
			}
		case "assistant":
			parts := make([]map[string]any, 0, len(message.ToolCalls)+1)
			if message.Content != "" {
				parts = append(parts, map[string]any{"text": message.Content})
			}
			for _, call := range message.ToolCalls {
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"id":   call.ID,
						"name": call.Name,
						"args": parseArguments(call.Arguments),
					},
				})
			}
			contents = append(contents, map[string]any{
				"role":  "model",
				"parts": parts,
			})
		case "tool":
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []map[string]any{{
					"functionResponse": map[string]any{
						"id":       message.ToolCallID,
						"name":     message.Name,
						"response": geminiToolResponse(message.Content),
					},
				}},
			})
		default:
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": []map[string]any{{"text": message.Content}},
			})
		}
	}
	tools := make([]map[string]any, 0, len(request.Tools))
	if len(request.Tools) > 0 {
		declarations := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			declarations = append(declarations, map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			})
		}
		tools = append(tools, map[string]any{"functionDeclarations": declarations})
	}
	body := map[string]any{
		"contents": contents,
		"generationConfig": map[string]any{
			"maxOutputTokens": request.MaxOutputTokens,
			"temperature":     request.Temperature,
		},
	}
	if len(systemParts) > 0 {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": strings.Join(systemParts, "\n\n")}},
		}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	headers := a.requestHeaders()
	if a.profile.RuntimeAuth.APIKey != "" {
		headers["x-goog-api-key"] = a.profile.RuntimeAuth.APIKey
	}
	action := "generateContent"
	if stream {
		action = "streamGenerateContent?alt=sse"
	}
	endpoint := strings.TrimRight(a.profile.BaseURL, "/") +
		"/models/" + url.PathEscape(a.profile.Model) + ":" + action
	return marshalNativeRequest(endpoint, headers, body)
}

func geminiToolResponse(content string) map[string]any {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err == nil {
		return parsed
	}
	return map[string]any{"content": content}
}

type geminiPayload struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text         string `json:"text"`
				FunctionCall *struct {
					ID   string         `json:"id"`
					Name string         `json:"name"`
					Args map[string]any `json:"args"`
				} `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Usage struct {
		Input  int `json:"promptTokenCount"`
		Output int `json:"candidatesTokenCount"`
		Total  int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func (a *geminiAdapter) NormalizeResponse(data []byte) (ModelResponse, error) {
	var payload geminiPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return ModelResponse{}, err
	}
	if len(payload.Candidates) == 0 {
		return ModelResponse{}, nil
	}
	candidate := payload.Candidates[0]
	var content strings.Builder
	toolCalls := make([]ModelToolCall, 0)
	for _, part := range candidate.Content.Parts {
		content.WriteString(part.Text)
		if part.FunctionCall != nil {
			toolCalls = append(toolCalls, ModelToolCall{
				ID:        part.FunctionCall.ID,
				Name:      part.FunctionCall.Name,
				Arguments: stringifyArguments(part.FunctionCall.Args),
			})
		}
	}
	return ModelResponse{
		Role:         "assistant",
		Content:      content.String(),
		ToolCalls:    toolCalls,
		FinishReason: normalizeFinishReason(candidate.FinishReason, len(toolCalls) > 0),
		Usage: ModelUsage{
			InputTokens:  payload.Usage.Input,
			OutputTokens: payload.Usage.Output,
			TotalTokens:  payload.Usage.Total,
		},
	}, nil
}

func (a *geminiAdapter) NormalizeStream(chunks []json.RawMessage) ([]ModelStreamEvent, error) {
	events := make([]ModelStreamEvent, 0, len(chunks))
	sawToolCall := false
	for _, data := range chunks {
		var payload geminiPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, err
		}
		if len(payload.Candidates) == 0 {
			continue
		}
		candidate := payload.Candidates[0]
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				events = append(events, ModelStreamEvent{
					Type:    "content-delta",
					Content: part.Text,
				})
			}
			if part.FunctionCall != nil {
				sawToolCall = true
				toolCall := ModelToolCall{
					ID:        part.FunctionCall.ID,
					Name:      part.FunctionCall.Name,
					Arguments: stringifyArguments(part.FunctionCall.Args),
				}
				events = append(events, ModelStreamEvent{
					Type:     "tool-call-delta",
					ToolCall: &toolCall,
				})
			}
		}
		if candidate.FinishReason != "" {
			usage := ModelUsage{
				InputTokens:  payload.Usage.Input,
				OutputTokens: payload.Usage.Output,
				TotalTokens:  payload.Usage.Total,
			}
			events = append(events, ModelStreamEvent{
				Type:         "message-complete",
				FinishReason: normalizeFinishReason(candidate.FinishReason, sawToolCall),
				Usage:        &usage,
			})
		}
	}
	return events, nil
}

func (a *geminiAdapter) NormalizeError(status int, body []byte) ModelError {
	return a.normalizeError(status, body)
}
