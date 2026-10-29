package assistant

import (
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

var analysisYearPattern = regexp.MustCompile(`\b(20\d{2})\b`)
var analysisRelativeYearPattern = regexp.MustCompile(`\b(?:ano passado|ano anterior|ano que vem|proximo ano|ultimos 12 meses|neste ano|este ano|deste ano|ano atual|ano corrente)\b`)

func assistantNow() time.Time {
	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		location = time.FixedZone("Brasilia", -3*60*60)
	}
	return time.Now().In(location)
}

// Resolve a bounded year window without another model call. The newest explicit
// reference wins; an unrelated older conversation must not pin the current year.
func analysisYears(message string, history []Message, now time.Time) []int {
	years := []int{}
	appendYear := func(year int) {
		if len(years) < 2 && !containsYear(years, year) {
			years = append(years, year)
		}
	}
	appendExplicitYears := func(text string) {
		for _, value := range analysisYearPattern.FindAllString(text, -1) {
			year, _ := strconv.Atoi(value)
			appendYear(year)
		}
	}
	appendExplicitYears(message)
	if len(years) > 0 {
		return years
	}
	// Resolve calendar references together, in their stated order. They override
	// an older conversation year without classifying the financial request.
	relative := strings.ToLower(strings.Join(strings.Fields(message), " "))
	relative = strings.NewReplacer("ó", "o", "ú", "u").Replace(relative)
	for _, reference := range analysisRelativeYearPattern.FindAllString(relative, -1) {
		switch reference {
		case "ano passado", "ano anterior":
			appendYear(now.Year() - 1)
		case "ano que vem", "proximo ano":
			appendYear(now.Year() + 1)
		case "ultimos 12 meses":
			appendYear(now.Year() - 1)
			appendYear(now.Year())
		default:
			appendYear(now.Year())
		}
	}
	if len(years) > 0 {
		return years
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" && analysisYearPattern.MatchString(history[i].Content) {
			appendExplicitYears(history[i].Content)
			return years
		}
	}
	return []int{now.Year()}
}

func containsYear(years []int, year int) bool {
	for _, value := range years {
		if value == year {
			return true
		}
	}
	return false
}

