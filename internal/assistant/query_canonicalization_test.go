package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
)

func canonicalQueryFixture() map[string]any {
	return map[string]any{
		"periods":       []any{map[string]any{"year": 2026, "months": []int{10, 11}}},
		"category_name": "Uber/99", "payment_source": "Salario", "group_by": "month", "operation": "compare",
		"limit": nil, "position": nil,
	}
}

func canonicalQueryGenerate(t *testing.T, format string, object map[string]any) (string, error) {
	t.Helper()
	var calls atomic.Int32
	content, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if format == "json_schema" {
		content = append(append([]byte(`{"action":`), content...), '}')
	} else if format == "prompt" {
		content = []byte("```json\n" + string(content) + "\n```")
	}
	setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			entry := testCatalogEntry("test/canonical:free")
			parameters := []string{"max_tokens"}
			if format == "json_schema" {
				parameters = append(parameters, "structured_outputs")
			} else if format == "json_object" {
				parameters = append(parameters, "response_format")
			}
			entry["supported_parameters"] = parameters
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{entry}})
			return
		}
		calls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		provider, _ := payload["provider"].(map[string]any)
		prices, _ := provider["max_price"].(map[string]any)
		if payload["max_tokens"] != float64(2048) || provider["data_collection"] != "deny" || provider["require_parameters"] != true || prices["prompt"] != float64(0) || prices["completion"] != float64(0) || prices["request"] != float64(0) {
			t.Error("query normalization changed output, privacy or price controls")
		}
		writeRouterReply(w, string(content))
	})
	generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "test/canonical:free")
	if err != nil {
		t.Fatal(err)
	}
	text, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent", CategoryNames: []string{"Uber/99", "Mercado"}})
	if calls.Load() != 1 {
		t.Fatalf("query normalization spent %d completion attempts", calls.Load())
	}
	return text, err
}

func TestOpenRouterFinancialQueryFormatsCanonicalizeEquivalentPlans(t *testing.T) {
	for _, test := range []struct {
		name, operation string
		limit, position any
		wantLimit       int
		wantPosition    int
	}{
		{"unused null controls", "compare", nil, nil, 24, 0},
		{"irrelevant ordinal", "compare", 2, 1, 2, 0},
		{"null ranking controls", "max", nil, nil, 1, 0},
		{"explicit ordinal", "max", nil, 2, 1, 2},
		{"sum with irrelevant ordinal", "sum", nil, 1, 24, 0},
		{"budget with unused null controls", "budget", nil, nil, 24, 0},
	} {
		for _, format := range []string{"json_schema", "json_object", "prompt"} {
			t.Run(test.name+"/"+format, func(t *testing.T) {
				query := canonicalQueryFixture()
				query["operation"], query["limit"], query["position"] = test.operation, test.limit, test.position
				text, err := canonicalQueryGenerate(t, format, map[string]any{"intent": "financial_query", "query": query})
				if err != nil {
					t.Fatal(err)
				}
				intent, err := parseFinancialIntent(cleanJSONText(text))
				if err != nil {
					t.Fatal(err)
				}
				plan, err := parseFinancialReadQuery(intent.provided["query"])
				if err != nil {
					t.Fatalf("equivalent response produced invalid_query in %s: %v", format, err)
				}
				want := financialReadQuery{Periods: []financialReadPeriod{{Year: 2026, Months: []int{10, 11}}}, CategoryName: "Uber/99", PaymentSource: "Salario", GroupBy: "month", Operation: test.operation, Limit: test.wantLimit, Position: test.wantPosition}
				if !reflect.DeepEqual(plan, want) {
					t.Fatalf("response format changed the query scope: got=%+v want=%+v", plan, want)
				}
				rows, err := aggregateFinancialReadQuery(plan, 7, 11, []expenses.Expense{
					{ID: 1, UserID: 7, CategoryID: 11, Amount: 80.50, Month: 10, Year: 2026, PaymentSource: "Salario"},
					{ID: 2, UserID: 7, CategoryID: 11, Amount: 230, Month: 11, Year: 2026, PaymentSource: "Salario"},
					{ID: 3, UserID: 8, CategoryID: 11, Amount: 99999, Month: 10, Year: 2026, PaymentSource: "Salario"},
					{ID: 4, UserID: 7, CategoryID: 12, Amount: 99999, Month: 10, Year: 2026, PaymentSource: "Salario"},
					{ID: 5, UserID: 7, CategoryID: 11, Amount: 99999, Month: 9, Year: 2026, PaymentSource: "Salario"},
					{ID: 6, UserID: 7, CategoryID: 11, Amount: 99999, Month: 10, Year: 2026, PaymentSource: "VA"},
				}, nil, map[uint]string{11: "Uber/99", 12: "Mercado"})
				if err != nil || len(rows) != 2 || rows[0].Amount != 80.50 || rows[1].Amount != 230 {
					t.Fatalf("canonicalization widened user/category/month/source scope: rows=%+v err=%v", rows, err)
				}
			})
		}
	}
}

