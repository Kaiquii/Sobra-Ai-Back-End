package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCatalogReasoningControlRequiresExplicitOptionalCapability(t *testing.T) {
	cases := []struct {
		id         string
		parameters []string
		metadata   any
		present    bool
		canDisable bool
	}{
		{"test/optional:free", []string{"reasoning"}, map[string]any{"mandatory": false}, true, true},
		{"test/mandatory:free", []string{"reasoning"}, map[string]any{"mandatory": true}, true, false},
		{"test/unknown-mandatory:free", []string{"reasoning"}, map[string]any{}, true, false},
		{"test/no-metadata:free", []string{"reasoning"}, nil, false, false},
		{"test/null-metadata:free", []string{"reasoning"}, nil, true, false},
		{"test/null-mandatory:free", []string{"reasoning"}, map[string]any{"mandatory": nil}, true, false},
		{"test/no-capability:free", []string{"max_tokens"}, map[string]any{"mandatory": false}, true, false},
		{"test/include-only:free", []string{"include_reasoning"}, map[string]any{"mandatory": false}, true, false},
		{"test/different-capability:free", []string{"reasoning.enabled"}, map[string]any{"mandatory": false}, true, false},
	}
	var catalogCalls atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		catalogCalls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" || r.Header.Get("Authorization") != "" {
			t.Error("reasoning capabilities must use the public catalog without authentication")
		}
		entries := make([]any, 0, len(cases))
		for _, test := range cases {
			entry := testCatalogEntry(test.id)
			entry["supported_parameters"] = test.parameters
			if test.present {
				entry["reasoning"] = test.metadata
			}
			entries = append(entries, entry)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": entries})
	})
	models, err := listFreeAssistantModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != len(cases) || catalogCalls.Load() != 1 {
		t.Fatalf("reasoning metadata must not filter eligible free models: count=%d calls=%d", len(models), catalogCalls.Load())
	}
	byID := make(map[string]AssistantModel, len(models))
	for _, model := range models {
		byID[model.ID] = model
		encoded, err := json.Marshal(model)
		if err != nil {
			t.Fatal(err)
		}
		var public map[string]any
		if err := json.Unmarshal(encoded, &public); err != nil || len(public) != 4 || public["id"] != model.ID {
			t.Fatalf("reasoning capability changed the public model contract: %s err=%v", encoded, err)
		}
	}
	for _, test := range cases {
		model, found := byID[test.id]
		if !found || model.CanDisableReasoning != test.canDisable {
			t.Errorf("capability for %s: found=%v canDisable=%v want=%v", test.id, found, model.CanDisableReasoning, test.canDisable)
		}
	}
}

func TestOpenRouterReasoningControlPayloadIsInterpreterOnly(t *testing.T) {
	for _, test := range []struct {
		name, stage string
		canDisable  bool
		present     bool
	}{
		{"optional interpreter", "assistant_intent", true, true},
		{"mandatory or unknown interpreter", "assistant_intent", false, false},
		{"other stage", "assistant_chat", true, false},
		{"unspecified stage", "", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := AssistantModel{ID: "test/optional:free", ContextLength: 32768, SupportsResponseFormat: true, SupportsMaxTokens: true, CanDisableReasoning: test.canDisable}
			payload, err := routerPayload(model, GeminiRequest{DebugStage: test.stage, Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "Ola"}}}}})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var serialized map[string]any
			if err := json.Unmarshal(encoded, &serialized); err != nil {
				t.Fatal(err)
			}
			raw, present := serialized["reasoning"]
			if present != test.present {
				t.Fatalf("reasoning presence=%v want=%v: %s", present, test.present, encoded)
			}
			if present {
				reasoning, ok := raw.(map[string]any)
				if !ok || len(reasoning) != 1 || reasoning["enabled"] != false {
					t.Fatalf("optional reasoning must be disabled explicitly, not merely hidden: %s", encoded)
				}
			}
			for _, key := range []string{"effort", "exclude", "reasoning_effort"} {
				if _, found := serialized[key]; found {
					t.Fatalf("unexpected reasoning substitute %q: %s", key, encoded)
				}
			}
		})
	}
}

func TestOpenRouterReasoningControlHTTPKeepsOneCallAndFreePrivateBudget(t *testing.T) {
	for _, scenario := range []string{"success", "invalid_json", "provider_error"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			payloads := make(chan map[string]any, 1)
			setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					optional := testCatalogEntry("test/optional:free")
					optional["supported_parameters"] = []string{"response_format", "max_tokens", "reasoning"}
					optional["reasoning"] = map[string]any{"mandatory": false}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{optional, testCatalogEntry("test/alternative:free")}})
					return
				}
				calls.Add(1)
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				select {
				case payloads <- payload:
				default:
					t.Error("message made more than one completion request")
				}
				switch scenario {
				case "invalid_json":
					writeRouterReply(w, "invalid JSON")
				case "provider_error":
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"error":{"code":503,"metadata":{"provider_name":"fixture"}}}`)
				default:
					writeRouterReply(w, `{"intent":"general","reply":"Ola"}`)
				}
			})
			ctx := withAssistantMessageBudget(context.Background())
			client, err := newAssistantGeneratorForModel(ctx, "test/optional:free")
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.generateText(GeminiRequest{DebugStage: "assistant_intent", Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "Ola"}}}}})
			if (err == nil) != (scenario == "success") || calls.Load() != 1 {
				t.Fatalf("reasoning changed per-message completion budget: calls=%d err=%v", calls.Load(), err)
			}
			var payload map[string]any
			select {
			case payload = <-payloads:
			default:
				t.Fatal("completion payload was not captured")
			}
			reasoning, ok := payload["reasoning"].(map[string]any)
			if !ok || len(reasoning) != 1 || reasoning["enabled"] != false {
				t.Fatalf("HTTP body did not serialize enabled=false: %+v", payload["reasoning"])
			}
			if payload["max_tokens"] != float64(2048) {
				t.Fatalf("interpreter output budget changed: %+v", payload)
			}
			provider, ok := payload["provider"].(map[string]any)
			if !ok || provider["data_collection"] != "deny" || provider["require_parameters"] != true || provider["allow_fallbacks"] != true {
				t.Fatalf("reasoning weakened privacy/provider safeguards: %+v", payload["provider"])
			}
			prices, ok := provider["max_price"].(map[string]any)
			if !ok || len(prices) != 3 || prices["prompt"] != float64(0) || prices["completion"] != float64(0) || prices["request"] != float64(0) {
				t.Fatalf("reasoning request can exceed free price ceiling: %+v", provider)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), `"effort"`) || strings.Contains(string(encoded), `"exclude"`) || strings.Contains(string(encoded), `"reasoning_effort"`) {
				t.Fatalf("HTTP body used a hiding/effort substitute: %s", encoded)
			}
			other, err := newAssistantGeneratorForModel(ctx, "test/alternative:free")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := other.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err == nil || calls.Load() != 1 {
				t.Fatalf("a second client bypassed the original message budget: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}
