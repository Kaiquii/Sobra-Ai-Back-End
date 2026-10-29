package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type financialGuidanceMonth struct {
	year, month                   int
	amount, cashIncome, otherCash int64
	categoryRecords               int
}

type financialGuidanceCategory struct {
	id     uint
	name   string
	amount int64
}

// Guidance is rendered from an authenticated read snapshot. It never treats a
// registered balance or historical average as a safe spending authorization.
func renderFinancialGuidance(query financialReadQuery, userID uint, expenseRows []expenses.Expense, incomeRows []incomes.Income, names map[uint]string, now time.Time) (string, map[string]any, error) {
	if userID == 0 || now.IsZero() || query.Limit < 0 || query.Limit > 24 || (query.Operation != "review" && query.Operation != "budget") ||
		(query.Operation == "review" && query.GroupBy != "category") || (query.Operation == "budget" && query.GroupBy != "month") {
		return "", nil, fmt.Errorf("invalid guidance scope")
	}
	category := strings.TrimSpace(query.CategoryName)
	var categoryID uint
	if category != "" {
		for id, name := range names {
			if strings.EqualFold(strings.TrimSpace(name), category) {
				if categoryID != 0 || id == 0 {
					return "", nil, fmt.Errorf("ambiguous guidance category")
				}
				categoryID = id
			}
		}
		if categoryID == 0 {
			return "", nil, fmt.Errorf("unknown guidance category")
		}
	} else if query.Operation == "budget" {
		return "", nil, fmt.Errorf("budget requires a category")
	}
	source := normalizeMoneySource(query.PaymentSource)
	if strings.TrimSpace(query.PaymentSource) != "" && source == "" {
		return "", nil, fmt.Errorf("invalid guidance source")
	}
	months, err := financialGuidanceMonths(query)
	if err != nil {
		return "", nil, err
	}
	var income, cashIncome, restrictedIncome, total, cashExpense, restrictedExpense int64
	groups := map[uint]*financialGuidanceCategory{}
	for _, row := range incomeRows {
		month := months[row.Year*100+row.Month]
		if row.UserID != userID || month == nil {
			continue
		}
		rowSource := normalizeMoneySource(row.Source)
		amount, err := financialQueryCents(row.Amount)
		if err != nil || amount < 0 || rowSource == "" {
			return "", nil, fmt.Errorf("invalid registered guidance income")
		}
		if source != "" && rowSource != source {
			continue
		}
		income += amount
		if rowSource == "va" || rowSource == "vr" {
			restrictedIncome += amount
		} else {
			cashIncome += amount
			month.cashIncome += amount
		}
	}
	for _, row := range expenseRows {
		monthNumber, year := expenses.EffectiveMonthYear(row)
		month := months[year*100+monthNumber]
		if row.UserID != userID || month == nil {
			continue
		}
		amount, cash, restricted, err := financialGuidanceExpenseParts(row, source)
		if err != nil {
			return "", nil, err
		}
		if source != "" && amount == 0 {
			continue
		}
		if categoryID != 0 && row.CategoryID != categoryID {
			month.otherCash += cash
			continue
		}
		month.amount += amount
		month.categoryRecords++
		total, cashExpense, restrictedExpense = total+amount, cashExpense+cash, restrictedExpense+restricted
		group := groups[row.CategoryID]
		if group == nil {
			name := strings.TrimSpace(names[row.CategoryID])
			if name == "" {
				name = "Sem categoria"
			}
			group = &financialGuidanceCategory{id: row.CategoryID, name: name}
			groups[row.CategoryID] = group
		}
		group.amount += amount
	}
	result := map[string]any{
		"status": "read_only", "query": query, "as_of": now.Format("2006-01-02"), "reply_source": "verified_guidance",
		"coverage_complete": true, "total_expense": float64(total) / 100, "total_income": float64(income) / 100,
		"total_cash_income": float64(cashIncome) / 100, "total_restricted_income": float64(restrictedIncome) / 100,
		"total_cash_expense": float64(cashExpense) / 100, "total_restricted_expense": float64(restrictedExpense) / 100,
	}
	if query.Operation == "review" {
		return renderSpendingReview(query, groups, months, result, total, cashIncome, cashExpense, now), result, nil
	}
	return renderCategoryBudget(query, months, result, now), result, nil
}

func financialGuidanceMonths(query financialReadQuery) (map[int]*financialGuidanceMonth, error) {
	if len(query.Periods) == 0 || len(query.Periods) > 24 {
		return nil, fmt.Errorf("invalid guidance periods")
	}
	years := map[int]bool{}
	months := map[int]*financialGuidanceMonth{}
	for _, period := range query.Periods {
		if period.Year < 2000 || period.Year > 9999 || len(period.Months) > 12 {
			return nil, fmt.Errorf("invalid guidance period")
		}
		years[period.Year] = true
		selected := period.Months
		if len(selected) == 0 {
			selected = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
		}
		for _, month := range selected {
			if month < 1 || month > 12 {
				return nil, fmt.Errorf("invalid guidance month")
			}
			key := period.Year*100 + month
			months[key] = &financialGuidanceMonth{year: period.Year, month: month}
		}
	}
	if len(years) > 2 {
		return nil, fmt.Errorf("too many guidance years")
	}
	return months, nil
}

