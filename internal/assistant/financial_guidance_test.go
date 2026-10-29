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

func guidanceTestNow() time.Time {
	return time.Date(2032, 6, 15, 12, 0, 0, 0, time.FixedZone("Brasilia", -10800))
}

func guidanceTestExpense(category uint, amount float64, month int) expenses.Expense {
	return expenses.Expense{UserID: 41, CategoryID: category, Amount: amount, Month: month, Year: 2032, PaymentSource: "Salario"}
}

func TestSpendingReviewCalculatesScopedRankingAndSeparatesBenefits(t *testing.T) {
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2032, Months: []int{2, 7}}}, GroupBy: "category", Operation: "review", Limit: 24}
	names := map[uint]string{1: "Moradia", 2: "Servicos", 3: "Saude", 4: "Lazer", 5: "Estudos", 6: "Pets", 7: "Mobilidade", 9: "Categoria externa"}
	rows := []expenses.Expense{
		guidanceTestExpense(1, 450.25, 2), guidanceTestExpense(2, 129.75, 2), guidanceTestExpense(3, 100, 2),
		guidanceTestExpense(4, 80, 2), guidanceTestExpense(5, 50, 2), guidanceTestExpense(6, 40, 2),
		guidanceTestExpense(7, 30, 2), guidanceTestExpense(6, 20, 7), guidanceTestExpense(1, 500, 1),
		{UserID: 99, CategoryID: 9, Amount: 8877, Month: 2, Year: 2032, PaymentSource: "Salario"},
	}
	rows[1].PaymentSplits = []expenses.PaymentSplit{{PaymentSource: "Salario", Amount: 29.75}, {PaymentSource: "VA", Amount: 100}}
	rows[3].PaymentSource = "VR"
	income := []incomes.Income{
		{UserID: 41, Source: "Salario", Amount: 1000, Month: 2, Year: 2032},
		{UserID: 41, Source: "VA", Amount: 300, Month: 2, Year: 2032},
		{UserID: 41, Source: "VR", Amount: 200, Month: 2, Year: 2032},
		{UserID: 99, Source: "Salario", Amount: 8877, Month: 2, Year: 2032},
	}
	before, _ := json.Marshal([]any{rows, income, names, query})
	reply, result, err := renderFinancialGuidance(query, 41, rows, income, names, guidanceTestNow())
	if err != nil || result["status"] != "read_only" || result["reply_source"] != "verified_guidance" || result["coverage_complete"] != true {
		t.Fatalf("review failed: %v %+v", err, result)
	}
	for key, want := range map[string]float64{"total_expense": 900, "total_income": 1500, "total_cash_expense": 720, "total_restricted_expense": 180, "total_cash_income": 1000, "total_restricted_income": 500} {
		if result[key] != want {
			t.Fatalf("%s=%v, want %v", key, result[key], want)
		}
	}
	categories := result["categories"].([]map[string]any)
	if len(categories) != 5 || result["matched_categories"] != 7 || result["limited"] != true || result["has_future_records"] != true || categories[0]["category_name"] != "Moradia" || categories[0]["percentage"] != 50.03 {
		t.Fatalf("ranking used a partial denominator, wrong source or wrong tenant: %+v", result)
	}
	for _, required := range []string{"50,03%", "dinheiro", "VA/VR", "nao saldo livre", "nao prova de desperdicio", "flexiveis", "inclusive futuros", "Exibindo 5 de 7"} {
		if !strings.Contains(reply, required) {
			t.Fatalf("review lost provenance or useful guidance %q: %s", required, reply)
		}
	}
	if strings.Contains(reply, "8877") || strings.Contains(reply, "Categoria externa") || strings.Contains(reply, "desperdicio comprovado") {
		t.Fatalf("review leaked or invented guidance: %s", reply)
	}
	after, _ := json.Marshal([]any{rows, income, names, query})
	if string(before) != string(after) {
		t.Fatal("guidance mutated its read snapshot")
	}
	query.PaymentSource = "VA"
	_, result, err = renderFinancialGuidance(query, 41, rows, income, names, guidanceTestNow())
	if err != nil || result["total_expense"] != float64(100) || result["total_income"] != float64(300) || result["total_cash_income"] != float64(0) || result["categories"].([]map[string]any)[0]["percentage"] != float64(100) {
		t.Fatalf("review ignored the exact split/source scope: %v %+v", err, result)
	}
	query.PaymentSource, query.CategoryName = "", "Saude"
	_, result, err = renderFinancialGuidance(query, 41, rows, income, names, guidanceTestNow())
	if err != nil || result["total_expense"] != float64(100) || len(result["categories"].([]map[string]any)) != 1 {
		t.Fatalf("review dropped its category filter: %v %+v", err, result)
	}
}

