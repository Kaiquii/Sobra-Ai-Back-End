package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCatalogPrioritizesBoundedNativeProtocolWithoutRemovingFreeOptions(t *testing.T) {
	setupRouterTest(t, func(w http.ResponseWriter, r *http.Request) {
		entries := []any{}
		for _, model := range []struct {
			id, name                  string
			native, reasoningOptional bool
		}{
			{legacyJSONAssistantModelID, "A legacy", true, true},
			{"test/plain:free", "B plain", false, true},
			{"test/mandatory:free", "C native mandatory", true, false},
			{"test/native:free", "Z native optional", true, true},
		} {
			entry := testCatalogEntry(model.id)
			entry["name"] = model.name
			parameters := []string{"max_tokens", "response_format", "reasoning"}
			if model.native {
				parameters = append(parameters, "structured_outputs")
			}
			entry["supported_parameters"] = parameters
			entry["reasoning"] = map[string]any{"mandatory": !model.reasoningOptional}
			entries = append(entries, entry)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": entries})
	})
	models, err := listFreeAssistantModels(context.Background())
	if err != nil || len(models) != 4 {
		t.Fatalf("free options were removed: %v %v", models, err)
	}
	want := []string{"test/native:free", "test/mandatory:free", legacyJSONAssistantModelID, "test/plain:free"}
	for i, model := range models {
		if model.ID != want[i] || model.Recommended != (i == 0) {
			t.Fatalf("fallback priority lost protocol or reasoning capabilities: %v", models)
		}
	}
}