func TestOpenRouterFinancialQueryFormatsPreserveInvalidScopeForClarification(t *testing.T) {
	for _, format := range []string{"json_schema", "json_object", "prompt"} {
		for _, test := range []struct {
			name   string
			change func(map[string]any)
		}{
			{"null periods", func(query map[string]any) { query["periods"] = nil }},
			{"null category", func(query map[string]any) { query["category_name"] = nil }},
			{"null payment source", func(query map[string]any) { query["payment_source"] = nil }},
			{"null group", func(query map[string]any) { query["group_by"] = nil }},
			{"null operation", func(query map[string]any) { query["operation"] = nil }},
			{"null year", func(query map[string]any) { query["periods"].([]any)[0].(map[string]any)["year"] = nil }},
			{"null months", func(query map[string]any) { query["periods"].([]any)[0].(map[string]any)["months"] = nil }},
			{"missing months", func(query map[string]any) { delete(query["periods"].([]any)[0].(map[string]any), "months") }},
			{"untrusted owner", func(query map[string]any) { query["user_id"] = 99 }},
			{"untrusted value", func(query map[string]any) { query["amount"] = 99999 }},
		} {
			t.Run(format+"/"+test.name, func(t *testing.T) {
				query := canonicalQueryFixture()
				test.change(query)
				want := responseSchemaTestJSON(t, query)
				delete(want, "limit")
				delete(want, "position")
				text, err := canonicalQueryGenerate(t, format, map[string]any{"intent": "financial_query", "query": query})
				if err != nil {
					t.Fatal(err)
				}
				intent, err := parseFinancialIntent(cleanJSONText(text))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(intent.provided["query"], want) {
					t.Fatalf("normalization removed or rewrote malformed scope: got=%v want=%v", intent.provided["query"], want)
				}
				// Parsing precedes database access. A malformed required scope must
				// ask for clarification even with no database available to query.
				handled, _, toolCall, raw := executeFinancialQueryDB(context.Background(), nil, 7, intent.provided["query"], assistantNow())
				result, ok := raw.(map[string]any)
				if !handled || toolCall != "financial_analysis" || !ok || result["status"] != "needs_clarification" || result["reason"] != "invalid_query" {
					t.Fatalf("malformed scope was broadened or reached the database: %+v", raw)
				}
			})
		}
		t.Run(format+"/null query", func(t *testing.T) {
			text, err := canonicalQueryGenerate(t, format, map[string]any{"intent": "financial_query", "query": nil})
			if err != nil {
				t.Fatal(err)
			}
			intent, err := parseFinancialIntent(cleanJSONText(text))
			if err != nil {
				t.Fatal(err)
			}
			if query, exists := intent.provided["query"]; !exists || query != nil {
				t.Fatal("null query became a different or missing scope")
			}
			if _, err := parseFinancialReadQuery(intent.provided["query"]); err == nil {
				t.Fatal("null query became executable")
			}
		})
	}
}

func TestOpenRouterQueryCanonicalizationLeavesExpenseMasksUntouched(t *testing.T) {
	draft := map[string]any{
		"description": "Compra conhecida", "amount": float64(80.50), "category_name": "Mercado",
		"payment_source": "Salario", "month": float64(10), "year": float64(2026), "day": float64(2), "type": "Unica",
	}
	for _, format := range []string{"json_schema", "json_object", "prompt"} {
		for _, test := range []struct {
			name   string
			mask   any
			amount any
		}{
			{"empty mask", []any{}, float64(999)},
			{"null mask", nil, float64(999)},
			{"masked explicit null", []any{"amount"}, nil},
		} {
			t.Run(format+"/"+test.name, func(t *testing.T) {
				text, err := canonicalQueryGenerate(t, format, map[string]any{
					"intent": "create_expense", "query": nil, "provided_fields": test.mask, "amount": test.amount,
				})
				if err != nil {
					t.Fatal(err)
				}
				intent, err := parseFinancialIntent(cleanJSONText(text))
				if err != nil {
					t.Fatal(err)
				}
				if mask, exists := intent.provided["provided_fields"]; !exists || !reflect.DeepEqual(mask, test.mask) {
					t.Fatalf("query normalization changed expense patch authority: %s", text)
				}
				args := expenseArgsFromIntent(intent, draft)
				if args["description"] != draft["description"] || args["category_name"] != draft["category_name"] || args["confirm"] != false {
					t.Fatalf("untouched draft fields or confirmation changed: %+v", args)
				}
				if test.amount == nil {
					if amount, exists := args["amount"]; !exists || amount != nil {
						t.Fatal("masked explicit null was replaced by previous money")
					}
				} else if args["amount"] != draft["amount"] {
					t.Fatal("empty or malformed mask allowed unrequested money to replace the draft")
				}
			})
		}
	}
}
