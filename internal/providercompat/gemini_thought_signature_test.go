package providercompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestWrapGeminiPreservesThoughtSignatureAcrossToolStep(t *testing.T) {
	var calls atomic.Int32
	requestBodies := make(chan []byte, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		requestBodies <- body
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, "data: {\"id\":\"resp-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gemini-3.5-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"write_todos\",\"arguments\":\"{}\"},\"extra_content\":{\"google\":{\"thought_signature\":\"signature-abc\"}}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"id\":\"resp-2\",\"object\":\"chat.completion.chunk\",\"created\":2,\"model\":\"gemini-3.5-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"继续回复\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	cfg := openai.ChatModelConfig{
		APIKey:     "test-key",
		BaseURL:    server.URL + "/v1",
		Model:      "gemini-3.5-flash",
		HTTPClient: server.Client(),
	}
	inner, err := openai.NewChatModel(context.Background(), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := Wrap(inner, cfg)
	userMessage := schema.UserMessage("先调用工具，再继续回复")
	first, err := collectProviderCompatStream(wrapped, []*schema.Message{userMessage})
	if err != nil {
		t.Fatal(err)
	}
	<-requestBodies
	if len(first.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want one", first.ToolCalls)
	}
	if got := first.ToolCalls[0].Extra["gemini_thought_signature"]; got != "signature-abc" {
		t.Fatalf("captured thought signature = %v, want signature-abc", got)
	}

	toolResult := schema.ToolMessage("todos updated", "call-1")
	second, err := collectProviderCompatStream(wrapped, []*schema.Message{userMessage, first, toolResult})
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := <-requestBodies
	if second.Content != "继续回复" {
		t.Fatalf("second response = %q, want 继续回复", second.Content)
	}

	var payload struct {
		Messages []struct {
			Role      string `json:"role"`
			ToolCalls []struct {
				ID           string `json:"id"`
				ExtraContent struct {
					Google struct {
						ThoughtSignature string `json:"thought_signature"`
					} `json:"google"`
				} `json:"extra_content"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(secondRequest, &payload); err != nil {
		t.Fatal(err)
	}
	var signature string
	for _, message := range payload.Messages {
		if message.Role == "assistant" && len(message.ToolCalls) == 1 {
			signature = message.ToolCalls[0].ExtraContent.Google.ThoughtSignature
		}
	}
	if signature != "signature-abc" {
		t.Fatalf("request thought signature = %q, want signature-abc", signature)
	}
}

func collectProviderCompatStream(chatModel interface {
	Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error)
}, messages []*schema.Message) (*schema.Message, error) {
	stream, err := chatModel.Stream(context.Background(), messages)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var chunks []*schema.Message
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return schema.ConcatMessages(chunks)
}
