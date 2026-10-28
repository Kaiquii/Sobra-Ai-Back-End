//go:build integration

package main

import (
	"Sobra_Ai_Back-end/internal/assistant"
	"Sobra_Ai_Back-end/internal/auth"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/migrations"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Intercept only the provider call; local API requests still use real TCP.
// No integration scenario may contact an external LLM.
type integrationLLMTransport struct {
	base      http.RoundTripper
	mu        sync.Mutex
	responses []string
	prompts   []string
}

func (transport *integrationLLMTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Hostname() == "127.0.0.1" {
		return transport.base.RoundTrip(req)
	}
	if req.URL.Hostname() != "openrouter.ai" {
		return nil, fmt.Errorf("external network forbidden in integration test")
	}
	if req.Method == http.MethodGet && req.URL.Path == "/api/v1/models" {
		body, err := integrationModelCatalogJSON()
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	}
	if req.Method != http.MethodPost || req.URL.Path != "/api/v1/chat/completions" {
		return nil, fmt.Errorf("unexpected provider endpoint")
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.prompts = append(transport.prompts, string(body))
	if len(transport.responses) == 0 {
		return nil, fmt.Errorf("unexpected provider call")
	}
	content := transport.responses[0]
	transport.responses = transport.responses[1:]
	encoded, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(encoded))), Request: req}, nil
}

// All integration transports expose the same public catalog. Production caches
// the public catalog independently from individual users and credentials.
func integrationModelCatalogJSON() ([]byte, error) {
	rows := make([]any, 0, 7)
	for _, id := range []string{"mock/api:free", "mock/settings-a:free", "mock/settings-b:free", "mock/extra-conversation:free", "mock/promo-free", "mock/settings-fee:free", "mock/not-chat:free", "openrouter/auto"} {
		price := "0"
		output := []string{"text"}
		parameters := []string{"response_format", "max_tokens", "temperature"}
		if id == "mock/settings-fee:free" {
			price = "0.01"
		}
		if id == "mock/not-chat:free" {
			output = []string{"embedding"}
		}
		if id == "mock/extra-conversation:free" {
			parameters = []string{"max_tokens", "temperature"}
		}
		rows = append(rows, map[string]any{
			"id": id, "name": id, "context_length": 32768,
			"architecture":         map[string]any{"input_modalities": []string{"text"}, "output_modalities": output},
			"pricing":              map[string]string{"prompt": "0", "completion": "0", "request": price},
			"supported_parameters": parameters,
		})
	}
	return json.Marshal(map[string]any{"data": rows})
}

