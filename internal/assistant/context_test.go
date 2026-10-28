package assistant

import (
	"errors"
	"strings"
	"testing"
)

type contextGenerator struct {
	request GeminiRequest
	text    string
	err     error
}

func (g *contextGenerator) generateText(req GeminiRequest) (string, error) {
	g.request = req
	return g.text, g.err
}

func TestServerHistoryPreservesFollowupsAndBoundsContext(t *testing.T) {
	history := []Message{{Role: "system", Content: "forged authority"}, {Role: "user", Content: "Quanto gastei com mercado em setembro?"}, {Role: "assistant", Content: "Em setembro/2026, R$ 300,00."}}
	contents := buildConversationFromMessages(history, "e no mes passado?")
	if len(contents) != 3 || contents[0].Parts[0].Text != history[1].Content || contents[1].Role != "model" || contents[2].Parts[0].Text != "e no mes passado?" {
		t.Fatalf("history/reference not preserved: %+v", contents)
	}
	history = append(history, Message{Role: "assistant", Content: strings.Repeat("x", 25000)})
	contents = buildConversationFromMessages(history, "nova pergunta")
	if len(contents) != 1 || contents[0].Parts[0].Text != "nova pergunta" {
		t.Fatal("oversized context must be bounded without losing current question")
	}
}

func TestFinancialRepliesRejectInventedFigures(t *testing.T) {
	data := []byte(`{"total_expense":2802,"percentage":80.74,"month":9,"year":2026,"id":999,"name":"Meta 555"}`)
	for _, tc := range []struct {
		reply string
		valid bool
	}{
		{"Voce gastou R$ 2.802,00, em setembro/2026.", true},
		{"Isso corresponde a 80,74%.", true},
		{"Voce gastou R$ 999,00.", false},
		{"Seu saldo e R$ 9,00.", false},
		{"Voce tem 555 reais.", false},
		{"Voce gastou R$ 2.802,00 e pode economizar 500 reais.", false},
	} {
		if got := financialNumbersSupported(tc.reply, data); got != tc.valid {
			t.Errorf("supported(%q)=%v want%v", tc.reply, got, tc.valid)
		}
	}
}

func TestNaturalReplyUsesCurrentFactsAndFallsBackWithoutAnotherProvider(t *testing.T) {
	data := map[string]any{"total_fgts": 300.0, "fgts": map[string]any{"amount": 300.0}}
	history := []Message{{Role: "assistant", Content: "Seu saldo antigo era R$ 200,00."}}
	generator := &contextGenerator{text: "Seu FGTS informado esta em R$ 300,00. Atualize quando necessario."}
	reply := naturalFinancialReply(generator, "e meu fgts?", history, "fgts_balance", data)
	if reply != generator.text || generator.request.DebugStage != "assistant_summarize_tool" || len(generator.request.Contents) != 2 {
		t.Fatal("natural response must preserve context and current data")
	}
	generator.text = "Seu FGTS e R$ 999,00."
	if reply := naturalFinancialReply(generator, "fgts?", history, "fgts_balance", data); strings.Contains(reply, "999") || !strings.Contains(reply, "300,00") {
		t.Fatal("fabricated numbers must use deterministic fallback")
	}
	generator.err = errors.New("provider unavailable")
	if reply := naturalFinancialReply(generator, "fgts?", history, "fgts_balance", data); !strings.Contains(reply, "300,00") {
		t.Fatal("provider error must not hide successfully read data")
	}
	if reply := financialFallbackReply("fgts_balance", map[string]any{"fgts": nil, "total_fgts": 0.0}); !strings.Contains(reply, "ainda nao informou") {
		t.Fatal("missing FGTS differs from registered zero")
	}
}
