package patrimony

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAmount(t *testing.T) {
	for _, tc := range []struct {
		input string
		cents int64
		valid bool
	}{
		{"0", 0, true}, {"0.01", 1, true}, {"10.10", 1010, true}, {"9999999999.99", 999999999999, true},
		{"-1", 0, false}, {"1.001", 0, false}, {"10000000000", 0, false}, {"1e999999999", 0, false},
		{"\"10\"", 0, false}, {"null", 0, false}, {"true", 0, false}, {"1e2", 0, false},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var value *amountNumber
			err := json.Unmarshal([]byte(tc.input), &value)
			cents, valid := parseAmount(value)
			valid = valid && err == nil
			if valid != tc.valid || (valid && cents != tc.cents) {
				t.Fatalf("got %d/%v, want %d/%v", cents, valid, tc.cents, tc.valid)
			}
		})
	}
}

func TestInvalidRequestsNeverReachDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"PUT", "/patrimony/fgts", `{}`, 400},
		{"PUT", "/patrimony/fgts", `{"amount":null}`, 400},
		{"PUT", "/patrimony/fgts", `{"amount":1,"user_id":9}`, 400},
		{"PUT", "/patrimony/fgts", `{"amount":1} {}`, 400},
		{"PUT", "/patrimony/fgts", `{"amount":-1}`, 400},
		{"PUT", "/patrimony/fgts", `{"amount":"10"}`, 400},
		{"POST", "/patrimony/investments", `{"name":"  ","category":"CDB","amount":10}`, 400},
		{"POST", "/patrimony/investments", `{"name":"Reserva","amount":10}`, 400},
		{"PATCH", "/patrimony/investments/1", `{}`, 400},
		{"PATCH", "/patrimony/investments/1", `null`, 400},
		{"PATCH", "/patrimony/investments/1", `{"name":null}`, 400},
		{"PATCH", "/patrimony/investments/1", `{"amount":null}`, 400},
		{"PATCH", "/patrimony/investments/1", `{"category":"  "}`, 400},
		{"PATCH", "/patrimony/investments/1", `{"user_id":2}`, 400},
		{"PATCH", "/patrimony/investments/abc", `{"amount":1}`, 400},
		{"DELETE", "/patrimony/investments/0", ``, 400},
		{"POST", "/patrimony/investments", `{"name":"Reserva","category_id":0,"amount":10}`, 400},
		{"POST", "/patrimony/investments", `{"name":"Reserva","category_id":1,"category":"CDB","amount":10}`, 400},
		{"PATCH", "/patrimony/investments/1", `{"category_id":null}`, 400},
		{"PATCH", "/patrimony/investments/1", `{"category_id":-1}`, 400},
		{"PATCH", "/patrimony/investments/1", `{"category_id":0}`, 400},
		{"PATCH", "/patrimony/investments/1", `{"category_id":1,"category":"CDB"}`, 400},
		{"PATCH", "/patrimony/investments/1", `{"category_id":9223372036854775808}`, 400},
		{"POST", "/patrimony/investment-categories", `{}`, 400},
		{"POST", "/patrimony/investment-categories", `{"name":"   "}`, 400},
		{"POST", "/patrimony/investment-categories", `{"name":null}`, 400},
		{"POST", "/patrimony/investment-categories", `{"name":"CDB","is_default":true}`, 400},
		{"PATCH", "/patrimony/investment-categories/1", `{"user_id":2}`, 400},
		{"DELETE", "/patrimony/investment-categories/0", ``, 400},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set("user_id", uint(1)) })
			RegisterRoutes(r.Group("/"))
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRoutesRequireIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []struct{ method, path string }{{"GET", "/patrimony/"}, {"PUT", "/patrimony/fgts"}, {"DELETE", "/patrimony/fgts"}, {"POST", "/patrimony/investments"}, {"PATCH", "/patrimony/investments/1"}, {"DELETE", "/patrimony/investments/1"}} {
		r := gin.New()
		RegisterRoutes(r.Group("/"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != 401 {
			t.Fatalf("%s %s: %d", route.method, route.path, w.Code)
		}
	}
}

func TestCategoryRoutesRequireIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []struct{ method, path string }{{"GET", "/patrimony/investment-categories"}, {"POST", "/patrimony/investment-categories"}, {"PATCH", "/patrimony/investment-categories/1"}, {"DELETE", "/patrimony/investment-categories/1"}} {
		r := gin.New()
		RegisterRoutes(r.Group("/"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != 401 {
			t.Fatalf("%s %s: %d", route.method, route.path, w.Code)
		}
	}
}

func TestCategoryNameValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
	}{{"CDB", true}, {"Minha categoria", true}, {strings.Repeat("á", 80), true}, {strings.Repeat("á", 81), false}, {"\u00a0", false}, {"a\x00b", false}, {"\xff", false}} {
		if validCategoryName(tc.name) != tc.valid {
			t.Errorf("%q: want %v", tc.name, tc.valid)
		}
	}
}
