package assistant

import (
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
)

const financialQueryExpenseLimit = 10000

type financialReadPeriod struct {
	Year   int   `json:"year"`
	Months []int `json:"months"`
}

type financialReadQuery struct {
	Periods       []financialReadPeriod `json:"periods"`
	CategoryName  string                `json:"category_name,omitempty"`
	PaymentSource string                `json:"payment_source,omitempty"`
	GroupBy       string                `json:"group_by"`
	Operation     string                `json:"operation"`
	Limit         int                   `json:"limit"`
	Position      int                   `json:"position,omitempty"`
}

type financialReadRow struct {
	Label           string  `json:"label"`
	Amount          float64 `json:"amount"`
	Year            int     `json:"year,omitempty"`
	Month           int     `json:"month,omitempty"`
	ExpenseID       uint    `json:"expense_id,omitempty"`
	SeriesID        string  `json:"series_id,omitempty"`
	RegisteredCount int     `json:"registered_count"`
	Registered      bool    `json:"registered"`
}

type financialQueryClarification struct{ reason, reply string }

func (e *financialQueryClarification) Error() string { return e.reason }

func queryClarification(reason, reply string) error {
	return &financialQueryClarification{reason: reason, reply: reply}
}

// The plan carries scope, never values or SQL. Invalid or unknown fields fail
// closed instead of silently weakening a category, source or period filter.
func parseFinancialReadQuery(raw any) (financialReadQuery, error) {
	var query financialReadQuery
	encoded, err := json.Marshal(raw)
	if err != nil || len(encoded) > 12000 {
		return query, fmt.Errorf("invalid query")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&query); err != nil {
		return query, fmt.Errorf("invalid query shape")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(encoded, &fields) != nil || fields == nil {
		return query, fmt.Errorf("invalid query object")
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return query, fmt.Errorf("null query field")
		}
	}
	var rawPeriods []map[string]json.RawMessage
	if json.Unmarshal(fields["periods"], &rawPeriods) != nil {
		return query, fmt.Errorf("invalid periods")
	}
	for _, period := range rawPeriods {
		if months, present := period["months"]; !present || bytes.Equal(bytes.TrimSpace(months), []byte("null")) {
			return query, fmt.Errorf("missing or null months")
		}
	}
	query.CategoryName = strings.TrimSpace(query.CategoryName)
	query.PaymentSource = strings.TrimSpace(query.PaymentSource)
	if len([]rune(query.CategoryName)) > 255 {
		return query, fmt.Errorf("invalid category")
	}
	if query.PaymentSource != "" {
		if normalizeMoneySource(query.PaymentSource) == "" {
			return query, fmt.Errorf("invalid payment source")
		}
		query.PaymentSource = normalizePaymentSource(query.PaymentSource)
	}
	switch query.GroupBy {
	case "month", "category", "expense", "installment", "purchase", "clarify_installment_or_purchase":
	default:
		return query, fmt.Errorf("invalid grouping")
	}
	switch query.Operation {
	case "list", "compare", "max", "min", "sum", "average", "review", "budget":
	default:
		return query, fmt.Errorf("invalid operation")
	}
	if _, present := fields["limit"]; !present {
		query.Limit = 24
		if query.Operation == "max" || query.Operation == "min" {
			query.Limit = 1
		}
	}
	if query.Limit < 1 || query.Limit > 24 || len(query.Periods) < 1 || len(query.Periods) > 24 {
		return query, fmt.Errorf("invalid query bounds")
	}
	if _, present := fields["position"]; present {
		if query.Position < 1 || query.Position > 24 || (query.Operation != "max" && query.Operation != "min") {
			return query, fmt.Errorf("invalid ranking position")
		}
		// Position denotes one ordinal, never pagination or a top-N count.
		query.Limit = 1
	}
	if query.Operation == "review" && query.GroupBy != "category" {
		return query, fmt.Errorf("review requires categories")
	}
	if query.Operation == "budget" && (query.GroupBy != "month" || query.CategoryName == "") {
		return query, fmt.Errorf("budget requires a category and months")
	}
	years := map[int]map[int]bool{}
	for _, period := range query.Periods {
		if period.Year < 2000 || period.Year > 9999 || len(period.Months) > 12 {
			return query, fmt.Errorf("invalid period")
		}
		if years[period.Year] == nil {
			years[period.Year] = map[int]bool{}
		}
		months := period.Months
		if len(months) == 0 {
			months = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
		}
		for _, month := range months {
			if month < 1 || month > 12 {
				return query, fmt.Errorf("invalid month")
			}
			years[period.Year][month] = true
		}
	}
	if len(years) > 2 {
		return query, fmt.Errorf("too many years")
	}
	query.Periods = nil
	for year, months := range years {
		period := financialReadPeriod{Year: year}
		for month := range months {
			period.Months = append(period.Months, month)
		}
		sort.Ints(period.Months)
		query.Periods = append(query.Periods, period)
	}
	sort.Slice(query.Periods, func(i, j int) bool { return query.Periods[i].Year < query.Periods[j].Year })
	return query, nil
}