// Never uses DB_DSN, .env, Docker's selected remote context or production volumes.
func disposablePostgres(t *testing.T) *gorm.DB {
	t.Helper()
	host := "unix:///var/run/docker.sock"
	if runtime.GOOS == "windows" {
		host = "npipe:////./pipe/dockerDesktopLinuxEngine"
	}
	docker := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", append([]string{"--host", host}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	name := fmt.Sprintf("sobraai-harness-%d", time.Now().UnixNano())
	out, err := docker("run", "--detach", "--rm", "--name", name, "--label", "sobraai.harness=true", "--tmpfs", "/var/lib/postgresql/data", "-p", "127.0.0.1::5432", "-e", "POSTGRES_PASSWORD=harness-only", "-e", "POSTGRES_DB=harness", "postgres:15-alpine")
	if err != nil {
		t.Fatalf("local Docker required (no skip): %v: %s", err, out)
	}
	t.Cleanup(func() {
		if out, err := docker("rm", "-f", name); err != nil {
			t.Errorf("cleanup container %s: %v: %s", name, err, out)
		}
	})
	address, err := docker("port", name, "5432/tcp")
	if err != nil || !strings.HasPrefix(address, "127.0.0.1:") {
		t.Fatalf("unexpected container address: %q %v", address, err)
	}
	dsn := fmt.Sprintf("host=127.0.0.1 port=%s user=postgres password=harness-only dbname=harness sslmode=disable connect_timeout=2", strings.TrimPrefix(address, "127.0.0.1:"))
	var db *gorm.DB
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); {
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestIntegrationAPI(t *testing.T) {
	db := disposablePostgres(t)
	if err := migrations.Validate(db); err == nil {
		t.Fatal("empty database must not validate")
	}
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Validate(db); err != nil {
		t.Fatal(err)
	}
	before, err := migrations.Status(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	after, err := migrations.Status(db)
	if err != nil || len(before) != len(after) || len(after) == 0 {
		t.Fatalf("migration idempotency: %v", err)
	}
	for _, entry := range after {
		if !entry.Applied {
			t.Fatalf("pending migration: %+v", entry)
		}
	}
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	t.Setenv("JWT_SECRET", "harness-only-signing-key")
	t.Setenv("UPLOADS_DIR", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "harness-only")
	previousTransport := http.DefaultTransport
	llmTransport := &integrationLLMTransport{base: previousTransport, responses: []string{
		`{"intent":"create_expense","description":"Compra IA","amount":600,"type":"Parcelada","installments":3}`,
		`{"intent":"create_expense","category_name":"Updated","payment_source":"Salario"}`,
		`{"intent":"create_expense","day":30,"month":9,"year":2026}`,
		`{"intent":"cancel_expense"}`,
	}}
	http.DefaultTransport = llmTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	gin.SetMode(gin.TestMode)
	password, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"owner@example.test", "other@example.test"} {
		if err := db.Create(&auth.User{Name: "Harness", Email: email, Password: string(password), Role: "user"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(newRouter())
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 5 * time.Second}
	request := func(method, path, token, body string, want int) map[string]any {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, res.StatusCode, want, data)
		}
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	login := func(email string) string {
		r := request("POST", "/api/auth/login", "", fmt.Sprintf(`{"email":%q,"password":"test-password"}`, email), 200)
		token, ok := r["token"].(string)
		if !ok || token == "" {
			t.Fatal("missing login token")
		}
		return token
	}
	request("GET", "/api/incomes/", "", "", 401)
	request("POST", "/api/auth/login", "", `{"email":"owner@example.test","password":"wrong"}`, 401)
	owner, other := login("owner@example.test"), login("other@example.test")
	request("POST", "/api/incomes/", owner, `{"source":"Salario","amount":-1,"month":9,"year":2026}`, 400)
	request("POST", "/api/incomes/", owner, `{"source":"Salario","amount":1000,"month":9,"year":2026}`, 201)
	list := request("GET", "/api/incomes/?month=9&year=2026", owner, "", 200)
	if list["total"] != float64(1) {
		t.Fatalf("income not persisted: %v", list)
	}
	rows := list["incomes"].([]any)
	id := int(rows[0].(map[string]any)["id"].(float64))
	path := fmt.Sprintf("/api/incomes/%d", id)
	if r := request("GET", "/api/incomes/", other, "", 200); r["total"] != float64(0) {
		t.Fatal("cross-user data leak")
	}
	request("PATCH", path, other, `{"amount":2000}`, 404)
	request("DELETE", path, other, "", 404)
	request("PATCH", path, owner, `{"amount":1500}`, 200)
	list = request("GET", "/api/incomes/", owner, "", 200)
	if list["incomes"].([]any)[0].(map[string]any)["amount"] != float64(1500) {
		t.Fatal("update not persisted")
	}
	request("DELETE", path, owner, "", 200)
	if r := request("GET", "/api/incomes/", owner, "", 200); r["total"] != float64(0) {
		t.Fatal("delete not persisted")
	}
	category := request("POST", "/api/categories/", owner, `{"name":"Harness"}`, 201)
	catID := int(category["data"].(map[string]any)["id"].(float64))
	catPath := fmt.Sprintf("/api/categories/%d", catID)
	request("PATCH", catPath, other, `{"name":"Forbidden"}`, 404)
	request("PATCH", catPath, owner, `{"name":"Updated"}`, 200)
	cats := request("GET", "/api/categories/", owner, "", 200)
	if cats["categories"].([]any)[0].(map[string]any)["name"] != "Updated" {
		t.Fatal("category update not persisted")
	}
	request("POST", "/api/expenses/", owner, fmt.Sprintf(`{"description":"Harness expense","amount":100,"category_id":%d,"date":"2026-09-20T12:00:00Z","type":"Unica","payment_source":"Salario"}`, catID), 201)
	expenseList := request("GET", "/api/expenses/?month=9&year=2026", owner, "", 200)
	if expenseList["total"] != float64(1) {
		t.Fatalf("expense not persisted: %v", expenseList)
	}
	expenseID := int(expenseList["expenses"].([]any)[0].(map[string]any)["id"].(float64))
	expensePath := fmt.Sprintf("/api/expenses/%d", expenseID)
	request("GET", expensePath, other, "", 404)
	request("DELETE", catPath, owner, "", 400)
	summary := request("GET", "/api/reports/summary?month=9&year=2026", owner, "", 200)
	if summary["total_expense"] != float64(100) {
		t.Fatalf("incorrect report: %v", summary)
	}
	request("PATCH", expensePath+"/payment-status", owner, `{"is_paid":true}`, 200)
	request("PATCH", expensePath+"/advance-status", owner, `{"is_advanced":true,"advanced_at":"2026-08-20"}`, 200)
	summary = request("GET", "/api/reports/summary?month=8&year=2026", owner, "", 200)
	if summary["total_expense"] != float64(100) {
		t.Fatalf("advance not reflected: %v", summary)
	}
	summary = request("GET", "/api/reports/summary?month=9&year=2026", owner, "", 200)
	if summary["total_expense"] != float64(0) {
		t.Fatalf("advance counted twice: %v", summary)
	}
	request("DELETE", expensePath, owner, "", 200)

	// Seed persisted proposals, exercising confirmation without any LLM/network
	// dependency. The server must revalidate even a malformed saved proposal.
	var ownerUser auth.User
	if err := db.Where("email = ?", "owner@example.test").First(&ownerUser).Error; err != nil {
		t.Fatal(err)
	}
	seedDraft := func(overrides map[string]any) uint {
		t.Helper()
		conversation := assistant.Conversation{UserID: ownerUser.ID, Title: "Cadastro de teste"}
		if err := db.Create(&conversation).Error; err != nil {
			t.Fatal(err)
		}
		draft := map[string]any{"status": "needs_confirmation", "draft_version": 2, "description": "Compra IA", "category_name": "Updated", "amount": 600, "payment_source": "Salario", "month": 9, "year": 2026, "day": 30, "type": "Parcelada", "installments": 3}
		for key, value := range overrides {
			draft[key] = value
		}
		encoded, err := json.Marshal(draft)
		if err != nil {
			t.Fatal(err)
		}
		message := assistant.Message{ConversationID: conversation.ID, UserID: ownerUser.ID, Role: "assistant", ToolCall: "create_expense", ToolResult: string(encoded)}
		if err := db.Create(&message).Error; err != nil {
			t.Fatal(err)
		}
		return conversation.ID
	}
	confirmDraft := func(conversationID uint, token string, status int) map[string]any {
		return request("POST", "/api/assistant/chat", token, fmt.Sprintf(`{"conversation_id":%d,"message":"sim"}`, conversationID), status)
	}
	countExpenses := func(description string) int64 {
		t.Helper()
		var count int64
		if err := db.Model(&expenses.Expense{}).Where("user_id = ? AND description = ?", ownerUser.ID, description).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	for _, invalid := range []map[string]any{
		{"description": nil}, {"description": "  "}, {"category_name": nil},
		{"category_name": "Nao existe"}, {"payment_source": ""}, {"amount": -1},
		{"day": 31}, {"draft_version": 1},
	} {
		r := confirmDraft(seedDraft(invalid), owner, 200)
		if r["tool_result"].(map[string]any)["status"] != "needs_information" {
			t.Fatalf("invalid proposal accepted: %v => %v", invalid, r)
		}
	}
	if countExpenses("Compra IA") != 0 {
		t.Fatal("incomplete proposals wrote expenses")
	}
	foreignCategory := request("POST", "/api/categories/", other, `{"name":"Categoria privada"}`, 201)
	foreignCatID := int(foreignCategory["data"].(map[string]any)["id"].(float64))
	r := confirmDraft(seedDraft(map[string]any{"category_name": "Categoria privada"}), owner, 200)
	if r["tool_result"].(map[string]any)["status"] != "needs_information" {
		t.Fatal("foreign category accepted")
	}
	request("POST", "/api/expenses/", owner, fmt.Sprintf(`{"description":"Foreign","amount":10,"category_id":%d,"date":"2026-09-20T12:00:00Z","type":"Unica","payment_source":"Salario"}`, foreignCatID), 400)
	first := request("POST", "/api/assistant/chat", owner, `{"message":"Comprei algo de 600 reais e parcelei em 3 vezes"}`, 200)
	conversationID := uint(first["conversation_id"].(float64))
	if first["tool_result"].(map[string]any)["status"] != "needs_information" || countExpenses("Compra IA") != 0 {
		t.Fatalf("incomplete purchase persisted: %v", first)
	}
	second := request("POST", "/api/assistant/chat", owner, fmt.Sprintf(`{"conversation_id":%d,"message":"Categoria Updated, pago com Salario"}`, conversationID), 200)
	if second["tool_result"].(map[string]any)["status"] != "needs_information" || countExpenses("Compra IA") != 0 {
		t.Fatalf("date was invented: %v", second)
	}
	third := request("POST", "/api/assistant/chat", owner, fmt.Sprintf(`{"conversation_id":%d,"message":"30 de setembro de 2026"}`, conversationID), 200)
	if third["tool_result"].(map[string]any)["status"] != "needs_confirmation" || countExpenses("Compra IA") != 0 {
		t.Fatalf("complete draft was not preserved or wrote without confirmation: %v", third)
	}
	confirmDraft(conversationID, other, 404)
	r = confirmDraft(conversationID, owner, 200)
	if r["tool_result"].(map[string]any)["status"] != "created" || countExpenses("Compra IA") != 3 {
		t.Fatalf("complete proposal failed: %v", r)
	}
	confirmDraft(conversationID, owner, 200)
	if countExpenses("Compra IA") != 3 {
		t.Fatal("confirmation replay duplicated expenses")
	}
	for _, mode := range []string{"scheduled", "effective"} {
		list := request("GET", "/api/expenses/?month=9&year=2026&period_mode="+mode, owner, "", 200)
		items := list["expenses"].([]any)
		if len(items) != 1 {
			t.Fatalf("unexpected period: %v", list)
		}
		item := items[0].(map[string]any)
		if item["description"] != "Compra IA" || item["category_id"] != float64(catID) || item["amount"] != float64(200) || item["type"] != "Parcelada" {
			t.Fatalf("invalid expense JSON: %v", item)
		}
		if item["payment_splits"].([]any)[0].(map[string]any)["amount"] != float64(200) {
			t.Fatal("payment splits diverge from installment")
		}
		path := fmt.Sprintf("/api/expenses/%.0f", item["id"])
		request("PATCH", path, owner, `{"description":null}`, 400)
		request("PATCH", path, owner, `{"description":" "}`, 400)
		request("PATCH", path, owner, `{"category_id":0}`, 400)
		request("PATCH", path, owner, fmt.Sprintf(`{"category_id":%d}`, foreignCatID), 400)
	}
	summary = request("GET", "/api/reports/summary?month=9&year=2026", owner, "", 200)
	if summary["total_expense"] != float64(200) {
		t.Fatalf("purchase total was repeated instead of divided: %v", summary)
	}
	// Force failure on the second installment and verify all writes roll back.
	if err := db.Exec(`ALTER TABLE expenses ADD CONSTRAINT harness_fail_second CHECK (description <> 'Rollback' OR current_install < 2)`).Error; err != nil {
		t.Fatal(err)
	}
	confirmDraft(seedDraft(map[string]any{"description": "Rollback"}), owner, 500)
	if countExpenses("Rollback") != 0 {
		t.Fatal("partial installment series survived rollback")
	}
	if err := db.Exec(`ALTER TABLE expenses DROP CONSTRAINT harness_fail_second`).Error; err != nil {
		t.Fatal(err)
	}
	cancelID := seedDraft(map[string]any{"description": "Cancelled"})
	request("POST", "/api/assistant/chat", owner, fmt.Sprintf(`{"conversation_id":%d,"message":"nao pode cadastrar"}`, cancelID), 200)
	confirmDraft(cancelID, owner, 200)
	if countExpenses("Cancelled") != 0 {
		t.Fatal("negative confirmation or cancelled draft wrote expenses")
	}
	concurrentID := seedDraft(map[string]any{"description": "Concurrent"})
	var confirmations sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		confirmations.Add(1)
		go func() {
			defer confirmations.Done()
			req, err := http.NewRequest("POST", server.URL+"/api/assistant/chat", strings.NewReader(fmt.Sprintf(`{"conversation_id":%d,"message":"sim"}`, concurrentID)))
			if err != nil {
				errors <- err
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+owner)
			response, err := client.Do(req)
			if err != nil {
				errors <- err
				return
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				errors <- fmt.Errorf("concurrent confirmation status=%d", response.StatusCode)
			}
		}()
	}
	confirmations.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if countExpenses("Concurrent") != 3 {
		t.Fatal("concurrent confirmations duplicated the purchase")
	}
	centsID := seedDraft(map[string]any{"description": "Cents", "amount": 100, "month": 1, "year": 2027, "day": 31})
	confirmDraft(centsID, owner, 200)
	var centsItems []expenses.Expense
	if err := db.Preload("PaymentSplits").Where("description = ? AND user_id = ?", "Cents", ownerUser.ID).Order("current_install").Find(&centsItems).Error; err != nil {
		t.Fatal(err)
	}
	if len(centsItems) != 3 {
		t.Fatalf("wrong installment count: %d", len(centsItems))
	}
	for i, item := range centsItems {
		if item.Month != i+1 || item.Date.Day() != []int{31, 28, 31}[i] || item.Amount != []float64{33.34, 33.33, 33.33}[i] || len(item.PaymentSplits) != 1 || item.PaymentSplits[0].Amount != item.Amount {
			t.Fatalf("incorrect monthly allocation: %+v", item)
		}
	}
	legacy := expenses.Expense{UserID: ownerUser.ID, Description: "Legacy", Amount: 1, Month: 12, Year: 2028, Type: "Única", PaymentSource: "Salario", Date: time.Date(2028, 12, 1, 12, 0, 0, 0, time.UTC)}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE expenses SET description = NULL, category_id = NULL WHERE id = ?", legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"scheduled", "effective"} {
		list := request("GET", "/api/expenses/?month=12&year=2028&period_mode="+mode, owner, "", 200)
		item := list["expenses"].([]any)[0].(map[string]any)
		if item["description"] != "" || item["category_id"] != float64(0) {
			t.Fatalf("legacy fields violated JSON types: %v", item)
		}
	}
	for _, source := range []string{"VA", "VR"} {
		proposal := seedDraft(map[string]any{"description": "Benefit assistant " + source, "payment_source": source, "type": "Unica", "installments": 1, "year": 2035})
		result := confirmDraft(proposal, owner, 200)
		if result["tool_result"].(map[string]any)["status"] != "created" || countExpenses("Benefit assistant "+source) != 1 {
			t.Fatalf("assistant rejected benefit %s: %v", source, result)
		}
	}
	testBenefitsLifecycle(t, db, request, owner, other, catID)
	testPatrimonyLifecycle(t, db, request, owner, other)
	testInvestmentCategories(t, db, request, owner, other)
	// Remove the generated series via public endpoints before the existing
	// category deletion checks. No production database is used by this test.
	all := request("GET", "/api/expenses/", owner, "", 200)
	for _, item := range all["expenses"].([]any) {
		request("DELETE", fmt.Sprintf("/api/expenses/%.0f", item.(map[string]any)["id"]), owner, "", 200)
	}
	request("DELETE", catPath, owner, "", 200)
	if err := db.Model(&auth.User{}).Where("email = ?", "owner@example.test").Update("access_blocked", true).Error; err != nil {
		t.Fatal(err)
	}
	request("GET", "/api/incomes/", owner, "", 403)
	request("GET", "/api/patrimony/", owner, "", 403)
	request("GET", "/api/patrimony/investment-categories", owner, "", 403)
}
