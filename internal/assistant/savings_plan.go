package assistant

import (
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Cash capacity is computed from the authenticated snapshot, never category
// averages or model-generated money. No balance is carried across months.
func renderSavingsPlan(q financialReadQuery, userID uint, expenseRows []expenses.Expense, incomeRows []incomes.Income, now time.Time) (string, map[string]any, error) {
	if userID == 0 || now.IsZero() || q.GroupBy != "month" || q.CategoryName != "" || q.PaymentSource != "" {
		return "", nil, fmt.Errorf("invalid savings scope")
	}
	months, err := financialGuidanceMonths(q)
	if err != nil {
		return "", nil, err
	}
	known := map[int]bool{}
	for _, row := range incomeRows {
		key := row.Year*100 + row.Month
		month := months[key]
		if row.UserID != userID || month == nil {
			continue
		}
		amount, err := financialQueryCents(row.Amount)
		source := normalizeMoneySource(row.Source)
		if err != nil || amount < 0 || source == "" {
			return "", nil, fmt.Errorf("invalid savings income")
		}
		if source != "va" && source != "vr" {
			month.cashIncome += amount
			known[key] = true
		}
	}
	for _, row := range expenseRows {
		m, y := expenses.EffectiveMonthYear(row)
		month := months[y*100+m]
		if row.UserID != userID || month == nil {
			continue
		}
		_, cash, _, err := financialGuidanceExpenseParts(row, "")
		if err != nil {
			return "", nil, err
		}
		month.otherCash += cash
	}
	keys := []int{}
	for k := range months {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	rows := []map[string]any{}
	lines := []string{"**Quanto pode separar para seu objetivo**"}
	for _, key := range keys {
		m := months[key]
		balance := m.cashIncome - m.otherCash
		row := map[string]any{"year": m.year, "month": m.month, "cash_income": float64(m.cashIncome) / 100, "cash_expense": float64(m.otherCash) / 100, "income_registered": known[key]}
		label := financialQueryMonth(m.month, m.year)
		if !known[key] {
			lines = append(lines, "- **"+label+":** falta renda em dinheiro cadastrada; nao consigo estimar quanto guardar.")
		} else {
			row["registered_cash_balance"] = float64(balance) / 100
			row["potential_savings"] = float64(max(int64(0), balance)) / 100
			if balance > 0 {
				lines = append(lines, fmt.Sprintf("- **%s:** sobra cadastrada de **R$ %s** (renda R$ %s menos despesas em dinheiro R$ %s). Esse e o valor potencial para separar, antes de gastos que ainda nao estejam registrados.", label, formatMoney(float64(balance)/100), formatMoney(float64(m.cashIncome)/100), formatMoney(float64(m.otherCash)/100)))
			} else {
				lines = append(lines, fmt.Sprintf("- **%s:** nao ha sobra cadastrada para guardar; saldo em dinheiro **R$ %s**.", label, formatMoney(float64(balance)/100)))
			}
		}
		rows = append(rows, row)
	}
	lines = append(lines, "VA/VR ficam fora desse calculo. Os valores sao dos cadastros, nao comprovam recebimento ou pagamento; meses futuros dependem da atualizacao das rendas e despesas.", "Para calcular quanto precisa guardar por mes e comparar com essa sobra, informe o valor do objetivo, quanto ja reservou e o mes/ano em que pretende comprar. Nenhum valor foi reservado nem meta cadastrada.")
	return strings.Join(lines, "\n\n"), map[string]any{"status": "read_only", "query": q, "reply_source": "verified_savings", "months": rows, "as_of": now.Format("2006-01-02"), "coverage_complete": true}, nil
}
