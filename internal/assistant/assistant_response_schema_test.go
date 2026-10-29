package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

func responseSchemaTestJSON(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func responseSchemaTestFixture(intent string) map[string]any {
	return map[string]any{
		"intent": intent, "reply": nil, "query": nil, "month": nil, "year": nil, "day": nil,
		"installments": nil, "amount": nil, "description": nil, "category_name": nil,
		"payment_source": nil, "type": nil, "new_expense": nil, "category_suggested": nil,
		"provided_fields": []any{},
	}
}

func responseSchemaTestQuery() map[string]any {
	return map[string]any{
		"periods":       []any{map[string]any{"year": float64(2026), "months": []any{float64(10), float64(11)}}},
		"category_name": "Uber/99", "payment_source": "", "group_by": "month", "operation": "compare",
		"limit": nil, "position": nil,
	}
}

func responseSchemaTestNativeFixture(intent string) map[string]any {
	var action map[string]any
	switch intent {
	case "financial_query":
		action = map[string]any{"intent": intent, "query": responseSchemaTestQuery()}
	case "create_expense":
		action = responseSchemaTestFixture(intent)
		delete(action, "query")
		delete(action, "reply")
	case "general", "app_help":
		action = map[string]any{"intent": intent, "reply": "Ola!"}
	case "monthly_summary":
		action = map[string]any{"intent": intent, "month": nil, "year": nil}
	default:
		action = map[string]any{"intent": intent}
	}
	return map[string]any{"action": action}
}

func responseSchemaTestActionSchema(t *testing.T, schema map[string]any, intent string) map[string]any {
	t.Helper()
	action := schema["properties"].(map[string]any)["action"].(map[string]any)
	for _, rawBranch := range action["anyOf"].([]any) {
		branch := rawBranch.(map[string]any)
		intentSchema := branch["properties"].(map[string]any)["intent"].(map[string]any)
		for _, candidate := range intentSchema["enum"].([]any) {
			if candidate == intent {
				return branch
			}
		}
	}
	t.Fatalf("native action branch missing for %s", intent)
	return nil
}

// This test-only validator covers the schema vocabulary used by the provider.
// It verifies fixtures independently of the Go interpreter's permissive legacy
// parsing; it does not add a production dependency or another model call.
func responseSchemaTestValidate(schema map[string]any, value any) error {
	if alternatives, ok := schema["anyOf"].([]any); ok {
		for _, raw := range alternatives {
			if responseSchemaTestValidate(raw.(map[string]any), value) == nil {
				return nil
			}
		}
		return fmt.Errorf("value does not match any action branch")
	}
	types := []any{schema["type"]}
	if union, ok := schema["type"].([]any); ok {
		types = union
	}
	validType := false
	for _, rawType := range types {
		switch rawType {
		case "null":
			validType = validType || value == nil
		case "string":
			_, ok := value.(string)
			validType = validType || ok
		case "boolean":
			_, ok := value.(bool)
			validType = validType || ok
		case "number", "integer":
			number, ok := value.(float64)
			validType = validType || (ok && (rawType == "number" || math.Trunc(number) == number))
		case "object":
			_, ok := value.(map[string]any)
			validType = validType || ok
		case "array":
			_, ok := value.([]any)
			validType = validType || ok
		}
	}
	if !validType {
		return fmt.Errorf("wrong type for %v", schema["type"])
	}
	if choices, ok := schema["enum"].([]any); ok {
		found := false
		for _, choice := range choices {
			found = found || reflect.DeepEqual(value, choice)
		}
		if !found {
			return fmt.Errorf("value absent from enum")
		}
	}
	if object, ok := value.(map[string]any); ok {
		properties, _ := schema["properties"].(map[string]any)
		for _, required := range schema["required"].([]any) {
			if _, exists := object[required.(string)]; !exists {
				return fmt.Errorf("missing field %s", required)
			}
		}
		for name, item := range object {
			property, exists := properties[name]
			if !exists {
				if schema["additionalProperties"] == false {
					return fmt.Errorf("unexpected field %s", name)
				}
				continue
			}
			if err := responseSchemaTestValidate(property.(map[string]any), item); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	if array, ok := value.([]any); ok {
		itemSchema, _ := schema["items"].(map[string]any)
		for _, item := range array {
			if err := responseSchemaTestValidate(itemSchema, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func responseSchemaTestObjectsAreClosed(t *testing.T, schema map[string]any) {
	t.Helper()
	for _, key := range []string{"minItems", "maxItems"} {
		if _, present := schema[key]; present {
			t.Errorf("schema must leave array bounds to Go: %s", key)
		}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		if schema["additionalProperties"] != false {
			t.Fatal("structured response object must reject additional properties")
		}
		var required, names []string
		for _, field := range schema["required"].([]any) {
			required = append(required, field.(string))
		}
		for name, child := range properties {
			names = append(names, name)
			responseSchemaTestObjectsAreClosed(t, child.(map[string]any))
		}
		sort.Strings(required)
		sort.Strings(names)
		if !reflect.DeepEqual(required, names) {
			t.Fatalf("every structured property must be required: properties=%v required=%v", names, required)
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		responseSchemaTestObjectsAreClosed(t, items)
	}
	if alternatives, ok := schema["anyOf"].([]any); ok {
		for _, raw := range alternatives {
			responseSchemaTestObjectsAreClosed(t, raw.(map[string]any))
		}
	}
}

func TestAssistantResponseSchemaUsesClosedRequiredObjectsAndSupportedIntents(t *testing.T) {
	schema := responseSchemaTestJSON(t, assistantResponseSchema())
	if schema["type"] != "object" {
		t.Fatal("root schema must be an object")
	}
	for _, key := range []string{"anyOf", "if", "then", "allOf"} {
		if _, exists := schema[key]; exists {
			t.Errorf("root schema has unsupported conditional/union keyword %s", key)
		}
	}
	responseSchemaTestObjectsAreClosed(t, schema)
	properties := schema["properties"].(map[string]any)
	action, exists := properties["action"].(map[string]any)
	if len(properties) != 1 || !exists {
		t.Fatalf("native envelope must contain only action: %v", properties)
	}
	alternatives, ok := action["anyOf"].([]any)
	if !ok || len(alternatives) != 5 {
		t.Fatalf("expected five nested action branches: %+v", action)
	}
	var intents []string
	for _, rawBranch := range alternatives {
		branch := rawBranch.(map[string]any)
		intentSchema := branch["properties"].(map[string]any)["intent"].(map[string]any)
		for _, value := range intentSchema["enum"].([]any) {
			intent := value.(string)
			if !validAssistantIntent(intent) || intent == "financial_analysis" || intent == "category_expenses" || intent == "all_category_expenses" {
				t.Errorf("schema permits unsupported or legacy financial intent %q", intent)
			}
			intents = append(intents, intent)
		}
	}
	wantIntents := []string{"general", "app_help", "financial_query", "monthly_summary", "list_categories", "create_expense", "cancel_expense", "patrimony_summary", "fgts_balance", "list_investments"}
	sort.Strings(intents)
	sort.Strings(wantIntents)
	if !reflect.DeepEqual(intents, wantIntents) {
		t.Fatalf("structured schema financial routing differs from the native contract: got=%v want=%v", intents, wantIntents)
	}
	create := responseSchemaTestActionSchema(t, schema, "create_expense")["properties"].(map[string]any)
	wantCreateFields := []string{"intent", "description", "category_name", "payment_source", "amount", "month", "year", "day", "type", "installments", "new_expense", "category_suggested", "provided_fields"}
	if len(create) != len(wantCreateFields) {
		t.Fatalf("expense action has non-expense fields: %+v", create)
	}
	for _, field := range wantCreateFields {
		if _, exists := create[field]; !exists {
			t.Errorf("expense action lacks field %s", field)
		}
	}
	for intent, expected := range map[string][]string{
		"financial_query": {"intent", "query"}, "general": {"intent", "reply"},
		"monthly_summary": {"intent", "month", "year"}, "list_categories": {"intent"},
	} {
		fields := responseSchemaTestActionSchema(t, schema, intent)["properties"].(map[string]any)
		var names []string
		for name := range fields {
			names = append(names, name)
		}
		sort.Strings(names)
		sort.Strings(expected)
		if !reflect.DeepEqual(names, expected) {
			t.Errorf("action %s exposes inapplicable controls: got=%v want=%v", intent, names, expected)
		}
	}
	mask := create["provided_fields"].(map[string]any)
	maskItems := mask["items"].(map[string]any)
	var patchFields []string
	for _, value := range maskItems["enum"].([]any) {
		patchFields = append(patchFields, value.(string))
	}
	wantPatchFields := []string{"description", "category_name", "payment_source", "amount", "month", "year", "day", "type", "installments"}
	sort.Strings(patchFields)
	sort.Strings(wantPatchFields)
	if mask["type"] != "array" || !reflect.DeepEqual(patchFields, wantPatchFields) {
		t.Fatalf("structured mask changed patch authority: type=%v fields=%v", mask["type"], patchFields)
	}
	format := responseSchemaTestJSON(t, assistantResponseFormat())
	wrapped, ok := format["json_schema"].(map[string]any)
	name, _ := wrapped["name"].(string)
	if format["type"] != "json_schema" || !ok || wrapped["strict"] != true || strings.TrimSpace(name) == "" || !reflect.DeepEqual(wrapped["schema"], schema) {
		t.Fatalf("invalid strict response-format wrapper: %+v", format)
	}
	for _, sample := range []struct {
		name       string
		categories []string
	}{{"unknown", nil}, {"empty", []string{}}, {"two_categories", []string{"Uber/99", "Mercado"}}} {
		encoded, err := json.Marshal(assistantResponseFormat(sample.categories))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("schema_wrapper_bytes %s=%d", sample.name, len(encoded))
	}
}

func TestAssistantResponseSchemaValidatesFinancialAndDraftFixtures(t *testing.T) {
	schema := responseSchemaTestJSON(t, assistantResponseSchema())
	for _, intent := range []string{"financial_query", "create_expense", "general", "app_help", "monthly_summary", "list_categories", "patrimony_summary", "fgts_balance", "list_investments", "cancel_expense"} {
		t.Run("action "+intent, func(t *testing.T) {
			if err := responseSchemaTestValidate(schema, responseSchemaTestJSON(t, responseSchemaTestNativeFixture(intent))); err != nil {
				t.Fatalf("native action fixture rejected: %v", err)
			}
		})
	}
	for _, test := range []struct {
		name, intent string
		fill         func(map[string]any)
	}{
		{"conversation", "general", func(v map[string]any) { v["reply"] = "Ola!" }},
		{"query", "financial_query", func(v map[string]any) { v["intent"], v["query"] = "financial_query", responseSchemaTestQuery() }},
		{"draft patch", "create_expense", func(v map[string]any) {
			v["intent"], v["description"], v["amount"], v["type"] = "create_expense", "Chocolate", float64(10), "Unica"
			v["provided_fields"] = []any{"description", "amount", "type"}
		}},
		{"all explicit nullable draft fields", "create_expense", func(v map[string]any) {
			v["intent"] = "create_expense"
			v["provided_fields"] = []any{"description", "category_name", "payment_source", "amount", "month", "year", "day", "type", "installments"}
		}},
		{"review", "financial_query", func(v map[string]any) {
			query := responseSchemaTestQuery()
			query["operation"], query["group_by"] = "review", "category"
			v["intent"], v["query"] = "financial_query", query
		}},
		{"budget", "financial_query", func(v map[string]any) {
			query := responseSchemaTestQuery()
			query["operation"] = "budget"
			v["intent"], v["query"] = "financial_query", query
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := responseSchemaTestNativeFixture(test.intent)
			test.fill(value["action"].(map[string]any))
			if err := responseSchemaTestValidate(schema, responseSchemaTestJSON(t, value)); err != nil {
				t.Fatalf("valid business fixture rejected: %v", err)
			}
		})
	}
	for _, test := range []struct {
		name, intent string
		fill         func(map[string]any)
	}{
		{"missing required", "general", func(v map[string]any) { delete(v, "reply") }},
		{"unexpected authority", "financial_query", func(v map[string]any) { v["user_id"] = float64(2) }},
		{"model confirmation", "create_expense", func(v map[string]any) { v["confirm"] = true }},
		{"unsupported intent", "general", func(v map[string]any) { v["intent"] = "delete_expense" }},
		{"freeform analysis", "general", func(v map[string]any) { v["intent"] = "financial_analysis" }},
		{"legacy category query", "general", func(v map[string]any) { v["intent"] = "category_expenses" }},
		{"legacy all-category query", "general", func(v map[string]any) { v["intent"] = "all_category_expenses" }},
		{"null field mask", "create_expense", func(v map[string]any) { v["provided_fields"] = nil }},
		{"unauthorized field mask", "create_expense", func(v map[string]any) { v["provided_fields"] = []any{"user_id"} }},
		{"numeric string", "create_expense", func(v map[string]any) { v["amount"] = "10" }},
		{"fractional day", "create_expense", func(v map[string]any) { v["day"] = 1.5 }},
		{"invalid expense type", "create_expense", func(v map[string]any) { v["type"] = "Diaria" }},
		{"boolean string", "create_expense", func(v map[string]any) { v["new_expense"] = "true" }},
		{"legacy calculation field", "general", func(v map[string]any) {
			v["calculations"] = []any{map[string]any{"operation": "sum", "paths": []any{"/total_expense"}}}
		}},
		{"null query category", "financial_query", func(v map[string]any) { v["query"].(map[string]any)["category_name"] = nil }},
		{"null query source", "financial_query", func(v map[string]any) { v["query"].(map[string]any)["payment_source"] = nil }},
		{"null query periods", "financial_query", func(v map[string]any) { v["query"].(map[string]any)["periods"] = nil }},
		{"unknown query field", "financial_query", func(v map[string]any) { v["query"].(map[string]any)["sql"] = "SELECT *" }},
		{"invalid limit", "financial_query", func(v map[string]any) { v["query"].(map[string]any)["limit"] = float64(0) }},
		{"invalid position", "financial_query", func(v map[string]any) { v["query"].(map[string]any)["position"] = float64(25) }},
		{"invalid month", "financial_query", func(v map[string]any) {
			v["query"].(map[string]any)["periods"].([]any)[0].(map[string]any)["months"] = []any{float64(13)}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := responseSchemaTestNativeFixture(test.intent)
			test.fill(value["action"].(map[string]any))
			if err := responseSchemaTestValidate(schema, responseSchemaTestJSON(t, value)); err == nil {
				t.Fatal("invalid financial/control fixture accepted")
			}
		})
	}
}

func TestAssistantResponseSchemaActionBranchesRejectInapplicableFields(t *testing.T) {
	schema := responseSchemaTestJSON(t, assistantResponseSchema())
	for _, test := range []struct {
		intent, field string
		value         any
	}{
		{"financial_query", "amount", float64(198)},
		{"financial_query", "reply", "Inventado"},
		{"create_expense", "query", responseSchemaTestQuery()},
		{"create_expense", "reply", "Consulta"},
		{"general", "query", responseSchemaTestQuery()},
		{"monthly_summary", "amount", float64(198)},
		{"list_categories", "month", float64(10)},
	} {
		t.Run(test.intent+" "+test.field, func(t *testing.T) {
			value := responseSchemaTestNativeFixture(test.intent)
			value["action"].(map[string]any)[test.field] = test.value
			if err := responseSchemaTestValidate(schema, responseSchemaTestJSON(t, value)); err == nil {
				t.Fatalf("native action accepted a field from another branch: %+v", value)
			}
		})
	}
}

func TestAssistantResponseSchemaEnvelopeRoundTripFlattensOnlyOneAction(t *testing.T) {
	for _, name := range []string{"financial_query", "create_expense", "general", "app_help", "monthly_summary", "list_categories", "patrimony_summary", "fgts_balance", "list_investments", "cancel_expense"} {
		t.Run(name, func(t *testing.T) {
			value := responseSchemaTestNativeFixture(name)
			encoded, _ := json.Marshal(value)
			text, err := normalizeAssistantSchemaResponse(string(encoded))
			if err != nil {
				t.Fatal(err)
			}
			intent, err := parseFinancialIntent(text)
			if err != nil || intent.Intent != name {
				t.Fatalf("native action did not flatten into the legacy interpreter contract: text=%s err=%v", text, err)
			}
			if _, remainsWrapped := intent.provided["action"]; remainsWrapped {
				t.Fatalf("native envelope leaked past the provider adapter: %s", text)
			}
		})
	}
	for _, raw := range []string{
		`{"action":null}`, `{"action":[]}`, `{"action":"financial_query"}`,
		`{"action":{"intent":"list_categories"},"intent":"create_expense"}`,
		`{"action":{"intent":"list_categories"},"reply":null}`,
		`{"action":{"intent":"list_categories"},"query":null}`,
	} {
		if _, err := normalizeAssistantSchemaResponse(raw); err == nil {
			t.Errorf("ambiguous or invalid native envelope was silently accepted: %s", raw)
		}
	}
	value := responseSchemaTestNativeFixture("create_expense")
	value["action"].(map[string]any)["provided_fields"] = []any{"amount"}
	encoded, _ := json.Marshal(value)
	text, err := normalizeAssistantSchemaResponse(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	intent, err := parseFinancialIntent(text)
	if err != nil {
		t.Fatal(err)
	}
	args := expenseArgsFromIntent(intent, map[string]any{"amount": float64(80.50), "description": "Compra conhecida"})
	if amount, exists := args["amount"]; !exists || amount != nil || args["description"] != "Compra conhecida" || args["confirm"] != false {
		t.Fatalf("unwrapping lost the explicit-null expense correction or authorized writing: %+v", args)
	}
}

func TestAssistantResponseSchemaDoesNotRemoveLegacyParserCompatibility(t *testing.T) {
	for _, name := range []string{"financial_analysis", "category_expenses", "all_category_expenses"} {
		t.Run(name, func(t *testing.T) {
			text := fmt.Sprintf(`{"intent":%q,"reply":"Conselho legado","calculations":[{"operation":"sum","paths":["/total_expense"]}]}`, name)
			intent, err := parseFinancialIntent(text)
			if err != nil || intent.Intent != name || intent.Reply != "Conselho legado" {
				t.Fatalf("native schema restriction changed the legacy parser: %+v err=%v", intent, err)
			}
			calculations, ok := intent.provided["calculations"].([]any)
			if !ok || len(calculations) != 1 {
				t.Fatalf("legacy calculations were discarded: %v", intent.provided)
			}
		})
	}
}

func TestAssistantResponseSchemaCategoryEnumsDistinguishUnknownEmptyAndKnownSets(t *testing.T) {
	for _, test := range []struct {
		name   string
		schema map[string]any
	}{
		{"legacy omitted metadata", assistantResponseSchema()},
		{"unknown nil metadata", assistantResponseSchema(nil)},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := responseSchemaTestJSON(t, test.schema)
			create := responseSchemaTestActionSchema(t, schema, "create_expense")["properties"].(map[string]any)
			query := responseSchemaTestActionSchema(t, schema, "financial_query")["properties"].(map[string]any)["query"].(map[string]any)
			queryCategory := query["properties"].(map[string]any)["category_name"].(map[string]any)
			for _, category := range []map[string]any{create["category_name"].(map[string]any), queryCategory} {
				if _, enforced := category["enum"]; enforced {
					t.Fatal("unknown categories were treated as an empty catalog")
				}
				if err := responseSchemaTestValidate(category, "Categoria legada"); err != nil {
					t.Fatalf("legacy unconstrained category rejected: %v", err)
				}
			}
		})
	}
	known := []string{"Uber/99", "Mercado", "Uber/99", "  Nome exato  "}
	before := append([]string(nil), known...)
	for _, test := range []struct {
		name       string
		categories []string
		allowed    []string
	}{
		{"known empty catalog", []string{}, []string{""}},
		{"known exact names", known, []string{"", "Uber/99", "Mercado", "  Nome exato  "}},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := responseSchemaTestJSON(t, assistantResponseSchema(test.categories))
			create := responseSchemaTestActionSchema(t, schema, "create_expense")["properties"].(map[string]any)
			query := responseSchemaTestActionSchema(t, schema, "financial_query")["properties"].(map[string]any)["query"].(map[string]any)
			topCategory := create["category_name"].(map[string]any)
			queryCategory := query["properties"].(map[string]any)["category_name"].(map[string]any)
			for _, category := range []map[string]any{topCategory, queryCategory} {
				for _, allowed := range test.allowed {
					if err := responseSchemaTestValidate(category, allowed); err != nil {
						t.Errorf("exact registered category %q rejected: %v", allowed, err)
					}
				}
				for _, forbidden := range []string{"Uber", "Outro nome", "Nome exato"} {
					if err := responseSchemaTestValidate(category, forbidden); err == nil {
						t.Errorf("category alias or invented name %q accepted", forbidden)
					}
				}
				enum := category["enum"].([]any)
				wantCount := len(test.allowed)
				if reflect.DeepEqual(category, topCategory) {
					wantCount++
				}
				if len(enum) != wantCount {
					t.Errorf("category enum did not deduplicate exact names: %+v", enum)
				}
			}
			if responseSchemaTestValidate(topCategory, nil) != nil || responseSchemaTestValidate(queryCategory, nil) == nil {
				t.Fatal("top optional category and required query category have the same nullability")
			}
		})
	}
	if !reflect.DeepEqual(known, before) {
		t.Fatal("dynamic schema mutated the registered category slice")
	}
}

func TestAssistantResponseSchemaTopPaymentSourceUsesCanonicalEnums(t *testing.T) {
	schema := responseSchemaTestJSON(t, assistantResponseSchema())
	source := responseSchemaTestActionSchema(t, schema, "create_expense")["properties"].(map[string]any)["payment_source"].(map[string]any)
	for _, allowed := range []any{"", "Salario", "Adiantamento", "Renda Extra", "VA", "VR", nil} {
		if err := responseSchemaTestValidate(source, allowed); err != nil {
			t.Errorf("canonical source %v rejected: %v", allowed, err)
		}
	}
	for _, forbidden := range []any{"Cartão", "Pix", float64(1)} {
		if err := responseSchemaTestValidate(source, forbidden); err == nil {
			t.Errorf("unknown payment source accepted: %v", forbidden)
		}
	}
}

func TestAssistantResponseSchemaNormalizationPreservesDraftMaskAndExplicitNulls(t *testing.T) {
	draft := map[string]any{"description": "Compra conhecida", "amount": float64(80.50), "category_name": "Mercado", "month": float64(10), "year": float64(2026), "day": float64(2), "payment_source": "Salario", "type": "Unica"}
	for _, test := range []struct {
		name string
		mask any
	}{
		{"empty mask", []any{}},
		{"malformed null mask", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := responseSchemaTestFixture("create_expense")
			value["provided_fields"], value["amount"] = test.mask, float64(999)
			encoded, _ := json.Marshal(value)
			text, err := normalizeAssistantSchemaResponse(string(encoded))
			if err != nil {
				t.Fatal(err)
			}
			intent, err := parseFinancialIntent(text)
			if err != nil {
				t.Fatal(err)
			}
			if mask, present := intent.provided["provided_fields"]; !present || !reflect.DeepEqual(mask, test.mask) {
				t.Fatalf("normalization weakened the field mask: %s", text)
			}
			args := expenseArgsFromIntent(intent, draft)
			for key, expected := range draft {
				if args[key] != expected {
					t.Fatalf("unrequested field changed after nullable schema normalization: %s=%v want=%v", key, args[key], expected)
				}
			}
		})
	}
	value := responseSchemaTestFixture("create_expense")
	value["provided_fields"] = []any{"amount"}
	encoded, _ := json.Marshal(value)
	text, err := normalizeAssistantSchemaResponse(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	intent, err := parseFinancialIntent(text)
	if err != nil {
		t.Fatal(err)
	}
	args := expenseArgsFromIntent(intent, draft)
	if amount, present := args["amount"]; !present || amount != nil || args["description"] != draft["description"] || args["confirm"] != false {
		t.Fatalf("explicit masked null was silently replaced by old money: %+v", args)
	}
	if _, exists := intent.provided["description"]; exists {
		t.Fatalf("unused nullable draft default was not removed: %s", text)
	}
}

func TestAssistantResponseSchemaNormalizationDoesNotCoerceOrWeakenQueryScope(t *testing.T) {
	query := responseSchemaTestQuery()
	value := responseSchemaTestFixture("financial_query")
	value["query"] = query
	encoded, _ := json.Marshal(value)
	text, err := normalizeAssistantSchemaResponse(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]any
	if err := json.Unmarshal([]byte(text), &normalized); err != nil {
		t.Fatal(err)
	}
	normalizedQuery := normalized["query"].(map[string]any)
	if _, exists := normalizedQuery["limit"]; exists {
		t.Fatal("nullable unused limit should become an omitted optional control")
	}
	if _, exists := normalizedQuery["position"]; exists {
		t.Fatal("nullable unused position should become an omitted optional control")
	}
	if _, err := parseFinancialReadQuery(normalizedQuery); err != nil {
		t.Fatalf("valid normalized query rejected: %v", err)
	}
	for _, field := range []string{"periods", "category_name", "payment_source", "group_by", "operation"} {
		t.Run("required null "+field, func(t *testing.T) {
			query := responseSchemaTestQuery()
			query[field] = nil
			value := responseSchemaTestFixture("financial_query")
			value["query"] = query
			encoded, _ := json.Marshal(value)
			text, err := normalizeAssistantSchemaResponse(string(encoded))
			if err != nil {
				t.Fatal(err)
			}
			var normalized map[string]any
			_ = json.Unmarshal([]byte(text), &normalized)
			query = normalized["query"].(map[string]any)
			if raw, exists := query[field]; !exists || raw != nil {
				t.Fatalf("normalization dropped a malformed required filter: %s", text)
			}
			if _, err := parseFinancialReadQuery(query); err == nil {
				t.Fatalf("malformed scope broadened instead of failing closed: %s", text)
			}
		})
	}
	for _, field := range []string{"year", "months"} {
		t.Run("required period null "+field, func(t *testing.T) {
			query := responseSchemaTestQuery()
			query["periods"].([]any)[0].(map[string]any)[field] = nil
			value := responseSchemaTestFixture("financial_query")
			value["query"] = query
			encoded, _ := json.Marshal(value)
			text, err := normalizeAssistantSchemaResponse(string(encoded))
			if err != nil {
				t.Fatal(err)
			}
			var normalized map[string]any
			_ = json.Unmarshal([]byte(text), &normalized)
			query = normalized["query"].(map[string]any)
			period := query["periods"].([]any)[0].(map[string]any)
			if raw, exists := period[field]; !exists || raw != nil {
				t.Fatalf("normalization removed an invalid required period: %s", text)
			}
			if _, err := parseFinancialReadQuery(query); err == nil {
				t.Fatalf("null period changed the requested scope: %s", text)
			}
		})
	}
	for _, intentName := range []string{"financial_query", "general"} {
		value := responseSchemaTestFixture(intentName)
		encoded, _ := json.Marshal(value)
		text, err := normalizeAssistantSchemaResponse(string(encoded))
		if err != nil {
			t.Fatal(err)
		}
		var normalized map[string]any
		_ = json.Unmarshal([]byte(text), &normalized)
		_, present := normalized["query"]
		if present != (intentName == "financial_query") {
			t.Fatalf("null query altered intent semantics: %s", text)
		}
	}
	legacy := `{"intent":"create_expense","amount":"10,50","description":"Legado"}`
	text, err = normalizeAssistantSchemaResponse(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var legacyValue map[string]any
	_ = json.Unmarshal([]byte(text), &legacyValue)
	if legacyValue["amount"] != "10,50" || legacyValue["description"] != "Legado" {
		t.Fatalf("normalization coerced a legacy value: %s", text)
	}
	if _, exists := legacyValue["provided_fields"]; exists {
		t.Fatal("normalization invented a field mask for a legacy response")
	}
	if _, err := normalizeAssistantSchemaResponse(`{"intent":`); err == nil {
		t.Fatal("normalization repaired malformed JSON")
	}
}

func TestAssistantResponseSchemaQueryAdapterDropsStrayExpenseFieldsAndExtraneousPosition(t *testing.T) {
	raw := `{"intent":"financial_query","query":{"periods":[{"year":2026,"months":[10,11]}],"category_name":"Uber","payment_source":"","group_by":"month","operation":"sum","limit":2,"position":1},"amount":198,"reply":"inventado","payment_source":"Cartão","category_name":"Outra","provided_fields":["amount"]}`
	text, err := normalizeAssistantSchemaResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]any
	if err := json.Unmarshal([]byte(text), &normalized); err != nil {
		t.Fatal(err)
	}
	query, ok := normalized["query"].(map[string]any)
	if len(normalized) != 2 || normalized["intent"] != "financial_query" || !ok {
		t.Fatalf("query retained a fabricated answer or expense fields: %s", text)
	}
	if query["category_name"] != "Uber" || query["operation"] != "sum" || query["limit"] != float64(2) || query["payment_source"] != "" {
		t.Fatalf("adapter guessed an alias or changed executable scope: %s", text)
	}
	if _, exists := query["position"]; exists {
		t.Fatalf("sum inherited an unrelated ranking ordinal: %s", text)
	}
	wantPeriods := []any{map[string]any{"year": float64(2026), "months": []any{float64(10), float64(11)}}}
	if !reflect.DeepEqual(query["periods"], wantPeriods) {
		t.Fatalf("adapter lost one requested month: %s", text)
	}
	for _, operation := range []string{"max", "min", "list", "compare", "sum", "average", "review", "budget"} {
		t.Run(operation, func(t *testing.T) {
			query := responseSchemaTestQuery()
			query["operation"], query["limit"], query["position"] = operation, float64(2), float64(1)
			encoded, _ := json.Marshal(map[string]any{"intent": "financial_query", "query": query})
			text, err := normalizeAssistantSchemaResponse(string(encoded))
			if err != nil {
				t.Fatal(err)
			}
			var normalized map[string]any
			_ = json.Unmarshal([]byte(text), &normalized)
			query = normalized["query"].(map[string]any)
			position, present := query["position"]
			if present != (operation == "max" || operation == "min") || (present && position != float64(1)) || query["limit"] != float64(2) {
				t.Fatalf("ordinal/quantity controls changed semantics for %s: %s", operation, text)
			}
		})
	}
}

func TestAssistantResponseSchemaCategoryMetadataIsInternalToProviderAdapter(t *testing.T) {
	request := GeminiRequest{CategoryNames: []string{"Uber/99", "Mercado"}, DebugStage: "assistant_intent"}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"CategoryNames", "category_names", "Uber/99", "Mercado"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("internal category metadata leaked through request JSON: %s", encoded)
		}
	}
}

func TestCatalogStructuredOutputCapabilityDoesNotFilterFreeModelsOrLeakFlags(t *testing.T) {
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		entries := []any{}
		for id, parameters := range map[string][]string{
			"test/structured:free": {"structured_outputs"}, "test/json-object:free": {"response_format"},
			"test/prompt:free": {}, "test/lookalike:free": {"json_schema"},
		} {
			entry := testCatalogEntry(id)
			entry["supported_parameters"] = parameters
			entries = append(entries, entry)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": entries})
	})
	models, err := listFreeAssistantModels(context.Background())
	if err != nil || len(models) != 4 {
		t.Fatalf("structured-output metadata filtered free models: %+v err=%v", models, err)
	}
	for _, model := range models {
		if model.SupportsStructuredOutputs != (model.ID == "test/structured:free") {
			t.Errorf("incorrect structured-output capability: %+v", model)
		}
		if public := responseSchemaTestJSON(t, model); len(public) != 4 {
			t.Fatalf("internal structured-output flag leaked into public JSON: %+v", public)
		}
	}
}

func TestOpenRouterStructuredOutputSchemaIsRestrictedToInterpretation(t *testing.T) {
	model := AssistantModel{ID: "test/schema:free", ContextLength: 32768, SupportsStructuredOutputs: true, SupportsResponseFormat: true, SupportsMaxTokens: true}
	payload, err := routerPayload(model, GeminiRequest{DebugStage: "assistant_chat", Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "Ola"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if serialized := responseSchemaTestJSON(t, payload); serialized["response_format"] != nil {
		t.Fatalf("non-interpreter stage received an intent schema: %+v", serialized)
	}
}

func TestOpenRouterStructuredOutputSchemaCountsTowardInputBudget(t *testing.T) {
	model := AssistantModel{ID: "test/small-context:free", ContextLength: 4096, SupportsResponseFormat: true, SupportsMaxTokens: true}
	request := GeminiRequest{
		DebugStage:        "assistant_intent",
		SystemInstruction: &GeminiContent{Parts: []GeminiPart{{Text: strings.Repeat("s", 500)}}},
		Contents:          []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "Ola"}}}},
	}
	if _, err := routerPayload(model, request); err != nil {
		t.Fatalf("small native-JSON request should fit without the strict schema: %v", err)
	}
	model.SupportsStructuredOutputs = true
	_, err := routerPayload(model, request)
	if code, _, ok := openRouterErrorInfo(err); !ok || code != "assistant_model_context_exceeded" {
		t.Fatalf("strict schema outside messages was omitted from the input budget: %v", err)
	}
}