// Reads are tenant-scoped and use one consistent database snapshot. No financial
// writes, private notes, email, tokens or conversation contents enter these facts.
func loadAnalysisFacts(ctx context.Context, db *gorm.DB, userID uint, years []int, now time.Time) (map[string]any, error) {
	if db == nil {
		return nil, nil // Pure interpreter unit tests do not have a database.
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	facts := map[string]any{"as_of": now.Format("2006-01-02"), "years": years}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var categoryRows []categories.Category
		if err := tx.Where("user_id = ?", userID).Order("id").Limit(1001).Find(&categoryRows).Error; err != nil {
			return err
		}
		if len(categoryRows) > 1000 {
			return fmt.Errorf("assistant category context too large")
		}
		names := map[uint]string{}
		for _, category := range categoryRows {
			names[category.ID] = category.Name
		}
		var periods []map[string]any
		for _, year := range years {
			var incomeRows []incomes.Income
			if err := tx.Where("user_id = ? AND year = ?", userID, year).Limit(5001).Find(&incomeRows).Error; err != nil {
				return err
			}
			var expenseRows []expenses.Expense
			query := expenses.ApplyEffectiveYear(tx.Preload("PaymentSplits").Where("user_id = ?", userID), year)
			if err := query.Order("id").Limit(10001).Find(&expenseRows).Error; err != nil {
				return err
			}
			if len(incomeRows) > 5000 || len(expenseRows) > 10000 {
				return fmt.Errorf("assistant annual context too large")
			}
			periods = append(periods, aggregateAnalysisYear(year, incomeRows, expenseRows, names, now))
		}
		facts["periods"] = periods
		// A purchase is different from a single installment. Group only explicit
		// series IDs; never guess that unrelated legacy entries are one purchase.
		var purchases []struct {
			Description            string  `json:"description"`
			TotalAmount            float64 `json:"total_amount"`
			RegisteredInstallments int     `json:"registered_installments"`
		}
		if err := tx.Model(&expenses.Expense{}).
			Select("MIN(description) AS description, SUM(amount) AS total_amount, COUNT(*) AS registered_installments").
			Where("user_id = ? AND type = ? AND series_id <> ''", userID, "Parcelada").
			Group("series_id").Order("SUM(amount) DESC, series_id").Limit(5).Scan(&purchases).Error; err != nil {
			return err
		}
		for i := range purchases {
			purchases[i].Description = boundedAnalysisText(purchases[i].Description, 120)
			purchases[i].TotalAmount = roundMoney(purchases[i].TotalAmount)
		}
		facts["largest_registered_installment_purchases_all_time"] = purchases
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return facts, err
}

func aggregateAnalysisYear(year int, incomeRows []incomes.Income, expenseRows []expenses.Expense, names map[uint]string, now time.Time) map[string]any {
	months := make([]map[string]any, 12)
	for i := range months {
		months[i] = map[string]any{"month": i + 1, "total_income": 0.0, "total_expense": 0.0, "total_cash_income": 0.0, "total_cash_expense": 0.0, "registered": false}
	}
	for _, row := range incomeRows {
		if row.Year != year || row.Month < 1 || row.Month > 12 {
			continue
		}
		month := months[row.Month-1]
		month["registered"] = true
		addAnalysisMoney(month, "total_income", row.Amount)
		if source := normalizeMoneySource(row.Source); source != "va" && source != "vr" {
			addAnalysisMoney(month, "total_cash_income", row.Amount)
		}
	}
	type categoryTotal struct {
		name    string
		amounts [12]float64
	}
	byCategory := map[uint]*categoryTotal{}
	var largestInstallment, largestExpense *expenses.Expense
	for i := range expenseRows {
		row := &expenseRows[i]
		monthNumber, rowYear := expenses.EffectiveMonthYear(*row)
		if rowYear != year || monthNumber < 1 || monthNumber > 12 {
			continue
		}
		month := months[monthNumber-1]
		month["registered"] = true
		addAnalysisMoney(month, "total_expense", row.Amount)
		for _, split := range expenses.PaymentSplitsOrLegacy(*row) {
			if source := normalizeMoneySource(split.PaymentSource); source != "va" && source != "vr" {
				addAnalysisMoney(month, "total_cash_expense", split.Amount)
			}
		}
		category := byCategory[row.CategoryID]
		if category == nil {
			name := names[row.CategoryID]
			if name == "" {
				name = "Sem categoria"
			}
			category = &categoryTotal{name: name}
			byCategory[row.CategoryID] = category
		}
		category.amounts[monthNumber-1] = roundMoney(category.amounts[monthNumber-1] + row.Amount)
		if largestExpense == nil || row.Amount > largestExpense.Amount {
			largestExpense = row
		}
		if normalizeExpenseTypeForCreate(row.Type) == "Parcelada" && (largestInstallment == nil || row.Amount > largestInstallment.Amount) {
			largestInstallment = row
		}
	}
	var totalIncome, totalExpense, futureCashBalance float64
	for i, month := range months {
		income := numberToFloat(month["total_income"])
		expense := numberToFloat(month["total_expense"])
		totalIncome += income
		totalExpense += expense
		month["total_balance"] = roundMoney(income - expense)
		cashIncome := numberToFloat(month["total_cash_income"])
		cashExpense := numberToFloat(month["total_cash_expense"])
		month["total_cash_balance"] = roundMoney(cashIncome - cashExpense)
		if income > 0 {
			month["expense_percentage"] = roundMoney(expense / income * 100)
		}
		if cashIncome > 0 && year == now.Year() && i+1 == int(now.Month()) {
			month["total_spending_target_85_percent"] = roundMoney(cashIncome * .85)
			month["total_saving_target_15_percent"] = roundMoney(cashIncome * .15)
		}
		if year == now.Year() && i+1 >= int(now.Month()) {
			futureCashBalance += cashIncome - cashExpense
		}
	}
	categoryRows := make([]map[string]any, 0, len(byCategory))
	for _, category := range byCategory {
		sum, maxAmount, maxMonth := 0.0, -1.0, 0
		for i, amount := range category.amounts {
			sum += amount
			if amount > maxAmount {
				maxAmount, maxMonth = amount, i+1
			}
		}
		categoryRows = append(categoryRows, map[string]any{"name": category.name, "monthly_amounts_jan_to_dec": category.amounts, "total_amount": roundMoney(sum), "highest_month": maxMonth, "total_highest_month": maxAmount})
	}
	sort.Slice(categoryRows, func(i, j int) bool {
		a, b := numberToFloat(categoryRows[i]["total_amount"]), numberToFloat(categoryRows[j]["total_amount"])
		if a == b {
			return categoryRows[i]["name"].(string) < categoryRows[j]["name"].(string)
		}
		return a > b
	})
	complete := len(categoryRows) <= 40
	if !complete {
		categoryRows = categoryRows[:40]
	}
	registeredMonths := make([]map[string]any, 0, 12)
	for _, month := range months {
		if month["registered"] == true {
			delete(month, "registered")
			registeredMonths = append(registeredMonths, month)
		}
	}
	result := map[string]any{"year": year, "months_with_records": registeredMonths, "total_income": roundMoney(totalIncome), "total_expense": roundMoney(totalExpense), "categories": categoryRows, "categories_complete": complete}
	if year == now.Year() {
		result["remaining_months_including_current"] = 13 - int(now.Month())
		result["total_registered_cash_balance_until_december"] = roundMoney(futureCashBalance)
	}
	for key, row := range map[string]*expenses.Expense{"largest_expense_entry": largestExpense, "largest_installment_entry": largestInstallment} {
		if row != nil {
			month, effectiveYear := expenses.EffectiveMonthYear(*row)
			result[key] = map[string]any{"description": boundedAnalysisText(row.Description, 120), "category": names[row.CategoryID], "amount": row.Amount, "month": month, "year": effectiveYear, "type": row.Type, "installment_number": row.CurrentInstall, "installments": row.Installments}
		}
	}
	return result
}

func addAnalysisMoney(data map[string]any, key string, amount float64) {
	data[key] = roundMoney(numberToFloat(data[key]) + amount)
}

func boundedAnalysisText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}