func TestSpendingReviewComparesOnlyRegisteredCashIncomeAndExpenses(t *testing.T) {
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2032, Months: []int{2}}}, GroupBy: "category", Operation: "review"}
	for _, test := range []struct {
		name                             string
		cashIncome, cashExpense, percent float64
		superam                          bool
	}{
		{"benefits do not dilute excess cash expenses", 100, 150, 150, true},
		{"lower ratio does not promise safe spending", 200, 50, 25, false},
		{"percentage rounds to two decimal places", 0.03, 0.02, 66.67, false},
		{"missing cash income has no percentage", 0, 150, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			income := []incomes.Income{{UserID: 41, Source: "Salario", Amount: test.cashIncome, Month: 2, Year: 2032}, {UserID: 41, Source: "VA", Amount: 1000, Month: 2, Year: 2032}}
			row := guidanceTestExpense(1, test.cashExpense+200, 2)
			row.PaymentSplits = []expenses.PaymentSplit{{PaymentSource: "Salario", Amount: test.cashExpense}, {PaymentSource: "VA", Amount: 200}}
			reply, result, err := renderFinancialGuidance(query, 41, []expenses.Expense{row}, income, map[uint]string{1: "Moradia"}, guidanceTestNow())
			if err != nil {
				t.Fatal(err)
			}
			if test.cashIncome == 0 {
				if _, present := result["registered_cash_expense_percentage"]; present || !strings.Contains(reply, "cadastro pode estar incompleto") || strings.Contains(reply, "equivalem a") {
					t.Fatalf("missing cash income became division by zero or a diagnosis: %+v %s", result, reply)
				}
			} else if result["registered_cash_expense_percentage"] != test.percent || !strings.Contains(reply, "As despesas em dinheiro cadastradas equivalem a **"+formatMoney(test.percent)+"%** das rendas em dinheiro cadastradas") {
				t.Fatalf("cash ratio included restricted benefits or lost provenance: %+v %s", result, reply)
			}
			if strings.Contains(reply, "superam as rendas") != test.superam || strings.Contains(reply, "folga segura") || strings.Contains(reply, "insolvencia") {
				t.Fatalf("review overstated what registered cash proves: %s", reply)
			}
		})
	}
	income := []incomes.Income{{UserID: 41, Source: "Salario", Amount: -100, Month: 2, Year: 2032}}
	if reply, result, err := renderFinancialGuidance(query, 41, nil, income, map[uint]string{}, guidanceTestNow()); err == nil || reply != "" || result != nil {
		t.Fatalf("negative income authorized a cash diagnosis: %v %+v %s", err, result, reply)
	}
}

func TestCategoryBudgetUsesOnlyCompletedRegisteredMonthsAndKeepsItsCategory(t *testing.T) {
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2032, Months: []int{2, 3, 4, 5, 6, 7}}}, CategoryName: "Deslocamento", GroupBy: "month", Operation: "budget"}
	names := map[uint]string{1: "Deslocamento", 2: "Moradia"}
	advanced := time.Date(2032, 3, 18, 12, 0, 0, 0, time.Local)
	rows := []expenses.Expense{
		guidanceTestExpense(1, 31.25, 2), guidanceTestExpense(1, 80.50, 3),
		guidanceTestExpense(1, 8.25, 7), guidanceTestExpense(1, 999.99, 6), guidanceTestExpense(1, 500, 7),
		guidanceTestExpense(2, 40, 2), guidanceTestExpense(2, 150, 3), guidanceTestExpense(1, 300, 1),
		{UserID: 99, CategoryID: 1, Amount: 8877, Month: 2, Year: 2032, PaymentSource: "Salario"},
	}
	rows[2].IsAdvanced, rows[2].AdvancedAt = true, &advanced
	rows[6].PaymentSplits = []expenses.PaymentSplit{{PaymentSource: "Salario", Amount: 100}, {PaymentSource: "VA", Amount: 50}}
	income := []incomes.Income{
		{UserID: 41, Source: "Salario", Amount: 1000, Month: 2, Year: 2032},
		{UserID: 41, Source: "Salario", Amount: 1200, Month: 3, Year: 2032},
		{UserID: 41, Source: "VA", Amount: 200, Month: 2, Year: 2032},
		{UserID: 41, Source: "VR", Amount: 300, Month: 3, Year: 2032},
		{UserID: 41, Source: "Salario", Amount: 3000, Month: 6, Year: 2032},
		{UserID: 41, Source: "Salario", Amount: 3000, Month: 7, Year: 2032},
	}
	reply, result, err := renderFinancialGuidance(query, 41, rows, income, names, guidanceTestNow())
	if err != nil || result["historical_reference_available"] != true || result["historical_months"] != 2 || result["missing_completed_months"] != 2 || result["historical_average_amount"] != float64(60) || result["historical_max_amount"] != 88.75 || result["historical_cash_income"] != float64(2200) || result["historical_other_cash_expense"] != float64(140) {
		t.Fatalf("budget mixed incomplete/future months or benefits into its history: %v %+v", err, result)
	}
	months := result["months"].([]map[string]any)
	if len(months) != 6 || months[1]["amount"] != 88.75 || months[2]["registered"] != false || months[4]["timing"] != "current_month" || months[5]["timing"] != "future_month" || result["has_future_records"] != true {
		t.Fatalf("monthly provenance was lost: %+v", result)
	}
	for _, required := range []string{"Deslocamento", "R$ 60,00", "R$ 88,75", "estimativa", "nao um limite seguro", "objetivo mensal", "despesas essenciais", "mes atual ainda parcial", "futuro cadastrado"} {
		if !strings.Contains(reply, required) {
			t.Fatalf("budget replaced the category plan or lost its caveat %q: %s", required, reply)
		}
	}
	if strings.Contains(reply, "Moradia") || strings.Contains(reply, "8877") || result["safe_spending_limit"] != nil {
		t.Fatalf("category budget became an all-category summary or invented a safe limit: %s", reply)
	}
}