func executeFinancialQuery(ctx context.Context, userID uint, raw any) (bool, string, string, any) {
	return executeFinancialQueryDB(ctx, database.DB, userID, raw, assistantNow())
}

func executeFinancialQueryDB(ctx context.Context, db *gorm.DB, userID uint, raw any, now time.Time) (bool, string, string, any) {
	query, err := parseFinancialReadQuery(raw)
	if err != nil {
		log.Printf("Assistant financial_query rejected reason=%s", financialQueryRejectionReason(err))
		return financialQueryNeedsClarification(nil, "invalid_query", "Nao consegui interpretar a consulta com confiabilidade. Pode dizer se quer os valores de cada mes, a soma ou um ranking? Nenhum dado foi alterado.")
	}
	if query.GroupBy == "clarify_installment_or_purchase" {
		return financialQueryNeedsClarification(&query, "installment_or_purchase", "Voce quer comparar o valor de cada parcela cadastrada ou o total das parcelas registradas de cada compra?")
	}
	if db == nil || userID == 0 {
		return financialQueryUnavailable()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var rows []financialReadRow
	var guidanceReply string
	var guidanceResult map[string]any
	guidance := query.Operation == "review" || query.Operation == "budget"
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var categoryID uint
		if query.CategoryName != "" {
			var owned []categories.Category
			if err := tx.Where("user_id = ?", userID).Order("id").Limit(1001).Find(&owned).Error; err != nil {
				return err
			}
			if len(owned) > 1000 {
				return queryClarification("scope_too_large", "Ha categorias demais para resolver esse filtro com confiabilidade. Nao apresentei resultados parciais.")
			}
			categoryID, query.CategoryName, err = resolveFinancialCategoryName(userID, query.CategoryName, owned)
			if err != nil {
				return err
			}
		}
		var entries []expenses.Expense
		var incomeRows []incomes.Income
		for _, period := range query.Periods {
			var annual []expenses.Expense
			if err := expenses.ApplyEffectiveYear(tx.Preload("PaymentSplits").Where("user_id = ?", userID), period.Year).Order("id").Limit(financialQueryExpenseLimit + 1).Find(&annual).Error; err != nil {
				return err
			}
			if len(annual) > financialQueryExpenseLimit {
				return queryClarification("scope_too_large", "Esse ano tem mais lancamentos do que consigo comparar de uma vez. A consulta nao foi apresentada com valores parciais.")
			}
			entries = append(entries, annual...)
			if guidance {
				var annualIncome []incomes.Income
				if err := tx.Where("user_id = ? AND year = ?", userID, period.Year).Limit(5001).Find(&annualIncome).Error; err != nil {
					return err
				}
				if len(annualIncome) > 5000 {
					return queryClarification("scope_too_large", "Esse periodo tem mais rendas do que consigo analisar de uma vez. Nao apresentei totais parciais.")
				}
				incomeRows = append(incomeRows, annualIncome...)
			}
		}
		series, err := financialQueryPurchaseSeries(query, userID, categoryID, entries)
		if err != nil {
			return err
		}
		var purchaseEntries []expenses.Expense
		if len(series) > 0 {
			// Fetch complete registered series, including installments outside the
			// requested periods. A bounded failure never masquerades as a total.
			if err := tx.Preload("PaymentSplits").Where("user_id = ? AND series_id IN ?", userID, series).Order("id").Limit(financialQueryExpenseLimit + 1).Find(&purchaseEntries).Error; err != nil {
				return err
			}
			if len(purchaseEntries) > financialQueryExpenseLimit {
				return queryClarification("purchase_scope_too_large", "As compras selecionadas tem mais parcelas do que consigo somar de uma vez. Consulte um periodo ou categoria menor; nao apresentei totais parciais.")
			}
		}
		ids := map[uint]bool{}
		for _, entry := range entries {
			ids[entry.CategoryID] = true
		}
		var categoryIDs []uint
		for id := range ids {
			categoryIDs = append(categoryIDs, id)
		}
		names := map[uint]string{}
		if len(categoryIDs) > 0 {
			var categoryRows []categories.Category
			if err := tx.Where("user_id = ? AND id IN ?", userID, categoryIDs).Find(&categoryRows).Error; err != nil {
				return err
			}
			for _, category := range categoryRows {
				names[category.ID] = category.Name
			}
		}
		if categoryID != 0 {
			names[categoryID] = query.CategoryName
		}
		if guidance {
			guidanceReply, guidanceResult, err = renderFinancialGuidance(query, userID, entries, incomeRows, names, now)
			return err
		}
		rows, err = aggregateFinancialReadQuery(query, userID, categoryID, entries, purchaseEntries, names)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		var clarification *financialQueryClarification
		if errors.As(err, &clarification) {
			return financialQueryNeedsClarification(&query, clarification.reason, clarification.reply)
		}
		return financialQueryUnavailable()
	}
	if guidance {
		return true, guidanceReply, "financial_analysis", guidanceResult
	}
	shown, total, matched := finalizeFinancialReadRows(query, rows)
	limited := (query.Operation != "sum" && query.Operation != "average") && len(shown) < matched
	result := map[string]any{
		"status": "read_only", "query": query, "rows": shown, "total_amount": total,
		"matched_rows": matched, "returned_rows": len(shown), "limited": limited,
		"coverage_complete": true, "as_of": now.Format("2006-01-02"),
	}
	if query.GroupBy == "purchase" {
		result["purchase_basis"] = "all_registered_installments_of_purchases_with_matching_period_entries"
	}
	if query.GroupBy == "month" && query.Operation == "sum" && len(rows) > 1 {
		result["monthly_breakdown"] = rows
	}
	return true, financialQueryReply(query, shown, total, matched, limited, rows), "financial_analysis", result
}

