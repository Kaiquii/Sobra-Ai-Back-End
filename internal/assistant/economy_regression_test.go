package assistant

import (
	"encoding/json"
	"testing"
	"time"
)

func TestInterpreterPromptStaysWithinExistingStaticBudget(t *testing.T) {
	// Bound fixed instructions/schema without compressing away task semantics.
	// Dynamic conversation state is bounded separately and measured in live eval.
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.FixedZone("Brasilia", -3*60*60))
	prompt := financialInterpreterPrompt(now, "nenhuma", nil, nil)
	schema, err := json.Marshal(assistantResponseFormat([]string{}))
	if err != nil {
		t.Fatal(err)
	}
	if total := len(prompt) + len(schema); total > 11000 {
		t.Fatalf("interpreter prompt + schema exceeds the explicit fixed context ceiling: %d bytes > 11000", total)
	}
}
