//go:build integration

package main

import (
	"Sobra_Ai_Back-end/internal/assistant"
	"Sobra_Ai_Back-end/internal/auth"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/migrations"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type automaticRoutingResponse struct {
	status int
	body   string
}

type automaticRoutingTransport struct {
	base      http.RoundTripper
	mu        sync.Mutex
	responses []automaticRoutingResponse
	models    []string
}

func (transport *automaticRoutingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Hostname() == "127.0.0.1" {
		return transport.base.RoundTrip(request)
	}
	if request.URL.Hostname() != "openrouter.ai" {
		return nil, fmt.Errorf("external network forbidden in automatic routing integration")
	}
	var body []byte
	status := http.StatusOK
	if request.Method == http.MethodGet && request.URL.Path == "/api/v1/models" {
		var err error
		body, err = integrationModelCatalogJSON()
		if err != nil {
			return nil, err
		}
	} else if request.Method == http.MethodPost && request.URL.Path == "/api/v1/chat/completions" {
		var payload struct{ Model string }
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			return nil, err
		}
		transport.mu.Lock()
		defer transport.mu.Unlock()
		transport.models = append(transport.models, payload.Model)
		if len(transport.responses) == 0 {
			return nil, fmt.Errorf("unexpected automatic routing provider attempt")
		}
		next := transport.responses[0]
		transport.responses = transport.responses[1:]
		status = next.status
		body = []byte(next.body)
	} else {
		return nil, fmt.Errorf("unexpected automatic routing provider endpoint")
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: request}, nil
}

func (transport *automaticRoutingTransport) setResponses(responses ...automaticRoutingResponse) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.responses = responses
	transport.models = nil
}

func (transport *automaticRoutingTransport) attemptedModels() []string {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]string(nil), transport.models...)
}

func automaticRoutingReply(content string) automaticRoutingResponse {
	body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"message": map[string]any{"content": content}, "finish_reason": "stop",
	}}})
	return automaticRoutingResponse{status: 200, body: string(body)}
}

