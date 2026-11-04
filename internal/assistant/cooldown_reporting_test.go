package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnavailableModelReplyPreservesActualWaitAndErrorContract(t *testing.T) {
	reply, code, retry := assistantErrorResponse(OpenRouterError{
		Code: "assistant_models_unavailable", StatusCode: http.StatusServiceUnavailable, RetryAfterSeconds: 120,
	})
	if code != "assistant_models_unavailable" || retry != 120 {
		t.Fatalf("availability reply changed error contract: code=%s retry=%d", code, retry)
	}
	if !strings.Contains(reply, "120") || strings.Contains(reply, "alguns instantes") {
		t.Fatalf("availability reply hid actual wait: %q", reply)
	}
}

func TestSingleModelRateLimitReportsAppliedCooldown(t *testing.T) {
	var posts atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{testCatalogEntry("test/only:free")}})
			return
		}
		posts.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"metadata":{"provider_name":"fixture"}}}`))
	})
	generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
	if err != nil {
		t.Fatal(err)
	}
	client := generator.(*openRouterClient)
	_, err = generator.generateText(GeminiRequest{})
	var failure OpenRouterError
	if !errors.As(err, &failure) || failure.Code != "assistant_quota_exceeded" || failure.Global || failure.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected isolated provider rate limit: %+v", err)
	}
	remaining := routerCooldownRemaining(client.account + ":test/only:free")
	if remaining <= 0 || failure.RetryAfterSeconds < remaining || failure.RetryAfterSeconds > remaining+1 {
		t.Fatalf("reported wait differs from applied pause: retry=%d remaining=%d", failure.RetryAfterSeconds, remaining)
	}
	if posts.Load() != 1 || routerCooldownRemaining(client.account) != 0 {
		t.Fatalf("single provider failure spent another call or paused account: posts=%d", posts.Load())
	}
}

func TestOnlyModelHonorsLongerProviderRetryAfter(t *testing.T) {
	var posts atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{testCatalogEntry("test/only:free")}})
			return
		}
		posts.Add(1)
		w.Header().Set("Retry-After", "1200")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"metadata":{"provider_name":"fixture"}}}`))
	})
	generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
	if err != nil {
		t.Fatal(err)
	}
	client := generator.(*openRouterClient)
	_, err = generator.generateText(GeminiRequest{})
	var failure OpenRouterError
	if !errors.As(err, &failure) || failure.Code != "assistant_quota_exceeded" || failure.Global {
		t.Fatalf("expected isolated provider limit: %+v", err)
	}
	remaining := routerCooldownRemaining(client.account + ":test/only:free")
	if remaining < 1199 || failure.RetryAfterSeconds < remaining || failure.RetryAfterSeconds > remaining+1 {
		t.Fatalf("single-model recovery shortened provider instruction: retry=%d remaining=%d", failure.RetryAfterSeconds, remaining)
	}
	if posts.Load() != 1 || routerCooldownRemaining(client.account) != 0 {
		t.Fatalf("provider pause sent extra request or became global: posts=%d", posts.Load())
	}
}

func TestRejectedResponseReportsAppliedPauseWithoutSemanticRetry(t *testing.T) {
	var posts atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{testCatalogEntry("test/only:free")}})
			return
		}
		posts.Add(1)
		writeRouterReply(w, "invalid JSON")
	})
	generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
	if err != nil {
		t.Fatal(err)
	}
	client := generator.(*openRouterClient)
	_, err = generator.generateText(GeminiRequest{DebugStage: "assistant_intent"})
	var failure OpenRouterError
	if !errors.As(err, &failure) || failure.Code != "assistant_invalid_response" || failure.Global || failure.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected fail-closed response rejection: %+v", err)
	}
	remaining := routerCooldownRemaining(client.account + ":test/only:free")
	if remaining <= 0 || failure.RetryAfterSeconds < remaining || failure.RetryAfterSeconds > remaining+1 {
		t.Fatalf("semantic rejection hid applied pause: retry=%d remaining=%d", failure.RetryAfterSeconds, remaining)
	}
	if posts.Load() != 1 || routerCooldownRemaining(client.account) != 0 {
		t.Fatalf("semantic rejection spent another completion or paused account: posts=%d", posts.Load())
	}
}

