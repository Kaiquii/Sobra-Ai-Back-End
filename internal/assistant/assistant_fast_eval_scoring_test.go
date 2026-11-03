package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These fixed, synthetic expectations score an opt-in evaluation only. They
// never select production actions or replace the financial safety validator.
type fastEvalCorpus struct {
	Version    int            `json:"version"`
	Now        time.Time      `json:"now"`
	Categories []string       `json:"categories"`
	Facts      map[string]any `json:"facts"`
	Cases      []fastEvalCase `json:"cases"`
}

type fastEvalCase struct {
	ID      string              `json:"id"`
	Message string              `json:"message"`
	History []Message           `json:"history,omitempty"`
	Draft   map[string]any      `json:"draft,omitempty"`
	Expect  fastEvalExpectation `json:"expect"`
}

type fastEvalExpectation struct {
	Kind              string  `json:"kind"`
	Operation         string  `json:"operation,omitempty"`
	GroupBy           string  `json:"group_by,omitempty"`
	Category          string  `json:"category,omitempty"`
	PaymentSource     string  `json:"payment_source,omitempty"`
	Year              int     `json:"year,omitempty"`
	Months            []int   `json:"months,omitempty"`
	Amount            float64 `json:"amount,omitempty"`
	Description       string  `json:"description,omitempty"`
	Income            float64 `json:"income,omitempty"`
	Expense           float64 `json:"expense,omitempty"`
	Balance           float64 `json:"balance,omitempty"`
	HistoricalAverage float64 `json:"historical_average,omitempty"`
	HistoricalMax     float64 `json:"historical_max,omitempty"`
	CurrentAmount     float64 `json:"current_amount,omitempty"`
}

type fastEvalScore struct {
	Passed        bool   `json:"passed"`
	Reason        string `json:"reason,omitempty"`
	RenderedReply string `json:"rendered_reply,omitempty"`
	ToolResult    any    `json:"tool_result,omitempty"`
}

