package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMessageBudgetIsSharedUnderConcurrentAttempts(t *testing.T) {
	ctx := withAssistantMessageBudget(context.Background())
	var successful atomic.Int32
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			if reserveAssistantCall(ctx) == nil {
				successful.Add(1)
			}
		}()
	}
	group.Wait()
	if successful.Load() != 1 {
		t.Fatalf("shared message budget allowed %d attempts", successful.Load())
	}
}

func TestDefaultDailyAllowanceSupportsFiftyIndependentMessages(t *testing.T) {
	t.Setenv("OPENROUTER_USER_RPM", "60")
	t.Setenv("OPENROUTER_USER_DAILY_LIMIT", "")
	user := withAssistantUsage(context.Background(), 994028)
	identity := user.Value(assistantUsageContextKey{}).(assistantUsageIdentity)
	assistantCallUsage.Lock()
	delete(assistantCallUsage.users, identity)
	assistantCallUsage.Unlock()
	t.Cleanup(func() {
		assistantCallUsage.Lock()
		delete(assistantCallUsage.users, identity)
		assistantCallUsage.Unlock()
	})
	for i := range 50 {
		if err := reserveAssistantCall(withAssistantMessageBudget(user)); err != nil {
			t.Fatalf("message %d rejected: %v", i+1, err)
		}
	}
	usageError(t, reserveAssistantCall(withAssistantMessageBudget(user)))
}

func TestChatMessageMakesAtMostOneCompletionIncludingFailures(t *testing.T) {
	for _, scenario := range []string{"success", "invalid_json", "length", "provider_429", "server_503"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					writeTestCatalog(w)
					return
				}
				calls++
				var payload openRouterRequest
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload.MaxTokens != 2048 {
					t.Errorf("the larger output allowance must preserve one completion: max_tokens=%d", payload.MaxTokens)
				}
				if calls == 1 {
					switch scenario {
					case "invalid_json":
						writeRouterReply(w, "invalid JSON")
						return
					case "length":
						_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"truncated"},"finish_reason":"length"}]}`)
						return
					case "provider_429":
						w.WriteHeader(429)
						_, _ = io.WriteString(w, `{"error":{"code":429,"metadata":{"provider_name":"fixture"}}}`)
						return
					case "server_503":
						w.WriteHeader(503)
						return
					}
				}
				writeRouterReply(w, `{"intent":"general","reply":"Olá"}`)
			})
			ctx := withAssistantMessageBudget(context.Background())
			generator, err := newAssistantGeneratorForModel(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = generator.generateText(GeminiRequest{DebugStage: "assistant_intent"})
			if (err == nil) != (scenario == "success") || calls != 1 {
				t.Fatalf("first message: calls=%d err=%v", calls, err)
			}
			if _, err := generator.generateText(GeminiRequest{}); err == nil || calls != 1 {
				t.Fatalf("second generation spent another call: %d %v", calls, err)
			}
			// Even another client shares the budget of this same HTTP message.
			other, err := newAssistantGeneratorForModel(ctx, "test/third:free")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := other.generateText(GeminiRequest{}); err == nil || calls != 1 {
				t.Fatal("another client bypassed message budget")
			}
			next, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := next.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err != nil || calls != 2 {
				t.Fatalf("new message did not get independent budget: %d %v", calls, err)
			}
		})
	}
}

func TestFinancialReplyDoesNotMakeSecondCompletion(t *testing.T) {
	calls := 0
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls++
		writeRouterReply(w, `{"intent":"fgts_balance"}`)
	})
	client, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err != nil {
		t.Fatal(err)
	}
	reply := naturalFinancialReply(client, "e meu FGTS?", nil, "fgts_balance", map[string]any{"fgts": map[string]any{"amount": 300.0}, "total_fgts": 300.0})
	if calls != 1 || !strings.Contains(reply, "300,00") {
		t.Fatalf("financial query spent another call or lost facts: %d %s", calls, reply)
	}
}
