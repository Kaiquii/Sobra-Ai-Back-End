package assistant

import (
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
)

type financialIntent struct {
	Intent        string  `json:"intent"`
	Reply         string  `json:"reply"`
	Month         int     `json:"month"`
	Year          int     `json:"year"`
	CategoryName  string  `json:"category_name"`
	PaymentSource string  `json:"payment_source"`
	Description   string  `json:"description"`
	Amount        float64 `json:"amount"`
	Day           int     `json:"day"`
	Type          string  `json:"type"`
	Installments  int     `json:"installments"`
	provided      map[string]any
	facts         map[string]any
}

func handleFinancialMessageWithContext(client textGenerator, userID uint, message string, drafts ...map[string]any) (bool, string, string, any) {
	return handleFinancialMessageWithHistory(context.Background(), client, userID, message, nil, drafts...)
}

func handleFinancialMessageWithHistory(ctx context.Context, client textGenerator, userID uint, message string, history []Message, drafts ...map[string]any) (bool, string, string, any) {
	intent, err := interpretFinancialIntentWithHistory(client, userID, message, history, drafts...)
	if err != nil {
		reply, code, retryAfter := assistantErrorResponse(err)
		return true, reply, "", map[string]any{"error_code": code, "retry_after_seconds": retryAfter}
	}

	log.Printf("Assistant intent interpreted")

	switch intent.Intent {
	case "financial_analysis":
		return analysisReply(intent)
	case "cancel_expense":
		result := map[string]any{"status": "cancelled"}
		return true, localToolReply("create_expense", result), "create_expense", result
	case "general", "app_help":
		reply := strings.TrimSpace(intent.Reply)
		if financialMoneyPattern.MatchString(reply) {
			// Classification cannot bypass the same provenance check used for
			// personalized analysis by labelling financial prose as small talk.
			return analysisReply(intent)
		}
		if reply == "" {
			reply = "Claro. Como posso te ajudar com suas financas?"
		}
		return true, reply, intent.Intent, nil
	case "patrimony_summary", "fgts_balance", "list_investments":
		result, err := assistantPatrimonyData(ctx, userID, intent.Intent)
		if err != nil {
			return financialReadError()
		}
		return true, naturalFinancialReply(client, message, history, intent.Intent, result), intent.Intent, result

	case "create_expense":
		args := expenseArgsFromIntent(intent, drafts...)

		result, err := createExpenseFromTool(userID, args)
		if err != nil {
			return true, "Nao consegui validar o cadastro agora. Tente novamente; nenhuma despesa foi cadastrada.", "", map[string]any{"error_code": "expense_validation_failed"}
		}

		reply := localToolReply("create_expense", result)
		return true, reply, "create_expense", result

	case "category_expenses":
		if strings.TrimSpace(intent.CategoryName) == "" {
			result, err := getAllCategoryExpenseSummary(userID, intent.Month, intent.Year, intent.PaymentSource)
			if err != nil {
				return financialReadError()
			}

			reply := naturalFinancialReply(client, message, history, "get_all_category_expenses", result)
			return true, reply, "get_all_category_expenses", result
		}

		result, err := getCategoryExpenseSummary(userID, intent.Month, intent.Year, intent.CategoryName, intent.PaymentSource)
		if err != nil {
			return financialReadError()
		}

		reply := naturalFinancialReply(client, message, history, "get_category_expenses", result)
		return true, reply, "get_category_expenses", result

	case "list_categories":
		result, err := listUserCategories(userID)
		if err != nil {
			return financialReadError()
		}

		reply := localToolReply("list_categories", result)
		return true, reply, "list_categories", result

	case "all_category_expenses":
		result, err := getAllCategoryExpenseSummary(userID, intent.Month, intent.Year, intent.PaymentSource)
		if err != nil {
			return financialReadError()
		}

		reply := naturalFinancialReply(client, message, history, "get_all_category_expenses", result)
		return true, reply, "get_all_category_expenses", result

	case "monthly_summary":
		result, err := getMonthlySummary(userID, intent.Month, intent.Year)
		if err != nil {
			return financialReadError()
		}

		reply := naturalFinancialReply(client, message, history, "get_monthly_summary", result)
		return true, reply, "get_monthly_summary", result

	default:
		return true, "Posso consultar seus gastos, rendas, categorias, FGTS e investimentos, ou preparar uma despesa. Qual informacao voce precisa?", "", nil
	}
}

