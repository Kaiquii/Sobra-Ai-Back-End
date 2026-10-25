package reports

import (
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(rg *gin.RouterGroup) {
	reportsGroup := rg.Group("/reports")
	{
		reportsGroup.GET("/summary", getMonthlySummary)
		reportsGroup.GET("/categories", getCategorySummary)
		reportsGroup.GET("/chart", getChartData)
		reportsGroup.GET("/yearly-summary", getYearlySummary)
		reportsGroup.GET("/installment-commitments", getInstallmentCommitments)
		reportsGroup.GET("/month-comparison", getMonthComparison)
		reportsGroup.GET("/export", exportReport)
	}
}

func normalizeMoneySource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "salário", "salario":
		return "salario"
	case "adiantamento":
		return "adiantamento"
	case "renda extra", "renda_extra":
		return "renda_extra"
	case "va", "vale alimentação", "vale alimentacao", "vale-alimentação", "vale-alimentacao", "vale_alimentacao":
		return "va"
	case "vr", "vale refeição", "vale refeicao", "vale-refeição", "vale-refeicao", "vale_refeicao":
		return "vr"
	default:
		return ""
	}
}

func getMonthlySummary(c *gin.Context) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}
	userID := userIDObj.(uint)

	monthStr := c.Query("month")
	yearStr := c.Query("year")
	if monthStr == "" || yearStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mês e ano são obrigatórios. Ex: ?month=3&year=2026"})
		return
	}

	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mês inválido"})
		return
	}

	year, err := strconv.Atoi(yearStr)
	if err != nil || year < 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ano inválido"})
		return
	}

	var incomesList []incomes.Income
	if err := database.DB.Where("user_id = ? AND month = ? AND year = ?", userID, month, year).Find(&incomesList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar rendas"})
		return
	}

	var expensesList []expenses.Expense
	if err := expenses.ApplyEffectivePeriod(database.DB.Preload("PaymentSplits").Where("user_id = ?", userID), month, year).Find(&expensesList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar despesas"})
		return
	}

	var totalIncome float64
	var totalExpense float64

	var totalSalary float64
	var totalAdiantamento float64
	var totalRendaExtra float64
	var totalVA, totalVR, spentVA, spentVR float64

	var totalSpentSalary float64
	var totalSpentAdiantamento float64
	var totalSpentRendaExtra float64

	for _, income := range incomesList {
		totalIncome += income.Amount

		switch normalizeMoneySource(income.Source) {
		case "salario":
			totalSalary += income.Amount
		case "adiantamento":
			totalAdiantamento += income.Amount
		case "renda_extra":
			totalRendaExtra += income.Amount
		case "va":
			totalVA += income.Amount
		case "vr":
			totalVR += income.Amount
		}
	}

	for _, expense := range expensesList {
		totalExpense += expense.Amount

		for _, split := range expenses.PaymentSplitsOrLegacy(expense) {
			switch normalizeMoneySource(split.PaymentSource) {
			case "salario":
				totalSpentSalary += split.Amount
			case "adiantamento":
				totalSpentAdiantamento += split.Amount
			case "renda_extra":
				totalSpentRendaExtra += split.Amount
			case "va":
				spentVA += split.Amount
			case "vr":
				spentVR += split.Amount
			}
		}
	}

	restanteSalario := totalSalary - totalSpentSalary
	restanteAdiantamento := totalAdiantamento - totalSpentAdiantamento
	restanteRendaExtra := totalRendaExtra - totalSpentRendaExtra

	totalGeralDisponivel := totalIncome - totalExpense

	c.JSON(http.StatusOK, gin.H{
		"month":                    month,
		"year":                     year,
		"total_income":             totalIncome,
		"total_expense":            totalExpense,
		"total_geral_disponivel":   totalGeralDisponivel,
		"salario":                  totalSalary,
		"adiantamento":             totalAdiantamento,
		"renda_extra_amt":          totalRendaExtra,
		"total_gasto_salario":      totalSpentSalary,
		"total_gasto_adiantamento": totalSpentAdiantamento,
		"total_gasto_renda_extra":  totalSpentRendaExtra,
		"restante_salario":         restanteSalario,
		"restante_adiantamento":    restanteAdiantamento,
		"restante_renda_extra":     restanteRendaExtra,
		"va":                       totalVA,
		"vr":                       totalVR,
		"total_gasto_va":           spentVA,
		"total_gasto_vr":           spentVR,
		"restante_va":              totalVA - spentVA,
		"restante_vr":              totalVR - spentVR,
	})
}

