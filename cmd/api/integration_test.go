//go:build integration

package main

import (
	"Sobra_Ai_Back-end/internal/auth"
	"Sobra_Ai_Back-end/internal/database"
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
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

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
	request("DELETE", catPath, owner, "", 200)
	if err := db.Model(&auth.User{}).Where("email = ?", "owner@example.test").Update("access_blocked", true).Error; err != nil {
		t.Fatal(err)
	}
	request("GET", "/api/incomes/", owner, "", 403)
}
