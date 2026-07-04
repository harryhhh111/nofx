package kernel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"nofx/mcp"
)

func TestValidateCompiledStrategyNormalizesPositiveShortThreshold(t *testing.T) {
	result := &StrategyCompileResult{
		StrategyMode: "scoring",
		Rules:        []StrategyRule{},
		ScoringConfig: &ScoringStrategy{
			Enabled: true,
			SelectedFactors: []string{
				"trend",
				"momentum",
			},
			FactorWeights: map[string]float64{
				"trend":    0.5,
				"momentum": 0.5,
			},
			LongThreshold:           70,
			ShortThreshold:          30,
			MinAvailableWeightRatio: 0.5,
			MinConfidence:           70,
			Timeframe:               "15m",
			Execution: RuleExecution{
				Leverage:        2,
				PositionSizeUSD: 12,
			},
		},
	}

	if err := validateCompiledStrategy(result); err != nil {
		t.Fatalf("validateCompiledStrategy returned error: %v", err)
	}
	if result.ScoringConfig.ShortThreshold != -30 {
		t.Fatalf("expected short threshold to normalize to -30, got %v", result.ScoringConfig.ShortThreshold)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected normalization warning")
	}
}

func TestBuildStrategyCompileLLMRequestSkipsResponseFormatForDeepSeek(t *testing.T) {
	client := &testCompileClient{
		base: &mcp.Client{
			Provider: mcp.ProviderDeepSeek,
			BaseURL:  mcp.DefaultDeepSeekBaseURL,
			Model:    mcp.DefaultDeepSeekModel,
		},
	}

	req, err := buildStrategyCompileLLMRequest(context.Background(), client, "system", "user")
	if err != nil {
		t.Fatalf("buildStrategyCompileLLMRequest returned error: %v", err)
	}
	if req.ResponseFormat != nil {
		t.Fatalf("expected DeepSeek native request to omit response_format, got %#v", req.ResponseFormat)
	}
}

func TestBuildStrategyCompileLLMRequestKeepsResponseFormatForOpenAI(t *testing.T) {
	client := &testCompileClient{
		base: &mcp.Client{
			Provider: mcp.ProviderOpenAI,
			BaseURL:  "https://api.openai.com/v1",
			Model:    "gpt-4o-mini",
		},
	}

	req, err := buildStrategyCompileLLMRequest(context.Background(), client, "system", "user")
	if err != nil {
		t.Fatalf("buildStrategyCompileLLMRequest returned error: %v", err)
	}
	if req.ResponseFormat == nil {
		t.Fatal("expected OpenAI request to include response_format")
	}
}

func TestPrepareOpenRouterStructuredRequestUsesStrictRouting(t *testing.T) {
	client := &testCompileClient{
		base: &mcp.Client{
			Provider: mcp.ProviderOpenAI,
			BaseURL:  "https://openrouter.ai/api/v1",
			Model:    "openai/gpt-5.4",
		},
	}

	req := &mcp.Request{}
	prepareOpenRouterStructuredRequest(client, req)
	if !req.MinimalParameters {
		t.Fatal("expected OpenRouter structured output request to use minimal parameters")
	}
	if got := req.Provider["require_parameters"]; got != true {
		t.Fatalf("expected require_parameters=true, got %#v", req.Provider)
	}
}

func TestStrategyCompileResponseFormatUsesOpenAIStrictObjects(t *testing.T) {
	format := strategyCompileResponseFormat()
	jsonSchema, ok := format["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("response format missing json_schema: %#v", format)
	}
	schema, ok := jsonSchema["schema"].(map[string]any)
	if !ok {
		t.Fatalf("response format missing schema: %#v", jsonSchema)
	}
	assertOpenAIStrictObjects(t, "schema", schema)
}

