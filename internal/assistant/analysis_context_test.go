package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAnalysisAggregatesVideoPeriodsAndRestrictedBenefits(t *testing.T) {
	now := time.Date(2026, 10, 28, 21, 0, 0, 0, time.FixedZone("Brasilia", -10800))
	advanced := time.Date(2026, 10, 12, 12, 0, 0, 0, time.Local)
	rows := []expenses.Expense{
		{CategoryID: 1, Amount: 131.34, Month: 10, Year: 2026, PaymentSource: "Salario"},
		{CategoryID: 1, Amount: 113.36, Month: 11, Year: 2026, PaymentSource: "Salario"},
		{CategoryID: 2, Amount: 500, Month: 10, Year: 2026, PaymentSource: "VA"},
		{CategoryID: 2, Amount: 300, Month: 2, Year: 2027, Type: "Parcelada", Description: "Compra", IsAdvanced: true, AdvancedAt: &advanced, PaymentSource: "Salario"},
	}
	data := aggregateAnalysisYear(2026, []incomes.Income{{Year: 2026, Month: 10, Amount: 2000, Source: "Salario"}, {Year: 2026, Month: 10, Amount: 700, Source: "VA"}}, rows, map[uint]string{1: "Uber/99", 2: "Mercado"}, now)
	months := data["months_with_records"].([]map[string]any)
	if len(months) != 2 || months[0]["month"] != 10 || months[1]["month"] != 11 {
		t.Fatalf("wrong periods: %+v", months)
	}
	if months[0]["total_expense"] != 931.34 || months[0]["total_cash_expense"] != 431.34 || months[0]["total_cash_balance"] != 1568.66 {
		t.Fatalf("advanced entries or restricted benefits mixed: %+v", months[0])
	}
	if data["remaining_months_including_current"] != 3 || data["largest_installment_entry"].(map[string]any)["amount"] != float64(300) {
		t.Fatal("calendar or installment facts missing")
	}
	for _, category := range data["categories"].([]map[string]any) {
		if category["name"] == "Uber/99" && (category["highest_month"] != 10 || category["total_highest_month"] != 131.34 || category["total_amount"] != 244.7) {
			t.Fatalf("annual category ranking: %v", category)
		}
	}
}

func TestAnalysisValidatesDerivedMoneyAndRejectsUnbackedAmounts(t *testing.T) {
	facts := map[string]any{"years": []int{2026}, "total_october": 131.34, "total_november": 113.36}
	for _, test := range []struct {
		operation, reply string
		valid            bool
	}{
		{"sum", "Os meses somam R$ 244,70.", true},
		{"average", "A media e R$ 122,35.", true},
		{"subtract", "A diferenca e R$ 17,98.", true},
		{"sum", "Pode gastar R$ 999,00.", false},
		{"execute", "Os meses somam R$ 244,70.", false},
	} {
		intent := financialIntent{Intent: "financial_analysis", Reply: test.reply, facts: facts, provided: map[string]any{"calculations": []any{map[string]any{"operation": test.operation, "paths": []any{"/total_october", "/total_november"}}}}}
		_, reply, _, result := analysisReply(intent)
		if got := result.(map[string]any)["status"] == "read_only"; got != test.valid {
			t.Fatalf("%s valid=%v reply=%s", test.operation, got, reply)
		}
	}
	for _, path := range []string{"/years/0", "/absent", "/total_october/0", "/years/-1"} {
		if _, err := analysisNumberAt(map[string]any{"years": []any{float64(2026)}, "total_october": 131.34}, path); err == nil {
			t.Fatalf("non-money accepted: %s", path)
		}
	}
}

func TestExpenseDraftKeepsKnownFieldsWhenModelReturnsEmptyDefaults(t *testing.T) {
	draft := map[string]any{"description": "chocolate", "amount": float64(10), "payment_source": "Salario", "month": float64(10), "year": float64(2026), "day": float64(28), "type": "Unica"}
	intent, err := parseFinancialIntent(`{"intent":"create_expense","description":"","amount":0,"payment_source":"","day":0,"month":0,"year":0,"type":"","category_name":"Mercado","category_suggested":true}`)
	if err != nil {
		t.Fatal(err)
	}
	args := expenseArgsFromIntent(intent, draft)
	for key, value := range draft {
		if args[key] != value {
			t.Fatalf("draft field erased: %s: %v", key, args[key])
		}
	}
	if args["category_name"] != "Mercado" || args["category_suggested"] != true || args["confirm"] != false {
		t.Fatal("category proposal bypassed draft")
	}
	intent, _ = parseFinancialIntent(`{"intent":"create_expense","amount":0,"provided_fields":["amount"]}`)
	if expenseArgsFromIntent(intent, draft)["amount"] != float64(0) {
		t.Fatal("explicit invalid correction silently reused old amount")
	}
	intent, _ = parseFinancialIntent(`{"intent":"create_expense","description":"Outra compra","new_expense":true}`)
	if _, exists := expenseArgsFromIntent(intent, draft)["amount"]; exists {
		t.Fatal("new purchase inherited old amount")
	}
}

