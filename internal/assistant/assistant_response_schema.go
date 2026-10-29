package assistant

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
)

// Native strict endpoints require every declared property. Null represents an
// unused optional value; only the native response adapter removes those nulls.
// All financial validation and authorization still belong to the Go executor.
func assistantResponseSchema(categorySets ...[]string) map[string]any {
	period := assistantSchemaObject(map[string]any{
		"year":   map[string]any{"type": "integer"},
		"months": map[string]any{"type": "array", "items": assistantSchemaEnum("integer", assistantSchemaIntegers(1, 12)...), "description": "Meses solicitados; [] indica o ano inteiro."},
	})
	limit := assistantNullableSchema(assistantSchemaEnum("integer", assistantSchemaIntegers(1, 24)...))
	limit["description"] = "Quantidade pedida; null se nao pedida. Ordinal usa 1."
	position := assistantNullableSchema(assistantSchemaEnum("integer", assistantSchemaIntegers(1, 24)...))
	position["description"] = "Ordinal em max/min: 2o=2; null fora disso. Nao e quantidade."
	category := assistantCategoryNameSchema(categorySets...)
	category["description"] = "Categoria solicitada; '' se nao pedida. Nao inferir."
	source := assistantSchemaEnum("string", "", "Salario", "Adiantamento", "Renda Extra", "VA", "VR")
	source["description"] = "Fonte solicitada; '' se nao pedida."
	query := assistantSchemaObject(map[string]any{
		"periods":        map[string]any{"type": "array", "items": period, "description": "Todos os periodos solicitados, em ate dois anos."},
		"category_name":  category,
		"payment_source": source,
		"group_by":       assistantSchemaEnum("string", "month", "category", "expense", "installment", "purchase", "clarify_installment_or_purchase"),
		"operation":      assistantSchemaEnum("string", "list", "compare", "max", "min", "sum", "average", "review", "budget"),
		"limit":          limit,
		"position":       position,
	})
	query["description"] = "Filtros vazios significam sem filtro."
	create := assistantSchemaObject(map[string]any{
		"intent":             assistantSchemaEnum("string", "create_expense"),
		"description":        assistantNullableSchema(map[string]any{"type": "string"}),
		"category_name":      assistantNullableSchema(assistantCategoryNameSchema(categorySets...)),
		"payment_source":     assistantNullableSchema(assistantSchemaEnum("string", "", "Salario", "Adiantamento", "Renda Extra", "VA", "VR")),
		"amount":             assistantNullableSchema(map[string]any{"type": "number"}),
		"month":              assistantNullableSchema(map[string]any{"type": "integer"}),
		"year":               assistantNullableSchema(map[string]any{"type": "integer"}),
		"day":                assistantNullableSchema(map[string]any{"type": "integer"}),
		"type":               assistantNullableSchema(assistantSchemaEnum("string", "Unica", "Parcelada", "Fixa")),
		"installments":       assistantNullableSchema(map[string]any{"type": "integer"}),
		"new_expense":        assistantNullableSchema(map[string]any{"type": "boolean"}),
		"category_suggested": assistantNullableSchema(map[string]any{"type": "boolean"}),
		"provided_fields":    map[string]any{"type": "array", "items": assistantSchemaEnum("string", "description", "category_name", "payment_source", "amount", "month", "year", "day", "type", "installments")},
	})
	// The portable strict subset permits nested anyOf, but the root must remain
	// an object. Distinct intent enums make these closed action shapes exclusive.
	return assistantSchemaObject(map[string]any{
		"action": map[string]any{"anyOf": []any{
			assistantSchemaObject(map[string]any{"intent": assistantSchemaEnum("string", "financial_query"), "query": query}),
			create,
			assistantSchemaObject(map[string]any{"intent": assistantSchemaEnum("string", "general", "app_help"), "reply": map[string]any{"type": "string"}}),
			assistantSchemaObject(map[string]any{"intent": assistantSchemaEnum("string", "monthly_summary"), "month": assistantNullableSchema(map[string]any{"type": "integer"}), "year": assistantNullableSchema(map[string]any{"type": "integer"})}),
			assistantSchemaObject(map[string]any{"intent": assistantSchemaEnum("string", "list_categories", "patrimony_summary", "fgts_balance", "list_investments", "cancel_expense")}),
		}},
	})
}

