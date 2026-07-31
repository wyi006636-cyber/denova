package yanzhouadapter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const modelContractFixtureDigest = "8971282b27aab70c868d561c85cad2fa24a99e313b99a6a7b2e081fd01ecfed7"
const modelContractSecret = "wp2-go-adapter-secret-sentinel"

type modelContractFixture struct {
	SchemaVersion string
	Request       ModelRequest
	Expected      modelContractExpected
	Providers     map[string]modelProviderFixture
}

type modelContractExpected struct {
	Response ModelResponse
	Stream   []ModelStreamEvent
	Error    ModelError
}

type modelProviderFixture struct {
	Response json.RawMessage
	Stream   []json.RawMessage
	Error    json.RawMessage
}

func loadModelContractFixture(t *testing.T) ([]byte, modelContractFixture) {
	t.Helper()
	data, err := os.ReadFile("testdata/model-adapter-contracts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture modelContractFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return data, fixture
}

func modelProfile(provider ProviderType) EffectiveModelProfile {
	adapterID := string(provider)
	if provider == ProviderCustomLocal {
		adapterID = string(ProviderOpenAICompatible)
	}
	auth := RuntimeAuth{Mode: RuntimeAuthInlineStdin, APIKey: modelContractSecret}
	if provider == ProviderOllama || provider == ProviderLMStudio {
		auth = RuntimeAuth{Mode: RuntimeAuthNone}
	}
	return EffectiveModelProfile{
		ProfileID:    "fixture-" + string(provider),
		ProviderType: provider,
		AdapterID:    adapterID,
		BaseURL:      "https://fixture.invalid",
		Model:        "fixture-model",
		TimeoutMS:    120000,
		RuntimeAuth:  auth,
	}
}

func TestModelContractFixtureDigest(t *testing.T) {
	data, fixture := loadModelContractFixture(t)
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != modelContractFixtureDigest {
		t.Fatalf("fixture digest = %s, want %s", got, modelContractFixtureDigest)
	}
	if fixture.SchemaVersion != "1" {
		t.Fatalf("fixture schema = %q, want 1", fixture.SchemaVersion)
	}
}

func TestModelAdapterFactoryKeepsNativeAndCompatibleProvidersDistinct(t *testing.T) {
	cases := []struct {
		provider ProviderType
		wantID   string
	}{
		{ProviderOpenAICompatible, "openai-compatible"},
		{ProviderAnthropicNative, "anthropic-native"},
		{ProviderGeminiNative, "gemini-native"},
		{ProviderOllama, "ollama"},
		{ProviderLMStudio, "lm-studio"},
		{ProviderCustomLocal, "openai-compatible"},
	}
	for _, tc := range cases {
		adapter, err := NewModelAdapter(modelProfile(tc.provider))
		if err != nil {
			t.Fatalf("%s: %v", tc.provider, err)
		}
		if got := adapter.AdapterID(); got != tc.wantID {
			t.Fatalf("%s adapter id = %q, want %q", tc.provider, got, tc.wantID)
		}
	}
	if _, err := NewModelAdapter(modelProfile(ProviderType("unknown-provider"))); err == nil {
		t.Fatal("unknown provider should fail closed")
	}
	mismatched := modelProfile(ProviderAnthropicNative)
	mismatched.AdapterID = "openai-compatible"
	if _, err := NewModelAdapter(mismatched); err == nil {
		t.Fatal("Anthropic native profile must not be routed through the compatible adapter")
	}
}

func TestModelAdaptersMatchSharedMessageToolStreamUsageFinishAndErrorContract(t *testing.T) {
	_, fixture := loadModelContractFixture(t)
	cases := []struct {
		provider   ProviderType
		fixtureKey string
	}{
		{ProviderOpenAICompatible, "openai-compatible"},
		{ProviderAnthropicNative, "anthropic-native"},
		{ProviderGeminiNative, "gemini-native"},
		{ProviderOllama, "openai-compatible"},
		{ProviderLMStudio, "openai-compatible"},
	}

	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			profile := modelProfile(tc.provider)
			adapter, err := NewModelAdapter(profile)
			if err != nil {
				t.Fatal(err)
			}
			providerFixture := fixture.Providers[tc.fixtureKey]
			response, err := adapter.NormalizeResponse(providerFixture.Response)
			if err != nil {
				t.Fatal(err)
			}
			if !modelResponseEqual(response, fixture.Expected.Response) {
				t.Fatalf("response mismatch:\n got %#v\nwant %#v", response, fixture.Expected.Response)
			}
			stream, err := adapter.NormalizeStream(providerFixture.Stream)
			if err != nil {
				t.Fatal(err)
			}
			if !modelStreamEqual(stream, fixture.Expected.Stream) {
				t.Fatalf("stream mismatch:\n got %#v\nwant %#v", stream, fixture.Expected.Stream)
			}
			errorBody := bytes.ReplaceAll(
				providerFixture.Error,
				[]byte("{{API_KEY}}"),
				[]byte(modelContractSecret),
			)
			normalizedError := adapter.NormalizeError(429, errorBody)
			if normalizedError != fixture.Expected.Error {
				t.Fatalf("error mismatch: got %#v want %#v", normalizedError, fixture.Expected.Error)
			}
			errorJSON, err := json.Marshal(normalizedError)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(errorJSON, []byte(modelContractSecret)) {
				t.Fatal("normalized error leaked the runtime credential")
			}

			request, err := adapter.BuildRequest(fixture.Request, false)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(request.URL, modelContractSecret) ||
				bytes.Contains(request.Body, []byte(modelContractSecret)) {
				t.Fatal("runtime credential must stay in native request headers")
			}
			assertNativeModelRequest(t, tc.provider, request)
		})
	}
}

