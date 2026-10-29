package assistant

import (
	"strings"
	"testing"
	"time"
)

func TestRankingPositionSelectsTheOrdinalRatherThanTheFirstN(t *testing.T) {
	rows := []financialReadRow{
		{Label: "Compra A", Amount: 95, Registered: true, RegisteredCount: 1},
		{Label: "Compra B", Amount: 62.50, Registered: true, RegisteredCount: 1},
		{Label: "Compra C", Amount: 40, Registered: true, RegisteredCount: 1},
	}
	for _, test := range []struct {
		operation string
		position  int
		want      string
	}{
		{"max", 1, "Compra A"}, {"max", 2, "Compra B"}, {"min", 2, "Compra B"},
		{"max", 4, ""},
	} {
		query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2032, Months: []int{10}}}, GroupBy: "installment", Operation: test.operation, Position: test.position, Limit: 1}
		shown, total, matched := finalizeFinancialReadRows(query, rows)
		if total != 197.50 || matched != 3 {
			t.Fatalf("ordinal changed the complete scope: total=%v matched=%v", total, matched)
		}
		if test.want == "" {
			if len(shown) != 0 || !strings.Contains(financialQueryReply(query, shown, total, matched, true), "posicao 4") {
				t.Fatal("missing ordinal was replaced with another item or no-records claim")
			}
		} else if len(shown) != 1 || shown[0].Label != test.want {
			t.Fatalf("ordinal returned the first N rather than the requested item: %+v", shown)
		}
	}
	query := financialReadQuery{Operation: "max", Limit: 2}
	shown, _, _ := finalizeFinancialReadRows(query, rows)
	if len(shown) != 2 || shown[0].Label != "Compra A" || shown[1].Label != "Compra B" {
		t.Fatal("ordinary top-two list changed while adding ordinal selection")
	}
}

func TestQueryPositionAndGuidanceHaveUnambiguousShapes(t *testing.T) {
	for _, body := range []string{
		`{"periods":[{"year":2032,"months":[10]}],"group_by":"installment","operation":"max","position":2}`,
		`{"periods":[{"year":2032,"months":[10]}],"group_by":"installment","operation":"max","position":2,"limit":2}`,
		`{"periods":[{"year":2032,"months":[]}],"group_by":"category","operation":"review"}`,
		`{"periods":[{"year":2032,"months":[]}],"category_name":"Uber/99","group_by":"month","operation":"budget"}`,
	} {
		if _, err := parseFinancialReadQuery(financialQueryTestRaw(t, body)); err != nil {
			t.Fatalf("valid query rejected: %s: %v", body, err)
		}
	}
	for _, body := range []string{
		`{"periods":[{"year":2032,"months":[10]}],"group_by":"installment","operation":"max","position":0}`,
		`{"periods":[{"year":2032,"months":[10]}],"group_by":"installment","operation":"max","position":25}`,
		`{"periods":[{"year":2032,"months":[10]}],"group_by":"month","operation":"sum","position":2}`,
		`{"periods":[{"year":2032,"months":[]}],"group_by":"month","operation":"review"}`,
		`{"periods":[{"year":2032,"months":[]}],"group_by":"category","operation":"budget"}`,
		`{"periods":[{"year":2032,"months":[]}],"group_by":"month","operation":"budget"}`,
	} {
		if _, err := parseFinancialReadQuery(financialQueryTestRaw(t, body)); err == nil {
			t.Fatalf("ambiguous/invalid query accepted: %s", body)
		}
	}
}

func TestInterpreterUsesScopeWithoutSendingTheAnnualLedger(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	facts := map[string]any{"private_financial_sentinel": "HIDDEN_ANNUAL_DATA_826", "total_income": 9876.54}
	prompt := financialInterpreterPrompt(now, "Uber/99, Mercado", facts)
	for _, private := range []string{"HIDDEN_ANNUAL_DATA_826", "9876.54", "monthly_amounts_jan_to_dec", "FATOS ATUAIS"} {
		if strings.Contains(prompt, private) {
			t.Fatalf("financial ledger entered the scope interpreter: %s", private)
		}
	}
	for _, scope := range []string{"Uber/99", "2026-10-29", "operation=budget", "position=2", "month+max"} {
		if !strings.Contains(prompt, scope) {
			t.Fatalf("necessary planning context absent: %s", scope)
		}
	}
}