func analysisFactsJSON(facts map[string]any) string {
	if facts == nil {
		return "null"
	}
	data, err := json.Marshal(facts)
	if err != nil {
		return "null"
	}
	return string(data)
}

func analysisContextForClient(client textGenerator, userID uint, message string, history []Message) (map[string]any, error) {
	ctx := context.Background()
	if router, ok := client.(*openRouterClient); ok {
		ctx = router.ctx
	}
	now := assistantNow()
	return loadAnalysisFacts(ctx, database.DB, userID, analysisYears(message, history, now), now)
}

func analysisReply(intent financialIntent) (bool, string, string, any) {
	calculations, err := verifiedAnalysisCalculations(intent.facts, intent.provided["calculations"])
	validation := map[string]any{"facts": intent.facts, "verified_calculations": calculations}
	data := []byte(analysisFactsJSON(validation))
	if err != nil || intent.facts == nil || strings.TrimSpace(intent.Reply) == "" || !financialNumbersSupported(intent.Reply, data) {
		if intent.Intent == "financial_analysis" {
			if reply, ok := verifiedAnalysisSummary(intent.facts); ok {
				return true, reply, "financial_analysis", map[string]any{
					"status": "read_only", "as_of": intent.facts["as_of"], "years": intent.facts["years"],
					"reply_source": "verified_summary", "analysis_status": "analysis_not_verified",
				}
			}
		}
		return true, "Nao consegui validar essa analise com os dados atuais. Pode indicar o periodo e o que deseja comparar? Nenhum dado foi alterado.", "financial_analysis", map[string]any{"status": "analysis_not_verified"}
	}
	return true, strings.TrimSpace(intent.Reply), "financial_analysis", map[string]any{"status": "read_only", "as_of": intent.facts["as_of"], "years": intent.facts["years"]}
}
