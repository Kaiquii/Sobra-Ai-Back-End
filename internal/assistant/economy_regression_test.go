package assistant

import (
	"testing"
	"time"
)

func TestInterpreterPromptStaysWithinExistingStaticBudget(t *testing.T) {
	// The previous interpreter instructions and shared reply style occupied
	// 5,669 UTF-8 bytes, before dynamic facts. Improvements must fit that budget.
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.FixedZone("Brasilia", -3*60*60))
	prompt := financialInterpreterPrompt(now, "nenhuma", nil, nil)
	if len(prompt) > 5669 {
		t.Fatalf("interpreter prompt grew beyond the existing input budget: %d bytes > 5669", len(prompt))
	}
}
