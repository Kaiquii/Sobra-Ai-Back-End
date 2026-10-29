package assistant

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Calculations can only use numeric paths in the authenticated snapshot. The
// model supplies the operation, never the result or arbitrary financial inputs.
func verifiedAnalysisCalculations(facts map[string]any, raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	calculations, ok := raw.([]any)
	if !ok || len(calculations) > 12 {
		return nil, fmt.Errorf("invalid calculations")
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return nil, err
	}
	var root any
	if err := json.Unmarshal(encoded, &root); err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(calculations))
	for _, rawCalculation := range calculations {
		calculation, ok := rawCalculation.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid calculation")
		}
		paths, ok := calculation["paths"].([]any)
		if !ok || len(paths) == 0 || len(paths) > 24 {
			return nil, fmt.Errorf("invalid calculation paths")
		}
		values := make([]float64, 0, len(paths))
		for _, rawPath := range paths {
			path, ok := rawPath.(string)
			if !ok {
				return nil, fmt.Errorf("invalid path")
			}
			value, err := analysisNumberAt(root, path)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		value := values[0]
		resultKey := "amount"
		switch calculation["operation"] {
		case "percentage":
			if len(values) != 2 || values[1] == 0 {
				return nil, fmt.Errorf("invalid percentage operands")
			}
			value = values[0] / values[1] * 100
			resultKey = "percentage"
		case "sum", "average":
			for _, operand := range values[1:] {
				value += operand
			}
			if calculation["operation"] == "average" {
				value /= float64(len(values))
			}
		case "subtract":
			for _, operand := range values[1:] {
				value -= operand
			}
		case "min":
			for _, operand := range values[1:] {
				value = math.Min(value, operand)
			}
		case "max":
			for _, operand := range values[1:] {
				value = math.Max(value, operand)
			}
		default:
			return nil, fmt.Errorf("unsupported calculation")
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("invalid result")
		}
		results = append(results, map[string]any{resultKey: roundMoney(value)})
	}
	return results, nil
}

func analysisNumberAt(root any, path string) (float64, error) {
	if !strings.HasPrefix(path, "/") || len(path) > 256 {
		return 0, fmt.Errorf("invalid numeric path")
	}
	var node = root
	for _, part := range strings.Split(path[1:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch typed := node.(type) {
		case map[string]any:
			node = typed[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return 0, fmt.Errorf("invalid index")
			}
			node = typed[index]
		default:
			return 0, fmt.Errorf("invalid numeric path")
		}
	}
	value, ok := node.(float64)
	// Only money fields and monthly money arrays are valid operands. IDs, years
	// and counts must never authorize an invented monetary amount.
	parts := strings.Split(path, "/")
	field := parts[len(parts)-1]
	money := field == "amount" || strings.HasPrefix(field, "total_")
	if len(parts) > 2 && parts[len(parts)-2] == "monthly_amounts_jan_to_dec" {
		money = true
	}
	if !ok || !money || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("path is not a money fact")
	}
	return value, nil
}
