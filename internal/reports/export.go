package reports

import (
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	exportTypeExpenses               = "expenses"
	exportTypeIncomes                = "incomes"
	exportTypeCategories             = "categories"
	exportTypeSummary                = "summary"
	exportTypeMonthComparison        = "month_comparison"
	exportTypeInstallmentCommitments = "installment_commitments"
	exportTypeFullReport             = "full_report"
)

var exportFileLabels = map[string]string{
	exportTypeExpenses:               "despesas",
	exportTypeIncomes:                "receitas",
	exportTypeCategories:             "categorias",
	exportTypeSummary:                "resumo",
	exportTypeMonthComparison:        "comparativo",
	exportTypeInstallmentCommitments: "compromissos-parcelados",
	exportTypeFullReport:             "completo",
}

type exportOptions struct {
	ReportType              string
	Format                  string
	Month                   int
	Year                    int
	CompareMonth            int
	CompareYear             int
	Months                  int
	IncludeCurrentMonthPaid bool
}

type monthlyExportSummary struct {
	VA, VR, SpentVA, SpentVR, RemainingVA, RemainingVR float64
	TotalIncome                                        float64
	TotalExpense                                       float64
	Balance                                            float64
	Salary                                             float64
	Advance                                            float64
	ExtraIncome                                        float64
	SpentSalary                                        float64
	SpentAdvance                                       float64
	SpentExtraIncome                                   float64
	RemainingSalary                                    float64
	RemainingAdvance                                   float64
	RemainingExtraIncome                               float64
}

type exportDataset struct {
	Options         exportOptions
	Incomes         []incomes.Income
	Expenses        []expenses.Expense
	CategoryNames   map[uint]string
	CategorySummary []CategoryResult
	Summary         monthlyExportSummary
	Comparison      *MonthComparisonResponse
	Commitments     *InstallmentCommitmentsResponse
}

func exportReport(c *gin.Context) {
	userIDObj, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return
	}
	userID, ok := userIDObj.(uint)
	if !ok || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuario nao identificado"})
		return
	}

	options, err := parseExportOptions(c.Request.URL.Query())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dataset, err := loadExportDataset(userID, options)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao preparar exportacao"})
		return
	}

	content, contentType, extension, err := generateExportFile(dataset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao gerar arquivo de exportacao"})
		return
	}

	filename := fmt.Sprintf("relatorio-%s-%04d-%02d.%s", exportFileLabels[options.ReportType], options.Year, options.Month, extension)
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, contentType, content)
}

func generateExportFile(dataset exportDataset) ([]byte, string, string, error) {
	switch dataset.Options.Format {
	case "csv":
		content, err := generateExportCSV(dataset)
		return content, "text/csv; charset=utf-8", "csv", err
	case "xlsx":
		content, err := generateExportXLSX(dataset)
		return content, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx", err
	case "pdf":
		content, err := generateExportPDF(dataset)
		return content, "application/pdf", "pdf", err
	default:
		return nil, "", "", fmt.Errorf("unsupported export format")
	}
}

