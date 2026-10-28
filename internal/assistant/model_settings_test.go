package assistant

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDecodeModelPreference(t *testing.T) {
	for _, test := range []struct {
		name, body, want string
		valid            bool
	}{
		{"selected", `{"model_id":" mock/a:free "}`, "mock/a:free", true},
		{"recommended", `{"model_id":""}`, "", true},
		{"missing", `{}`, "", false},
		{"null", `{"model_id":null}`, "", false},
		{"wrong type", `{"model_id":1}`, "", false},
		{"unknown field", `{"model_id":"","user_id":1}`, "", false},
		{"multiple values", `{"model_id":""}{}`, "", false},
		{"too long", `{"model_id":"` + strings.Repeat("a", 161) + `"}`, "", false},
		{"too large", `{"model_id":"` + strings.Repeat(" ", 4097) + `"}`, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("PUT", "/", strings.NewReader(test.body))
			got, valid := decodeModelPreference(c)
			if valid != test.valid || got != test.want {
				t.Fatalf("got %q, %t; want %q, %t", got, valid, test.want, test.valid)
			}
		})
	}
}

func TestSettingsEffectiveModel(t *testing.T) {
	models := []AssistantModel{{ID: "mock/a:free"}, {ID: "mock/b:free", Recommended: true}}
	for _, test := range []struct {
		name, preference, effective string
		available                   bool
	}{
		{"recommended", "", "mock/b:free", true},
		{"explicit", "mock/a:free", "mock/a:free", true},
		{"retired", "mock/retired:free", "mock/b:free", false},
		{"paid", "mock/paid", "mock/b:free", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := settingsEffectiveModel(test.preference, models)
			if got.ModelID != test.preference || got.EffectiveModelID != test.effective || got.Available != test.available {
				t.Fatalf("unexpected selection: %+v", got)
			}
		})
	}
	if got := settingsEffectiveModel("mock/a:free", nil); got.Available || got.EffectiveModelID != "" {
		t.Fatalf("empty catalog accepted: %+v", got)
	}
}

func TestAssistantModelSettingsRequireAuthenticatedUser(t *testing.T) {
	for _, handler := range []gin.HandlerFunc{listAssistantModels, getAssistantModelPreference, updateAssistantModelPreference} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		handler(c)
		if w.Code != 401 {
			t.Fatalf("unauthenticated settings returned %d", w.Code)
		}
	}
}
