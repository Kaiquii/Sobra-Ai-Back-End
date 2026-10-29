package assistant

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func completeExpenseArgs() map[string]any {
	return map[string]any{
		"description": "Cigarro", "category_name": "Pessoal", "amount": 600.0,
		"payment_source": "Salario", "month": 10, "year": 2026, "day": 15,
		"type": "Parcelada", "installments": 3, "confirm": false,
	}
}

func TestExpenseDraftRequiresInformationBeforeAnyDatabaseAccess(t *testing.T) {
	tests := []struct {
		name, key, field string
		value            any
	}{
		{"missing description", "description", "description", nil},
		{"blank description", "description", "description", " \t "},
		{"long description", "description", "description", strings.Repeat("x", 256)},
		{"wrong description type", "description", "description", 123},
		{"missing category", "category_name", "category_name", nil},
		{"blank category", "category_name", "category_name", " "},
		{"missing source", "payment_source", "payment_source", nil},
		{"invalid source", "payment_source", "payment_source", "Cartao"},
		{"zero total", "amount", "amount", 0.0},
		{"negative total", "amount", "amount", -10.0},
		{"NaN total", "amount", "amount", math.NaN()},
		{"infinite total", "amount", "amount", math.Inf(1)},
		{"subcent total", "amount", "amount", 1.001},
		{"excessive total", "amount", "amount", 1e10 + 1},
		{"fractional month", "month", "date", 10.5},
		{"invalid day", "day", "date", 32},
		{"missing day", "day", "date", nil},
		{"missing type", "type", "type", nil},
		{"invalid type", "type", "type", "Outro"},
		{"missing installments", "installments", "installments", nil},
		{"fractional installments", "installments", "installments", 3.5},
		{"too many installments", "installments", "installments", 121},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, confirm := range []bool{false, true} {
				args := completeExpenseArgs()
				args[test.key] = test.value
				args["confirm"] = confirm
				// A nil DB proves invalid drafts cannot query or write even when
				// a caller bypasses the LLM and submits confirm=true directly.
				result, err := createExpenseFromToolDB(nil, 1, args)
				if err != nil || result["status"] != "needs_information" {
					t.Fatalf("result=%v error=%v", result, err)
				}
				if !containsField(result["missing_fields"].([]string), test.field) {
					t.Fatalf("missing %s in %v", test.field, result)
				}
				if _, err := json.Marshal(result); err != nil {
					t.Fatal(err)
				}
				if reply := localToolReply("create_expense", result); !strings.Contains(reply, "Nenhuma despesa") || strings.Contains(reply, "Posso confirmar") {
					t.Fatalf("unsafe reply: %s", reply)
				}
			}
		})
	}
}

func containsField(fields []string, wanted string) bool {
	for _, field := range fields {
		if field == wanted {
			return true
		}
	}
	return false
}

func TestInstallmentAmountsPreserveTotal(t *testing.T) {
	for _, test := range []struct {
		total float64
		count int
		want  []float64
	}{
		{600, 3, []float64{200, 200, 200}},
		{100, 3, []float64{33.34, 33.33, 33.33}},
		{0.04, 3, []float64{0.02, 0.01, 0.01}},
	} {
		amounts, err := installmentAmounts(test.total, "Parcelada", test.count)
		if err != nil || !reflect.DeepEqual(amounts, test.want) {
			t.Fatalf("total %v: got %v error=%v, want %v", test.total, amounts, err, test.want)
		}
		var cents int64
		for _, amount := range amounts {
			cents += int64(math.Round(amount * 100))
		}
		if cents != int64(math.Round(test.total*100)) {
			t.Fatalf("sum changed: %d", cents)
		}
	}
	if _, err := installmentAmounts(0.02, "Parcelada", 3); err == nil {
		t.Fatal("zero-value installments accepted")
	}
}

func TestFixedDraftKeepsTheFullMonthlyValueForEveryScheduledEntry(t *testing.T) {
	args := completeExpenseArgs()
	args["type"], args["amount"] = "Fixa", 0.01
	draft, amounts := prepareExpenseDraft(args)
	if draft["status"] != "needs_confirmation" || draft["installments"] != 0 || len(amounts) != 120 {
		t.Fatalf("fixed draft lost the canonical schedule: %+v", draft)
	}
	for _, amount := range amounts {
		if amount != 0.01 {
			t.Fatalf("fixed monthly amount was split as a purchase total: %v", amounts)
		}
	}
}

func TestDraftContinuationPreservesFieldsAndCannotAuthorizeWrite(t *testing.T) {
	draft, _ := prepareExpenseDraft(completeExpenseArgs())
	intent, err := parseFinancialIntent(`{"intent":"create_expense","category_name":"Saude","confirm":true}`)
	if err != nil {
		t.Fatal(err)
	}
	args := expenseArgsFromIntent(intent, draft)
	if args["amount"] != 600.0 || args["description"] != "Cigarro" || args["category_name"] != "Saude" || args["confirm"] != false {
		t.Fatalf("lost or unsafe draft: %v", args)
	}
	intent, _ = parseFinancialIntent(`{"intent":"create_expense","new_expense":true,"description":"Outra"}`)
	args = expenseArgsFromIntent(intent, draft)
	if _, ok := args["amount"]; ok {
		t.Fatal("new expense inherited the previous amount")
	}
}

func TestMissingOrNullIntentFieldsAreNotInvented(t *testing.T) {
	for _, raw := range []string{
		`{"intent":"create_expense"}`,
		`{"intent":"create_expense","description":null,"category_name":null,"amount":null,"day":null}`,
	} {
		intent, err := parseFinancialIntent(raw)
		if err != nil {
			t.Fatal(err)
		}
		draft, _ := prepareExpenseDraft(expenseArgsFromIntent(intent))
		if draft["description"] != "" || draft["amount"] != 0.0 || draft["day"] != 0 || draft["type"] != "" {
			t.Fatalf("invented fields: %v", draft)
		}
		if len(draft["missing_fields"].([]string)) < 6 {
			t.Fatalf("missing fields not requested: %v", draft)
		}
	}
}

func TestConfirmationRequiresStandaloneAffirmation(t *testing.T) {
	for _, message := range []string{"nao", "não pode cadastrar", "não confirmo", "sim, mas troque o valor", "pode mudar a categoria?", "isso esta errado", "ok cancelar"} {
		if isConfirmationMessage(message) {
			t.Errorf("unsafe confirmation: %s", message)
		}
	}
	for _, message := range []string{"sim", " SIM! ", "pode cadastrar", "confirmo", "ok."} {
		if !isConfirmationMessage(message) {
			t.Errorf("valid confirmation rejected: %s", message)
		}
	}
}

func TestConfirmationShowsCategoryAndInstallments(t *testing.T) {
	draft, _ := prepareExpenseDraft(completeExpenseArgs())
	reply := localToolReply("create_expense", draft)
	for _, expected := range []string{"Pessoal", "Parcelada", "600,00", "3 parcelas", "200,00", "2026-10-15", "Salario", "Posso confirmar"} {
		if !strings.Contains(reply, expected) {
			t.Fatalf("missing %q: %s", expected, reply)
		}
	}
}
