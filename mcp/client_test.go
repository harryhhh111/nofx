package mcp

import "testing"

func TestBuildRequestBodyFromRequestUsesOpenRouterCompatibleParameters(t *testing.T) {
	client := NewClient(
		WithProvider(ProviderOpenAI),
		WithBaseURL("https://openrouter.ai/api/v1"),
		WithModel("openai/gpt-5.4"),
		WithMaxTokens(4096),
	).(*Client)

	maxTokens := 12000
	temperature := 0.0
	req := &Request{
		Model:             "openai/gpt-5.4",
		Messages:          []Message{NewUserMessage("compile this strategy")},
		MaxTokens:         &maxTokens,
		Temperature:       &temperature,
		MinimalParameters: true,
		Provider:          map[string]any{"require_parameters": true},
		ResponseFormat:    map[string]any{"type": "json_schema"},
	}

	body := client.BuildRequestBodyFromRequest(req)
	if _, ok := body["temperature"]; ok {
		t.Fatalf("OpenRouter request must omit unsupported temperature, got body %#v", body)
	}
	if got := body["max_tokens"]; got != maxTokens {
		t.Fatalf("OpenRouter request max_tokens = %#v, want %d", got, maxTokens)
	}
	if _, ok := body["max_completion_tokens"]; ok {
		t.Fatalf("OpenRouter request must not use max_completion_tokens: %#v", body)
	}
}

func TestBuildRequestBodyFromRequestOmitsSamplingControlsForMinimalParameters(t *testing.T) {
	client := NewClient(
		WithProvider(ProviderOpenAI),
		WithBaseURL("https://openrouter.ai/api/v1"),
		WithModel("openai/gpt-5.4"),
	).(*Client)

	maxTokens := 12000
	temperature := 0.0
	topP := 0.9
	frequencyPenalty := 0.1
	presencePenalty := 0.2
	req := &Request{
		Model:             "openai/gpt-5.4",
		Messages:          []Message{NewUserMessage("compile this strategy")},
		MaxTokens:         &maxTokens,
		Temperature:       &temperature,
		TopP:              &topP,
		FrequencyPenalty:  &frequencyPenalty,
		PresencePenalty:   &presencePenalty,
		Stop:              []string{"END"},
		MinimalParameters: true,
		ResponseFormat:    map[string]any{"type": "json_schema"},
		Provider:          map[string]any{"require_parameters": true},
	}

	body := client.BuildRequestBodyFromRequest(req)
	for _, key := range []string{"temperature", "top_p", "frequency_penalty", "presence_penalty", "stop"} {
		if _, ok := body[key]; ok {
			t.Fatalf("minimal request must omit %s: %#v", key, body)
		}
	}
	if got := body["max_tokens"]; got != maxTokens {
		t.Fatalf("minimal request max_tokens = %#v, want %d", got, maxTokens)
	}
	if body["response_format"] == nil {
		t.Fatalf("minimal request must keep response_format: %#v", body)
	}
	if body["provider"] == nil {
		t.Fatalf("minimal request must keep provider routing preferences: %#v", body)
	}
}

func TestBuildRequestBodyFromRequestKeepsOpenAIParametersForNativeOpenAI(t *testing.T) {
	client := NewClient(
		WithProvider(ProviderOpenAI),
		WithBaseURL("https://api.openai.com/v1"),
		WithModel("gpt-5.4"),
	).(*Client)

	maxTokens := 12000
	temperature := 0.0
	req := &Request{
		Model:       "gpt-5.4",
		Messages:    []Message{NewUserMessage("compile this strategy")},
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
	}

	body := client.BuildRequestBodyFromRequest(req)
	if got := body["temperature"]; got != temperature {
		t.Fatalf("native OpenAI request temperature = %#v, want %v", got, temperature)
	}
	if got := body["max_completion_tokens"]; got != maxTokens {
		t.Fatalf("native OpenAI request max_completion_tokens = %#v, want %d", got, maxTokens)
	}
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("native OpenAI request should use max_completion_tokens: %#v", body)
	}
}
