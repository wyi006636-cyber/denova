package yanzhouadapter

import (
	"encoding/json"
	"strings"
)

type anthropicAdapter struct {
	modelAdapterBase
}

func newAnthropicAdapter(profile EffectiveModelProfile) ModelAdapter {
	return &anthropicAdapter{
		modelAdapterBase: modelAdapterBase{profile: profile, id: "anthropic-native"},
	}
}

func (a *anthropicAdapter) BuildRequest(request ModelRequest, stream bool) (NativeModelRequest, error) {
	systemParts := make([]string, 0)
	messages := make([]map[string]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		switch message.Role {
		case "system":
			if message.Content != "" {
				systemParts = append(systemParts, message.Content)
			}
		case "assistant":
			content := make([]map[string]any, 0, len(message.ToolCalls)+1)
			if message.Content != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": message.Content,
				})
			}
			for _, call := range message.ToolCalls {
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    call.ID,
					"name":  call.Name,
					"input": parseArguments(call.Arguments),
				})
			}
			messages = append(messages, map[string]any{
				"role":    "assistant",
				"content": content,
			})
		case "tool":
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": message.ToolCallID,
					"content":     message.Content,
				}},
			})
		default:
			messages = append(messages, map[string]any{
				"role":    "user",
				"content": message.Content,
			})
		}
	}
	tools := make([]map[string]any, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": tool.InputSchema,
		})
	}
	body := map[string]any{
		"model":       a.profile.Model,
		"max_tokens":  request.MaxOutputTokens,
		"temperature": request.Temperature,
		"stream":      stream,
		"messages":    messages,
	}
	if len(systemParts) > 0 {
		body["system"] = strings.Join(systemParts, "\n\n")
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	headers := a.requestHeaders()
	if a.profile.RuntimeAuth.APIKey != "" {
		headers["x-api-key"] = a.profile.RuntimeAuth.APIKey
	}
	headers["anthropic-version"] = "2023-06-01"
	base := strings.TrimRight(a.profile.BaseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		base += "/messages"
	} else {
		base += "/v1/messages"
	}
	return marshalNativeRequest(base, headers, body)
}

func (a *anthropicAdapter) NormalizeResponse(data []byte) (ModelResponse, error) {
	var payload struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ModelResponse{}, err
	}
	var content strings.Builder
	toolCalls := make([]ModelToolCall, 0)
	for _, block := range payload.Content {
		switch block.Type {
		case "text":
			content.WriteString(block.Text)
		case "tool_use":
			toolCalls = append(toolCalls, ModelToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: stringifyArguments(block.Input),
			})
		}
	}
	return ModelResponse{
		Role:         "assistant",
		Content:      content.String(),
		ToolCalls:    toolCalls,
		FinishReason: normalizeFinishReason(payload.StopReason, len(toolCalls) > 0),
		Usage: ModelUsage{
			InputTokens:  payload.Usage.Input,
			OutputTokens: payload.Usage.Output,
			TotalTokens:  payload.Usage.Input + payload.Usage.Output,
		},
	}, nil
}

func (a *anthropicAdapter) NormalizeStream(chunks []json.RawMessage) ([]ModelStreamEvent, error) {
	events := make([]ModelStreamEvent, 0, len(chunks))
	inputTokens := 0
	for _, data := range chunks {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Usage struct {
					Input int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			ContentBlock struct {
				Type  string         `json:"type"`
				ID    string         `json:"id"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			} `json:"content_block"`
			Delta struct {
				Type       string `json:"type"`
				Text       string `json:"text"`
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				Output int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		switch event.Type {
		case "message_start":
			inputTokens = event.Message.Usage.Input
		case "content_block_delta":
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				events = append(events, ModelStreamEvent{
					Type:    "content-delta",
					Content: event.Delta.Text,
				})
			}
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				toolCall := ModelToolCall{
					ID:        event.ContentBlock.ID,
					Name:      event.ContentBlock.Name,
					Arguments: stringifyArguments(event.ContentBlock.Input),
				}
				events = append(events, ModelStreamEvent{
					Type:     "tool-call-delta",
					ToolCall: &toolCall,
				})
			}
		case "message_delta":
			if event.Delta.StopReason != "" {
				usage := ModelUsage{
					InputTokens:  inputTokens,
					OutputTokens: event.Usage.Output,
					TotalTokens:  inputTokens + event.Usage.Output,
				}
				events = append(events, ModelStreamEvent{
					Type:         "message-complete",
					FinishReason: normalizeFinishReason(event.Delta.StopReason, false),
					Usage:        &usage,
				})
			}
		}
	}
	return events, nil
}

func (a *anthropicAdapter) NormalizeError(status int, body []byte) ModelError {
	return a.normalizeError(status, body)
}
