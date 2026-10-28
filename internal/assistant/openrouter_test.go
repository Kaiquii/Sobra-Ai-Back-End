package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type routerTestTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (transport routerTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Hostname() != "openrouter.ai" {
		return nil, fmt.Errorf("unexpected external request")
	}
	cloned := request.Clone(request.Context())
	cloned.URL.Scheme = transport.target.Scheme
	cloned.URL.Host = transport.target.Host
	return transport.base.RoundTrip(cloned)
}

func setupRouterTest(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "test-openrouter-secret")
	assistantModelCatalog.Lock()
	assistantModelCatalog.models = nil
	assistantModelCatalog.expiresAt = time.Time{}
	assistantModelCatalog.lastError = nil
	assistantModelCatalog.retryAt = time.Time{}
	assistantModelCatalog.fetching = nil
	assistantModelCatalog.Unlock()
	openRouterCooldowns.Lock()
	openRouterCooldowns.until = make(map[string]time.Time)
	openRouterCooldowns.Unlock()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	address, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	oldTransport := http.DefaultTransport
	http.DefaultTransport = routerTestTransport{base: oldTransport, target: address}
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
}

func testCatalogEntry(id string) map[string]any {
	return map[string]any{
		"id": id, "name": id, "context_length": 32768,
		"architecture":         map[string]any{"input_modalities": []string{"text"}, "output_modalities": []string{"text"}},
		"pricing":              map[string]string{"prompt": "0", "completion": "0"},
		"supported_parameters": []string{"response_format", "max_tokens", "temperature"},
	}
}

func writeTestCatalog(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
		testCatalogEntry("test/first:free"), testCatalogEntry("test/second:free"), testCatalogEntry("test/third:free"),
	}})
}

func writeRouterReply(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
		"message": map[string]any{"content": content}, "finish_reason": "stop",
	}}})
}

func TestOpenRouterFallbackKeepsEveryAttemptFreeAndPrivate(t *testing.T) {
	var attempted []string
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			writeTestCatalog(w)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-openrouter-secret" {
			t.Error("missing server authorization")
		}
		var payload openRouterRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		attempted = append(attempted, payload.Model)
		if payload.Provider["data_collection"] != "deny" || payload.Provider["require_parameters"] != true || payload.Provider["allow_fallbacks"] != true {
			t.Errorf("unsafe provider preferences: %+v", payload.Provider)
		}
		prices, ok := payload.Provider["max_price"].(map[string]any)
		if !ok || prices["prompt"] != float64(0) || prices["completion"] != float64(0) || prices["request"] != float64(0) {
			t.Errorf("cost ceiling missing: %+v", payload.Provider)
		}
		if payload.ResponseFormat["type"] != "json_object" || payload.MaxTokens != 2048 {
			t.Errorf("intent JSON format or output limit missing")
		}
		if len(attempted) == 1 {
			w.Header().Set("Retry-After", "42")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":429,"message":"Provider returned error","metadata":{"provider_name":"test-provider"}}}`)
			return
		}
		writeRouterReply(w, `{"intent":"general","reply":"Olá!"}`)
	})
	generator, err := newAssistantGeneratorForModel(context.Background(), "test/second:free")
	if err != nil {
		t.Fatal(err)
	}
	text, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent", Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "Oi"}}}}})
	if err != nil || !strings.Contains(text, "Olá") {
		t.Fatalf("fallback failed: %q %v", text, err)
	}
	if strings.Join(attempted, ",") != "test/second:free,test/first:free" {
		t.Fatalf("selection or fallback order wrong: %v", attempted)
	}
	client := generator.(*openRouterClient)
	if client.modelUsed() != "test/first:free" || routerCooldownRemaining(client.account+":test/second:free") < 40 {
		t.Fatal("successful model or per-model cooldown not retained")
	}
}

