package assistant

import (
	"Sobra_Ai_Back-end/internal/categories"
	"errors"
	"strings"
	"testing"
)

func TestFinancialCategoryAliasesAreOwnedUniqueAndExactNamesWin(t *testing.T) {
	owned := []categories.Category{
		{ID: 11, UserID: 7, Name: "Uber/99"},
		{ID: 12, UserID: 7, Name: "Alimentação"},
		{ID: 99, UserID: 8, Name: "Uber"},
	}
	for _, test := range []struct {
		hint string
		want uint
	}{{"Uber", 11}, {"uber/99", 11}, {"99", 11}, {"alimentacao", 12}} {
		id, name, err := resolveFinancialCategoryName(7, test.hint, owned)
		if err != nil || id != test.want || name == "" {
			t.Fatalf("unique owned alias %q failed: id=%d name=%s err=%v", test.hint, id, name, err)
		}
	}
	for _, hint := range []string{"SuperUber", "Unknown", ""} {
		_, _, err := resolveFinancialCategoryName(7, hint, owned)
		var clarification *financialQueryClarification
		if !errors.As(err, &clarification) || clarification.reason != "category_not_found" {
			t.Fatalf("unmatched hint became another category: %q err=%v", hint, err)
		}
	}
	ambiguous := append(append([]categories.Category{}, owned...), categories.Category{ID: 13, UserID: 7, Name: "Uber Trabalho"})
	_, _, err := resolveFinancialCategoryName(7, "Uber", ambiguous)
	var clarification *financialQueryClarification
	if !errors.As(err, &clarification) || clarification.reason != "category_ambiguous" {
		t.Fatalf("ambiguous alias guessed a category: %v", err)
	}
	exact := append(ambiguous, categories.Category{ID: 14, UserID: 7, Name: "Uber"})
	id, name, err := resolveFinancialCategoryName(7, "Uber", exact)
	if err != nil || id != 14 || name != "Uber" {
		t.Fatalf("exact owned name did not take precedence: %d %q %v", id, name, err)
	}
	if _, _, err := resolveFinancialCategoryName(0, "Uber", []categories.Category{{ID: 1, UserID: 0, Name: "Uber"}}); err == nil {
		t.Fatal("unauthenticated category was accepted")
	}
}

func TestMonthlySumAlsoAnswersEachRequestedMonthWithoutModelValues(t *testing.T) {
	query := financialReadQuery{Periods: []financialReadPeriod{{Year: 2032, Months: []int{10, 11}}}, CategoryName: "Uber/99", GroupBy: "month", Operation: "sum", Limit: 2}
	monthly := []financialReadRow{{Label: "outubro/2032", Year: 2032, Month: 10, Amount: 46.25, Registered: true}, {Label: "novembro/2032", Year: 2032, Month: 11, Amount: 79.50, Registered: true}}
	shown, total, matched := finalizeFinancialReadRows(query, monthly)
	reply := financialQueryReply(query, shown, total, matched, false, monthly)
	for _, expected := range []string{"125,75", "outubro/2032", "46,25", "novembro/2032", "79,50"} {
		if !strings.Contains(reply, expected) {
			t.Fatalf("sum lost requested monthly scope %q: %s", expected, reply)
		}
	}
	if total != 125.75 || len(shown) != 1 || matched != 2 {
		t.Fatal("adding breakdown changed the sum contract")
	}
}
