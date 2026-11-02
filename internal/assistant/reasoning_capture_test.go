package assistant

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCapturedOpenAnswersPassServerValidation(t *testing.T) {
	data, err := os.ReadFile("testdata/reasoning_captured_20261102.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Facts   map[string]any `json:"facts"`
		Records []struct {
			ID       string `json:"id"`
			Response string `json:"response"`
		} `json:"records"`
	}
	if json.Unmarshal(data, &fixture) != nil || len(fixture.Records) != 6 {
		t.Fatal("invalid capture")
	}
	for _, record := range fixture.Records {
		t.Run(record.ID, func(t *testing.T) {
			text, err := normalizeAssistantSchemaResponse(record.Response)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := parseFinancialIntent(text)
			if err != nil {
				t.Fatal(err)
			}
			intent.facts = fixture.Facts
			_, _, _, result := analysisReply(intent)
			if result.(map[string]any)["status"] != "read_only" {
				t.Fatalf("real response rejected: %s", record.ID)
			}
		})
	}
}
