//go:build integration

package main

import (
	"Sobra_Ai_Back-end/internal/assistant"
	"Sobra_Ai_Back-end/internal/auth"
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"Sobra_Ai_Back-end/internal/migrations"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type semanticsCompletionRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	MaxTokens           *int           `json:"max_tokens"`
	MaxCompletionTokens *int           `json:"max_completion_tokens"`
	Provider            map[string]any `json:"provider"`
}

// Only this test server may use real HTTP. Every OpenRouter response is local,
// including catalog discovery; unexpected external destinations fail closed.
type semanticsTransport struct {
	base      http.RoundTripper
	localHost string
	mu        sync.Mutex
	response  string
	requests  []semanticsCompletionRequest
}

func (transport *semanticsTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme == "http" && request.URL.Host == transport.localHost {
		return transport.base.RoundTrip(request)
	}
	if request.URL.Scheme != "https" || request.URL.Host != "openrouter.ai" {
		return nil, fmt.Errorf("external network forbidden in assistant semantics integration")
	}
	var body []byte
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/api/v1/models":
		var err error
		body, err = integrationModelCatalogJSON()
		if err != nil {
			return nil, err
		}
	case request.Method == http.MethodPost && request.URL.Path == "/api/v1/chat/completions":
		var payload semanticsCompletionRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			return nil, err
		}
		transport.mu.Lock()
		defer transport.mu.Unlock()
		transport.requests = append(transport.requests, payload)
		if transport.response == "" {
			return nil, fmt.Errorf("unexpected extra semantics completion")
		}
		body = []byte(transport.response)
		transport.response = ""
	default:
		return nil, fmt.Errorf("unexpected semantics provider endpoint")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: request}, nil
}

func (transport *semanticsTransport) plan(t *testing.T, content, finishReason string) {
	t.Helper()
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.response != "" {
		t.Fatal("previous semantics completion was not consumed")
	}
	transport.requests = nil
	if finishReason == "" {
		finishReason = "stop"
	}
	encoded, err := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"message": map[string]any{"content": content}, "finish_reason": finishReason,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	transport.response = string(encoded)
}

func (transport *semanticsTransport) assertCalls(t *testing.T, want int) string {
	t.Helper()
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.requests) != want || transport.response != "" {
		t.Fatalf("completion budget: got %d calls, want %d, unused response=%t", len(transport.requests), want, transport.response != "")
	}
	var prompt strings.Builder
	for _, request := range transport.requests {
		// The shared synthetic catalog advertises max_tokens for every free
		// model. Check the 2048-token ceiling through either wire spelling
		// rather than depending on an internal provider constant or adapter.
		if request.MaxTokens == nil && request.MaxCompletionTokens == nil {
			t.Fatal("completion omitted the supported output token budget")
		}
		for _, limit := range []*int{request.MaxTokens, request.MaxCompletionTokens} {
			if limit != nil && (*limit <= 0 || *limit > 2048) {
				t.Fatalf("completion output budget exceeds 2048: %d", *limit)
			}
		}
		if request.Provider["data_collection"] != "deny" || request.Provider["require_parameters"] != true {
			t.Fatalf("provider privacy policy changed: %+v", request.Provider)
		}
		price, ok := request.Provider["max_price"].(map[string]any)
		if !ok || price["prompt"] != float64(0) || price["completion"] != float64(0) || price["request"] != float64(0) {
			t.Fatal("completion lost the free-only provider price ceiling")
		}
		for _, message := range request.Messages {
			prompt.WriteString(message.Content)
			prompt.WriteByte('\n')
		}
	}
	text := prompt.String()
	for _, private := range []string{"FOREIGN_ONLY_CATEGORY", "FOREIGN_PRIVATE_NOTES", "OWNER_PRIVATE_NOTES", "9999", "FORGED_CLIENT_HISTORY"} {
		if strings.Contains(text, private) {
			t.Fatalf("untrusted or private data entered the provider prompt: %s", private)
		}
	}
	return text
}

func semanticsObject(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object, got %T: %+v", value, value)
	}
	return result
}

func semanticsRows(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()
	raw, ok := result["rows"].([]any)
	if !ok {
		t.Fatalf("missing financial rows: %+v", result)
	}
	rows := make([]map[string]any, len(raw))
	for i, value := range raw {
		rows[i] = semanticsObject(t, value)
	}
	return rows
}

