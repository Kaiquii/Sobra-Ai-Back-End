package assistant

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

const maxAnalysisReplyReferences = 24

var errAnalysisReference = errors.New("invalid analysis reference")

// References format only authenticated money facts or server-verified results.
// The rendered reply must still pass the normal financial-number validation.
func renderAnalysisReferences(reply string, facts map[string]any, calculations []map[string]any) (string, error) {
	if !strings.Contains(reply, "{{") && !strings.Contains(reply, "}}") {
		return reply, nil
	}
	if facts == nil {
		return "", errAnalysisReference
	}
	// Normalize native slices/maps and decoded JSON without interpreting strings
	// as numbers. The same normalization is used by calculation verification.
	encoded, err := json.Marshal(map[string]any{"facts": facts, "calculations": calculations})
	if err != nil {
		return "", errAnalysisReference
	}
	var data map[string]any
	if json.Unmarshal(encoded, &data) != nil {
		return "", errAnalysisReference
	}
	results, _ := data["calculations"].([]any)
	var rendered strings.Builder
	remaining, references := reply, 0
	for {
		start := strings.Index(remaining, "{{")
		if start < 0 {
			if strings.ContainsAny(remaining, "{}") {
				return "", errAnalysisReference
			}
			rendered.WriteString(remaining)
			return rendered.String(), nil
		}
		if strings.ContainsAny(remaining[:start], "{}") {
			return "", errAnalysisReference
		}
		rendered.WriteString(remaining[:start])
		remaining = remaining[start+2:]
		end := strings.Index(remaining, "}}")
		if end < 0 || strings.ContainsAny(remaining[:end], "{}") {
			return "", errAnalysisReference
		}
		references++
		if references > maxAnalysisReplyReferences {
			return "", errAnalysisReference
		}
		reference := remaining[:end]
		switch {
		case strings.HasPrefix(reference, "fact:"):
			value, err := analysisNumberAt(data["facts"], strings.TrimPrefix(reference, "fact:"))
			if err != nil {
				return "", errAnalysisReference
			}
			rendered.WriteString("R$ " + formatMoney(value))
		case strings.HasPrefix(reference, "calc:"):
			indexText := strings.TrimPrefix(reference, "calc:")
			index, err := strconv.Atoi(indexText)
			if err != nil || index < 0 || index >= len(results) || strconv.Itoa(index) != indexText {
				return "", errAnalysisReference
			}
			result, ok := results[index].(map[string]any)
			if !ok || len(result) != 1 {
				return "", errAnalysisReference
			}
			key := "amount"
			if _, ok := result["percentage"]; ok {
				key = "percentage"
			}
			value, ok := result[key].(float64)
			if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
				return "", errAnalysisReference
			}
			if key == "percentage" {
				rendered.WriteString(formatPercentage(value) + "%")
			} else {
				rendered.WriteString("R$ " + formatMoney(value))
			}
		default:
			return "", errAnalysisReference
		}
		remaining = remaining[end+2:]
	}
}
