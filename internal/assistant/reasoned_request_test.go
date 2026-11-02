package assistant

import (
	"strings"
	"testing"
)

func TestReasoningSeesCurrentFactsAndUserCorrection(t *testing.T) {
	facts := map[string]any{"total_cash_income": 2000, "total_cash_expense": 1300, "total_cash_balance": 700}
	history := []Message{{Role: "user", Content: "Compare meus gastos"}, {Role: "assistant", Content: "Interpretacao antiga"}}
	request, err := interpreterRequestWithFacts(guidanceTestNow(), []string{}, history, "Nao foi isso. Quero avaliar minha capacidade de poupar.", facts)
	if err != nil {
		t.Fatal(err)
	}
	system := request.SystemInstruction.Parts[0].Text
	if !strings.Contains(system, `"total_cash_balance":700`) || !strings.Contains(system, "financial_analysis") {
		t.Fatal("model cannot reason from current facts")
	}
	if !strings.Contains(request.Contents[len(request.Contents)-1].Parts[0].Text, "Nao foi isso") {
		t.Fatal("lost current correction")
	}
	for _, reply := range []string{"A sobra cadastrada e R$ 700,00.", "Voce tem R$ 900,00 para guardar."} {
		_, _, _, raw := analysisReply(financialIntent{Intent: "financial_analysis", Reply: reply, facts: facts, provided: map[string]any{}})
		want := "read_only"
		if strings.Contains(reply, "900") {
			want = "analysis_not_verified"
		}
		if raw.(map[string]any)["status"] != want {
			t.Fatalf("unverified answer: %+v", raw)
		}
	}
}

func TestAnalyticalRequestEnablesReasoningInSameCompletion(t *testing.T) {
	request, err := interpreterRequestWithFacts(guidanceTestNow(), nil, nil, "analise", map[string]any{"total_cash_balance": 700})
	if err != nil {
		t.Fatal(err)
	}
	model := AssistantModel{ID: preferredAssistantModelID, ContextLength: 32768, CanDisableReasoning: true, SupportsMaxTokens: true, SupportsResponseFormat: true}
	payload, err := routerPayload(model, request)
	if err != nil || payload.Reasoning["enabled"] != true {
		t.Fatalf("reasoning still disabled: %v %+v", err, payload.Reasoning)
	}
}

func TestReasoningRejectsMissingOrOversizedSnapshot(t *testing.T) {
	for _, facts := range []map[string]any{nil, {"name": strings.Repeat("x", 48001)}} {
		if _, err := interpreterRequestWithFacts(guidanceTestNow(), nil, nil, "ajuda", facts); err == nil {
			t.Fatal("missing/truncated data accepted")
		}
	}
}

func TestAnalyticalProtocolAcceptsVerifiedCalculationAndRejectsFabrication(t *testing.T) {
	schema := responseSchemaTestJSON(t, assistantResponseSchema())
	action := map[string]any{"action": map[string]any{"intent": "financial_analysis", "reply": "Sua sobra e R$ 700,00.", "calculations": []any{map[string]any{"operation": "subtract", "paths": []any{"/total_cash_income", "/total_cash_expense"}}}}}
	if err := responseSchemaTestValidate(schema, action); err != nil {
		t.Fatal(err)
	}
	data := responseSchemaTestJSON(t, action["action"].(map[string]any))
	intent := financialIntent{Intent: "financial_analysis", Reply: "Sua sobra e R$ 700,00.", provided: data, facts: map[string]any{"total_cash_income": 2000, "total_cash_expense": 1300}}
	_, _, _, raw := analysisReply(intent)
	if raw.(map[string]any)["status"] != "read_only" {
		t.Fatal(raw)
	}
	intent.Reply = "Sua sobra e R$ 800,00."
	_, _, _, raw = analysisReply(intent)
	if raw.(map[string]any)["status"] != "analysis_not_verified" {
		t.Fatal("fabricated calculation accepted")
	}
}
