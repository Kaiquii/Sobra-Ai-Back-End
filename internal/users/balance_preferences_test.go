package users

import (
	"Sobra_Ai_Back-end/internal/database"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestBalancePreferencesPatchPreservesPresenceAndExplicitFalse(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       map[string]bool
	}{
		{"no-op", `{}`, nil},
		{"VA enabled only", `{"exclude_va_from_available":true}`, map[string]bool{"va": true}},
		{"VA disabled explicitly", `{"exclude_va_from_available":false}`, map[string]bool{"va": false}},
		{"VR enabled only", `{"exclude_vr_from_available":true}`, map[string]bool{"vr": true}},
		{"VR disabled explicitly", `{"exclude_vr_from_available":false}`, map[string]bool{"vr": false}},
		{"both independent", `{"exclude_vr_from_available":false,"exclude_va_from_available":true}`, map[string]bool{"va": true, "vr": false}},
		{"surrounding whitespace", " \n {\"exclude_va_from_available\": false} \t\n", map[string]bool{"va": false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			patch, err := decodeBalancePreferencesPatch(strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range map[string]*bool{"va": patch.excludeVA, "vr": patch.excludeVR} {
				want, present := test.want[key]
				if (value != nil) != present || present && *value != want {
					t.Fatalf("flag %s lost omission/boolean value: present=%t want_present=%t", key, value != nil, present)
				}
			}
		})
	}
}

func balancePreferencesTestRouter(userID any) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if userID != nil {
		router.Use(func(c *gin.Context) { c.Set("user_id", userID) })
	}
	RegisterRoutes(router.Group("/api"))
	return router
}

func TestBalancePreferencesRoutesRequireAuthenticatedCurrentUser(t *testing.T) {
	for _, userID := range []any{nil, uint(0), "1", 1} {
		for _, method := range []string{http.MethodGet, http.MethodPatch} {
			router := balancePreferencesTestRouter(userID)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(method, "/api/users/balance-preferences", strings.NewReader(`{"exclude_va_from_available":true}`))
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("%s accepted absent/invalid current user %T: status=%d", method, userID, recorder.Code)
			}
		}
	}
}

func TestBalancePreferencesPatchRejectsMalformedJSONBeforeDatabaseAccess(t *testing.T) {
	previousDB := database.DB
	database.DB = nil
	t.Cleanup(func() { database.DB = previousDB })
	for _, test := range []struct{ name, body string }{
		{"empty body", ""},
		{"null object", `null`},
		{"array", `[]`},
		{"scalar", `true`},
		{"missing end", `{"exclude_va_from_available":true`},
		{"null flag", `{"exclude_va_from_available":null}`},
		{"string flag", `{"exclude_va_from_available":"false"}`},
		{"number flag", `{"exclude_vr_from_available":0}`},
		{"object flag", `{"exclude_vr_from_available":{}}`},
		{"array flag", `{"exclude_vr_from_available":[]}`},
		{"unknown field", `{"user_id":200,"exclude_va_from_available":true}`},
		{"wrong casing", `{"ExcludeVAFromAvailable":true}`},
		{"duplicate field", `{"exclude_va_from_available":true,"exclude_va_from_available":false}`},
		{"escaped duplicate", `{"exclude_va_from_available":true,"exclude_\u0076a_from_available":false}`},
		{"second object", `{} {"exclude_vr_from_available":true}`},
		{"trailing null", `{} null`},
		{"trailing malformed JSON", `{} garbage`},
		{"mixed invalid flags", `{"exclude_va_from_available":true,"exclude_vr_from_available":null}`},
		{"oversized body", `{"exclude_va_from_available":true}` + strings.Repeat(" ", 4096)},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := balancePreferencesTestRouter(uint(77))
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPatch, "/api/users/balance-preferences", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("invalid patch was accepted or reached database: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var response map[string]any
			if json.Unmarshal(recorder.Body.Bytes(), &response) != nil || response["code"] != "invalid_balance_preferences" {
				t.Fatalf("invalid patch lost its error contract: %s", recorder.Body.String())
			}
		})
	}
}

func TestBalancePreferencesDatabaseFailureIsReportedWithoutPrivateDetails(t *testing.T) {
	previousDB := database.DB
	database.DB = nil
	t.Cleanup(func() { database.DB = previousDB })
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		router := balancePreferencesTestRouter(uint(77))
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, "/api/users/balance-preferences", strings.NewReader(`{"exclude_vr_from_available":false}`))
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("%s database failure returned %d", method, recorder.Code)
		}
		var response map[string]any
		if json.Unmarshal(recorder.Body.Bytes(), &response) != nil || len(response) != 1 || response["error"] != "Nao foi possivel acessar as preferencias de saldo" {
			t.Fatalf("%s database failure leaked details: %s", method, recorder.Body.String())
		}
	}
	if _, err := LoadBalancePreferences(context.Background(), nil, 77); err == nil {
		t.Fatal("missing database was mistaken for a user's default preferences")
	}
}

func TestBalancePreferencesResponseExposesOnlyBothBooleanFlags(t *testing.T) {
	preferences := BalancePreferences{
		UserID: 77, ExcludeVAFromAvailable: true, ExcludeVRFromAvailable: false, UpdatedAt: time.Now(),
	}
	encoded, err := json.Marshal(preferences)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if json.Unmarshal(encoded, &response) != nil || len(response) != 2 || response["exclude_va_from_available"] != true || response["exclude_vr_from_available"] != false {
		t.Fatalf("preferences JSON lost explicit false or exposed internal fields: %s", encoded)
	}
}
