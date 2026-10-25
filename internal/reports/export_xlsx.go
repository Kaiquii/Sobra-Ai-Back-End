package reports

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

type xlsxCellKind int

const (
	xlsxText xlsxCellKind = iota
	xlsxWrappedText
	xlsxInteger
	xlsxCurrency
	xlsxPercentage
	xlsxDate
)

type xlsxCell struct {
	Value any
	Kind  xlsxCellKind
}

type xlsxSection struct {
	Title   string
	Headers []string
	Rows    [][]xlsxCell
	Filter  bool
}

type xlsxSheet struct {
	Name     string
	Title    string
	Subtitle string
	Sections []xlsxSection
}

type xlsxStyles struct {
	title       int
	subtitle    int
	section     int
	header      int
	text        int
	wrappedText int
	integer     int
	currency    int
	percentage  int
	date        int
}

func generateExportXLSX(dataset exportDataset) ([]byte, error) {
	sheets, err := buildXLSXSheets(dataset)
	if err != nil {
		return nil, err
	}

	workbook := excelize.NewFile()
	defer workbook.Close()
	workbook.SetDefaultFont("Aptos")

	styles, err := newXLSXStyles(workbook)
	if err != nil {
		return nil, err
	}

	for index, sheet := range sheets {
		if index == 0 {
			if err := workbook.SetSheetName("Sheet1", sheet.Name); err != nil {
				return nil, err
			}
		} else if _, err := workbook.NewSheet(sheet.Name); err != nil {
			return nil, err
		}

		if err := writeXLSXSheet(workbook, styles, sheet); err != nil {
			return nil, err
		}
	}

	if len(sheets) > 0 {
		if index, err := workbook.GetSheetIndex(sheets[0].Name); err == nil {
			workbook.SetActiveSheet(index)
		}
	}
	if err := workbook.SetDocProps(&excelize.DocProperties{
		Title:       sheets[0].Title,
		Subject:     "Relatório financeiro do SobraAi",
		Creator:     "SobraAi",
		Description: "Relatório financeiro gerado pelo SobraAi",
		Created:     time.Now().Format(time.RFC3339),
	}); err != nil {
		return nil, err
	}

	buffer, err := workbook.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func newXLSXStyles(workbook *excelize.File) (xlsxStyles, error) {
	border := []excelize.Border{{Type: "bottom", Color: "D7DEE8", Style: 1}}
	currencyFormat := `R$ #,##0.00;[Red]-R$ #,##0.00`
	percentageFormat := `0.00%`
	dateFormat := `dd/mm/yyyy`

	definitions := []*excelize.Style{
		{Font: &excelize.Font{Bold: true, Color: "FFFFFF", Size: 18}, Fill: excelize.Fill{Type: "pattern", Color: []string{"17324D"}, Pattern: 1}, Alignment: &excelize.Alignment{Vertical: "center"}},
		{Font: &excelize.Font{Color: "52606D", Size: 10}, Alignment: &excelize.Alignment{Vertical: "center"}},
		{Font: &excelize.Font{Bold: true, Color: "17324D", Size: 12}, Fill: excelize.Fill{Type: "pattern", Color: []string{"E8F3F1"}, Pattern: 1}, Alignment: &excelize.Alignment{Vertical: "center"}},
		{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"167D7F"}, Pattern: 1}, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}, Border: border},
		{Alignment: &excelize.Alignment{Vertical: "center"}, Border: border},
		{Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}, Border: border},
		{Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"}, Border: border, NumFmt: 1},
		{Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"}, Border: border, CustomNumFmt: &currencyFormat},
		{Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"}, Border: border, CustomNumFmt: &percentageFormat},
		{Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}, Border: border, CustomNumFmt: &dateFormat},
	}

	ids := make([]int, len(definitions))
	for index, definition := range definitions {
		styleID, err := workbook.NewStyle(definition)
		if err != nil {
			return xlsxStyles{}, err
		}
		ids[index] = styleID
	}

	return xlsxStyles{
		title: ids[0], subtitle: ids[1], section: ids[2], header: ids[3], text: ids[4],
		wrappedText: ids[5], integer: ids[6], currency: ids[7], percentage: ids[8], date: ids[9],
	}, nil
}

