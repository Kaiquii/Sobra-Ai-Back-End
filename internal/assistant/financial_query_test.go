package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func financialQueryTestRaw(t *testing.T, encoded string) any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(encoded), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFinancialReadQueryRejectsInvalidOrUnexpectedStructure(t *testing.T) {
	for _, test := range []struct {
		name, encoded string
	}{
		{"missing query", `null`},
		{"wrong container", `[]`},
		{"no periods", `{"group_by":"month","operation":"list"}`},
		{"empty periods", `{"periods":[],"group_by":"month","operation":"list"}`},
		{"year below range", `{"periods":[{"year":1999}],"group_by":"month","operation":"list"}`},
		{"year above range", `{"periods":[{"year":10000}],"group_by":"month","operation":"list"}`},
		{"fractional year", `{"periods":[{"year":2026.5}],"group_by":"month","operation":"list"}`},
		{"string year", `{"periods":[{"year":"2026"}],"group_by":"month","operation":"list"}`},
		{"third year", `{"periods":[{"year":2024},{"year":2025},{"year":2026}],"group_by":"month","operation":"list"}`},
		{"month zero", `{"periods":[{"year":2026,"months":[0]}],"group_by":"month","operation":"list"}`},
		{"month thirteen", `{"periods":[{"year":2026,"months":[13]}],"group_by":"month","operation":"list"}`},
		{"fractional month", `{"periods":[{"year":2026,"months":[10.5]}],"group_by":"month","operation":"list"}`},
		{"string month", `{"periods":[{"year":2026,"months":["10"]}],"group_by":"month","operation":"list"}`},
		{"null months", `{"periods":[{"year":2026,"months":null}],"group_by":"month","operation":"list"}`},
		{"unknown grouping", `{"periods":[{"year":2026}],"group_by":"user","operation":"list"}`},
		{"write operation", `{"periods":[{"year":2026}],"group_by":"expense","operation":"delete"}`},
		{"zero limit", `{"periods":[{"year":2026}],"group_by":"expense","operation":"list","limit":0}`},
		{"excessive limit", `{"periods":[{"year":2026}],"group_by":"expense","operation":"list","limit":25}`},
		{"fractional limit", `{"periods":[{"year":2026}],"group_by":"expense","operation":"list","limit":1.5}`},
		{"unknown source", `{"periods":[{"year":2026}],"group_by":"month","operation":"list","payment_source":"Banco"}`},
		{"unexpected authority", `{"periods":[{"year":2026}],"group_by":"month","operation":"list","user_id":2}`},
		{"unexpected financial input", `{"periods":[{"year":2026}],"group_by":"month","operation":"list","amount":999}`},
		{"unexpected period input", `{"periods":[{"year":2026,"sql":"select *"}],"group_by":"month","operation":"list"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseFinancialReadQuery(financialQueryTestRaw(t, test.encoded)); err == nil {
				t.Fatal("invalid query was accepted")
			}
		})
	}
}

func TestFinancialReadQueryNormalizesPeriodsAndSafeDefaults(t *testing.T) {
	query, err := parseFinancialReadQuery(financialQueryTestRaw(t, `{"periods":[{"year":2027,"months":[2,1,2]},{"year":2026,"months":[11,10,11]},{"year":2026,"months":[12]}],"group_by":"month","operation":"compare","category_name":"Uber/99"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []financialReadPeriod{{Year: 2026, Months: []int{10, 11, 12}}, {Year: 2027, Months: []int{1, 2}}}
	if !reflect.DeepEqual(query.Periods, want) || query.Limit != 24 || query.CategoryName != "Uber/99" {
		t.Fatalf("query was not normalized: %+v", query)
	}
	for _, operation := range []string{"list", "compare", "sum", "average", "max", "min"} {
		t.Run(operation, func(t *testing.T) {
			query, err := parseFinancialReadQuery(financialQueryTestRaw(t, `{"periods":[{"year":2026,"months":[]}],"group_by":"month","operation":"`+operation+`"}`))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(query.Periods[0].Months, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}) {
				t.Fatalf("empty months did not select the complete year: %+v", query.Periods)
			}
			if (operation == "max" || operation == "min") && query.Limit != 1 {
				t.Fatalf("extreme ranking default must be one: %+v", query)
			}
		})
	}
}

func TestFinancialReadMonthsKeepCategoryScopeTenantAndUnregisteredPeriods(t *testing.T) {
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2026, Months: []int{10, 11, 12}}}, GroupBy: "month", Operation: "compare", CategoryName: "Uber/99", Limit: 24}
	rows := []expenses.Expense{
		{ID: 1, UserID: 7, CategoryID: 11, Amount: 80.50, Month: 10, Year: 2026, Description: "Uber outubro"},
		{ID: 2, UserID: 7, CategoryID: 11, Amount: 230, Month: 11, Year: 2026, Description: "Uber novembro"},
		{ID: 3, UserID: 7, CategoryID: 12, Amount: 900, Month: 10, Year: 2026},
		{ID: 4, UserID: 8, CategoryID: 11, Amount: 99999, Month: 10, Year: 2026},
		{ID: 5, UserID: 7, CategoryID: 11, Amount: 700, Month: 9, Year: 2026},
	}
	got, err := aggregateFinancialReadQuery(query, 7, 11, rows, nil, map[uint]string{11: "Uber/99", 12: "Mercado"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("all requested months must be present: %+v", got)
	}
	for index, want := range []struct {
		month      int
		amount     float64
		registered bool
	}{{10, 80.50, true}, {11, 230, true}, {12, 0, false}} {
		if got[index].Year != 2026 || got[index].Month != want.month || got[index].Amount != want.amount || got[index].Registered != want.registered {
			t.Fatalf("wrong monthly comparison at %d: %+v", index, got)
		}
	}
}