func assistantResponseFormat(categorySets ...[]string) map[string]any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name": "sobraai_assistant", "strict": true, "schema": assistantResponseSchema(categorySets...),
		},
	}
}

// Nil means unknown to legacy/internal callers. A non-nil empty slice is an
// authenticated empty catalog and allows only the no-category value.
func assistantCategoryNameSchema(categorySets ...[]string) map[string]any {
	if len(categorySets) == 0 || categorySets[0] == nil {
		return map[string]any{"type": "string"}
	}
	values := []any{""}
	seen := map[string]bool{"": true}
	for _, name := range categorySets[0] {
		if !seen[name] {
			values = append(values, name)
			seen[name] = true
		}
	}
	return assistantSchemaEnum("string", values...)
}

func assistantSchemaObject(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for property := range properties {
		required = append(required, property)
	}
	sort.Strings(required)
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func assistantSchemaEnum(kind string, values ...any) map[string]any {
	return map[string]any{"type": kind, "enum": values}
}

func assistantSchemaIntegers(first, last int) []any {
	values := make([]any, 0, last-first+1)
	for value := first; value <= last; value++ {
		values = append(values, value)
	}
	return values
}

func assistantNullableSchema(schema map[string]any) map[string]any {
	schema["type"] = []string{schema["type"].(string), "null"}
	if values, ok := schema["enum"].([]any); ok {
		schema["enum"] = append(values, nil)
	}
	return schema
}

// This adapter is used exclusively after requesting the native schema. It must
// never turn required query nulls into a wider scope or erase an expense mask.
func normalizeAssistantSchemaResponse(text string) (string, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &object) != nil || object == nil {
		return "", errors.New("invalid structured response")
	}
	if raw, wrapped := object["action"]; wrapped {
		// Flat legacy responses remain readable for existing mocks and replays.
		// An envelope cannot also carry a second, conflicting root-level action.
		var action map[string]json.RawMessage
		if len(object) != 1 || json.Unmarshal(raw, &action) != nil || action == nil {
			return "", errors.New("invalid structured response")
		}
		object = action
	}
	var intent string
	_ = json.Unmarshal(object["intent"], &intent)
	if intent == "financial_query" {
		// Query values come from the Go read executor. Even a schema-capable
		// model may fill unused draft/reply fields; none may become financial facts.
		for field := range object {
			if field != "intent" && field != "query" {
				delete(object, field)
			}
		}
	}
	masked := map[string]bool{}
	if intent == "create_expense" {
		var fields []string
		_ = json.Unmarshal(object["provided_fields"], &fields)
		for _, field := range fields {
			masked[field] = true
		}
	}
	for field, value := range object {
		if !assistantSchemaNull(value) || field == "provided_fields" || masked[field] || (field == "query" && intent == "financial_query") {
			continue
		}
		delete(object, field)
	}
	if raw, exists := object["query"]; exists && !assistantSchemaNull(raw) {
		var query map[string]json.RawMessage
		if json.Unmarshal(raw, &query) != nil || query == nil {
			return "", errors.New("invalid structured query")
		}
		for _, control := range []string{"limit", "position"} {
			if assistantSchemaNull(query[control]) {
				delete(query, control)
			}
		}
		var operation string
		_ = json.Unmarshal(query["operation"], &operation)
		if operation != "max" && operation != "min" {
			delete(query, "position")
		}
		encoded, err := json.Marshal(query)
		if err != nil {
			return "", errors.New("invalid structured query")
		}
		object["query"] = encoded
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return "", errors.New("invalid structured response")
	}
	return string(encoded), nil
}

func assistantSchemaNull(value json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}
