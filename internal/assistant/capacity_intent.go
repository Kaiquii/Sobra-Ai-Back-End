package assistant

import (
	"fmt"
	"time"
)

const currentSavingsCapacityIntent = "current_savings_capacity"
const periodSavingsOperation = "period_savings"

// The model selects a task, not the calendar of the current cash snapshot. Only
// this argument-free task is compiled; all legacy query scopes remain intact.
func parseInterpretedFinancialIntent(text string, now time.Time) (financialIntent, error) {
	intent, err := parseFinancialIntent(cleanJSONText(text))
	if err != nil || intent.Intent != currentSavingsCapacityIntent {
		return intent, err
	}
	if now.IsZero() || now.Year() < 2000 || now.Year() > 9999 {
		return financialIntent{}, fmt.Errorf("assistant current period unavailable")
	}
	query := map[string]any{
		"operation": "savings", "group_by": "month", "category_name": "", "payment_source": "",
		"periods": []any{map[string]any{"year": now.Year(), "months": []any{int(now.Month())}}},
	}
	// Do not retain model-supplied dates, filters, prose or write fields, even
	// when a legacy JSON endpoint sends extras forbidden by the native schema.
	return financialIntent{Intent: "financial_query", provided: map[string]any{"intent": "financial_query", "query": query}}, nil
}