func getCategorySummary(c *gin.Context) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}
	userID := userIDObj.(uint)

	yearStr := c.Query("year")
	monthStr := c.Query("month")

	if yearStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "O ano é obrigatório"})
		return
	}
	year, err := strconv.Atoi(yearStr)
	if err != nil || year < 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ano inválido"})
		return
	}

	month := 0
	if monthStr != "" {
		month, err = strconv.Atoi(monthStr)
		if err != nil || month < 1 || month > 12 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Mês inválido"})
			return
		}
	}

	var expenseItems []expenses.Expense
	query := database.DB.Where("user_id = ?", userID)
	if month > 0 {
		query = expenses.ApplyEffectivePeriod(query, month, year)
	} else {
		query = expenses.ApplyEffectiveYear(query, year)
	}
	if err := query.Find(&expenseItems).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar resumo por categoria"})
		return
	}
	categoryNames, err := loadCategoryNames(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar categorias"})
		return
	}
	totals := make(map[uint]float64)
	for _, expense := range expenseItems {
		totals[expense.CategoryID] += expense.Amount
	}
	results := make([]CategoryResult, 0, len(totals))
	for categoryID, total := range totals {
		name := strings.TrimSpace(categoryNames[categoryID])
		if name == "" {
			name = "Sem categoria"
		}
		results = append(results, CategoryResult{CategoryID: categoryID, CategoryName: name, TotalAmount: total})
	}

	var totalGeral float64
	for _, r := range results {
		totalGeral += r.TotalAmount
	}

	if totalGeral > 0 {
		for i := range results {
			results[i].Percentage = (results[i].TotalAmount / totalGeral) * 100
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].TotalAmount > results[j].TotalAmount })

	c.JSON(http.StatusOK, results)
}

func getChartData(c *gin.Context) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}
	userID := userIDObj.(uint)

	yearStr := c.Query("year")
	if yearStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "O ano é obrigatório. Ex: ?year=2026"})
		return
	}
	year, err := strconv.Atoi(yearStr)
	if err != nil || year < 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ano inválido"})
		return
	}

	var incomesList []incomes.Income
	if err := database.DB.Where("user_id = ? AND year = ?", userID, year).Find(&incomesList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar rendas do ano"})
		return
	}

	var expensesList []expenses.Expense
	if err := expenses.ApplyEffectiveYear(database.DB.Preload("PaymentSplits").Where("user_id = ?", userID), year).Find(&expensesList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar despesas do ano"})
		return
	}

	results := buildChartResults(incomesList, expensesList)
	c.JSON(http.StatusOK, results)
}

func buildChartResults(incomesList []incomes.Income, expensesList []expenses.Expense) []ChartResult {
	if len(incomesList) == 0 && len(expensesList) == 0 {
		return []ChartResult{}
	}

	monthlyData := make(map[int]ChartResult)

	for _, inc := range incomesList {
		data := monthlyData[inc.Month]
		data.Month = inc.Month
		data.Income += inc.Amount
		monthlyData[inc.Month] = data
	}

	for _, exp := range expensesList {
		month, _ := expenses.EffectiveMonthYear(exp)
		data := monthlyData[month]
		data.Month = month
		data.Expense += exp.Amount
		monthlyData[month] = data
	}

	results := make([]ChartResult, 0, 12)
	for i := 1; i <= 12; i++ {
		if data, ok := monthlyData[i]; ok {
			results = append(results, data)
		} else {
			results = append(results, ChartResult{Month: i, Income: 0, Expense: 0})
		}
	}

	return results
}

func getYearlySummary(c *gin.Context) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não identificado"})
		return
	}
	userID := userIDObj.(uint)

	yearStr := c.Query("year")
	if yearStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "O ano é obrigatório. Ex: ?year=2026"})
		return
	}
	year, err := strconv.Atoi(yearStr)
	if err != nil || year < 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ano inválido"})
		return
	}

	var totalIncome float64
	if err := database.DB.Table("incomes").
		Where("user_id = ? AND year = ?", userID, year).
		Select("COALESCE(sum(amount), 0)").Scan(&totalIncome).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar total de rendas do ano"})
		return
	}

	var totalExpense float64
	if err := expenses.ApplyEffectiveYear(database.DB.Table("expenses").Where("user_id = ?", userID), year).
		Select("COALESCE(sum(amount), 0)").Scan(&totalExpense).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar total de despesas do ano"})
		return
	}

	economiaTotal := totalIncome - totalExpense
	mediaMensal := totalExpense / 12

	c.JSON(http.StatusOK, gin.H{
		"year":           year,
		"economia_total": economiaTotal,
		"media_mensal":   mediaMensal,
	})
}