func TestCategoryBudgetExplainsInsufficientHistoryWithoutInventingGoal(t *testing.T) {
	for _, test := range []struct {
		name string
		rows []expenses.Expense
		past int
	}{
		{"no history", nil, 0},
		{"future and current do not count", []expenses.Expense{guidanceTestExpense(1, 250, 6), guidanceTestExpense(1, 450, 7)}, 0},
		{"one complete month", []expenses.Expense{guidanceTestExpense(1, 31.25, 2), guidanceTestExpense(1, 450, 7)}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2032, Months: []int{2, 3, 6, 7}}}, CategoryName: "Deslocamento", GroupBy: "month", Operation: "budget"}
			reply, result, err := renderFinancialGuidance(query, 41, test.rows, nil, map[uint]string{1: "Deslocamento"}, guidanceTestNow())
			if err != nil || result["historical_reference_available"] != false || result["historical_months"] != test.past || result["historical_average_amount"] != nil || !strings.Contains(reply, "objetivo mensal") || !strings.Contains(reply, "essenciais") || strings.Contains(reply, "Ano de") {
				t.Fatalf("insufficient category history invented a forecast or generic annual summary: %v %+v %s", err, result, reply)
			}
			if len(test.rows) == 0 && strings.Contains(reply, "futuro cadastrado") {
				t.Fatal("an empty future month was presented as a registered future expense")
			}
		})
	}
}

func TestCategoryBudgetReplyLeadsWithReferenceAndShowsOnlyRecentRegisteredMonths(t *testing.T) {
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2032, Months: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}}}, CategoryName: "Deslocamento", GroupBy: "month", Operation: "budget"}
	var rows []expenses.Expense
	for month := 1; month <= 8; month++ {
		rows = append(rows, guidanceTestExpense(1, float64(month*10), month))
	}
	reply, result, err := renderFinancialGuidance(query, 41, rows, nil, map[uint]string{1: "Deslocamento"}, guidanceTestNow())
	if err != nil {
		t.Fatal(err)
	}
	months := result["months"].([]map[string]any)
	if len(months) != 12 || months[8]["registered"] != false || months[8]["amount"] != float64(0) || result["historical_average_amount"] != float64(30) || result["historical_months"] != 5 {
		t.Fatalf("compact presentation changed the complete monthly contract: %v %+v", err, result)
	}
	if strings.Index(reply, "A media cadastrada") > strings.Index(reply, "- **") || strings.Count(reply, "- **") != 6 || !strings.Contains(reply, "marco/2032") || !strings.Contains(reply, "agosto/2032") || strings.Contains(reply, "janeiro/2032") || strings.Contains(reply, "setembro/2032") {
		t.Fatalf("reference did not precede six recent registered months: %s", reply)
	}
	if strings.Count(reply, "sem lancamentos da categoria") != 1 || !strings.Contains(reply, "Ha 4 meses") || strings.Contains(reply, "Qual objetivo") || strings.Contains(reply, "R$ 0,00") {
		t.Fatalf("budget repeated empty months or asked for an unsupported goal: %s", reply)
	}
}

