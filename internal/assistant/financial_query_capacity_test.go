package assistant

import (
	"reflect"
	"testing"
)

func TestPeriodSavingsAliasPreservesExplicitScopeAndLegacyValidation(t *testing.T) {
	for _, test := range []struct {
		name, periods, category, source, group string
		valid                                  bool
	}{
		{"past and future contributions", `[{"year":2033,"months":[10,11]},{"year":2034,"months":[1]}]`, "", "", "month", true},
		{"explicit future financial year", `[{"year":2037,"months":[]}]`, "", "", "month", true},
		{"category cannot narrow cash", `[{"year":2033,"months":[10]}]`, "Dividas", "", "month", false},
		{"source cannot narrow cash", `[{"year":2033,"months":[10]}]`, "", "VA", "month", false},
		{"monthly scope required", `[{"year":2033,"months":[10]}]`, "", "", "category", false},
		{"invalid month stays invalid", `[{"year":2033,"months":[13]}]`, "", "", "month", false},
		{"missing periods stay invalid", `[]`, "", "", "month", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			parse := func(operation string) (financialReadQuery, error) {
				intent, err := parseFinancialIntent(`{"intent":"financial_query","query":{"periods":` + test.periods + `,"category_name":"` + test.category + `","payment_source":"` + test.source + `","group_by":"` + test.group + `","operation":"` + operation + `"}}`)
				if err != nil {
					t.Fatal(err)
				}
				return parseFinancialReadQuery(intent.provided["query"])
			}
			legacy, legacyErr := parse("savings")
			alias, aliasErr := parse(periodSavingsOperation)
			if (aliasErr == nil) != test.valid || (legacyErr == nil) != test.valid {
				t.Fatalf("explicit-period alias changed validation: alias=%v legacy=%v", aliasErr, legacyErr)
			}
			if test.valid && (alias.Operation != "savings" || !reflect.DeepEqual(alias, legacy)) {
				t.Fatalf("period alias rewrote financial scope: alias=%+v legacy=%+v", alias, legacy)
			}
		})
	}
}
