package assistant

import (
	"encoding/json"
	"fmt"
	"time"
)

const compactAnalysisCategoryLimit = 8
const compactAnalysisContextLimit = 16000

// Preserve numeric paths and their indices while projecting the same snapshot
// for generation and validation. Exact, exhaustive reads remain available via
// financial_query; a shortened category list never claims complete coverage.
func compactAnalysisFactsForPrompt(facts map[string]any) (map[string]any, error) {
	encoded, err := json.Marshal(facts)
	if err != nil || facts == nil || len(encoded) > 48000 {
		return nil, fmt.Errorf("assistant snapshot unavailable or too large")
	}
	var source map[string]any
	if json.Unmarshal(encoded, &source) != nil {
		return nil, fmt.Errorf("assistant snapshot unavailable")
	}
	// Internal unit callers also use flat snapshots. Keep that contract without
	// mutating their input or silently deleting supported monetary facts.
	if _, present := source["periods"]; !present {
		if len(encoded) > compactAnalysisContextLimit {
			return nil, fmt.Errorf("assistant compact snapshot too large")
		}
		return source, nil
	}
	periods, ok := source["periods"].([]any)
	if !ok || len(periods) > 2 {
		return nil, fmt.Errorf("assistant snapshot periods invalid")
	}
	result := map[string]any{"as_of": source["as_of"], "years": source["years"]}
	date, dateErr := time.Parse("2006-01-02", stringValue(source["as_of"]))
	if dateErr == nil {
		result["calendar"] = map[string]any{"year": date.Year(), "next_year": date.Year() + 1, "month": int(date.Month()), "months_per_year": 12}
	}
	projected := make([]any, 0, len(periods))
	latestKey := 0
	for _, raw := range periods {
		period, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("assistant snapshot period invalid")
		}
		view := copyAnalysisFields(period, "year", "total_income", "total_expense", "remaining_months_including_current", "total_registered_cash_balance_until_december")
		months, ok := period["months_with_records"].([]any)
		if !ok || len(months) > 12 {
			return nil, fmt.Errorf("assistant snapshot months invalid")
		}
		monthViews := make([]any, 0, len(months))
		for _, rawMonth := range months {
			month, ok := rawMonth.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("assistant snapshot month invalid")
			}
			item := copyAnalysisFields(month, "month", "total_income", "total_expense", "total_balance", "total_cash_income", "total_cash_expense", "total_cash_balance", "cash_income_registered", "expenses_registered")
			monthViews = append(monthViews, item)
			if dateErr == nil {
				year, number := intValue(period["year"]), intValue(month["month"])
				key := year*100 + number
				if year >= 2000 && number >= 1 && number <= 12 && key <= date.Year()*100+int(date.Month()) {
					alias := copyAnalysisFields(item, "month", "total_cash_income", "total_cash_expense", "total_cash_balance", "cash_income_registered", "expenses_registered")
					alias["year"] = year
					if key > latestKey {
						latestKey = key
						result["latest_registered_month"] = alias
					}
					if key == date.Year()*100+int(date.Month()) {
						result["current_month"] = alias
					}
				}
			}
		}
		view["months_with_records"] = monthViews
		categories, ok := period["categories"].([]any)
		if !ok {
			return nil, fmt.Errorf("assistant snapshot categories invalid")
		}
		shown := make([]any, 0, min(len(categories), compactAnalysisCategoryLimit))
		for _, rawCategory := range categories[:min(len(categories), compactAnalysisCategoryLimit)] {
			category, ok := rawCategory.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("assistant snapshot category invalid")
			}
			shown = append(shown, copyAnalysisFields(category, "name", "total_amount", "highest_month", "total_highest_month", "monthly_amounts_jan_to_dec"))
		}
		view["categories"] = shown
		view["categories_complete"] = period["categories_complete"] == true && len(categories) <= compactAnalysisCategoryLimit
		view["represented_category_count"] = max(len(categories), intValue(period["represented_category_count"]))
		projected = append(projected, view)
	}
	result["periods"] = projected
	for {
		data, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("assistant compact snapshot invalid")
		}
		if len(data) <= compactAnalysisContextLimit {
			break
		}
		// Long category labels must not make an otherwise bounded cash snapshot
		// unusable. Keep a prefix, its original indices and explicit incomplete
		// coverage; never discard monthly totals or silently claim all categories.
		var selected map[string]any
		largest := 0
		for _, raw := range projected {
			view := raw.(map[string]any)
			if count := len(view["categories"].([]any)); count > largest {
				selected, largest = view, count
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("assistant compact snapshot too large")
		}
		selected["categories"] = selected["categories"].([]any)[:largest-1]
		selected["categories_complete"] = false
	}
	return result, nil
}

func copyAnalysisFields(source map[string]any, keys ...string) map[string]any {
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, exists := source[key]; exists {
			result[key] = value
		}
	}
	return result
}
