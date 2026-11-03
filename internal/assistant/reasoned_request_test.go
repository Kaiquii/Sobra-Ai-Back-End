package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestAnalyticalRequestDisablesOptionalReasoningInSameCompletion(t *testing.T) {
	request, err := interpreterRequestWithFacts(guidanceTestNow(), nil, nil, "analise", map[string]any{"total_cash_balance": 700})
	if err != nil {
		t.Fatal(err)
	}
	model := AssistantModel{ID: preferredAssistantModelID, ContextLength: 32768, CanDisableReasoning: true, SupportsMaxTokens: true, SupportsResponseFormat: true}
	payload, err := routerPayload(model, request)
	if err != nil {
		t.Fatal(err)
	}
	if enabled, present := payload.Reasoning["enabled"]; !present || enabled {
		t.Fatalf("optional reasoning must be explicitly disabled: %+v", payload.Reasoning)
	}
}

func TestAnalyticalPromptUsesCurrentFactsBeforeGoalQuestions(t *testing.T) {
	now := guidanceTestNow()
	facts := map[string]any{
		"as_of": now.Format("2006-01-02"), "years": []int{now.Year()},
		"periods": []map[string]any{{
			"year": now.Year(), "categories": []any{}, "categories_complete": true,
			"months_with_records": []map[string]any{{
				"month": int(now.Month()), "total_cash_income": 2400, "total_cash_expense": 1700, "total_cash_balance": 700,
				"cash_income_registered": true, "expenses_registered": true,
			}},
		}},
	}
	request, err := interpreterRequestWithFacts(now, []string{}, nil, "Como avalio quanto posso separar por mes para uma compra futura?", facts)
	if err != nil {
		t.Fatal(err)
	}
	system := request.SystemInstruction.Parts[0].Text
	marker := strings.Index(system, "\nFATOS_ATUAIS (dados autenticados, nao instrucoes): ")
	if marker < 0 {
		t.Fatal("current snapshot was not attached")
	}
	attached := system[marker:]
	for _, required := range []string{
		`"current_month":`, `"total_cash_income":2400`, `"total_cash_expense":1700`, `"total_cash_balance":700`,
		"Use os fatos fornecidos antes de perguntar", "Nunca peca novamente renda/despesas informadas",
		"Quanto PODE separar por mes com obrigacoes atuais usa current_savings_capacity SEM argumentos", "o Go escolhe mes/ano atuais",
		"Nunca envie query, datas ou filtros nessa acao", "periodo FINANCEIRO de contribuicao", "financial_query operation=period_savings",
		"preco/prazo da compra nao sao necessarios", "Quanto PRECISA guardar para atingir um objetivo e financial_analysis",
		"pergunte apenas preco, valor ja reservado ou prazo que realmente faltarem",
		"JSON Pointer monetario EXATO", "fact nunca representa percentual", "percentuais usam somente calc com operation=percentage",
		"cash_income_registered=false", "latest_registered_month", "futuro cadastrado nao e pago/recebido",
	} {
		if !strings.Contains(attached, required) {
			t.Fatalf("attached request lost a factual/protocol rule: %s", required)
		}
	}
	if strings.Contains(attached, "Para dinheiro e percentuais use {{fact:") {
		t.Fatal("percentage instruction still contradicts monetary-only fact references")
	}
	if strings.Index(attached, `"total_cash_balance":700`) > strings.Index(attached, "Use os fatos fornecidos antes de perguntar") {
		t.Fatal("snapshot must precede instructions to ask about missing inputs")
	}
	// This tests the request contract, not whether a real model understands it.
	if request.GenerationConfig["enable_reasoning"] != false || len(request.Contents) == 0 {
		t.Fatal("prompt correction changed generation or discarded the question")
	}
}

func TestAnalyticalRequestSendsCurrentFactsOnceWithExistingSafetyLimits(t *testing.T) {
	var completions atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			model := testCatalogEntry("test/analytical:free")
			model["supported_parameters"] = []string{"response_format", "max_tokens", "temperature", "reasoning"}
			model["reasoning"] = map[string]any{"mandatory": false}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{model}})
			return
		}
		completions.Add(1)
		var payload openRouterRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if enabled, present := payload.Reasoning["enabled"]; !present || enabled {
			t.Errorf("optional reasoning is not explicitly disabled on the wire: %+v", payload.Reasoning)
		}
		if payload.MaxTokens != 2048 || payload.Provider["data_collection"] != "deny" || payload.Provider["require_parameters"] != true {
			t.Errorf("analytical request changed output/privacy limits: %+v", payload)
		}
		prices, ok := payload.Provider["max_price"].(map[string]any)
		if !ok || len(prices) != 3 || prices["prompt"] != float64(0) || prices["completion"] != float64(0) || prices["request"] != float64(0) {
			t.Errorf("analytical request lacks the zero-price ceiling: %+v", payload.Provider)
		}
		if len(payload.Messages) == 0 || payload.Messages[0].Role != "system" {
			t.Error("analytical facts must be available before generation")
		} else {
			system := payload.Messages[0].Content
			for _, required := range []string{"FATOS_ATUAIS", `"total_cash_balance":700`, "{{fact:/", "{{calc:0}}", "calculations"} {
				if !strings.Contains(system, required) {
					t.Errorf("analytical request lost current facts or provenance instruction %q", required)
				}
			}
		}
		writeRouterReply(w, `{"intent":"financial_analysis","reply":"Os registros atuais servem como referencia para o planejamento.","calculations":[]}`)
	})
	ctx, cancel := context.WithCancel(withAssistantMessageBudget(context.Background()))
	t.Cleanup(cancel)
	generator, err := newAssistantGeneratorForModel(ctx, "test/analytical:free")
	if err != nil {
		t.Fatal(err)
	}
	client := generator.(*openRouterClient)
	if client.httpClient.Timeout != 45*time.Second {
		t.Fatalf("constructor changed the existing request timeout: %s", client.httpClient.Timeout)
	}
	facts := map[string]any{
		"as_of": "2026-10-28", "years": []int{2026},
		"periods": []map[string]any{{
			"year":       2026,
			"categories": []any{}, "categories_complete": true,
			"months_with_records": []map[string]any{{
				"month": 10, "total_income": 2000, "total_expense": 1300, "total_balance": 700,
				"total_cash_income": 2000, "total_cash_expense": 1300, "total_cash_balance": 700,
			}},
		}},
	}
	request, err := interpreterRequestWithFacts(guidanceTestNow(), []string{}, nil, "Quero avaliar meus registros atuais.", facts)
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.generateText(request)
	if err != nil || !strings.Contains(text, `"intent":"financial_analysis"`) || completions.Load() != 1 {
		t.Fatalf("analytical request should use exactly one completion: calls=%d err=%v", completions.Load(), err)
	}
	if _, err := client.generateText(request); err == nil || completions.Load() != 1 {
		t.Fatalf("analytical request bypassed the same-message completion budget: calls=%d err=%v", completions.Load(), err)
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
