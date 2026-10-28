package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAutomaticRoutingHealthExpiresWithoutBeingPinnedByTraffic(t *testing.T) {
	var state assistantAutoRoutingState
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	models := []AssistantModel{{ID: "test/a:free"}, {ID: "test/b:free"}}
	state.remember("account-a", models[0].ID, now)
	state.remember("account-b", models[1].ID, now)
	if state.preferred("account-a", models, now.Add(time.Minute)) != models[0].ID || state.preferred("account-b", models, now) != models[1].ID {
		t.Fatal("observed health must remain isolated by account")
	}
	state.remember("account-a", models[0].ID, now.Add(4*time.Minute))
	if got := state.preferred("account-a", models, now.Add(assistantAutoRouteTTL)); got != "" {
		t.Fatalf("successful traffic must not pin a fallback forever: %s", got)
	}
	if len(state.accounts) != 0 {
		t.Fatal("expired account health entries must be pruned")
	}
}

func TestAutomaticRoutingDiscardsRemovedModelsAndOnlyMatchingFailures(t *testing.T) {
	var state assistantAutoRoutingState
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	models := []AssistantModel{{ID: "test/a:free"}, {ID: "test/b:free"}}
	state.remember("account", models[1].ID, now)
	state.forget("account", models[0].ID)
	if state.preferred("account", models, now) != models[1].ID {
		t.Fatal("a late failure for another model must not erase newer healthy state")
	}
	if state.preferred("account", models[:1], now) != "" {
		t.Fatal("a model removed from the current free catalog cannot remain preferred")
	}
	state.remember("account", models[1].ID, now)
	state.forget("account", models[1].ID)
	if state.preferred("account", models, now) != "" {
		t.Fatal("observed failure must invalidate matching healthy state")
	}
}

func TestAutomaticRoutingStateIsConcurrentSafeAndPruned(t *testing.T) {
	var state assistantAutoRoutingState
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	models := []AssistantModel{{ID: "test/a:free"}, {ID: "test/b:free"}}
	var group sync.WaitGroup
	for i := range 64 {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			account := fmt.Sprintf("account-%d", index%8)
			model := models[index%2].ID
			state.remember(account, model, now)
			preferred := state.preferred(account, models, now)
			if preferred != "" && preferred != models[0].ID && preferred != models[1].ID {
				t.Errorf("unexpected model in concurrent state: %q", preferred)
			}
			state.forget(account, model)
		}(i)
	}
	group.Wait()
	_ = state.preferred("anything", models, now.Add(assistantAutoRouteTTL))
	if len(state.accounts) != 0 {
		t.Fatal("expired state must not accumulate")
	}
}

