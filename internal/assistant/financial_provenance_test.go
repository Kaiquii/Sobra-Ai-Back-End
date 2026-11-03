package assistant

import (
	"encoding/json"
	"testing"
)

func TestFinancialCalendarMetadataDoesNotAuthorizeMoneyOrPercentages(t *testing.T) {
	data := []byte(`{"as_of":"2035-11-03","year":2034,"month":11,"amount":800,"id":734829,"name":"Plano 55"}`)
	for _, test := range []struct {
		reply string
		valid bool
	}{
		{"Data de referencia: 03/11/2035.", true},
		{"Data de referencia: 2035-11-03.", true},
		{"Os dados atuais sao de 2035.", true},
		{"Em 2034, o valor cadastrado foi R$ 800,00.", true},
		{"A categoria cadastrada e Plano 55.", true},
		{"O valor e R$ 2.035,00.", false},
		{"O valor e R$ **2035**.", false},
		{"O valor e R$ 3,00.", false},
		{"A participacao e 2035%.", false},
		{"A participacao e **2035** %.", false},
		{"A participacao e **3%**.", false},
		{"A participacao e 11%.", false},
		{"A participacao e 2034%.", false},
		{"A participacao e 800%.", false},
		{"A participacao e 734829%.", false},
		{"Plano 55 tem participacao de 55%.", false},
		{"O identificador e 734829.", false},
	} {
		t.Run(test.reply, func(t *testing.T) {
			if got := financialNumbersSupported(test.reply, data); got != test.valid {
				t.Fatalf("supported=%v want %v", got, test.valid)
			}
		})
	}
	if !financialNumbersSupported("Data do registro: 2035-11-03.", []byte(`{"date":"2035-11-03"}`)) {
		t.Fatal("existing exact ISO date spelling became invalid")
	}
}

func TestFinancialPercentageRequiresExplicitPercentageProvenance(t *testing.T) {
	data := []byte(`{"percentage":26.25,"expense_percentage":65,"registered_cash_expense_percentage":150,"total_percentage":40,"amount":800,"year":2035,"name":"Produto 88"}`)
	for _, test := range []struct {
		reply string
		valid bool
	}{
		{"A participacao e 26,25%.", true},
		{"A participacao e **26,25**\u00a0%.", true},
		{"A participacao e __65__\u202f%.", true},
		{"As despesas equivalem a 150,00% das rendas.", true},
		{"A proporcao e 40%.", true},
		{"O valor e R$ 800,00.", true},
		{"O valor e R$ 26,25.", false},
		{"O valor e R$ 40,00.", false},
		{"A participacao e 800%.", false},
		{"A participacao e 2035%.", false},
		{"Produto 88 tem participacao de 88%.", false},
		{"A participacao e -26,25%.", false},
		{"A participacao e 26,24%.", false},
	} {
		t.Run(test.reply, func(t *testing.T) {
			if got := financialNumbersSupported(test.reply, data); got != test.valid {
				t.Fatalf("supported=%v want %v", got, test.valid)
			}
		})
	}
	if !financialNumbersSupported("A variacao e **−12,50**%.", []byte(`{"percentage":-12.5}`)) {
		t.Fatal("negative verified percentage lost its sign")
	}
}

func TestVerifiedPercentageReferencesAndLegacyTextRemainSupported(t *testing.T) {
	facts := map[string]any{"as_of": "2035-11-03", "total_part": 525.0, "total_all": 2000.0}
	calculations, err := verifiedAnalysisCalculations(facts, []any{map[string]any{"operation": "percentage", "paths": []any{"/total_part", "/total_all"}}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"facts": facts, "verified_calculations": calculations})
	for _, text := range []string{"Participacao: 26,25%.", "Participacao: {{calc:0}}."} {
		reply, err := renderAnalysisReferences(text, facts, calculations)
		if err != nil || !financialNumbersSupported(reply, data) {
			t.Fatal("verified percentage was rejected")
		}
	}
	if financialNumbersSupported("Pode gastar R$ 26,25.", data) || financialNumbersSupported("Pode usar 525%.", data) {
		t.Fatal("percentage and money provenance were conflated")
	}
}
