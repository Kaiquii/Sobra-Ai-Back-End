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

func TestOpenRouterRejectsConflictingExpenseReadPlanBeforeSuccess(t *testing.T) {
	const selected = "test/first:free"
	const privateMarker = "PRIVATE_CONFLICTING_DRAFT_693482"
	const privateAmount = "846729.13"
	const conflicting = `{"intent":"create_expense","query":{"periods":[{"year":2026,"months":[10]}],"category_name":"Uber/99","payment_source":"","group_by":"installment","operation":"max","position":2,"limit":1},"description":"` + privateMarker + `","category_name":"` + privateMarker + `","amount":` + privateAmount + `,"payment_source":"Renda Extra","provided_fields":["description","category_name","amount","payment_source"]}`
	for _, format := range []string{"json_schema", "json_object", "prompt"} {
		t.Run(format, func(t *testing.T) {
			var calls atomic.Int32
			setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					entry := testCatalogEntry(selected)
					parameters := []string{"max_tokens", "temperature"}
					if format == "json_schema" {
						parameters = append(parameters, "structured_outputs")
					} else if format == "json_object" {
						parameters = append(parameters, "response_format")
					}
					entry["supported_parameters"] = parameters
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{entry, testCatalogEntry("test/second:free")}})
					return
				}
				calls.Add(1)
				var payload openRouterRequest
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload.Model == selected {
					if payload.MaxTokens != assistantIntentMaxOutputTokens {
						t.Errorf("intent allowance changed: %d", payload.MaxTokens)
					}
					if got, _ := payload.ResponseFormat["type"].(string); (format == "prompt" && got != "") || (format != "prompt" && got != format) {
						t.Errorf("response format mismatch: %q", got)
					}
					content := conflicting
					if format == "json_schema" {
						content = `{"action":` + content + `}`
					}
					writeRouterReply(w, content)
					return
				}
				writeRouterReply(w, `{"intent":"general","reply":"Posso ajudar com a consulta."}`)
			})
			var logs bytes.Buffer
			previousOutput := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previousOutput) })
			generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), selected)
			if err != nil {
				t.Fatal(err)
			}
			text, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent"})
			var typed OpenRouterError
			if text != "" || !errors.As(err, &typed) || typed.Code != "assistant_invalid_response" || typed.StatusCode != http.StatusBadGateway || typed.RetryAfterSeconds != 15 || typed.Global {
				t.Fatalf("conflicting response did not fail closed: text=%q error=%v", text, err)
			}
			client := generator.(*openRouterClient)
			if calls.Load() != 1 || client.modelUsed() != "" {
				t.Fatalf("conflicting intent was published or retried: calls=%d used=%q", calls.Load(), client.modelUsed())
			}
			if routerCooldownRemaining(client.account+":"+selected) == 0 || routerCooldownRemaining(client.account) != 0 {
				t.Fatal("conflicting response must cool down only the failed model")
			}
			if !strings.Contains(logs.String(), "invalid_response stage=assistant_intent reason=intent_conflicting_action") {
				t.Fatal("missing closed conflicting action diagnostic")
			}
			for _, marker := range []string{privateMarker, privateAmount, "test-openrouter-secret", "Authorization", "Bearer"} {
				if strings.Contains(logs.String(), marker) || strings.Contains(err.Error(), marker) {
					t.Fatal("conflicting provider content escaped into an error or diagnostic")
				}
			}
			// A new message may select the next verified model; this message may
			// neither repair the intent nor consume another completion attempt.
			if _, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err == nil || calls.Load() != 1 {
				t.Fatal("conflicting response bypassed the single-message budget")
			}
			next, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), selected)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := next.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err != nil || calls.Load() != 2 || next.(*openRouterClient).modelUsed() != "test/second:free" {
				t.Fatalf("next message did not skip the conflicting model: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestOpenRouterExpenseWithoutReadPlanRemainsCompatible(t *testing.T) {
	for _, format := range []string{"json_schema", "json_object", "prompt"} {
		for _, query := range []string{"absent", "null"} {
			t.Run(format+"/"+query, func(t *testing.T) {
				var calls atomic.Int32
				content := `{"intent":"create_expense","description":"Consulta","provided_fields":["description"]}`
				if query == "null" {
					content = strings.TrimSuffix(content, "}") + `,"query":null}`
				}
				if format == "json_schema" {
					content = `{"action":` + content + `}`
				}
				setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
					if request.Method == http.MethodGet {
						entry := testCatalogEntry("test/first:free")
						parameters := []string{"max_tokens"}
						if format == "json_schema" {
							parameters = append(parameters, "structured_outputs")
						} else if format == "json_object" {
							parameters = append(parameters, "response_format")
						}
						entry["supported_parameters"] = parameters
						_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{entry}})
						return
					}
					calls.Add(1)
					writeRouterReply(w, content)
				})
				generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "test/first:free")
				if err != nil {
					t.Fatal(err)
				}
				text, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent"})
				if err != nil || text == "" || calls.Load() != 1 || generator.(*openRouterClient).modelUsed() != "test/first:free" {
					t.Fatalf("compatible create response changed: calls=%d err=%v", calls.Load(), err)
				}
				intent, err := parseFinancialIntent(text)
				if err != nil || intent.Intent != "create_expense" || intent.provided["query"] != nil {
					t.Fatal("compatible expense or null query changed")
				}
			})
		}
	}
}

