package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestFallbackStartWindowLeavesTimeForAUsefulResponse(t *testing.T) {
	start := time.Now()
	for _, test := range []struct {
		name      string
		elapsed   time.Duration
		remaining time.Duration
		first     bool
		want      bool
	}{
		{"first attempt keeps caller deadline", 0, time.Second, true, true},
		{"quick rejection", time.Second, 44 * time.Second, false, true},
		{"before window closes", 8*time.Second - time.Nanosecond, 37 * time.Second, false, true},
		{"window closed", 8 * time.Second, 37 * time.Second, false, false},
		{"late rejection", 20 * time.Second, 25 * time.Second, false, false},
		{"enough caller time", time.Second, 10 * time.Second, false, true},
		{"too little caller time", time.Second, 10*time.Second - time.Nanosecond, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := start.Add(test.elapsed)
			ctx, cancel := context.WithDeadline(context.Background(), now.Add(test.remaining))
			defer cancel()
			before := start.Add(openRouterFallbackStartWindow)
			if test.first {
				before = time.Time{}
			}
			if got := routerFallbackCanStart(ctx, before, now); got != test.want {
				t.Fatalf("fallback admission=%t want=%t", got, test.want)
			}
		})
	}
}

func TestShortCallerDeadlineStopsBeforeChargingOrPausingUnusedFallback(t *testing.T) {
	var calls atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		var payload openRouterRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		calls.Add(1)
		if payload.Model == "test/first:free" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeRouterReply(w, "Recovered")
	})
	t.Setenv("OPENROUTER_USER_RPM", "60")
	userCtx := withAssistantUsage(context.Background(), 996214)
	identity := userCtx.Value(assistantUsageContextKey{}).(assistantUsageIdentity)
	assistantCallUsage.Lock()
	delete(assistantCallUsage.users, identity)
	assistantCallUsage.Unlock()
	t.Cleanup(func() {
		assistantCallUsage.Lock()
		delete(assistantCallUsage.users, identity)
		assistantCallUsage.Unlock()
	})
	ctx, cancel := context.WithTimeout(withAssistantMessageBudget(userCtx), 5*time.Second)
	defer cancel()
	generator, err := newAssistantGeneratorForModel(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.generateText(GeminiRequest{})
	var failure OpenRouterError
	if !errors.As(err, &failure) || failure.Code != "assistant_models_unavailable" || calls.Load() != 1 {
		t.Fatalf("fallback spent a call too near caller deadline: calls=%d err=%v", calls.Load(), err)
	}
	client := generator.(*openRouterClient)
	if routerCooldownRemaining(client.account+":test/first:free") == 0 {
		t.Fatal("actually failed model did not enter cooldown")
	}
	for _, model := range []string{"test/second:free", "test/third:free"} {
		if routerCooldownRemaining(client.account+":"+model) != 0 {
			t.Fatal("a model never called was paused")
		}
	}
	assistantCallUsage.Lock()
	charged := assistantCallUsage.users[identity].daily
	assistantCallUsage.Unlock()
	if charged != 1 {
		t.Fatalf("unused fallback consumed allowance: %d", charged)
	}
	next, err := newAssistantGeneratorForModel(withAssistantMessageBudget(userCtx), "")
	if err != nil {
		t.Fatal(err)
	}
	if reply, err := next.generateText(GeminiRequest{}); err != nil || reply != "Recovered" || calls.Load() != 2 {
		t.Fatalf("next message lost the uncalled healthy model: calls=%d reply=%q err=%v", calls.Load(), reply, err)
	}
}

func TestFirstSuccessDoesNotRequireFallbackTimeRemaining(t *testing.T) {
	var calls atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls.Add(1)
		writeRouterReply(w, "Answered")
	})
	ctx, cancel := context.WithTimeout(withAssistantMessageBudget(context.Background()), 5*time.Second)
	defer cancel()
	generator, err := newAssistantGeneratorForModel(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if reply, err := generator.generateText(GeminiRequest{}); err != nil || reply != "Answered" || calls.Load() != 1 {
		t.Fatalf("first model was restricted by the fallback admission window: calls=%d reply=%q err=%v", calls.Load(), reply, err)
	}
}
