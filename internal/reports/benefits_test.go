package reports

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"testing"
)

func TestBenefitSummaryAndExports(t *testing.T) {
	s := buildMonthlyExportSummary([]incomes.Income{{Source: "VA", Amount: 600}, {Source: "VR", Amount: 400}}, []expenses.Expense{{Amount: 150, PaymentSplits: []expenses.PaymentSplit{{PaymentSource: "VA", Amount: 100}, {PaymentSource: "VR", Amount: 50}}}})
	if s.RemainingVA != 500 || s.RemainingVR != 350 || s.TotalExpense != 150 || s.Balance != 850 {
		t.Fatalf("wrong benefit summary: %+v", s)
	}
	rows := summaryRows(s)
	if len(rows) != 18 || rows[16][1] != formatDecimal(500) || rows[17][1] != formatDecimal(350) {
		t.Fatalf("missing CSV benefits: %v", rows)
	}
	if len(xlsxSummarySection(s).Rows) != 18 {
		t.Fatal("missing XLSX benefits")
	}
	comparison := buildPaymentSourceComparisons([]expenses.Expense{{Amount: 100, PaymentSource: "VA"}}, nil)
	if len(comparison) != 1 || comparison[0].PaymentSource != "VA" {
		t.Fatalf("wrong comparison: %+v", comparison)
	}
}
