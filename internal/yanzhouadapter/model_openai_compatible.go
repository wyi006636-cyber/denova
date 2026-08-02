package yanzhouadapter

import (
	"encoding/json"
	"strings"
)

type openAICompatibleAdapter struct {
	modelAdapterBase
}

var modelToolAliases = map[string]string{
	"story.get_target": "story_get_target", "story.get_outline": "story_get_outline",
	"story.get_adjacent_chapters": "story_get_adjacent_chapters", "story.search_chapters": "story_search_chapters",
	"story.get_characters": "story_get_characters", "story.get_open_threads": "story_get_open_threads",
	"command.run": "command_run", "web.search": "web_search", "image.generate": "image_generate",
}

func modelToolName(name string) string {
	if alias := modelToolAliases[name]; alias != "" {
		return alias
	}
	return name
}
func canonicalModelToolName(name string) string {
	for canonical, alias := range modelToolAliases {
		if name == alias {
			return canonical
		}
	}
	return name
}

func newOpenAICompatibleAdapter(profile EffectiveModelProfile, adapterID string) ModelAdapter {
	return &openAICompatibleAdapter{
		modelAdapterBase: modelAdapterBase{profile: profile, id: adapterID},
	}
}

func (a *openAICompatibleAdapter) BuildRequest(request ModelRequest, stream bool) (NativeModelRequest, error) {
	messages := make([]map[string]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		item := map[string]any{
			"role":    message.Role,
			"content": message.Content,
		}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			toolCalls := make([]map[string]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				toolCalls = append(toolCalls, map[string]any{
					"id":   call.ID,
					"type": "function",
					"function": map[string]any{
						"name":      modelToolName(call.Name),
						"arguments": call.Arguments,
					},
				})
			}
			item["tool_calls"] = toolCalls
		}
		if message.Role == "tool" {
			item["tool_call_id"] = message.ToolCallID
			item["name"] = modelToolName(message.Name)
		}
		messages = append(messages, item)
	}
	tools := make([]map[string]any, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        modelToolName(tool.Name),
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			},
		})
	}
	body := map[string]any{
		"model":       a.profile.Model,
		"messages":    messages,
		"temperature": request.Temperature,
		"max_tokens":  request.MaxOutputTokens,
		"stream":      stream,
	}
	if len(tools) > 0 {
		body["tools"] = tools
		body["parallel_tool_calls"] = false
	}
	if stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	headers := a.requestHeaders()
	apiKey := a.profile.RuntimeAuth.APIKey
	if apiKey == "" && a.profile.ProviderType == ProviderOllama {
		apiKey = "ollama"
	}
	if apiKey == "" && a.profile.ProviderType == ProviderLMStudio {
		apiKey = "lm-studio"
	}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	base := strings.TrimRight(a.profile.BaseURL, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return marshalNativeRequest(base, headers, body)
	}
	return marshalNativeRequest(base+"/chat/completions", headers, body)
}

func (a *openAICompatibleAdapter) NormalizeResponse(data []byte) (ModelResponse, error) {
	var payload struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Input  int `json:"prompt_tokens"`
			Output int `json:"completion_tokens"`
			Total  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ModelResponse{}, err
	}
	if len(payload.Choices) == 0 {
		return ModelResponse{}, nil
	}
	choice := payload.Choices[0]
	toolCalls := make([]ModelToolCall, 0, len(choice.Message.ToolCalls))
	for _, call := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, ModelToolCall{
			ID:        call.ID,
			Name:      canonicalModelToolName(call.Function.Name),
			Arguments: call.Function.Arguments,
		})
	}
	return ModelResponse{
		Role:         "assistant",
		Content:      choice.Message.Content,
		ToolCalls:    toolCalls,
		FinishReason: normalizeFinishReason(choice.FinishReason, len(toolCalls) > 0),
		Usage: ModelUsage{
			InputTokens:  payload.Usage.Input,
			OutputTokens: payload.Usage.Output,
			TotalTokens:  payload.Usage.Total,
		},
	}, nil
}

func (a *openAICompatibleAdapter) NormalizeStream(chunks []json.RawMessage) ([]ModelStreamEvent, error) {
	events := make([]ModelStreamEvent, 0, len(chunks))
	for _, data := range chunks {
		var payload struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				Input  int `json:"prompt_tokens"`
				Output int `json:"completion_tokens"`
				Total  int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, err
		}
		if len(payload.Choices) == 0 {
			continue
		}
		choice := payload.Choices[0]
		if choice.Delta.Content != "" {
			events = append(events, ModelStreamEvent{
				Type:    "content-delta",
				Content: choice.Delta.Content,
			})
		}
		for _, call := range choice.Delta.ToolCalls {
			toolCall := ModelToolCall{
				ID:        call.ID,
				Name:      canonicalModelToolName(call.Function.Name),
				Arguments: call.Function.Arguments,
			}
			events = append(events, ModelStreamEvent{
				Type:     "tool-call-delta",
				ToolCall: &toolCall,
			})
		}
		if choice.FinishReason != "" {
			usage := ModelUsage{
				InputTokens:  payload.Usage.Input,
				OutputTokens: payload.Usage.Output,
				TotalTokens:  payload.Usage.Total,
			}
			events = append(events, ModelStreamEvent{
				Type:         "message-complete",
				FinishReason: normalizeFinishReason(choice.FinishReason, choice.FinishReason == "tool_calls"),
				Usage:        &usage,
			})
		}
	}
	return events, nil
}

func (a *openAICompatibleAdapter) NormalizeError(status int, body []byte) ModelError {
	return a.normalizeError(status, body)
}