func TestOpenRouterGlobalQuotaStopsFallbackAndSharesCooldown(t *testing.T) {
	var calls int
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls++
		w.Header().Set("X-RateLimit-Limit", "50")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":429,"message":"test-openrouter-secret upstream content","metadata":{"error_type":"rate_limit_exceeded"}}}`)
	})
	first, err := newAssistantGenerator()
	if err != nil {
		t.Fatal(err)
	}
	_, err = first.generateText(GeminiRequest{})
	code, retry, ok := openRouterErrorInfo(err)
	if !ok || code != "assistant_quota_exceeded" || retry != 60 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("incorrect sanitized global quota error: %v", err)
	}
	second, err := newAssistantGeneratorForModel(context.Background(), "test/third:free")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.generateText(GeminiRequest{}); err == nil || calls != 1 {
		t.Fatalf("global quota must stop account-wide retry: calls=%d err=%v", calls, err)
	}
}

func TestOpenRouterMalformedIntentAndEmptyReplyUseNextFreeModel(t *testing.T) {
	var calls int
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls++
		switch calls {
		case 1:
			writeRouterReply(w, "not valid JSON")
		case 2:
			writeRouterReply(w, "")
		default:
			writeRouterReply(w, `{"intent":"general","reply":"Olá"}`)
		}
	})
	generator, err := newAssistantGenerator()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err != nil || calls != 3 {
		t.Fatalf("malformed output should use bounded fallback: calls=%d err=%v", calls, err)
	}
}

func TestOpenRouterMissingIntentUsesNextFreeModel(t *testing.T) {
	var calls int
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls++
		if calls == 1 {
			writeRouterReply(w, `{}`)
			return
		}
		writeRouterReply(w, `{"intent":"general","reply":"Olá"}`)
	})
	generator, err := newAssistantGenerator()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err != nil || calls != 2 {
		t.Fatalf("missing intent must not stop fallback: calls=%d err=%v", calls, err)
	}
}

func TestOpenRouterRemovedPreferenceCannotRequestAnUnapprovedModel(t *testing.T) {
	var requested string
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		var body openRouterRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requested = body.Model
		writeRouterReply(w, "Olá")
	})
	generator, err := newAssistantGeneratorForModel(context.Background(), "vendor/previously-free-now-paid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.generateText(GeminiRequest{}); err != nil || requested != "test/first:free" {
		t.Fatalf("stale preference must use approved recommendation: model=%q err=%v", requested, err)
	}
}

func TestOpenRouterStopsAfterAllApprovedModelsFail(t *testing.T) {
	var calls int
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	generator, err := newAssistantGenerator()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.generateText(GeminiRequest{}); err == nil || calls != 3 {
		t.Fatalf("fallback attempts must be bounded by approved models: calls=%d err=%v", calls, err)
	}
}

func TestOpenRouterNeverLogsProviderErrorContentOrKey(t *testing.T) {
	var output bytes.Buffer
	oldLog := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(oldLog) })
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":401,"message":"test-openrouter-secret and private-financial-message"}}`)
	})
	generator, err := newAssistantGenerator()
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.generateText(GeminiRequest{DebugStage: "private-financial-message"})
	if err == nil {
		t.Fatal("expected authorization error")
	}
	if strings.Contains(output.String()+err.Error(), "test-openrouter-secret") || strings.Contains(output.String()+err.Error(), "private-financial-message") {
		t.Fatal("provider error content or token leaked")
	}
}

func TestOpenRouterPaymentRequiredDoesNotTryAnotherModel(t *testing.T) {
	var calls int
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls++
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = io.WriteString(w, `{"error":{"code":402,"message":"insufficient credits"}}`)
	})
	generator, err := newAssistantGenerator()
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.generateText(GeminiRequest{})
	if code, _, ok := openRouterErrorInfo(err); !ok || code != "assistant_quota_exceeded" || calls != 1 {
		t.Fatalf("account credit errors cannot be fixed by changing models: %v calls=%d", err, calls)
	}
}

func TestOpenRouterCancelledRequestDoesNotCallAnyModel(t *testing.T) {
	var calls int
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls++
	})
	ctx, cancel := context.WithCancel(context.Background())
	generator, err := newAssistantGeneratorForModel(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_, err = generator.generateText(GeminiRequest{})
	if code, _, ok := openRouterErrorInfo(err); !ok || code != "assistant_request_cancelled" || calls != 0 {
		t.Fatalf("cancelled request reached provider: calls=%d err=%v", calls, err)
	}
}

