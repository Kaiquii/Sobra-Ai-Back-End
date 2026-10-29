package assistant

import (
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
)

const expenseDraftVersion = 2

// Only explicit fields from the new interpretation replace a pending draft.
// Confirmation is accepted by the HTTP conversation flow, never by the model.
func expenseArgsFromIntent(intent financialIntent, drafts ...map[string]any) map[string]any {
	args := map[string]any{}
	newExpense, _ := intent.provided["new_expense"].(bool)
	if len(drafts) > 0 && !newExpense {
		for key, value := range drafts[0] {
			args[key] = value
		}
	}
	fields := map[string]any{"description": intent.Description, "category_name": intent.CategoryName, "payment_source": intent.PaymentSource, "amount": intent.Amount, "month": intent.Month, "year": intent.Year, "day": intent.Day, "type": intent.Type, "installments": intent.Installments}
	for key, value := range fields {
		if intent.provided != nil {
			if explicit, ok := intent.provided[key]; ok {
				// Models sometimes serialize a full schema with empty defaults even
				// when asked to return a patch. Absence cannot erase a known field.
				if emptyDraftPatchValue(explicit) && !explicitDraftField(intent.provided, key) {
					continue
				}
				args[key] = explicit
			}
		} else {
			args[key] = value
		}
	}
	if suggested, _ := intent.provided["category_suggested"].(bool); suggested {
		args["category_suggested"] = true
	} else if strings.TrimSpace(intent.CategoryName) != "" {
		delete(args, "category_suggested")
	}
	delete(args, "date")
	args["confirm"] = false
	return args
}

func emptyDraftPatchValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case float64:
		return typed == 0
	case int:
		return typed == 0
	}
	return false
}

func explicitDraftField(raw map[string]any, key string) bool {
	fields, _ := raw["provided_fields"].([]any)
	for _, field := range fields {
		if name, ok := field.(string); ok && name == key {
			return true
		}
	}
	return false
}

func expenseDraftContext(drafts ...map[string]any) string {
	if len(drafts) == 0 {
		return "null"
	}
	compact := map[string]any{}
	for _, key := range []string{"description", "category_name", "payment_source", "amount", "month", "year", "day", "type", "installments", "status", "missing_fields", "category_suggested"} {
		if value, exists := drafts[0][key]; exists {
			compact[key] = value
		}
	}
	encoded, err := json.Marshal(compact)
	if err != nil {
		return "null"
	}
	return string(encoded)
}

func createExpenseFromTool(userID uint, args map[string]any) (map[string]any, error) {
	var result map[string]any
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = createExpenseFromToolDB(tx, userID, args)
		return err
	})
	return result, err
}

func createExpenseFromToolDB(db *gorm.DB, userID uint, args map[string]any) (map[string]any, error) {
	result := map[string]any{"draft_version": expenseDraftVersion}
	for key, value := range args {
		if key != "confirm" && key != "status" && key != "draft_version" {
			result[key] = value
		}
	}
	missing := []string{}
	description, _ := stringArg(args, "description")
	if description == "" || len([]rune(description)) > 255 {
		missing = append(missing, "description")
	}
	result["description"] = description
	name, _ := stringArg(args, "category_name")
	var category categories.Category
	if name != "" {
		err := db.Where("user_id = ? AND LOWER(name) = LOWER(?)", userID, name).First(&category).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if category.ID == 0 {
		missing = append(missing, "category_name")
	} else {
		name = category.Name
	}
	result["category_name"] = name
	source, _ := stringArg(args, "payment_source")
	source = normalizePaymentSource(source)
	if normalizeMoneySource(source) == "" {
		missing = append(missing, "payment_source")
	}
	result["payment_source"] = source
	amount, _ := floatArg(args, "amount")
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) || amount > 1e10 || math.Abs(amount*100-math.Round(amount*100)) > 0.0001 {
		missing = append(missing, "amount")
	}
	result["amount"] = amount
	month, monthErr := intArg(args, "month")
	year, yearErr := intArg(args, "year")
	day, dayErr := intArg(args, "day")
	date := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.Local)
	if monthErr != nil || yearErr != nil || dayErr != nil || year < 2000 || year > 9999 || month < 1 || month > 12 || day < 1 || date.Day() != day || int(date.Month()) != month || date.Year() != year {
		missing = append(missing, "date")
	}
	result["month"] = month
	result["year"] = year
	result["day"] = day
	result["date"] = date.Format("2006-01-02")
	kind, _ := stringArg(args, "type")
	kind = normalizeExpenseTypeForCreate(kind)
	if kind == "" {
		missing = append(missing, "type")
	}
	result["type"] = kind
	count := 1
	installments := 1
	if kind == "Parcelada" {
		var err error
		installments, err = intArg(args, "installments")
		if err != nil || installments < 2 || installments > 120 {
			missing = append(missing, "installments")
		} else {
			count = installments
		}
	} else if kind == "Fixa" {
		count = 120
		installments = 0
	}
	result["installments"] = installments
	if len(missing) > 0 {
		result["status"] = "needs_information"
		result["missing_fields"] = missing
		return result, nil
	}
	cents := int64(math.Round(amount * 100))
	if kind == "Parcelada" && cents < int64(count) {
		result["status"] = "needs_information"
		result["missing_fields"] = []string{"amount"}
		return result, nil
	}
	amounts := make([]float64, count)
	for i := range amounts {
		part := cents
		if kind == "Parcelada" {
			part = cents / int64(count)
			if int64(i) < cents%int64(count) {
				part++
			}
		}
		amounts[i] = float64(part) / 100
	}
	result["installment_amounts"] = amounts
	confirm, _ := args["confirm"].(bool)
	if !confirm {
		result["status"] = "needs_confirmation"
		return result, nil
	}
	series := ""
	if count > 1 {
		series = fmt.Sprintf("assistant-%d-%d", userID, time.Now().UnixNano())
	}
	for i, value := range amounts {
		// Start from the first of each target month to avoid AddDate overflow.
		start := time.Date(year, time.Month(month)+time.Month(i), 1, 12, 0, 0, 0, time.Local)
		lastDay := start.AddDate(0, 1, -1).Day()
		targetDay := day
		if targetDay > lastDay {
			targetDay = lastDay
		}
		target := time.Date(start.Year(), start.Month(), targetDay, 12, 0, 0, 0, time.Local)
		expense := expenses.Expense{UserID: userID, Description: description, CategoryID: category.ID, Amount: value, PaymentSource: source, Type: kind, Date: target, Month: int(target.Month()), Year: target.Year(), SeriesID: series, Installments: installments, CurrentInstall: i + 1}
		if err := db.Create(&expense).Error; err != nil {
			return nil, err
		}
		if err := db.Create(&expenses.PaymentSplit{ExpenseID: expense.ID, PaymentSource: source, Amount: value}).Error; err != nil {
			return nil, err
		}
	}
	result["status"] = "created"
	result["created_count"] = count
	return result, nil
}