func TestFinancialReadUsesEffectivePeriodInsteadOfScheduledInstallment(t *testing.T) {
	advancedAt := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	rows := []expenses.Expense{
		{ID: 1, UserID: 7, CategoryID: 11, Amount: 80.50, Month: 1, Year: 2027, Type: "Parcelada", IsAdvanced: true, AdvancedAt: &advancedAt},
		{ID: 2, UserID: 7, CategoryID: 11, Amount: 230, Month: 10, Year: 2026},
	}
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2026, Months: []int{10, 11}}}, GroupBy: "month", Operation: "list", Limit: 24}
	got, err := aggregateFinancialReadQuery(query, 7, 0, rows, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Month != 10 || got[0].Amount != 230 || got[1].Month != 11 || got[1].Amount != 80.50 {
		t.Fatalf("advanced installment was attributed to its scheduled month: %+v", got)
	}
	query.Periods = []financialReadPeriod{{Year: 2027, Months: []int{1}}}
	got, err = aggregateFinancialReadQuery(query, 7, 0, rows, nil, nil)
	if err != nil || len(got) != 1 || got[0].Amount != 0 || got[0].Registered {
		t.Fatalf("advanced installment was counted twice: %+v, err=%v", got, err)
	}
}

func TestFinancialReadSourceFiltersUseOnlyMatchingPaymentSplit(t *testing.T) {
	rows := []expenses.Expense{
		{ID: 1, UserID: 7, CategoryID: 11, Amount: 300, Month: 10, Year: 2026, PaymentSplits: []expenses.PaymentSplit{{PaymentSource: "VA", Amount: 120}, {PaymentSource: "VR", Amount: 80}, {PaymentSource: "Salario", Amount: 100}}},
		{ID: 2, UserID: 7, CategoryID: 11, Amount: 60, Month: 10, Year: 2026, PaymentSource: "Salario"},
		{ID: 3, UserID: 8, CategoryID: 11, Amount: 999, Month: 10, Year: 2026, PaymentSource: "VA"},
	}
	for _, test := range []struct {
		source string
		amount float64
	}{{"VA", 120}, {"VR", 80}, {"Salario", 160}, {"", 360}} {
		t.Run(test.source, func(t *testing.T) {
			query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2026, Months: []int{10}}}, GroupBy: "month", Operation: "sum", PaymentSource: test.source, Limit: 24}
			got, err := aggregateFinancialReadQuery(query, 7, 0, rows, nil, nil)
			if err != nil || len(got) != 1 || got[0].Amount != test.amount {
				t.Fatalf("source total used the full expense or another tenant: %+v err=%v", got, err)
			}
		})
	}
	if rows[0].Amount != 300 || rows[0].PaymentSplits[0].Amount != 120 {
		t.Fatal("query mutated the source data")
	}
}

func TestFinancialReadCategoryAndExpenseRankingKeepAllMatchingRows(t *testing.T) {
	rows := []expenses.Expense{
		{ID: 3, UserID: 7, CategoryID: 11, Amount: 80.50, Month: 10, Year: 2026, Description: "Z transporte"},
		{ID: 2, UserID: 7, CategoryID: 11, Amount: 230, Month: 11, Year: 2026, Description: "A transporte"},
		{ID: 1, UserID: 7, CategoryID: 12, Amount: 900, Month: 10, Year: 2026, Description: "Mercado"},
		{ID: 4, UserID: 8, CategoryID: 12, Amount: 5000, Month: 10, Year: 2026},
	}
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2026, Months: []int{10, 11}}}, GroupBy: "category", Operation: "max", Limit: 1}
	got, err := aggregateFinancialReadQuery(query, 7, 0, rows, nil, map[uint]string{11: "Uber/99", 12: "Mercado"})
	if err != nil || len(got) != 2 || got[0].Label != "Mercado" || got[0].Amount != 900 || got[1].Label != "Uber/99" || got[1].Amount != 310.50 {
		t.Fatalf("category ranking was truncated before finalization or lost scope: %+v err=%v", got, err)
	}
	query.GroupBy = "expense"
	got, err = aggregateFinancialReadQuery(query, 7, 0, rows, nil, nil)
	if err != nil || len(got) != 3 || got[0].ExpenseID != 1 || got[1].ExpenseID != 2 || got[2].ExpenseID != 3 {
		t.Fatalf("expense ranking must retain the full matching set: %+v err=%v", got, err)
	}
}

