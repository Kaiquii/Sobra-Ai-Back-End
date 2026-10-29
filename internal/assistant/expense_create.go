package assistant

import (
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"encoding/json"
	"errors"
	"fmt"
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
	_, hasFieldMask := intent.provided["provided_fields"]
	categoryChanged := false
	for key, value := range fields {
		if intent.provided != nil {
			if explicit, ok := intent.provided[key]; ok {
				if hasFieldMask && !explicitDraftField(intent.provided, key) {
					continue
				}
				// Models sometimes serialize a full schema with empty defaults even
				// when asked to return a patch. Absence cannot erase a known field.
				if emptyDraftPatchValue(explicit) && !explicitDraftField(intent.provided, key) {
					continue
				}
				args[key] = explicit
				categoryChanged = categoryChanged || key == "category_name"
			}
		} else {
			args[key] = value
		}
	}
	if categoryChanged || intent.provided == nil {
		if suggested, _ := intent.provided["category_suggested"].(bool); suggested {
			args["category_suggested"] = true
		} else if strings.TrimSpace(intent.CategoryName) != "" {
			delete(args, "category_suggested")
		}
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
	result, amounts := prepareExpenseDraft(args)
	if result["status"] == "needs_information" {
		return result, nil
	}
	if db == nil {
		return nil, errors.New("banco indisponivel para validar a categoria")
	}
	name := result["category_name"].(string)
	var category categories.Category
	if err := db.Where("user_id = ? AND LOWER(name) = LOWER(?)", userID, name).First(&category).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if category.ID == 0 {
		result["status"] = "needs_information"
		result["missing_fields"] = []string{"category_name"}
		return result, nil
	}
	result["category_id"], result["category_name"] = category.ID, category.Name
	confirm, _ := args["confirm"].(bool)
	if !confirm {
		result["status"] = "needs_confirmation"
		return result, nil
	}
	description := result["description"].(string)
	source := result["payment_source"].(string)
	kind := result["type"].(string)
	month, year, day := result["month"].(int), result["year"].(int), result["day"].(int)
	installments, count := result["installments"].(int), len(amounts)
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
