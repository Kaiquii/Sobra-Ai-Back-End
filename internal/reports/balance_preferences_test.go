package reports

import "testing"

func TestAvailableBalanceExcludesEachBenefitRemainderIndependently(t *testing.T) {
	for _, test := range []struct {
		name                 string
		inclusive, va, vr    float64
		excludeVA, excludeVR bool
		want                 float64
	}{
		{"legacy default", 1325, 125, 200, false, false, 1325},
		{"VA only", 1325, 125, 200, true, false, 1200},
		{"VR only", 1325, 125, 200, false, true, 1125},
		{"both", 1325, 125, 200, true, true, 1000},
		{"only VA 725 less expense 600", 125, 125, 0, true, false, 0},
		{"VA 725 less expense 600 keeps other money", 1125, 125, 0, true, false, 1000},
		{"fully used benefit", 1000, 0, 0, true, true, 1000},
		{"negative VA excluded with its sign", 1100, -100, 200, true, false, 1200},
		{"negative benefits remain separate", 850, -100, -50, true, true, 1000},
		{"cash deficit remains visible", 75, 125, 0, true, false, -50},
		{"no benefits", 1000, 0, 0, true, true, 1000},
		{"cent precision on cancellation", 0.30, 0.10, 0.20, true, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := balanceWithBenefitPreferences(test.inclusive, test.va, test.vr, test.excludeVA, test.excludeVR)
			if got != test.want {
				t.Fatalf("available=%v want=%v", got, test.want)
			}
		})
	}
}