func TestOpenRouterCooldownIsConcurrentSafeAndHonorsLongestDelay(t *testing.T) {
	setupRouterTest(t, func(http.ResponseWriter, *http.Request) {})
	var group sync.WaitGroup
	for i := range 24 {
		group.Add(1)
		go func(seconds int) {
			defer group.Done()
			setRouterCooldown("test-account", seconds)
			_ = routerCooldownRemaining("test-account")
		}(i + 1)
	}
	group.Wait()
	if routerCooldownRemaining("test-account") < 23 {
		t.Fatal("concurrent cooldown lost the longest retry delay")
	}
}

func TestRouterRetryAfterHandlesHTTPDateAndClamps(t *testing.T) {
	if got := routerRetryAfter(time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)); got < 29 || got > 31 {
		t.Fatalf("HTTP date retry = %d", got)
	}
	if routerRetryAfter("-1") != 0 || routerRetryAfter("999999999") != 86400 || routerRetryAfter("garbage") != 0 {
		t.Fatal("retry limits not bounded")
	}
	for _, value := range []string{
		strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10),
		strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10),
	} {
		if got := routerRateLimitReset(value); got < 3599 || got > 3601 {
			t.Fatalf("rate-limit reset should use actual timestamp: got=%d", got)
		}
	}
}

func TestOpenRouterGlobalQuotaHonorsResetWhenRetryAfterMissing(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header)}
	response.Header.Set("X-RateLimit-Limit", "50")
	response.Header.Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10))
	err := classifyRouterError(response, openRouterResponse{})
	if !err.Global || err.RetryAfterSeconds < 3599 || err.RetryAfterSeconds > 3601 {
		t.Fatalf("global daily gate needs reset cooldown: %+v", err)
	}
}

func TestOpenRouterModelWithoutNativeJSONUsesPromptAndRecommendedFallback(t *testing.T) {
	var attempted []string
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			plain := testCatalogEntry("test/free-chat-without-suffix")
			plain["supported_parameters"] = []string{}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{plain, testCatalogEntry(preferredAssistantModelID)}})
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		modelID, _ := body["model"].(string)
		attempted = append(attempted, modelID)
		if modelID == "test/free-chat-without-suffix" {
			for _, field := range []string{"temperature", "max_tokens", "max_completion_tokens", "response_format"} {
				if _, sent := body[field]; sent {
					t.Errorf("unsupported parameter %s was sent", field)
				}
			}
			messages, _ := json.Marshal(body["messages"])
			if !strings.Contains(string(messages), "JSON valido") {
				t.Error("non-native model must receive explicit JSON instruction")
			}
			writeRouterReply(w, "not JSON")
			return
		}
		writeRouterReply(w, `{"intent":"general","reply":"Olá"}`)
	})
	generator, err := newAssistantGeneratorForModel(context.Background(), "test/free-chat-without-suffix")
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.generateText(GeminiRequest{DebugStage: "assistant_intent", Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "Oi"}}}}})
	if err != nil || strings.Join(attempted, ",") != "test/free-chat-without-suffix,"+preferredAssistantModelID {
		t.Fatalf("non-native free model must be selectable and safely fall back: %v %v", attempted, err)
	}
}