func getMonthComparison(c *gin.Context) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return
	}
	userID := userIDObj.(uint)

	monthStr := c.Query("month")
	yearStr := c.Query("year")
	if monthStr == "" || yearStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mes e ano sao obrigatorios. Ex: ?month=6&year=2026"})
		return
	}

	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mes invalido"})
		return
	}

	year, err := strconv.Atoi(yearStr)
	if err != nil || year < 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ano invalido"})
		return
	}

	comparedMonth, comparedYear, err := comparisonMonthYear(c, month, year)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var currentIncomes []incomes.Income
	if err := database.DB.Where("user_id = ? AND month = ? AND year = ?", userID, month, year).Find(&currentIncomes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar rendas do mes atual"})
		return
	}

	var comparedIncomes []incomes.Income
	if err := database.DB.Where("user_id = ? AND month = ? AND year = ?", userID, comparedMonth, comparedYear).Find(&comparedIncomes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar rendas do mes comparado"})
		return
	}

	var currentExpenses []expenses.Expense
	if err := expenses.ApplyEffectivePeriod(database.DB.Preload("PaymentSplits").Where("user_id = ?", userID), month, year).Find(&currentExpenses).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar despesas do mes atual"})
		return
	}

	var comparedExpenses []expenses.Expense
	if err := expenses.ApplyEffectivePeriod(database.DB.Preload("PaymentSplits").Where("user_id = ?", userID), comparedMonth, comparedYear).Find(&comparedExpenses).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar despesas do mes comparado"})
		return
	}

	categoryNames, err := loadCategoryNames(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar categorias"})
		return
	}

	response := buildMonthComparisonResponse(
		currentIncomes,
		comparedIncomes,
		currentExpenses,
		comparedExpenses,
		categoryNames,
		month,
		year,
		comparedMonth,
		comparedYear,
	)

	c.JSON(http.StatusOK, response)
}

type comparisonTotals struct {
	current  float64
	previous float64
}

func buildMonthComparisonResponse(currentIncomes []incomes.Income, previousIncomes []incomes.Income, currentExpenses []expenses.Expense, previousExpenses []expenses.Expense, categoryNames map[uint]string, currentMonth int, currentYear int, comparedMonth int, comparedYear int) MonthComparisonResponse {
	currentIncomeTotal := sumIncomeAmounts(currentIncomes)
	previousIncomeTotal := sumIncomeAmounts(previousIncomes)
	currentExpenseTotal := sumExpenseAmounts(currentExpenses)
	previousExpenseTotal := sumExpenseAmounts(previousExpenses)
	currentBalance := currentIncomeTotal - currentExpenseTotal
	previousBalance := previousIncomeTotal - previousExpenseTotal

	summary := MonthComparisonSummary{
		CurrentIncome:     roundMoney(currentIncomeTotal),
		PreviousIncome:    roundMoney(previousIncomeTotal),
		IncomeDifference:  roundMoney(currentIncomeTotal - previousIncomeTotal),
		IncomePercentage:  percentageDifference(currentIncomeTotal, previousIncomeTotal),
		IncomeStatus:      comparisonStatus(currentIncomeTotal - previousIncomeTotal),
		CurrentExpense:    roundMoney(currentExpenseTotal),
		PreviousExpense:   roundMoney(previousExpenseTotal),
		ExpenseDifference: roundMoney(currentExpenseTotal - previousExpenseTotal),
		ExpensePercentage: percentageDifference(currentExpenseTotal, previousExpenseTotal),
		ExpenseStatus:     comparisonStatus(currentExpenseTotal - previousExpenseTotal),
		CurrentBalance:    roundMoney(currentBalance),
		PreviousBalance:   roundMoney(previousBalance),
		BalanceDifference: roundMoney(currentBalance - previousBalance),
		BalancePercentage: percentageDifference(currentBalance, previousBalance),
		BalanceStatus:     balanceStatus(currentBalance - previousBalance),
	}

	categoriesComparison := buildCategoryComparisons(currentExpenses, previousExpenses, categoryNames)
	paymentSourcesComparison := buildPaymentSourceComparisons(currentExpenses, previousExpenses)
	expenseTypesComparison := buildExpenseTypeComparisons(currentExpenses, previousExpenses)
	insights := buildMonthComparisonInsights(summary, categoriesComparison)

	return MonthComparisonResponse{
		CurrentMonth:   currentMonth,
		CurrentYear:    currentYear,
		ComparedMonth:  comparedMonth,
		ComparedYear:   comparedYear,
		Summary:        summary,
		Categories:     categoriesComparison,
		PaymentSources: paymentSourcesComparison,
		ExpenseTypes:   expenseTypesComparison,
		Insights:       insights,
	}
}