func financialQueryNeedsClarification(query *financialReadQuery, reason, reply string) (bool, string, string, any) {
	result := map[string]any{"status": "needs_clarification", "reason": reason}
	if query != nil {
		result["query"] = *query
	}
	return true, reply, "financial_analysis", result
}

func financialQueryUnavailable() (bool, string, string, any) {
	return true, "Nao consegui consultar seus dados agora. Tente novamente; nenhum dado foi alterado.", "financial_analysis", map[string]any{"status": "unavailable", "error_code": "assistant_data_unavailable"}
}

func financialQueryContainsPeriod(query financialReadQuery, row expenses.Expense) bool {
	month, year := expenses.EffectiveMonthYear(row)
	for _, period := range query.Periods {
		if period.Year == year {
			for _, selectedMonth := range period.Months {
				if selectedMonth == month {
					return true
				}
			}
		}
	}
	return false
}

func financialQueryCents(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1e10 || math.Abs(value*100-math.Round(value*100)) > .0001 {
		return 0, fmt.Errorf("invalid registered amount")
	}
	return int64(math.Round(value * 100)), nil
}

func financialQueryEntryAmount(row expenses.Expense, source string) (int64, error) {
	total, err := financialQueryCents(row.Amount)
	if err != nil || source == "" {
		return total, err
	}
	var splitTotal, selected int64
	for _, split := range expenses.PaymentSplitsOrLegacy(row) {
		amount, err := financialQueryCents(split.Amount)
		if err != nil || amount < 0 {
			return 0, fmt.Errorf("invalid registered split")
		}
		splitTotal += amount
		if normalizeMoneySource(split.PaymentSource) == normalizeMoneySource(source) {
			selected += amount
		}
	}
	if len(row.PaymentSplits) > 0 && splitTotal != total {
		return 0, fmt.Errorf("inconsistent registered splits")
	}
	return selected, nil
}

