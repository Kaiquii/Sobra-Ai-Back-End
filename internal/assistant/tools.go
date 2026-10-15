package assistant

import (
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

func toolDeclarations() []FunctionDeclaration {
	return []FunctionDeclaration{
		{
			Name:        "get_monthly_summary",
			Description: "Consulta o resumo financeiro de um usuario em um mes e ano especificos: renda total, despesas totais, gastos por fonte de pagamento e saldo restante.",
			Parameters: objectSchema(map[string]any{
				"month": integerSchema("Mes numerico entre 1 e 12."),
				"year":  integerSchema("Ano com quatro digitos, por exemplo 2026."),
			}, []string{"month", "year"}),
		},
		{
			Name:        "get_category_summary",
			Description: "Consulta quanto o usuario gastou por categoria em um mes/ano, ou no ano inteiro se o mes nao for informado.",
			Parameters: objectSchema(map[string]any{
				"month": integerSchema("Mes numerico entre 1 e 12. Opcional."),
				"year":  integerSchema("Ano com quatro digitos, por exemplo 2026."),
			}, []string{"year"}),
		},
		{
			Name:        "list_expenses",
			Description: "Lista despesas do usuario com filtros opcionais por mes, ano, categoria e fonte de pagamento.",
			Parameters: objectSchema(map[string]any{
				"month":          integerSchema("Mes numerico entre 1 e 12. Opcional."),
				"year":           integerSchema("Ano com quatro digitos. Opcional."),
				"category_name":  stringSchema("Nome da categoria para filtrar. Opcional."),
				"payment_source": stringSchema("Fonte usada no pagamento: Salario, Adiantamento ou Renda Extra. Opcional."),
				"limit":          integerSchema("Quantidade maxima de despesas para retornar. Use no maximo 20."),
			}, []string{}),
		},
		{
			Name:        "create_expense",
			Description: "Prepara ou cadastra uma despesa para o usuario. Use confirm=false quando ainda estiver interpretando o pedido. Use confirm=true apenas se a mensagem atual do usuario confirmar claramente um cadastro ja apresentado.",
			Parameters: objectSchema(map[string]any{
				"description":    stringSchema("Descricao curta da despesa, por exemplo Pao ou Uber."),
				"amount":         numberSchema("Valor TOTAL da compra em reais. Para parceladas, o Go divide esse total; nao envie o valor de uma parcela."),
				"category_name":  stringSchema("Nome exato de uma categoria existente do usuario. Se ausente, pergunte antes de confirmar."),
				"payment_source": enumSchema("Fonte usada no pagamento.", []string{"Salario", "Adiantamento", "Renda Extra"}),
				"month":          integerSchema("Mes numerico entre 1 e 12."),
				"year":           integerSchema("Ano com quatro digitos."),
				"day":            integerSchema("Dia informado pelo usuario. Se ausente, pergunte."),
				"type":           enumSchema("Tipo da despesa.", []string{"Unica", "Parcelada", "Fixa"}),
				"installments":   integerSchema("Quantidade de parcelas. Use 1 para despesa unica."),
				"confirm":        boolSchema("Verdadeiro somente quando o usuario confirmar explicitamente o cadastro."),
			}, []string{"description", "amount", "category_name", "payment_source", "month", "year", "day", "type", "installments", "confirm"}),
		},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func integerSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func numberSchema(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func boolSchema(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func enumSchema(description string, values []string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func executeTool(userID uint, call FunctionCall) (any, error) {
	switch call.Name {
	case "get_monthly_summary":
		month, err := intArg(call.Args, "month")
		if err != nil {
			return nil, err
		}
		year, err := intArg(call.Args, "year")
		if err != nil {
			return nil, err
		}
		return getMonthlySummary(userID, month, year)
	case "get_category_summary":
		year, err := intArg(call.Args, "year")
		if err != nil {
			return nil, err
		}
		month, _ := intArg(call.Args, "month")
		return getCategorySummary(userID, month, year)
	case "list_expenses":
		return listExpenses(userID, call.Args)
	case "create_expense":
		return createExpenseFromTool(userID, call.Args)
	default:
		return nil, fmt.Errorf("ferramenta nao suportada: %s", call.Name)
	}
}

func getMonthlySummary(userID uint, month int, year int) (map[string]any, error) {
	if err := validateMonthYear(month, year); err != nil {
		return nil, err
	}

	var incomesList []incomes.Income
	if err := database.DB.Where("user_id = ? AND month = ? AND year = ?", userID, month, year).Find(&incomesList).Error; err != nil {
		return nil, err
	}

	var expensesList []expenses.Expense
	if err := expenses.ApplyEffectivePeriod(database.DB.Preload("PaymentSplits").Where("user_id = ?", userID), month, year).Find(&expensesList).Error; err != nil {
		return nil, err
	}

	var totalIncome, totalExpense float64
	var totalSalary, totalAdiantamento, totalRendaExtra float64
	var totalSpentSalary, totalSpentAdiantamento, totalSpentRendaExtra float64

	for _, income := range incomesList {
		totalIncome += income.Amount
		switch normalizeMoneySource(income.Source) {
		case "salario":
			totalSalary += income.Amount
		case "adiantamento":
			totalAdiantamento += income.Amount
		case "renda_extra":
			totalRendaExtra += income.Amount
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
			}
		}
	}

	return map[string]any{
		"month":                    month,
		"year":                     year,
		"total_income":             roundMoney(totalIncome),
		"total_expense":            roundMoney(totalExpense),
		"total_geral_disponivel":   roundMoney(totalIncome - totalExpense),
		"salario":                  roundMoney(totalSalary),
		"adiantamento":             roundMoney(totalAdiantamento),
		"renda_extra_amt":          roundMoney(totalRendaExtra),
		"total_gasto_salario":      roundMoney(totalSpentSalary),
		"total_gasto_adiantamento": roundMoney(totalSpentAdiantamento),
		"total_gasto_renda_extra":  roundMoney(totalSpentRendaExtra),
		"restante_salario":         roundMoney(totalSalary - totalSpentSalary),
		"restante_adiantamento":    roundMoney(totalAdiantamento - totalSpentAdiantamento),
		"restante_renda_extra":     roundMoney(totalRendaExtra - totalSpentRendaExtra),
	}, nil
}

func getCategorySummary(userID uint, month int, year int) ([]map[string]any, error) {
	if year < 2000 {
		return nil, errors.New("ano invalido")
	}
	if month != 0 {
		if err := validateMonthYear(month, year); err != nil {
			return nil, err
		}
	}

	var expenseItems []expenses.Expense
	query := database.DB.Where("user_id = ?", userID)
	if month != 0 {
		query = expenses.ApplyEffectivePeriod(query, month, year)
	} else {
		query = expenses.ApplyEffectiveYear(query, year)
	}
	if err := query.Find(&expenseItems).Error; err != nil {
		return nil, err
	}

	var categoriesList []categories.Category
	if err := database.DB.Where("user_id = ?", userID).Find(&categoriesList).Error; err != nil {
		return nil, err
	}
	categoryNames := make(map[uint]string, len(categoriesList))
	for _, category := range categoriesList {
		categoryNames[category.ID] = category.Name
	}
	totals := make(map[uint]float64)
	var total float64
	for _, expense := range expenseItems {
		totals[expense.CategoryID] += expense.Amount
		total += expense.Amount
	}

	results := make([]map[string]any, 0, len(totals))
	for categoryID, amount := range totals {
		percentage := 0.0
		if total > 0 {
			percentage = (amount / total) * 100
		}
		name := strings.TrimSpace(categoryNames[categoryID])
		if name == "" {
			name = "Sem categoria"
		}
		results = append(results, map[string]any{
			"category_id":   categoryID,
			"category_name": name,
			"total_amount":  roundMoney(amount),
			"percentage":    roundMoney(percentage),
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return numberToFloat(results[i]["total_amount"]) > numberToFloat(results[j]["total_amount"])
	})

	return results, nil
}

func listExpenses(userID uint, args map[string]any) ([]expenses.Expense, error) {
	query := database.DB.Where("user_id = ?", userID)

	if month, err := intArg(args, "month"); err == nil && month > 0 {
		query = query.Where("month = ?", month)
	}
	if year, err := intArg(args, "year"); err == nil && year > 0 {
		query = query.Where("year = ?", year)
	}
	paymentSource, _ := stringArg(args, "payment_source")
	if categoryName, _ := stringArg(args, "category_name"); categoryName != "" {
		var category categories.Category
		if err := database.DB.Where("user_id = ? AND LOWER(name) = ?", userID, strings.ToLower(categoryName)).First(&category).Error; err != nil {
			return []expenses.Expense{}, nil
		}
		query = query.Where("category_id = ?", category.ID)
	}

	limit, _ := intArg(args, "limit")
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	var expensesList []expenses.Expense
	if err := query.Preload("PaymentSplits").Order("date desc").Find(&expensesList).Error; err != nil {
		return nil, err
	}
	expensesList = filterExpensesByPaymentSource(expensesList, paymentSource)
	if len(expensesList) > limit {
		expensesList = expensesList[:limit]
	}
	return expensesList, nil
}

func validateMonthYear(month int, year int) error {
	if month < 1 || month > 12 {
		return errors.New("mes invalido")
	}
	if year < 2000 {
		return errors.New("ano invalido")
	}
	return nil
}

func normalizeMoneySource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "salario", "salário":
		return "salario"
	case "adiantamento":
		return "adiantamento"
	case "renda extra", "renda_extra":
		return "renda_extra"
	default:
		return ""
	}
}

func normalizePaymentSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "salario", "salário":
		return "Salario"
	case "adiantamento":
		return "Adiantamento"
	case "renda extra", "renda_extra":
		return "Renda Extra"
	default:
		return source
	}
}

func paymentSourceVariants(source string) []string {
	normalized := normalizeMoneySource(source)
	switch normalized {
	case "salario":
		return []string{"salario", "salário", "salã¡rio"}
	case "adiantamento":
		return []string{"adiantamento"}
	case "renda_extra":
		return []string{"renda extra", "renda_extra"}
	default:
		return []string{strings.ToLower(strings.TrimSpace(source))}
	}
}

func normalizeExpenseTypeForCreate(expenseType string) string {
	switch strings.TrimSpace(strings.ToLower(expenseType)) {
	case "unica", "única":
		return "Única"
	case "parcelada":
		return "Parcelada"
	case "fixa":
		return "Fixa"
	default:
		return ""
	}
}

func roundMoney(value float64) float64 {
	return math.Round(value*100) / 100
}

func stringArg(args map[string]any, key string) (string, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return "", fmt.Errorf("argumento obrigatorio ausente: %s", key)
	}
	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("argumento %s deve ser texto", key)
	}
	return strings.TrimSpace(str), nil
}

func intArg(args map[string]any, key string) (int, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return 0, fmt.Errorf("argumento obrigatorio ausente: %s", key)
	}
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed || math.Abs(typed) > 1e9 {
			return 0, fmt.Errorf("argumento %s deve ser inteiro", key)
		}
		return int(typed), nil
	case int:
		return typed, nil
	default:
		return 0, fmt.Errorf("argumento %s deve ser inteiro", key)
	}
}

func floatArg(args map[string]any, key string) (float64, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return 0, fmt.Errorf("argumento obrigatorio ausente: %s", key)
	}
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	default:
		return 0, fmt.Errorf("argumento %s deve ser numero", key)
	}
}

func boolArg(args map[string]any, key string) (bool, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return false, fmt.Errorf("argumento obrigatorio ausente: %s", key)
	}
	boolValue, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("argumento %s deve ser booleano", key)
	}
	return boolValue, nil
}