func buildCategoryComparisons(currentExpenses []expenses.Expense, previousExpenses []expenses.Expense, categoryNames map[uint]string) []MonthComparisonCategory {
	totalsByCategory := make(map[uint]*comparisonTotals)

	for _, expense := range currentExpenses {
		totals := totalsByCategory[expense.CategoryID]
		if totals == nil {
			totals = &comparisonTotals{}
			totalsByCategory[expense.CategoryID] = totals
		}
		totals.current += expense.Amount
	}

	for _, expense := range previousExpenses {
		totals := totalsByCategory[expense.CategoryID]
		if totals == nil {
			totals = &comparisonTotals{}
			totalsByCategory[expense.CategoryID] = totals
		}
		totals.previous += expense.Amount
	}

	comparisons := make([]MonthComparisonCategory, 0, len(totalsByCategory))
	for categoryID, totals := range totalsByCategory {
		categoryName := categoryNames[categoryID]
		if strings.TrimSpace(categoryName) == "" {
			categoryName = "Sem categoria"
		}

		difference := totals.current - totals.previous
		comparisons = append(comparisons, MonthComparisonCategory{
			CategoryID:     categoryID,
			CategoryName:   categoryName,
			CurrentAmount:  roundMoney(totals.current),
			PreviousAmount: roundMoney(totals.previous),
			Difference:     roundMoney(difference),
			Percentage:     percentageDifference(totals.current, totals.previous),
			Status:         comparisonStatus(difference),
		})
	}

	sort.Slice(comparisons, func(i, j int) bool {
		left := math.Abs(comparisons[i].Difference)
		right := math.Abs(comparisons[j].Difference)
		if left != right {
			return left > right
		}
		return comparisons[i].CategoryName < comparisons[j].CategoryName
	})

	return comparisons
}

func buildPaymentSourceComparisons(currentExpenses []expenses.Expense, previousExpenses []expenses.Expense) []MonthComparisonPaymentSource {
	totalsBySource := make(map[string]*comparisonTotals)

	for _, expense := range currentExpenses {
		for _, split := range expenses.PaymentSplitsOrLegacy(expense) {
			source := paymentSourceLabel(split.PaymentSource)
			totals := totalsBySource[source]
			if totals == nil {
				totals = &comparisonTotals{}
				totalsBySource[source] = totals
			}
			totals.current += split.Amount
		}
	}

	for _, expense := range previousExpenses {
		for _, split := range expenses.PaymentSplitsOrLegacy(expense) {
			source := paymentSourceLabel(split.PaymentSource)
			totals := totalsBySource[source]
			if totals == nil {
				totals = &comparisonTotals{}
				totalsBySource[source] = totals
			}
			totals.previous += split.Amount
		}
	}

	comparisons := make([]MonthComparisonPaymentSource, 0, len(totalsBySource))
	for source, totals := range totalsBySource {
		difference := totals.current - totals.previous
		comparisons = append(comparisons, MonthComparisonPaymentSource{
			PaymentSource:  source,
			CurrentAmount:  roundMoney(totals.current),
			PreviousAmount: roundMoney(totals.previous),
			Difference:     roundMoney(difference),
			Percentage:     percentageDifference(totals.current, totals.previous),
			Status:         comparisonStatus(difference),
		})
	}

	sort.Slice(comparisons, func(i, j int) bool {
		left := math.Abs(comparisons[i].Difference)
		right := math.Abs(comparisons[j].Difference)
		if left != right {
			return left > right
		}
		return comparisons[i].PaymentSource < comparisons[j].PaymentSource
	})

	return comparisons
}