func writeXLSXSheet(workbook *excelize.File, styles xlsxStyles, sheet xlsxSheet) error {
	maxColumns := 1
	for _, section := range sheet.Sections {
		if len(section.Headers) > maxColumns {
			maxColumns = len(section.Headers)
		}
	}
	lastColumn, _ := excelize.ColumnNumberToName(maxColumns)

	if err := workbook.MergeCell(sheet.Name, "A1", lastColumn+"1"); err != nil {
		return err
	}
	if err := workbook.SetCellValue(sheet.Name, "A1", sheet.Title); err != nil {
		return err
	}
	if err := workbook.SetCellStyle(sheet.Name, "A1", lastColumn+"1", styles.title); err != nil {
		return err
	}
	if err := workbook.SetRowHeight(sheet.Name, 1, 32); err != nil {
		return err
	}

	if err := workbook.MergeCell(sheet.Name, "A2", lastColumn+"2"); err != nil {
		return err
	}
	if err := workbook.SetCellValue(sheet.Name, "A2", sheet.Subtitle); err != nil {
		return err
	}
	if err := workbook.SetCellStyle(sheet.Name, "A2", lastColumn+"2", styles.subtitle); err != nil {
		return err
	}

	widths := make([]float64, maxColumns)
	for index := range widths {
		widths[index] = 12
	}

	row := 4
	for sectionIndex, section := range sheet.Sections {
		sectionLastColumn, _ := excelize.ColumnNumberToName(maxInt(1, len(section.Headers)))
		if err := workbook.MergeCell(sheet.Name, fmt.Sprintf("A%d", row), fmt.Sprintf("%s%d", sectionLastColumn, row)); err != nil {
			return err
		}
		if err := workbook.SetCellValue(sheet.Name, fmt.Sprintf("A%d", row), section.Title); err != nil {
			return err
		}
		if err := workbook.SetCellStyle(sheet.Name, fmt.Sprintf("A%d", row), fmt.Sprintf("%s%d", sectionLastColumn, row), styles.section); err != nil {
			return err
		}
		row++

		headerRow := row
		for columnIndex, header := range section.Headers {
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, headerRow)
			if err := workbook.SetCellValue(sheet.Name, cell, header); err != nil {
				return err
			}
			widths[columnIndex] = maxFloat(widths[columnIndex], float64(utf8.RuneCountInString(header)+3))
		}
		if len(section.Headers) > 0 {
			if err := workbook.SetCellStyle(sheet.Name, fmt.Sprintf("A%d", headerRow), fmt.Sprintf("%s%d", sectionLastColumn, headerRow), styles.header); err != nil {
				return err
			}
		}
		row++

		for _, values := range section.Rows {
			for columnIndex, value := range values {
				cell, _ := excelize.CoordinatesToCellName(columnIndex+1, row)
				if err := workbook.SetCellValue(sheet.Name, cell, value.Value); err != nil {
					return err
				}
				if err := workbook.SetCellStyle(sheet.Name, cell, cell, xlsxStyleForKind(styles, value.Kind)); err != nil {
					return err
				}
				widths[columnIndex] = maxFloat(widths[columnIndex], xlsxCellDisplayWidth(value))
			}
			row++
		}

		if section.Filter && len(section.Rows) > 0 && len(section.Headers) > 0 {
			rangeRef := fmt.Sprintf("A%d:%s%d", headerRow, sectionLastColumn, row-1)
			if err := workbook.AutoFilter(sheet.Name, rangeRef, nil); err != nil {
				return err
			}
		}

		if sectionIndex < len(sheet.Sections)-1 {
			row += 2
		}
	}

	for columnIndex, width := range widths {
		column, _ := excelize.ColumnNumberToName(columnIndex + 1)
		if width > 46 {
			width = 46
		}
		if err := workbook.SetColWidth(sheet.Name, column, column, width); err != nil {
			return err
		}
	}

	return nil
}

func xlsxStyleForKind(styles xlsxStyles, kind xlsxCellKind) int {
	switch kind {
	case xlsxWrappedText:
		return styles.wrappedText
	case xlsxInteger:
		return styles.integer
	case xlsxCurrency:
		return styles.currency
	case xlsxPercentage:
		return styles.percentage
	case xlsxDate:
		return styles.date
	default:
		return styles.text
	}
}

