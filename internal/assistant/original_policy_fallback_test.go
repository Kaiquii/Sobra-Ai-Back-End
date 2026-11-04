package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func originalPolicyPayload(t *testing.T, r *http.Request) openRouterRequest {
	t.Helper()
	var payload openRouterRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Errorf("completion body invalid: %v", err)
		return payload
	}
	if payload.Provider["data_collection"] != "deny" || payload.Provider["require_parameters"] != true || payload.Provider["allow_fallbacks"] != true {
		t.Errorf("fallback weakened original provider restrictions: %+v", payload.Provider)
	}
	prices, ok := payload.Provider["max_price"].(map[string]any)
	if !ok || len(prices) != 3 || prices["prompt"] != float64(0) || prices["completion"] != float64(0) || prices["request"] != float64(0) {
		t.Errorf("fallback lost strict zero-price ceiling: %+v", payload.Provider["max_price"])
	}
	if _, present := payload.Provider["zdr"]; present {
		t.Error("original policy unexpectedly imposed the extra ZDR filter")
	}
	if _, present := payload.Provider["only"]; present {
		t.Error("original policy unexpectedly pinned the old ZDR-only provider list")
	}
	if max(payload.MaxTokens, payload.MaxCompletionTokens) != 2048 {
		t.Error("fallback changed the output allowance")
	}
	return payload
}

func TestOriginalPolicyCatalogExplicitlyIncludesFreeAlternativesWithoutZDRRequest(t *testing.T) {
	for _, mode := range []string{"false"} {
		t.Run("mode="+mode, func(t *testing.T) {
			var catalogCalls, endpointCalls atomic.Int32
			setupRouterPolicyTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
					t.Errorf("unexpected catalog request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("public catalog received a credential")
				}
				catalogCalls.Add(1)
				paid := testCatalogEntry("test/paid:free")
				paid["pricing"] = map[string]string{"prompt": "0", "completion": "0", "request": "0.01"}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
					testCatalogEntry(preferredAssistantModelID), testCatalogEntry("test/free-alternative:free"), paid,
				}})
			}, func(w http.ResponseWriter, r *http.Request) {
				endpointCalls.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			})
			t.Setenv("OPENROUTER_REQUIRE_ZDR", mode)
			models, err := listFreeAssistantModels(context.Background())
			if err != nil || len(models) != 2 || models[0].ID != preferredAssistantModelID || !models[0].Recommended {
				t.Fatalf("free alternatives were excluded by an optional strict filter: models=%+v err=%v", models, err)
			}
			if catalogCalls.Load() != 1 || endpointCalls.Load() != 0 {
				t.Fatalf("original policy used ZDR endpoint discovery: catalog=%d endpoints=%d", catalogCalls.Load(), endpointCalls.Load())
			}
			for _, model := range models {
				if len(model.privateProviders) != 0 || model.ID == "test/paid:free" {
					t.Fatalf("model inherited strict-only providers or paid pricing: %+v", model)
				}
				encoded, err := json.Marshal(model)
				if err != nil {
					t.Fatal(err)
				}
				var public map[string]any
				if json.Unmarshal(encoded, &public) != nil || len(public) != 4 {
					t.Fatalf("model JSON contract changed: %s", encoded)
				}
			}
		})
	}
}

func TestOriginalPolicyUnsetConfigurationPreservesStrictZDRDefault(t *testing.T) {
	var endpointCalls atomic.Int32
	setupRouterPolicyTest(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
			testCatalogEntry("test/strict:free"), testCatalogEntry("test/unverified:free"),
		}})
	}, func(w http.ResponseWriter, r *http.Request) {
		endpointCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{
			"model_id": "test/strict:free", "tag": "private-provider", "status": 0,
			"safety_retention": false, "pricing": map[string]string{"prompt": "0", "completion": "0"},
		}}})
	})
	t.Setenv("OPENROUTER_REQUIRE_ZDR", "")
	models, err := listFreeAssistantModels(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "test/strict:free" || !reflect.DeepEqual(models[0].privateProviders, []string{"private-provider"}) {
		t.Fatalf("unset configuration weakened existing policy: models=%+v err=%v", models, err)
	}
	if endpointCalls.Load() != 1 {
		t.Fatalf("strict default did not verify endpoint metadata: calls=%d", endpointCalls.Load())
	}
}

