package yanzhouadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type ProviderType string

const (
	ProviderOpenAICompatible ProviderType = "openai-compatible"
	ProviderAnthropicNative  ProviderType = "anthropic-native"
	ProviderGeminiNative     ProviderType = "gemini-native"
	ProviderOllama           ProviderType = "ollama"
	ProviderLMStudio         ProviderType = "lm-studio"
	ProviderCustomLocal      ProviderType = "custom-local"
)

type RuntimeAuthMode string

const (
	RuntimeAuthInlineStdin RuntimeAuthMode = "inline-stdin"
	RuntimeAuthNone        RuntimeAuthMode = "none"
)

type RuntimeAuth struct {
	Mode   RuntimeAuthMode `json:"mode"`
	APIKey string          `json:"apiKey,omitempty"`
}

type EffectiveModelProfile struct {
	ProfileID    string            `json:"profileId"`
	ProviderType ProviderType      `json:"providerType"`
	AdapterID    string            `json:"adapterId"`
	BaseURL      string            `json:"baseUrl,omitempty"`
	Model        string            `json:"model"`
	TimeoutMS    int               `json:"timeoutMs"`
	ExtraHeaders map[string]string `json:"extraHeaders,omitempty"`
	RuntimeAuth  RuntimeAuth       `json:"runtimeAuth"`
}

type ModelMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCalls  []ModelToolCall `json:"toolCalls,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	Name       string          `json:"name,omitempty"`
}

type ModelToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ModelTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ModelRequest struct {
	Messages        []ModelMessage `json:"messages"`
	Tools           []ModelTool    `json:"tools,omitempty"`
	Temperature     float64        `json:"temperature,omitempty"`
	MaxOutputTokens int            `json:"maxOutputTokens,omitempty"`
}

type ModelUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

type ModelResponse struct {
	Role         string          `json:"role"`
	Content      string          `json:"content"`
	ToolCalls    []ModelToolCall `json:"toolCalls"`
	FinishReason string          `json:"finishReason"`
	Usage        ModelUsage      `json:"usage"`
}

type ModelStreamEvent struct {
	Type         string         `json:"type"`
	Content      string         `json:"content,omitempty"`
	ToolCall     *ModelToolCall `json:"toolCall,omitempty"`
	FinishReason string         `json:"finishReason,omitempty"`
	Usage        *ModelUsage    `json:"usage,omitempty"`
}

type ModelError struct {
	Code      string `json:"code"`
	Status    int    `json:"status"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type NativeModelRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

type ModelAdapter interface {
	AdapterID() string
	BuildRequest(ModelRequest, bool) (NativeModelRequest, error)
	NormalizeResponse([]byte) (ModelResponse, error)
	NormalizeStream([]json.RawMessage) ([]ModelStreamEvent, error)
	NormalizeError(int, []byte) ModelError
}

type modelAdapterBase struct {
	profile EffectiveModelProfile
	id      string
}

func (a modelAdapterBase) AdapterID() string {
	return a.id
}

func (a modelAdapterBase) requestHeaders() map[string]string {
	headers := map[string]string{"Content-Type": "application/json"}
	for name, value := range a.profile.ExtraHeaders {
		headers[name] = value
	}
	return headers
}

func (a modelAdapterBase) normalizeError(status int, body []byte) ModelError {
	if status == http.StatusTooManyRequests {
		return ModelError{
			Code:      "rate_limit",
			Status:    status,
			Message:   "rate limited",
			Retryable: true,
		}
	}
	message := providerErrorMessage(body)
	if a.profile.RuntimeAuth.APIKey != "" {
		message = strings.ReplaceAll(message, a.profile.RuntimeAuth.APIKey, "[REDACTED]")
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ModelError{Code: "authentication", Status: status, Message: "authentication failed"}
	}
	if status >= 500 {
		return ModelError{
			Code:      "provider_unavailable",
			Status:    status,
			Message:   "provider unavailable",
			Retryable: true,
		}
	}
	return ModelError{Code: "provider_error", Status: status, Message: message}
}

func NewModelAdapter(profile EffectiveModelProfile) (ModelAdapter, error) {
	if err := validateEffectiveModelProfile(profile); err != nil {
		return nil, err
	}
	switch profile.ProviderType {
	case ProviderOpenAICompatible:
		return newOpenAICompatibleAdapter(profile, "openai-compatible"), nil
	case ProviderAnthropicNative:
		return newAnthropicAdapter(profile), nil
	case ProviderGeminiNative:
		return newGeminiAdapter(profile), nil
	case ProviderOllama:
		return newOpenAICompatibleAdapter(profile, "ollama"), nil
	case ProviderLMStudio:
		return newOpenAICompatibleAdapter(profile, "lm-studio"), nil
	case ProviderCustomLocal:
		return newOpenAICompatibleAdapter(profile, "openai-compatible"), nil
	default:
		return nil, fmt.Errorf("unsupported model provider %q", profile.ProviderType)
	}
}

func validateEffectiveModelProfile(profile EffectiveModelProfile) error {
	if strings.TrimSpace(profile.ProfileID) == "" || strings.TrimSpace(profile.Model) == "" {
		return errors.New("effective model profile id and model are required")
	}
	if profile.TimeoutMS < 1000 || profile.TimeoutMS > 600000 {
		return errors.New("effective model profile timeout is out of range")
	}
	if profile.RuntimeAuth.Mode != RuntimeAuthInlineStdin && profile.RuntimeAuth.Mode != RuntimeAuthNone {
		return errors.New("effective model profile runtime auth mode is invalid")
	}
	if profile.RuntimeAuth.Mode == RuntimeAuthNone && profile.RuntimeAuth.APIKey != "" {
		return errors.New("runtime auth key requires inline-stdin mode")
	}
	expectedAdapterID := map[ProviderType]string{
		ProviderOpenAICompatible: "openai-compatible",
		ProviderAnthropicNative:  "anthropic-native",
		ProviderGeminiNative:     "gemini-native",
		ProviderOllama:           "ollama",
		ProviderLMStudio:         "lm-studio",
		ProviderCustomLocal:      "openai-compatible",
	}[profile.ProviderType]
	if expectedAdapterID != "" && profile.AdapterID != expectedAdapterID {
		return fmt.Errorf(
			"effective model adapter %q does not match provider %q",
			profile.AdapterID,
			profile.ProviderType,
		)
	}
	return nil
}

func marshalNativeRequest(url string, headers map[string]string, body any) (NativeModelRequest, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return NativeModelRequest{}, err
	}
	return NativeModelRequest{
		Method:  http.MethodPost,
		URL:     url,
		Headers: headers,
		Body:    data,
	}, nil
}

func parseArguments(value string) map[string]any {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return map[string]any{}
	}
	return parsed
}

func stringifyArguments(value any) string {
	if value == nil {
		return "{}"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func normalizeFinishReason(value string, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_calls"
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "tool_calls", "tool_use", "function_call", "function_calls":
		return "tool_calls"
	case "length", "max_tokens", "max_token":
		return "max_tokens"
	case "stop", "end_turn", "completed":
		return "stop"
	case "content_filter", "safety", "blocked":
		return "content_filter"
	default:
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return "unknown"
		}
		return value
	}
}

func providerErrorMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if payload.Error.Message != "" {
			return payload.Error.Message
		}
		if payload.Message != "" {
			return payload.Message
		}
	}
	if len(body) == 0 {
		return "provider error"
	}
	return string(body)
}