func buildExpenseTypeComparisons(currentExpenses []expenses.Expense, previousExpenses []expenses.Expense) []MonthComparisonExpenseType {
	totalsByType := make(map[string]*comparisonTotals)

	for _, expense := range currentExpenses {
		expenseType := expenseTypeLabel(expense.Type)
		totals := totalsByType[expenseType]
		if totals == nil {
			totals = &comparisonTotals{}
			totalsByType[expenseType] = totals
		}
		totals.current += expense.Amount
	}

	for _, expense := range previousExpenses {
		expenseType := expenseTypeLabel(expense.Type)
		totals := totalsByType[expenseType]
		if totals == nil {
			totals = &comparisonTotals{}
			totalsByType[expenseType] = totals
		}
		totals.previous += expense.Amount
	}

	comparisons := make([]MonthComparisonExpenseType, 0, len(totalsByType))
	for expenseType, totals := range totalsByType {
		difference := totals.current - totals.previous
		comparisons = append(comparisons, MonthComparisonExpenseType{
			Type:           expenseType,
			CurrentAmount:  roundMoney(totals.current),
			PreviousAmount: roundMoney(totals.previous),
			Difference:     roundMoney(difference),
			Percentage:     percentageDifference(totals.current, totals.previous),
			Status:         comparisonStatus(difference),
		})
	}

	sort.Slice(comparisons, func(i, j int) bool {
		left := math.Abs(comparisons[i].Difference)
		right := math.Abs(comparisons[j].Difference)
		if left != right {
			return left > right
		}
		return comparisons[i].Type < comparisons[j].Type
	})

	return comparisons
}

func buildMonthComparisonInsights(summary MonthComparisonSummary, categories []MonthComparisonCategory) []string {
	insights := make([]string, 0, 4)

	if summary.ExpenseDifference > 0 {
		insights = append(insights, fmt.Sprintf("Seu gasto total aumentou R$ %.2f em relacao ao mes anterior.", math.Abs(summary.ExpenseDifference)))
	} else if summary.ExpenseDifference < 0 {
		insights = append(insights, fmt.Sprintf("Voce gastou R$ %.2f a menos que no mes anterior.", math.Abs(summary.ExpenseDifference)))
	} else {
		insights = append(insights, "Seu gasto total ficou igual ao mes anterior.")
	}

	if summary.IncomeDifference > 0 {
		insights = append(insights, fmt.Sprintf("Suas receitas aumentaram R$ %.2f.", math.Abs(summary.IncomeDifference)))
	} else if summary.IncomeDifference < 0 {
		insights = append(insights, fmt.Sprintf("Suas receitas cairam R$ %.2f.", math.Abs(summary.IncomeDifference)))
	}

	if summary.BalanceDifference > 0 {
		insights = append(insights, fmt.Sprintf("Seu saldo final melhorou R$ %.2f.", math.Abs(summary.BalanceDifference)))
	} else if summary.BalanceDifference < 0 {
		insights = append(insights, fmt.Sprintf("Seu saldo final piorou R$ %.2f.", math.Abs(summary.BalanceDifference)))
	}

	if category := mostIncreasedCategory(categories); category != nil && category.Difference > 0 {
		insights = append(insights, fmt.Sprintf("%s foi a categoria que mais subiu.", category.CategoryName))
	}

	return insights
}

func mostIncreasedCategory(categories []MonthComparisonCategory) *MonthComparisonCategory {
	var selected *MonthComparisonCategory
	for i := range categories {
		if categories[i].Difference <= 0 {
			continue
		}
		if selected == nil || categories[i].Difference > selected.Difference {
			selected = &categories[i]
		}
	}
	return selected
}

func sumIncomeAmounts(items []incomes.Income) float64 {
	var total float64
	for _, income := range items {
		total += income.Amount
	}
	return total
}

func sumExpenseAmounts(items []expenses.Expense) float64 {
	var total float64
	for _, expense := range items {
		total += expense.Amount
	}
	return total
}

func previousMonthYear(month int, year int) (int, int) {
	if month == 1 {
		return 12, year - 1
	}
	return month - 1, year
}