func financialQueryPurchaseSeries(query financialReadQuery, userID, categoryID uint, rows []expenses.Expense) ([]string, error) {
	if query.GroupBy != "purchase" {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row.UserID != userID || (categoryID != 0 && row.CategoryID != categoryID) || !financialQueryContainsPeriod(query, row) || normalizeExpenseTypeForCreate(row.Type) != "Parcelada" {
			continue
		}
		amount, err := financialQueryEntryAmount(row, query.PaymentSource)
		if err != nil {
			return nil, err
		}
		if query.PaymentSource != "" && amount == 0 {
			continue
		}
		if strings.TrimSpace(row.SeriesID) == "" {
			return nil, queryClarification("unidentified_purchase", "Ha parcelas cadastradas sem identificacao de compra nesse periodo. Posso comparar as parcelas, mas nao determinar com seguranca o total de cada compra.")
		}
		seen[row.SeriesID] = true
	}
	series := make([]string, 0, len(seen))
	for id := range seen {
		series = append(series, id)
	}
	sort.Strings(series)
	return series, nil
}

func aggregateFinancialReadQuery(query financialReadQuery, userID, categoryID uint, entries, purchaseEntries []expenses.Expense, names map[uint]string) ([]financialReadRow, error) {
	grouped := map[string]*financialReadRow{}
	amounts := map[string]int64{}
	if query.GroupBy == "month" {
		for _, period := range query.Periods {
			for _, month := range period.Months {
				key := fmt.Sprintf("%04d-%02d", period.Year, month)
				grouped[key] = &financialReadRow{Label: financialQueryMonth(month, period.Year), Month: month, Year: period.Year}
			}
		}
	}
	var series map[string]bool
	rows := entries
	if query.GroupBy == "purchase" {
		ids, err := financialQueryPurchaseSeries(query, userID, categoryID, entries)
		if err != nil {
			return nil, err
		}
		series = map[string]bool{}
		for _, id := range ids {
			series[id] = true
		}
		rows = purchaseEntries
	}
	for _, row := range rows {
		if row.UserID != userID {
			continue
		}
		if query.GroupBy == "purchase" {
			if !series[row.SeriesID] || normalizeExpenseTypeForCreate(row.Type) != "Parcelada" {
				continue
			}
		} else if (categoryID != 0 && row.CategoryID != categoryID) || !financialQueryContainsPeriod(query, row) {
			continue
		}
		if query.GroupBy == "installment" && normalizeExpenseTypeForCreate(row.Type) != "Parcelada" {
			continue
		}
		amount, err := financialQueryEntryAmount(row, query.PaymentSource)
		if err != nil {
			return nil, err
		}
		if query.PaymentSource != "" && amount == 0 {
			continue
		}
		month, year := expenses.EffectiveMonthYear(row)
		key, label := fmt.Sprintf("expense-%d", row.ID), row.Description
		switch query.GroupBy {
		case "month":
			key, label = fmt.Sprintf("%04d-%02d", year, month), financialQueryMonth(month, year)
		case "category":
			key, label = fmt.Sprintf("category-%d", row.CategoryID), names[row.CategoryID]
			if label == "" {
				label = "Sem categoria"
			}
		case "purchase":
			key = row.SeriesID
		}
		item := grouped[key]
		if item == nil {
			item = &financialReadRow{Label: label}
			if query.GroupBy == "expense" || query.GroupBy == "installment" {
				item.ExpenseID, item.Month, item.Year = row.ID, month, year
			}
			if query.GroupBy == "purchase" {
				item.SeriesID = row.SeriesID
			}
			grouped[key] = item
		}
		// A stable representative label must not depend on database row order.
		if query.GroupBy == "purchase" && row.Description < item.Label {
			item.Label = row.Description
		}
		item.Registered, item.RegisteredCount = true, item.RegisteredCount+1
		amounts[key] += amount
	}
	result := make([]financialReadRow, 0, len(grouped))
	for key, row := range grouped {
		row.Amount = float64(amounts[key]) / 100
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		if query.GroupBy == "month" {
			if result[i].Year != result[j].Year {
				return result[i].Year < result[j].Year
			}
			return result[i].Month < result[j].Month
		}
		return financialReadRowLess(result[i], result[j], false)
	})
	return result, nil
}