func TestOpenRouterMaxCompletionTokensIsUsedOnlyWhenSupported(t *testing.T) {
	model := AssistantModel{ID: "test/free-model", ContextLength: 32768, SupportsMaxCompletionTokens: true}
	payload, err := routerPayload(model, GeminiRequest{DebugStage: "assistant_intent", Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "Oi"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["max_completion_tokens"] != float64(2048) {
		t.Fatalf("supported completion limit missing: %s", data)
	}
	for _, field := range []string{"max_tokens", "temperature", "response_format"} {
		if _, sent := fields[field]; sent {
			t.Errorf("unsupported parameter %s was sent", field)
		}
	}
}

func TestOpenRouterLargeMenuStillMakesAtMostThreeHTTPAttempts(t *testing.T) {
	for _, skipCoolingModels := range []bool{false, true} {
		t.Run(strconv.FormatBool(skipCoolingModels), func(t *testing.T) {
			var attempted []string
			setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					entries := []any{testCatalogEntry(preferredAssistantModelID)}
					for index := range 8 {
						entries = append(entries, testCatalogEntry(fmt.Sprintf("test/model-%d:free", index)))
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": entries})
					return
				}
				var body openRouterRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				attempted = append(attempted, body.Model)
				w.WriteHeader(http.StatusServiceUnavailable)
			})
			generator, err := newAssistantGeneratorForModel(context.Background(), "test/model-7:free")
			if err != nil {
				t.Fatal(err)
			}
			client := generator.(*openRouterClient)
			if len(client.models) != 9 {
				t.Fatalf("runtime attempt limit must not restrict menu: %d", len(client.models))
			}
			if skipCoolingModels {
				for _, modelID := range []string{"test/model-7:free", preferredAssistantModelID, "test/model-0:free"} {
					setRouterCooldown(client.account+":"+modelID, 30)
				}
			}
			if _, err := client.generateText(GeminiRequest{}); err == nil || len(attempted) != 3 {
				t.Fatalf("HTTP attempt limit must remain three regardless of menu/cooldown: attempts=%v err=%v", attempted, err)
			}
			if !skipCoolingModels && (attempted[0] != "test/model-7:free" || attempted[1] != preferredAssistantModelID) {
				t.Fatalf("selected model must precede recommended fallback: %v", attempted)
			}
			if skipCoolingModels && attempted[0] != "test/model-1:free" {
				t.Fatalf("cooldown skips must not consume attempts: %v", attempted)
			}
		})
	}
}

func TestOpenRouterFreeRouterOnlyAcceptsVerifiedActualModels(t *testing.T) {
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{testCatalogEntry("openrouter/free"), testCatalogEntry("test/actual-free-model")}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "test/actual-free-model", "choices": []any{map[string]any{
			"message": map[string]any{"content": `{"intent":"general","reply":"Olá"}`}, "finish_reason": "stop",
		}}})
	})
	generator, err := newAssistantGeneratorForModel(context.Background(), "openrouter/free")
	if err != nil {
		t.Fatal(err)
	}
	client := generator.(*openRouterClient)
	if _, err := client.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err != nil || client.modelUsed() != "test/actual-free-model" {
		t.Fatalf("free router should accept known zero-priced actual model: model=%s err=%v", client.modelUsed(), err)
	}
	if client.responseModelAllowed("openrouter/free", "vendor/paid-model") || client.responseModelAllowed("openrouter/free", "unknown/unverified:free") {
		t.Fatal("free router must reject paid or unknown actual models")
	}
	if client.responseModelAllowed("test/actual-free-model", "openrouter/free") {
		t.Fatal("direct model response must still match the requested model")
	}
}

func TestOpenRouterAdaptsHistoryToSmallContextWithoutLosingSafetyOrCurrentMessage(t *testing.T) {
	model := AssistantModel{ID: "test/small-free", ContextLength: 4096, SupportsMaxTokens: true, SupportsResponseFormat: true}
	request := GeminiRequest{
		SystemInstruction: &GeminiContent{Parts: []GeminiPart{{Text: "Never invent a financial amount or execute a write without confirmation."}}},
		Contents: []GeminiContent{
			{Role: "user", Parts: []GeminiPart{{Text: strings.Repeat("old user message", 180)}}},
			{Role: "model", Parts: []GeminiPart{{Text: strings.Repeat("old reply", 180)}}},
			{Role: "user", Parts: []GeminiPart{{Text: "Current financial question"}}},
		},
		DebugStage: "assistant_intent",
	}
	payload, err := routerPayload(model, request)
	if err != nil || len(payload.Messages) >= 4 || payload.Messages[0].Content != request.SystemInstruction.Parts[0].Text || payload.Messages[len(payload.Messages)-1].Content != "Current financial question" {
		t.Fatalf("context adaptation lost mandatory instructions or newest message: %+v err=%v", payload.Messages, err)
	}
	request.SystemInstruction.Parts[0].Text = strings.Repeat("mandatory safety instruction ", 200)
	_, err = routerPayload(model, request)
	var typed OpenRouterError
	if !errors.As(err, &typed) || typed.Code != "assistant_model_context_exceeded" {
		t.Fatalf("mandatory context overflow must fail safely: %v", err)
	}
}