func parseExportOptions(values url.Values) (exportOptions, error) {
	options := exportOptions{
		ReportType: strings.TrimSpace(values.Get("type")),
		Format:     strings.TrimSpace(strings.ToLower(values.Get("format"))),
		Months:     12,
	}

	if options.ReportType == "" {
		return options, fmt.Errorf("Tipo de exportacao e obrigatorio")
	}
	if _, valid := exportFileLabels[options.ReportType]; !valid {
		return options, fmt.Errorf("Tipo de exportacao invalido")
	}

	if options.Format == "" {
		return options, fmt.Errorf("Formato de exportacao e obrigatorio. Use csv, xlsx ou pdf")
	}
	if options.Format != "csv" && options.Format != "xlsx" && options.Format != "pdf" {
		return options, fmt.Errorf("Formato invalido. Use csv, xlsx ou pdf")
	}

	month, err := strconv.Atoi(values.Get("month"))
	if err != nil || month < 1 || month > 12 {
		return options, fmt.Errorf("Mes e ano sao obrigatorios e devem ser validos")
	}
	year, err := strconv.Atoi(values.Get("year"))
	if err != nil || year < 2000 {
		return options, fmt.Errorf("Mes e ano sao obrigatorios e devem ser validos")
	}
	options.Month = month
	options.Year = year

	if options.ReportType == exportTypeMonthComparison || options.ReportType == exportTypeFullReport {
		compareMonth, compareYear, err := parseComparisonPeriod(values, month, year)
		if err != nil {
			return options, err
		}
		options.CompareMonth = compareMonth
		options.CompareYear = compareYear
	}

	if options.ReportType == exportTypeInstallmentCommitments || options.ReportType == exportTypeFullReport {
		if monthsValue := values.Get("months"); monthsValue != "" {
			months, err := strconv.Atoi(monthsValue)
			if err != nil || months < 1 || months > 60 {
				return options, fmt.Errorf("Months deve ser um numero entre 1 e 60")
			}
			options.Months = months
		}

		if paidValue := values.Get("include_current_month_as_paid"); paidValue != "" {
			paid, err := strconv.ParseBool(paidValue)
			if err != nil {
				return options, fmt.Errorf("include_current_month_as_paid deve ser true ou false")
			}
			options.IncludeCurrentMonthPaid = paid
		}
	}

	return options, nil
}

func parseComparisonPeriod(values url.Values, month int, year int) (int, int, error) {
	monthValue := values.Get("compare_month")
	yearValue := values.Get("compare_year")
	if monthValue == "" && yearValue == "" {
		previousMonth, previousYear := previousMonthYear(month, year)
		return previousMonth, previousYear, nil
	}
	if monthValue == "" || yearValue == "" {
		return 0, 0, fmt.Errorf("compare_month e compare_year devem ser enviados juntos")
	}

	compareMonth, err := strconv.Atoi(monthValue)
	if err != nil || compareMonth < 1 || compareMonth > 12 {
		return 0, 0, fmt.Errorf("Mes de comparacao invalido")
	}
	compareYear, err := strconv.Atoi(yearValue)
	if err != nil || compareYear < 2000 {
		return 0, 0, fmt.Errorf("Ano de comparacao invalido")
	}
	return compareMonth, compareYear, nil
}

func loadExportDataset(userID uint, options exportOptions) (exportDataset, error) {
	dataset := exportDataset{Options: options}
	needsMonthlyData := options.ReportType != exportTypeInstallmentCommitments

	if needsMonthlyData {
		if err := database.DB.Where("user_id = ? AND month = ? AND year = ?", userID, options.Month, options.Year).
			Order("month asc, year asc, id asc").Find(&dataset.Incomes).Error; err != nil {
			return dataset, err
		}
		if err := expenses.ApplyEffectivePeriod(database.DB.Preload("PaymentSplits").Where("user_id = ?", userID), options.Month, options.Year).
			Order("date asc, id asc").Find(&dataset.Expenses).Error; err != nil {
			return dataset, err
		}
	}

	categoryNames, err := loadCategoryNames(userID)
	if err != nil {
		return dataset, err
	}
	dataset.CategoryNames = categoryNames
	dataset.CategorySummary = buildExportCategorySummary(dataset.Expenses, categoryNames)
	dataset.Summary = buildMonthlyExportSummary(dataset.Incomes, dataset.Expenses)

	if options.ReportType == exportTypeMonthComparison || options.ReportType == exportTypeFullReport {
		var comparedIncomes []incomes.Income
		if err := database.DB.Where("user_id = ? AND month = ? AND year = ?", userID, options.CompareMonth, options.CompareYear).
			Find(&comparedIncomes).Error; err != nil {
			return dataset, err
		}
		var comparedExpenses []expenses.Expense
		if err := expenses.ApplyEffectivePeriod(database.DB.Preload("PaymentSplits").Where("user_id = ?", userID), options.CompareMonth, options.CompareYear).
			Find(&comparedExpenses).Error; err != nil {
			return dataset, err
		}
		comparison := buildMonthComparisonResponse(dataset.Incomes, comparedIncomes, dataset.Expenses, comparedExpenses, categoryNames, options.Month, options.Year, options.CompareMonth, options.CompareYear)
		dataset.Comparison = &comparison
	}

	if options.ReportType == exportTypeInstallmentCommitments || options.ReportType == exportTypeFullReport {
		var installments []expenses.Expense
		if err := database.DB.Preload("PaymentSplits").Where("user_id = ? AND type = ?", userID, "Parcelada").
			Order("year asc, month asc, current_install asc, id asc").Find(&installments).Error; err != nil {
			return dataset, err
		}
		commitments := buildInstallmentCommitmentsResponse(installments, categoryNames, options.Month, options.Year, options.Months, options.IncludeCurrentMonthPaid)
		dataset.Commitments = &commitments
	}

	return dataset, nil
}

