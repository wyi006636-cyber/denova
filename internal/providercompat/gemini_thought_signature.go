package providercompat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const geminiThoughtSignatureExtraKey = "gemini_thought_signature"

type geminiThoughtSignaturePolyfill struct{}

func (geminiThoughtSignaturePolyfill) apply(inner model.ToolCallingChatModel) model.ToolCallingChatModel {
	return &geminiThoughtSignatureModel{inner: inner}
}

type geminiThoughtSignatureModel struct {
	inner model.ToolCallingChatModel
}

func (m *geminiThoughtSignatureModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return m.inner.Generate(ctx, in, geminiThoughtSignatureOptions(opts)...)
}

func (m *geminiThoughtSignatureModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.inner.Stream(ctx, in, geminiThoughtSignatureOptions(opts)...)
}

func (m *geminiThoughtSignatureModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	inner, err := m.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &geminiThoughtSignatureModel{inner: inner}, nil
}

func geminiThoughtSignatureOptions(opts []model.Option) []model.Option {
	result := make([]model.Option, 0, len(opts)+3)
	result = append(result, opts...)
	result = append(result,
		openai.WithRequestPayloadModifier(injectGeminiThoughtSignatures),
		openai.WithResponseMessageModifier(captureGeminiThoughtSignatures),
		openai.WithResponseChunkMessageModifier(func(_ context.Context, msg *schema.Message, rawBody []byte, _ bool) (*schema.Message, error) {
			return captureGeminiThoughtSignatures(context.Background(), msg, rawBody)
		}),
	)
	return result
}

type geminiResponseToolCall struct {
	Index        *int   `json:"index"`
	ID           string `json:"id"`
	ExtraContent struct {
		Google struct {
			ThoughtSignature string `json:"thought_signature"`
		} `json:"google"`
	} `json:"extra_content"`
}

func captureGeminiThoughtSignatures(_ context.Context, msg *schema.Message, rawBody []byte) (*schema.Message, error) {
	if msg == nil || len(msg.ToolCalls) == 0 || len(rawBody) == 0 {
		return msg, nil
	}
	var response struct {
		Choices []struct {
			Message struct {
				ToolCalls []geminiResponseToolCall `json:"tool_calls"`
			} `json:"message"`
			Delta struct {
				ToolCalls []geminiResponseToolCall `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawBody, &response); err != nil || len(response.Choices) == 0 {
		return msg, nil
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) == 0 {
		calls = response.Choices[0].Delta.ToolCalls
	}
	for position, call := range calls {
		signature := call.ExtraContent.Google.ThoughtSignature
		if signature == "" {
			continue
		}
		if target := matchingGeminiToolCall(msg.ToolCalls, call, position); target != nil {
			if target.Extra == nil {
				target.Extra = map[string]any{}
			}
			target.Extra[geminiThoughtSignatureExtraKey] = signature
		}
	}
	return msg, nil
}

func matchingGeminiToolCall(calls []schema.ToolCall, source geminiResponseToolCall, position int) *schema.ToolCall {
	for i := range calls {
		if source.ID != "" && calls[i].ID == source.ID {
			return &calls[i]
		}
		if source.Index != nil && calls[i].Index != nil && *calls[i].Index == *source.Index {
			return &calls[i]
		}
	}
	if position >= 0 && position < len(calls) {
		return &calls[position]
	}
	return nil
}

func injectGeminiThoughtSignatures(_ context.Context, messages []*schema.Message, rawBody []byte) ([]byte, error) {
	hasSignature := false
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			if signature, ok := call.Extra[geminiThoughtSignatureExtraKey].(string); ok && signature != "" {
				hasSignature = true
				break
			}
		}
	}
	if !hasSignature {
		return rawBody, nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil, fmt.Errorf("decode Gemini request payload: %w", err)
	}
	var requestMessages []map[string]json.RawMessage
	if err := json.Unmarshal(payload["messages"], &requestMessages); err != nil {
		return nil, fmt.Errorf("decode Gemini request messages: %w", err)
	}
	if len(requestMessages) != len(messages) {
		return nil, fmt.Errorf("Gemini request message count = %d, want %d", len(requestMessages), len(messages))
	}

	for i, message := range messages {
		if message == nil || len(message.ToolCalls) == 0 {
			continue
		}
		var requestCalls []map[string]any
		if err := json.Unmarshal(requestMessages[i]["tool_calls"], &requestCalls); err != nil {
			return nil, fmt.Errorf("decode Gemini tool calls for message %d: %w", i, err)
		}
		for position, call := range message.ToolCalls {
			signature, ok := call.Extra[geminiThoughtSignatureExtraKey].(string)
			if !ok || signature == "" {
				continue
			}
			target := matchingRequestToolCall(requestCalls, call.ID, position)
			if target == nil {
				return nil, fmt.Errorf("Gemini request tool call %q not found", call.ID)
			}
			extraContent, _ := (*target)["extra_content"].(map[string]any)
			if extraContent == nil {
				extraContent = map[string]any{}
				(*target)["extra_content"] = extraContent
			}
			google, _ := extraContent["google"].(map[string]any)
			if google == nil {
				google = map[string]any{}
				extraContent["google"] = google
			}
			google["thought_signature"] = signature
		}
		encodedCalls, err := json.Marshal(requestCalls)
		if err != nil {
			return nil, fmt.Errorf("encode Gemini tool calls for message %d: %w", i, err)
		}
		requestMessages[i]["tool_calls"] = encodedCalls
	}
	encodedMessages, err := json.Marshal(requestMessages)
	if err != nil {
		return nil, fmt.Errorf("encode Gemini request messages: %w", err)
	}
	payload["messages"] = encodedMessages
	return json.Marshal(payload)
}

func matchingRequestToolCall(calls []map[string]any, id string, position int) *map[string]any {
	for i := range calls {
		if requestID, _ := calls[i]["id"].(string); id != "" && requestID == id {
			return &calls[i]
		}
	}
	if position >= 0 && position < len(calls) {
		return &calls[position]
	}
	return nil
}
