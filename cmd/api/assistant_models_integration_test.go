//go:build integration

package main

import (
	"Sobra_Ai_Back-end/internal/auth"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/migrations"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type modelCatalogIntegrationTransport struct{ base http.RoundTripper }

func (transport modelCatalogIntegrationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Hostname() == "127.0.0.1" {
		return transport.base.RoundTrip(req)
	}
	if req.URL.Hostname() != "openrouter.ai" || req.Method != http.MethodGet || req.URL.Path != "/api/v1/models" {
		return nil, fmt.Errorf("external network forbidden in model settings integration")
	}
	body, err := integrationModelCatalogJSON()
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
}

func TestIntegrationAssistantModelSettings(t *testing.T) {
	db := disposablePostgres(t)
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	// Replay only the additive upgrade from the last published schema.
	if err := db.Exec("DROP TABLE assistant_model_preferences; DELETE FROM schema_migrations WHERE version = 9").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrations.Validate(db); err == nil {
		t.Fatal("schema 8 must require the new model preferences migration")
	}
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(db); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Validate(db); err != nil {
		t.Fatal(err)
	}
	previousDB, previousTransport := database.DB, http.DefaultTransport
	database.DB = db
	http.DefaultTransport = modelCatalogIntegrationTransport{base: previousTransport}
	t.Cleanup(func() { database.DB, http.DefaultTransport = previousDB, previousTransport })
	// A legacy shortlist must no longer hide models from the dynamic catalog.
	t.Setenv("OPENROUTER_MODELS", "mock/api:free")
	t.Setenv("OPENROUTER_API_KEY", "harness-only")
	t.Setenv("JWT_SECRET", "model-settings-harness-only")
	owner := auth.User{Name: "Settings owner", Email: "model-owner@example.test", Password: "unused"}
	other := auth.User{Name: "Settings other", Email: "model-other@example.test", Password: "unused"}
	for _, user := range []*auth.User{&owner, &other} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	tokenFor := func(user auth.User) string {
		t.Helper()
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": user.ID, "role": "user", "exp": time.Now().Add(time.Hour).Unix(),
		}).SignedString([]byte("model-settings-harness-only"))
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	server := httptest.NewServer(newRouter())
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 5 * time.Second}
	request := func(method, path, token, body string, want int) map[string]any {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var decoded map[string]any
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: status %d want %d body %+v", method, path, response.StatusCode, want, decoded)
		}
		return decoded
	}
	base := "/api/assistant"
	ownerToken, otherToken := tokenFor(owner), tokenFor(other)
	for _, path := range []string{"/models", "/model-preference"} {
		request("GET", base+path, "", "", 401)
	}
	request("PUT", base+"/model-preference", "", `{"model_id":""}`, 401)
	catalog := request("GET", base+"/models", ownerToken, "", 200)
	if catalog["selected_model"] != "" || catalog["recommended_model"] != "mock/api:free" || catalog["automatic_fallback"] != true || len(catalog["models"].([]any)) != 5 {
		t.Fatalf("catalog allows paid model or wrong default: %+v", catalog)
	}
	preference := request("GET", base+"/model-preference", ownerToken, "", 200)
	if preference["model_id"] != "" || preference["effective_model_id"] != "mock/api:free" || preference["available"] != true {
		t.Fatalf("missing preference did not use recommendation: %+v", preference)
	}
	preference = request("PUT", base+"/model-preference", ownerToken, `{"model_id":"mock/settings-b:free"}`, 200)
	if preference["model_id"] != "mock/settings-b:free" || preference["effective_model_id"] != "mock/settings-b:free" {
		t.Fatalf("selected model not returned: %+v", preference)
	}
	for _, id := range []string{"mock/extra-conversation:free", "mock/promo-free"} {
		selected := request("PUT", base+"/model-preference", ownerToken, `{"model_id":"`+id+`"}`, 200)
		if selected["model_id"] != id || selected["effective_model_id"] != id {
			t.Fatalf("free model outside former shortlist rejected: %+v", selected)
		}
	}
	request("PUT", base+"/model-preference", ownerToken, `{"model_id":"mock/settings-a:free"}`, 200)
	request("PUT", base+"/model-preference", ownerToken, `{"model_id":"mock/settings-b:free"}`, 200)
	var preferenceRows int64
	if err := db.Table("assistant_model_preferences").Where("user_id = ?", owner.ID).Count(&preferenceRows).Error; err != nil || preferenceRows != 1 {
		t.Fatalf("preference upsert: %d %v", preferenceRows, err)
	}
	if after := request("GET", base+"/model-preference", ownerToken, "", 200); after["model_id"] != "mock/settings-b:free" {
		t.Fatalf("preference not persisted: %+v", after)
	}
	if isolated := request("GET", base+"/model-preference", otherToken, "", 200); isolated["model_id"] != "" {
		t.Fatalf("preference leaked to other user: %+v", isolated)
	}
	for _, body := range []string{
		`{}`, `{"model_id":null}`, `{"model_id":1}`, `{"model_id":"","user_id":999}`,
		`{"model_id":"openai/paid"}`, `{"model_id":"mock/unknown:free"}`, `{"model_id":"mock/settings-fee:free"}`,
		`{"model_id":"` + strings.Repeat("a", 161) + `"}`, `{"model_id":""}{}`,
	} {
		request("PUT", base+"/model-preference", ownerToken, body, 400)
	}
	if after := request("GET", base+"/model-preference", ownerToken, "", 200); after["model_id"] != "mock/settings-b:free" {
		t.Fatal("rejected write changed preference")
	}
	// A removed model falls back without rewriting the user's stored choice.
	if err := db.Exec("UPDATE assistant_model_preferences SET model_id = 'mock/retired:free' WHERE user_id = ?", owner.ID).Error; err != nil {
		t.Fatal(err)
	}
	retired := request("GET", base+"/model-preference", ownerToken, "", 200)
	if retired["model_id"] != "mock/retired:free" || retired["available"] != false || retired["effective_model_id"] != "mock/api:free" {
		t.Fatalf("retired preference: %+v", retired)
	}
	if after := request("GET", base+"/models", ownerToken, "", 200); after["selected_model"] != "" || after["effective_model_id"] != "mock/api:free" {
		t.Fatalf("catalog should select recommendation for retired model: %+v", after)
	}
	request("PUT", base+"/model-preference", ownerToken, `{"model_id":""}`, 200)
	request("PUT", base+"/model-preference", otherToken, `{"model_id":"mock/settings-a:free"}`, 200)
	if err := db.Model(&other).Update("access_blocked", true).Error; err != nil {
		t.Fatal(err)
	}
	request("GET", base+"/models", otherToken, "", 403)
	request("PUT", base+"/model-preference", otherToken, `{"model_id":""}`, 403)
	if err := db.Delete(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("assistant_model_preferences").Where("user_id = ?", other.ID).Count(&preferenceRows).Error; err != nil || preferenceRows != 0 {
		t.Fatalf("preference was not deleted with user: %d %v", preferenceRows, err)
	}
	if err := db.Exec("INSERT INTO assistant_model_preferences (user_id, model_id) VALUES (999999999, '')").Error; err == nil {
		t.Fatal("preference table is missing user foreign key")
	}
}