func TestCategoryBudgetKeepsTheRequestedPaymentSourceSeparateFromCash(t *testing.T) {
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2032, Months: []int{2, 3}}}, CategoryName: "Deslocamento", PaymentSource: "VA", GroupBy: "month", Operation: "budget"}
	rows := []expenses.Expense{guidanceTestExpense(1, 101.01, 2), guidanceTestExpense(1, 201.02, 3)}
	rows[0].PaymentSplits = []expenses.PaymentSplit{{PaymentSource: "Salario", Amount: 1.01}, {PaymentSource: "VA", Amount: 100}}
	rows[1].PaymentSplits = []expenses.PaymentSplit{{PaymentSource: "Salario", Amount: 1.02}, {PaymentSource: "VA", Amount: 200}}
	income := []incomes.Income{{UserID: 41, Source: "Salario", Amount: 5000, Month: 2, Year: 2032}, {UserID: 41, Source: "VA", Amount: 300, Month: 2, Year: 2032}}
	_, result, err := renderFinancialGuidance(query, 41, rows, income, map[uint]string{1: "Deslocamento"}, guidanceTestNow())
	if err != nil || result["total_expense"] != float64(300) || result["total_cash_expense"] != float64(0) || result["total_restricted_expense"] != float64(300) || result["historical_average_amount"] != float64(150) || result["historical_cash_income"] != float64(0) {
		t.Fatalf("budget treated another split or benefit income as free cash: %v %+v", err, result)
	}
}

func TestGuidanceRejectsInvalidMoneyOrScopeAndEscapesNames(t *testing.T) {
	base := financialReadQuery{Periods: []financialReadPeriod{{Year: 2032, Months: []int{2}}}, GroupBy: "category", Operation: "review"}
	for _, test := range []struct {
		name   string
		query  financialReadQuery
		rows   []expenses.Expense
		income []incomes.Income
	}{
		{"negative expense", base, []expenses.Expense{guidanceTestExpense(1, -1, 2)}, nil},
		{"non-finite expense", base, []expenses.Expense{guidanceTestExpense(1, math.NaN(), 2)}, nil},
		{"negative income", base, nil, []incomes.Income{{UserID: 41, Source: "Salario", Amount: -1, Month: 2, Year: 2032}}},
		{"subcent income", base, nil, []incomes.Income{{UserID: 41, Source: "Salario", Amount: 1.001, Month: 2, Year: 2032}}},
		{"inconsistent split", base, []expenses.Expense{{UserID: 41, CategoryID: 1, Amount: 100, Month: 2, Year: 2032, PaymentSplits: []expenses.PaymentSplit{{PaymentSource: "Salario", Amount: 90}}}}, nil},
		{"unknown source", base, []expenses.Expense{{UserID: 41, CategoryID: 1, Amount: 100, Month: 2, Year: 2032, PaymentSource: "Cartao"}}, nil},
		{"missing periods", financialReadQuery{GroupBy: "category", Operation: "review"}, nil, nil},
		{"unsupported operation", financialReadQuery{Periods: base.Periods, GroupBy: "category", Operation: "execute"}, nil, nil},
		{"budget missing category", financialReadQuery{Periods: base.Periods, GroupBy: "month", Operation: "budget"}, nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if reply, result, err := renderFinancialGuidance(test.query, 41, test.rows, test.income, map[uint]string{1: "Moradia"}, guidanceTestNow()); err == nil || reply != "" || result != nil {
				t.Fatalf("invalid guidance produced a partial answer: %v %+v %q", err, result, reply)
			}
		})
	}
	foreign := guidanceTestExpense(1, math.NaN(), 2)
	foreign.UserID = 99
	rows := []expenses.Expense{guidanceTestExpense(1, 12.34, 2), foreign}
	reply, result, err := renderFinancialGuidance(base, 41, rows, nil, map[uint]string{1: "**Casa**\n[abrir](x)"}, guidanceTestNow())
	if err != nil || result["total_expense"] != 12.34 || !strings.Contains(reply, `\*\*Casa\*\* \[abrir\]\(x\)`) {
		t.Fatalf("foreign invalid data affected the owner or names injected markup: %v %s", err, reply)
	}
}

func TestBudgetCentRoundingAndCalendarBoundary(t *testing.T) {
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2031, Months: []int{11, 12}}, {Year: 2032, Months: []int{1}}}, CategoryName: "Deslocamento", GroupBy: "month", Operation: "budget"}
	rows := []expenses.Expense{guidanceTestExpense(1, 0.01, 11), guidanceTestExpense(1, 0.02, 12), guidanceTestExpense(1, 100, 1)}
	rows[0].Year, rows[1].Year = 2031, 2031
	now := time.Date(2032, 1, 15, 0, 0, 0, 0, time.UTC)
	_, result, err := renderFinancialGuidance(query, 41, rows, nil, map[uint]string{1: "Deslocamento"}, now)
	if err != nil || result["historical_months"] != 2 || result["historical_average_amount"] != 0.02 || result["historical_max_amount"] != 0.02 {
		t.Fatalf("cent arithmetic or year rollover changed the reference: %v %+v", err, result)
	}
}