func buildMonthlyExportSummary(incomeItems []incomes.Income, expenseItems []expenses.Expense) monthlyExportSummary {
	result := monthlyExportSummary{}
	for _, income := range incomeItems {
		result.TotalIncome += income.Amount
		switch normalizeMoneySource(income.Source) {
		case "salario":
			result.Salary += income.Amount
		case "adiantamento":
			result.Advance += income.Amount
		case "renda_extra":
			result.ExtraIncome += income.Amount
		case "va":
			result.VA += income.Amount
		case "vr":
			result.VR += income.Amount
		}
	}
	for _, expense := range expenseItems {
		result.TotalExpense += expense.Amount
		for _, split := range expenses.PaymentSplitsOrLegacy(expense) {
			switch normalizeMoneySource(split.PaymentSource) {
			case "salario":
				result.SpentSalary += split.Amount
			case "adiantamento":
				result.SpentAdvance += split.Amount
			case "renda_extra":
				result.SpentExtraIncome += split.Amount
			case "va":
				result.SpentVA += split.Amount
			case "vr":
				result.SpentVR += split.Amount
			}
		}
	}
	result.Balance = result.TotalIncome - result.TotalExpense
	result.RemainingSalary = result.Salary - result.SpentSalary
	result.RemainingAdvance = result.Advance - result.SpentAdvance
	result.RemainingExtraIncome = result.ExtraIncome - result.SpentExtraIncome
	result.RemainingVA = result.VA - result.SpentVA
	result.RemainingVR = result.VR - result.SpentVR
	return result
}

func buildExportCategorySummary(items []expenses.Expense, categoryNames map[uint]string) []CategoryResult {
	totals := make(map[uint]float64)
	var overall float64
	for _, expense := range items {
		totals[expense.CategoryID] += expense.Amount
		overall += expense.Amount
	}

	results := make([]CategoryResult, 0, len(totals))
	for categoryID, total := range totals {
		name := strings.TrimSpace(categoryNames[categoryID])
		if name == "" {
			name = "Sem categoria"
		}
		percentage := 0.0
		if overall != 0 {
			percentage = total / overall * 100
		}
		results = append(results, CategoryResult{CategoryID: categoryID, CategoryName: name, TotalAmount: roundMoney(total), Percentage: roundMoney(percentage)})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].TotalAmount != results[j].TotalAmount {
			return results[i].TotalAmount > results[j].TotalAmount
		}
		return results[i].CategoryName < results[j].CategoryName
	})
	return results
}