func TestCompactHistoryAndDraftPreserveReferencesWithoutRepeatingReports(t *testing.T) {
	history := []Message{{Role: "user", Content: "Quanto gastei de Uber?"}, {Role: "assistant", Content: strings.Repeat("Relatorio antigo ", 100), ToolCall: "get_category_expenses", ToolResult: `{"month":10,"year":2026,"category_name":"Uber/99","total_expense":999}`}}
	compact := compactFinancialHistory(history)
	if compact[0].Content != history[0].Content || !strings.Contains(compact[1].Content, "Uber/99") || !strings.Contains(compact[1].Content, "2026") || strings.Contains(compact[1].Content, "999") || compact[1].Content == history[1].Content {
		t.Fatal("lost reference or retained old financial data")
	}
	if !strings.Contains(history[1].Content, "Relatorio antigo") {
		t.Fatal("stored history mutated")
	}
	draft := map[string]any{"amount": 600, "installments": 3, "installment_amounts": []float64{200, 200, 200}, "category_name": "Mercado"}
	encoded := expenseDraftContext(draft)
	if strings.Contains(encoded, "installment_amounts") || !strings.Contains(encoded, "600") {
		t.Fatal("draft compression lost purchase or repeated derived list")
	}
}

func TestAnalysisYearWindowAndPromptCoversVideoFailures(t *testing.T) {
	now := time.Date(2026, 10, 28, 23, 0, 0, 0, time.UTC)
	if got := analysisYears("compare 2024 e 2025", nil, now); !reflect.DeepEqual(got, []int{2024, 2025}) {
		t.Fatal(got)
	}
	if got := analysisYears("e novembro?", []Message{{Role: "user", Content: "outubro de 2025"}}, now); !reflect.DeepEqual(got, []int{2025}) {
		t.Fatal(got)
	}
	prompt := financialInterpreterPrompt(now, "Mercado", nil)
	for _, required := range []string{"financial_analysis", "Janeiro pertence ao proximo ano", "category_suggested=true", "provided_fields", "nao cria metas/limites", "VA/VR", "pergunta atual prevalece"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("missing contract: %s", required)
		}
	}
	// Analysis is accepted as structured output, rather than rejected as an
	// unknown intent and wasting the user's single allowed inference.
	if _, err := parseFinancialIntent(`{"intent":"financial_analysis","reply":"Analise"}`); err != nil {
		t.Fatal(err)
	}
}

func TestMonthlyMoneyArraysValidateButMetadataDoesNotBecomeMoney(t *testing.T) {
	data, _ := json.Marshal(map[string]any{"monthly_amounts_jan_to_dec": []float64{131.34, 113.36}, "year": 2026})
	if !financialNumbersSupported("Outubro R$ 131,34; novembro R$ 113,36.", data) || financialNumbersSupported("Saldo R$ 2.026,00.", data) {
		t.Fatal("money provenance invalid")
	}
}

func TestVerifiedPercentageIsNotTreatedAsMoney(t *testing.T) {
	facts := map[string]any{"total_category": 769.71, "total_expense": 2931.87}
	raw := []any{map[string]any{"operation": "percentage", "paths": []any{"/total_category", "/total_expense"}}}
	calculated, err := verifiedAnalysisCalculations(facts, raw)
	if err != nil || calculated[0]["percentage"] != 26.25 {
		t.Fatalf("wrong percentage: %v %v", calculated, err)
	}
	encoded, _ := json.Marshal(map[string]any{"facts": facts, "calculated": calculated})
	if !financialNumbersSupported("Representa 26,25%.", encoded) || financialNumbersSupported("Pode gastar R$ 26,25.", encoded) {
		t.Fatal("percentage was confused with available money")
	}
}

func TestRegisteredNumericCategoryIsNotConfusedWithMoney(t *testing.T) {
	data := []byte(`{"name":"Uber/99","total_amount":131.34}`)
	for _, test := range []struct {
		reply string
		valid bool
	}{
		{"Com Uber/99, voce gastou R$ 131,34.", true},
		{"Com Uber/99, voce gastou R$ 99,00.", false},
		{"Com Uber/99, voce gastou 99%.", false},
		{"Com Uber/88, voce gastou R$ 131,34.", false},
	} {
		if financialNumbersSupported(test.reply, data) != test.valid {
			t.Fatalf("numeric category validation: %s", test.reply)
		}
	}
}