func TestOpenRouterStructuredOutputHTTPPreservesLegacyAndOneFreeCall(t *testing.T) {
	for _, mode := range []string{"structured", "json_object", "prompt"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			payloads := make(chan map[string]any, 1)
			categoryNames := []string{"Uber/99", "Mercado"}
			fixture := responseSchemaTestNativeFixture("financial_query")
			content, _ := json.Marshal(fixture)
			if mode != "structured" {
				content = []byte(`{"intent":"monthly_summary","month":10,"year":2026}`)
			}
			setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					entry := testCatalogEntry("test/schema:free")
					parameters := []string{"max_tokens"}
					if mode == "structured" {
						parameters = append(parameters, "structured_outputs")
					} else if mode == "json_object" {
						parameters = append(parameters, "response_format")
					}
					entry["supported_parameters"] = parameters
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{entry}})
					return
				}
				calls.Add(1)
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				select {
				case payloads <- payload:
				default:
					t.Error("structured response caused a second completion")
				}
				writeRouterReply(w, string(content))
			})
			ctx := withAssistantMessageBudget(context.Background())
			client, err := newAssistantGeneratorForModel(ctx, "test/schema:free")
			if err != nil {
				t.Fatal(err)
			}
			text, err := client.generateText(GeminiRequest{DebugStage: "assistant_intent", CategoryNames: categoryNames, Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "Compare outubro e novembro com Uber"}}}}})
			if err != nil || calls.Load() != 1 {
				t.Fatalf("structured response did not preserve one inference: text=%q calls=%d err=%v", text, calls.Load(), err)
			}
			payload := <-payloads
			format, _ := payload["response_format"].(map[string]any)
			if mode == "structured" {
				if !reflect.DeepEqual(format, responseSchemaTestJSON(t, assistantResponseFormat(categoryNames))) {
					t.Fatalf("strict schema not sent with structured_outputs-only capability: %+v", format)
				}
				intent, err := parseFinancialIntent(text)
				if err != nil {
					t.Fatal(err)
				}
				if _, exists := intent.provided["action"]; exists || len(intent.provided) != 2 {
					t.Fatalf("native response did not flatten to only intent/query: %s", text)
				}
				query, ok := intent.provided["query"].(map[string]any)
				if !ok {
					t.Fatalf("structured financial scope was lost: %s", text)
				}
				if _, exists := query["limit"]; exists {
					t.Fatalf("provider did not normalize nullable query controls: %s", text)
				}
				if _, err := parseFinancialReadQuery(query); err != nil {
					t.Fatalf("structured response cannot execute after normalization: %v", err)
				}
			} else if mode == "json_object" {
				if len(format) != 1 || format["type"] != "json_object" {
					t.Fatalf("legacy native JSON mode changed: %+v", format)
				}
			} else if _, present := payload["response_format"]; present {
				t.Fatalf("unsupported structured response parameter was sent: %+v", payload)
			}
			if mode != "structured" {
				intent, err := parseFinancialIntent(text)
				if err != nil || intent.Intent != "monthly_summary" || intent.Month != 10 || intent.Year != 2026 {
					t.Fatalf("legacy response contract changed: %s err=%v", text, err)
				}
			}
			provider, _ := payload["provider"].(map[string]any)
			prices, _ := provider["max_price"].(map[string]any)
			if payload["max_tokens"] != float64(2048) || provider["data_collection"] != "deny" || provider["require_parameters"] != true || prices["prompt"] != float64(0) || prices["completion"] != float64(0) || prices["request"] != float64(0) {
				t.Fatalf("structured output weakened output, privacy or price controls: %+v", payload)
			}
			if _, err := client.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err == nil || calls.Load() != 1 {
				t.Fatalf("structured output enabled another call in the same message: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}
