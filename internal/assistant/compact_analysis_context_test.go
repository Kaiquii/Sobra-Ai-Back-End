package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCompactAnalysisSnapshotPreservesScopeAndMoneyWithoutMutation(t *testing.T) {
	now := time.Date(2034, 11, 3, 12, 0, 0, 0, time.UTC)
	periods := []map[string]any{}
	for _, year := range []int{2035, 2034} {
		var expenseRows []expenses.Expense
		var incomeRows []incomes.Income
		names := map[uint]string{}
		for month := 1; month <= 12; month++ {
			incomeRows = append(incomeRows, incomes.Income{UserID: 7, Year: year, Month: month, Amount: 3000, Source: "Salario"})
			for category := 1; category <= 25; category++ {
				names[uint(category)] = fmt.Sprintf("Categoria %02d", category)
				expenseRows = append(expenseRows, expenses.Expense{UserID: 7, CategoryID: uint(category), Year: year, Month: month, Amount: float64(category), PaymentSource: "Salario"})
			}
		}
		periods = append(periods, aggregateAnalysisYear(year, incomeRows, expenseRows, names, now))
	}
	facts := map[string]any{"as_of": "2034-11-03", "years": []int{2035, 2034}, "periods": periods, "largest_registered_installment_purchases_all_time": []any{map[string]any{"description": "PRIVATE_UNNEEDED_PURCHASE_837", "total_amount": 99999}}}
	before, _ := json.Marshal(facts)
	compact, err := compactAnalysisFactsForPrompt(facts)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(facts)
	if string(before) != string(after) {
		t.Fatal("context projection mutated the authenticated snapshot")
	}
	encoded, _ := json.Marshal(compact)
	if len(encoded) >= len(before) || len(encoded) > compactAnalysisContextLimit || strings.Contains(string(encoded), "PRIVATE_UNNEEDED_PURCHASE_837") {
		t.Fatalf("unneeded payload was retained: original=%d compact=%d", len(before), len(encoded))
	}
	t.Logf("snapshot bytes: original=%d compact=%d", len(before), len(encoded))
	var original any
	_ = json.Unmarshal(before, &original)
	for _, path := range []string{"/periods/0/total_expense", "/periods/1/months_with_records/10/total_cash_balance", "/periods/1/categories/0/total_amount", "/periods/1/categories/7/total_amount", "/periods/1/categories/0/monthly_amounts_jan_to_dec/9"} {
		want, err := analysisNumberAt(original, path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := analysisNumberAt(compact, path)
		if err != nil || got != want {
			t.Fatalf("projection reindexed or changed monetary scope: %s got=%v want=%v err=%v", path, got, want, err)
		}
	}
	view := compact["periods"].([]any)[1].(map[string]any)
	if view["categories_complete"] != false || len(view["categories"].([]any)) != compactAnalysisCategoryLimit || view["represented_category_count"] != 25 {
		t.Fatal("shortened category coverage was presented as complete")
	}
	current := compact["current_month"].(map[string]any)
	if current["year"] != 2034 || intValue(current["month"]) != 11 || numberToFloat(current["total_cash_balance"]) != 2675 {
		t.Fatal("future objective year replaced current capacity")
	}
	for _, path := range []string{"/calendar/next_year", "/current_month/year", "/current_month/month"} {
		if _, err := analysisNumberAt(compact, path); err == nil {
			t.Fatal("calendar metadata became money")
		}
	}
	again, err := compactAnalysisFactsForPrompt(compact)
	if err != nil || !reflect.DeepEqual(again, compact) {
		t.Fatalf("generation and validation do not share an idempotent view: %v", err)
	}
}

func TestCompactAnalysisSnapshotDoesNotInventCurrentRecords(t *testing.T) {
	facts := map[string]any{"as_of": "2034-11-03", "years": []int{2034}, "periods": []any{map[string]any{"year": 2034, "months_with_records": []any{map[string]any{"month": 10, "total_cash_balance": 150}, map[string]any{"month": 12, "total_cash_balance": 900}}, "categories": []any{}, "categories_complete": true}}}
	view, err := compactAnalysisFactsForPrompt(facts)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := view["current_month"]; exists || intValue(view["latest_registered_month"].(map[string]any)["month"]) != 10 {
		t.Fatal("a missing current month or future registration became present cash")
	}
	if got := intValue(view["calendar"].(map[string]any)["next_year"]); got != 2035 {
		t.Fatal("trusted calendar did not preserve next year's goal")
	}
}

func TestCompactAnalysisSnapshotRejectsUnboundedOrMalformedData(t *testing.T) {
	for _, facts := range []map[string]any{nil, {"value": strings.Repeat("x", 48001)}, {"value": strings.Repeat("x", 16001)}, {"periods": nil}, {"periods": []any{nil}}} {
		if _, err := compactAnalysisFactsForPrompt(facts); err == nil {
			t.Fatal("invalid or oversized data was silently accepted")
		}
	}
}

func TestAnalysisIncomeRegistrationSeparatesMissingFromRegisteredZero(t *testing.T) {
	now := time.Date(2034, 11, 3, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name  string
		rows  []incomes.Income
		known bool
	}{
		{"missing", nil, false},
		{"registered zero", []incomes.Income{{Month: 11, Year: 2034, Amount: 0, Source: "Salario"}}, true},
		{"restricted benefits only", []incomes.Income{{Month: 11, Year: 2034, Amount: 100, Source: "VA"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			period := aggregateAnalysisYear(2034, test.rows, []expenses.Expense{{Month: 11, Year: 2034, CategoryID: 1, Amount: 50, PaymentSource: "Salario"}}, map[uint]string{1: "Moradia"}, now)
			view, err := compactAnalysisFactsForPrompt(map[string]any{"as_of": "2034-11-03", "years": []int{2034}, "periods": []map[string]any{period}})
			if err != nil {
				t.Fatal(err)
			}
			month := view["current_month"].(map[string]any)
			if month["cash_income_registered"] != test.known || month["expenses_registered"] != true || numberToFloat(month["total_cash_balance"]) != -50 {
				t.Fatal("missing income became a verified zero salary or cash totals changed")
			}
		})
	}
}

func TestCompactAnalysisLongLabelsPreserveCashAndDeclarePartialCategories(t *testing.T) {
	periods := []any{}
	for _, year := range []int{2034, 2035} {
		months := []any{}
		for month := 1; month <= 12; month++ {
			months = append(months, map[string]any{"month": month, "total_income": 3200, "total_expense": 2400, "total_balance": 800, "total_cash_income": 3200, "total_cash_expense": 2400, "total_cash_balance": 800, "cash_income_registered": true, "expenses_registered": true})
		}
		categories := []any{}
		for i := 0; i < 19; i++ {
			categories = append(categories, map[string]any{"name": strings.Repeat("界", 253) + fmt.Sprintf("%02d", i), "total_amount": 100 + i})
		}
		periods = append(periods, map[string]any{"year": year, "months_with_records": months, "categories": categories, "categories_complete": true, "total_expense": 28800})
	}
	facts := map[string]any{"as_of": "2034-11-03", "years": []int{2034, 2035}, "periods": periods}
	view, err := compactAnalysisFactsForPrompt(facts)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(view)
	if len(encoded) > compactAnalysisContextLimit {
		t.Fatal("category labels exceeded the compact input ceiling")
	}
	for _, raw := range view["periods"].([]any) {
		period := raw.(map[string]any)
		if period["categories_complete"] != false || period["represented_category_count"] != 19 || len(period["months_with_records"].([]any)) != 12 || numberToFloat(period["total_expense"]) != 28800 {
			t.Fatal("long labels weakened totals, months or completeness metadata")
		}
	}
	again, err := compactAnalysisFactsForPrompt(view)
	if err != nil || !reflect.DeepEqual(view, again) {
		t.Fatal("size-driven projection was not idempotent")
	}
}