func generateExportCSV(dataset exportDataset) ([]byte, error) {
	buffer := &bytes.Buffer{}
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(buffer)
	writer.Comma = ';'

	var err error
	switch dataset.Options.ReportType {
	case exportTypeExpenses:
		err = writeExpensesCSV(writer, dataset.Expenses, dataset.CategoryNames)
	case exportTypeIncomes:
		err = writeIncomesCSV(writer, dataset.Incomes)
	case exportTypeCategories:
		err = writeCategoriesCSV(writer, dataset.CategorySummary)
	case exportTypeSummary:
		err = writeSummaryCSV(writer, dataset.Summary)
	case exportTypeMonthComparison:
		err = writeMonthComparisonCSV(writer, dataset.Comparison)
	case exportTypeInstallmentCommitments:
		err = writeInstallmentCommitmentsCSV(writer, dataset.Commitments)
	case exportTypeFullReport:
		err = writeFullReportCSV(writer, dataset)
	default:
		err = fmt.Errorf("unsupported export type")
	}
	if err != nil {
		return nil, err
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeExpensesCSV(writer *csv.Writer, items []expenses.Expense, categoryNames map[uint]string) error {
	if err := writer.Write([]string{"Data", "Descricao", "Categoria", "Fonte de Pagamento", "Tipo", "Parcela", "Valor", "Observacoes", "Data considerada", "Adiantamento"}); err != nil {
		return err
	}
	for _, expense := range items {
		installment := ""
		if expenseTypeLabel(expense.Type) == "Parcelada" {
			installment = formatInstallment(expense.CurrentInstall, expense.Installments)
		}
		category := strings.TrimSpace(categoryNames[expense.CategoryID])
		if category == "" {
			category = "Sem categoria"
		}
		if err := writer.Write([]string{expense.Date.Format("2006-01-02"), safeCSVText(expense.Description), safeCSVText(category), safeCSVText(paymentSplitsLabel(expense)), safeCSVText(expenseTypeLabel(expense.Type)), installment, formatDecimal(expense.Amount), safeCSVText(expense.Notes), expenseFinancialDate(expense).Format("2006-01-02"), safeCSVText(expenseAdvanceLabel(expense))}); err != nil {
			return err
		}
	}
	return nil
}

func expenseFinancialDate(expense expenses.Expense) time.Time {
	if expense.IsAdvanced && expense.AdvancedAt != nil {
		return *expense.AdvancedAt
	}
	return expense.Date
}

func expenseAdvanceLabel(expense expenses.Expense) string {
	if !expense.IsAdvanced || expense.AdvancedAt == nil {
		return ""
	}
	return "Adiantada em " + expense.AdvancedAt.In(time.Local).Format("02/01/2006")
}

func paymentSplitsLabel(expense expenses.Expense) string {
	splits := expenses.PaymentSplitsOrLegacy(expense)
	labels := make([]string, 0, len(splits))
	for _, split := range splits {
		labels = append(labels, fmt.Sprintf("%s: %s", paymentSourceLabel(split.PaymentSource), formatDecimal(split.Amount)))
	}
	if len(labels) == 0 {
		return paymentSourceLabel(expense.PaymentSource)
	}
	return strings.Join(labels, " | ")
}

func writeIncomesCSV(writer *csv.Writer, items []incomes.Income) error {
	if err := writer.Write([]string{"Mes", "Ano", "Fonte", "Valor"}); err != nil {
		return err
	}
	for _, income := range items {
		if err := writer.Write([]string{strconv.Itoa(income.Month), strconv.Itoa(income.Year), safeCSVText(paymentSourceLabel(income.Source)), formatDecimal(income.Amount)}); err != nil {
			return err
		}
	}
	return nil
}

func writeCategoriesCSV(writer *csv.Writer, items []CategoryResult) error {
	if err := writer.Write([]string{"Categoria", "Valor", "Percentual"}); err != nil {
		return err
	}
	for _, category := range items {
		if err := writer.Write([]string{safeCSVText(category.CategoryName), formatDecimal(category.TotalAmount), formatDecimal(category.Percentage)}); err != nil {
			return err
		}
	}
	return nil
}

func writeSummaryCSV(writer *csv.Writer, summary monthlyExportSummary) error {
	if err := writer.Write([]string{"Campo", "Valor"}); err != nil {
		return err
	}
	rows := summaryRows(summary)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func summaryRows(summary monthlyExportSummary) [][]string {
	return [][]string{
		{"Receitas", formatDecimal(summary.TotalIncome)},
		{"Despesas", formatDecimal(summary.TotalExpense)},
		{"Saldo", formatDecimal(summary.Balance)},
		{"Salario", formatDecimal(summary.Salary)},
		{"Adiantamento", formatDecimal(summary.Advance)},
		{"Renda Extra", formatDecimal(summary.ExtraIncome)},
		{"Gasto Salario", formatDecimal(summary.SpentSalary)},
		{"Gasto Adiantamento", formatDecimal(summary.SpentAdvance)},
		{"Gasto Renda Extra", formatDecimal(summary.SpentExtraIncome)},
		{"Restante Salario", formatDecimal(summary.RemainingSalary)},
		{"Restante Adiantamento", formatDecimal(summary.RemainingAdvance)},
		{"Restante Renda Extra", formatDecimal(summary.RemainingExtraIncome)},
		{"VA", formatDecimal(summary.VA)},
		{"VR", formatDecimal(summary.VR)},
		{"Gasto VA", formatDecimal(summary.SpentVA)},
		{"Gasto VR", formatDecimal(summary.SpentVR)},
		{"Restante VA", formatDecimal(summary.RemainingVA)},
		{"Restante VR", formatDecimal(summary.RemainingVR)},
	}
}

func writeMonthComparisonCSV(writer *csv.Writer, comparison *MonthComparisonResponse) error {
	if comparison == nil {
		return fmt.Errorf("missing month comparison")
	}
	if err := writer.Write([]string{"Secao", "Campo", "Valor Atual", "Valor Comparado", "Diferenca", "Percentual", "Status"}); err != nil {
		return err
	}
	for _, row := range monthComparisonRows(*comparison) {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func monthComparisonRows(comparison MonthComparisonResponse) [][]string {
	rows := [][]string{
		comparisonRow("Resumo", "Receitas", comparison.Summary.CurrentIncome, comparison.Summary.PreviousIncome, comparison.Summary.IncomeDifference, comparison.Summary.IncomePercentage, comparison.Summary.IncomeStatus),
		comparisonRow("Resumo", "Despesas", comparison.Summary.CurrentExpense, comparison.Summary.PreviousExpense, comparison.Summary.ExpenseDifference, comparison.Summary.ExpensePercentage, comparison.Summary.ExpenseStatus),
		comparisonRow("Resumo", "Saldo", comparison.Summary.CurrentBalance, comparison.Summary.PreviousBalance, comparison.Summary.BalanceDifference, comparison.Summary.BalancePercentage, comparison.Summary.BalanceStatus),
	}
	for _, category := range comparison.Categories {
		rows = append(rows, comparisonRow("Categoria", category.CategoryName, category.CurrentAmount, category.PreviousAmount, category.Difference, category.Percentage, category.Status))
	}
	for _, source := range comparison.PaymentSources {
		rows = append(rows, comparisonRow("Fonte de Pagamento", source.PaymentSource, source.CurrentAmount, source.PreviousAmount, source.Difference, source.Percentage, source.Status))
	}
	for _, expenseType := range comparison.ExpenseTypes {
		rows = append(rows, comparisonRow("Tipo de Despesa", expenseType.Type, expenseType.CurrentAmount, expenseType.PreviousAmount, expenseType.Difference, expenseType.Percentage, expenseType.Status))
	}
	for _, insight := range comparison.Insights {
		rows = append(rows, []string{"Insight", "Mensagem", safeCSVText(insight), "", "", "", ""})
	}
	return rows
}

func comparisonRow(section string, field string, current float64, previous float64, difference float64, percentage float64, status string) []string {
	return []string{section, safeCSVText(field), formatDecimal(current), formatDecimal(previous), formatDecimal(difference), formatDecimal(percentage), safeCSVText(status)}
}

func writeInstallmentCommitmentsCSV(writer *csv.Writer, commitments *InstallmentCommitmentsResponse) error {
	if commitments == nil {
		return fmt.Errorf("missing installment commitments")
	}
	if err := writer.Write([]string{"Secao", "Descricao", "Mes", "Ano", "Valor", "Parcela Atual", "Total Parcelas", "Categoria", "Fonte"}); err != nil {
		return err
	}
	for _, row := range installmentCommitmentRows(*commitments) {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func installmentCommitmentRows(commitments InstallmentCommitmentsResponse) [][]string {
	rows := [][]string{
		{"Resumo", "Total Original", "", "", formatDecimal(commitments.Summary.OriginalTotal), "", "", "", ""},
		{"Resumo", "Total Pago", "", "", formatDecimal(commitments.Summary.PaidTotal), "", "", "", ""},
		{"Resumo", "Total Restante", "", "", formatDecimal(commitments.Summary.RemainingTotal), "", "", "", ""},
	}
	for _, purchase := range commitments.Purchases {
		currentInstallment := ""
		month := purchase.FirstMonth
		year := purchase.FirstYear
		if purchase.NextInstallment != nil {
			currentInstallment = strconv.Itoa(purchase.NextInstallment.CurrentInstallment)
			month = purchase.NextInstallment.Month
			year = purchase.NextInstallment.Year
		}
		rows = append(rows, []string{"Compra", safeCSVText(purchase.Description), strconv.Itoa(month), strconv.Itoa(year), formatDecimal(purchase.InstallmentAmount), currentInstallment, strconv.Itoa(purchase.TotalInstallments), safeCSVText(purchase.CategoryName), safeCSVText(paymentSourceLabel(purchase.PaymentSource))})
	}
	for _, month := range commitments.Timeline {
		for _, installment := range month.Installments {
			rows = append(rows, []string{"Linha do Tempo", safeCSVText(installment.Description), strconv.Itoa(installment.Month), strconv.Itoa(installment.Year), formatDecimal(installment.Amount), strconv.Itoa(installment.CurrentInstallment), strconv.Itoa(installment.TotalInstallments), safeCSVText(installment.CategoryName), safeCSVText(paymentSourceLabel(installment.PaymentSource))})
		}
	}
	return rows
}

func writeFullReportCSV(writer *csv.Writer, dataset exportDataset) error {
	if dataset.Comparison == nil || dataset.Commitments == nil {
		return fmt.Errorf("missing full report sections")
	}

	if err := writeFullReportSection(writer, "RESUMO MENSAL", []string{"Campo", "Valor"}, summaryRows(dataset.Summary)); err != nil {
		return err
	}

	incomeRows := make([][]string, 0, len(dataset.Incomes))
	for _, income := range dataset.Incomes {
		incomeRows = append(incomeRows, []string{safeCSVText(paymentSourceLabel(income.Source)), formatDecimal(income.Amount), strconv.Itoa(income.Month), strconv.Itoa(income.Year)})
	}
	if err := writeFullReportSection(writer, "RECEITAS", []string{"Fonte", "Valor", "Mes", "Ano"}, incomeRows); err != nil {
		return err
	}

	expenseRows := make([][]string, 0, len(dataset.Expenses))
	for _, expense := range dataset.Expenses {
		category := strings.TrimSpace(dataset.CategoryNames[expense.CategoryID])
		if category == "" {
			category = "Sem categoria"
		}
		installment := ""
		if expenseTypeLabel(expense.Type) == "Parcelada" {
			installment = formatInstallment(expense.CurrentInstall, expense.Installments)
		}
		expenseRows = append(expenseRows, []string{expense.Date.Format("2006-01-02"), safeCSVText(expense.Description), safeCSVText(category), safeCSVText(paymentSplitsLabel(expense)), safeCSVText(expenseTypeLabel(expense.Type)), installment, formatDecimal(expense.Amount), safeCSVText(expense.Notes)})
	}
	if err := writeFullReportSection(writer, "DESPESAS", []string{"Data", "Descricao", "Categoria", "Fonte de Pagamento", "Tipo", "Parcela", "Valor", "Observacoes"}, expenseRows); err != nil {
		return err
	}

	categoryRows := make([][]string, 0, len(dataset.CategorySummary))
	for _, category := range dataset.CategorySummary {
		categoryRows = append(categoryRows, []string{safeCSVText(category.CategoryName), formatDecimal(category.TotalAmount), formatDecimal(category.Percentage)})
	}
	if err := writeFullReportSection(writer, "RESUMO POR CATEGORIA", []string{"Categoria", "Valor", "Percentual"}, categoryRows); err != nil {
		return err
	}

	comparison := *dataset.Comparison
	comparisonHeader := []string{"Campo", "Valor Atual", "Valor Comparado", "Diferenca", "Percentual", "Status"}
	comparisonSummaryRows := [][]string{
		comparisonValuesRow("Receitas", comparison.Summary.CurrentIncome, comparison.Summary.PreviousIncome, comparison.Summary.IncomeDifference, comparison.Summary.IncomePercentage, comparison.Summary.IncomeStatus),
		comparisonValuesRow("Despesas", comparison.Summary.CurrentExpense, comparison.Summary.PreviousExpense, comparison.Summary.ExpenseDifference, comparison.Summary.ExpensePercentage, comparison.Summary.ExpenseStatus),
		comparisonValuesRow("Saldo", comparison.Summary.CurrentBalance, comparison.Summary.PreviousBalance, comparison.Summary.BalanceDifference, comparison.Summary.BalancePercentage, comparison.Summary.BalanceStatus),
	}
	if err := writeFullReportSection(writer, "COMPARATIVO - RESUMO", comparisonHeader, comparisonSummaryRows); err != nil {
		return err
	}

	comparisonCategoryRows := make([][]string, 0, len(comparison.Categories))
	for _, category := range comparison.Categories {
		comparisonCategoryRows = append(comparisonCategoryRows, comparisonValuesRow(category.CategoryName, category.CurrentAmount, category.PreviousAmount, category.Difference, category.Percentage, category.Status))
	}
	if err := writeFullReportSection(writer, "COMPARATIVO - CATEGORIAS", comparisonHeader, comparisonCategoryRows); err != nil {
		return err
	}

	comparisonSourceRows := make([][]string, 0, len(comparison.PaymentSources))
	for _, source := range comparison.PaymentSources {
		comparisonSourceRows = append(comparisonSourceRows, comparisonValuesRow(source.PaymentSource, source.CurrentAmount, source.PreviousAmount, source.Difference, source.Percentage, source.Status))
	}
	if err := writeFullReportSection(writer, "COMPARATIVO - FONTES DE PAGAMENTO", comparisonHeader, comparisonSourceRows); err != nil {
		return err
	}

	comparisonTypeRows := make([][]string, 0, len(comparison.ExpenseTypes))
	for _, expenseType := range comparison.ExpenseTypes {
		comparisonTypeRows = append(comparisonTypeRows, comparisonValuesRow(expenseType.Type, expenseType.CurrentAmount, expenseType.PreviousAmount, expenseType.Difference, expenseType.Percentage, expenseType.Status))
	}
	if err := writeFullReportSection(writer, "COMPARATIVO - TIPOS DE DESPESA", comparisonHeader, comparisonTypeRows); err != nil {
		return err
	}

	insightRows := make([][]string, 0, len(comparison.Insights))
	for _, insight := range comparison.Insights {
		insightRows = append(insightRows, []string{safeCSVText(insight)})
	}
	if err := writeFullReportSection(writer, "INSIGHTS DO COMPARATIVO", []string{"Mensagem"}, insightRows); err != nil {
		return err
	}

	commitments := *dataset.Commitments
	commitmentSummaryRows := [][]string{
		{"Total Original", formatDecimal(commitments.Summary.OriginalTotal)},
		{"Total Pago", formatDecimal(commitments.Summary.PaidTotal)},
		{"Total Restante", formatDecimal(commitments.Summary.RemainingTotal)},
		{"Parcelas Pagas", strconv.Itoa(commitments.Summary.PaidInstallments)},
		{"Parcelas Restantes", strconv.Itoa(commitments.Summary.RemainingInstallments)},
		{"Total de Compras", strconv.Itoa(commitments.Summary.TotalPurchases)},
	}
	if commitments.Summary.HeaviestMonth != nil {
		heaviest := commitments.Summary.HeaviestMonth
		commitmentSummaryRows = append(commitmentSummaryRows, []string{"Mes Mais Pesado", fmt.Sprintf("%02d/%04d - R$ %s", heaviest.Month, heaviest.Year, formatDecimal(heaviest.Total))})
	}
	if err := writeFullReportSection(writer, "COMPROMISSOS PARCELADOS - RESUMO", []string{"Campo", "Valor"}, commitmentSummaryRows); err != nil {
		return err
	}

	purchaseRows := make([][]string, 0, len(commitments.Purchases))
	for _, purchase := range commitments.Purchases {
		progress := fmt.Sprintf("%d pagas de %d", purchase.PaidInstallments, purchase.TotalInstallments)
		nextInstallment := ""
		if purchase.NextInstallment != nil {
			next := purchase.NextInstallment
			nextInstallment = fmt.Sprintf("%s - %02d/%04d", formatInstallment(next.CurrentInstallment, next.TotalInstallments), next.Month, next.Year)
		}
		purchaseRows = append(purchaseRows, []string{safeCSVText(purchase.Description), safeCSVText(purchase.CategoryName), safeCSVText(paymentSourceLabel(purchase.PaymentSource)), formatDecimal(purchase.InstallmentAmount), formatDecimal(purchase.OriginalTotal), formatDecimal(purchase.PaidTotal), formatDecimal(purchase.RemainingTotal), progress, nextInstallment})
	}
	if err := writeFullReportSection(writer, "COMPROMISSOS PARCELADOS - COMPRAS", []string{"Descricao", "Categoria", "Fonte", "Valor da Parcela", "Total Original", "Total Pago", "Total Restante", "Progresso", "Proxima Parcela"}, purchaseRows); err != nil {
		return err
	}

	timelineRows := make([][]string, 0)
	for _, month := range commitments.Timeline {
		for _, installment := range month.Installments {
			timelineRows = append(timelineRows, []string{strconv.Itoa(installment.Month), strconv.Itoa(installment.Year), safeCSVText(installment.Description), safeCSVText(installment.CategoryName), safeCSVText(paymentSourceLabel(installment.PaymentSource)), formatInstallment(installment.CurrentInstallment, installment.TotalInstallments), formatDecimal(installment.Amount)})
		}
	}
	if err := writeFullReportSection(writer, "COMPROMISSOS PARCELADOS - LINHA DO TEMPO", []string{"Mes", "Ano", "Descricao", "Categoria", "Fonte", "Parcela", "Valor"}, timelineRows); err != nil {
		return err
	}
	return nil
}

func comparisonValuesRow(field string, current float64, previous float64, difference float64, percentage float64, status string) []string {
	return []string{safeCSVText(field), formatDecimal(current), formatDecimal(previous), formatDecimal(difference), formatDecimal(percentage), safeCSVText(status)}
}

func writeFullReportSection(writer *csv.Writer, title string, header []string, rows [][]string) error {
	const width = 9
	if err := writePaddedRow(writer, []string{title}, width); err != nil {
		return err
	}
	if err := writePaddedRow(writer, header, width); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writePaddedRow(writer, row, width); err != nil {
			return err
		}
	}
	return writePaddedRow(writer, nil, width)
}

func writePaddedRow(writer *csv.Writer, row []string, width int) error {
	if len(row) > width {
		return fmt.Errorf("CSV row has %d columns, expected at most %d", len(row), width)
	}
	for len(row) < width {
		row = append(row, "")
	}
	return writer.Write(row)
}

func formatDecimal(value float64) string {
	formatted := strconv.FormatFloat(roundMoney(value), 'f', 2, 64)
	return strings.Replace(formatted, ".", ",", 1)
}

func formatInstallment(current int, total int) string {
	return fmt.Sprintf("%d de %d", current, total)
}

func safeCSVText(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	default:
		return value
	}
}
