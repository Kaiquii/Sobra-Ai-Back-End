package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFreeModelPricingRejectsEveryChargeAndUnknownPrice(t *testing.T) {
	cases := []struct {
		name, raw string
		free      bool
	}{
		{"free", `{"prompt":"0","completion":"0"}`, true},
		{"scientific zero", `{"prompt":"0e-6","completion":0}`, true},
		{"extra zero prices", `{"prompt":"0","completion":"0","request":"0","image":"0","web_search":"0"}`, true},
		{"missing completion", `{"prompt":"0"}`, false},
		{"empty", `{}`, false},
		{"prompt charge", `{"prompt":"0.0000001","completion":"0"}`, false},
		{"underflow charge", `{"prompt":"1e-9999","completion":"0"}`, false},
		{"request charge", `{"prompt":"0","completion":"0","request":"0.001"}`, false},
		{"image charge", `{"prompt":"0","completion":"0","image":"1"}`, false},
		{"search charge", `{"prompt":"0","completion":"0","web_search":"0.01"}`, false},
		{"future billing field", `{"prompt":"0","completion":"0","audio":"0.1"}`, false},
		{"unknown price", `{"prompt":"0","completion":"0","request":null}`, false},
		{"negative price", `{"prompt":"0","completion":"-1"}`, false},
		{"NaN price", `{"prompt":"0","completion":"NaN"}`, false},
		{"override charge", `{"prompt":"0","completion":"0","overrides":[{"min_prompt_tokens":10000,"prompt":"0.1","completion":"0"}]}`, false},
		{"free override", `{"prompt":"0","completion":"0","overrides":[{"min_prompt_tokens":10000,"prompt":"0","completion":"0"}]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pricing map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.raw), &pricing); err != nil {
				t.Fatal(err)
			}
			if got := freeModelPricing(pricing); got != tc.free {
				t.Fatalf("freeModelPricing = %v; want %v", got, tc.free)
			}
		})
	}
}

func TestAssistantCatalogReturnsAllFreeChatModelsWithoutWhitelistOrCapabilityGate(t *testing.T) {
	var calls atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("catalog must use GET")
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("public model catalog must not receive token")
		}
		calls.Add(1)
		recommended := testCatalogEntry(preferredAssistantModelID)
		recommended["name"] = "Z Dots"
		recommended["context_length"] = 0
		recommended["supported_parameters"] = []string{}
		direct := testCatalogEntry("vendor/direct-zero")
		direct["name"] = "Beta"
		direct["context_length"] = 4000
		direct["supported_parameters"] = []string{"max_completion_tokens"}
		alpha := testCatalogEntry("vendor/alpha:free")
		alpha["name"] = "Alpha\n Model\x00"
		alpha["description"] = strings.Repeat("Olá\n", 200)
		alpha["supported_parameters"] = []string{}
		safety := testCatalogEntry("vendor/safety:free")
		safety["name"] = "Safety"
		freeRouter := testCatalogEntry("openrouter/free")
		freeRouter["name"] = "Free Router"
		freeRouter["architecture"].(map[string]any)["tokenizer"] = "Router"
		paid := testCatalogEntry("vendor/paid:free")
		paid["pricing"] = map[string]string{"prompt": "0", "completion": "0.01"}
		fee := testCatalogEntry("vendor/request-fee")
		fee["pricing"] = map[string]string{"prompt": "0", "completion": "0", "request": "0.01"}
		music := testCatalogEntry("vendor/music")
		music["architecture"].(map[string]any)["output_modalities"] = []string{"text", "audio"}
		rerank := testCatalogEntry("vendor/rerank:free")
		rerank["architecture"].(map[string]any)["output_modalities"] = []string{"score"}
		opaqueRouter := testCatalogEntry("openrouter/auto")
		opaqueRouter["architecture"].(map[string]any)["tokenizer"] = "Router"
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{paid, safety, freeRouter, direct, music, alpha, recommended, rerank, opaqueRouter, fee, alpha}})
	})
	// The old environment whitelist must no longer hide available free models.
	t.Setenv("OPENROUTER_MODELS", "vendor/alpha:free")
	models, err := listFreeAssistantModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	want := []string{preferredAssistantModelID, "vendor/alpha:free", "vendor/direct-zero", "openrouter/free", "vendor/safety:free"}
	if !reflect.DeepEqual(ids, want) || !models[0].Recommended {
		t.Fatalf("catalog missed a free chat option or admitted paid/non-chat models: %+v", models)
	}
	for _, model := range models[1:] {
		if model.Recommended {
			t.Fatal("catalog must recommend exactly one model")
		}
	}
	if models[1].Name != "Alpha Model" || len([]rune(models[1].Description)) > 500 || strings.Contains(models[1].Description, "\n") {
		t.Fatal("catalog descriptions or names contain unbounded/control text")
	}
	if model := models[2]; !model.SupportsMaxCompletionTokens || model.SupportsMaxTokens || model.SupportsResponseFormat || model.SupportsTemperature || model.ContextLength != 4000 {
		t.Fatalf("optional capabilities were not preserved for provider adaptation: %+v", model)
	}
	encoded, err := json.Marshal(models[2])
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if err := json.Unmarshal(encoded, &public); err != nil || len(public) != 4 {
		t.Fatalf("internal capability flags changed the public model contract: %s %v", encoded, err)
	}
	models[0].ID = "mutated/by-caller:free"
	again, err := listFreeAssistantModels(context.Background())
	if err != nil || calls.Load() != 1 || again[0].ID != preferredAssistantModelID {
		t.Fatalf("cache must return copy without another request: %+v, calls=%d, err=%v", again, calls.Load(), err)
	}
}

func TestEligibleFreeModelAcceptsCatalogShape(t *testing.T) {
	data, err := json.Marshal(testCatalogEntry("test/first:free"))
	if err != nil {
		t.Fatal(err)
	}
	var entry catalogModel
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	if !eligibleFreeModel(entry) {
		t.Fatalf("catalog fixture ineligible: %+v", entry)
	}
}

func TestAssistantCatalogDoesNotTrustExpiredPriceData(t *testing.T) {
	var available atomic.Bool
	available.Store(true)
	var calls atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !available.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		writeTestCatalog(w)
	})
	if _, err := listFreeAssistantModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	available.Store(false)
	assistantModelCatalog.Lock()
	assistantModelCatalog.expiresAt = time.Now().Add(-time.Second)
	assistantModelCatalog.Unlock()
	if _, err := listFreeAssistantModels(context.Background()); err == nil {
		t.Fatal("expired catalog must fail closed when current pricing cannot be verified")
	}
	if models, err := listFreeAssistantModels(context.Background()); err == nil || len(models) != 0 || calls.Load() != 2 {
		t.Fatal("temporary failure must be cached without returning stale price data")
	}
	available.Store(true)
	assistantModelCatalog.Lock()
	assistantModelCatalog.retryAt = time.Now().Add(-time.Second)
	assistantModelCatalog.Unlock()
	if _, err := listFreeAssistantModels(context.Background()); err != nil || calls.Load() != 3 {
		t.Fatalf("catalog did not recover after short negative cache: %v", err)
	}
}

func TestAssistantCatalogRecommendsFirstAlphabeticallyWithoutPreferredModel(t *testing.T) {
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		z := testCatalogEntry("vendor/z:free")
		z["name"] = "Zulu"
		a := testCatalogEntry("vendor/a:free")
		a["name"] = "Alpha"
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{z, a}})
	})
	models, err := listFreeAssistantModels(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "vendor/a:free" || !models[0].Recommended || models[1].Recommended {
		t.Fatalf("alphabetical recommendation is unstable: %+v %v", models, err)
	}
}

func TestAssistantCatalogSharedRefreshAllowsCallerCancellation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var releaseOnce sync.Once
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-release
		writeTestCatalog(w)
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := listFreeAssistantModels(ctx); first <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("catalog refresh did not start")
	}
	second := make(chan error, 1)
	go func() { _, err := listFreeAssistantModels(context.Background()); second <- err }()
	cancel()
	select {
	case err := <-first:
		if err == nil {
			t.Fatal("cancelled caller should stop waiting")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("catalog mutex or fetch blocked caller cancellation")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("one caller cancelled the shared refresh for other users: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shared catalog result did not reach waiting caller")
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent cache miss caused duplicate fetches: %d", calls.Load())
	}
}
