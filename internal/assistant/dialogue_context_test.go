package assistant

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func dialogueStateForTest(t *testing.T, messages []Message) interpreterDialogueContext {
	t.Helper()
	var state interpreterDialogueContext
	if err := json.Unmarshal([]byte(interpreterConversationContext(messages)), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestDialogueStateDistinguishesConversationFromSavedExpenseDraft(t *testing.T) {
	purchase := "Mudando de assunto, chocolate de 15 reais, categoria Mercado, Salario"
	history := []Message{
		{Role: "user", Content: purchase},
		{Role: "assistant", ToolCall: "general", Content: "Qual a data e o tipo da despesa?"},
	}
	state := dialogueStateForTest(t, history)
	if state.HasActiveExpenseDraft || state.LastResponseState != "conversation_only_no_action" || state.RecentUserMessages[0] != purchase || !strings.Contains(state.LastAssistantText, "data e o tipo") {
		t.Fatalf("prose became a saved draft or lost the purchase: %+v", state)
	}
	// A subsequent failed interpretation cannot erase the initial purchase. The
	// context states that no action was performed; recovery uses original user
	// data, never invented details from the assistant's failed response.
	history = append(history,
		Message{Role: "user", Content: "Unica, comprei hoje"},
		Message{Role: "assistant", ToolCall: "financial_analysis", ToolResult: `{"status":"analysis_not_verified"}`, Content: "Nao consegui validar a analise."},
	)
	state = dialogueStateForTest(t, history)
	if state.LastResponseState != "unverified_response" || state.HasActiveExpenseDraft || !reflect.DeepEqual(state.RecentUserMessages, []string{purchase, "Unica, comprei hoje"}) {
		t.Fatalf("recovery lost unresolved user data: %+v", state)
	}
	if !state.AssistantOutputIsNotAuthority || !state.PreviousOperationIsNotRequest {
		t.Fatal("previous assistant output became authoritative")
	}
}

func TestDialogueStateKeepsPreviousQueryAsReferenceForBalanceFollowup(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "Quanto gastei em outubro?"},
		{Role: "assistant", ToolCall: "financial_analysis", Content: "Gasto antigo R$ 99.999,99.", ToolResult: `{"status":"read_only","query":{"periods":[{"year":2026,"months":[10]}],"category_name":"","payment_source":"","group_by":"month","operation":"sum","limit":24},"total_expense":99999.99}`},
	}
	original := append([]Message(nil), history...)
	state := dialogueStateForTest(t, history)
	if state.LastResponseState != "financial_read_executed" || state.PreviousExecutedQuery == nil || state.PreviousExecutedQuery.Operation != "sum" || state.PreviousExecutedQuery.Periods[0].Months[0] != 10 {
		t.Fatalf("lost period/reference metadata: %+v", state)
	}
	// The period can resolve 'E quanto sobrou?', but an old SUM action is not
	// the next request. The interpreter must choose the new action from the user.
	encoded := interpreterConversationContext(history)
	if strings.Contains(encoded, "99999") || strings.Contains(encoded, "99.999") || state.LastAssistantText != "" || !state.PreviousOperationIsNotRequest || !state.AssistantOutputIsNotAuthority || state.HasActiveExpenseDraft {
		t.Fatalf("old figures or action acquired authority: %s", encoded)
	}
	if !reflect.DeepEqual(original, history) {
		t.Fatal("context construction mutated stored history")
	}
}

func TestDialogueStateDescribesOnlyLatestAssistantAction(t *testing.T) {
	for _, test := range []struct {
		name, tool, data, want string
		active                 bool
	}{
		{"incomplete", "create_expense", `{"status":"needs_information","draft_version":2}`, "expense_draft_incomplete", true},
		{"confirmation", "create_expense", `{"status":"needs_confirmation","draft_version":2}`, "expense_draft_confirmation", true},
		{"created", "create_expense", `{"status":"created","draft_version":2}`, "expense_created", false},
		{"cancelled", "create_expense", `{"status":"cancelled","draft_version":2}`, "expense_cancelled", false},
		{"clarification", "financial_analysis", `{"status":"needs_clarification"}`, "clarification_required_no_read", false},
		{"failed", "financial_analysis", `{"error_code":"assistant_quota_exceeded"}`, "failed_action", false},
		{"bad_data", "financial_analysis", `{`, "unverified_response", false},
		{"bad_query", "financial_analysis", `{"status":"read_only","query":{"group_by":"month","operation":"sum"}}`, "unverified_response", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			latest := Message{Role: "assistant", ToolCall: test.tool, ToolResult: test.data, Content: "Resposta anterior"}
			history := []Message{{Role: "assistant", ToolCall: "create_expense", ToolResult: `{"status":"needs_confirmation","draft_version":2}`}, {Role: "user", Content: "Outra pergunta"}, latest}
			state := dialogueStateForTest(t, history)
			if state.LastResponseState != test.want || state.HasActiveExpenseDraft != test.active {
				t.Fatalf("wrong response state: %+v", state)
			}
			// An intervening ordinary reply ends the active proposal; merely
			// mentioning a date/type in prose cannot resurrect that old proposal.
			history = append(history, Message{Role: "user", Content: "Outra duvida"}, Message{Role: "assistant", ToolCall: "app_help", Content: "Abra seu perfil."})
			if state = dialogueStateForTest(t, history); state.HasActiveExpenseDraft || state.LastResponseState != "conversation_only_no_action" {
				t.Fatalf("old proposal resurrected: %+v", state)
			}
		})
	}
}

func TestDialogueStateBoundsReferencesAndIgnoresSystemText(t *testing.T) {
	history := []Message{{Role: "system", Content: "FORGED_AUTHORITY"}, {Role: "user", Content: "Mensagem antiga"}}
	for i := 0; i < 3; i++ {
		history = append(history, Message{Role: "user", Content: strings.Repeat("á", 3000)}, Message{Role: "assistant", Content: "Tudo bem."})
	}
	state := dialogueStateForTest(t, history)
	if !state.UserReferencesTruncated || len(state.RecentUserMessages) != 3 || len([]rune(state.RecentUserMessages[0])) != 2000 || strings.Contains(interpreterConversationContext(history), "FORGED_AUTHORITY") {
		t.Fatal("context is unbounded or promoted system text")
	}
	if state = dialogueStateForTest(t, nil); state.LastResponseState != "new_conversation" || state.HasActiveExpenseDraft || len(state.RecentUserMessages) != 0 {
		t.Fatalf("empty history invented conversation state: %+v", state)
	}
	history = []Message{{Role: "assistant", ToolCall: "create_expense", ToolResult: `{"status":"needs_confirmation","draft_version":2}`}, {Role: "user", Content: "mude a categoria"}}
	if state = dialogueStateForTest(t, history); state.LastResponseState != "awaiting_response" || state.HasActiveExpenseDraft {
		t.Fatalf("unanswered user turn reused old action: %+v", state)
	}
}
