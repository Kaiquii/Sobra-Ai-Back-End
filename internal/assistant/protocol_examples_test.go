package assistant

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProtocolDemonstrationsAreValidAndSeparateCapacityFromGoal(t *testing.T) {
	contents := interpreterContents(nil, "mensagem real")
	schema := responseSchemaTestJSON(t, assistantResponseSchema())
	now := time.Date(2034, 11, 3, 9, 0, 0, 0, time.FixedZone("Brasilia", -10800))
	intents := []string{}
	for _, content := range contents {
		if content.Role != "model" {
			continue
		}
		var example map[string]any
		if err := json.Unmarshal([]byte(content.Parts[0].Text), &example); err != nil {
			t.Fatal(err)
		}
		if err := responseSchemaTestValidate(schema, example); err != nil {
			t.Fatal(err)
		}
		text, err := normalizeAssistantSchemaResponse(content.Parts[0].Text)
		if err != nil {
			t.Fatal(err)
		}
		rawIntent, err := parseFinancialIntent(text)
		if err != nil {
			t.Fatal(err)
		}
		intents = append(intents, rawIntent.Intent)
		intent, err := parseInterpretedFinancialIntent(text, now)
		if err != nil {
			t.Fatal(err)
		}
		if intent.Intent == "financial_query" {
			query, err := parseFinancialReadQuery(intent.provided["query"])
			if err != nil || query.Operation != "savings" || query.CategoryName != "" || query.PaymentSource != "" || len(query.Periods) != 1 {
				t.Fatal("capacity demonstration lost its authenticated cash scope")
			}
			if rawIntent.Intent == currentSavingsCapacityIntent {
				if query.Periods[0].Year != now.Year() || len(query.Periods[0].Months) != 1 || query.Periods[0].Months[0] != int(now.Month()) {
					t.Fatal("current capacity copied a demonstration or goal date instead of the server clock")
				}
			} else if query.Periods[0].Year != 2031 || len(query.Periods[0].Months) != 2 || query.Periods[0].Months[0] != 6 || query.Periods[0].Months[1] != 7 {
				t.Fatal("explicit contribution period was rewritten as current capacity")
			}
		}
	}
	if len(intents) != 4 || intents[0] != "create_expense" || intents[1] != currentSavingsCapacityIntent || intents[2] != "financial_query" || intents[3] != "financial_analysis" {
		t.Fatalf("incomplete protocol demonstrations: %v", intents)
	}
	if contents[len(contents)-1].Parts[0].Text != "mensagem real" {
		t.Fatal("a demonstration replaced the current user request")
	}
}

func TestCapturedFastAnswerAskingKnownIncomeFailsSemanticQualification(t *testing.T) {
	corpus := fastEvalLoadFixedCorpus(t)
	facts, err := compactAnalysisFactsForPrompt(corpus.Facts)
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic capture from the first authorized qualification call. Numeric
	// validation alone cannot prove that the model used the supplied context.
	intent := fastEvalParseForTest(t, `{"intent":"financial_analysis","reply":"Para planejar a compra de um carro no próximo ano, preciso de algumas informações: qual é o valor total que você estima para o carro, qual prazo você tem em mente (em quantos meses) e qual renda mensal você recebe? Com esses dados, posso calcular quanto você pode reservar por mês, considerando suas dívidas atuais."}`)
	score := scoreFastEval(corpus.Cases[0], intent, facts, corpus.Now)
	if score.Passed || score.Reason != "asks_registered_income_or_expenses" {
		t.Fatalf("a fast but ungrounded answer passed: %+v", score)
	}
}

func TestCapacityParaphraseAcceptsTheSameAuthenticatedSavingsScope(t *testing.T) {
	corpus := fastEvalLoadFixedCorpus(t)
	facts, err := compactAnalysisFactsForPrompt(corpus.Facts)
	if err != nil {
		t.Fatal(err)
	}
	intent := fastEvalParseForTest(t, `{"intent":"financial_query","query":{"periods":[{"year":2026,"months":[11]}],"category_name":"","payment_source":"","group_by":"month","operation":"savings"}}`)
	score := scoreFastEval(corpus.Cases[1], intent, facts, corpus.Now)
	if !score.Passed {
		t.Fatalf("the equivalent capacity question rejected the correct savings plan: %+v", score)
	}
}

func TestCapturedGoalYearQueryStillFailsSemanticQualification(t *testing.T) {
	corpus := fastEvalLoadFixedCorpus(t)
	facts, err := compactAnalysisFactsForPrompt(corpus.Facts)
	if err != nil {
		t.Fatal(err)
	}
	// The real synthetic response selected the right operation, but consulted
	// the purchase year instead of the current cash-flow period. The protocol
	// correction must not turn that old, explicitly scoped query into a pass.
	intent, err := parseInterpretedFinancialIntent(`{"intent":"financial_query","query":{"periods":[{"year":2027,"months":[]}],"category_name":"","payment_source":"","group_by":"month","operation":"savings"}}`, corpus.Now)
	if err != nil {
		t.Fatal(err)
	}
	score := scoreFastEval(corpus.Cases[0], intent, facts, corpus.Now)
	if score.Passed || score.Reason != "capacity_scope_mismatch" {
		t.Fatalf("the old wrong-period response was silently rewritten or accepted: %+v", score)
	}
}