func scoreFastEval(entry fastEvalCase, intent financialIntent, facts map[string]any, now time.Time) fastEvalScore {
	intent.facts = facts
	score := fastEvalScore{}
	fail := func(reason string) fastEvalScore {
		score.Reason = reason
		return score
	}
	switch entry.Expect.Kind {
	case "capacity", "analysis":
		if intent.Intent == "financial_analysis" {
			_, score.RenderedReply, _, score.ToolResult = analysisReply(intent)
			result, _ := score.ToolResult.(map[string]any)
			if result["status"] != "read_only" {
				return fail("analysis_not_read_only")
			}
		} else if entry.Expect.Kind == "capacity" && intent.Intent == "financial_query" {
			query, err := parseFinancialReadQuery(intent.provided["query"])
			if err != nil || !fastEvalQueryMatches(query, "savings", "month", "", "", entry.Expect.Year, entry.Expect.Months) {
				return fail("capacity_scope_mismatch")
			}
			rows, income, _ := fastEvalSyntheticRows()
			reply, result, err := renderSavingsPlan(query, 41, rows, income, now)
			score.RenderedReply, score.ToolResult = reply, result
			if err != nil || result["status"] != "read_only" {
				return fail("savings_not_read_only")
			}
			months, _ := result["months"].([]map[string]any)
			if len(months) != 1 || months[0]["potential_savings"] != entry.Expect.Balance {
				return fail("savings_balance_mismatch")
			}
		} else {
			return fail("planning_action_mismatch")
		}
		if reason := fastEvalAnalysisReason(score.RenderedReply, entry.Expect, now); reason != "" {
			return fail(reason)
		}
	case "query":
		if intent.Intent != "financial_query" {
			return fail("query_action_mismatch")
		}
		query, err := parseFinancialReadQuery(intent.provided["query"])
		if err != nil || !fastEvalQueryMatches(query, entry.Expect.Operation, entry.Expect.GroupBy, entry.Expect.Category, entry.Expect.PaymentSource, entry.Expect.Year, entry.Expect.Months) {
			return fail("query_scope_mismatch")
		}
		rows, income, names := fastEvalSyntheticRows()
		reply, result, err := renderFinancialGuidance(query, 41, rows, income, names, now)
		score.RenderedReply, score.ToolResult = reply, result
		if err != nil || result["status"] != "read_only" || result["reply_source"] != "verified_guidance" {
			return fail("guidance_not_read_only")
		}
		if result["historical_average_amount"] != entry.Expect.HistoricalAverage || result["historical_max_amount"] != entry.Expect.HistoricalMax || result["historical_months"] != 2 {
			return fail("budget_history_mismatch")
		}
		currentFound := false
		for _, month := range result["months"].([]map[string]any) {
			if month["year"] == now.Year() && month["month"] == int(now.Month()) {
				currentFound = month["amount"] == entry.Expect.CurrentAmount && month["timing"] == "current_month"
			}
		}
		if !currentFound {
			return fail("budget_current_month_mismatch")
		}
	case "expense":
		if intent.Intent != "create_expense" {
			return fail("expense_action_mismatch")
		}
		fields, ok := intent.provided["provided_fields"].([]any)
		wanted := map[string]bool{"description": true, "amount": true, "category_name": true, "payment_source": true}
		if !ok || len(fields) != len(wanted) {
			return fail("expense_field_mask_mismatch")
		}
		for _, raw := range fields {
			field, ok := raw.(string)
			if !ok || !wanted[field] {
				return fail("expense_field_mask_mismatch")
			}
			delete(wanted, field)
		}
		if intent.Day != 0 || intent.Month != 0 || intent.Year != 0 || intent.Type != "" || intent.Installments != 0 {
			return fail("expense_invented_missing_fields")
		}
		if raw, present := intent.provided["confirm"]; present && raw != false {
			return fail("expense_model_confirmation")
		}
		args := expenseArgsFromIntent(intent, entry.Draft)
		result, _ := prepareExpenseDraft(args)
		score.ToolResult = result
		if args["confirm"] != false || result["status"] != "needs_information" || numberToFloat(result["amount"]) != entry.Expect.Amount ||
			result["category_name"] != entry.Expect.Category || result["payment_source"] != entry.Expect.PaymentSource ||
			!strings.EqualFold(stringValue(result["description"]), entry.Expect.Description) || result["date"] != "" || result["type"] != "" {
			return fail("expense_draft_mismatch")
		}
		missing, _ := result["missing_fields"].([]string)
		if !reflect.DeepEqual(missing, []string{"date", "type"}) {
			return fail("expense_missing_fields_mismatch")
		}
	default:
		return fail("unknown_expectation")
	}
	score.Passed = true
	return score
}

func fastEvalQueryMatches(query financialReadQuery, operation, group, category, source string, year int, months []int) bool {
	return query.Operation == operation && query.GroupBy == group && query.CategoryName == category && query.PaymentSource == source && query.Position == 0 &&
		len(query.Periods) == 1 && query.Periods[0].Year == year && reflect.DeepEqual(query.Periods[0].Months, months)
}

var fastEvalKnownMoneyQuestion = regexp.MustCompile(`\b(?:qual (?:e |a |sua |a sua )*(?:renda|salario)|(?:quais|qual).{0,24}(?:suas despesas|seus gastos|despesas fixas)|quanto (?:voce )?(?:ganha|recebe|gasta)|(?:informe|diga|forneca|preciso (?:saber|de)).{0,35}(?:sua renda|seu salario|suas despesas|seus gastos))\b`)
var fastEvalGoalPriceQuestion = regexp.MustCompile(`\b(?:qual|quanto|informe|preciso|falta|diga|saber)\b.{0,120}\b(?:preco|valor|custo|custa|juntar)\b`)
var fastEvalGoalDeadlineQuestion = regexp.MustCompile(`\b(?:qual|quando|quanto|informe|preciso|falta|diga|saber)\b.{0,180}\b(?:prazo|mes|data|comprar)\b`)
var fastEvalMoneyLabel = regexp.MustCompile(`\b(?:renda|receita|entrada|entradas|despesa|despesas|gasto|gastos|saida|saidas|saldo|sobra|guardar|poupar|separar)\b`)
var fastEvalCapacityClaim = regexp.MustCompile(`\b(?:guardar|poupar|separar|disponivel|livre)\b[^.!?\n]{0,80}`)
var fastEvalOtherMoneyLabel = regexp.MustCompile(`\b(?:renda|receita|entradas?|despesas?|gastos?|saidas?)\b`)