func comparisonMonthYear(c *gin.Context, currentMonth int, currentYear int) (int, int, error) {
	compareMonthStr := c.Query("compare_month")
	compareYearStr := c.Query("compare_year")
	if compareMonthStr == "" && compareYearStr == "" {
		month, year := previousMonthYear(currentMonth, currentYear)
		return month, year, nil
	}

	if compareMonthStr == "" || compareYearStr == "" {
		return 0, 0, fmt.Errorf("compare_month e compare_year devem ser enviados juntos")
	}

	compareMonth, err := strconv.Atoi(compareMonthStr)
	if err != nil || compareMonth < 1 || compareMonth > 12 {
		return 0, 0, fmt.Errorf("Mes de comparacao invalido")
	}

	compareYear, err := strconv.Atoi(compareYearStr)
	if err != nil || compareYear < 2000 {
		return 0, 0, fmt.Errorf("Ano de comparacao invalido")
	}

	return compareMonth, compareYear, nil
}

func percentageDifference(current float64, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return roundMoney(((current - previous) / math.Abs(previous)) * 100)
}

func comparisonStatus(difference float64) string {
	if difference > 0 {
		return "subiu"
	}
	if difference < 0 {
		return "caiu"
	}
	return "igual"
}

func balanceStatus(difference float64) string {
	if difference > 0 {
		return "melhorou"
	}
	if difference < 0 {
		return "piorou"
	}
	return "igual"
}

func paymentSourceLabel(source string) string {
	switch normalizeMoneySource(source) {
	case "salario":
		return "Salario"
	case "adiantamento":
		return "Adiantamento"
	case "renda_extra":
		return "Renda Extra"
	case "va":
		return "VA"
	case "vr":
		return "VR"
	default:
		source = strings.TrimSpace(source)
		if source == "" {
			return "Nao informado"
		}
		return source
	}
}

func expenseTypeLabel(expenseType string) string {
	normalizedType := strings.TrimSpace(strings.ToLower(expenseType))
	if strings.Contains(normalizedType, "nica") {
		return "Unica"
	}

	switch normalizedType {
	case "fixa":
		return "Fixa"
	case "parcelada":
		return "Parcelada"
	default:
		expenseType = strings.TrimSpace(expenseType)
		if expenseType == "" {
			return "Nao informado"
		}
		return expenseType
	}
}

func getInstallmentCommitments(c *gin.Context) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return
	}
	userID := userIDObj.(uint)

	now := time.Now()
	baseMonth := int(now.Month())
	baseYear := now.Year()

	if monthStr := c.Query("month"); monthStr != "" {
		month, err := strconv.Atoi(monthStr)
		if err != nil || month < 1 || month > 12 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Mes invalido"})
			return
		}
		baseMonth = month
	}

	if yearStr := c.Query("year"); yearStr != "" {
		year, err := strconv.Atoi(yearStr)
		if err != nil || year < 2000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Ano invalido"})
			return
		}
		baseYear = year
	}

	months := 12
	if monthsStr := c.Query("months"); monthsStr != "" {
		parsedMonths, err := strconv.Atoi(monthsStr)
		if err != nil || parsedMonths < 1 || parsedMonths > 60 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Months deve ser um numero entre 1 e 60"})
			return
		}
		months = parsedMonths
	}

	includeCurrentMonthAsPaid := c.Query("include_current_month_as_paid") == "true"

	var installments []expenses.Expense
	if err := database.DB.Preload("PaymentSplits").
		Where("user_id = ? AND type = ?", userID, "Parcelada").
		Order("year asc, month asc, current_install asc, id asc").
		Find(&installments).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar parcelas"})
		return
	}

	categoryNames, err := loadCategoryNames(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar categorias"})
		return
	}

	response := buildInstallmentCommitmentsResponse(installments, categoryNames, baseMonth, baseYear, months, includeCurrentMonthAsPaid)
	c.JSON(http.StatusOK, response)
}

func loadCategoryNames(userID uint) (map[uint]string, error) {
	var categoriesList []categories.Category
	if err := database.DB.Where("user_id = ?", userID).Find(&categoriesList).Error; err != nil {
		return nil, err
	}

	categoryNames := make(map[uint]string, len(categoriesList))
	for _, category := range categoriesList {
		categoryNames[category.ID] = category.Name
	}

	return categoryNames, nil
}

