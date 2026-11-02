package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"strings"
	"testing"
)

func TestSavingsUsesAllCashCommitmentsAndExcludesBenefits(t *testing.T) {
	q := financialReadQuery{Operation: "savings", GroupBy: "month", Periods: []financialReadPeriod{{Year: 2032, Months: []int{2, 3, 4}}}}
	income := []incomes.Income{{UserID: 41, Year: 2032, Month: 2, Source: "Salario", Amount: 2000}, {UserID: 41, Year: 2032, Month: 2, Source: "VA", Amount: 900}, {UserID: 99, Year: 2032, Month: 2, Source: "Salario", Amount: 99999}, {UserID: 41, Year: 2032, Month: 3, Source: "Salario", Amount: 100}}
	rows := []expenses.Expense{guidanceTestExpense(1, 600, 2), guidanceTestExpense(2, 800, 2), guidanceTestExpense(2, 150, 3)}
	rows[1].PaymentSplits = []expenses.PaymentSplit{{PaymentSource: "Salario", Amount: 500}, {PaymentSource: "VA", Amount: 300}}
	reply, result, err := renderSavingsPlan(q, 41, rows, income, guidanceTestNow())
	if err != nil {
		t.Fatal(err)
	}
	got := result["months"].([]map[string]any)
	if got[0]["potential_savings"] != float64(900) || got[1]["potential_savings"] != float64(0) || got[1]["registered_cash_balance"] != float64(-50) || got[2]["potential_savings"] != nil {
		t.Fatalf("incorrect cash capacity: %+v", got)
	}
	if !strings.Contains(reply, "R$ 900,00") || strings.Contains(reply, "media") || !strings.Contains(reply, "falta renda") {
		t.Fatal(reply)
	}
	q.CategoryName = "Dividas"
	if _, _, err := renderSavingsPlan(q, 41, rows, income, guidanceTestNow()); err == nil {
		t.Fatal("category debt average accepted as capacity")
	}
}

func TestSavingsProtocolRejectsResidualCategory(t *testing.T) {
	raw := map[string]any{"periods": []any{map[string]any{"year": 2032, "months": []int{2}}}, "operation": "savings", "group_by": "month", "category_name": "", "payment_source": ""}
	if _, err := parseFinancialReadQuery(raw); err != nil {
		t.Fatal(err)
	}
	raw["category_name"] = "Dividas"
	if _, err := parseFinancialReadQuery(raw); err == nil {
		t.Fatal("residual category silently accepted")
	}
}
