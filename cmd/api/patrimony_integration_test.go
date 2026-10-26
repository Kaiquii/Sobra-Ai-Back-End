//go:build integration

package main

import (
	"Sobra_Ai_Back-end/internal/auth"
	"Sobra_Ai_Back-end/internal/migrations"
	"Sobra_Ai_Back-end/internal/patrimony"
	"fmt"
	"reflect"
	"testing"

	"gorm.io/gorm"
)

func testPatrimonyLifecycle(t *testing.T, db *gorm.DB, request func(string, string, string, string, int) map[string]any, owner, other string) {
	t.Helper()
	// Reproduce upgrade from the deployed schema (versions 1..5), with existing users and financial data.
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DROP TABLE investments; DROP TABLE fgts_balances; DROP TABLE investment_categories").Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM schema_migrations WHERE version >= 6").Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Validate(db); err == nil {
		t.Fatal("API accepted pending migration")
	}
	beforeUpgrade := request("GET", "/api/reports/summary?month=12&year=2032", owner, "", 200)
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Validate(db); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	if after := request("GET", "/api/reports/summary?month=12&year=2032", owner, "", 200); !reflect.DeepEqual(beforeUpgrade, after) {
		t.Fatal("upgrade changed existing finances")
	}
	base := "/api/patrimony"
	request("GET", base+"/", "", "", 401)
	request("PUT", base+"/fgts", "", `{"amount":100}`, 401)
	empty := request("GET", base+"/", owner, "", 200)
	if empty["fgts"] != nil || len(empty["investments"].([]any)) != 0 || empty["total_patrimony"] != float64(0) {
		t.Fatalf("empty: %v", empty)
	}
	monthlyBefore := request("GET", "/api/reports/summary?month=12&year=2032", owner, "", 200)
	request("PUT", base+"/fgts", owner, `{"amount":1000.25}`, 200)
	request("PUT", base+"/fgts", owner, `{"amount":2000.50}`, 200)
	request("PUT", base+"/fgts", other, `{"amount":25}`, 200)
	first := request("POST", base+"/investments", owner, `{"name":" Reserva ","category":" CDB ","amount":500.10}`, 201)["data"].(map[string]any)
	if first["name"] != "Reserva" || first["category"] != "CDB" || first["updated_at"] == nil {
		t.Fatalf("create: %v", first)
	}
	path := fmt.Sprintf("%s/investments/%.0f", base, first["id"])
	request("POST", base+"/investments", owner, `{"name":"Outro","category":"Minha categoria","amount":0.20}`, 201)
	request("PATCH", path, other, `{"amount":1}`, 404)
	request("DELETE", path, other, "", 404)
	request("PATCH", path, owner, `{"user_id":999}`, 400)
	request("PATCH", path, owner, `{"amount":-1}`, 400)
	request("PATCH", path, owner, `{"amount":1.001}`, 400)
	request("PATCH", path, owner, `{"name":null}`, 400)
	updated := request("PATCH", path, owner, `{"name":"Emergencia","category":"Renda fixa","amount":0}`, 200)["data"].(map[string]any)
	if updated["amount"] != float64(0) || updated["name"] != "Emergencia" || updated["category"] != "Renda fixa" {
		t.Fatalf("patch: %v", updated)
	}
	request("PATCH", path, owner, `{"amount":600.10}`, 200)
	updated = request("PATCH", path, owner, `{"name":"Renomeado"}`, 200)["data"].(map[string]any)
	if updated["amount"] != 600.10 || updated["category"] != "Renda fixa" {
		t.Fatal("partial update overwrote fields")
	}
	summary := request("GET", base+"/", owner, "", 200)
	if summary["total_fgts"] != 2000.50 || summary["total_investments"] != 600.30 || summary["total_patrimony"] != 2600.80 || len(summary["investments"].([]any)) != 2 {
		t.Fatalf("totals: %v", summary)
	}
	isolated := request("GET", base+"/", other, "", 200)
	if isolated["total_patrimony"] != float64(25) || len(isolated["investments"].([]any)) != 0 {
		t.Fatalf("isolation: %v", isolated)
	}
	monthlyAfter := request("GET", "/api/reports/summary?month=12&year=2032", owner, "", 200)
	if !reflect.DeepEqual(monthlyBefore, monthlyAfter) {
		t.Fatalf("patrimony changed monthly report: %v -> %v", monthlyBefore, monthlyAfter)
	}
	request("PUT", base+"/fgts", owner, `{"amount":0}`, 200)
	request("DELETE", base+"/fgts", owner, "", 200)
	request("DELETE", base+"/fgts", owner, "", 404)
	request("DELETE", path, owner, "", 200)
	request("DELETE", path, owner, "", 404)
	summary = request("GET", base+"/", owner, "", 200)
	if summary["fgts"] != nil || summary["total_patrimony"] != 0.20 {
		t.Fatalf("delete totals: %v", summary)
	}
	// Database constraints reject invalid money and deleting a user removes only their patrimony.
	var user auth.User
	if err := db.Where("email = ?", "other@example.test").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE fgts_balances SET amount_cents = -1 WHERE user_id = ?", user.ID).Error; err == nil {
		t.Fatal("missing amount constraint")
	}
	probe := auth.User{Name: "Cascade", Email: "cascade@example.test", Password: "unused"}
	if err := db.Create(&probe).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&patrimony.FGTSBalance{UserID: probe.ID, AmountCents: 1}).Error; err != nil {
		t.Fatal(err)
	}
	var defaultCategory patrimony.InvestmentCategory
	if err := db.Where("user_id IS NULL AND name = ?", "CDB").First(&defaultCategory).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&patrimony.Investment{UserID: probe.ID, Name: "Teste", Category: "CDB", CategoryID: defaultCategory.ID, AmountCents: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&probe).Error; err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&patrimony.FGTSBalance{}, &patrimony.Investment{}} {
		var count int64
		if err := db.Model(model).Where("user_id = ?", probe.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("cascade: %d %v", count, err)
		}
	}
}