func TestFinancialReadPurchaseRanksRegisteredSeriesInsteadOfSingleInstallment(t *testing.T) {
	first := expenses.Expense{ID: 1, UserID: 7, CategoryID: 11, Amount: 100, Month: 10, Year: 2026, Type: "Parcelada", Description: "Compra A", SeriesID: "series-a"}
	second := expenses.Expense{ID: 2, UserID: 7, CategoryID: 11, Amount: 230, Month: 10, Year: 2026, Type: "Parcelada", Description: "Compra B", SeriesID: "series-b"}
	rows := []expenses.Expense{first, second, {ID: 3, UserID: 7, CategoryID: 11, Amount: 800, Month: 10, Year: 2026, Type: "Unica", Description: "Avulsa"}}
	purchases := []expenses.Expense{
		first, second,
		{ID: 4, UserID: 7, CategoryID: 11, Amount: 100, Month: 11, Year: 2026, Type: "Parcelada", Description: "Compra A", SeriesID: "series-a"},
		{ID: 5, UserID: 7, CategoryID: 11, Amount: 200, Month: 12, Year: 2025, Type: "Parcelada", Description: "Compra A", SeriesID: "series-a"},
		{ID: 6, UserID: 8, CategoryID: 11, Amount: 99999, Month: 12, Year: 2025, Type: "Parcelada", SeriesID: "series-a"},
		{ID: 7, UserID: 7, CategoryID: 11, Amount: 700, Month: 10, Year: 2026, Type: "Unica", SeriesID: "series-a"},
		{ID: 8, UserID: 7, CategoryID: 11, Amount: 5000, Month: 1, Year: 2025, Type: "Parcelada", SeriesID: "unselected-series"},
	}
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2026, Months: []int{10}}}, GroupBy: "installment", Operation: "max", Limit: 1}
	got, err := aggregateFinancialReadQuery(query, 7, 11, rows, purchases, nil)
	if err != nil || len(got) != 2 || got[0].ExpenseID != second.ID || got[0].Amount != 230 {
		t.Fatalf("installment ranking included unique expenses or grouped purchases: %+v err=%v", got, err)
	}
	query.GroupBy = "purchase"
	got, err = aggregateFinancialReadQuery(query, 7, 11, rows, purchases, nil)
	if err != nil || len(got) != 2 || got[0].SeriesID != "series-a" || got[0].Amount != 400 || got[0].RegisteredCount != 3 || got[1].SeriesID != "series-b" || got[1].Amount != 230 {
		t.Fatalf("purchase ranking lost registered installments or included unrelated rows: %+v err=%v", got, err)
	}
	legacy := expenses.Expense{ID: 9, UserID: 7, CategoryID: 11, Amount: 100, Month: 10, Year: 2026, Type: "Parcelada"}
	if _, err := aggregateFinancialReadQuery(query, 7, 11, []expenses.Expense{legacy}, []expenses.Expense{legacy}, nil); err == nil {
		t.Fatal("legacy installments without a series were guessed into a purchase")
	}
}

func TestFinancialReadFinalizationDoesNotHideRequestedComparisonMonths(t *testing.T) {
	rows := []financialReadRow{{Label: "outubro", Year: 2026, Month: 10, Amount: 80.50, Registered: true}, {Label: "novembro", Year: 2026, Month: 11, Amount: 230, Registered: true}, {Label: "dezembro", Year: 2026, Month: 12, Amount: 0}}
	query := financialReadQuery{GroupBy: "month", Operation: "compare", Limit: 1}
	shown, total, matched := finalizeFinancialReadRows(query, rows)
	if len(shown) != 3 || total != 310.50 || matched != 3 || !reflect.DeepEqual(shown, rows) {
		t.Fatalf("comparison was silently truncated: %+v total=%v matched=%d", shown, total, matched)
	}
	query.Operation = "list"
	shown, total, matched = finalizeFinancialReadRows(query, rows)
	if len(shown) != 1 || total != 310.50 || matched != 3 {
		t.Fatalf("list limit changed aggregate facts: %+v total=%v matched=%d", shown, total, matched)
	}
}

