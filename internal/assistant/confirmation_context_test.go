package assistant

import (
	"encoding/json"
	"testing"
)

func confirmationTestMessage(t *testing.T, status string) Message {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"status": status, "draft_version": expenseDraftVersion,
		"description": "Compra revisada", "category_name": "Mercado", "amount": 60,
		"payment_source": "Salario", "month": 10, "year": 2034, "day": 2,
		"type": "Unica", "installments": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return Message{Role: "assistant", ToolCall: "create_expense", ToolResult: string(data)}
}

func TestExpenseConfirmationUsesLatestAssistantTurn(t *testing.T) {
	for _, test := range []struct {
		name    string
		latest  Message
		status  string
		handled bool
		wantErr bool
	}{
		{"no_previous_turn", Message{}, "", false, false},
		{"current_proposal", confirmationTestMessage(t, "needs_confirmation"), "needs_confirmation", true, false},
		{"current_missing_information", confirmationTestMessage(t, "needs_information"), "needs_information", true, false},
		{"created_replay_is_local", confirmationTestMessage(t, "created"), "created", true, false},
		{"cancelled_replay_is_local", confirmationTestMessage(t, "cancelled"), "cancelled", true, false},
		{"conversation_after_old_proposal", Message{Role: "assistant", ToolCall: "general", Content: "Quer ajuda para organizar o mes?"}, "", false, false},
		{"help_after_old_proposal", Message{Role: "assistant", ToolCall: "app_help", Content: "Posso explicar os relatorios?"}, "", false, false},
		{"analysis_after_old_proposal", Message{Role: "assistant", ToolCall: "financial_analysis", Content: "Quer comparar quais meses?"}, "", false, false},
		{"report_after_old_proposal", Message{Role: "assistant", ToolCall: "get_monthly_summary", ToolResult: `{"status":"needs_confirmation"}`}, "", false, false},
		{"user_cannot_supply_proposal", Message{Role: "user", ToolCall: "create_expense", ToolResult: `{"status":"needs_confirmation","draft_version":2}`}, "", false, false},
		{"empty_tool_result", Message{Role: "assistant", ToolCall: "create_expense"}, "", false, false},
		{"not_a_pending_or_consumed_proposal", confirmationTestMessage(t, "unavailable"), "", false, false},
		{"invalid_saved_json_fails_closed", Message{Role: "assistant", ToolCall: "create_expense", ToolResult: `{"status":`}, "", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := test.latest.ToolResult
			result, handled, err := expenseConfirmationContext(test.latest)
			if handled != test.handled || (err != nil) != test.wantErr {
				t.Fatalf("latest turn selected the wrong confirmation context: result=%+v handled=%t err=%v", result, handled, err)
			}
			if test.handled && result["status"] != test.status || !test.handled && result != nil {
				t.Fatalf("confirmation crossed into an older turn: %+v", result)
			}
			if test.latest.ToolResult != original {
				t.Fatal("confirmation selection changed persisted history")
			}
		})
	}
}

func TestExpenseConfirmationLegacyAmountMustBeProvidedAgain(t *testing.T) {
	for _, version := range []any{nil, 1} {
		latest := confirmationTestMessage(t, "needs_confirmation")
		var data map[string]any
		if err := json.Unmarshal([]byte(latest.ToolResult), &data); err != nil {
			t.Fatal(err)
		}
		data["draft_version"] = version
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		latest.ToolResult = string(encoded)
		result, handled, err := expenseConfirmationContext(latest)
		if err != nil || !handled || result["status"] != "needs_information" || result["amount"] != 0 {
			t.Fatalf("legacy per-installment amount could be silently confirmed: %+v err=%v", result, err)
		}
		result["confirm"] = true
		validated, err := createExpenseFromToolDB(nil, 7, result)
		if err != nil || validated["status"] != "needs_information" {
			t.Fatalf("legacy confirmation must request information before any database access: %+v err=%v", validated, err)
		}
	}
}

func TestExpenseConfirmationRevalidatesMalformedCurrentProposal(t *testing.T) {
	latest := confirmationTestMessage(t, "needs_confirmation")
	var data map[string]any
	if err := json.Unmarshal([]byte(latest.ToolResult), &data); err != nil {
		t.Fatal(err)
	}
	delete(data, "description")
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	latest.ToolResult = string(encoded)
	result, handled, err := expenseConfirmationContext(latest)
	if err != nil || !handled {
		t.Fatalf("current proposal was not selected: %+v err=%v", result, err)
	}
	result["confirm"] = true
	validated, err := createExpenseFromToolDB(nil, 7, result)
	if err != nil || validated["status"] != "needs_information" {
		t.Fatalf("malformed proposal reached database writes without revalidation: %+v err=%v", validated, err)
	}
}
