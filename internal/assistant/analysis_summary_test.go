package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func analysisSummaryTestFacts() map[string]any {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.FixedZone("Brasilia", -10800))
	period := aggregateAnalysisYear(2026, []incomes.Income{
		{Month: 10, Year: 2026, Amount: 2000, Source: "Salario"},
		{Month: 11, Year: 2026, Amount: 2000, Source: "Salario"},
	}, []expenses.Expense{
		{CategoryID: 1, Amount: 80.50, Month: 10, Year: 2026, PaymentSource: "Salario"},
		{CategoryID: 1, Amount: 230, Month: 11, Year: 2026, PaymentSource: "Salario"},
		{CategoryID: 2, Amount: 1000, Month: 10, Year: 2026, PaymentSource: "Salario"},
		{CategoryID: 3, Amount: 150, Month: 10, Year: 2026, PaymentSource: "Salario"},
	}, map[uint]string{1: "Uber/99", 2: "Mercado", 3: "Delivery"}, now)
	return map[string]any{"as_of": "2026-10-29", "years": []int{2026}, "periods": []map[string]any{period}}
}

func decodedAnalysisSummaryFacts(t *testing.T) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(analysisSummaryTestFacts())
	if err != nil {
		t.Fatal(err)
	}
	var facts map[string]any
	if err := json.Unmarshal(encoded, &facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

func summaryTestPeriod(facts map[string]any) map[string]any {
	return facts["periods"].([]any)[0].(map[string]any)
}

func TestVerifiedAnalysisSummaryUsesOnlyRegisteredAnnualFacts(t *testing.T) {
	for _, facts := range []map[string]any{analysisSummaryTestFacts(), decodedAnalysisSummaryFacts(t)} {
		before, _ := json.Marshal(facts)
		reply, ok := verifiedAnalysisSummary(facts)
		if !ok {
			t.Fatal("valid authenticated annual snapshot was rejected")
		}
		for _, required := range []string{"2026", "janeiro a dezembro", "R$ 4.000,00", "R$ 1.460,50", "Mercado", "R$ 1.000,00", "Uber/99", "R$ 310,50", "Delivery", "R$ 150,00", "inclusive futuros", "nao prova de desperdicio", "essencial", "Ausencia de registros nao prova ausencia de gastos"} {
			if !strings.Contains(reply, required) {
				t.Fatalf("summary lost verified scope, amount or caveat %q: %s", required, reply)
			}
		}
		for _, unsafe := range []string{"%", "36,5", "68,5", "discricionaria", "excessivo", "ja gastou", "disponivel", "pode gastar"} {
			if strings.Contains(strings.ToLower(reply), unsafe) {
				t.Fatalf("summary introduced unverified advice or percentages %q", unsafe)
			}
		}
		after, _ := json.Marshal(facts)
		if string(before) != string(after) {
			t.Fatal("summary changed the authenticated facts")
		}
	}
}

func TestVerifiedAnalysisSummarySortsYearsAndLimitsEscapedCategoryRanking(t *testing.T) {
	facts := decodedAnalysisSummaryFacts(t)
	period := summaryTestPeriod(facts)
	categories := period["categories"].([]any)
	categories[0].(map[string]any)["name"] = "**Mercado**\n[abrir](x)"
	// Divide the existing Delivery category without changing registered totals.
	category := categories[2].(map[string]any)
	category["total_amount"] = float64(100)
	category["monthly_amounts_jan_to_dec"].([]any)[9] = float64(100)
	months := make([]any, 12)
	for i := range months {
		months[i] = float64(0)
	}
	months[9] = float64(50)
	period["categories"] = append(categories, map[string]any{"name": "Fourth category", "total_amount": float64(50), "monthly_amounts_jan_to_dec": months})
	empty := aggregateAnalysisYear(2025, nil, nil, nil, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
	facts["years"] = []int{2026, 2025}
	facts["periods"] = []any{period, empty}
	reply, ok := verifiedAnalysisSummary(facts)
	if !ok || strings.Index(reply, "Ano de 2025") >= strings.Index(reply, "Ano de 2026") {
		t.Fatalf("bounded annual scopes were dropped or misordered: %s", reply)
	}
	if !strings.Contains(reply, `\*\*Mercado\*\* \[abrir\]\(x\)`) || strings.Contains(reply, "Fourth category") || strings.Count(reply, "- **") != 3 {
		t.Fatalf("category presentation escaped incorrectly or exceeded three: %s", reply)
	}
}

func TestVerifiedAnalysisSummaryOmitsRankingWhenCoverageIsExplicitlyPartial(t *testing.T) {
	facts := decodedAnalysisSummaryFacts(t)
	period := summaryTestPeriod(facts)
	period["categories_complete"] = false
	period["categories"] = period["categories"].([]any)[:1]
	reply, ok := verifiedAnalysisSummary(facts)
	if !ok || !strings.Contains(reply, "R$ 1.460,50") || !strings.Contains(reply, "cobertura das categorias e parcial") || strings.Contains(reply, "Mercado") || strings.Contains(reply, "maiores despesas") {
		t.Fatalf("partial category coverage became a complete ranking: %s", reply)
	}
}

func TestVerifiedAnalysisSummaryRejectsIncompleteOrInconsistentFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing years", func(f map[string]any) { delete(f, "years") }},
		{"missing periods", func(f map[string]any) { delete(f, "periods") }},
		{"duplicate years", func(f map[string]any) { f["years"] = []int{2026, 2026} }},
		{"too many years", func(f map[string]any) { f["years"] = []int{2024, 2025, 2026} }},
		{"different year", func(f map[string]any) { summaryTestPeriod(f)["year"] = 2025 }},
		{"duplicate periods", func(f map[string]any) {
			f["years"] = []int{2025, 2026}
			f["periods"] = []any{summaryTestPeriod(f), summaryTestPeriod(f)}
		}},
		{"fractional year", func(f map[string]any) { summaryTestPeriod(f)["year"] = 2026.5 }},
		{"missing income", func(f map[string]any) { delete(summaryTestPeriod(f), "total_income") }},
		{"null expense", func(f map[string]any) { summaryTestPeriod(f)["total_expense"] = nil }},
		{"negative income", func(f map[string]any) { summaryTestPeriod(f)["total_income"] = -1 }},
		{"negative expense", func(f map[string]any) { summaryTestPeriod(f)["total_expense"] = -1 }},
		{"not finite", func(f map[string]any) { summaryTestPeriod(f)["total_expense"] = math.NaN() }},
		{"infinite", func(f map[string]any) { summaryTestPeriod(f)["total_income"] = math.Inf(1) }},
		{"fractional cents", func(f map[string]any) { summaryTestPeriod(f)["total_income"] = 4000.001 }},
		{"income sum mismatch", func(f map[string]any) { summaryTestPeriod(f)["total_income"] = 4001 }},
		{"expense sum mismatch", func(f map[string]any) { summaryTestPeriod(f)["total_expense"] = 1461 }},
		{"missing months", func(f map[string]any) { delete(summaryTestPeriod(f), "months_with_records") }},
		{"duplicate months", func(f map[string]any) {
			p := summaryTestPeriod(f)
			m := p["months_with_records"].([]any)
			p["months_with_records"] = append(m, m[0])
		}},
		{"missing categories", func(f map[string]any) { delete(summaryTestPeriod(f), "categories") }},
		{"missing coverage", func(f map[string]any) { delete(summaryTestPeriod(f), "categories_complete") }},
		{"incomplete categories declared complete", func(f map[string]any) { p := summaryTestPeriod(f); p["categories"] = p["categories"].([]any)[:1] }},
		{"category total mismatch", func(f map[string]any) {
			summaryTestPeriod(f)["categories"].([]any)[0].(map[string]any)["total_amount"] = 1001
		}},
		{"category lacks full calendar", func(f map[string]any) {
			c := summaryTestPeriod(f)["categories"].([]any)[0].(map[string]any)
			c["monthly_amounts_jan_to_dec"] = []float64{1000}
		}},
		{"null category amount", func(f map[string]any) {
			summaryTestPeriod(f)["categories"].([]any)[0].(map[string]any)["monthly_amounts_jan_to_dec"].([]any)[9] = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := decodedAnalysisSummaryFacts(t)
			test.mutate(facts)
			if reply, ok := verifiedAnalysisSummary(facts); ok || reply != "" {
				t.Fatalf("unsafe facts produced a partial or invented summary: %q", reply)
			}
		})
	}
	if reply, ok := verifiedAnalysisSummary(nil); ok || reply != "" {
		t.Fatal("absent facts produced a summary")
	}
}
