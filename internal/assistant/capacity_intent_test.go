package assistant

import (
	"reflect"
	"testing"
	"time"
)

func TestCurrentSavingsCapacityUsesOnlyTheCapturedCurrentPeriod(t *testing.T) {
	brasilia := time.FixedZone("Brasilia", -3*60*60)
	for _, now := range []time.Time{
		time.Date(2036, 1, 1, 1, 15, 0, 0, time.UTC).In(brasilia), // Still December in Brasilia.
		time.Date(2036, 1, 1, 0, 15, 0, 0, brasilia),
		time.Date(2042, 7, 19, 23, 59, 0, 0, brasilia),
	} {
		t.Run(now.Format(time.RFC3339), func(t *testing.T) {
			intent, err := parseInterpretedFinancialIntent(`{"intent":"current_savings_capacity"}`, now)
			if err != nil {
				t.Fatal(err)
			}
			assertCurrentSavingsCapacityIntent(t, intent, now)
		})
	}
}

func TestCurrentSavingsCapacityDiscardsAllModelSuppliedArguments(t *testing.T) {
	now := time.Date(2034, 11, 3, 9, 0, 0, 0, time.FixedZone("Brasilia", -10800))
	for _, text := range []string{
		`{"intent":"current_savings_capacity","query":{"periods":[{"year":2035,"months":[]}],"category_name":"Dividas","payment_source":"VA","group_by":"category","operation":"sum","limit":1,"position":2},"reply":"Pode guardar R$ 999,00.","amount":999,"month":1,"year":2035,"category_name":"Dividas","payment_source":"VA","description":"Compra inventada","day":31,"type":"Parcelada","installments":12,"provided_fields":["amount","year"],"new_expense":true,"confirm":true,"calculations":[{"operation":"sum","paths":["/invented/amount"]}]}`,
		`{"intent":"current_savings_capacity","query":null,"month":12,"year":2099,"amount":8000}`,
		`{"intent":"current_savings_capacity","query":"not a query","reply":"PRIVATE_UNVERIFIED_REPLY_57293","user_id":999,"model_id":"unrelated/model"}`,
	} {
		intent, err := parseInterpretedFinancialIntent(text, now)
		if err != nil {
			t.Fatal(err)
		}
		assertCurrentSavingsCapacityIntent(t, intent, now)
	}
}

func TestCurrentSavingsCapacityRejectsInvalidClockWithoutChangingLegacyParsing(t *testing.T) {
	for _, now := range []time.Time{
		{},
		time.Date(1999, 12, 31, 23, 0, 0, 0, time.UTC),
		time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		if _, err := parseInterpretedFinancialIntent(`{"intent":"current_savings_capacity"}`, now); err == nil {
			t.Fatalf("invalid clock became a capacity period: %v", now)
		}
		text := `{"intent":"general","reply":"Conversa sem consulta financeira."}`
		want, err := parseFinancialIntent(text)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := parseInterpretedFinancialIntent(text, now); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("capacity clock validation changed a legacy conversation")
		}
	}
}

func assertCurrentSavingsCapacityIntent(t *testing.T, intent financialIntent, now time.Time) {
	t.Helper()
	if intent.Intent != "financial_query" || len(intent.provided) != 2 || intent.provided["intent"] != "financial_query" {
		t.Fatalf("capacity alias retained extra authority: %+v", intent)
	}
	query, err := parseFinancialReadQuery(intent.provided["query"])
	if err != nil {
		t.Fatalf("normalized capacity is not executable: %v", err)
	}
	wantPeriods := []financialReadPeriod{{Year: now.Year(), Months: []int{int(now.Month())}}}
	if query.Operation != "savings" || query.GroupBy != "month" || query.CategoryName != "" || query.PaymentSource != "" ||
		!reflect.DeepEqual(query.Periods, wantPeriods) || query.Position != 0 {
		t.Fatalf("model supplied the current capacity's scope: %+v", query)
	}
	withoutProvided := intent
	withoutProvided.provided = nil
	if !reflect.DeepEqual(withoutProvided, financialIntent{Intent: "financial_query"}) {
		t.Fatalf("alias retained model supplied scalar fields or financial facts: %+v", withoutProvided)
	}
}

