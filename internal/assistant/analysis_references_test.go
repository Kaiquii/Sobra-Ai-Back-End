package assistant

import (
	"bytes"
	"encoding/json"
	"log"
	"math"
	"strings"
	"testing"
)

func analysisReferenceFacts() map[string]any {
	return map[string]any{
		"as_of": "2032-10-31", "years": []int{2032}, "id": 734829,
		"private_note":   "PRIVATE_NOTE_918276",
		"total_positive": 700.0, "total_negative": -50.25, "total_zero": 0,
		"total_category": 769.71, "total_expense": 2931.87,
		"periods":    []map[string]any{{"year": 2032, "months_with_records": []map[string]any{{"month": 10, "total_cash_income": 2000.0, "total_cash_expense": 1300.0, "total_cash_balance": 700.0}}}},
		"categories": []map[string]any{{"name": "Categoria ficticia", "monthly_amounts_jan_to_dec": [2]float64{131.34, 113.36}}},
	}
}

func TestAnalysisReferencesRenderAuthenticatedMoneyAndVerifiedCalculations(t *testing.T) {
	facts := analysisReferenceFacts()
	calculations, err := verifiedAnalysisCalculations(facts, []any{
		map[string]any{"operation": "subtract", "paths": []any{"/total_positive", "/total_negative"}},
		map[string]any{"operation": "percentage", "paths": []any{"/total_category", "/total_expense"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeFacts, _ := json.Marshal(facts)
	beforeCalculations, _ := json.Marshal(calculations)
	for _, test := range []struct{ name, input, want string }{
		{"nested native snapshot", "Sobra cadastrada: **{{fact:/periods/0/months_with_records/0/total_cash_balance}}**.", "Sobra cadastrada: **R$ 700,00**."},
		{"negative", "Saldo: {{fact:/total_negative}}.", "Saldo: R$ -50,25."},
		{"registered zero", "Total: {{fact:/total_zero}}.", "Total: R$ 0,00."},
		{"monthly money array", "Mes: {{fact:/categories/0/monthly_amounts_jan_to_dec/1}}.", "Mes: R$ 113,36."},
		{"verified amount", "Diferenca: {{calc:0}}.", "Diferenca: R$ 750,25."},
		{"verified percentage", "Participacao: {{calc:1}}.", "Participacao: 26,25%."},
		{"mixed", "{{fact:/total_positive}}; {{calc:0}}; {{calc:1}}.", "R$ 700,00; R$ 750,25; 26,25%."},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := renderAnalysisReferences(test.input, facts, calculations)
			if err != nil || got != test.want {
				t.Fatalf("rendered %q, want %q: %v", got, test.want, err)
			}
			data, _ := json.Marshal(map[string]any{"facts": facts, "verified_calculations": calculations})
			if !financialNumbersSupported(got, data) {
				t.Fatal("server-rendered reference lost financial provenance")
			}
		})
	}
	afterFacts, _ := json.Marshal(facts)
	afterCalculations, _ := json.Marshal(calculations)
	if !bytes.Equal(beforeFacts, afterFacts) || !bytes.Equal(beforeCalculations, afterCalculations) {
		t.Fatal("rendering mutated snapshot or verified results")
	}
}

func TestAnalysisReferencesFailClosedWithoutExposingUnresolvedReferences(t *testing.T) {
	facts := analysisReferenceFacts()
	calculations := []map[string]any{{"amount": 700.0}}
	for _, input := range []string{
		"{{fact:/years/0}}", "{{fact:/periods/0/year}}", "{{fact:/id}}", "{{fact:/private_note}}",
		"{{fact:/missing/total_amount}}", "{{fact:/total_positive/0}}", "{{fact:total_positive}}",
		"{{fact:/categories/0/name}}", "{{fact:/categories/-1/monthly_amounts_jan_to_dec/0}}",
		"{{unknown:/total_positive}}", "{{}}", "{{ fact:/total_positive}}",
		"{{fact:/total_positive}", "fact:/total_positive}}", "{{{fact:/total_positive}}}",
		"{{fact:/total_positive}}}", "{{fact:/{{total_positive}}}}",
		"{{calc:}}", "{{calc:-1}}", "{{calc:1}}", "{{calc:00}}", "{{calc:+0}}", "{{calc:0.0}}",
		"{{calc:999999999999999999999999999999}}",
		"PRIVATE_RESPONSE_826741 {{fact:/PRIVATE_PATH_572839}}",
	} {
		t.Run(input, func(t *testing.T) {
			got, err := renderAnalysisReferences(input, facts, calculations)
			if got != "" || err == nil || err.Error() != "invalid analysis reference" {
				t.Fatalf("invalid reference did not fail with a closed error: rendered=%q error=%v", got, err)
			}
		})
	}
	for _, invalid := range []map[string]any{
		{"amount": "700"}, {"amount": math.NaN()}, {"percentage": math.Inf(1)},
		{"amount": 700.0, "percentage": 10.0}, {"total_amount": 700.0}, nil,
	} {
		if got, err := renderAnalysisReferences("{{calc:0}}", facts, []map[string]any{invalid}); err == nil || got != "" {
			t.Fatal("malformed verified result was rendered")
		}
	}
	if got, err := renderAnalysisReferences("{{fact:/total_positive}}", nil, nil); err == nil || got != "" {
		t.Fatal("missing authenticated snapshot was rendered")
	}
}

func TestAnalysisReferencesAreBoundedAndKeepLegacyRepliesUnchanged(t *testing.T) {
	facts := analysisReferenceFacts()
	atLimit := strings.Repeat("{{fact:/total_positive}} ", maxAnalysisReplyReferences)
	if got, err := renderAnalysisReferences(atLimit, facts, nil); err != nil || strings.Count(got, "R$ 700,00") != maxAnalysisReplyReferences {
		t.Fatal("bounded reference reply was rejected")
	}
	if got, err := renderAnalysisReferences(atLimit+"{{fact:/total_positive}}", facts, nil); err == nil || got != "" {
		t.Fatal("unbounded references were accepted")
	}
	for _, reply := range []string{"  Saldo **R$ 700,00**.\n", "Texto comum com {chaves}.", ""} {
		if got, err := renderAnalysisReferences(reply, nil, nil); err != nil || got != reply {
			t.Fatal("reference-free legacy text changed")
		}
	}
}

func TestAnalysisReplyReferencesPreserveFinalValidationAndClosedDiagnostics(t *testing.T) {
	facts := analysisReferenceFacts()
	percentage := []any{map[string]any{"operation": "percentage", "paths": []any{"/total_category", "/total_expense"}}}
	for _, test := range []struct {
		name, reply, reason, want string
		facts                     map[string]any
		calculations              any
	}{
		{name: "fact", reply: "Sobra cadastrada: {{fact:/total_positive}}.", facts: facts, want: "Sobra cadastrada: R$ 700,00."},
		{name: "negative", reply: "Saldo: {{fact:/total_negative}}.", facts: facts, want: "Saldo: R$ -50,25."},
		{name: "percentage", reply: "Participacao: {{calc:0}}.", facts: facts, calculations: percentage, want: "Participacao: 26,25%."},
		{name: "legacy", reply: "Sobra cadastrada: R$ 700,00.", facts: facts, want: "Sobra cadastrada: R$ 700,00."},
		{name: "missing facts", reply: "PRIVATE_RESPONSE_826741", reason: "facts_missing"},
		{name: "empty reply", reply: " \n\t", facts: facts, reason: "reply_empty"},
		{name: "invalid calculation", reply: "PRIVATE_RESPONSE_826741", facts: facts, reason: "calculations_invalid", calculations: []any{map[string]any{"operation": "PRIVATE_OPERATION_941276", "paths": []any{"/total_positive"}}}},
		{name: "invalid reference", reply: "PRIVATE_RESPONSE_826741 {{fact:/PRIVATE_PATH_572839}}", facts: facts, reason: "reference_invalid"},
		{name: "invented literal alongside fact", reply: "Sobra {{fact:/total_positive}} e R$ 826.741,29.", facts: facts, reason: "numbers_unsupported"},
		{name: "positive cannot become negative", reply: "Saldo -{{fact:/total_positive}}.", facts: facts, reason: "numbers_unsupported"},
		{name: "percentage cannot become money", reply: "Pode gastar R$ {{calc:0}}.", facts: facts, calculations: percentage, reason: "numbers_unsupported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			previousOutput := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previousOutput) })
			intent := financialIntent{Intent: "financial_analysis", Reply: test.reply, facts: test.facts, provided: map[string]any{"calculations": test.calculations}}
			_, reply, tool, raw := analysisReply(intent)
			result := raw.(map[string]any)
			if tool != "financial_analysis" {
				t.Fatal("analysis tool contract changed")
			}
			if test.reason == "" {
				if result["status"] != "read_only" || reply != test.want || logs.Len() != 0 || result["as_of"] != facts["as_of"] {
					t.Fatalf("verified reply changed: %q %+v logs=%q", reply, result, logs.String())
				}
				return
			}
			if result["status"] != "analysis_not_verified" || len(result) != 1 || !strings.Contains(logs.String(), "analysis_not_verified reason="+test.reason) {
				t.Fatalf("missing closed rejection diagnostic: %+v logs=%q", result, logs.String())
			}
			for _, marker := range []string{"PRIVATE_RESPONSE", "PRIVATE_PATH", "PRIVATE_OPERATION", "PRIVATE_NOTE", "826.741,29", "734829", "/total_positive", "{{"} {
				if strings.Contains(logs.String(), marker) || strings.Contains(reply, marker) {
					t.Fatal("private model content or paths escaped a rejection")
				}
			}
		})
	}
}
