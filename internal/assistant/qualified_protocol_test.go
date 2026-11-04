package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLegacyDotsUsesJSONWithServerValidation(t *testing.T) {
	model := AssistantModel{ID: legacyJSONAssistantModelID, ContextLength: 32768, SupportsStructuredOutputs: true, SupportsResponseFormat: true}
	payload, err := routerPayload(model, GeminiRequest{DebugStage: "assistant_intent"})
	if err != nil || payload.ResponseFormat["type"] != "json_object" {
		t.Fatalf("unqualified native format enabled: %v %v", payload.ResponseFormat, err)
	}
}

func TestNativeAlternativeUsesSchemaWithoutInheritingDotsException(t *testing.T) {
	model := AssistantModel{ID: "xingchen-agi/xing4.0-29b-a4b:free", ContextLength: 32768, SupportsStructuredOutputs: true, SupportsResponseFormat: true, SupportsMaxTokens: true}
	payload, err := routerPayload(model, GeminiRequest{DebugStage: "assistant_intent", CategoryNames: []string{"Transporte", "Moradia"}})
	if err != nil || payload.ResponseFormat["type"] != "json_schema" || payload.MaxTokens != 2048 {
		t.Fatalf("native alternative lost its bounded protocol: %v %v", payload, err)
	}
	format := responseSchemaTestJSON(t, payload.ResponseFormat)
	schema := format["json_schema"].(map[string]any)["schema"].(map[string]any)
	query := responseSchemaTestQuery()
	query["category_name"] = "Dividas"
	if responseSchemaTestValidate(schema, map[string]any{"action": map[string]any{"intent": "financial_query", "query": query}}) == nil {
		t.Fatal("native schema authorized an invented category")
	}
}

func TestCapturedQualifiedPlansRemainExecutable(t *testing.T) {
	data, err := os.ReadFile("testdata/qualified_responses_20261101.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Records []struct {
			ID       string `json:"id"`
			Response string `json:"response"`
		} `json:"records"`
	}
	if json.Unmarshal(data, &fixture) != nil || len(fixture.Records) != 6 {
		t.Fatal("invalid qualification fixture")
	}
	for _, record := range fixture.Records {
		t.Run(record.ID, func(t *testing.T) {
			text, err := normalizeAssistantSchemaResponse(record.Response)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := parseFinancialIntent(text)
			if err != nil {
				t.Fatal(err)
			}
			switch record.ID {
			case "budget_uber_year", "global_review_after_uber":
				query, err := parseFinancialReadQuery(intent.provided["query"])
				want := "budget"
				if record.ID == "global_review_after_uber" {
					want = "review"
				}
				if err != nil || query.Operation != want || len(query.Periods) != 1 || query.Periods[0].Year != 2026 {
					t.Fatalf("query lost semantics: %+v %v", query, err)
				}
				if want == "budget" {
					// Continue the actual captured plan through the renderer with a
					// synthetic authenticated snapshot. A valid intent alone is not
					// sufficient if the user never receives the requested proposal.
					reply, result, err := renderFinancialGuidance(query, 7, []expenses.Expense{
						{UserID: 7, CategoryID: 1, Amount: 100, Month: 8, Year: 2026, PaymentSource: "Salario"},
						{UserID: 7, CategoryID: 1, Amount: 200, Month: 9, Year: 2026, PaymentSource: "Salario"},
					}, nil, map[uint]string{1: "Uber/99"}, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
					if err != nil || result["suggested_monthly_target"] != float64(150) || !strings.Contains(reply, "sugiro **R$ 150,00**") {
						t.Fatalf("qualified intent did not deliver a reference: %v %+v %s", err, result, reply)
					}
				}
			case "residual_balance":
				if intent.Intent != "monthly_summary" || intent.Month != 10 || intent.Year != 2026 {
					t.Fatal("balance became expenses")
				}
			case "incomplete_chocolate", "recover_purchase":
				if intent.Intent != "create_expense" {
					t.Fatal("purchase became conversation")
				}
				args := expenseArgsFromIntent(intent)
				draft, _ := prepareExpenseDraft(args)
				want := "needs_information"
				if record.ID == "recover_purchase" {
					want = "needs_confirmation"
				}
				if draft["status"] != want || draft["amount"] != float64(15) || draft["category_name"] != "Mercado" || args["confirm"] != false {
					t.Fatalf("draft lost fields or bypassed confirmation: %v", draft)
				}
			case "cancel_draft":
				if intent.Intent != "cancel_expense" {
					t.Fatal("cancellation became another action")
				}
			}
		})
	}
}

func TestMonthlyCategoryReviewCompilesToHistoricalBudgetWithoutWidening(t *testing.T) {
	raw := map[string]any{"periods": []any{map[string]any{"year": 2026, "months": []int{5, 6}}}, "category_name": "Uber/99", "payment_source": "Salario", "group_by": "month", "operation": "review"}
	query, err := parseFinancialReadQuery(raw)
	if err != nil || query.Operation != "budget" || query.CategoryName != "Uber/99" || query.PaymentSource != "Salario" || len(query.Periods) != 1 || len(query.Periods[0].Months) != 2 {
		t.Fatalf("monthly guidance lost scope: %+v %v", query, err)
	}
	raw["category_name"] = ""
	if _, err := parseFinancialReadQuery(raw); err == nil {
		t.Fatal("monthly guidance without category was silently widened")
	}
}
