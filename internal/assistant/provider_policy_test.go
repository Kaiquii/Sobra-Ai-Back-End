package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestProviderPolicyRequiresExactFreeActiveNonRetainingEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(map[string]any)
		allowed bool
	}{
		{"compatible", func(e map[string]any) {}, true},
		{"paid deployment", func(e map[string]any) { e["pricing"] = map[string]string{"prompt": "0", "completion": "0.01"} }, false},
		{"paid request", func(e map[string]any) {
			e["pricing"] = map[string]string{"prompt": "0", "completion": "0", "request": "0.01"}
		}, false},
		{"different variant", func(e map[string]any) { e["model_id"] = "test/policy" }, false},
		{"safety retention", func(e map[string]any) { e["safety_retention"] = true }, false},
		{"unknown retention", func(e map[string]any) { delete(e, "safety_retention") }, false},
		{"null retention", func(e map[string]any) { e["safety_retention"] = nil }, false},
		{"inactive", func(e map[string]any) { e["status"] = -2 }, false},
		{"unknown status", func(e map[string]any) { delete(e, "status") }, false},
		{"invalid tag", func(e map[string]any) { e["tag"] = "provider,unsafe" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupRouterPolicyTest(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{testCatalogEntry("test/policy:free")}})
			}, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" {
					t.Error("public metadata must not receive credentials")
				}
				e := map[string]any{"model_id": "test/policy:free", "tag": "private-provider", "status": 0, "safety_retention": false, "pricing": map[string]string{"prompt": "0", "completion": "0"}}
				tc.change(e)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{e}})
			})
			models, err := listFreeAssistantModels(context.Background())
			if (err == nil) != tc.allowed {
				t.Fatalf("policy allowed=%t models=%v err=%v", tc.allowed, models, err)
			}
			if tc.allowed && !reflect.DeepEqual(models[0].privateProviders, []string{"private-provider"}) {
				t.Fatal("route restrictions lost")
			}
		})
	}
}

func TestProviderPolicyMetadataFailureDiscardsExpiredSnapshotAndRecovers(t *testing.T) {
	failing := false
	setupRouterPolicyTest(t, func(w http.ResponseWriter, r *http.Request) { writeTestCatalog(w) }, func(w http.ResponseWriter, r *http.Request) {
		if failing {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"model_id": "test/first:free", "tag": "private-provider", "status": 0, "safety_retention": false, "pricing": map[string]string{"prompt": "0", "completion": "0"}}}})
	})
	models, err := listFreeAssistantModels(context.Background())
	if err != nil || len(models) != 1 {
		t.Fatal("initial snapshot unavailable")
	}
	models[0].privateProviders[0] = "caller-mutation"
	again, err := listFreeAssistantModels(context.Background())
	if err != nil || again[0].privateProviders[0] != "private-provider" {
		t.Fatal("caller mutated cached restrictions")
	}
	failing = true
	assistantModelCatalog.Lock()
	assistantModelCatalog.expiresAt = time.Now().Add(-time.Second)
	assistantModelCatalog.Unlock()
	if models, err := listFreeAssistantModels(context.Background()); err == nil || len(models) != 0 {
		t.Fatal("stale restrictions used after metadata failure")
	}
	failing = false
	assistantModelCatalog.Lock()
	assistantModelCatalog.retryAt = time.Now().Add(-time.Second)
	assistantModelCatalog.Unlock()
	if models, err := listFreeAssistantModels(context.Background()); err != nil || len(models) != 1 {
		t.Fatal("metadata did not recover")
	}
}

func TestProviderPolicyPinsCompletionToVerifiedRoutes(t *testing.T) {
	posts := 0
	setupRouterPolicyTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		posts++
		var payload openRouterRequest
		_ = json.NewDecoder(r.Body).Decode(&payload)
		only, _ := payload.Provider["only"].([]any)
		if payload.Model != "test/second:free" || len(only) != 1 || only[0] != "approved/fp16" || payload.Provider["data_collection"] != "deny" || payload.Provider["zdr"] != true {
			t.Error("completion used unverified route")
		}
		writeRouterReply(w, "Ola")
	}, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"model_id": "test/second:free", "tag": "approved/fp16", "status": 0, "safety_retention": false, "pricing": map[string]string{"prompt": "0", "completion": "0"}}}})
	})
	g, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "test/first:free")
	if err != nil {
		t.Fatal(err)
	}
	if text, err := g.generateText(GeminiRequest{}); err != nil || text != "Ola" || posts != 1 {
		t.Fatalf("compatible route failed: %q %v calls=%d", text, err, posts)
	}
}