// Lexical checks are intentionally confined to evaluation. Production uses
// structured actions and the same authenticated numerical validator as before.
func fastEvalAnalysisReason(reply string, expectation fastEvalExpectation, now time.Time) string {
	text := fastEvalNormalizedText(financialValidationText(reply))
	if fastEvalKnownMoneyQuestion.MatchString(text) {
		return "asks_registered_income_or_expenses"
	}
	if (fastEvalMentionsMoney(text, 4000) || fastEvalMentionsMoney(text, 3000) || fastEvalMentionsMoney(text, 1000)) &&
		(!strings.Contains(text, "dinheiro") || (!regexp.MustCompile(`\bva\b`).MatchString(text) && !strings.Contains(text, "beneficio"))) {
		return "benefits_not_distinguished"
	}
	for _, claim := range fastEvalCapacityClaim.FindAllString(text, -1) {
		match := financialMoneyPattern.FindStringSubmatchIndex(claim)
		if match == nil || fastEvalOtherMoneyLabel.MatchString(claim[:match[0]]) {
			continue
		}
		start, end := match[2], match[3]
		if start < 0 {
			start, end = match[4], match[5]
		}
		if canonicalFinancialNumber(claim[start:end]) != strconv.FormatFloat(expectation.Balance, 'f', 2, 64) {
			return "wrong_cash_capacity_claim"
		}
	}
	for _, required := range []struct {
		labels, reason string
		amount         float64
	}{
		{`\b(?:renda|receita|entradas?)\b`, "missing_current_income", expectation.Income},
		{`\b(?:despesas?|gastos?|saidas?)\b`, "missing_current_expense", expectation.Expense},
		{`\b(?:sobra|saldo|guardar|poupar|separar)\b`, "missing_current_balance", expectation.Balance},
	} {
		if !fastEvalLabeledMoney(text, required.labels, required.amount) {
			return required.reason
		}
	}
	monthNames := []string{"janeiro", "fevereiro", "marco", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}
	month := monthNames[int(now.Month())-1]
	if !strings.Contains(text, month) && !strings.Contains(text, "mes atual") && !strings.Contains(text, "neste mes") {
		return "missing_current_period"
	}
	calendar := regexp.MustCompile(`\b` + month + `\s*(?:de\s*|/\s*)?(20\d{2})\b`)
	for _, match := range calendar.FindAllStringSubmatch(text, -1) {
		if match[1] != strconv.Itoa(expectation.Year) {
			return "wrong_current_period"
		}
	}
	if !fastEvalGoalPriceQuestion.MatchString(text) {
		return "missing_goal_price_question"
	}
	if !fastEvalGoalDeadlineQuestion.MatchString(text) {
		return "missing_goal_deadline_question"
	}
	return ""
}

func fastEvalLabeledMoney(text, labels string, amount float64) bool {
	wanted := strconv.FormatFloat(amount, 'f', 2, 64)
	for _, index := range regexp.MustCompile(labels).FindAllStringIndex(text, -1) {
		tail := text[index[1]:min(len(text), index[1]+120)]
		if match := financialMoneyPattern.FindStringSubmatchIndex(tail); match != nil && !fastEvalMoneyLabel.MatchString(tail[:match[0]]) {
			start, end := match[2], match[3]
			if start < 0 {
				start, end = match[4], match[5]
			}
			if canonicalFinancialNumber(tail[start:end]) == wanted {
				return true
			}
		}
		prefix := text[max(0, index[0]-60):index[0]]
		matches := financialMoneyPattern.FindAllStringSubmatchIndex(prefix, -1)
		if len(matches) == 0 {
			continue
		}
		match := matches[len(matches)-1]
		if between := strings.TrimSpace(prefix[match[1]:]); between != "de" && between != "em" {
			continue
		}
		start, end := match[2], match[3]
		if start < 0 {
			start, end = match[4], match[5]
		}
		if canonicalFinancialNumber(prefix[start:end]) == wanted {
			return true
		}
	}
	return false
}