func semanticsFinancialSnapshot(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range []string{"categories", "expenses", "expense_payment_splits", "incomes", "fgts_balances", "investments", "investment_categories"} {
		var snapshot string
		// Names are the fixed allowlist above; no request data enters this SQL.
		query := "SELECT COALESCE(jsonb_agg(to_jsonb(entry) ORDER BY to_jsonb(entry)::text), '[]'::jsonb)::text FROM " + table + " AS entry"
		if err := db.Raw(query).Scan(&snapshot).Error; err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		result[table] = snapshot
	}
	return result
}

func TestIntegrationAssistantFinancialSemantics(t *testing.T) {
	db := disposablePostgres(t)
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	previousDB, previousTransport := database.DB, http.DefaultTransport
	database.DB = db
	transport := &semanticsTransport{base: previousTransport}
	http.DefaultTransport = transport
	t.Cleanup(func() { database.DB, http.DefaultTransport = previousDB, previousTransport })
	t.Setenv("JWT_SECRET", "semantics-jwt-synthetic-only")
	t.Setenv("OPENROUTER_API_KEY", "semantics-synthetic-only")
	t.Setenv("OPENROUTER_USER_RPM", "60")
	t.Setenv("OPENROUTER_USER_DAILY_LIMIT", "1000")

	owner := auth.User{Name: "Semantics owner", Email: "semantics-owner@example.test", Password: "unused"}
	other := auth.User{Name: "Semantics other", Email: "semantics-other@example.test", Password: "unused"}
	for _, user := range []*auth.User{&owner, &other} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedCategory := func(userID uint, name string) categories.Category {
		t.Helper()
		category := categories.Category{UserID: userID, Name: name}
		if err := db.Create(&category).Error; err != nil {
			t.Fatal(err)
		}
		return category
	}
	uber := seedCategory(owner.ID, "Uber/99")
	market := seedCategory(owner.ID, "Mercado")
	installments := seedCategory(owner.ID, "Compras parceladas")
	otherUber := seedCategory(other.ID, "Uber/99")
	foreignOnly := seedCategory(other.ID, "FOREIGN_ONLY_CATEGORY")
	seedExpense := func(userID, categoryID uint, description string, amount float64, month, year int, series string, current, count int) {
		t.Helper()
		kind, notes := "Unica", "OWNER_PRIVATE_NOTES"
		if series != "" {
			kind = "Parcelada"
		}
		if userID == other.ID {
			notes = "FOREIGN_PRIVATE_NOTES"
		}
		row := expenses.Expense{
			UserID: userID, CategoryID: categoryID, Description: description, Amount: amount, Notes: notes,
			Date: time.Date(year, time.Month(month), 5, 12, 0, 0, 0, time.UTC), Month: month, Year: year,
			Type: kind, PaymentSource: "Salario", SeriesID: series, CurrentInstall: current, Installments: count,
			PaymentSplits: []expenses.PaymentSplit{{PaymentSource: "Salario", Amount: amount}},
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedExpense(owner.ID, uber.ID, "Uber outubro", 80.50, 10, 2034, "", 1, 1)
	seedExpense(owner.ID, uber.ID, "Uber novembro", 230, 11, 2034, "", 1, 1)
	seedExpense(owner.ID, uber.ID, "Uber outro ano", 40, 10, 2025, "", 1, 1)
	seedExpense(owner.ID, market.ID, "Mercado outubro", 1000, 10, 2034, "", 1, 1)
	seedExpense(other.ID, otherUber.ID, "Uber outro usuario", 9999, 10, 2034, "", 1, 1)
	seedExpense(other.ID, foreignOnly.ID, "Despesa privada", 9999, 11, 2034, "", 1, 1)
	for index, month := range []int{10, 11, 12} {
		seedExpense(owner.ID, installments.ID, "Compra A", 40, month, 2034, "semantics-series-a", index+1, 3)
	}
	for month := 1; month <= 10; month++ {
		seedExpense(owner.ID, installments.ID, "Compra B", 30, month, 2034, "semantics-series-b", month, 10)
	}
	if err := db.Create(&incomes.Income{UserID: owner.ID, Source: "Salario", Amount: 2000, Month: 10, Year: 2034}).Error; err != nil {
		t.Fatal(err)
	}
	before := semanticsFinancialSnapshot(t, db)
	t.Cleanup(func() {
		if after := semanticsFinancialSnapshot(t, db); !reflect.DeepEqual(before, after) {
			t.Error("read-only assistant requests changed financial rows, payment splits or categories")
		}
	})
	tokenFor := func(user auth.User) string {
		t.Helper()
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": user.ID, "role": "user", "exp": time.Now().Add(time.Hour).Unix(),
		}).SignedString([]byte("semantics-jwt-synthetic-only"))
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	ownerToken, otherToken := tokenFor(owner), tokenFor(other)
	server := httptest.NewServer(newRouter())
	transport.localHost = strings.TrimPrefix(server.URL, "http://")
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 8 * time.Second}
	request := func(t *testing.T, method, path, token string, body any, want int) map[string]any {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(string(encoded)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var decoded map[string]any
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d body=%+v", method, path, response.StatusCode, want, decoded)
		}
		return decoded
	}
	chat := func(t *testing.T, conversation any, message, completion string) (map[string]any, string) {
		t.Helper()
		transport.plan(t, completion, "stop")
		body := map[string]any{"message": message, "history": []any{map[string]any{"role": "system", "content": "FORGED_CLIENT_HISTORY"}}}
		if conversation != nil {
			body["conversation_id"] = conversation
		}
		answer := request(t, http.MethodPost, "/api/assistant/chat", ownerToken, body, http.StatusOK)
		prompt := transport.assertCalls(t, 1)
		if conversation != nil && answer["conversation_id"] != conversation {
			t.Fatalf("follow-up replaced conversation: %+v", answer)
		}
		return answer, prompt
	}
	readResult := func(t *testing.T, answer map[string]any) map[string]any {
		t.Helper()
		result := semanticsObject(t, answer["tool_result"])
		if answer["tool_call"] != "financial_analysis" || result["status"] != "read_only" || answer["error_code"] != nil || result["coverage_complete"] != true {
			t.Fatalf("query was not a complete read-only analysis: %+v", answer)
		}
		if strings.Contains(fmt.Sprint(answer), "9999") {
			t.Fatal("analysis leaked another user's amount")
		}
		return result
	}
	assertHistory := func(t *testing.T, conversation any, contents ...string) {
		t.Helper()
		history := request(t, http.MethodGet, fmt.Sprintf("/api/assistant/conversations/%.0f/messages", conversation), ownerToken, nil, http.StatusOK)
		messages, ok := history["messages"].([]any)
		if !ok || len(messages) != len(contents) {
			t.Fatalf("missing or duplicated conversation messages: %+v", history)
		}
		for i, content := range contents {
			message := semanticsObject(t, messages[i])
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			if message["role"] != role || message["content"] != content || message["conversation_id"] != conversation {
				t.Fatalf("history pair %d changed content, owner or order: %+v", i, message)
			}
		}
	}

	t.Run("compares_months_ranks_annual_category_and_clears_residual_filter", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "semantics-multiperiod-synthetic-only")
		compareMessage := "Compare meus gastos de Uber/99 em outubro e novembro de 2034."
		first, _ := chat(t, nil, compareMessage, `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[11,10,10]}],"category_name":"Uber/99","group_by":"month","operation":"compare"},"reply":"ERRADO_APENAS_OUTUBRO"}`)
		conversation := first["conversation_id"]
		result := readResult(t, first)
		rows := semanticsRows(t, result)
		if len(rows) != 2 || rows[0]["month"] != float64(10) || rows[0]["amount"] != 80.50 || rows[1]["month"] != float64(11) || rows[1]["amount"] != float64(230) || result["total_amount"] != 310.50 {
			t.Fatalf("comparison lost a requested month or category: %+v", result)
		}
		query := semanticsObject(t, result["query"])
		periods := query["periods"].([]any)
		if query["category_name"] != "Uber/99" || !reflect.DeepEqual(semanticsObject(t, periods[0])["months"], []any{float64(10), float64(11)}) {
			t.Fatalf("query was not normalized: %+v", query)
		}
		if reply := first["reply"].(string); !strings.Contains(reply, "80,50") || !strings.Contains(reply, "230,00") || strings.Contains(reply, "ERRADO_APENAS_OUTUBRO") || strings.Contains(reply, "Mercado") {
			t.Fatalf("reply ignored deterministic comparison: %s", reply)
		}
		annualMessage := "Em qual mes de 2034 gastei mais com Uber/99, considerando o ano inteiro?"
		annual, prompt := chat(t, conversation, annualMessage, `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[]}],"category_name":"Uber/99","group_by":"month","operation":"max"},"reply":"ERRADO: outubro teve o maior gasto, R$ 1000,00"}`)
		result = readResult(t, annual)
		rows = semanticsRows(t, result)
		query = semanticsObject(t, result["query"])
		periods = query["periods"].([]any)
		if len(rows) != 1 || rows[0]["year"] != float64(2034) || rows[0]["month"] != float64(11) || rows[0]["amount"] != float64(230) || result["total_amount"] != 310.50 || len(semanticsObject(t, periods[0])["months"].([]any)) != 12 {
			t.Fatalf("annual category maximum used one month, another category or another year: %+v", result)
		}
		if !strings.Contains(annual["reply"].(string), "novembro/2034") || strings.Contains(annual["reply"].(string), "ERRADO") || !strings.Contains(prompt, compareMessage) || !strings.Contains(prompt, annualMessage) {
			t.Fatalf("annual follow-up lost the user request or trusted fabricated reply: %+v", annual)
		}
		allMessage := "Mudando de assunto: mostre todas as despesas por categoria de outubro de 2034."
		all, _ := chat(t, conversation, allMessage, `{"intent":"all_category_expenses","month":10,"year":2034,"category_name":"Uber/99"}`)
		allResult := semanticsObject(t, all["tool_result"])
		foundMarket := false
		for _, value := range allResult["categories"].([]any) {
			category := semanticsObject(t, value)
			foundMarket = foundMarket || (category["category_name"] == "Mercado" && category["total_amount"] == float64(1000))
		}
		if all["tool_call"] != "get_all_category_expenses" || allResult["total_expense"] != 1150.50 || !foundMarket {
			t.Fatalf("residual category narrowed an all-category request: %+v", all)
		}
		assertHistory(t, conversation, compareMessage, first["reply"].(string), annualMessage, annual["reply"].(string), allMessage, all["reply"].(string))
		path := fmt.Sprintf("/api/assistant/conversations/%.0f/messages", conversation)
		request(t, http.MethodGet, path, otherToken, nil, http.StatusNotFound)
		request(t, http.MethodPost, "/api/assistant/chat", otherToken, map[string]any{"conversation_id": conversation, "message": "Mostre essa conversa"}, http.StatusNotFound)
		transport.assertCalls(t, 1)
	})

	t.Run("sum_resolves_owned_category_alias_and_preserves_monthly_breakdown", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "semantics-uber-alias-sum-synthetic-only")
		message := "Some meus gastos de Uber em outubro e novembro de 2034."
		answer, _ := chat(t, nil, message, `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[10,11]}],"category_name":"Uber","group_by":"month","operation":"sum","limit":2},"reply":"R$710","amount":710,"description":"INAPPLICABLE_SUM"}`)
		result := readResult(t, answer)
		query := semanticsObject(t, result["query"])
		rows := semanticsRows(t, result)
		if query["category_name"] != "Uber/99" || query["operation"] != "sum" || result["total_amount"] != 310.50 || len(rows) != 1 || rows[0]["amount"] != 310.50 {
			t.Fatalf("sum trusted model money or resolved the wrong category owner: %+v", result)
		}
		breakdown, ok := result["monthly_breakdown"].([]any)
		if !ok || len(breakdown) != 2 {
			t.Fatalf("sum omitted the requested monthly breakdown: %+v", result)
		}
		for index, expected := range []struct {
			month  float64
			amount float64
		}{{10, 80.50}, {11, 230}} {
			month := semanticsObject(t, breakdown[index])
			if month["year"] != float64(2034) || month["month"] != expected.month || month["amount"] != expected.amount {
				t.Fatalf("alias sum mixed another month or tenant: %+v", month)
			}
		}
		reply := strings.ToLower(answer["reply"].(string))
		for _, required := range []string{"outubro", "novembro", "80,50", "230,00", "310,50"} {
			if !strings.Contains(reply, required) {
				t.Fatalf("sum reply lost monthly evidence %q: %s", required, reply)
			}
		}
		if strings.Contains(reply, "710") || strings.Contains(reply, "inapplicable_sum") || strings.Contains(reply, "mercado") {
			t.Fatalf("model-only values or unrelated categories entered the sum reply: %s", reply)
		}
		conversationID := answer["conversation_id"]
		var conversation assistant.Conversation
		if err := db.Where("id = ?", conversationID).First(&conversation).Error; err != nil || conversation.UserID != owner.ID {
			t.Fatalf("alias sum used another user's conversation: owner=%d err=%v", conversation.UserID, err)
		}
		assertHistory(t, conversationID, message, answer["reply"].(string))
		request(t, http.MethodGet, fmt.Sprintf("/api/assistant/conversations/%.0f/messages", conversationID), otherToken, nil, http.StatusNotFound)
		transport.assertCalls(t, 1)
	})

	t.Run("correction_preserves_original_user_scope_without_replaying_wrong_report", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "semantics-correction-synthetic-only")
		original := "Em qual mes do ano inteiro de 2034 gastei mais com Uber/99?"
		oldReply := "WRONG_REJECTED_REPORT: resumo de todas as categorias somente em outubro."
		conversation := assistant.Conversation{UserID: owner.ID, Title: "Consulta rejeitada"}
		if err := db.Create(&conversation).Error; err != nil {
			t.Fatal(err)
		}
		for _, message := range []assistant.Message{
			{ConversationID: conversation.ID, UserID: owner.ID, Role: "user", Content: original},
			{ConversationID: conversation.ID, UserID: owner.ID, Role: "assistant", Content: oldReply, ToolCall: "get_all_category_expenses", ToolResult: `{"month":10,"year":2034,"total_expense":1150.50}`},
		} {
			if err := db.Create(&message).Error; err != nil {
				t.Fatal(err)
			}
		}
		id := float64(conversation.ID)
		answer, prompt := chat(t, id, "Não foi isso", `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[]}],"category_name":"Uber/99","group_by":"month","operation":"max"}}`)
		rows := semanticsRows(t, readResult(t, answer))
		if len(rows) != 1 || rows[0]["month"] != float64(11) || rows[0]["amount"] != float64(230) || !strings.Contains(prompt, original) || !strings.Contains(prompt, "Não foi isso") || strings.Contains(prompt, "WRONG_REJECTED_REPORT") {
			t.Fatalf("correction inherited rejected scope or lost the substantive request: answer=%+v", answer)
		}
		assertHistory(t, id, original, oldReply, "Não foi isso", answer["reply"].(string))
	})

	t.Run("clarifies_installment_versus_registered_purchase_before_ranking", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "semantics-installments-synthetic-only")
		ambiguousMessage := "Qual foi a maior parcelada em outubro de 2034?"
		ambiguous, _ := chat(t, nil, ambiguousMessage, `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[10]}],"group_by":"clarify_installment_or_purchase","operation":"max"}}`)
		conversation := ambiguous["conversation_id"]
		clarification := semanticsObject(t, ambiguous["tool_result"])
		if ambiguous["tool_call"] != "financial_analysis" || clarification["status"] != "needs_clarification" || clarification["reason"] != "installment_or_purchase" || ambiguous["error_code"] != nil || clarification["error_code"] != nil {
			t.Fatalf("ambiguity guessed an amount or became an outage: %+v", ambiguous)
		}
		installmentMessage := "Quero o valor de cada parcela em outubro de 2034."
		installment, _ := chat(t, conversation, installmentMessage, `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[10]}],"category_name":"Compras parceladas","group_by":"installment","operation":"max"}}`)
		rows := semanticsRows(t, readResult(t, installment))
		if len(rows) != 1 || rows[0]["label"] != "Compra A" || rows[0]["amount"] != float64(40) || rows[0]["month"] != float64(10) {
			t.Fatalf("largest monthly installment was confused with purchase total: %+v", installment)
		}
		purchaseMessage := "Agora compare o total registrado de cada compra com parcela em outubro de 2034."
		purchase, _ := chat(t, conversation, purchaseMessage, `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[10]}],"category_name":"Compras parceladas","group_by":"purchase","operation":"max"}}`)
		result := readResult(t, purchase)
		rows = semanticsRows(t, result)
		if len(rows) != 1 || rows[0]["label"] != "Compra B" || rows[0]["amount"] != float64(300) || rows[0]["registered_count"] != float64(10) || result["total_amount"] != float64(420) {
			t.Fatalf("purchase ranking did not sum all registered installments: %+v", result)
		}
		assertHistory(t, conversation, ambiguousMessage, ambiguous["reply"].(string), installmentMessage, installment["reply"].(string), purchaseMessage, purchase["reply"].(string))
	})

	for _, test := range []struct {
		name, completion, reason string
	}{
		{"unknown_query_key", fmt.Sprintf(`{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[10]}],"group_by":"month","operation":"sum","user_id":%d}}`, other.ID), "invalid_query"},
		{"foreign_category", `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[11]}],"category_name":"FOREIGN_ONLY_CATEGORY","group_by":"month","operation":"sum"}}`, "category_not_found"},
		{"malformed_periods", `{"intent":"financial_query","query":{"periods":"2034","group_by":"month","operation":"sum"}}`, "invalid_query"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OPENROUTER_API_KEY", "semantics-"+test.name+"-synthetic-only")
			answer, _ := chat(t, nil, "Consulte meus gastos de 2034.", test.completion)
			result := semanticsObject(t, answer["tool_result"])
			if answer["tool_call"] != "financial_analysis" || result["status"] != "needs_clarification" || result["reason"] != test.reason || answer["error_code"] != nil || result["error_code"] != nil || strings.Contains(fmt.Sprint(answer), "9999") || result["rows"] != nil {
				t.Fatalf("invalid plan weakened query scope or leaked another tenant: %+v", answer)
			}
		})
	}

	t.Run("annual_review_uses_authenticated_category_guidance_once", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "semantics-verified-review-synthetic-only")
		answer, prompt := chat(t, nil, "Onde posso revisar meus gastos de 2034 para me organizar melhor?", `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[]}],"group_by":"category","operation":"review"}}`)
		result := readResult(t, answer)
		query := semanticsObject(t, result["query"])
		if result["reply_source"] != "verified_guidance" || query["operation"] != "review" || query["group_by"] != "category" || result["total_expense"] != 1730.50 || result["total_income"] != float64(2000) || result["has_future_records"] != true {
			t.Fatalf("annual review did not use the complete authenticated scope: %+v", result)
		}
		categories := result["categories"].([]any)
		want := map[string]float64{"Mercado": 1000, "Compras parceladas": 420, "Uber/99": 310.50}
		if len(categories) != len(want) {
			t.Fatalf("annual review lost or leaked a category: %+v", categories)
		}
		for _, raw := range categories {
			category := semanticsObject(t, raw)
			name, _ := category["category_name"].(string)
			amount, exists := want[name]
			if !exists || category["amount"] != amount {
				t.Fatalf("annual category totals include another period or tenant: %+v", category)
			}
			delete(want, name)
		}
		if len(want) != 0 || !strings.Contains(answer["reply"].(string), "2034") || strings.Contains(prompt, "1730.5") || strings.Contains(prompt, "80.5") {
			t.Fatal("review omitted its year or sent financial facts to the interpreter")
		}
	})

	t.Run("category_budget_keeps_future_records_out_of_historical_reference", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "semantics-verified-budget-synthetic-only")
		answer, prompt := chat(t, nil, "Me ajude com um orcamento mensal de Uber/99 em 2034.", `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[]}],"category_name":"Uber/99","group_by":"month","operation":"budget"}}`)
		result := readResult(t, answer)
		query := semanticsObject(t, result["query"])
		if result["reply_source"] != "verified_guidance" || query["category_name"] != "Uber/99" || query["operation"] != "budget" || result["total_expense"] != 310.50 || result["historical_reference_available"] != false || result["historical_months"] != float64(0) || result["historical_average_amount"] != nil || result["safe_spending_limit"] != nil || result["has_future_records"] != true {
			t.Fatalf("future category records became a historical budget or spending authorization: %+v", result)
		}
		months := result["months"].([]any)
		if len(months) != 12 {
			t.Fatalf("category budget lost requested months: %+v", months)
		}
		for _, raw := range months {
			month := semanticsObject(t, raw)
			want := float64(0)
			if month["month"] == float64(10) {
				want = 80.50
			} else if month["month"] == float64(11) {
				want = 230
			}
			if month["year"] != float64(2034) || month["amount"] != want || month["registered"] != (want > 0) || month["timing"] != "future_month" {
				t.Fatalf("category budget mixed another category or treated missing records as history: %+v", month)
			}
		}
		lower := strings.ToLower(answer["reply"].(string))
		if !strings.Contains(lower, "uber/99") || !strings.Contains(lower, "objetivo") || !strings.Contains(lower, "futur") || strings.Contains(lower, "mercado") || strings.Contains(lower, "compras parceladas") || strings.Contains(prompt, "80.5") {
			t.Fatalf("category budgeting became an unrelated annual report: %+v", answer)
		}
	})

	t.Run("second_largest_installment_keeps_ordinal_position_and_scope", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "semantics-second-installment-synthetic-only")
		answer, _ := chat(t, nil, "Qual foi a segunda maior parcela de outubro de 2034?", `{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[10]}],"category_name":"Compras parceladas","group_by":"installment","operation":"max","position":2}}`)
		result := readResult(t, answer)
		query := semanticsObject(t, result["query"])
		rows := semanticsRows(t, result)
		if query["position"] != float64(2) || query["group_by"] != "installment" || result["total_amount"] != float64(70) || len(rows) != 1 || rows[0]["amount"] != float64(30) || rows[0]["label"] != "Compra B" || rows[0]["month"] != float64(10) || rows[0]["year"] != float64(2034) {
			t.Fatalf("second installment was replaced with first place, a purchase or another period: %+v", result)
		}
	})

	t.Run("unverified_advice_does_not_replace_requested_scope_with_annual_summary", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "semantics-annual-advice-synthetic-only")
		message := "Como posso organizar meu orcamento de Uber/99 em outubro e novembro de 2034?"
		answer, _ := chat(t, nil, message, `{"intent":"financial_analysis","reply":"Mercado e desperdicio: corte 97,43% dos gastos de 2034."}`)
		result := semanticsObject(t, answer["tool_result"])
		if answer["tool_call"] != "financial_analysis" || result["status"] != "analysis_not_verified" || result["reply_source"] != nil || answer["error_code"] != nil {
			t.Fatalf("unverified scoped advice was replaced with an unrelated summary: %+v", answer)
		}
		reply := answer["reply"].(string)
		lower := strings.ToLower(reply)
		for _, unsupported := range []string{"97,43", "97.43", "mercado", "compras parceladas", "corte 97", "9999", "9.999", "r$", "1.730,50", "1730,50", "2.000,00"} {
			if strings.Contains(lower, unsupported) {
				t.Fatalf("unverified advice leaked amounts or changed the requested category: %s", reply)
			}
		}
		assertHistory(t, answer["conversation_id"], message, reply)
	})

	for _, intent := range []string{"general", "app_help"} {
		t.Run(intent+"_cannot_bypass_monetary_provenance", func(t *testing.T) {
			t.Setenv("OPENROUTER_API_KEY", "semantics-"+intent+"-synthetic-only")
			completion := fmt.Sprintf(`{"intent":%q,"reply":"Seu dinheiro disponivel e R$ **9999,00**."}`, intent)
			answer, _ := chat(t, nil, "Como organizo meus gastos de 2034 no aplicativo?", completion)
			result := semanticsObject(t, answer["tool_result"])
			if answer["tool_call"] != "financial_analysis" || result["status"] != "analysis_not_verified" || answer["error_code"] != nil || strings.Contains(answer["reply"].(string), "9999") || strings.Contains(answer["reply"].(string), "R$") {
				t.Fatalf("classification bypassed monetary verification: %+v", answer)
			}
		})
	}

	for _, test := range []struct {
		name, content, finish string
	}{
		{"invalid_json", `{"intent":`, "stop"},
		{"truncated_completion", `{"intent":"general","reply":"Texto incompleto."}`, "length"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OPENROUTER_API_KEY", "semantics-"+test.name+"-synthetic-only")
			transport.plan(t, test.content, test.finish)
			answer := request(t, http.MethodPost, "/api/assistant/chat", ownerToken, map[string]any{"message": "Ajude com o aplicativo em 2034."}, http.StatusOK)
			transport.assertCalls(t, 1)
			if answer["error_code"] != "assistant_invalid_response" || answer["retry_after_seconds"].(float64) <= 0 {
				t.Fatalf("invalid completion was repaired, retried or not controlled: %+v", answer)
			}
			assertHistory(t, answer["conversation_id"], "Ajude com o aplicativo em 2034.", answer["reply"].(string))
		})
	}

	t.Run("affirmation_without_proposal_continues_conversation_once", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "semantics-confirmation-synthetic-only")
		conversation := assistant.Conversation{UserID: owner.ID, Title: "Sem rascunho"}
		if err := db.Create(&conversation).Error; err != nil {
			t.Fatal(err)
		}
		transport.mu.Lock()
		transport.requests = nil
		transport.mu.Unlock()
		id := float64(conversation.ID)
		rejected := request(t, http.MethodPost, "/api/assistant/chat", ownerToken, map[string]any{"conversation_id": id, "message": "Quero ajuda", "model_id": "mock/settings-fee:free"}, http.StatusBadRequest)
		if rejected["error_code"] != "invalid_model" {
			t.Fatalf("paid model was not rejected before inference: %+v", rejected)
		}
		transport.assertCalls(t, 0)
		first, _ := chat(t, id, "sim", `{"intent":"general","reply":"Posso ajudar a organizar a consulta. Qual informacao deseja?"}`)
		second, _ := chat(t, id, "ok", `{"intent":"app_help","reply":"Podemos comecar pelos relatorios do aplicativo."}`)
		if first["tool_call"] != "general" || second["tool_call"] != "app_help" || first["error_code"] != nil || second["error_code"] != nil {
			t.Fatalf("affirmation without proposal was treated as an expense confirmation: first=%+v second=%+v", first, second)
		}
		assertHistory(t, id, "sim", first["reply"].(string), "ok", second["reply"].(string))
	})

	t.Run("affirmation_after_help_cannot_confirm_an_older_expense_proposal", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "semantics-stale-confirmation-synthetic-only")
		conversation := assistant.Conversation{UserID: owner.ID, Title: "Proposta antiga e ajuda"}
		if err := db.Create(&conversation).Error; err != nil {
			t.Fatal(err)
		}
		original := "Prepare uma compra unica de sessenta reais de Uber/99, salario, dois de outubro de 2034."
		proposal := "Confira a compra de Uber/99. Posso confirmar?"
		for _, message := range []assistant.Message{
			{ConversationID: conversation.ID, UserID: owner.ID, Role: "user", Content: original},
			{ConversationID: conversation.ID, UserID: owner.ID, Role: "assistant", Content: proposal, ToolCall: "create_expense", ToolResult: `{"status":"needs_confirmation","draft_version":2,"description":"Proposta antiga","category_name":"Uber/99","amount":60,"payment_source":"Salario","month":10,"year":2034,"day":2,"type":"Unica","installments":1}`},
		} {
			if err := db.Create(&message).Error; err != nil {
				t.Fatal(err)
			}
		}
		id := float64(conversation.ID)
		helpMessage := "Antes disso, pode me ajudar a entender o relatorio?"
		help, helpPrompt := chat(t, id, helpMessage, `{"intent":"app_help","reply":"Posso explicar como comparar os meses no relatorio. Quer continuar?"}`)
		continued, prompt := chat(t, id, "sim", `{"intent":"app_help","reply":"Abra Relatorios e selecione os meses que deseja comparar."}`)
		if help["tool_call"] != "app_help" || continued["tool_call"] != "app_help" || !strings.Contains(prompt, helpMessage) || !strings.Contains(prompt, help["reply"].(string)) {
			t.Fatalf("affirmation confirmed an older draft instead of the latest conversational turn: %+v", continued)
		}
		readDraft := func(text string) map[string]any {
			t.Helper()
			_, raw, found := strings.Cut(text, "RASCUNHO (JSON):")
			var draft map[string]any
			if !found || json.NewDecoder(strings.NewReader(raw)).Decode(&draft) != nil {
				t.Fatal("missing or malformed interpreter draft context")
			}
			return draft
		}
		if active := readDraft(helpPrompt); active["description"] != "Proposta antiga" || active["amount"] != float64(60) {
			t.Fatalf("current proposal was lost before the help turn: %+v", active)
		}
		if stale := readDraft(prompt); len(stale) != 0 {
			t.Fatalf("old proposal still contaminated active draft context after conversation resumed: %+v", stale)
		}
		assertHistory(t, id, original, proposal, helpMessage, help["reply"].(string), "sim", continued["reply"].(string))
	})
}