func TestIntegrationAssistantAutomaticRouting(t *testing.T) {
	db := disposablePostgres(t)
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	previousDB, previousTransport := database.DB, http.DefaultTransport
	database.DB = db
	transport := &automaticRoutingTransport{base: previousTransport}
	http.DefaultTransport = transport
	t.Cleanup(func() { database.DB, http.DefaultTransport = previousDB, previousTransport })
	t.Setenv("JWT_SECRET", "automatic-routing-jwt-only")
	t.Setenv("OPENROUTER_USER_RPM", "60")
	t.Setenv("OPENROUTER_USER_DAILY_LIMIT", "1000")
	owner := auth.User{Name: "Automatic routing", Email: "automatic-routing@example.test", Password: "unused"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": owner.ID, "role": "user", "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte("automatic-routing-jwt-only"))
	if err != nil {
		t.Fatal(err)
	}
	storedTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.Exec("INSERT INTO assistant_model_preferences (user_id, model_id, updated_at) VALUES (?, ?, ?)", owner.ID, "mock/settings-b:free", storedTime).Error; err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(newRouter())
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 8 * time.Second}
	request := func(t *testing.T, method, path string, data any, expected ...int) map[string]any {
		t.Helper()
		body, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var decoded map[string]any
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		want := http.StatusOK
		if len(expected) != 0 {
			want = expected[0]
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s returned %d: %+v", method, path, response.StatusCode, decoded)
		}
		return decoded
	}
	assertPreferencePreserved := func(t *testing.T) {
		t.Helper()
		var stored struct {
			ModelID   string
			UpdatedAt time.Time
		}
		if err := db.Table("assistant_model_preferences").Where("user_id = ?", owner.ID).First(&stored).Error; err != nil || stored.ModelID != "mock/settings-b:free" || !stored.UpdatedAt.Equal(storedTime) {
			t.Fatalf("automatic chat reset legacy preference: %+v %v", stored, err)
		}
		if legacy := request(t, "GET", "/api/assistant/model-preference", nil); legacy["model_id"] != "mock/settings-b:free" || legacy["available"] != true {
			t.Fatalf("legacy preference route changed: %+v", legacy)
		}
	}

	t.Run("rotates_only_on_next_message_without_duplicate_writes", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "automatic-routing-preference-only")
		transport.setResponses(
			automaticRoutingReply(`{"intent":"general","reply":"Automatico em operacao."}`),
			automaticRoutingReply(`{"intent":"general","reply":"Escolha antiga compativel."}`),
			automaticRoutingReply(`{"intent":"general","reply":"Continuamos na mesma conversa."}`),
			automaticRoutingResponse{429, `{"error":{"code":429,"message":"provider busy","metadata":{"provider_name":"fixture-provider","provider_code":429}}}`},
			automaticRoutingReply(`{"intent":"create_expense","description":"Compra automatica","amount":60,"category_name":"Mercado automatico","payment_source":"Salario","month":10,"year":2031,"day":2,"type":"Unica","installments":1}`),
		)
		category := request(t, "POST", "/api/categories/", map[string]any{"name": "Mercado automatico"}, 201)
		if category["data"] == nil {
			t.Fatal("missing category fixture")
		}
		first := request(t, "POST", "/api/assistant/chat", map[string]any{"message": "vamos organizar minhas despesas"})
		conversation := first["conversation_id"]
		request(t, "POST", "/api/assistant/chat", map[string]any{"conversation_id": conversation, "message": "oi de novo", "model_id": "mock/settings-b:free"})
		automatic := request(t, "POST", "/api/assistant/chat", map[string]any{"conversation_id": conversation, "message": "continuamos?", "model_id": ""})
		if automatic["conversation_id"] != conversation {
			t.Fatal("automatic selection replaced the conversation")
		}
		models := transport.attemptedModels()
		if len(models) != 3 || models[0] != "mock/api:free" || models[1] != "mock/settings-b:free" || models[2] != "mock/api:free" {
			t.Fatalf("absent/empty selection used stored preference or legacy override failed: %v", models)
		}
		failed := request(t, "POST", "/api/assistant/chat", map[string]any{"conversation_id": conversation, "message": "cadastra sessenta reais de Mercado automatico, unica, salario, dois de outubro de 2031"})
		if failed["error_code"] != "assistant_quota_exceeded" || len(transport.attemptedModels()) != 4 {
			t.Fatalf("failed message must spend only one call: %+v", failed)
		}
		proposal := request(t, "POST", "/api/assistant/chat", map[string]any{"conversation_id": conversation, "message": "tenta cadastrar de novo"})
		if proposal["conversation_id"] != conversation || proposal["tool_call"] != "create_expense" || proposal["tool_result"].(map[string]any)["status"] != "needs_confirmation" {
			t.Fatalf("fallback did not finish the same proposal: %+v", proposal)
		}
		models = transport.attemptedModels()
		catalog := request(t, "GET", "/api/assistant/models", nil)
		allowed := make(map[string]bool)
		for _, model := range catalog["models"].([]any) {
			allowed[model.(map[string]any)["id"].(string)] = true
		}
		if len(models) != 5 || models[3] != "mock/api:free" || models[4] == models[3] || !allowed[models[4]] {
			t.Fatalf("model-level 429 did not try another allowed free model: %v", models)
		}
		var count int64
		if err := db.Model(&expenses.Expense{}).Where("user_id = ?", owner.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("fallback wrote before user confirmation: %d %v", count, err)
		}
		created := request(t, "POST", "/api/assistant/chat", map[string]any{"conversation_id": conversation, "message": "sim"})
		if created["tool_result"].(map[string]any)["status"] != "created" {
			t.Fatalf("confirmed fallback proposal was not created: %+v", created)
		}
		request(t, "POST", "/api/assistant/chat", map[string]any{"conversation_id": conversation, "message": "sim"})
		if len(transport.attemptedModels()) != 5 {
			t.Fatal("confirmation retried model inference")
		}
		if err := db.Model(&expenses.Expense{}).Where("user_id = ?", owner.ID).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("confirmation duplicated purchase: %d %v", count, err)
		}
		for _, role := range []string{"user", "assistant"} {
			if err := db.Model(&assistant.Message{}).Where("conversation_id = ? AND user_id = ? AND role = ?", conversation, owner.ID, role).Count(&count).Error; err != nil || count != 6 {
				t.Fatalf("fallback duplicated conversation messages for %s: %d %v", role, count, err)
			}
		}
		assertPreferencePreserved(t)
	})

	t.Run("preserves_formatted_reply_and_history_without_extra_inference", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "automatic-routing-formatted-reply-only")
		richReply := "💡 **Organize por categoria**\n\n- **Veja o resumo** do mês.\n- Revise \"Transporte\" com calma."
		intent, err := json.Marshal(map[string]any{"intent": "app_help", "reply": richReply})
		if err != nil {
			t.Fatal(err)
		}
		transport.setResponses(automaticRoutingReply(string(intent)))
		message := "Como posso organizar meus gastos?"
		answer := request(t, "POST", "/api/assistant/chat", map[string]any{"message": message})
		if answer["reply"] != richReply || answer["tool_call"] != "app_help" || len(transport.attemptedModels()) != 1 {
			t.Fatalf("formatted reply was modified or required extra inference: %+v", answer)
		}
		conversation := fmt.Sprintf("%.0f", answer["conversation_id"].(float64))
		history := request(t, "GET", "/api/assistant/conversations/"+conversation+"/messages", nil)
		messages := history["messages"].([]any)
		if len(messages) != 2 || messages[0].(map[string]any)["content"] != message || messages[1].(map[string]any)["content"] != richReply {
			t.Fatalf("formatting changed the persisted conversation: %+v", messages)
		}
		assertPreferencePreserved(t)
	})

	for _, status := range []int{429, 402} {
		t.Run(fmt.Sprintf("global_%d_stops_retries", status), func(t *testing.T) {
			t.Setenv("OPENROUTER_API_KEY", fmt.Sprintf("automatic-routing-global-%d-only", status))
			body := fmt.Sprintf(`{"error":{"code":%d,"message":"account limit","metadata":{"error_type":"rate_limit_exceeded"}}}`, status)
			transport.setResponses(automaticRoutingResponse{status, body})
			first := request(t, "POST", "/api/assistant/chat", map[string]any{"message": "quanto posso consultar agora?"})
			if first["error_code"] != "assistant_quota_exceeded" || first["retry_after_seconds"].(float64) <= 0 || len(transport.attemptedModels()) != 1 {
				t.Fatalf("global quota retried or returned incompatible error: %+v models=%v", first, transport.attemptedModels())
			}
			second := request(t, "POST", "/api/assistant/chat", map[string]any{"conversation_id": first["conversation_id"], "message": "e agora?", "model_id": "mock/settings-b:free"})
			if second["error_code"] != "assistant_quota_exceeded" || len(transport.attemptedModels()) != 1 {
				t.Fatalf("legacy override bypassed account cooldown: %+v models=%v", second, transport.attemptedModels())
			}
		})
	}

	t.Run("user_limit_is_shared_across_automatic_and_legacy_selection", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "automatic-routing-user-limit-only")
		t.Setenv("OPENROUTER_USER_DAILY_LIMIT", "1")
		transport.setResponses(automaticRoutingReply(`{"intent":"general","reply":"Primeira mensagem concluida."}`))
		first := request(t, "POST", "/api/assistant/chat", map[string]any{"message": "oi"})
		for _, model := range []string{"mock/settings-b:free", ""} {
			limited := request(t, "POST", "/api/assistant/chat", map[string]any{"conversation_id": first["conversation_id"], "message": "mais uma", "model_id": model})
			if limited["error_code"] != "assistant_user_limit" || limited["retry_after_seconds"].(float64) <= 0 || len(transport.attemptedModels()) != 1 {
				t.Fatalf("changing selection bypassed user allowance: %+v models=%v", limited, transport.attemptedModels())
			}
		}
		assertPreferencePreserved(t)
	})
}
