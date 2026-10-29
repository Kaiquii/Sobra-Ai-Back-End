package assistant

import "testing"

func TestNormalizeInterpretedIntentDoesNotNarrowAllCategoriesFromResidualName(t *testing.T) {
	intent := normalizeInterpretedIntent(financialIntent{
		Intent:       "all_category_expenses",
		Month:        6,
		Year:         2026,
		CategoryName: "Comida",
	})

	if intent.Intent != "all_category_expenses" {
		t.Fatalf("residual category narrowed the requested scope: %q", intent.Intent)
	}
}

func TestNormalizeInterpretedIntentDoesNotChangeAllCategoriesWithoutCategoryName(t *testing.T) {
	intent := normalizeInterpretedIntent(financialIntent{
		Intent: "all_category_expenses",
		Month:  6,
		Year:   2026,
	})

	if intent.Intent != "all_category_expenses" {
		t.Fatalf("expected all_category_expenses, got %q", intent.Intent)
	}
}

func TestNormalizeInterpretedIntentDoesNotNarrowMonthlySummaryFromResidualName(t *testing.T) {
	intent := normalizeInterpretedIntent(financialIntent{
		Intent:       "monthly_summary",
		Month:        6,
		Year:         2026,
		CategoryName: "Comida",
	})

	if intent.Intent != "monthly_summary" {
		t.Fatalf("residual category changed the monthly summary: %q", intent.Intent)
	}
}

func TestNormalizeInterpretedIntentPreservesExplicitCategoryQuery(t *testing.T) {
	intent := normalizeInterpretedIntent(financialIntent{Intent: "category_expenses", CategoryName: " Comida "})
	if intent.Intent != "category_expenses" || intent.CategoryName != "Comida" {
		t.Fatalf("explicit category query was changed: %+v", intent)
	}
}
