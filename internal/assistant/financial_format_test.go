package assistant

import "testing"

func TestFinancialValidationUnderstandsBrazilianThousandsAndListMarkers(t *testing.T) {
	data := []byte(`{"total_income":2000,"total_expense":1300,"total_balance":700}`)
	for _, reply := range []string{"Renda R$ 2.000, despesas R$ 1.300, sobra R$ 700.", "1. Renda R$ 2.000\n2. Despesas R$ 1.300\n3. Sobra R$ 700."} {
		if !financialNumbersSupported(reply, data) {
			t.Fatal(reply)
		}
	}
	for _, reply := range []string{"1. Saldo R$ 800.", "2. Renda R$ 2.001.", "Saldo R$ 2."} {
		if financialNumbersSupported(reply, data) {
			t.Fatal("invented money accepted", reply)
		}
	}
}