func financialReadRowLess(a, b financialReadRow, ascending bool) bool {
	if a.Amount != b.Amount {
		if ascending {
			return a.Amount < b.Amount
		}
		return a.Amount > b.Amount
	}
	if a.Year != b.Year {
		return a.Year < b.Year
	}
	if a.Month != b.Month {
		return a.Month < b.Month
	}
	if a.Label != b.Label {
		return a.Label < b.Label
	}
	if a.ExpenseID != b.ExpenseID {
		return a.ExpenseID < b.ExpenseID
	}
	return a.SeriesID < b.SeriesID
}

func finalizeFinancialReadRows(query financialReadQuery, rows []financialReadRow) ([]financialReadRow, float64, int) {
	shown := append([]financialReadRow{}, rows...)
	var totalCents int64
	registered, registeredCount := false, 0
	for _, row := range rows {
		totalCents += int64(math.Round(row.Amount * 100))
		registered = registered || row.Registered
		registeredCount += row.RegisteredCount
	}
	total := float64(totalCents) / 100
	matched := len(rows)
	if query.Operation == "sum" || query.Operation == "average" {
		amount, label := total, "Total cadastrado"
		if query.Operation == "average" {
			label = "Media dos totais cadastrados"
			if matched > 0 {
				amount = roundMoney(total / float64(matched))
			}
		}
		return []financialReadRow{{Label: label, Amount: amount, Registered: registered, RegisteredCount: registeredCount}}, total, matched
	}
	if query.Operation == "max" || query.Operation == "min" {
		// A missing month's zero is not evidence of the lowest real expenditure.
		shown = shown[:0]
		for _, row := range rows {
			if row.Registered {
				shown = append(shown, row)
			}
		}
		matched = len(shown)
		sort.Slice(shown, func(i, j int) bool { return financialReadRowLess(shown[i], shown[j], query.Operation == "min") })
		if query.Position > 0 {
			if query.Position > len(shown) {
				return []financialReadRow{}, total, matched
			}
			shown = shown[query.Position-1 : query.Position]
		}
	}
	if !(query.GroupBy == "month" && query.Operation == "compare") && len(shown) > query.Limit {
		shown = shown[:query.Limit]
	}
	return shown, total, matched
}

func financialQueryMonth(month, year int) string {
	names := [...]string{"", "janeiro", "fevereiro", "marco", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}
	if month < 1 || month > 12 {
		return fmt.Sprintf("%d/%d", month, year)
	}
	return fmt.Sprintf("%s/%d", names[month], year)
}

func financialQueryLabel(label string) string {
	label = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, label)
	label = strings.Join(strings.Fields(label), " ")
	if len([]rune(label)) > 120 {
		label = string([]rune(label)[:120]) + "…"
	}
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "`", "\\`", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "#", "\\#", ">", "\\>", "!", "\\!", "|", "\\|", "~", "\\~", "<", "\\<").Replace(label)
}