func financialGuidanceExpenseParts(row expenses.Expense, source string) (int64, int64, int64, error) {
	total, err := financialQueryCents(row.Amount)
	if err != nil || total < 0 {
		return 0, 0, 0, fmt.Errorf("invalid registered guidance expense")
	}
	var sum, selected, cash, restricted int64
	seen := map[string]bool{}
	for _, split := range expenses.PaymentSplitsOrLegacy(row) {
		amount, err := financialQueryCents(split.Amount)
		kind := normalizeMoneySource(split.PaymentSource)
		if err != nil || amount < 0 || kind == "" || seen[kind] {
			return 0, 0, 0, fmt.Errorf("invalid registered guidance split")
		}
		seen[kind], sum = true, sum+amount
		if source != "" && kind != source {
			continue
		}
		selected += amount
		if kind == "va" || kind == "vr" {
			restricted += amount
		} else {
			cash += amount
		}
	}
	if sum != total {
		return 0, 0, 0, fmt.Errorf("inconsistent registered guidance splits")
	}
	return selected, cash, restricted, nil
}

func renderSpendingReview(query financialReadQuery, groups map[uint]*financialGuidanceCategory, months map[int]*financialGuidanceMonth, result map[string]any, total, cashIncome, cashExpense int64, now time.Time) string {
	ordered := make([]financialGuidanceCategory, 0, len(groups))
	for _, group := range groups {
		ordered = append(ordered, *group)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].amount != ordered[j].amount {
			return ordered[i].amount > ordered[j].amount
		}
		if ordered[i].name != ordered[j].name {
			return ordered[i].name < ordered[j].name
		}
		return ordered[i].id < ordered[j].id
	})
	limit := 5
	if query.Limit > 0 && query.Limit < limit {
		limit = query.Limit
	}
	shown := ordered[:min(limit, len(ordered))]
	rows := make([]map[string]any, 0, len(shown))
	lines := []string{"**Onde as despesas cadastradas mais pesam**", financialGuidanceScope(query), "Total de despesas no escopo: **R$ " + formatMoney(float64(total)/100) + "**."}
	for _, group := range shown {
		percentage := 0.0
		if total > 0 {
			percentage = math.Round(float64(group.amount)*10000/float64(total)) / 100
		}
		rows = append(rows, map[string]any{"category_name": group.name, "amount": float64(group.amount) / 100, "percentage": percentage})
		lines = append(lines, "- **"+financialQueryLabel(group.name)+":** R$ "+formatMoney(float64(group.amount)/100)+" ("+formatMoney(percentage)+"% das despesas no escopo).")
	}
	if len(shown) == 0 {
		lines = append(lines, "Nao encontrei despesas cadastradas nesse recorte; isso nao prova ausencia de gastos reais.")
	}
	lines = append(lines, "Rendas cadastradas no periodo: **R$ "+formatMoney(numberToFloat(result["total_income"]))+"**, sendo **R$ "+formatMoney(numberToFloat(result["total_cash_income"]))+"** em dinheiro e **R$ "+formatMoney(numberToFloat(result["total_restricted_income"]))+"** em VA/VR. Sao registros, nao saldo livre ou recebimento confirmado.")
	if cashIncome > 0 {
		percentage := math.Round(float64(cashExpense)*10000/float64(cashIncome)) / 100
		result["registered_cash_expense_percentage"] = percentage
		lines = append(lines, "As despesas em dinheiro cadastradas equivalem a **"+formatMoney(percentage)+"%** das rendas em dinheiro cadastradas no escopo.")
		if cashExpense > cashIncome {
			lines = append(lines, "As despesas em dinheiro cadastradas superam as rendas em dinheiro cadastradas nesse escopo; isso pede revisao dos compromissos e da completude do cadastro.")
		}
	} else {
		lines = append(lines, "Sem renda em dinheiro cadastrada no escopo, nao calculei esse percentual; o cadastro pode estar incompleto.")
	}
	lines = append(lines,
		"Maior valor e um ponto de revisao, nao prova de desperdicio. Quais dessas categorias sao flexiveis e o que e essencial para voce antes de definir cortes?",
		financialGuidanceCaveat(),
	)
	missing, future := 0, false
	for key, month := range months {
		if month.categoryRecords == 0 {
			missing++
		}
		future = future || (key > now.Year()*100+int(now.Month()) && month.categoryRecords > 0)
	}
	result["categories"], result["matched_categories"], result["limited"] = rows, len(ordered), len(shown) < len(ordered)
	result["missing_registered_months"], result["has_future_records"] = missing, future
	if missing > 0 {
		lines = append(lines, fmt.Sprintf("Ha %d meses solicitados sem despesas cadastradas nesse escopo; nao os trate como prova de gasto real zero.", missing))
	}
	if len(shown) < len(ordered) {
		lines = append(lines, fmt.Sprintf("Exibindo %d de %d categorias; totais e participacoes usam todas as categorias do escopo.", len(shown), len(ordered)))
	}
	return strings.Join(lines, "\n\n")
}