func fastEvalMentionsMoney(text string, amount float64) bool {
	wanted := strconv.FormatFloat(amount, 'f', 2, 64)
	for _, match := range financialMoneyPattern.FindAllStringSubmatch(text, -1) {
		token := match[1]
		if token == "" {
			token = match[2]
		}
		if canonicalFinancialNumber(token) == wanted {
			return true
		}
	}
	return false
}

func fastEvalNormalizedText(text string) string {
	return strings.NewReplacer("á", "a", "à", "a", "ã", "a", "â", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ü", "u", "ç", "c").Replace(strings.ToLower(text))
}

func fastEvalSyntheticRows() ([]expenses.Expense, []incomes.Income, map[uint]string) {
	var rows []expenses.Expense
	var income []incomes.Income
	for i, month := range []int{9, 10, 11} {
		transport := []float64{180, 190, 160}[i]
		marketCash := []float64{730, 720, 850}[i]
		salary := []float64{3100, 3100, 3200}[i]
		rows = append(rows,
			expenses.Expense{UserID: 41, CategoryID: 1, Year: 2026, Month: month, Amount: transport, PaymentSource: "Salario"},
			expenses.Expense{UserID: 41, CategoryID: 2, Year: 2026, Month: month, Amount: 1390, PaymentSource: "Salario"},
			expenses.Expense{UserID: 41, CategoryID: 3, Year: 2026, Month: month, Amount: marketCash + 600, PaymentSplits: []expenses.PaymentSplit{{PaymentSource: "Salario", Amount: marketCash}, {PaymentSource: "VA", Amount: 600}}},
		)
		income = append(income,
			incomes.Income{UserID: 41, Year: 2026, Month: month, Amount: salary, Source: "Salario"},
			incomes.Income{UserID: 41, Year: 2026, Month: month, Amount: 800, Source: "VA"},
		)
	}
	return rows, income, map[uint]string{1: "Transporte", 2: "Moradia", 3: "Mercado", 4: "Educação"}
}

func fastEvalLoadFixedCorpus(t *testing.T) fastEvalCorpus {
	t.Helper()
	raw, err := os.ReadFile("testdata/fast_eval.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus fastEvalCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func fastEvalParseForTest(t *testing.T, raw string) financialIntent {
	t.Helper()
	intent, err := parseFinancialIntent(raw)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func fastEvalVerifiedAnalysisText() string {
	return "Em novembro de 2026, sua renda cadastrada é {{fact:/current_month/total_cash_income}}, as despesas em dinheiro são {{fact:/current_month/total_cash_expense}} e a sobra cadastrada é {{fact:/current_month/total_cash_balance}}. Essa sobra pode servir de referência, sem garantir meses futuros nem considerar gastos não cadastrados. Qual é o valor do objetivo e em que mês pretende comprar?"
}

func TestFastEvalCorpusKeepsFourFixedCasesAndConsistentSnapshot(t *testing.T) {
	corpus := fastEvalLoadFixedCorpus(t)
	if corpus.Version != 1 || corpus.Now.Format(time.RFC3339) != "2026-11-03T09:00:00-03:00" || len(corpus.Cases) != 4 ||
		!reflect.DeepEqual(corpus.Categories, []string{"Transporte", "Moradia", "Mercado", "Educação"}) {
		t.Fatal("fixed evaluation identity changed")
	}
	wantedIDs := []string{"car_exact", "car_paraphrase", "category_budget", "purchase_partial"}
	for i, entry := range corpus.Cases {
		if entry.ID != wantedIDs[i] {
			t.Fatalf("fixed case order/identity changed: %s", entry.ID)
		}
	}
	if corpus.Cases[0].Message != "To planejando, comprar um carro ano que vem, qual valor consigo guardar por mês, de acordo com minhas dívidas?" || corpus.Cases[3].Message != "Registra material escolar por 85 reais, Salario, categoria Educação." {
		t.Fatal("exact regression or partial-purchase message changed")
	}
	projected, err := compactAnalysisFactsForPrompt(corpus.Facts)
	if err != nil {
		t.Fatal(err)
	}
	rows, income, names := fastEvalSyntheticRows()
	aggregated := aggregateAnalysisYear(2026, income, rows, names, corpus.Now)
	future := aggregateAnalysisYear(2027, nil, nil, names, corpus.Now)
	computed, err := compactAnalysisFactsForPrompt(map[string]any{"as_of": corpus.Now.Format("2006-01-02"), "years": []int{2027, 2026}, "periods": []any{future, aggregated}})
	if err != nil || !reflect.DeepEqual(projected, computed) {
		t.Fatal("synthetic Go rows and the complete projected prompt snapshot differ")
	}
	months := aggregated["months_with_records"].([]map[string]any)
	for _, month := range months {
		if month["total_cash_balance"] != float64(800) || month["total_balance"] != float64(1000) || month["cash_income_registered"] != true || month["expenses_registered"] != true {
			t.Fatalf("synthetic monthly registration/capacity changed: %+v", month)
		}
	}
	current := projected["current_month"].(map[string]any)
	if current["month"] != float64(11) || current["total_cash_income"] != float64(3200) || current["total_cash_expense"] != float64(2400) || current["total_cash_balance"] != float64(800) {
		t.Fatal("current November snapshot is not the frozen 800-real capacity")
	}
	for _, alias := range []string{"current_month", "latest_registered_month"} {
		for _, key := range []string{"total_income", "total_expense", "total_balance"} {
			if _, present := projected[alias].(map[string]any)[key]; present {
				t.Fatalf("cash alias %s exposes benefit-inclusive %s", alias, key)
			}
		}
	}
}

func TestFastEvalScoringAcceptsFourIntendedActionsWithoutDatabase(t *testing.T) {
	corpus := fastEvalLoadFixedCorpus(t)
	facts, err := compactAnalysisFactsForPrompt(corpus.Facts)
	if err != nil {
		t.Fatal(err)
	}
	analysis, _ := json.Marshal(map[string]any{"intent": "financial_analysis", "reply": fastEvalVerifiedAnalysisText(), "calculations": []any{}})
	raw := []string{
		`{"intent":"financial_query","query":{"periods":[{"year":2026,"months":[11]}],"category_name":"","payment_source":"","group_by":"month","operation":"savings"}}`,
		string(analysis),
		`{"intent":"financial_query","query":{"periods":[{"year":2026,"months":[]}],"category_name":"Transporte","payment_source":"","group_by":"month","operation":"budget"}}`,
		`{"intent":"create_expense","description":"material escolar","amount":85,"category_name":"Educação","payment_source":"Salario","provided_fields":["description","amount","category_name","payment_source"]}`,
	}
	for i, entry := range corpus.Cases {
		t.Run(entry.ID, func(t *testing.T) {
			score := scoreFastEval(entry, fastEvalParseForTest(t, raw[i]), facts, corpus.Now)
			if !score.Passed || score.Reason != "" || score.ToolResult == nil {
				t.Fatalf("fixed correct action failed: %+v", score)
			}
		})
	}
	if score := scoreFastEval(corpus.Cases[0], fastEvalParseForTest(t, string(analysis)), facts, corpus.Now); !score.Passed {
		t.Fatalf("exact capacity question must also accept a verified useful analysis: %+v", score)
	}
}

func TestFastEvalScoringRejectsNumericallyValidButUselessCapturedAdvice(t *testing.T) {
	corpus := fastEvalLoadFixedCorpus(t)
	raw, err := os.ReadFile("testdata/reasoning_captured_20261102.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Facts   map[string]any `json:"facts"`
		Records []struct {
			ID       string `json:"id"`
			Response string `json:"response"`
		} `json:"records"`
	}
	if err := json.Unmarshal(raw, &capture); err != nil {
		t.Fatal(err)
	}
	for _, record := range capture.Records {
		if record.ID != "open_capacity" {
			continue
		}
		intent := fastEvalParseForTest(t, record.Response)
		intent.facts = capture.Facts
		_, _, _, result := analysisReply(intent)
		if result.(map[string]any)["status"] != "read_only" {
			t.Fatal("captured regression must demonstrate the old numerical-only acceptance")
		}
		score := scoreFastEval(corpus.Cases[0], intent, capture.Facts, corpus.Now)
		if score.Passed || score.Reason != "asks_registered_income_or_expenses" {
			t.Fatalf("numerical validity alone qualified captured useless advice: %+v", score)
		}
		return
	}
	t.Fatal("fixed open_capacity capture is missing")
}

func TestFastEvalScoringSeparatesCashCapacityFromBenefitsAndEmptyFuture(t *testing.T) {
	corpus := fastEvalLoadFixedCorpus(t)
	facts, err := compactAnalysisFactsForPrompt(corpus.Facts)
	if err != nil {
		t.Fatal(err)
	}
	grossExplanation := " Os totais com benefícios VA são renda de {{fact:/periods/1/months_with_records/2/total_income}}, despesas de {{fact:/periods/1/months_with_records/2/total_expense}} e saldo total de {{fact:/periods/1/months_with_records/2/total_balance}}. Esses benefícios não são dinheiro livre para a compra."
	for _, test := range []struct {
		name, reply, reason string
	}{
		{"explained gross totals alongside correct cash", fastEvalVerifiedAnalysisText() + grossExplanation, ""},
		{"benefit balance cannot be saved", fastEvalVerifiedAnalysisText() + grossExplanation + " Você pode guardar {{fact:/periods/1/months_with_records/2/total_balance}} por mês.", "wrong_cash_capacity_claim"},
		{"empty future is not current capacity", fastEvalVerifiedAnalysisText() + " Você pode guardar {{fact:/periods/0/total_income}} por mês.", "wrong_cash_capacity_claim"},
		{"gross totals need a benefits explanation", fastEvalVerifiedAnalysisText() + " O saldo total é {{fact:/periods/1/months_with_records/2/total_balance}}.", "benefits_not_distinguished"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"intent": "financial_analysis", "reply": test.reply, "calculations": []any{}})
			intent := fastEvalParseForTest(t, string(raw))
			intent.facts = facts
			_, _, _, result := analysisReply(intent)
			if result.(map[string]any)["status"] != "read_only" {
				t.Fatal("fixed counterexample must reach the semantic scorer with supported numbers")
			}
			score := scoreFastEval(corpus.Cases[0], intent, facts, corpus.Now)
			if score.Reason != test.reason || score.Passed != (test.reason == "") {
				t.Fatalf("benefit/future semantics scored incorrectly: got %+v, wanted %q", score, test.reason)
			}
		})
	}
}