func TestOriginalPolicyFallbackRecoversWithinMessageAndRemembersReplacement(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusNotFound, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var mu sync.Mutex
			attempted := []string{}
			setupRouterPolicyTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					paid := testCatalogEntry("test/paid:free")
					paid["pricing"] = map[string]string{"prompt": "0.00001", "completion": "0"}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
						testCatalogEntry(preferredAssistantModelID), testCatalogEntry("test/replacement:free"), paid,
					}})
					return
				}
				payload := originalPolicyPayload(t, r)
				mu.Lock()
				attempted = append(attempted, payload.Model)
				mu.Unlock()
				if payload.Model == preferredAssistantModelID {
					w.Header().Set("Retry-After", "30")
					w.WriteHeader(status)
					if status == http.StatusNotFound {
						_, _ = w.Write([]byte(`{"error":{"code":404,"message":"No endpoints found matching your data policy","metadata":{"provider_name":"fixture"}}}`))
					} else {
						_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": status, "metadata": map[string]any{"provider_name": "fixture"}}})
					}
					return
				}
				if payload.Model != "test/replacement:free" {
					t.Errorf("fallback attempted an unapproved or paid model: %s", payload.Model)
				}
				writeRouterReply(w, "Recovered response")
			}, func(w http.ResponseWriter, r *http.Request) {
				t.Error("original-policy fallback fetched optional ZDR metadata")
				w.WriteHeader(http.StatusServiceUnavailable)
			})
			t.Setenv("OPENROUTER_REQUIRE_ZDR", "false")
			t.Setenv("OPENROUTER_USER_RPM", "60")
			t.Setenv("OPENROUTER_USER_DAILY_LIMIT", "50")
			userCtx := withAssistantUsage(context.Background(), uint(992000+status))
			identity := userCtx.Value(assistantUsageContextKey{}).(assistantUsageIdentity)
			assistantCallUsage.Lock()
			delete(assistantCallUsage.users, identity)
			assistantCallUsage.Unlock()
			t.Cleanup(func() {
				assistantCallUsage.Lock()
				delete(assistantCallUsage.users, identity)
				assistantCallUsage.Unlock()
			})
			firstCtx := withAssistantMessageBudget(userCtx)
			generator, err := newAssistantGeneratorForModel(firstCtx, "")
			if err != nil {
				t.Fatal(err)
			}
			if reply, err := generator.generateText(GeminiRequest{}); err != nil || reply != "Recovered response" {
				t.Fatalf("same-message automatic fallback failed: reply=%q err=%v", reply, err)
			}
			client := generator.(*openRouterClient)
			if client.modelUsed() != "test/replacement:free" || routerCooldownRemaining(client.account+":"+preferredAssistantModelID) <= 0 || routerCooldownRemaining(client.account) != 0 {
				t.Fatal("model failure was not isolated or replacement was not retained")
			}
			// A later generation in the same HTTP message must not spend more.
			if _, err := generator.generateText(GeminiRequest{}); err == nil {
				t.Fatal("same message was allowed another generation after recovery")
			}
			next, err := newAssistantGeneratorForModel(withAssistantMessageBudget(userCtx), "")
			if err != nil {
				t.Fatal(err)
			}
			if reply, err := next.generateText(GeminiRequest{}); err != nil || reply != "Recovered response" {
				t.Fatalf("next message did not reuse replacement: reply=%q err=%v", reply, err)
			}
			mu.Lock()
			got := append([]string(nil), attempted...)
			mu.Unlock()
			want := []string{preferredAssistantModelID, "test/replacement:free", "test/replacement:free"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("fallback repeated a failing/paid model or exceeded attempts: got=%v want=%v", got, want)
			}
			assistantCallUsage.Lock()
			entry := assistantCallUsage.users[identity]
			actual := 0
			if entry != nil {
				actual = entry.daily
			}
			assistantCallUsage.Unlock()
			if actual != 3 {
				t.Fatalf("model rotation reset or failed to charge attempt allowance: daily=%d want=3", actual)
			}
		})
	}
}

func TestOriginalPolicyGlobalLimitNeverRotatesOrResetsCooldown(t *testing.T) {
	var posts atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		originalPolicyPayload(t, r)
		posts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"message":"daily limit exceeded","metadata":{"limit_source":"openrouter"}}}`))
	})
	t.Setenv("OPENROUTER_REQUIRE_ZDR", "false")
	for range 2 {
		generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = generator.generateText(GeminiRequest{})
		var typed OpenRouterError
		if !errors.As(err, &typed) || !typed.Global || typed.Code != "assistant_quota_exceeded" {
			t.Fatalf("global limit was weakened: %v", err)
		}
	}
	if posts.Load() != 1 {
		t.Fatalf("global quota caused more completion attempts: %d", posts.Load())
	}
}

func TestOriginalPolicyInvalidZDRConfigurationFailsBeforeRequests(t *testing.T) {
	var requests atomic.Int32
	setupRouterPolicyTest(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeTestCatalog(w)
	}, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	t.Setenv("OPENROUTER_REQUIRE_ZDR", "unrecognized")
	if models, err := listFreeAssistantModels(context.Background()); err == nil || len(models) != 0 {
		t.Fatalf("invalid privacy configuration exposed a catalog: models=%v err=%v", models, err)
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid configuration triggered outbound requests: %d", requests.Load())
	}
}