func xlsxCellDisplayWidth(cell xlsxCell) float64 {
	switch value := cell.Value.(type) {
	case time.Time:
		return 13
	case float64:
		return 16
	case int:
		return 10
	case string:
		longest := 0
		for _, line := range strings.Split(value, "\n") {
			longest = maxInt(longest, utf8.RuneCountInString(line))
		}
		return float64(longest + 3)
	default:
		return 12
	}
}

func buildXLSXSheets(dataset exportDataset) ([]xlsxSheet, error) {
	subtitle := xlsxPeriodSubtitle(dataset.Options)
	switch dataset.Options.ReportType {
	case exportTypeExpenses:
		return []xlsxSheet{{Name: "Despesas", Title: "Relatório de Despesas", Subtitle: subtitle, Sections: []xlsxSection{xlsxExpensesSection(dataset)}}}, nil
	case exportTypeIncomes:
		return []xlsxSheet{{Name: "Receitas", Title: "Relatório de Receitas", Subtitle: subtitle, Sections: []xlsxSection{xlsxIncomesSection(dataset)}}}, nil
	case exportTypeCategories:
		return []xlsxSheet{{Name: "Categorias", Title: "Resumo por Categoria", Subtitle: subtitle, Sections: []xlsxSection{xlsxCategoriesSection(dataset)}}}, nil
	case exportTypeSummary:
		return []xlsxSheet{{Name: "Resumo", Title: "Resumo Financeiro", Subtitle: subtitle, Sections: []xlsxSection{xlsxSummarySection(dataset.Summary)}}}, nil
	case exportTypeMonthComparison:
		if dataset.Comparison == nil {
			return nil, fmt.Errorf("missing month comparison")
		}
		return []xlsxSheet{{Name: "Comparativo", Title: "Comparativo Mensal", Subtitle: subtitle, Sections: xlsxComparisonSections(*dataset.Comparison, true)}}, nil
	case exportTypeInstallmentCommitments:
		if dataset.Commitments == nil {
			return nil, fmt.Errorf("missing installment commitments")
		}
		return []xlsxSheet{{Name: "Parcelamentos", Title: "Compromissos Parcelados", Subtitle: subtitle, Sections: xlsxCommitmentSections(*dataset.Commitments)}}, nil
	case exportTypeFullReport:
		if dataset.Comparison == nil || dataset.Commitments == nil {
			return nil, fmt.Errorf("missing full report sections")
		}
		comparisonSections := xlsxComparisonSections(*dataset.Comparison, false)
		return []xlsxSheet{
			{Name: "Resumo", Title: "Relatório Financeiro Completo", Subtitle: subtitle, Sections: []xlsxSection{xlsxSummarySection(dataset.Summary)}},
			{Name: "Receitas", Title: "Receitas", Subtitle: subtitle, Sections: []xlsxSection{xlsxIncomesSection(dataset)}},
			{Name: "Despesas", Title: "Despesas", Subtitle: subtitle, Sections: []xlsxSection{xlsxExpensesSection(dataset)}},
			{Name: "Categorias", Title: "Resumo por Categoria", Subtitle: subtitle, Sections: []xlsxSection{xlsxCategoriesSection(dataset)}},
			{Name: "Comparativo", Title: "Comparativo Mensal", Subtitle: subtitle, Sections: comparisonSections},
			{Name: "Parcelamentos", Title: "Compromissos Parcelados", Subtitle: subtitle, Sections: xlsxCommitmentSections(*dataset.Commitments)},
			{Name: "Insights", Title: "Insights do Comparativo", Subtitle: subtitle, Sections: []xlsxSection{xlsxInsightsSection(dataset.Comparison.Insights)}},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported export type")
	}
}

func xlsxExpensesSection(dataset exportDataset) xlsxSection {
	rows := make([][]xlsxCell, 0, len(dataset.Expenses))
	for _, expense := range dataset.Expenses {
		category := strings.TrimSpace(dataset.CategoryNames[expense.CategoryID])
		if category == "" {
			category = "Sem categoria"
		}
		installment := ""
		if expenseTypeLabel(expense.Type) == "Parcelada" {
			installment = formatInstallment(expense.CurrentInstall, expense.Installments)
		}
		rows = append(rows, []xlsxCell{
			{expense.Date, xlsxDate}, {expense.Description, xlsxText}, {category, xlsxText},
			{paymentSplitsLabel(expense), xlsxText}, {expenseTypeLabel(expense.Type), xlsxText},
			{installment, xlsxText}, {roundMoney(expense.Amount), xlsxCurrency}, {expense.Notes, xlsxWrappedText},
			{expenseFinancialDate(expense), xlsxDate}, {expenseAdvanceLabel(expense), xlsxText},
		})
	}
	return xlsxSection{Title: "Despesas", Headers: []string{"Data", "Descrição", "Categoria", "Fonte de Pagamento", "Tipo", "Parcela", "Valor", "Observações", "Data considerada", "Adiantamento"}, Rows: rows, Filter: true}
}

func xlsxIncomesSection(dataset exportDataset) xlsxSection {
	rows := make([][]xlsxCell, 0, len(dataset.Incomes))
	for _, income := range dataset.Incomes {
		rows = append(rows, []xlsxCell{{income.Month, xlsxInteger}, {income.Year, xlsxInteger}, {paymentSourceLabel(income.Source), xlsxText}, {roundMoney(income.Amount), xlsxCurrency}})
	}
	return xlsxSection{Title: "Receitas", Headers: []string{"Mês", "Ano", "Fonte", "Valor"}, Rows: rows, Filter: true}
}

func xlsxCategoriesSection(dataset exportDataset) xlsxSection {
	rows := make([][]xlsxCell, 0, len(dataset.CategorySummary))
	for _, category := range dataset.CategorySummary {
		rows = append(rows, []xlsxCell{{category.CategoryName, xlsxText}, {roundMoney(category.TotalAmount), xlsxCurrency}, {category.Percentage / 100, xlsxPercentage}})
	}
	return xlsxSection{Title: "Categorias", Headers: []string{"Categoria", "Valor", "Percentual"}, Rows: rows, Filter: true}
}

func xlsxSummarySection(summary monthlyExportSummary) xlsxSection {
	return xlsxSection{Title: "Resumo Mensal", Headers: []string{"Campo", "Valor"}, Rows: [][]xlsxCell{
		{{"Receitas", xlsxText}, {roundMoney(summary.TotalIncome), xlsxCurrency}},
		{{"Despesas", xlsxText}, {roundMoney(summary.TotalExpense), xlsxCurrency}},
		{{"Saldo", xlsxText}, {roundMoney(summary.Balance), xlsxCurrency}},
		{{"Salário", xlsxText}, {roundMoney(summary.Salary), xlsxCurrency}},
		{{"Adiantamento", xlsxText}, {roundMoney(summary.Advance), xlsxCurrency}},
		{{"Renda Extra", xlsxText}, {roundMoney(summary.ExtraIncome), xlsxCurrency}},
		{{"Gasto Salário", xlsxText}, {roundMoney(summary.SpentSalary), xlsxCurrency}},
		{{"Gasto Adiantamento", xlsxText}, {roundMoney(summary.SpentAdvance), xlsxCurrency}},
		{{"Gasto Renda Extra", xlsxText}, {roundMoney(summary.SpentExtraIncome), xlsxCurrency}},
		{{"Restante Salário", xlsxText}, {roundMoney(summary.RemainingSalary), xlsxCurrency}},
		{{"Restante Adiantamento", xlsxText}, {roundMoney(summary.RemainingAdvance), xlsxCurrency}},
		{{"Restante Renda Extra", xlsxText}, {roundMoney(summary.RemainingExtraIncome), xlsxCurrency}},
		{{"VA", xlsxText}, {roundMoney(summary.VA), xlsxCurrency}},
		{{"VR", xlsxText}, {roundMoney(summary.VR), xlsxCurrency}},
		{{"Gasto VA", xlsxText}, {roundMoney(summary.SpentVA), xlsxCurrency}},
		{{"Gasto VR", xlsxText}, {roundMoney(summary.SpentVR), xlsxCurrency}},
		{{"Restante VA", xlsxText}, {roundMoney(summary.RemainingVA), xlsxCurrency}},
		{{"Restante VR", xlsxText}, {roundMoney(summary.RemainingVR), xlsxCurrency}},
	}}
}

func xlsxComparisonSections(comparison MonthComparisonResponse, includeInsights bool) []xlsxSection {
	header := []string{"Campo", "Valor Atual", "Valor Comparado", "Diferença", "Percentual", "Status"}
	sections := []xlsxSection{{Title: "Resumo", Headers: header, Rows: [][]xlsxCell{
		xlsxComparisonRow("Receitas", comparison.Summary.CurrentIncome, comparison.Summary.PreviousIncome, comparison.Summary.IncomeDifference, comparison.Summary.IncomePercentage, comparison.Summary.IncomeStatus),
		xlsxComparisonRow("Despesas", comparison.Summary.CurrentExpense, comparison.Summary.PreviousExpense, comparison.Summary.ExpenseDifference, comparison.Summary.ExpensePercentage, comparison.Summary.ExpenseStatus),
		xlsxComparisonRow("Saldo", comparison.Summary.CurrentBalance, comparison.Summary.PreviousBalance, comparison.Summary.BalanceDifference, comparison.Summary.BalancePercentage, comparison.Summary.BalanceStatus),
	}}}

	categoryRows := make([][]xlsxCell, 0, len(comparison.Categories))
	for _, item := range comparison.Categories {
		categoryRows = append(categoryRows, xlsxComparisonRow(item.CategoryName, item.CurrentAmount, item.PreviousAmount, item.Difference, item.Percentage, item.Status))
	}
	sections = append(sections, xlsxSection{Title: "Categorias", Headers: header, Rows: categoryRows})

	sourceRows := make([][]xlsxCell, 0, len(comparison.PaymentSources))
	for _, item := range comparison.PaymentSources {
		sourceRows = append(sourceRows, xlsxComparisonRow(paymentSourceLabel(item.PaymentSource), item.CurrentAmount, item.PreviousAmount, item.Difference, item.Percentage, item.Status))
	}
	sections = append(sections, xlsxSection{Title: "Fontes de Pagamento", Headers: header, Rows: sourceRows})

	typeRows := make([][]xlsxCell, 0, len(comparison.ExpenseTypes))
	for _, item := range comparison.ExpenseTypes {
		typeRows = append(typeRows, xlsxComparisonRow(expenseTypeLabel(item.Type), item.CurrentAmount, item.PreviousAmount, item.Difference, item.Percentage, item.Status))
	}
	sections = append(sections, xlsxSection{Title: "Tipos de Despesa", Headers: header, Rows: typeRows})
	if includeInsights {
		sections = append(sections, xlsxInsightsSection(comparison.Insights))
	}
	return sections
}

func xlsxComparisonRow(field string, current, previous, difference, percentage float64, status string) []xlsxCell {
	return []xlsxCell{{field, xlsxText}, {roundMoney(current), xlsxCurrency}, {roundMoney(previous), xlsxCurrency}, {roundMoney(difference), xlsxCurrency}, {percentage / 100, xlsxPercentage}, {status, xlsxText}}
}

func xlsxInsightsSection(insights []string) xlsxSection {
	rows := make([][]xlsxCell, 0, len(insights))
	for _, insight := range insights {
		rows = append(rows, []xlsxCell{{insight, xlsxWrappedText}})
	}
	return xlsxSection{Title: "Insights", Headers: []string{"Mensagem"}, Rows: rows}
}

func xlsxCommitmentSections(commitments InstallmentCommitmentsResponse) []xlsxSection {
	summaryRows := [][]xlsxCell{
		{{"Total Original", xlsxText}, {roundMoney(commitments.Summary.OriginalTotal), xlsxCurrency}},
		{{"Total Pago", xlsxText}, {roundMoney(commitments.Summary.PaidTotal), xlsxCurrency}},
		{{"Total Restante", xlsxText}, {roundMoney(commitments.Summary.RemainingTotal), xlsxCurrency}},
		{{"Parcelas Pagas", xlsxText}, {commitments.Summary.PaidInstallments, xlsxInteger}},
		{{"Parcelas Restantes", xlsxText}, {commitments.Summary.RemainingInstallments, xlsxInteger}},
		{{"Total de Compras", xlsxText}, {commitments.Summary.TotalPurchases, xlsxInteger}},
	}
	if heaviest := commitments.Summary.HeaviestMonth; heaviest != nil {
		summaryRows = append(summaryRows, []xlsxCell{{"Mês Mais Pesado", xlsxText}, {fmt.Sprintf("%02d/%04d", heaviest.Month, heaviest.Year), xlsxText}, {roundMoney(heaviest.Total), xlsxCurrency}})
	}

	purchaseRows := make([][]xlsxCell, 0, len(commitments.Purchases))
	for _, purchase := range commitments.Purchases {
		nextInstallment := ""
		if next := purchase.NextInstallment; next != nil {
			nextInstallment = fmt.Sprintf("%s - %02d/%04d", formatInstallment(next.CurrentInstallment, next.TotalInstallments), next.Month, next.Year)
		}
		purchaseRows = append(purchaseRows, []xlsxCell{
			{purchase.Description, xlsxText}, {purchase.CategoryName, xlsxText}, {paymentSourceLabel(purchase.PaymentSource), xlsxText},
			{roundMoney(purchase.InstallmentAmount), xlsxCurrency}, {roundMoney(purchase.OriginalTotal), xlsxCurrency},
			{roundMoney(purchase.PaidTotal), xlsxCurrency}, {roundMoney(purchase.RemainingTotal), xlsxCurrency},
			{purchase.PaidInstallments, xlsxInteger}, {purchase.RemainingInstallments, xlsxInteger},
			{purchase.TotalInstallments, xlsxInteger}, {nextInstallment, xlsxText},
		})
	}

	timelineRows := make([][]xlsxCell, 0)
	for _, month := range commitments.Timeline {
		for _, installment := range month.Installments {
			advanceLabel := ""
			if installment.IsAdvanced && installment.AdvancedAt != nil {
				advanceLabel = "Adiantada em " + installment.AdvancedAt.In(time.Local).Format("02/01/2006")
			}
			timelineRows = append(timelineRows, []xlsxCell{
				{installment.Month, xlsxInteger}, {installment.Year, xlsxInteger}, {installment.Description, xlsxText},
				{installment.CategoryName, xlsxText}, {paymentSourceLabel(installment.PaymentSource), xlsxText},
				{formatInstallment(installment.CurrentInstallment, installment.TotalInstallments), xlsxText}, {roundMoney(installment.Amount), xlsxCurrency}, {advanceLabel, xlsxText},
			})
		}
	}

	return []xlsxSection{
		{Title: "Resumo", Headers: []string{"Campo", "Valor", "Total"}, Rows: summaryRows},
		{Title: "Compras", Headers: []string{"Descrição", "Categoria", "Fonte", "Valor da Parcela", "Total Original", "Total Pago", "Total Restante", "Parcelas Pagas", "Parcelas Restantes", "Total Parcelas", "Próxima Parcela"}, Rows: purchaseRows, Filter: true},
		{Title: "Linha do Tempo", Headers: []string{"Mês", "Ano", "Descrição", "Categoria", "Fonte", "Parcela", "Valor", "Adiantamento"}, Rows: timelineRows},
	}
}

func xlsxPeriodSubtitle(options exportOptions) string {
	base := fmt.Sprintf("Período: %02d/%04d", options.Month, options.Year)
	if (options.ReportType == exportTypeMonthComparison || options.ReportType == exportTypeFullReport) && options.CompareMonth >= 1 && options.CompareYear >= 2000 {
		base += fmt.Sprintf(" | Comparado a: %02d/%04d", options.CompareMonth, options.CompareYear)
	}
	if (options.ReportType == exportTypeInstallmentCommitments || options.ReportType == exportTypeFullReport) && options.Months > 0 {
		base += fmt.Sprintf(" | Projeção: %d meses", options.Months)
	}
	return base + " | Gerado em: " + time.Now().Format("02/01/2006 15:04")
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
