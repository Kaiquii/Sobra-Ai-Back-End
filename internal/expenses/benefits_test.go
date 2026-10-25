package expenses

import "testing"

func TestBenefitsPaymentSplits(t *testing.T) {
	splits, _, err := ValidatePaymentSplits(150, "", []PaymentSplitInput{{PaymentSource: "Vale-alimentação", Amount: 100}, {PaymentSource: "vr", Amount: 50}})
	if err != nil || len(splits) != 2 || splits[0].PaymentSource != "VA" || splits[1].PaymentSource != "VR" {
		t.Fatalf("splits: %+v %v", splits, err)
	}
	if _, _, err := ValidatePaymentSplits(150, "", []PaymentSplitInput{{PaymentSource: "VA", Amount: 100}, {PaymentSource: "vale_alimentacao", Amount: 50}}); err == nil {
		t.Fatal("duplicate benefit accepted")
	}
	if _, _, err := ValidatePaymentSplits(100, "VA/VR", nil); err == nil {
		t.Fatal("combined benefit accepted")
	}
}