func interpretFinancialIntent(client textGenerator, userID uint, message string, drafts ...map[string]any) (financialIntent, error) {
	return interpretFinancialIntentWithHistory(client, userID, message, nil, drafts...)
}

func interpretFinancialIntentWithHistory(client textGenerator, userID uint, message string, history []Message, drafts ...map[string]any) (financialIntent, error) {
	now := assistantNow()
	facts, err := analysisContextForClient(client, userID, message, history)
	if err != nil {
		return financialIntent{}, fmt.Errorf("assistant financial context unavailable")
	}
	prompt := financialInterpreterPrompt(now, categoryNamesForPrompt(userID), facts, drafts...)
	text, err := client.generateText(GeminiRequest{
		SystemInstruction: &GeminiContent{Parts: []GeminiPart{{Text: prompt}}},
		Contents:          buildConversationFromMessages(compactFinancialHistory(history), message),
		GenerationConfig:  map[string]any{"temperature": 0},
		DebugStage:        "assistant_intent",
	})
	if err != nil {
		return financialIntent{}, err
	}

	text = cleanJSONText(text)
	intent, err := parseFinancialIntent(text)
	if err != nil {
		log.Printf("Assistant intent: resposta estruturada invalida")
		return financialIntent{}, err
	}

	if intent.Intent != "create_expense" {
		if intent.Month == 0 {
			intent.Month = int(now.Month())
		}
		if intent.Year == 0 {
			intent.Year = now.Year()
		}
		if intent.Day == 0 {
			intent.Day = 1
		}
		if intent.Type == "" {
			intent.Type = "Unica"
		}
		if intent.Installments == 0 {
			intent.Installments = 1
		}
	}
	intent.facts = facts

	return normalizeInterpretedIntent(intent), nil
}

func categoryNamesForPrompt(userID uint) string {
	var categoriesList []categories.Category
	if err := database.DB.Where("user_id = ?", userID).Order("name asc").Find(&categoriesList).Error; err != nil {
		return "nenhuma"
	}

	names := make([]string, 0, len(categoriesList))
	for _, category := range categoriesList {
		name := strings.TrimSpace(category.Name)
		if name != "" {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return "nenhuma"
	}
	return strings.Join(names, ", ")
}

func normalizeInterpretedIntent(intent financialIntent) financialIntent {
	intent.Intent = strings.TrimSpace(intent.Intent)
	if strings.TrimSpace(intent.CategoryName) == "" {
		return intent
	}

	switch intent.Intent {
	case "monthly_summary", "all_category_expenses":
		intent.Intent = "category_expenses"
	}

	return intent
}

func cleanJSONText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(text[start : end+1])
	}

	return text
}

func parseFinancialIntent(text string) (financialIntent, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return financialIntent{}, err
	}
	if !validAssistantIntent(stringValue(raw["intent"])) {
		return financialIntent{}, fmt.Errorf("unsupported assistant intent")
	}

	return financialIntent{
		provided:      raw,
		Intent:        stringValue(raw["intent"]),
		Reply:         stringValue(raw["reply"]),
		Month:         intValue(raw["month"]),
		Year:          intValue(raw["year"]),
		CategoryName:  stringValue(raw["category_name"]),
		PaymentSource: stringValue(raw["payment_source"]),
		Description:   stringValue(raw["description"]),
		Amount:        floatValue(raw["amount"]),
		Day:           intValue(raw["day"]),
		Type:          stringValue(raw["type"]),
		Installments:  intValue(raw["installments"]),
	}, nil
}

