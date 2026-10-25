//go:build integration

package main

import (
	"Sobra_Ai_Back-end/internal/incomes"
	"fmt"
	"gorm.io/gorm"
	"testing"
)

func testBenefitsLifecycle(t *testing.T, db *gorm.DB, request func(string, string, string, string, int) map[string]any, owner, other string, catID int) {
	t.Helper()
	for _, source := range []string{"VA", "VR"} {
		request("POST", "/api/incomes/", owner, fmt.Sprintf(`{"source":%q,"amount":600,"month":12,"year":2032,"type":"Fixa"}`, source), 201)
		var items []incomes.Income
		if err := db.Where("source = ? AND year >= 2032", source).Order("year,month").Find(&items).Error; err != nil || len(items) != 60 {
			t.Fatalf("recurrence %s: %d %v", source, len(items), err)
		}
		path := fmt.Sprintf("/api/incomes/%d", items[0].ID)
		request("PATCH", path, other, `{"amount":900,"update_future":true}`, 404)
		request("PATCH", path, owner, `{"amount":500,"update_future":false}`, 200)
		request("PATCH", path, owner, `{"user_id":999}`, 400)
		request("PATCH", path, owner, `{"amount":-1}`, 400)
		request("PATCH", path, owner, `{"source":"VA/VR"}`, 400)
		request("PATCH", path, owner, `{"month":1,"update_future":true}`, 400)
		request("PATCH", path, owner, `{"update_future":"true"}`, 400)
		var next incomes.Income
		db.First(&next, items[1].ID)
		if next.Amount != 600 {
			t.Fatal("single-month update changed future")
		}
		secondPath := fmt.Sprintf("/api/incomes/%d", items[1].ID)
		request("PATCH", secondPath, owner, `{"amount":700,"update_future":true}`, 200)
		var first, last incomes.Income
		db.First(&first, items[0].ID)
		db.First(&last, items[59].ID)
		if first.Amount != 500 || last.Amount != 700 {
			t.Fatalf("future scope: %+v %+v", first, last)
		}
		request("DELETE", secondPath+"?delete_future=true", other, "", 404)
		request("DELETE", secondPath+"?delete_future=true", owner, "", 200)
		var count int64
		db.Model(&incomes.Income{}).Where("source = ? AND year >= 2032", source).Count(&count)
		if count != 1 {
			t.Fatalf("future delete: %d", count)
		}
	}
	// No carry-forward: January has no income although December has unspent benefits.
	summary := request("GET", "/api/reports/summary?month=1&year=2033", owner, "", 200)
	if summary["va"] != float64(0) || summary["vr"] != float64(0) {
		t.Fatalf("carried balance: %v", summary)
	}
	request("POST", "/api/expenses/", owner, fmt.Sprintf(`{"description":"Benefits split","amount":150,"category_id":%d,"date":"2032-12-20T12:00:00Z","type":"Unica","payment_splits":[{"payment_source":"VA","amount":100},{"payment_source":"VR","amount":50}]}`, catID), 201)
	check := func(va, vr float64) {
		t.Helper()
		s := request("GET", "/api/reports/summary?month=12&year=2032", owner, "", 200)
		if s["restante_va"] != va || s["restante_vr"] != vr || s["total_geral_disponivel"] != va+vr {
			t.Fatalf("wrong balances: %v", s)
		}
	}
	check(400, 450)
	list := request("GET", "/api/expenses/?month=12&year=2032", owner, "", 200)
	id := list["expenses"].([]any)[0].(map[string]any)["id"].(float64)
	path := fmt.Sprintf("/api/expenses/%.0f", id)
	request("PATCH", path, owner, `{"amount":200,"payment_splits":[{"payment_source":"VA","amount":80},{"payment_source":"VR","amount":120}]}`, 200)
	check(420, 380)
	request("PATCH", path, owner, `{"payment_source":"VA","amount":200}`, 200)
	check(300, 500)
	request("PATCH", path+"/payment-status", owner, `{"is_paid":true}`, 200)
	check(300, 500)
	request("DELETE", path, owner, "", 200)
	check(500, 500)
	// A failing later row must roll back all preceding recurring writes.
	if err := db.Exec(`CREATE FUNCTION reject_benefit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.source = 'VA' AND NEW.year = 2040 AND NEW.month = 2 THEN RAISE EXCEPTION 'test failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_benefit_test BEFORE INSERT OR UPDATE ON incomes FOR EACH ROW EXECUTE FUNCTION reject_benefit_test()`).Error; err != nil {
		t.Fatal(err)
	}
	request("POST", "/api/incomes/", owner, `{"source":"VA","amount":100,"month":1,"year":2040,"type":"Fixa"}`, 500)
	var count int64
	db.Model(&incomes.Income{}).Where("year >= 2040").Count(&count)
	if count != 0 {
		t.Fatal("partial recurring creation")
	}
	db.Exec(`DROP TRIGGER reject_benefit_test ON incomes; DROP FUNCTION reject_benefit_test()`)
	request("POST", "/api/incomes/", owner, `{"source":"VR","amount":100,"month":1,"year":2040,"type":"Fixa"}`, 201)
	var row incomes.Income
	db.Where("source = ? AND year = ? AND month = ?", "VR", 2040, 1).First(&row)
	db.Exec(`CREATE FUNCTION reject_benefit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.source = 'VR' AND NEW.year = 2040 AND NEW.month = 2 AND NEW.amount = 200 THEN RAISE EXCEPTION 'test failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_benefit_test BEFORE UPDATE ON incomes FOR EACH ROW EXECUTE FUNCTION reject_benefit_test()`)
	request("PATCH", fmt.Sprintf("/api/incomes/%d", row.ID), owner, `{"amount":200,"update_future":true}`, 500)
	db.First(&row, row.ID)
	if row.Amount != 100 {
		t.Fatal("partial future update")
	}
	db.Exec(`DROP TRIGGER reject_benefit_test ON incomes; DROP FUNCTION reject_benefit_test()`)
}