func TestOpenRouterRejectsExpenseWhenUserExplicitlyAskedRead(t *testing.T) {
	const privateMarker = "PRIVATE_UNREQUESTED_DRAFT_734281"
	const action = `{"intent":"create_expense","description":"` + privateMarker + `","amount":73921.65,"provided_fields":["description","amount"]}`
	for _, format := range []string{"json_schema", "json_object", "prompt"} {
		for _, test := range []struct {
			name, message string
			blocked       bool
		}{
			{"qual", "Qual foi meu segundo maior gasto parcelado?", true},
			{"quanto", "quanto gastei em outubro?", true},
			{"quais", "Quais parcelas cadastrei?", true},
			{"followup", "e QUAL foi o maior?", true},
			{"creation", "Gastei 15 reais no mercado", false},
			{"creation request", "Pode cadastrar?", false},
			{"word boundary", "Qualquer despesa que eu informar pode ser cadastrada", false},
			{"unicode word boundary", "qualé o nome da despesa", false},
			{"legacy no text", "", false},
		} {
			t.Run(format+"/"+test.name, func(t *testing.T) {
				var calls atomic.Int32
				setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
					if request.Method == http.MethodGet {
						entry := testCatalogEntry("test/first:free")
						parameters := []string{"max_tokens"}
						if format == "json_schema" {
							parameters = append(parameters, "structured_outputs")
						} else if format == "json_object" {
							parameters = append(parameters, "response_format")
						}
						entry["supported_parameters"] = parameters
						_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{entry}})
						return
					}
					calls.Add(1)
					content := action
					if format == "json_schema" {
						content = `{"action":` + content + `}`
					}
					writeRouterReply(w, content)
				})
				var logs bytes.Buffer
				previousOutput := log.Writer()
				log.SetOutput(&logs)
				t.Cleanup(func() { log.SetOutput(previousOutput) })
				generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "test/first:free")
				if err != nil {
					t.Fatal(err)
				}
				request := GeminiRequest{DebugStage: "assistant_intent"}
				if test.message != "" {
					request.Contents = []GeminiContent{
						{Role: "user", Parts: []GeminiPart{{Text: "Qual foi o gasto anterior?"}}},
						{Role: "model", Parts: []GeminiPart{{Text: "Posso consultar ou preparar uma despesa."}}},
						{Role: "user", Parts: []GeminiPart{{Text: test.message}}},
					}
				}
				text, err := generator.generateText(request)
				client := generator.(*openRouterClient)
				if calls.Load() != 1 {
					t.Fatalf("request consent guard spent extra attempts: %d", calls.Load())
				}
				if test.blocked {
					var typed OpenRouterError
					if text != "" || !errors.As(err, &typed) || typed.Code != "assistant_invalid_response" || typed.StatusCode != http.StatusBadGateway || typed.RetryAfterSeconds != 15 || typed.Global || client.modelUsed() != "" {
						t.Fatalf("unrequested creation was accepted: text=%q err=%v", text, err)
					}
					if !strings.Contains(logs.String(), "reason=intent_action_not_requested") || routerCooldownRemaining(client.account+":test/first:free") == 0 {
						t.Fatal("unrequested action lost the closed reason or model cooldown")
					}
				} else if err != nil || text == "" || client.modelUsed() != "test/first:free" {
					t.Fatalf("creation request or legacy caller was rejected: err=%v", err)
				}
				for _, marker := range []string{privateMarker, "73921.65", "test-openrouter-secret", "Authorization", "Bearer"} {
					if strings.Contains(logs.String(), marker) {
						t.Fatal("unrequested action content escaped into diagnostics")
					}
				}
			})
		}
	}
}

func TestRouterReadQuestionUsesLastUserAndWholeWord(t *testing.T) {
	for _, test := range []struct {
		text string
		want bool
	}{
		{"\t e\tQUANTO foi?", true}, {"Qual?", true}, {"quais: parcelas", true},
		{"qualquer", false}, {"qualidade", false}, {"qual_outra", false}, {"qual2", false},
		{"qualé", false}, {"e-qual", false}, {"Como cadastrar?", false}, {"Onde cadastro?", false},
	} {
		request := GeminiRequest{Contents: []GeminiContent{
			{Role: "user", Parts: []GeminiPart{{Text: test.text}}},
			{Role: "model", Parts: []GeminiPart{{Text: "Qual foi a pergunta?"}}},
		}}
		if got := routerLastUserRequestsRead(request); got != test.want {
			t.Errorf("whole-word read question %q: got %t want %t", test.text, got, test.want)
		}
	}
	if routerLastUserRequestsRead(GeminiRequest{}) || routerLastUserRequestsRead(GeminiRequest{Contents: []GeminiContent{{Role: "model", Parts: []GeminiPart{{Text: "Qual foi?"}}}}}) {
		t.Fatal("caller without user content acquired a read guard")
	}
}
