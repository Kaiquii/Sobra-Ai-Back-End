package assistant

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type analysisSummaryFacts struct {
	Years   []int                   `json:"years"`
	Periods []analysisSummaryPeriod `json:"periods"`
}

type analysisSummaryPeriod struct {
	Year               int                        `json:"year"`
	TotalIncome        *float64                   `json:"total_income"`
	TotalExpense       *float64                   `json:"total_expense"`
	Months             *[]analysisSummaryMonth    `json:"months_with_records"`
	Categories         *[]analysisSummaryCategory `json:"categories"`
	CategoriesComplete *bool                      `json:"categories_complete"`
}

type analysisSummaryMonth struct {
	Month        int      `json:"month"`
	TotalIncome  *float64 `json:"total_income"`
	TotalExpense *float64 `json:"total_expense"`
}

type analysisSummaryCategory struct {
	Name          string     `json:"name"`
	TotalAmount   *float64   `json:"total_amount"`
	MonthlyAmount []*float64 `json:"monthly_amounts_jan_to_dec"`
}

// This fallback receives only the authenticated snapshot, never the rejected
// reply or its calculations. Invalid or inconsistent facts produce no summary.
func verifiedAnalysisSummary(facts map[string]any) (string, bool) {
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", false
	}
	var data analysisSummaryFacts
	if json.Unmarshal(encoded, &data) != nil || len(data.Years) == 0 || len(data.Years) > 2 || len(data.Periods) != len(data.Years) {
		return "", false
	}
	years := map[int]bool{}
	for _, year := range data.Years {
		if year < 2000 || year > 9999 || years[year] {
			return "", false
		}
		years[year] = true
	}
	sort.Slice(data.Periods, func(i, j int) bool { return data.Periods[i].Year < data.Periods[j].Year })
	var blocks []string
	for _, period := range data.Periods {
		if !years[period.Year] {
			return "", false
		}
		delete(years, period.Year)
		income, incomeOK := analysisSummaryCents(period.TotalIncome)
		expense, expenseOK := analysisSummaryCents(period.TotalExpense)
		if !incomeOK || !expenseOK || !validAnalysisSummaryPeriod(period, income, expense) {
			return "", false
		}
		lines := []string{
			fmt.Sprintf("**Ano de %d — janeiro a dezembro**", period.Year),
			"Rendas cadastradas: **R$ " + formatMoney(float64(income)/100) + "**.",
			"Despesas cadastradas: **R$ " + formatMoney(float64(expense)/100) + "**.",
		}
		if !*period.CategoriesComplete {
			lines = append(lines, "A cobertura das categorias e parcial; nao apresentei um ranking.")
		} else if len(*period.Categories) > 0 {
			categories := *period.Categories
			sort.Slice(categories, func(i, j int) bool {
				if *categories[i].TotalAmount != *categories[j].TotalAmount {
					return *categories[i].TotalAmount > *categories[j].TotalAmount
				}
				return categories[i].Name < categories[j].Name
			})
			lines = append(lines, "Categorias com maiores despesas cadastradas:")
			for _, category := range categories[:min(3, len(categories))] {
				lines = append(lines, "- **"+financialQueryLabel(category.Name)+":** R$ "+formatMoney(*category.TotalAmount)+".")
			}
		}
		blocks = append(blocks, strings.Join(lines, "\n\n"))
	}
	blocks = append(blocks,
		"O maior valor e um ponto de revisao, nao prova de desperdicio. Confirme o que e essencial antes de definir cortes.",
		"Este resumo considera lancamentos cadastrados, inclusive futuros, e nao confirma recebimento ou pagamento. Ausencia de registros nao prova ausencia de gastos.",
	)
	return strings.Join(blocks, "\n\n"), true
}

func analysisSummaryCents(amount *float64) (int64, bool) {
	if amount == nil || *amount < 0 {
		return 0, false
	}
	cents, err := financialQueryCents(*amount)
	return cents, err == nil
}

func validAnalysisSummaryPeriod(period analysisSummaryPeriod, income, expense int64) bool {
	if period.Months == nil || len(*period.Months) > 12 || period.Categories == nil || len(*period.Categories) > 40 || period.CategoriesComplete == nil {
		return false
	}
	var months [12]int64
	var seen [12]bool
	var monthlyIncome, monthlyExpense int64
	for _, month := range *period.Months {
		if month.Month < 1 || month.Month > 12 || seen[month.Month-1] {
			return false
		}
		income, incomeOK := analysisSummaryCents(month.TotalIncome)
		expense, expenseOK := analysisSummaryCents(month.TotalExpense)
		if !incomeOK || !expenseOK {
			return false
		}
		seen[month.Month-1], months[month.Month-1] = true, expense
		monthlyIncome, monthlyExpense = monthlyIncome+income, monthlyExpense+expense
	}
	if monthlyIncome != income || monthlyExpense != expense {
		return false
	}
	var categoryMonths [12]int64
	for _, category := range *period.Categories {
		total, ok := analysisSummaryCents(category.TotalAmount)
		if !ok || financialQueryLabel(category.Name) == "" || len(category.MonthlyAmount) != 12 {
			return false
		}
		var registered int64
		for i, amount := range category.MonthlyAmount {
			cents, ok := analysisSummaryCents(amount)
			if !ok {
				return false
			}
			registered += cents
			categoryMonths[i] += cents
		}
		if registered != total {
			return false
		}
	}
	for i, amount := range categoryMonths {
		if amount > months[i] || (*period.CategoriesComplete && amount != months[i]) {
			return false
		}
	}
	return true
}
