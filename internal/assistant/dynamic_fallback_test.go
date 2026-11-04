package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

type dynamicFallbackTrace struct {
	sync.Mutex
	models []string
}

func (trace *dynamicFallbackTrace) record(model string) int {
	trace.Lock()
	defer trace.Unlock()
	trace.models = append(trace.models, model)
	return len(trace.models)
}

func (trace *dynamicFallbackTrace) snapshot() []string {
	trace.Lock()
	defer trace.Unlock()
	return append([]string(nil), trace.models...)
}

func dynamicFallbackModelIDs(count int) []string {
	models := make([]string, count)
	for index := range models {
		models[index] = fmt.Sprintf("test/dynamic-%02d:free", index+1)
	}
	return models
}

func writeDynamicFallbackCatalog(w http.ResponseWriter, count int) {
	entries := make([]any, 0, count+1)
	for _, id := range dynamicFallbackModelIDs(count) {
		entries = append(entries, testCatalogEntry(id))
	}
	paid := testCatalogEntry("test/paid-dynamic:free")
	paid["pricing"] = map[string]string{"prompt": "0", "completion": "0.01"}
	entries = append(entries, paid)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": entries})
}

func writeDynamicFallbackFailure(w http.ResponseWriter, attempt int) {
	switch attempt % 3 {
	case 1:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"No endpoints found matching your data policy"}}`))
	case 2:
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"metadata":{"provider_name":"fixture"}}}`))
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}
}

func dynamicFallbackUser(t *testing.T, userID uint, rpm, daily int) context.Context {
	t.Helper()
	t.Setenv("OPENROUTER_USER_RPM", strconv.Itoa(rpm))
	t.Setenv("OPENROUTER_USER_DAILY_LIMIT", strconv.Itoa(daily))
	ctx := withAssistantUsage(context.Background(), userID)
	identity := ctx.Value(assistantUsageContextKey{}).(assistantUsageIdentity)
	assistantCallUsage.Lock()
	delete(assistantCallUsage.users, identity)
	assistantCallUsage.Unlock()
	t.Cleanup(func() {
		assistantCallUsage.Lock()
		delete(assistantCallUsage.users, identity)
		assistantCallUsage.Unlock()
	})
	return ctx
}

func assertDynamicFallbackUsage(t *testing.T, ctx context.Context, want int) {
	t.Helper()
	identity := ctx.Value(assistantUsageContextKey{}).(assistantUsageIdentity)
	assistantCallUsage.Lock()
	entry := assistantCallUsage.users[identity]
	var daily, minute int
	if entry != nil {
		daily, minute = entry.daily, len(entry.attempts)
	}
	assistantCallUsage.Unlock()
	if daily != want || minute != want {
		t.Fatalf("rotation reset or miscounted usage: daily=%d minute=%d want=%d", daily, minute, want)
	}
}

func TestDynamicFallbackReachesSixthFreeModelAndReusesHealthyReplacement(t *testing.T) {
	var trace dynamicFallbackTrace
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeDynamicFallbackCatalog(w, 8)
			return
		}
		payload := originalPolicyPayload(t, r)
		attempt := trace.record(payload.Model)
		if attempt <= 5 {
			writeDynamicFallbackFailure(w, attempt)
			return
		}
		if payload.Model != "test/dynamic-06:free" {
			t.Errorf("did not reuse the healthy free model: %s", payload.Model)
		}
		writeRouterReply(w, `{"intent":"general","reply":"Resposta recuperada"}`)
	})
	t.Setenv("OPENROUTER_REQUIRE_ZDR", "false")
	userCtx := dynamicFallbackUser(t, 993601, 60, 50)
	messageCtx := withAssistantMessageBudget(userCtx)
	generator, err := newAssistantGeneratorForModel(messageCtx, "")
	if err != nil {
		t.Fatal(err)
	}
	client := generator.(*openRouterClient)
	if text, err := client.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err != nil || text == "" || client.modelUsed() != "test/dynamic-06:free" {
		t.Fatalf("sixth candidate did not recover the same message: text=%q model=%q err=%v", text, client.modelUsed(), err)
	}
	if got, want := trace.snapshot(), dynamicFallbackModelIDs(6); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected candidate order or extra completion: got=%v want=%v", got, want)
	}
	assertDynamicFallbackUsage(t, userCtx, 6)
	for _, id := range dynamicFallbackModelIDs(5) {
		if routerCooldownRemaining(client.account+":"+id) <= 0 {
			t.Fatalf("failed model was not paused: %s", id)
		}
	}
	if routerCooldownRemaining(client.account) != 0 {
		t.Fatal("model-specific failures paused the whole account")
	}
	other, err := newAssistantGeneratorForModel(messageCtx, "test/dynamic-08:free")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.generateText(GeminiRequest{}); err == nil || len(trace.snapshot()) != 6 {
		t.Fatal("second client restarted the same message's generation")
	}
	next, err := newAssistantGeneratorForModel(withAssistantMessageBudget(userCtx), "")
	if err != nil {
		t.Fatal(err)
	}
	if text, err := next.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err != nil || text == "" {
		t.Fatalf("next message did not reuse the healthy replacement: text=%q err=%v", text, err)
	}
	want := append(dynamicFallbackModelIDs(6), "test/dynamic-06:free")
	if got := trace.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("next message repeated failed models: got=%v want=%v", got, want)
	}
	assertDynamicFallbackUsage(t, userCtx, 7)
}