func assertOpenAIStrictObjects(t *testing.T, path string, node any) {
	t.Helper()

	switch v := node.(type) {
	case map[string]any:
		if schemaType, _ := v["type"].(string); schemaType == "object" {
			properties, _ := v["properties"].(map[string]any)
			if len(properties) > 0 {
				if additional, ok := v["additionalProperties"].(bool); !ok || additional {
					t.Fatalf("%s must set additionalProperties=false", path)
				}
				required, err := requiredStringSet(v["required"])
				if err != nil {
					t.Fatalf("%s has invalid required: %v", path, err)
				}
				for key := range properties {
					if !required[key] {
						t.Fatalf("%s required is missing property %q", path, key)
					}
				}
				for key := range required {
					if _, ok := properties[key]; !ok {
						t.Fatalf("%s required includes extra key %q", path, key)
					}
				}
			}
		}
		if properties, ok := v["properties"].(map[string]any); ok {
			for key, child := range properties {
				assertOpenAIStrictObjects(t, path+".properties."+key, child)
			}
		}
		if items, ok := v["items"]; ok {
			assertOpenAIStrictObjects(t, path+".items", items)
		}
		if variants, ok := v["anyOf"].([]any); ok {
			for i, variant := range variants {
				assertOpenAIStrictObjects(t, fmt.Sprintf("%s.anyOf.%d", path, i), variant)
			}
		}
	case []any:
		for i, child := range v {
			assertOpenAIStrictObjects(t, fmt.Sprintf("%s.%d", path, i), child)
		}
	}
}

func requiredStringSet(value any) (map[string]bool, error) {
	required, ok := value.([]string)
	if !ok {
		raw, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("must be a string array")
		}
		required = make([]string, 0, len(raw))
		for _, item := range raw {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("contains non-string item %#v", item)
			}
			required = append(required, s)
		}
	}
	out := make(map[string]bool, len(required))
	for _, key := range required {
		out[key] = true
	}
	return out, nil
}

type testCompileClient struct {
	base *mcp.Client
}

func (c *testCompileClient) BaseClient() *mcp.Client { return c.base }
func (c *testCompileClient) SetAPIKey(apiKey string, customURL string, customModel string) {
}
func (c *testCompileClient) SetTimeout(timeout time.Duration) {}
func (c *testCompileClient) CallWithMessages(systemPrompt, userPrompt string) (string, error) {
	return "", nil
}
func (c *testCompileClient) CallWithRequest(req *mcp.Request) (string, error) {
	return "", nil
}
func (c *testCompileClient) CallWithRequestStream(req *mcp.Request, onChunk func(string)) (string, error) {
	return "", nil
}
func (c *testCompileClient) CallWithRequestFull(req *mcp.Request) (*mcp.LLMResponse, error) {
	return nil, nil
}

func TestParseStrategyCompileResponseRequiresStrictJSON(t *testing.T) {
	text := `
{
  "strategy_mode": "scoring",
  "rules": [],
  "scoring_config": {
    "enabled": true,
    "selected_factors": ["trend"],
    "factor_weights": {"trend": 1},
    "long_threshold": 70,
    "short_threshold": -70,
    "min_available_weight_ratio": 0.5,
    "min_confidence": 70,
    "timeframe": "15m",
    "execution": {
      "leverage": 2,
      "position_size_usd": 12
    }
  },
  "warnings": [],
  "errors": []
}
`

	result, err := parseStrategyCompileResponse(text)
	if err != nil {
		t.Fatalf("parseStrategyCompileResponse returned error: %v", err)
	}
	if result.StrategyMode != "scoring" {
		t.Fatalf("expected scoring mode, got %q", result.StrategyMode)
	}
}

func TestParseStrategyCompileResponseAcceptsCompleteJSONFence(t *testing.T) {
	text := "```json\n" + `
{
  "strategy_mode": "scoring",
  "rules": [],
  "scoring_config": {
    "enabled": true,
    "selected_factors": ["trend"],
    "factor_weights": {"trend": 1},
    "long_threshold": 70,
    "short_threshold": -70,
    "min_available_weight_ratio": 0.5,
    "min_confidence": 70,
    "timeframe": "15m",
    "execution": {
      "leverage": 2,
      "position_size_usd": 12
    }
  },
  "warnings": [],
  "errors": []
}
` + "```"

	result, err := parseStrategyCompileResponse(text)
	if err != nil {
		t.Fatalf("parseStrategyCompileResponse returned error: %v", err)
	}
	if result.StrategyMode != "scoring" {
		t.Fatalf("expected scoring mode, got %q", result.StrategyMode)
	}
}

func TestParseStrategyCompileResponseRejectsIncompleteJSONFence(t *testing.T) {
	_, err := parseStrategyCompileResponse("```json\n{\"strategy_mode\":\"scoring\"")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "incomplete or invalid") {
		t.Fatalf("expected incomplete fenced JSON error, got %v", err)
	}
}

func TestParseStrategyCompileResponseReportsNonJSONPreview(t *testing.T) {
	_, err := parseStrategyCompileResponse("ânot jsonâ")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "structured output was not valid JSON") {
		t.Fatalf("expected structured output JSON error, got %v", err)
	}
}
