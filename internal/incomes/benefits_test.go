package incomes

import "testing"

func TestBenefitSources(t *testing.T) {
	for input, want := range map[string]string{"VA": "VA", "vale-alimentação": "VA", " vale_alimentacao ": "VA", "vr": "VR", "Vale Refeição": "VR", "vale-refeicao": "VR", "VA/VR": "", "salario": "Salário"} {
		if got := normalizeIncomeSource(input); got != want {
			t.Errorf("%q = %q, want %q", input, got, want)
		}
	}
}