func validAssistantIntent(intent string) bool {
	switch intent {
	case "general", "app_help", "financial_analysis", "monthly_summary", "category_expenses", "list_categories", "all_category_expenses", "create_expense", "cancel_expense", "patrimony_summary", "fgts_balance", "list_investments":
		return true
	default:
		return false
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return 0
		}
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func floatValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case string:
		typed = strings.TrimSpace(strings.ReplaceAll(typed, ",", "."))
		if typed == "" {
			return 0
		}
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func getCategoryExpenseSummary(userID uint, month int, year int, categoryName string, paymentSource string) (map[string]any, error) {
	var category categories.Category
	err := database.DB.
		Where("user_id = ? AND LOWER(name) = ?", userID, strings.ToLower(strings.TrimSpace(categoryName))).
		First(&category).Error
	if err != nil {
		return map[string]any{
			"month":          month,
			"year":           year,
			"category_name":  categoryName,
			"payment_source": paymentSource,
			"total_expense":  0,
			"monthly_total":  0,
			"percentage":     0,
			"expenses":       []expenses.Expense{},
		}, nil
	}

	query := expenses.ApplyEffectivePeriod(database.DB.Where("user_id = ? AND category_id = ?", userID, category.ID), month, year)

	var expensesList []expenses.Expense
	if err := query.Preload("PaymentSplits").Order("date desc").Find(&expensesList).Error; err != nil {
		return nil, err
	}
	expensesList = filterExpensesByPaymentSource(expensesList, paymentSource)

	total := 0.0
	for _, expense := range expensesList {
		total += expense.Amount
	}

	monthlyTotal, err := getMonthlyExpenseTotal(userID, month, year, paymentSource)
	if err != nil {
		return nil, err
	}

	percentage := 0.0
	if monthlyTotal > 0 {
		percentage = (total / monthlyTotal) * 100
	}

	return map[string]any{
		"month":          month,
		"year":           year,
		"category_id":    category.ID,
		"category_name":  category.Name,
		"payment_source": paymentSource,
		"total_expense":  roundMoney(total),
		"monthly_total":  roundMoney(monthlyTotal),
		"percentage":     roundMoney(percentage),
		"expenses":       expensesList,
	}, nil
}

func getMonthlyExpenseTotal(userID uint, month int, year int, paymentSource string) (float64, error) {
	var items []expenses.Expense
	if err := expenses.ApplyEffectivePeriod(database.DB.Preload("PaymentSplits").Where("user_id = ?", userID), month, year).Find(&items).Error; err != nil {
		return 0, err
	}
	items = filterExpensesByPaymentSource(items, paymentSource)
	var total float64
	for _, item := range items {
		total += item.Amount
	}
	return total, nil
}

func listUserCategories(userID uint) (map[string]any, error) {
	var categoriesList []categories.Category
	if err := database.DB.Where("user_id = ?", userID).Order("name asc").Find(&categoriesList).Error; err != nil {
		return nil, err
	}

	names := make([]string, 0, len(categoriesList))
	for _, category := range categoriesList {
		names = append(names, category.Name)
	}

	return map[string]any{
		"total":      len(categoriesList),
		"categories": categoriesList,
		"names":      names,
	}, nil
}

func getAllCategoryExpenseSummary(userID uint, month int, year int, paymentSource string) (map[string]any, error) {
	type row struct {
		CategoryID   uint
		CategoryName string
		TotalAmount  float64
	}

	var expenseItems []expenses.Expense
	if err := expenses.ApplyEffectivePeriod(database.DB.Preload("PaymentSplits").Where("user_id = ?", userID), month, year).Find(&expenseItems).Error; err != nil {
		return nil, err
	}
	expenseItems = filterExpensesByPaymentSource(expenseItems, paymentSource)
	totals := make(map[uint]float64)
	for _, expense := range expenseItems {
		totals[expense.CategoryID] += expense.Amount
	}
	rows := make([]row, 0, len(totals))
	for categoryID, totalAmount := range totals {
		var category categories.Category
		if err := database.DB.Where("id = ? AND user_id = ?", categoryID, userID).First(&category).Error; err != nil {
			continue
		}
		rows = append(rows, row{CategoryID: categoryID, CategoryName: category.Name, TotalAmount: totalAmount})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TotalAmount > rows[j].TotalAmount })

	items := make([]map[string]any, 0, len(rows))
	total := 0.0
	for _, row := range rows {
		total += row.TotalAmount
		items = append(items, map[string]any{
			"category_id":   row.CategoryID,
			"category_name": row.CategoryName,
			"total_amount":  roundMoney(row.TotalAmount),
		})
	}

	return map[string]any{
		"month":          month,
		"year":           year,
		"payment_source": paymentSource,
		"total_expense":  roundMoney(total),
		"categories":     items,
	}, nil
}