func TestFinancialReadFinalizationComputesTotalsAndAveragesBeforePresentationLimit(t *testing.T) {
	rows := []financialReadRow{{Label: "outubro", Amount: 100, Registered: true, RegisteredCount: 2}, {Label: "novembro"}, {Label: "dezembro"}}
	for _, test := range []struct {
		operation string
		amount    float64
	}{{"sum", 100}, {"average", 33.33}} {
		t.Run(test.operation, func(t *testing.T) {
			query := financialReadQuery{GroupBy: "month", Operation: test.operation, Limit: 1}
			shown, total, matched := finalizeFinancialReadRows(query, rows)
			if len(shown) != 1 || shown[0].Amount != test.amount || !shown[0].Registered || shown[0].RegisteredCount != 2 || total != 100 || matched != 3 {
				t.Fatalf("aggregate was based on presentation limit or omitted requested zero months: %+v total=%v matched=%d", shown, total, matched)
			}
			shown, total, matched = finalizeFinancialReadRows(query, nil)
			if len(shown) != 1 || shown[0].Amount != 0 || shown[0].Registered || shown[0].RegisteredCount != 0 || total != 0 || matched != 0 {
				t.Fatalf("empty aggregate must remain a finite unregistered zero: %+v total=%v matched=%d", shown, total, matched)
			}
			missingMonths := []financialReadRow{{Label: "outubro"}, {Label: "novembro"}, {Label: "dezembro"}}
			shown, total, matched = finalizeFinancialReadRows(query, missingMonths)
			if len(shown) != 1 || shown[0].Amount != 0 || shown[0].Registered || shown[0].RegisteredCount != 0 || total != 0 || matched != 3 {
				t.Fatalf("requested zero months were mistaken for registered transactions: %+v total=%v matched=%d", shown, total, matched)
			}
		})
	}
}

func TestFinancialReadExtremesExcludeMissingMonthsAndHaveStableTies(t *testing.T) {
	rows := []financialReadRow{
		{Label: "C", Amount: 100, Year: 2026, Month: 11, ExpenseID: 4, Registered: true},
		{Label: "A", Amount: 100, Year: 2026, Month: 10, ExpenseID: 3, Registered: true},
		{Label: "A", Amount: 100, Year: 2026, Month: 10, ExpenseID: 2, Registered: true},
		{Label: "B", Amount: 100, Year: 2026, Month: 10, ExpenseID: 1, Registered: true},
		{Label: "Futuro", Amount: 50, Year: 2027, Month: 1, ExpenseID: 5, Registered: true},
		{Label: "Sem lancamentos", Year: 2026, Month: 9},
	}
	before := append([]financialReadRow(nil), rows...)
	for _, test := range []struct {
		operation string
		wantIDs   []uint
	}{{"max", []uint{2, 3, 1}}, {"min", []uint{5, 2, 3}}} {
		t.Run(test.operation, func(t *testing.T) {
			query := financialReadQuery{GroupBy: "expense", Operation: test.operation, Limit: 3}
			shown, total, matched := finalizeFinancialReadRows(query, rows)
			var ids []uint
			for _, row := range shown {
				ids = append(ids, row.ExpenseID)
			}
			if !reflect.DeepEqual(ids, test.wantIDs) || total != 450 || matched != 5 {
				t.Fatalf("wrong extreme ranking: ids=%v total=%v matched=%d", ids, total, matched)
			}
		})
	}
	if !reflect.DeepEqual(rows, before) {
		t.Fatal("ranking mutated the original comparison rows")
	}
}

func TestFinancialQueryAmbiguityAsksBeforeAnyDatabaseAccess(t *testing.T) {
	raw := financialQueryTestRaw(t, `{"periods":[{"year":2026,"months":[10]}],"group_by":"clarify_installment_or_purchase","operation":"max"}`)
	handled, reply, toolCall, rawResult := executeFinancialQueryDB(context.Background(), nil, 7, raw, time.Date(2026, 10, 29, 12, 0, 0, 0, time.Local))
	result, ok := rawResult.(map[string]any)
	if !handled || !ok || toolCall != "financial_analysis" || result["status"] != "needs_clarification" || result["reason"] != "installment_or_purchase" || !strings.Contains(reply, "parcela") || !strings.Contains(reply, "total") {
		t.Fatalf("ambiguous request accessed the database or guessed purchase semantics: reply=%q result=%v", reply, rawResult)
	}
}

func TestFinancialQueryLabelEscapesFormattingAndCollapsesControlWhitespace(t *testing.T) {
	input := " **Reserva**\n[abrir](x)\r\n\u00a0<script>\t"
	want := `\*\*Reserva\*\* \[abrir\]\(x\) \<script\>`
	if got := financialQueryLabel(input); got != want {
		t.Fatalf("registered label could inject presentation markup: got %q want %q", got, want)
	}
}