func TestFastEvalScoringRejectsWrongScopeAndMissingPlanningUtility(t *testing.T) {
	corpus := fastEvalLoadFixedCorpus(t)
	facts, err := compactAnalysisFactsForPrompt(corpus.Facts)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, reply, reason string
	}{
		{"registered income asked despite correct money", fastEvalVerifiedAnalysisText() + " Qual é sua renda mensal atual?", "asks_registered_income_or_expenses"},
		{"registered expenses asked despite correct money", fastEvalVerifiedAnalysisText() + " Quais são suas despesas fixas mensais?", "asks_registered_income_or_expenses"},
		{"missing price and deadline", strings.Split(fastEvalVerifiedAnalysisText(), "Qual é")[0], "missing_goal_price_question"},
		{"only missing deadline", strings.Split(fastEvalVerifiedAnalysisText(), "Qual é")[0] + " Qual é o preço do objetivo?", "missing_goal_deadline_question"},
		{"wrong current period", strings.ReplaceAll(fastEvalVerifiedAnalysisText(), "novembro de 2026", "novembro de 2027"), "wrong_current_period"},
		{"swapped income and expense", strings.NewReplacer("/total_cash_income", "/total_cash_expense", "/total_cash_expense", "/total_cash_income").Replace(fastEvalVerifiedAnalysisText()), "missing_current_income"},
		{"missing balance", strings.Replace(fastEvalVerifiedAnalysisText(), " e a sobra cadastrada é {{fact:/current_month/total_cash_balance}}", "", 1), "missing_current_balance"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"intent": "financial_analysis", "reply": test.reply, "calculations": []any{}})
			score := scoreFastEval(corpus.Cases[0], fastEvalParseForTest(t, string(raw)), facts, corpus.Now)
			if score.Passed || score.Reason != test.reason {
				t.Fatalf("unexpected semantic score: got %+v, wanted %s", score, test.reason)
			}
		})
	}
	for _, test := range []struct{ name, query string }{
		{"goal year is not contribution year", `{"periods":[{"year":2027,"months":[11]}],"category_name":"","payment_source":"","group_by":"month","operation":"savings"}`},
		{"category ranking is not capacity", `{"periods":[{"year":2026,"months":[11]}],"category_name":"Transporte","payment_source":"","group_by":"category","operation":"max"}`},
		{"full year is not current capacity", `{"periods":[{"year":2026,"months":[]}],"category_name":"","payment_source":"","group_by":"month","operation":"savings"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent := fastEvalParseForTest(t, `{"intent":"financial_query","query":`+test.query+`}`)
			if score := scoreFastEval(corpus.Cases[0], intent, facts, corpus.Now); score.Passed || score.Reason != "capacity_scope_mismatch" {
				t.Fatalf("wrong capacity scope passed: %+v", score)
			}
		})
	}
	// Asking about unregistered expenses is a valid limitation, not a request for
	// the already supplied totals. Keep this distinct from the captured failure.
	raw, _ := json.Marshal(map[string]any{"intent": "financial_analysis", "reply": fastEvalVerifiedAnalysisText() + " Há despesas não cadastradas?", "calculations": []any{}})
	if score := scoreFastEval(corpus.Cases[0], fastEvalParseForTest(t, string(raw)), facts, corpus.Now); !score.Passed {
		t.Fatalf("honest coverage question confused with asking known expenses: %+v", score)
	}
}

func TestFastEvalScoringRejectsInventedPartialPurchaseFields(t *testing.T) {
	corpus := fastEvalLoadFixedCorpus(t)
	base := map[string]any{"intent": "create_expense", "description": "material escolar", "amount": 85, "category_name": "Educação", "payment_source": "Salario", "provided_fields": []any{"description", "amount", "category_name", "payment_source"}}
	for _, test := range []struct {
		name, field string
		value       any
	}{
		{"invented date", "day", 3},
		{"invented type", "type", "Unica"},
		{"model confirms", "confirm", true},
		{"wrong amount", "amount", 850},
		{"wrong category", "category_name", "Moradia"},
		{"expanded mask", "provided_fields", []any{"description", "amount", "category_name", "payment_source", "day"}},
		{"duplicated mask", "provided_fields", []any{"description", "amount", "category_name", "category_name"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := map[string]any{}
			for key, value := range base {
				changed[key] = value
			}
			changed[test.field] = test.value
			raw, _ := json.Marshal(changed)
			score := scoreFastEval(corpus.Cases[3], fastEvalParseForTest(t, string(raw)), nil, corpus.Now)
			if score.Passed || score.Reason == "" {
				t.Fatalf("invalid partial purchase qualified: %+v", score)
			}
		})
	}
}

func TestFastEvalCategoryBudgetRequiresWholeYearAndTheRequestedCategory(t *testing.T) {
	corpus := fastEvalLoadFixedCorpus(t)
	base := map[string]any{"periods": []any{map[string]any{"year": 2026, "months": []int{}}}, "category_name": "Transporte", "payment_source": "", "group_by": "month", "operation": "budget"}
	for _, test := range []struct {
		name, field string
		value       any
	}{
		{"sum instead of budget", "operation", "sum"},
		{"category ranking", "group_by", "category"},
		{"wrong category", "category_name", "Moradia"},
		{"unexpected source", "payment_source", "Salario"},
		{"only recorded months", "periods", []any{map[string]any{"year": 2026, "months": []int{9, 10, 11}}}},
		{"wrong year", "periods", []any{map[string]any{"year": 2027, "months": []int{}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := map[string]any{}
			for key, value := range base {
				query[key] = value
			}
			query[test.field] = test.value
			raw, _ := json.Marshal(map[string]any{"intent": "financial_query", "query": query})
			score := scoreFastEval(corpus.Cases[2], fastEvalParseForTest(t, string(raw)), nil, corpus.Now)
			if score.Passed || score.Reason != "query_scope_mismatch" {
				t.Fatalf("wrong annual category plan qualified: %+v", score)
			}
		})
	}
}