func renderCategoryBudget(query financialReadQuery, months map[int]*financialGuidanceMonth, result map[string]any, now time.Time) string {
	keys := make([]int, 0, len(months))
	for key := range months {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	current := now.Year()*100 + int(now.Month())
	var historicalTotal, highest, cashIncome, otherCash int64
	history, missing, future := 0, 0, false
	missingRecords := 0
	rows := make([]map[string]any, 0, len(keys))
	var registeredLines []string
	for _, key := range keys {
		month := months[key]
		registered := month.categoryRecords > 0
		timing := "completed_month"
		label := "mes encerrado"
		if key == current {
			timing, label = "current_month", "mes atual ainda parcial"
		} else if key > current {
			timing, label = "future_month", "futuro cadastrado"
		}
		rows = append(rows, map[string]any{"year": month.year, "month": month.month, "amount": float64(month.amount) / 100, "registered": registered, "timing": timing})
		if registered {
			registeredLines = append(registeredLines, "- **"+financialQueryMonth(month.month, month.year)+":** R$ "+formatMoney(float64(month.amount)/100)+" ("+label+").")
		} else {
			missingRecords++
		}
		if key < current {
			if registered {
				history++
				historicalTotal += month.amount
				highest = max(highest, month.amount)
				cashIncome += month.cashIncome
				otherCash += month.otherCash
			} else {
				missing++
			}
		}
		future = future || (key > current && registered)
	}
	result["months"], result["historical_months"], result["missing_completed_months"] = rows, history, missing
	result["has_future_records"], result["historical_reference_available"] = future, history >= 2
	lines := []string{"**Planejamento para " + financialQueryLabel(query.CategoryName) + "**"}
	if history >= 2 {
		average := (historicalTotal + int64(history)/2) / int64(history)
		result["historical_average_amount"], result["historical_max_amount"] = float64(average)/100, float64(highest)/100
		result["historical_cash_income"], result["historical_other_cash_expense"] = float64(cashIncome)/100, float64(otherCash)/100
		lines = append(lines, fmt.Sprintf("A media cadastrada foi **R$ %s** e o maior valor mensal foi **R$ %s**, considerando %d meses encerrados com registros da categoria. Sao referencias para uma estimativa, nao um limite seguro ou previsao garantida.", formatMoney(float64(average)/100), formatMoney(float64(highest)/100), history))
	} else {
		lines = append(lines, "Ainda nao ha historico suficiente para uma referencia mensal: sao necessarios ao menos dois meses encerrados com registros dessa categoria.")
	}
	lines = append(lines, financialGuidanceScope(query))
	if len(registeredLines) > 0 {
		shown := registeredLines[max(0, len(registeredLines)-6):]
		lines = append(lines, "Meses com registros mais recentes:\n"+strings.Join(shown, "\n"))
		if len(shown) < len(registeredLines) {
			lines = append(lines, fmt.Sprintf("Exibindo os %d meses registrados mais recentes; a referencia considera todos os meses encerrados com registros no escopo.", len(shown)))
		}
	}
	if missingRecords > 0 {
		lines = append(lines, fmt.Sprintf("Ha %d meses solicitados sem lancamentos da categoria; isso nao prova gasto real zero.", missingRecords))
	}
	if history >= 2 && (cashIncome > 0 || otherCash > 0) {
		lines = append(lines, "Nos mesmos meses usados na referencia: **R$ "+formatMoney(float64(cashIncome)/100)+"** de rendas em dinheiro e **R$ "+formatMoney(float64(otherCash)/100)+"** de outras despesas em dinheiro cadastradas. Esses totais nao demonstram dinheiro livre.")
	}
	lines = append(lines,
		"Para escolher um teto, avalie seu objetivo mensal e as despesas essenciais ainda nao cadastradas. Voce pode consultar outro periodo para ampliar o historico.",
		"O mes atual e os futuros ficam fora da referencia historica. Valores cadastrados nao comprovam pagamento ou recebimento; VA/VR nao sao dinheiro livre.",
	)
	return strings.Join(lines, "\n\n")
}

func financialGuidanceScope(query financialReadQuery) string {
	var periods []string
	for _, period := range query.Periods {
		if len(period.Months) == 0 || len(period.Months) == 12 {
			periods = append(periods, fmt.Sprintf("%d (janeiro a dezembro)", period.Year))
		} else {
			for _, month := range period.Months {
				periods = append(periods, financialQueryMonth(month, period.Year))
			}
		}
	}
	scope := "Escopo: " + strings.Join(periods, ", ")
	if query.CategoryName != "" {
		scope += "; categoria " + financialQueryLabel(query.CategoryName)
	}
	if query.PaymentSource != "" {
		scope += "; somente a parte atribuida a " + financialQueryLabel(query.PaymentSource)
	}
	return scope + "."
}

func financialGuidanceCaveat() string {
	return "Valores cadastrados, inclusive futuros, nao comprovam pagamento, recebimento ou despesas essenciais completas. Ausencia de registros nao prova ausencia de gastos; VA/VR nao sao dinheiro livre."
}
