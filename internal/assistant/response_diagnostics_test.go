package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func responseDiagnosticEnvelope(model, finish, content string) string {
	encoded, _ := json.Marshal(map[string]any{
		"model": model,
		"choices": []any{map[string]any{
			"finish_reason": finish,
			"message":       map[string]any{"content": content},
		}},
		"usage": map[string]int{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30},
	})
	return string(encoded)
}

func TestOpenRouterResponseDiagnosticsAreClosedAndDoNotExposeProviderData(t *testing.T) {
	const model = "test/first:free"
	const privateContent = "PRIVATE_FINANCIAL_CONTENT_498612"
	const privateModel = "PRIVATE_RETURNED_MODEL_81579\nforged_log=true"
	const privateIntent = "PRIVATE_UNSUPPORTED_INTENT_72153"
	validIntent := `{"intent":"general","reply":"` + privateContent + `"}`
	for _, test := range []struct {
		name, body, reason string
	}{
		{"decode", `{"model":"` + privateContent, "response_decode_failed"},
		{"choices", `{"model":"` + privateContent + `","choices":[]}`, "missing_choices"},
		{"truncated", responseDiagnosticEnvelope(model, "length", validIntent), "output_truncated"},
		{"empty", responseDiagnosticEnvelope(privateModel, "stop", "  \n\t"), "empty_content"},
		{"identity", responseDiagnosticEnvelope(privateModel, "stop", validIntent), "model_mismatch"},
		{"intent JSON", responseDiagnosticEnvelope(model, "stop", privateContent), "intent_json_invalid"},
		{"intent array", responseDiagnosticEnvelope(model, "stop", `["`+privateContent+`"]`), "intent_json_invalid"},
		{"intent null", responseDiagnosticEnvelope(model, "stop", "null"), "intent_json_invalid"},
		{"intent unsupported", responseDiagnosticEnvelope(model, "stop", `{"intent":"`+privateIntent+`","reply":"`+privateContent+`"}`), "intent_missing_or_unsupported"},
		{"intent missing", responseDiagnosticEnvelope(model, "stop", `{"reply":"`+privateContent+`"}`), "intent_missing_or_unsupported"},
		{"filtered", responseDiagnosticEnvelope(model, "content_filter", validIntent), "intent_content_filtered"},
		{"provider error", responseDiagnosticEnvelope(model, "error", validIntent), "intent_provider_error"},
		{"tool calls", responseDiagnosticEnvelope(model, "tool_calls", validIntent), "intent_tool_calls"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					writeTestCatalog(w)
					return
				}
				calls.Add(1)
				_, _ = io.WriteString(w, test.body)
			})
			var logs bytes.Buffer
			previousOutput := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previousOutput) })
			generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), model)
			if err != nil {
				t.Fatal(err)
			}
			text, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent"})
			var typed OpenRouterError
			if text != "" || !errors.As(err, &typed) || typed.Code != "assistant_invalid_response" || typed.StatusCode != http.StatusBadGateway || typed.RetryAfterSeconds != 15 || typed.Global {
				t.Fatalf("invalid response changed public error contract: text=%q error=%v", text, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("diagnostics spent extra completion attempts: %d", calls.Load())
			}
			output := logs.String()
			if !strings.Contains(output, "invalid_response stage=assistant_intent reason="+test.reason) {
				t.Fatalf("expected closed reason %q, got %q", test.reason, output)
			}
			for _, secret := range []string{privateContent, "PRIVATE_RETURNED_MODEL", "forged_log", privateIntent, "test-openrouter-secret", "Authorization", "Bearer"} {
				if strings.Contains(output, secret) {
					t.Fatalf("private provider data escaped into diagnostics: marker=%s", secret)
				}
			}
		})
	}
}

func TestOpenRouterResponseFinishReasonsPreserveCompatibleStages(t *testing.T) {
	const model = "test/first:free"
	const privateFinish = "PRIVATE_FINISH_REASON_4936\nforged_log=true"
	for _, test := range []struct {
		name, stage, finish, content string
	}{
		{"stop", "assistant_intent", "stop", `{"intent":"general","reply":"Ola"}`},
		{"mock missing finish", "assistant_intent", "", `{"intent":"general","reply":"Ola"}`},
		{"unknown finish remains unlogged", "assistant_intent", privateFinish, `{"intent":"general","reply":"Ola"}`},
		{"other stage filter", "assistant_summarize_tool", "content_filter", "Texto de leitura"},
		{"other stage error", "assistant_summarize_tool", "error", "Texto de leitura"},
		{"other stage tools", "assistant_summarize_tool", "tool_calls", "Texto de leitura"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					writeTestCatalog(w)
					return
				}
				calls.Add(1)
				_, _ = io.WriteString(w, responseDiagnosticEnvelope(model, test.finish, test.content))
			})
			var logs bytes.Buffer
			previousOutput := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previousOutput) })
			generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), model)
			if err != nil {
				t.Fatal(err)
			}
			text, err := generator.generateText(GeminiRequest{DebugStage: test.stage})
			if err != nil || text != test.content || calls.Load() != 1 {
				t.Fatalf("compatible response changed: text=%q err=%v attempts=%d", text, err, calls.Load())
			}
			if strings.Contains(logs.String(), "invalid_response") || strings.Contains(logs.String(), "PRIVATE_FINISH") || strings.Contains(logs.String(), "forged_log") {
				t.Fatal("successful response emitted an invalid or unsafe diagnostic")
			}
		})
	}
	if got := routerInvalidResponseReason(255).String(); got != "invalid_response" {
		t.Fatalf("unexpected diagnostic values must remain a fixed fallback: %q", got)
	}
}
