package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestChatFallbackReachesThirdModelAndNextMessageSkipsBothFailedModels(t *testing.T) {
	var attempted []string
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		var payload openRouterRequest
		_ = json.NewDecoder(r.Body).Decode(&payload)
		attempted = append(attempted, payload.Model)
		if len(attempted) <= 2 {
			w.WriteHeader(503)
			return
		}
		writeRouterReply(w, "Recovered")
	})
	ctx := withAssistantMessageBudget(context.Background())
	first, err := newAssistantGeneratorForModel(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if reply, err := first.generateText(GeminiRequest{}); err != nil || reply != "Recovered" || len(attempted) != 3 {
		t.Fatalf("third candidate was not reached in the same message: calls=%v reply=%q err=%v", attempted, reply, err)
	}
	other, _ := newAssistantGeneratorForModel(ctx, "")
	if _, err := other.generateText(GeminiRequest{}); err == nil || len(attempted) != 3 {
		t.Fatal("second client bypassed failed generation budget")
	}
	next, _ := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
	if _, err := next.generateText(GeminiRequest{}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(attempted, ",") != "test/first:free,test/second:free,test/third:free,test/third:free" {
		t.Fatalf("repeated a failed model: %v", attempted)
	}
}

func TestChatProviderRateLimitWithHeadersRotatesAndKeepsLongCooldown(t *testing.T) {
	var attempted []string
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		var payload openRouterRequest
		_ = json.NewDecoder(r.Body).Decode(&payload)
		attempted = append(attempted, payload.Model)
		if len(attempted) == 1 {
			w.Header().Set("X-RateLimit-Limit", "20")
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"code":429,"metadata":{"provider_name":"fixture"}}}`))
			return
		}
		writeRouterReply(w, "Recovered")
	})
	for range 2 {
		generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := generator.generateText(GeminiRequest{}); err != nil {
			t.Fatal(err)
		}
		client := generator.(*openRouterClient)
		if routerCooldownRemaining(client.account+":test/first:free") < 890 || routerCooldownRemaining(client.account) != 0 {
			t.Fatal("provider failure did not stay isolated for fifteen minutes")
		}
	}
	if strings.Join(attempted, ",") != "test/first:free,test/second:free,test/second:free" {
		t.Fatalf("did not reuse recovered model: %v", attempted)
	}
}

func TestChatGlobalLimitNeverRetriesAnotherModel(t *testing.T) {
	for _, status := range []int{402, 401, 403, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					writeTestCatalog(w)
					return
				}
				calls++
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"daily limit exceeded","metadata":{"limit_source":"openrouter"}}}`))
			})
			generator, _ := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
			if _, err := generator.generateText(GeminiRequest{}); err == nil || calls != 1 {
				t.Fatalf("global rejection retried: calls=%d err=%v", calls, err)
			}
		})
	}
}