func buildInstallmentCommitmentsResponse(installments []expenses.Expense, categoryNames map[uint]string, baseMonth int, baseYear int, months int, includeCurrentMonthAsPaid bool) InstallmentCommitmentsResponse {
	baseDate := monthDate(baseYear, baseMonth)
	openStartDate := baseDate
	if includeCurrentMonthAsPaid {
		openStartDate = baseDate.AddDate(0, 1, 0)
	}
	lastVisibleDate := openStartDate.AddDate(0, months-1, 0)
	purchasesByKey := make(map[string][]expenses.Expense)
	monthTotals := make(map[string]*InstallmentMonthSummary)

	for _, installment := range installments {
		key := installment.SeriesID
		if key == "" {
			key = legacyInstallmentKey(installment)
		}
		purchasesByKey[key] = append(purchasesByKey[key], installment)

		installmentDate := monthDate(installment.Year, installment.Month)
		if installmentDate.Before(openStartDate) || installmentDate.After(lastVisibleDate) {
			continue
		}

		monthKey := monthMapKey(installment.Year, installment.Month)
		monthSummary := monthTotals[monthKey]
		if monthSummary == nil {
			monthSummary = &InstallmentMonthSummary{
				Month:        installment.Month,
				Year:         installment.Year,
				Installments: []InstallmentItemSummary{},
			}
			monthTotals[monthKey] = monthSummary
		}

		if !installment.IsAdvanced {
			monthSummary.Total += installment.Amount
		}
		monthSummary.Installments = append(monthSummary.Installments, installmentItemSummary(installment, categoryNames))
	}

	purchases := make([]InstallmentPurchaseSummary, 0, len(purchasesByKey))
	summary := InstallmentCommitmentsSummary{}

	for seriesKey, purchaseInstallments := range purchasesByKey {
		sortInstallments(purchaseInstallments)
		purchase := buildPurchaseSummary(seriesKey, purchaseInstallments, categoryNames, openStartDate)

		summary.OriginalTotal += purchase.OriginalTotal
		summary.PaidTotal += purchase.PaidTotal
		summary.RemainingTotal += purchase.RemainingTotal
		summary.PaidInstallments += purchase.PaidInstallments
		summary.RemainingInstallments += purchase.RemainingInstallments

		purchases = append(purchases, purchase)
	}

	sort.Slice(purchases, func(i, j int) bool {
		if purchases[i].RemainingTotal != purchases[j].RemainingTotal {
			return purchases[i].RemainingTotal > purchases[j].RemainingTotal
		}
		return purchases[i].Description < purchases[j].Description
	})

	timeline := make([]InstallmentMonthSummary, 0, months)
	for i := 0; i < months; i++ {
		currentDate := openStartDate.AddDate(0, i, 0)
		month := int(currentDate.Month())
		year := currentDate.Year()
		monthSummary := monthTotals[monthMapKey(year, month)]
		if monthSummary == nil {
			monthSummary = &InstallmentMonthSummary{
				Month:        month,
				Year:         year,
				Installments: []InstallmentItemSummary{},
			}
		}

		sort.Slice(monthSummary.Installments, func(i, j int) bool {
			if monthSummary.Installments[i].Amount != monthSummary.Installments[j].Amount {
				return monthSummary.Installments[i].Amount > monthSummary.Installments[j].Amount
			}
			return monthSummary.Installments[i].Description < monthSummary.Installments[j].Description
		})

		monthSummary.Total = roundMoney(monthSummary.Total)
		timeline = append(timeline, *monthSummary)
	}

	var heaviestMonth *InstallmentHeaviestMonth
	for _, monthSummary := range timeline {
		if monthSummary.Total <= 0 {
			continue
		}
		if heaviestMonth == nil || monthSummary.Total > heaviestMonth.Total {
			heaviestMonth = &InstallmentHeaviestMonth{
				Month: monthSummary.Month,
				Year:  monthSummary.Year,
				Total: monthSummary.Total,
			}
		}
	}

	summary.OriginalTotal = roundMoney(summary.OriginalTotal)
	summary.PaidTotal = roundMoney(summary.PaidTotal)
	summary.RemainingTotal = roundMoney(summary.RemainingTotal)
	summary.TotalPurchases = len(purchases)
	summary.HeaviestMonth = heaviestMonth

	return InstallmentCommitmentsResponse{
		BaseMonth: baseMonth,
		BaseYear:  baseYear,
		Months:    months,
		Summary:   summary,
		Purchases: purchases,
		Timeline:  timeline,
	}
}

