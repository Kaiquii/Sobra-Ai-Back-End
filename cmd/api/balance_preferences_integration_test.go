//go:build integration

package main

import (
	"Sobra_Ai_Back-end/internal/auth"
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"Sobra_Ai_Back-end/internal/migrations"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type balancePreferencesIntegrationTransport struct {
	base     http.RoundTripper
	external atomic.Int32
}

func (transport *balancePreferencesIntegrationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Hostname() != "127.0.0.1" {
		transport.external.Add(1)
		return nil, fmt.Errorf("external request forbidden in balance-preference integration")
	}
	return transport.base.RoundTrip(request)
}

func TestIntegrationBalancePreferences(t *testing.T) {
	db := disposablePostgres(t)
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	owner := auth.User{Name: "Existing balance owner", Email: "balance-owner@example.test", Password: "unused"}
	other := auth.User{Name: "Balance other", Email: "balance-other@example.test", Password: "unused"}
	defaultRowUser := auth.User{Name: "SQL defaults", Email: "balance-defaults@example.test", Password: "unused"}
	for _, user := range []*auth.User{&owner, &other, &defaultRowUser} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Replay the actual additive upgrade on a disposable schema 9 containing
	// existing users. Production migration history is never changed by this test.
	if err := db.Exec("DROP TABLE user_balance_preferences; DELETE FROM schema_migrations WHERE version = 10").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrations.Validate(db); !errors.Is(err, migrations.ErrPendingMigrations) {
		t.Fatalf("schema 9 did not require the new preference migration: %v", err)
	}
	for range 2 {
		if err := migrations.Up(db); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrations.Validate(db); err != nil {
		t.Fatal(err)
	}
	var registered int64
	if err := db.Table("schema_migrations").Where("version = ?", 10).Count(&registered).Error; err != nil || registered != 1 {
		t.Fatalf("upgrade was not registered exactly once: count=%d err=%v", registered, err)
	}
	if err := db.Exec("INSERT INTO user_balance_preferences (user_id) VALUES (?)", defaultRowUser.ID).Error; err != nil {
		t.Fatal(err)
	}
	var sqlDefaults struct {
		ExcludeVAFromAvailable bool
		ExcludeVRFromAvailable bool
	}
	if err := db.Table("user_balance_preferences").Where("user_id = ?", defaultRowUser.ID).Take(&sqlDefaults).Error; err != nil || sqlDefaults.ExcludeVAFromAvailable || sqlDefaults.ExcludeVRFromAvailable {
		t.Fatalf("database defaults must be independently false: %+v %v", sqlDefaults, err)
	}
	if err := db.Delete(&defaultRowUser).Error; err != nil {
		t.Fatal(err)
	}
	var orphanRows int64
	if err := db.Table("user_balance_preferences").Where("user_id = ?", defaultRowUser.ID).Count(&orphanRows).Error; err != nil || orphanRows != 0 {
		t.Fatalf("deleting user left orphan preference: count=%d err=%v", orphanRows, err)
	}

	previousDB, previousTransport := database.DB, http.DefaultTransport
	database.DB = db
	transport := &balancePreferencesIntegrationTransport{base: previousTransport}
	http.DefaultTransport = transport
	t.Cleanup(func() {
		database.DB, http.DefaultTransport = previousDB, previousTransport
		if transport.external.Load() != 0 {
			t.Errorf("balance-preference scenarios attempted %d external requests", transport.external.Load())
		}
	})
	t.Setenv("JWT_SECRET", "balance-preferences-harness-only")
	t.Setenv("OPENROUTER_API_KEY", "harness-no-real-inference")
	tokenFor := func(user auth.User) string {
		t.Helper()
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": user.ID, "role": "user", "exp": time.Now().Add(time.Hour).Unix(),
		}).SignedString([]byte("balance-preferences-harness-only"))
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	ownerToken, otherToken := tokenFor(owner), tokenFor(other)
	server := httptest.NewServer(newRouter())
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 5 * time.Second}
	rawRequest := func(method, path, token, body string) ([]byte, int, error) {
		request, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			return nil, 0, err
		}
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, 0, err
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		return data, response.StatusCode, err
	}
	request := func(method, path, token, body string, want int) map[string]any {
		t.Helper()
		data, status, err := rawRequest(method, path, token, body)
		if err != nil || status != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s err=%v", method, path, status, want, data, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	const preferencesPath = "/api/users/balance-preferences"
	assertPreferences := func(got map[string]any, va, vr bool) {
		t.Helper()
		if len(got) != 2 || got["exclude_va_from_available"] != va || got["exclude_vr_from_available"] != vr {
			t.Fatalf("wrong independent preferences: got=%v want va=%t vr=%t", got, va, vr)
		}
	}
	request("GET", preferencesPath, "", "", http.StatusUnauthorized)
	request("PATCH", preferencesPath, "", `{"exclude_va_from_available":true}`, http.StatusUnauthorized)
	assertPreferences(request("GET", preferencesPath, ownerToken, "", 200), false, false)
	var count int64
	if err := db.Table("user_balance_preferences").Where("user_id = ?", owner.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("reading missing preference must not write a row: count=%d err=%v", count, err)
	}
	assertPreferences(request("GET", preferencesPath, otherToken, "", 200), false, false)
	for _, body := range []string{
		`null`, `[]`, ``, `{"exclude_va_from_available":null}`, `{"exclude_vr_from_available":null}`,
		`{"exclude_va_from_available":"true"}`, `{"exclude_vr_from_available":1}`,
		`{"exclude_va_from_available":true,"exclude_vr_from_available":null}`,
		`{"user_id":1,"exclude_va_from_available":true}`, `{"unknown":false}`, `{} {}`,
		`{"exclude_va_from_available":true,"exclude_va_from_available":false}`,
	} {
		request("PATCH", preferencesPath, ownerToken, body, 400)
		assertPreferences(request("GET", preferencesPath, ownerToken, "", 200), false, false)
	}
	assertPreferences(request("PATCH", preferencesPath, ownerToken, `{}`, 200), false, false)
	if err := db.Model(&other).Update("access_blocked", true).Error; err != nil {
		t.Fatal(err)
	}
	request("PATCH", preferencesPath, otherToken, `{"exclude_va_from_available":true}`, 403)
	if err := db.Model(&other).Update("access_blocked", false).Error; err != nil {
		t.Fatal(err)
	}

	category := categories.Category{UserID: owner.ID, Name: "Balance integration"}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	addIncome := func(source string, amount float64, month, year int) {
		t.Helper()
		if err := db.Create(&incomes.Income{UserID: owner.ID, Source: source, Amount: amount, Month: month, Year: year}).Error; err != nil {
			t.Fatal(err)
		}
	}
	addExpense := func(source string, amount float64, month, year int, splits ...expenses.PaymentSplit) expenses.Expense {
		t.Helper()
		expense := expenses.Expense{UserID: owner.ID, CategoryID: category.ID, Description: "Balance fixture", Amount: amount, PaymentSource: source, Month: month, Year: year, Date: time.Date(year, time.Month(month), 20, 12, 0, 0, 0, time.UTC), Type: "Única", PaymentSplits: splits}
		if err := db.Create(&expense).Error; err != nil {
			t.Fatal(err)
		}
		return expense
	}
	summary := func(month, year int) map[string]any {
		t.Helper()
		return request("GET", fmt.Sprintf("/api/reports/summary?month=%d&year=%d", month, year), ownerToken, "", 200)
	}
	assertMoney := func(got map[string]any, key string, want float64) {
		t.Helper()
		amount, ok := got[key].(float64)
		if !ok || math.Abs(amount-want) > 0.0000001 {
			t.Fatalf("%s=%v want %.2f; summary=%v", key, got[key], want, got)
		}
	}
	financialFingerprint := func() map[string]string {
		t.Helper()
		result := make(map[string]string)
		for _, table := range []string{"incomes", "expenses", "expense_payment_splits", "categories"} {
			var fingerprint string
			query := fmt.Sprintf("SELECT md5(COALESCE(string_agg(row_to_json(t)::text, '' ORDER BY id), '')) FROM %s t", table)
			if err := db.Raw(query).Scan(&fingerprint).Error; err != nil {
				t.Fatal(err)
			}
			result[table] = fingerprint
		}
		return result
	}
	addIncome("Salário", 1000, 11, 2034)
	addIncome("VA", 725, 11, 2034)
	addIncome("VR", 300, 11, 2034)
	addExpense("VA", 600, 11, 2034)
	addExpense("VR", 100, 11, 2034)
	addExpense("Salário", 200, 11, 2034)
	baseline := summary(11, 2034)
	assertMoney(baseline, "total_geral_disponivel", 1125)
	assertMoney(baseline, "restante_va", 125)
	assertMoney(baseline, "restante_vr", 200)
	beforePreferences := financialFingerprint()
	exportPath := "/api/reports/export?type=summary&format=csv&month=11&year=2034"
	exportBefore, exportStatus, err := rawRequest("GET", exportPath, ownerToken, "")
	if err != nil || exportStatus != 200 {
		t.Fatalf("baseline export: status=%d err=%v", exportStatus, err)
	}
	for _, test := range []struct {
		va, vr    bool
		available float64
	}{
		{false, false, 1125}, {true, false, 1000}, {false, true, 925}, {true, true, 800},
	} {
		assertPreferences(request("PATCH", preferencesPath, ownerToken, fmt.Sprintf(`{"exclude_va_from_available":%t,"exclude_vr_from_available":%t}`, test.va, test.vr), 200), test.va, test.vr)
		assertPreferences(request("GET", preferencesPath, ownerToken, "", 200), test.va, test.vr)
		got := summary(11, 2034)
		assertMoney(got, "total_geral_disponivel", test.available)
		if got["exclude_va_from_available"] != test.va || got["exclude_vr_from_available"] != test.vr {
			t.Fatalf("summary did not disclose applied preferences: %v", got)
		}
		for key, value := range baseline {
			if key == "total_geral_disponivel" || key == "exclude_va_from_available" || key == "exclude_vr_from_available" {
				continue
			}
			if !reflect.DeepEqual(got[key], value) {
				t.Fatalf("preference changed unrelated summary field %s: got=%v want=%v", key, got[key], value)
			}
		}
		assertPreferences(request("GET", preferencesPath, otherToken, "", 200), false, false)
	}
	otherSummary := request("GET", "/api/reports/summary?month=11&year=2034", otherToken, "", 200)
	assertMoney(otherSummary, "total_income", 0)
	assertMoney(otherSummary, "total_geral_disponivel", 0)
	if otherSummary["exclude_va_from_available"] != false || otherSummary["exclude_vr_from_available"] != false {
		t.Fatalf("summary leaked another user's preference: %v", otherSummary)
	}
	assertPreferences(request("GET", fmt.Sprintf("%s?user_id=%d", preferencesPath, owner.ID), otherToken, "", 200), false, false)
	assertPreferences(request("PATCH", preferencesPath, ownerToken, `{"exclude_va_from_available":false}`, 200), false, true)
	assertPreferences(request("PATCH", preferencesPath, ownerToken, `{"exclude_va_from_available":true}`, 200), true, true)
	assertPreferences(request("PATCH", preferencesPath, ownerToken, `{"exclude_vr_from_available":false}`, 200), true, false)
	assertPreferences(request("PATCH", preferencesPath, ownerToken, `{"exclude_vr_from_available":true}`, 200), true, true)
	if after := financialFingerprint(); !reflect.DeepEqual(beforePreferences, after) {
		t.Fatalf("preference rewrote financial records: before=%v after=%v", beforePreferences, after)
	}
	exportAfter, exportStatus, err := rawRequest("GET", exportPath, ownerToken, "")
	if err != nil || exportStatus != 200 || !bytes.Equal(exportBefore, exportAfter) {
		t.Fatalf("display preference altered integral export: status=%d err=%v", exportStatus, err)
	}

	// Concurrent partial updates must preserve both independent choices, even
	// when they race to insert the first preference record for a user.
	for range 5 {
		if err := db.Exec("DELETE FROM user_balance_preferences WHERE user_id = ?", owner.ID).Error; err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		failures := make(chan error, 2)
		var group sync.WaitGroup
		for _, body := range []string{`{"exclude_va_from_available":true}`, `{"exclude_vr_from_available":true}`} {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				data, status, err := rawRequest("PATCH", preferencesPath, ownerToken, body)
				if err != nil || status != 200 {
					failures <- fmt.Errorf("partial update: status=%d body=%s err=%v", status, data, err)
				}
			}()
		}
		close(start)
		group.Wait()
		close(failures)
		for err := range failures {
			t.Error(err)
		}
		assertPreferences(request("GET", preferencesPath, ownerToken, "", 200), true, true)
	}

	// The choice applies to every month, without transferring card balances.
	addIncome("VA", 725, 1, 2035)
	addExpense("VA", 600, 1, 2035)
	onlyVA := summary(1, 2035)
	assertMoney(onlyVA, "restante_va", 125)
	assertMoney(onlyVA, "total_geral_disponivel", 0)
	request("PATCH", preferencesPath, ownerToken, `{"exclude_va_from_available":false}`, 200)
	assertMoney(summary(1, 2035), "total_geral_disponivel", 125)
	request("PATCH", preferencesPath, ownerToken, `{"exclude_va_from_available":true}`, 200)
	assertMoney(summary(11, 2034), "total_geral_disponivel", 800)

	addIncome("Salário", 1000, 12, 2034)
	addIncome("VA", 725, 12, 2034)
	addIncome("VR", 300, 12, 2034)
	mixed := addExpense("", 1000, 12, 2034,
		expenses.PaymentSplit{PaymentSource: "VA", Amount: 600},
		expenses.PaymentSplit{PaymentSource: "VR", Amount: 100},
		expenses.PaymentSplit{PaymentSource: "Salário", Amount: 300})
	assertMoney(summary(12, 2034), "total_geral_disponivel", 700)
	mixedPath := fmt.Sprintf("/api/expenses/%d", mixed.ID)
	request("PATCH", mixedPath+"/payment-status", ownerToken, `{"is_paid":true}`, 200)
	assertMoney(summary(12, 2034), "total_geral_disponivel", 700)
	request("PATCH", mixedPath, otherToken, `{"amount":1}`, 404)
	request("PATCH", mixedPath, ownerToken, `{"amount":900,"payment_splits":[{"payment_source":"VA","amount":500},{"payment_source":"VR","amount":100},{"payment_source":"Salario","amount":300}]}`, 200)
	assertMoney(summary(12, 2034), "total_geral_disponivel", 700)
	request("PATCH", mixedPath, ownerToken, `{"amount":1000,"payment_splits":[{"payment_source":"VA","amount":500},{"payment_source":"VR","amount":100},{"payment_source":"Salario","amount":400}]}`, 200)
	assertMoney(summary(12, 2034), "total_geral_disponivel", 600)
	request("DELETE", mixedPath, ownerToken, "", 200)
	assertMoney(summary(12, 2034), "total_geral_disponivel", 1000)

	// Negative card balances stay signed; clamping them would subtract money
	// spent on the benefit from the cash balance a second time.
	addIncome("Salário", 500, 2, 2035)
	addIncome("VA", 100, 2, 2035)
	addIncome("VR", 75, 2, 2035)
	addExpense("VA", 150, 2, 2035)
	addExpense("VR", 100, 2, 2035)
	addExpense("Salário", 50, 2, 2035)
	negative := summary(2, 2035)
	assertMoney(negative, "restante_va", -50)
	assertMoney(negative, "restante_vr", -25)
	assertMoney(negative, "total_geral_disponivel", 450)
	for _, month := range []int{3, 4} {
		addIncome("Salário", 500, month, 2035)
		addIncome("VA", 100, month, 2035)
		addIncome("VR", 50, month, 2035)
	}
	advanced := addExpense("VA", 80, 4, 2035)
	advancePath := fmt.Sprintf("/api/expenses/%d/advance-status", advanced.ID)
	request("PATCH", advancePath, ownerToken, `{"is_advanced":true,"advanced_at":"2035-03-20"}`, 200)
	assertMoney(summary(3, 2035), "restante_va", 20)
	assertMoney(summary(4, 2035), "restante_va", 100)
	assertMoney(summary(3, 2035), "total_geral_disponivel", 500)
	assertMoney(summary(4, 2035), "total_geral_disponivel", 500)
	request("PATCH", advancePath, ownerToken, `{"is_advanced":false}`, 200)
	assertMoney(summary(3, 2035), "restante_va", 100)
	assertMoney(summary(4, 2035), "restante_va", 20)
	assertMoney(summary(4, 2035), "total_geral_disponivel", 500)
}