func TestDynamicFallbackFirstSuccessDoesNotProbeOtherModels(t *testing.T) {
	var trace dynamicFallbackTrace
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeDynamicFallbackCatalog(w, 8)
			return
		}
		payload := originalPolicyPayload(t, r)
		trace.record(payload.Model)
		writeRouterReply(w, `{"intent":"general","reply":"Ola"}`)
	})
	t.Setenv("OPENROUTER_REQUIRE_ZDR", "false")
	userCtx := dynamicFallbackUser(t, 993602, 6, 50)
	ctx := withAssistantMessageBudget(userCtx)
	generator, err := newAssistantGeneratorForModel(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if text, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err != nil || text == "" {
		t.Fatalf("first candidate failed: text=%q err=%v", text, err)
	}
	if _, err := generator.generateText(GeminiRequest{}); err == nil {
		t.Fatal("success permitted another generation")
	}
	if got, want := trace.snapshot(), dynamicFallbackModelIDs(1); !reflect.DeepEqual(got, want) {
		t.Fatalf("success sent extra completions: got=%v want=%v", got, want)
	}
	assertDynamicFallbackUsage(t, userCtx, 1)
}

func TestDynamicFallbackVisitsEachUniqueCatalogModelAtMostOnce(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate_client_candidates=%t", duplicate), func(t *testing.T) {
			var trace dynamicFallbackTrace
			setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					writeDynamicFallbackCatalog(w, 8)
					return
				}
				payload := originalPolicyPayload(t, r)
				attempt := trace.record(payload.Model)
				writeDynamicFallbackFailure(w, attempt)
			})
			t.Setenv("OPENROUTER_REQUIRE_ZDR", "false")
			ctx := withAssistantMessageBudget(context.Background())
			generator, err := newAssistantGeneratorForModel(ctx, "test/dynamic-01:free")
			if err != nil {
				t.Fatal(err)
			}
			client := generator.(*openRouterClient)
			if duplicate {
				// Bypass catalogue deduplication to exercise the runtime traversal.
				models := append([]AssistantModel(nil), client.models...)
				client.models = append(client.models, models...)
			}
			if _, err := client.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err == nil {
				t.Fatal("all failed candidates unexpectedly succeeded")
			}
			if got, want := trace.snapshot(), dynamicFallbackModelIDs(8); !reflect.DeepEqual(got, want) {
				t.Fatalf("did not traverse every unique free model once: got=%v want=%v", got, want)
			}
			other, err := newAssistantGeneratorForModel(ctx, "test/dynamic-08:free")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := other.generateText(GeminiRequest{}); err == nil || len(trace.snapshot()) != 8 {
				t.Fatal("another client restarted the exhausted traversal")
			}
		})
	}
}

func TestDynamicFallbackStopsAtExistingUserQuotaBeforeAnotherPOST(t *testing.T) {
	for _, test := range []struct {
		name       string
		userID     uint
		rpm, daily int
	}{
		{"minute", 993603, 4, 50},
		{"daily", 993604, 60, 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			var trace dynamicFallbackTrace
			setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					writeDynamicFallbackCatalog(w, 8)
					return
				}
				payload := originalPolicyPayload(t, r)
				trace.record(payload.Model)
				w.WriteHeader(http.StatusServiceUnavailable)
			})
			t.Setenv("OPENROUTER_REQUIRE_ZDR", "false")
			userCtx := dynamicFallbackUser(t, test.userID, test.rpm, test.daily)
			for range 2 {
				generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(userCtx), "")
				if err != nil {
					t.Fatal(err)
				}
				_, err = generator.generateText(GeminiRequest{DebugStage: "assistant_intent"})
				var failure OpenRouterError
				if !errors.As(err, &failure) || failure.Code != "assistant_user_limit" || failure.Global || failure.RetryAfterSeconds <= 0 {
					t.Fatalf("rotation ignored the existing user allowance: %+v", err)
				}
				if got, want := trace.snapshot(), dynamicFallbackModelIDs(4); !reflect.DeepEqual(got, want) {
					t.Fatalf("quota rejection sent another POST or reset with a new message: got=%v want=%v", got, want)
				}
				assertDynamicFallbackUsage(t, userCtx, 4)
			}
		})
	}
}