func buildPurchaseSummary(seriesKey string, installments []expenses.Expense, categoryNames map[uint]string, baseDate time.Time) InstallmentPurchaseSummary {
	first := installments[0]
	last := installments[len(installments)-1]
	totalInstallments := first.Installments
	if totalInstallments <= 0 {
		totalInstallments = len(installments)
	}

	installmentAmount := first.Amount
	originalTotal := installmentAmount * float64(totalInstallments)
	var paidTotal float64
	var remainingTotal float64
	var paidInstallments int
	var remainingInstallments int
	var nextInstallment *InstallmentItemSummary

	for _, installment := range installments {
		installmentDate := monthDate(installment.Year, installment.Month)
		if installment.IsAdvanced {
			paidTotal += installment.Amount
			paidInstallments++
			continue
		}
		if installmentDate.Before(baseDate) {
			paidTotal += installment.Amount
			paidInstallments++
			continue
		}

		remainingTotal += installment.Amount
		remainingInstallments++
		if nextInstallment == nil {
			item := installmentItemSummary(installment, categoryNames)
			nextInstallment = &item
		}
	}

	return InstallmentPurchaseSummary{
		SeriesID:              seriesKey,
		Description:           first.Description,
		CategoryID:            first.CategoryID,
		CategoryName:          categoryNames[first.CategoryID],
		PaymentSource:         paymentSplitsSummaryLabel(first),
		InstallmentAmount:     roundMoney(installmentAmount),
		OriginalTotal:         roundMoney(originalTotal),
		PaidTotal:             roundMoney(paidTotal),
		RemainingTotal:        roundMoney(remainingTotal),
		PaidInstallments:      paidInstallments,
		RemainingInstallments: remainingInstallments,
		TotalInstallments:     totalInstallments,
		FirstMonth:            first.Month,
		FirstYear:             first.Year,
		LastMonth:             last.Month,
		LastYear:              last.Year,
		NextInstallment:       nextInstallment,
	}
}

func installmentItemSummary(installment expenses.Expense, categoryNames map[uint]string) InstallmentItemSummary {
	return InstallmentItemSummary{
		ID:                 installment.ID,
		SeriesID:           installment.SeriesID,
		Description:        installment.Description,
		CategoryID:         installment.CategoryID,
		CategoryName:       categoryNames[installment.CategoryID],
		PaymentSource:      paymentSplitsSummaryLabel(installment),
		Amount:             roundMoney(installment.Amount),
		Month:              installment.Month,
		Year:               installment.Year,
		CurrentInstallment: installment.CurrentInstall,
		TotalInstallments:  installment.Installments,
		IsAdvanced:         installment.IsAdvanced,
		AdvancedAt:         installment.AdvancedAt,
	}
}

func paymentSplitsSummaryLabel(expense expenses.Expense) string {
	splits := expenses.PaymentSplitsOrLegacy(expense)
	if len(splits) == 0 {
		return paymentSourceLabel(expense.PaymentSource)
	}
	labels := make([]string, 0, len(splits))
	for _, split := range splits {
		labels = append(labels, fmt.Sprintf("%s: %.2f", paymentSourceLabel(split.PaymentSource), roundMoney(split.Amount)))
	}
	return strings.Join(labels, " | ")
}

func sortInstallments(installments []expenses.Expense) {
	sort.Slice(installments, func(i, j int) bool {
		left := monthDate(installments[i].Year, installments[i].Month)
		right := monthDate(installments[j].Year, installments[j].Month)
		if !left.Equal(right) {
			return left.Before(right)
		}
		return installments[i].CurrentInstall < installments[j].CurrentInstall
	})
}

func legacyInstallmentKey(expense expenses.Expense) string {
	return strings.Join([]string{
		expense.Description,
		strconv.FormatUint(uint64(expense.CategoryID), 10),
		expense.PaymentSource,
		strconv.FormatFloat(expense.Amount, 'f', 2, 64),
		strconv.Itoa(expense.Installments),
	}, "|")
}

func monthDate(year int, month int) time.Time {
	return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
}

func monthMapKey(year int, month int) string {
	return strconv.Itoa(year) + "-" + strconv.Itoa(month)
}

func roundMoney(value float64) float64 {
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(value, 'f', 2, 64), 64)
	return rounded
}