func TestAutomaticRoutingReusesObservedFallbackAcrossRequestsAndHonorsLegacyOverride(t *testing.T) {
	var attempted []string
	dotsCalls := 0
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{testCatalogEntry(preferredAssistantModelID), testCatalogEntry("test/fallback:free")}})
			return
		}
		var body openRouterRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		attempted = append(attempted, body.Model)
		if body.Model == preferredAssistantModelID {
			dotsCalls++
			if dotsCalls == 1 {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"error":{"code":429,"message":"Provider returned error","metadata":{"provider_name":"test-provider"}}}`)
				return
			}
		}
		writeRouterReply(w, "Resposta valida")
	})
	first, err := newAssistantGeneratorForModel(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	client := first.(*openRouterClient)
	if _, err := first.generateText(GeminiRequest{}); err != nil {
		t.Fatal(err)
	}
	// Simulate the model-specific retry interval expiring, without touching any
	// account or per-user allowance. The observed fallback should remain favored.
	openRouterCooldowns.Lock()
	delete(openRouterCooldowns.until, client.account+":"+preferredAssistantModelID)
	openRouterCooldowns.Unlock()
	second, err := newAssistantGeneratorForModel(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.generateText(GeminiRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.generateText(GeminiRequest{}); err != nil {
		t.Fatal(err)
	}
	explicit, err := newAssistantGeneratorForModel(context.Background(), preferredAssistantModelID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := explicit.generateText(GeminiRequest{}); err != nil {
		t.Fatal(err)
	}
	last, err := newAssistantGeneratorForModel(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := last.generateText(GeminiRequest{}); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{preferredAssistantModelID, "test/fallback:free", "test/fallback:free", "test/fallback:free", preferredAssistantModelID, "test/fallback:free"}, ",")
	if got := strings.Join(attempted, ","); got != want {
		t.Fatalf("auto health or explicit override order incorrect:\ngot %s\nwant %s", got, want)
	}
}

func TestAutomaticRoutingRotatesAgainWhenObservedFallbackFails(t *testing.T) {
	var attempted []string
	fallbackCalls := 0
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{testCatalogEntry(preferredAssistantModelID), testCatalogEntry("test/a:free"), testCatalogEntry("test/b:free")}})
			return
		}
		var body openRouterRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		attempted = append(attempted, body.Model)
		if body.Model == preferredAssistantModelID {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if body.Model == "test/a:free" {
			fallbackCalls++
			if fallbackCalls > 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		writeRouterReply(w, "Resposta valida")
	})
	for range 3 {
		generator, err := newAssistantGeneratorForModel(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := generator.generateText(GeminiRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	want := strings.Join([]string{preferredAssistantModelID, "test/a:free", "test/a:free", "test/b:free", "test/b:free"}, ",")
	if got := strings.Join(attempted, ","); got != want {
		t.Fatalf("automatic routing should react to subsequent failure and retain healthy replacement: %s", got)
	}
}

func TestAutomaticRoutingNeverBypassesGlobalQuota(t *testing.T) {
	calls := 0
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls++
		if calls > 1 {
			w.Header().Set("X-RateLimit-Limit", "50")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":429,"message":"Rate limit exceeded","metadata":{"error_type":"rate_limit_exceeded"}}}`)
			return
		}
		writeRouterReply(w, "Resposta valida")
	})
	first, err := newAssistantGeneratorForModel(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.generateText(GeminiRequest{}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		generator, err := newAssistantGeneratorForModel(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = generator.generateText(GeminiRequest{})
		if code, _, ok := openRouterErrorInfo(err); !ok || code != "assistant_quota_exceeded" {
			t.Fatalf("global quota must stop automatic rotation: %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("observed healthy state must not bypass account cooldown: calls=%d", calls)
	}
}

func TestAutomaticRoutingUserLimitAndCancellationDoNotSpendOrResetAllowance(t *testing.T) {
	t.Setenv("OPENROUTER_USER_RPM", "2")
	t.Setenv("OPENROUTER_USER_DAILY_LIMIT", "20")
	calls := 0
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeRouterReply(w, "Resposta valida")
	})
	ctx := withAssistantUsage(context.Background(), 7081)
	otherCtx := withAssistantUsage(context.Background(), 7082)
	assistantCallUsage.Lock()
	delete(assistantCallUsage.users, ctx.Value(assistantUsageContextKey{}).(assistantUsageIdentity))
	delete(assistantCallUsage.users, otherCtx.Value(assistantUsageContextKey{}).(assistantUsageIdentity))
	assistantCallUsage.Unlock()
	first, err := newAssistantGeneratorForModel(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.generateText(GeminiRequest{}); err != nil {
		t.Fatal(err)
	}
	second, err := newAssistantGeneratorForModel(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = second.generateText(GeminiRequest{})
	usageError(t, err)
	if calls != 2 {
		t.Fatalf("a new automatic client must not reset per-user allowance: calls=%d", calls)
	}
	client := first.(*openRouterClient)
	if openRouterAutoRouting.preferred(client.account, client.models, time.Now()) != "test/second:free" {
		t.Fatal("a user's exhausted allowance is not evidence of model failure")
	}
	cancelled, cancel := context.WithCancel(otherCtx)
	third, err := newAssistantGeneratorForModel(cancelled, "")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := third.generateText(GeminiRequest{}); err == nil || calls != 2 {
		t.Fatalf("cancelled auto request must not call a model: calls=%d err=%v", calls, err)
	}
	other, err := newAssistantGeneratorForModel(otherCtx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.generateText(GeminiRequest{}); err != nil || calls != 3 {
		t.Fatalf("another user must retain allowance and reuse backend routing: calls=%d err=%v", calls, err)
	}
}
