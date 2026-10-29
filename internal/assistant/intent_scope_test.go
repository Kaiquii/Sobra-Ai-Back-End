package assistant

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDraftFieldMaskRejectsUnrequestedNonEmptyChanges(t *testing.T) {
	draft := map[string]any{
		"description": "Compra anterior", "amount": float64(80.50), "month": float64(10),
		"year": float64(2034), "day": float64(2), "payment_source": "Salario", "type": "Unica",
		"category_name": "Mercado", "category_suggested": true,
	}
	intent, err := parseFinancialIntent(`{"intent":"create_expense","category_name":"Transporte","description":"Compra diferente","amount":999,"year":2099,"provided_fields":["category_name"]}`)
	if err != nil {
		t.Fatal(err)
	}
	args := expenseArgsFromIntent(intent, draft)
	for _, key := range []string{"description", "amount", "year", "month", "day", "payment_source", "type"} {
		if args[key] != draft[key] {
			t.Fatalf("category correction changed %s: %v", key, args[key])
		}
	}
	if args["category_name"] != "Transporte" || args["confirm"] != false {
		t.Fatalf("requested correction was not preserved as a draft: %+v", args)
	}
	if _, exists := args["category_suggested"]; exists {
		t.Fatal("user-selected category retained the old suggestion flag")
	}
	intent, _ = parseFinancialIntent(`{"intent":"create_expense","category_name":"Outra","category_suggested":true,"amount":25,"provided_fields":["amount"]}`)
	args = expenseArgsFromIntent(intent, draft)
	if args["amount"] != float64(25) || args["category_name"] != "Mercado" || args["category_suggested"] != true {
		t.Fatalf("amount correction overwrote category metadata: %+v", args)
	}
	intent, _ = parseFinancialIntent(`{"intent":"create_expense","amount":0,"provided_fields":["amount"]}`)
	if expenseArgsFromIntent(intent, draft)["amount"] != float64(0) {
		t.Fatal("explicit invalid amount was silently replaced by an older value")
	}
}

func TestValidatedQueryHistoryKeepsScopeWithoutRepeatingOldFigures(t *testing.T) {
	query := financialReadQuery{
		Periods: []financialReadPeriod{{Year: 2034, Months: []int{10, 11}}}, CategoryName: "Uber/99",
		GroupBy: "month", Operation: "compare", Limit: 24,
	}
	encoded, err := json.Marshal(map[string]any{"status": "read_only", "query": query})
	if err != nil {
		t.Fatal(err)
	}
	history := []Message{
		{Role: "user", Content: "Compare outubro e novembro de 2034 com Uber"},
		{Role: "assistant", Content: "Relatorio antigo com R$ 9.999,99", ToolCall: "financial_analysis", ToolResult: string(encoded)},
		{Role: "user", Content: "Nao foi isso que pedi"},
	}
	original := append([]Message(nil), history...)
	compacted := compactFinancialHistory(history)
	if !reflect.DeepEqual(history, original) || compacted[0].Content != original[0].Content || compacted[2].Content != original[2].Content {
		t.Fatal("history compaction changed stored messages or user corrections")
	}
	if strings.Contains(compacted[1].Content, "9.999,99") || !strings.Contains(compacted[1].Content, `"months":[10,11]`) || !strings.Contains(compacted[1].Content, `"category_name":"Uber/99"`) {
		t.Fatalf("validated scope lost or stale money repeated: %s", compacted[1].Content)
	}
	history[1].ToolResult = `{"status":"read_only","years":[2034]}`
	history[1].Content = "Conselho personalizado que depende do contexto anterior."
	if compactFinancialHistory(history)[1].Content != history[1].Content {
		t.Fatal("conversational advice without an executed plan was discarded")
	}
}

func TestInterpreterAcceptsQueryPlanWithoutFabricatedAnswer(t *testing.T) {
	intent, err := parseFinancialIntent(`{"intent":"financial_query","query":{"periods":[{"year":2034,"months":[10,11]}],"category_name":"Uber/99","group_by":"month","operation":"compare"}}`)
	if err != nil || intent.Intent != "financial_query" || intent.Reply != "" || intent.provided["query"] == nil {
		t.Fatalf("query plan requires a second inference or fabricated prose: %+v %v", intent, err)
	}
}

func TestMissingMonthsCannotSilentlyExpandQueryToWholeYear(t *testing.T) {
	raw := map[string]any{
		"periods":  []any{map[string]any{"year": 2034}},
		"group_by": "month", "operation": "max",
	}
	_, _, tool, result := executeFinancialQueryDB(nil, nil, 0, raw, assistantNow())
	if tool != "financial_analysis" || result.(map[string]any)["status"] != "needs_clarification" {
		t.Fatalf("incomplete period was silently widened: %+v", result)
	}
}

func TestVerifiedSummaryHistoryKeepsAnnualScopeWithoutOldFigures(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "Onde posso rever meus gastos de 2034?"},
		{Role: "assistant", Content: "Resumo antigo: R$ 9.999,99.", ToolCall: "financial_analysis", ToolResult: `{"status":"read_only","reply_source":"verified_summary","analysis_status":"analysis_not_verified","as_of":"2034-10-29","years":[2034]}`},
	}
	original := append([]Message(nil), history...)
	compacted := compactFinancialHistory(history)
	if !reflect.DeepEqual(history, original) || compacted[0].Content != history[0].Content {
		t.Fatal("summary compaction mutated stored history or the user's question")
	}
	if strings.Contains(compacted[1].Content, "9.999") || !strings.Contains(compacted[1].Content, `"years":[2034]`) || !strings.Contains(compacted[1].Content, `"verified_summary":true`) {
		t.Fatalf("summary retained old amounts or lost the annual scope: %s", compacted[1].Content)
	}
}
