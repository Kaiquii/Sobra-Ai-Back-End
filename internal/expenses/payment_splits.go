package expenses

import (
	"fmt"
	"math"
	"strings"
)

func normalizePaymentSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "salário", "salario":
		return "Salário"
	case "adiantamento":
		return "Adiantamento"
	case "renda extra", "renda_extra":
		return "Renda Extra"
	case "va", "vale alimentação", "vale alimentacao", "vale-alimentação", "vale-alimentacao", "vale_alimentacao":
		return "VA"
	case "vr", "vale refeição", "vale refeicao", "vale-refeição", "vale-refeicao", "vale_refeicao":
		return "VR"
	default:
		return ""
	}
}

// ValidatePaymentSplits validates and normalizes a split payment. An empty
// split list remains supported for legacy clients that send payment_source.
func ValidatePaymentSplits(total float64, legacySource string, inputs []PaymentSplitInput) ([]PaymentSplitInput, string, error) {
	if total <= 0 {
		return nil, "", fmt.Errorf("valor da despesa deve ser maior que zero")
	}
	if len(inputs) == 0 {
		source := normalizePaymentSource(legacySource)
		if source == "" {
			return nil, "", fmt.Errorf("origem do pagamento é obrigatória")
		}
		return []PaymentSplitInput{{PaymentSource: source, Amount: total}}, source, nil
	}
	if strings.TrimSpace(legacySource) != "" {
		return nil, "", fmt.Errorf("envie payment_source ou payment_splits, não ambos")
	}

	normalized := make([]PaymentSplitInput, 0, len(inputs))
	seen := make(map[string]bool, len(inputs))
	var sum float64
	for _, input := range inputs {
		source := normalizePaymentSource(input.PaymentSource)
		if source == "" {
			return nil, "", fmt.Errorf("origem de pagamento inválida")
		}
		if input.Amount <= 0 {
			return nil, "", fmt.Errorf("o valor de cada divisão deve ser maior que zero")
		}
		if seen[source] {
			return nil, "", fmt.Errorf("uma origem de pagamento não pode ser repetida")
		}
		seen[source] = true
		normalized = append(normalized, PaymentSplitInput{PaymentSource: source, Amount: input.Amount})
		sum += input.Amount
	}
	if math.Round(sum*100) != math.Round(total*100) {
		return nil, "", fmt.Errorf("a soma das divisões deve ser igual ao valor da despesa")
	}
	return normalized, normalized[0].PaymentSource, nil
}

// PaymentSplitsOrLegacy returns a single inferred split for expenses created
// before split payments were introduced.
func PaymentSplitsOrLegacy(expense Expense) []PaymentSplit {
	if len(expense.PaymentSplits) > 0 {
		return expense.PaymentSplits
	}
	if source := normalizePaymentSource(expense.PaymentSource); source != "" {
		return []PaymentSplit{{ExpenseID: expense.ID, PaymentSource: source, Amount: expense.Amount}}
	}
	return nil
}

// HydratePaymentSplits makes legacy expenses use the same response shape as
// split-payment expenses without persisting any new rows in the database.
func HydratePaymentSplits(expense *Expense) {
	if expense == nil || len(expense.PaymentSplits) > 0 {
		return
	}
	expense.PaymentSplits = PaymentSplitsOrLegacy(*expense)
}

func HydrateExpensesPaymentSplits(items []Expense) {
	for index := range items {
		HydratePaymentSplits(&items[index])
	}
}
