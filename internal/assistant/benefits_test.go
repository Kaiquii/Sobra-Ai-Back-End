package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"testing"
)

func TestBenefitsAssistantPaymentSources(t *testing.T) {
	for input, want := range map[string]string{"VA": "va", "Vale-alimentação": "va", "VR": "vr", "vale_refeicao": "vr"} {
		if got := normalizeMoneySource(input); got != want {
			t.Errorf("%s: %s", input, got)
		}
		if got := normalizeMoneySource(normalizePaymentSource(input)); got != want {
			t.Errorf("payment %s: %s", input, got)
		}
	}
	expense := expenses.Expense{Amount: 150, PaymentSplits: []expenses.PaymentSplit{{PaymentSource: "VA", Amount: 100}, {PaymentSource: "VR", Amount: 50}}}
	if expenseSourceAmount(expense, "VA") != 100 || expenseSourceAmount(expense, "VR") != 50 {
		t.Fatal("benefit filtering lost split")
	}
}
