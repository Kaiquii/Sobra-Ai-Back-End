//go:build integration

package main

import (
	"Sobra_Ai_Back-end/internal/auth"
	"Sobra_Ai_Back-end/internal/migrations"
	"Sobra_Ai_Back-end/internal/patrimony"
	"fmt"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"
)

func testInvestmentCategories(t *testing.T, db *gorm.DB, request func(string, string, string, string, int) map[string]any, owner, other string) {
	t.Helper()
	const catalog = "/api/patrimony/investment-categories"
	const investments = "/api/patrimony/investments"
	var ownerUser auth.User
	if err := db.Where("email = ?", "owner@example.test").First(&ownerUser).Error; err != nil {
		t.Fatal(err)
	}
	// Exercise upgrade from migration 6 with the original text category contract.
	if err := db.Transaction(func(tx *gorm.DB) error {
		return tx.Exec("ALTER TABLE investments DROP COLUMN category_id; DROP TABLE investment_categories; DELETE FROM schema_migrations WHERE version >= 7").Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO investments(user_id,name,category,amount_cents,created_at,updated_at) VALUES (?, 'Upgrade probe','CDI',101,'2026-10-01T12:00:00Z','2026-10-01T12:00:00Z')", ownerUser.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrations.Validate(db); err == nil {
		t.Fatal("missing migration 7 was accepted")
	}
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	var upgraded patrimony.Investment
	if err := db.Where("name = ?", "Upgrade probe").First(&upgraded).Error; err != nil {
		t.Fatal(err)
	}
	legacyTimestamp := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if upgraded.Category != "CDI" || upgraded.CategoryID == 0 || upgraded.AmountCents != 101 || !upgraded.CreatedAt.Equal(legacyTimestamp) || !upgraded.UpdatedAt.Equal(legacyTimestamp) {
		t.Fatalf("upgrade changed legacy data: %+v", upgraded)
	}
	var personalCDI patrimony.InvestmentCategory
	if err := db.First(&personalCDI, upgraded.CategoryID).Error; err != nil || personalCDI.UserID == nil || *personalCDI.UserID != ownerUser.ID || personalCDI.Name != "CDI" {
		t.Fatalf("migration 7 did not preserve the personal CDI category: %+v %v", personalCDI, err)
	}
	// Reconstruct schema 7 in this disposable fixture by removing only migration
	// 8's global seed. Existing personal CDI and its investment must survive 7 -> 8.
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id IS NULL AND name = ?", "CDI").Delete(&patrimony.InvestmentCategory{}).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM schema_migrations WHERE version = 8").Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Validate(db); err == nil {
		t.Fatal("missing migration 8 was accepted")
	}
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	var afterCDIUpgrade patrimony.Investment
	if err := db.First(&afterCDIUpgrade, upgraded.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterCDIUpgrade.ID != upgraded.ID || afterCDIUpgrade.UserID != upgraded.UserID || afterCDIUpgrade.CategoryID != personalCDI.ID || afterCDIUpgrade.Name != upgraded.Name || afterCDIUpgrade.Category != upgraded.Category || afterCDIUpgrade.AmountCents != upgraded.AmountCents || !afterCDIUpgrade.CreatedAt.Equal(upgraded.CreatedAt) || !afterCDIUpgrade.UpdatedAt.Equal(upgraded.UpdatedAt) {
		t.Fatalf("migration 8 changed an existing investment: before=%+v after=%+v", upgraded, afterCDIUpgrade)
	}
	var afterPersonalCDI patrimony.InvestmentCategory
	if err := db.First(&afterPersonalCDI, personalCDI.ID).Error; err != nil || afterPersonalCDI.UserID == nil || *afterPersonalCDI.UserID != ownerUser.ID || afterPersonalCDI.Name != personalCDI.Name {
		t.Fatalf("migration 8 changed personal category ownership or identity: %+v %v", afterPersonalCDI, err)
	}
	request("GET", catalog, "", "", 401)
	list := request("GET", catalog, owner, "", 200)
	defaultIDs := map[string]float64{}
	for _, value := range list["categories"].([]any) {
		item := value.(map[string]any)
		if item["is_default"] == true {
			defaultIDs[item["name"].(string)] = item["id"].(float64)
		}
	}
	if len(defaultIDs) != 15 || defaultIDs["CDI"] == 0 || defaultIDs["CDI"] == float64(personalCDI.ID) {
		t.Fatalf("unexpected defaults: %v", defaultIDs)
	}
	for _, name := range []string{"CDB", "CDI", "RDB", "Tesouro Direto", "LCI", "LCA", "CRI", "CRA", "Poupança", "Ações", "ETFs", "Outros"} {
		if defaultIDs[name] == 0 {
			t.Fatalf("missing default %q", name)
		}
	}
	otherList := request("GET", catalog, other, "", 200)
	if otherList["total"] != float64(15) {
		t.Fatalf("custom categories leaked: %v", otherList)
	}
	personalCDIVisible := false
	for _, value := range list["categories"].([]any) {
		item := value.(map[string]any)
		if item["id"] == float64(personalCDI.ID) {
			personalCDIVisible = item["name"] == "CDI" && item["is_default"] == false
		}
	}
	if !personalCDIVisible {
		t.Fatal("existing personal CDI was hidden by the global category")
	}
	cdiDefaultPath := fmt.Sprintf("%s/%.0f", catalog, defaultIDs["CDI"])
	if result := request("PATCH", cdiDefaultPath, owner, `{"name":"Changed CDI"}`, 403); result["code"] != "default_category" {
		t.Fatalf("CDI default edit was not protected: %v", result)
	}
	if result := request("DELETE", cdiDefaultPath, owner, "", 403); result["code"] != "default_category" {
		t.Fatalf("CDI default deletion was not protected: %v", result)
	}
	request("POST", catalog, owner, `{"name":" cDi "}`, 409)
	cdiLegacy := request("POST", investments, owner, `{"name":"CDI legacy client","category":"cDi","amount":2.22}`, 201)["data"].(map[string]any)
	if cdiLegacy["category_id"] != defaultIDs["CDI"] || cdiLegacy["category"] != "CDI" {
		t.Fatalf("legacy CDI did not prefer the global category: %v", cdiLegacy)
	}
	cdiLegacyPath := fmt.Sprintf("%s/%.0f", investments, cdiLegacy["id"])
	cdiByID := request("PATCH", cdiLegacyPath, owner, fmt.Sprintf(`{"category_id":%d}`, personalCDI.ID), 200)["data"].(map[string]any)
	if cdiByID["category_id"] != float64(personalCDI.ID) || cdiByID["category"] != "CDI" {
		t.Fatalf("personal CDI ID was overridden by the global category: %v", cdiByID)
	}
	cdiByText := request("PATCH", cdiLegacyPath, owner, `{"category":"cDi"}`, 200)["data"].(map[string]any)
	if cdiByText["category_id"] != defaultIDs["CDI"] || cdiByText["category"] != "CDI" {
		t.Fatalf("legacy CDI patch did not prefer the global category: %v", cdiByText)
	}
	cdiPersonal := request("POST", investments, owner, fmt.Sprintf(`{"name":"Personal CDI ID","category_id":%d,"amount":3.33}`, personalCDI.ID), 201)["data"].(map[string]any)
	if cdiPersonal["category_id"] != float64(personalCDI.ID) || cdiPersonal["category"] != "CDI" {
		t.Fatalf("personal CDI ID creation failed: %v", cdiPersonal)
	}
	personalCDIPath := fmt.Sprintf("%s/%d", catalog, personalCDI.ID)
	request("DELETE", personalCDIPath, owner, "", 409)
	request("DELETE", fmt.Sprintf("%s/%d", investments, upgraded.ID), owner, "", 200)
	request("DELETE", fmt.Sprintf("%s/%.0f", investments, cdiPersonal["id"]), owner, "", 200)
	request("DELETE", cdiLegacyPath, owner, "", 200)
	request("DELETE", personalCDIPath, owner, "", 200)
	defaultPath := fmt.Sprintf("%s/%.0f", catalog, defaultIDs["CDB"])
	request("PATCH", defaultPath, owner, `{"name":"Changed"}`, 403)
	request("DELETE", defaultPath, owner, "", 403)
	request("DELETE", defaultPath, other, "", 403)
	request("POST", catalog, owner, `{"name":" cdb "}`, 409)
	custom := request("POST", catalog, owner, `{"name":" Minha estrategia "}`, 201)["data"].(map[string]any)
	if custom["name"] != "Minha estrategia" || custom["is_default"] != false {
		t.Fatalf("custom category: %v", custom)
	}
	categoryID := custom["id"].(float64)
	path := fmt.Sprintf("%s/%.0f", catalog, categoryID)
	request("POST", catalog, owner, `{"name":"MINHA ESTRATEGIA"}`, 409)
	request("PATCH", path, other, `{"name":"Forbidden"}`, 404)
	request("DELETE", path, other, "", 404)
	request("POST", investments, other, fmt.Sprintf(`{"name":"Forbidden","category_id":%.0f,"amount":1}`, categoryID), 404)
	request("POST", investments, owner, `{"name":"Invalid","category_id":99999999,"amount":1}`, 404)
	monthlyBefore := request("GET", "/api/reports/summary?month=12&year=2032", owner, "", 200)
	// Read the generated ID only after the insert completes. Go permits evaluating
	// a struct return value before a method call in another return expression.
	generated := request("POST", investments, owner, `{"name":"New legacy category","category":"Generated legacy category","amount":1}`, 201)["data"].(map[string]any)
	generatedID := generated["category_id"].(float64)
	if generatedID <= 0 {
		t.Fatal("new legacy category returned a zero ID")
	}
	var persistedCategory patrimony.InvestmentCategory
	if err := db.First(&persistedCategory, uint(generatedID)).Error; err != nil || persistedCategory.Name != "Generated legacy category" {
		t.Fatalf("generated category not persisted: %+v %v", persistedCategory, err)
	}
	generatedPath := fmt.Sprintf("%s/%.0f", investments, generated["id"])
	reused := request("PATCH", generatedPath, owner, `{"category":"GENERATED LEGACY CATEGORY"}`, 200)["data"].(map[string]any)
	if reused["category_id"] != generatedID {
		t.Fatal("legacy text created a duplicate category")
	}
	changed := request("PATCH", generatedPath, owner, `{"category":"Another generated category"}`, 200)["data"].(map[string]any)
	if changed["category_id"].(float64) <= 0 || changed["category_id"] == generatedID {
		t.Fatal("legacy patch did not return the new generated ID")
	}
	request("DELETE", generatedPath, owner, "", 200)
	created := request("POST", investments, owner, fmt.Sprintf(`{"name":"Category test","category_id":%.0f,"amount":123.45}`, categoryID), 201)["data"].(map[string]any)
	if created["category_id"] != categoryID || created["category"] != "Minha estrategia" {
		t.Fatalf("association: %v", created)
	}
	investmentPath := fmt.Sprintf("%s/%.0f", investments, created["id"])
	request("PATCH", investmentPath, other, fmt.Sprintf(`{"category_id":%.0f}`, categoryID), 404)
	request("PATCH", path, owner, `{"name":"CDB"}`, 409)
	request("DELETE", path, owner, "", 409)
	// Force the second write to fail: both category name and legacy labels must roll back.
	if err := db.Exec(`CREATE FUNCTION reject_category_rename_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.category = 'Reject rename' THEN RAISE EXCEPTION 'test failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_category_rename_test BEFORE UPDATE ON investments FOR EACH ROW EXECUTE FUNCTION reject_category_rename_test()`).Error; err != nil {
		t.Fatal(err)
	}
	request("PATCH", path, owner, `{"name":"Reject rename"}`, 500)
	if err := db.Exec(`DROP TRIGGER reject_category_rename_test ON investments; DROP FUNCTION reject_category_rename_test()`).Error; err != nil {
		t.Fatal(err)
	}
	var unchanged patrimony.InvestmentCategory
	if err := db.First(&unchanged, uint(categoryID)).Error; err != nil || unchanged.Name != "Minha estrategia" {
		t.Fatalf("partial category rename: %+v %v", unchanged, err)
	}
	request("PATCH", path, owner, `{"name":"Estrategia atualizada"}`, 200)
	var updated patrimony.Investment
	if err := db.First(&updated, uint(created["id"].(float64))).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Category != "Estrategia atualizada" || updated.CategoryID != uint(categoryID) || updated.AmountCents != 12345 {
		t.Fatalf("rename changed money or failed propagation: %+v", updated)
	}
	// Text writes from older app versions resolve to the same catalog entry.
	legacy := request("POST", investments, owner, `{"name":"Old client","category":"cdb","amount":0}`, 201)["data"].(map[string]any)
	if legacy["category_id"] != defaultIDs["CDB"] || legacy["category"] != "CDB" {
		t.Fatalf("legacy category resolution: %v", legacy)
	}
	request("PATCH", investmentPath, owner, fmt.Sprintf(`{"category_id":%.0f}`, defaultIDs["LCI"]), 200)
	request("DELETE", path, owner, "", 200)
	request("DELETE", path, owner, "", 404)
	request("PATCH", investmentPath, owner, fmt.Sprintf(`{"category_id":%.0f}`, categoryID), 404)
	// If investment creation fails, an automatically resolved personal category must not persist.
	if err := db.Exec(`CREATE FUNCTION reject_category_insert_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.name = 'Reject new investment' THEN RAISE EXCEPTION 'test failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_category_insert_test BEFORE INSERT ON investments FOR EACH ROW EXECUTE FUNCTION reject_category_insert_test()`).Error; err != nil {
		t.Fatal(err)
	}
	request("POST", investments, owner, `{"name":"Reject new investment","category":"Rollback custom","amount":1}`, 500)
	if err := db.Exec(`DROP TRIGGER reject_category_insert_test ON investments; DROP FUNCTION reject_category_insert_test()`).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&patrimony.InvestmentCategory{}).Where("name = ?", "Rollback custom").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("orphan category after failure: %d %v", count, err)
	}
	if after := request("GET", "/api/reports/summary?month=12&year=2032", owner, "", 200); !reflect.DeepEqual(monthlyBefore, after) {
		t.Fatal("categories changed monthly finances")
	}
	request("DELETE", investmentPath, owner, "", 200)
	request("DELETE", fmt.Sprintf("%s/%.0f", investments, legacy["id"]), owner, "", 200)
	// User removal cascades through owned categories and investments, preserving system categories.
	probe := auth.User{Name: "Category cascade", Email: "category-cascade@example.test", Password: "unused"}
	if err := db.Create(&probe).Error; err != nil {
		t.Fatal(err)
	}
	probeCategory := patrimony.InvestmentCategory{UserID: &probe.ID, Name: "Owned"}
	if err := db.Create(&probeCategory).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&patrimony.Investment{UserID: probe.ID, Name: "Cascade", Category: "Owned", CategoryID: probeCategory.ID, AmountCents: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&probe).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&patrimony.InvestmentCategory{}).Where("user_id = ?", probe.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("category cascade: %d %v", count, err)
	}
	if err := db.Model(&patrimony.InvestmentCategory{}).Where("user_id IS NULL").Count(&count).Error; err != nil || count != 15 {
		t.Fatalf("defaults changed: %d %v", count, err)
	}
}
