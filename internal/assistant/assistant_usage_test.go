package assistant

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func usageTestIdentity(userID uint) assistantUsageIdentity {
	return assistantUsageIdentity{userID: userID}
}

func usageError(t *testing.T, err error) OpenRouterError {
	t.Helper()
	var result OpenRouterError
	if !errors.As(err, &result) || result.Code != "assistant_user_limit" || result.Global || result.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected isolated per-user limit error, got %v", err)
	}
	return result
}

func TestAssistantUsageConfigurationIsBounded(t *testing.T) {
	t.Setenv("OPENROUTER_USER_RPM", "")
	t.Setenv("OPENROUTER_USER_DAILY_LIMIT", "")
	rpm, daily, err := assistantUsageLimits()
	if err != nil || rpm != 6 || daily != 50 {
		t.Fatalf("unexpected default usage configuration: %d %d %v", rpm, daily, err)
	}
	for _, tc := range []struct{ name, value string }{
		{"OPENROUTER_USER_RPM", "0"}, {"OPENROUTER_USER_RPM", "61"},
		{"OPENROUTER_USER_RPM", "invalid"}, {"OPENROUTER_USER_DAILY_LIMIT", "-1"},
		{"OPENROUTER_USER_DAILY_LIMIT", "1001"},
	} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			if _, _, err := assistantUsageLimits(); err == nil {
				t.Fatal("unsafe usage limit configuration was accepted")
			}
		})
	}
	t.Setenv("OPENROUTER_USER_RPM", "60")
	t.Setenv("OPENROUTER_USER_DAILY_LIMIT", "1000")
	if rpm, daily, err := assistantUsageLimits(); err != nil || rpm != 60 || daily != 1000 {
		t.Fatalf("documented upper limits rejected: %d %d %v", rpm, daily, err)
	}
}

func TestAssistantUsageMinuteGateIsRollingAndIsolatedByUser(t *testing.T) {
	var limiter assistantUsageLimiter
	now := time.Date(2026, 10, 28, 12, 0, 55, 0, time.UTC)
	for range 6 {
		if err := limiter.reserve(usageTestIdentity(11), now, 6, 20); err != nil {
			t.Fatal(err)
		}
	}
	if err := usageError(t, limiter.reserve(usageTestIdentity(11), now.Add(10*time.Second), 6, 20)); err.RetryAfterSeconds != 50 {
		t.Fatalf("minute boundary must not reset rolling limit: %+v", err)
	}
	if err := limiter.reserve(usageTestIdentity(12), now.Add(10*time.Second), 6, 20); err != nil {
		t.Fatalf("one user's gate leaked into another user: %v", err)
	}
	if err := limiter.reserve(usageTestIdentity(11), now.Add(time.Minute), 6, 20); err != nil {
		t.Fatalf("minute window did not release expired attempts: %v", err)
	}
}

func TestAssistantUsageDailyLimitResetsAtUTCMidnight(t *testing.T) {
	var limiter assistantUsageLimiter
	// A local timezone cannot change the UTC billing window.
	now := time.Date(2026, 10, 28, 20, 40, 0, 0, time.FixedZone("SaoPaulo", -3*3600))
	for i := range 20 {
		if err := limiter.reserve(usageTestIdentity(11), now.Add(time.Duration(i)*time.Minute), 6, 20); err != nil {
			t.Fatal(err)
		}
	}
	beforeMidnight := now.Add(19*time.Minute + 30*time.Second)
	if err := usageError(t, limiter.reserve(usageTestIdentity(11), beforeMidnight, 6, 20)); err.RetryAfterSeconds != 30 {
		t.Fatalf("daily cooldown must end at UTC midnight: %+v", err)
	}
	if err := limiter.reserve(usageTestIdentity(11), now.Add(20*time.Minute), 6, 20); err != nil {
		t.Fatalf("next UTC day must receive a new allowance: %v", err)
	}
}

func TestAssistantUsageMinuteLimitSurvivesDailyReset(t *testing.T) {
	var limiter assistantUsageLimiter
	now := time.Date(2026, 10, 28, 23, 59, 45, 0, time.UTC)
	for range 6 {
		if err := limiter.reserve(usageTestIdentity(11), now, 6, 20); err != nil {
			t.Fatal(err)
		}
	}
	if err := usageError(t, limiter.reserve(usageTestIdentity(11), now.Add(20*time.Second), 6, 20)); err.RetryAfterSeconds != 40 {
		t.Fatalf("UTC rollover incorrectly bypassed rolling minute limit: %+v", err)
	}
}