func financialQueryReply(query financialReadQuery, rows []financialReadRow, total float64, matched int, limited bool, breakdown ...[]financialReadRow) string {
	var scope []string
	for _, period := range query.Periods {
		if len(period.Months) == 12 {
			scope = append(scope, fmt.Sprintf("%d (janeiro a dezembro)", period.Year))
		} else {
			for _, month := range period.Months {
				scope = append(scope, financialQueryMonth(month, period.Year))
			}
		}
	}
	header := "Despesas cadastradas"
	if query.GroupBy == "installment" {
		header = "Parcelas cadastradas"
	} else if query.GroupBy == "purchase" {
		header = "Compras com parcelas cadastradas no periodo"
	}
	if query.CategoryName != "" {
		header += " de **" + financialQueryLabel(query.CategoryName) + "**"
	}
	header += " em " + strings.Join(scope, ", ")
	if query.PaymentSource != "" {
		header += ", considerando somente a parte atribuida a " + financialQueryLabel(query.PaymentSource)
	}
	var lines []string
	lines = append(lines, header+".")
	if len(rows) == 0 && query.Position > 0 && matched > 0 {
		lines = append(lines, fmt.Sprintf("Encontrei %d grupos com registros; nao ha um valor na posicao %d do ranking solicitado.", matched, query.Position))
	} else if len(rows) == 0 {
		lines = append(lines, "Nao encontrei lancamentos cadastrados para essa consulta; isso nao garante ausencia de gastos reais.")
	} else {
		if query.Position > 0 {
			order := "maior"
			if query.Operation == "min" {
				order = "menor"
			}
			lines = append(lines, fmt.Sprintf("%dº %s valor cadastrado:", query.Position, order))
		} else if query.Operation == "max" {
			lines = append(lines, "Maiores valores cadastrados:")
		} else if query.Operation == "min" {
			lines = append(lines, "Menores valores cadastrados entre os grupos com lancamentos:")
		}
		for _, row := range rows {
			label := financialQueryLabel(row.Label)
			if label == "" {
				label = "Sem descricao"
			}
			if row.ExpenseID != 0 {
				label += " (" + financialQueryMonth(row.Month, row.Year) + ")"
			}
			line := "- **" + label + ":** R$ " + formatMoney(row.Amount)
			if query.GroupBy == "purchase" && query.Operation != "sum" && query.Operation != "average" {
				line += fmt.Sprintf(" em %d parcelas registradas", row.RegisteredCount)
			}
			if query.GroupBy == "month" && !row.Registered {
				line += " (sem lancamentos cadastrados; nao significa ausencia de gastos reais)"
			}
			lines = append(lines, line+".")
		}
		if query.GroupBy == "month" && query.Operation == "sum" && len(breakdown) > 0 && len(breakdown[0]) > 1 {
			lines = append(lines, "Valores por mes no mesmo escopo:")
			for _, month := range breakdown[0][:min(6, len(breakdown[0]))] {
				line := "- **" + financialQueryMonth(month.Month, month.Year) + ":** R$ " + formatMoney(month.Amount)
				if !month.Registered {
					line += " (sem lancamentos cadastrados)"
				}
				lines = append(lines, line+".")
			}
			if len(breakdown[0]) > 6 {
				lines = append(lines, fmt.Sprintf("Detalhes dos primeiros 6 de %d meses; a soma inclui todos os meses pedidos.", len(breakdown[0])))
			}
		}
		if query.Operation != "sum" && query.Operation != "average" {
			lines = append(lines, "Total cadastrado no escopo: **R$ "+formatMoney(total)+"**.")
		}
		if query.Operation == "average" {
			lines = append(lines, fmt.Sprintf("Media de %d grupos solicitados; meses sem cadastro contam como zero quando o agrupamento e mensal.", matched))
		}
	}
	if limited {
		lines = append(lines, fmt.Sprintf("Exibindo %d de %d grupos; os totais consideram todo o escopo consultado.", len(rows), matched))
	}
	if query.GroupBy == "purchase" {
		lines = append(lines, "Os valores somam as parcelas registradas dessas compras em todos os anos, inclusive fora do periodo selecionado. Nao garantem o preco original se houve exclusoes.")
	}
	lines = append(lines, "Lancamentos cadastrados, inclusive futuros, nao significam valores ja pagos.")
	return strings.Join(lines, "\n\n")
}
