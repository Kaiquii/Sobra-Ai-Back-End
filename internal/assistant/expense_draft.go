package assistant

import (
	"errors"
	"math"
	"time"
)

// Prepare and validate a draft without touching the database. The canonical
// create/confirmation path resolves ownership and writes only after this step.
func prepareExpenseDraft(args map[string]any) (map[string]any, []float64) {
	result := map[string]any{"draft_version": expenseDraftVersion}
	for key, value := range args {
		if key != "confirm" && key != "status" && key != "draft_version" {
			result[key] = value
		}
	}
	description, _ := stringArg(args, "description")
	category, _ := stringArg(args, "category_name")
	source, _ := stringArg(args, "payment_source")
	source = normalizePaymentSource(source)
	amount, _ := floatArg(args, "amount")
	month, _ := intArg(args, "month")
	year, _ := intArg(args, "year")
	day, _ := intArg(args, "day")
	kind, _ := stringArg(args, "type")
	kind = normalizeExpenseTypeForCreate(kind)
	installments, _ := intArg(args, "installments")
	missing := []string{}
	if description == "" || len([]rune(description)) > 255 {
		missing = append(missing, "description")
	}
	if category == "" {
		missing = append(missing, "category_name")
	}
	if normalizeMoneySource(source) == "" {
		missing = append(missing, "payment_source")
	}
	amounts, amountErr := installmentAmounts(amount, kind, installments)
	if amountErr != nil {
		// Invalid/non-finite values must never reach JSON or the database.
		if math.IsNaN(amount) || math.IsInf(amount, 0) {
			amount = 0
		}
		missing = append(missing, "amount")
	}
	date := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.Local)
	dateText := ""
	if year < 2000 || year > 9999 || month < 1 || month > 12 || day < 1 || date.Year() != year || int(date.Month()) != month || date.Day() != day {
		missing = append(missing, "date")
	} else {
		dateText = date.Format("2006-01-02")
	}
	if kind == "" {
		missing = append(missing, "type")
	}
	if kind == "Parcelada" && (installments < 2 || installments > 120) {
		missing = append(missing, "installments")
	}
	if kind == "Única" {
		installments = 1
	} else if kind == "Fixa" {
		installments = 0
	}
	status := "needs_information"
	if len(missing) == 0 {
		status = "needs_confirmation"
	}
	for key, value := range map[string]any{
		"status": status, "description": description, "amount": amount,
		"category_name": category, "category_id": uint(0), "payment_source": source,
		"date": dateText, "month": month, "year": year, "day": day, "type": kind,
		"installments": installments, "installment_amounts": amounts, "missing_fields": missing,
	} {
		result[key] = value
	}
	return result, amounts
}

// Allocate integer cents, distributing the remainder to the first installments.
// Sum(amounts) must equal the informed total, including non-divisible totals.
func installmentAmounts(total float64, kind string, count int) ([]float64, error) {
	if math.IsNaN(total) || math.IsInf(total, 0) || total <= 0 || total > 1e10 {
		return nil, errors.New("valor deve ser positivo e finito")
	}
	centsFloat := total * 100
	if math.Abs(centsFloat-math.Round(centsFloat)) > 0.0001 {
		return nil, errors.New("informe o valor com no maximo duas casas decimais")
	}
	cents := int64(math.Round(centsFloat))
	if kind == "Fixa" {
		count = 120
	} else if kind != "Parcelada" {
		count = 1
	} else if count < 2 || count > 120 {
		// The caller asks for installments separately.
		return nil, nil
	}
	if kind == "Parcelada" && cents < int64(count) {
		return nil, errors.New("total insuficiente para parcelas de pelo menos um centavo")
	}
	amounts := make([]float64, count)
	for i := range amounts {
		part := cents
		if kind == "Parcelada" {
			part = cents / int64(count)
			if int64(i) < cents%int64(count) {
				part++
			}
		}
		amounts[i] = float64(part) / 100
	}
	return amounts, nil
}