func TestModelLocalAdapterErrorWithoutCredentialIsNotCorrupted(t *testing.T) {
	adapter, err := NewModelAdapter(modelProfile(ProviderOllama))
	if err != nil {
		t.Fatal(err)
	}
	normalized := adapter.NormalizeError(400, []byte(`{"error":{"message":"bad local request"}}`))
	if normalized.Message != "bad local request" {
		t.Fatalf("local error message = %q, want uncorrupted provider message", normalized.Message)
	}
}

func TestOpenAICompatibleAdapterAliasesAuthorReadTools(t *testing.T) {
	adapter, err := NewModelAdapter(modelProfile(ProviderOpenAICompatible))
	if err != nil {
		t.Fatal(err)
	}
	request, err := adapter.BuildRequest(ModelRequest{Tools: []ModelTool{{Name: "story.get_open_threads", InputSchema: map[string]any{"type": "object"}}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(request.Body, []byte("story_get_open_threads")) || bytes.Contains(request.Body, []byte("story.get_open_threads")) {
		t.Fatalf("tool alias request=%s", request.Body)
	}
	response, err := adapter.NormalizeResponse([]byte(`{"choices":[{"message":{"tool_calls":[{"id":"call-1","function":{"name":"story_get_open_threads","arguments":"{}"}}]}}]}`))
	if err != nil || len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "story.get_open_threads" {
		t.Fatalf("tool alias response=%#v err=%v", response, err)
	}
}

func assertNativeModelRequest(t *testing.T, provider ProviderType, request NativeModelRequest) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(request.Body, &body); err != nil {
		t.Fatal(err)
	}
	switch provider {
	case ProviderOpenAICompatible, ProviderOllama, ProviderLMStudio:
		messages, _ := body["messages"].([]any)
		if len(messages) != 4 || request.Headers["Authorization"] == "" {
			t.Fatalf("OpenAI-compatible request lost messages or auth: %#v", request)
		}
	case ProviderAnthropicNative:
		if body["system"] == nil || request.Headers["x-api-key"] != modelContractSecret {
			t.Fatalf("Anthropic request did not use Messages native semantics: %#v", request)
		}
		if request.Headers["Authorization"] != "" {
			t.Fatal("Anthropic native request must not use bearer compatibility auth")
		}
	case ProviderGeminiNative:
		if body["systemInstruction"] == nil || request.Headers["x-goog-api-key"] != modelContractSecret {
			t.Fatalf("Gemini request did not use generateContent native semantics: %#v", request)
		}
	}
}

func modelResponseEqual(left, right ModelResponse) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func modelStreamEqual(left, right []ModelStreamEvent) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}