func TestAssistantUsageConcurrentAttemptsCannotExceedAllowance(t *testing.T) {
	var limiter assistantUsageLimiter
	var accepted atomic.Int32
	var group sync.WaitGroup
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for range 80 {
		group.Add(1)
		go func() {
			defer group.Done()
			if limiter.reserve(usageTestIdentity(11), now, 6, 20) == nil {
				accepted.Add(1)
			}
		}()
	}
	group.Wait()
	if accepted.Load() != 6 {
		t.Fatalf("concurrent requests exceeded or lost allowance: accepted=%d", accepted.Load())
	}
}

func TestAssistantUsagePrunesExpiredUsersWithoutResettingToday(t *testing.T) {
	var limiter assistantUsageLimiter
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if err := limiter.reserve(usageTestIdentity(11), now, 6, 20); err != nil {
		t.Fatal(err)
	}
	if err := limiter.reserve(usageTestIdentity(12), now.Add(24*time.Hour), 6, 20); err != nil {
		t.Fatal(err)
	}
	if len(limiter.users) != 1 || limiter.users[usageTestIdentity(11)] != nil || limiter.users[usageTestIdentity(12)].daily != 1 {
		t.Fatal("expired user usage must be pruned")
	}
	if err := limiter.reserve(usageTestIdentity(13), now.Add(24*time.Hour+time.Minute), 6, 20); err != nil {
		t.Fatal(err)
	}
	if limiter.users[usageTestIdentity(12)] == nil || limiter.users[usageTestIdentity(12)].daily != 1 {
		t.Fatal("pruning must retain active day's total")
	}
}

func TestAssistantUsageAccountFingerprintSeparatesSameUserIDs(t *testing.T) {
	var limiter assistantUsageLimiter
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	first := assistantUsageIdentity{account: sha256.Sum256([]byte("account-one")), userID: 11}
	second := assistantUsageIdentity{account: sha256.Sum256([]byte("account-two")), userID: 11}
	if err := limiter.reserve(first, now, 1, 20); err != nil {
		t.Fatal(err)
	}
	usageError(t, limiter.reserve(first, now, 1, 20))
	if err := limiter.reserve(second, now, 1, 20); err != nil {
		t.Fatalf("rotated account key must not inherit another account's user allowance: %v", err)
	}
}

func TestAssistantUsageUngatedContextDoesNotConsumeAllowance(t *testing.T) {
	t.Setenv("OPENROUTER_USER_RPM", "invalid")
	if err := reserveAssistantCall(context.Background()); err != nil {
		t.Fatalf("local confirmation/CLI without LLM user context must not consume allowance: %v", err)
	}
	if err := reserveAssistantCall(withAssistantUsage(context.Background(), 0)); err != nil {
		t.Fatalf("absent user must not create a shared anonymous quota: %v", err)
	}
}

func TestOpenRouterUsageGateCountsEachHTTPFallbackAndKeepsUsersSeparate(t *testing.T) {
	t.Setenv("OPENROUTER_USER_RPM", "1")
	t.Setenv("OPENROUTER_USER_DAILY_LIMIT", "20")
	assistantCallUsage.Lock()
	assistantCallUsage.users = nil
	assistantCallUsage.lastPruned = time.Time{}
	assistantCallUsage.Unlock()
	var calls int
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
		writeRouterReply(w, "Olá")
	})
	first, err := newAssistantGeneratorForModel(withAssistantUsage(context.Background(), 991), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = first.generateText(GeminiRequest{})
	usageError(t, err)
	if calls != 1 {
		t.Fatalf("fallback should stop before HTTP once per-user allowance ends: calls=%d", calls)
	}
	client := first.(*openRouterClient)
	if routerCooldownRemaining(client.account) != 0 {
		t.Fatal("one user's quota must never block the entire account")
	}
	other, err := newAssistantGeneratorForModel(withAssistantUsage(context.Background(), 992), "")
	if err != nil {
		t.Fatal(err)
	}
	if reply, err := other.generateText(GeminiRequest{}); err != nil || reply != "Olá" || calls != 2 {
		t.Fatalf("user B should still reach a free model: reply=%q calls=%d err=%v", reply, calls, err)
	}
}