func TestInterpretedFinancialParserPreservesEveryNonCapacityIntent(t *testing.T) {
	now := time.Date(2034, 11, 3, 9, 0, 0, 0, time.FixedZone("Brasilia", -10800))
	for _, test := range []struct{ name, text string }{
		{"legacy savings explicit future year", `{"intent":"financial_query","query":{"periods":[{"year":2037,"months":[]}],"category_name":"","payment_source":"","group_by":"month","operation":"savings"}}`},
		{"period savings explicit months", `{"intent":"financial_query","query":{"periods":[{"year":2033,"months":[9,10]},{"year":2034,"months":[1]}],"category_name":"","payment_source":"","group_by":"month","operation":"period_savings"}}`},
		{"other query filters", `{"intent":"financial_query","query":{"periods":[{"year":2035,"months":[1]}],"category_name":"Transporte","payment_source":"Salario","group_by":"month","operation":"sum"}}`},
		{"general planning text", `{"intent":"general","reply":"Quero conversar sobre uma compra no proximo ano e quanto posso guardar."}`},
		{"app help", `{"intent":"app_help","reply":"Abra a tela de rendas."}`},
		{"analytical reply and calculations", `{"intent":"financial_analysis","reply":"Participacao: {{calc:0}}.","calculations":[{"operation":"percentage","paths":["/current_month/total_cash_expense","/current_month/total_cash_income"]}]}`},
		{"monthly summary explicit period", `{"intent":"monthly_summary","month":1,"year":2037,"category_name":"Residual"}`},
		{"create mask and null correction", `{"intent":"create_expense","description":"Livro","amount":null,"category_name":"Educacao","month":1,"year":2037,"provided_fields":["amount","year"],"new_expense":false}`},
		{"cancellation", `{"intent":"cancel_expense"}`},
		{"patrimony", `{"intent":"patrimony_summary"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			want, err := parseFinancialIntent(test.text)
			if err != nil {
				t.Fatal(err)
			}
			got, err := parseInterpretedFinancialIntent(test.text, now)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("non-capacity intent was rewritten: got=%+v want=%+v err=%v", got, want, err)
			}
		})
	}
}

func TestInterpretedFinancialParserAcceptsFencedResponsesWithoutChangingAuthority(t *testing.T) {
	now := time.Date(2034, 11, 3, 9, 0, 0, 0, time.FixedZone("Brasilia", -10800))
	for _, fence := range []struct{ name, prefix string }{{"json fence", "```json\n"}, {"plain fence", "```\n"}} {
		t.Run(fence.name, func(t *testing.T) {
			alias := `{"intent":"current_savings_capacity","month":1,"year":2037,"amount":999,"reply":"Unverified","query":{"periods":[{"year":2037,"months":[]}]}}`
			intent, err := parseInterpretedFinancialIntent(fence.prefix+alias+"\n```", now)
			if err != nil {
				t.Fatal(err)
			}
			assertCurrentSavingsCapacityIntent(t, intent, now)
			for _, body := range []string{
				`{"intent":"financial_query","query":{"periods":[{"year":2037,"months":[1,2]}],"category_name":"Transporte","payment_source":"Salario","group_by":"month","operation":"sum"}}`,
				`{"intent":"create_expense","description":"Livro","amount":null,"year":2037,"provided_fields":["amount","year"],"new_expense":false}`,
				`{"intent":"financial_analysis","reply":"Participacao: {{calc:0}}.","calculations":[{"operation":"percentage","paths":["/total_expense","/total_income"]}]}`,
			} {
				want, err := parseFinancialIntent(body)
				if err != nil {
					t.Fatal(err)
				}
				got, err := parseInterpretedFinancialIntent(fence.prefix+body+"\n```", now)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("fence cleanup changed legacy fields or scope: got=%+v want=%+v err=%v", got, want, err)
				}
			}
		})
	}
}

func TestInterpretedFinancialParserDoesNotRepairInvalidInput(t *testing.T) {
	now := time.Date(2034, 11, 3, 9, 0, 0, 0, time.UTC)
	for _, text := range []string{
		`{"intent":"current_savings_capacity",`,
		`{"intent":"current_savings_capacity_later"}`,
		`{"reply":"current_savings_capacity"}`,
		`null`,
		`["current_savings_capacity"]`,
	} {
		if _, err := parseInterpretedFinancialIntent(text, now); err == nil {
			t.Fatalf("invalid response was repaired into capacity: %s", text)
		}
	}
}