func TestOnlyModelResumesAfterProviderPauseExpires(t *testing.T) {
	var posts atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{testCatalogEntry("test/only:free")}})
			return
		}
		if posts.Add(1) == 1 {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"metadata":{"provider_name":"fixture"}}}`))
			return
		}
		writeRouterReply(w, "Provider recovered")
	})
	first, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
	if err != nil {
		t.Fatal(err)
	}
	client := first.(*openRouterClient)
	if _, err := first.generateText(GeminiRequest{}); err == nil {
		t.Fatal("provider rate limit unexpectedly succeeded")
	}
	key := client.account + ":test/only:free"
	if remaining := routerCooldownRemaining(key); remaining <= 0 || remaining > 31 {
		t.Fatalf("only model must retain provider pause without long alternate-model quarantine: remaining=%d", remaining)
	}
	paused, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := paused.generateText(GeminiRequest{}); err == nil || posts.Load() != 1 {
		t.Fatalf("pause sent another completion: posts=%d err=%v", posts.Load(), err)
	}
	// Move only the test's model timer; do not wait, reset user/account quotas,
	// or bypass a live provider's instruction.
	openRouterCooldowns.Lock()
	openRouterCooldowns.until[key] = time.Now().Add(-time.Second)
	openRouterCooldowns.Unlock()
	resumed, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
	if err != nil {
		t.Fatal(err)
	}
	if text, err := resumed.generateText(GeminiRequest{}); err != nil || text != "Provider recovered" || posts.Load() != 2 {
		t.Fatalf("expired provider pause did not recover: text=%q posts=%d err=%v", text, posts.Load(), err)
	}
	if routerCooldownRemaining(client.account) != 0 {
		t.Fatal("single provider pause must not become a shared account limit")
	}
}

func TestAllModelsCoolingDownReportEarliestActualReleaseWithoutPOST(t *testing.T) {
	var posts atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		posts.Add(1)
		writeRouterReply(w, "Unexpected provider call")
	})
	generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
	if err != nil {
		t.Fatal(err)
	}
	client := generator.(*openRouterClient)
	setRouterCooldown(client.account+":test/first:free", 300)
	setRouterCooldown(client.account+":test/second:free", 90)
	setRouterCooldown(client.account+":test/third:free", 240)
	_, err = generator.generateText(GeminiRequest{})
	var failure OpenRouterError
	if !errors.As(err, &failure) || failure.Code != "assistant_models_unavailable" || failure.Global || failure.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected local model availability error: %+v", err)
	}
	remaining := routerCooldownRemaining(client.account + ":test/second:free")
	if failure.RetryAfterSeconds < remaining || failure.RetryAfterSeconds > remaining+1 {
		t.Fatalf("wait must match earliest model release: retry=%d remaining=%d", failure.RetryAfterSeconds, remaining)
	}
	if posts.Load() != 0 || routerCooldownRemaining(client.account) != 0 {
		t.Fatalf("local cooldown sent a POST or blocked entire account: posts=%d", posts.Load())
	}
}

func TestCoolingDownMessageDoesNotConsumeUserAllowance(t *testing.T) {
	t.Setenv("OPENROUTER_USER_RPM", "6")
	t.Setenv("OPENROUTER_USER_DAILY_LIMIT", "50")
	var posts atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		posts.Add(1)
		writeRouterReply(w, "Unexpected provider call")
	})
	usageCtx := withAssistantUsage(context.Background(), 891724)
	identity := usageCtx.Value(assistantUsageContextKey{}).(assistantUsageIdentity)
	assistantCallUsage.Lock()
	delete(assistantCallUsage.users, identity)
	assistantCallUsage.Unlock()
	t.Cleanup(func() {
		assistantCallUsage.Lock()
		delete(assistantCallUsage.users, identity)
		assistantCallUsage.Unlock()
	})
	for range 2 {
		generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(usageCtx), "")
		if err != nil {
			t.Fatal(err)
		}
		client := generator.(*openRouterClient)
		for _, model := range client.models {
			setRouterCooldown(client.account+":"+model.ID, 120)
		}
		if _, err := generator.generateText(GeminiRequest{}); err == nil {
			t.Fatal("cooling down models unexpectedly returned a response")
		}
		if routerCooldownRemaining(client.account) != 0 {
			t.Fatal("model pauses must not become a shared account limit")
		}
	}
	assistantCallUsage.Lock()
	entry := assistantCallUsage.users[identity]
	assistantCallUsage.Unlock()
	if posts.Load() != 0 || entry != nil {
		t.Fatalf("cooldown-only messages consumed allowance: posts=%d entry=%+v", posts.Load(), entry)
	}
}
