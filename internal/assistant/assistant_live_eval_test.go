//go:build assistant_eval

package assistant

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

type liveEvalExpectation struct {
	Kind      string `json:"kind"`
	Operation string `json:"operation"`
	GroupBy   string `json:"group_by"`
	Category  string `json:"category"`
	Year      int    `json:"year"`
	Months    []int  `json:"months"`
}
type liveEvalCase struct {
	ID      string              `json:"id"`
	Message string              `json:"message"`
	History []Message           `json:"history"`
	Draft   map[string]any      `json:"draft"`
	Expect  liveEvalExpectation `json:"expect"`
}
type liveEvalCorpus struct {
	Version    int            `json:"version"`
	Now        time.Time      `json:"now"`
	Categories []string       `json:"categories"`
	Cases      []liveEvalCase `json:"cases"`
}
type liveEvalRecord struct {
	ID                string         `json:"id"`
	Passed            bool           `json:"passed"`
	Reason            string         `json:"reason,omitempty"`
	Actual            map[string]any `json:"actual_plan,omitempty"`
	PromptHash        string         `json:"prompt_sha256"`
	SchemaHash        string         `json:"schema_sha256"`
	RequestHash       string         `json:"request_sha256,omitempty"`
	Model             string         `json:"model"`
	DurationMillis    int64          `json:"duration_ms"`
	PromptTokens      int64          `json:"prompt_tokens"`
	CompletionTokens  int64          `json:"completion_tokens"`
	TotalTokens       int64          `json:"total_tokens"`
	Attempts          int            `json:"completion_attempts"`
	SyntheticResponse string         `json:"synthetic_response,omitempty"`
}
type liveEvalTransport struct {
	base            http.RoundTripper
	maxCalls, calls int
	record          *liveEvalRecord
}

func (transport *liveEvalTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "openrouter.ai" {
		return nil, fmt.Errorf("evaluation destination rejected")
	}
	if request.Method != http.MethodPost || request.URL.Path != "/api/v1/chat/completions" {
		return transport.base.RoundTrip(request)
	}
	if transport.calls >= transport.maxCalls {
		return nil, fmt.Errorf("evaluation completion ceiling reached")
	}
	transport.calls++
	transport.record.Attempts++
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	transport.record.RequestHash = liveEvalHash(body)
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(data))
	if err != nil {
		return response, nil
	}
	var decoded openRouterResponse
	if json.Unmarshal(data, &decoded) == nil && len(decoded.Choices) > 0 {
		transport.record.SyntheticResponse = decoded.Choices[0].Message.Content
	}
	if json.Unmarshal(data, &decoded) == nil && decoded.Usage != nil {
		transport.record.PromptTokens = decoded.Usage.PromptTokens
		transport.record.CompletionTokens = decoded.Usage.CompletionTokens
		transport.record.TotalTokens = decoded.Usage.TotalTokens
	}
	return response, nil
}

func liveEvalHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

// This opt-in test is separate from the offline harness. It never opens a DB,
// reads .env, connects to a VM, retries a completion, or records credentials.
func TestAssistantLiveEvaluation(t *testing.T) {
	if os.Getenv("ASSISTANT_EVAL_ENABLE") != "1" {
		t.Skip("live evaluation requires explicit opt-in")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" || !strings.HasPrefix(key, "sk-or-") {
		t.Fatal("a real key must be supplied privately through the environment")
	}
	maxCalls, err := strconv.Atoi(os.Getenv("ASSISTANT_EVAL_MAX_CALLS"))
	if err != nil || maxCalls < 1 || maxCalls > 12 {
		t.Fatal("ASSISTANT_EVAL_MAX_CALLS must be an explicit ceiling from 1 to 12")
	}
	selection := strings.Split(os.Getenv("ASSISTANT_EVAL_CASES"), ",")
	if len(selection) == 0 || selection[0] == "" || len(selection) > maxCalls {
		t.Fatal("select explicit case IDs within the completion ceiling")
	}
	output := os.Getenv("ASSISTANT_EVAL_OUTPUT")
	if output == "" || !filepath.IsAbs(output) {
		t.Fatal("ASSISTANT_EVAL_OUTPUT must be an absolute path outside Git")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("output must be a new artifact, never overwrite a previous evaluation")
	}
	corpusBytes, err := os.ReadFile("testdata/live_eval.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus liveEvalCorpus
	if json.Unmarshal(corpusBytes, &corpus) != nil {
		t.Fatal("invalid evaluation corpus")
	}
	byID := map[string]liveEvalCase{}
	for _, entry := range corpus.Cases {
		byID[entry.ID] = entry
	}
	seen := map[string]bool{}
	for _, id := range selection {
		if _, ok := byID[id]; !ok || seen[id] {
			t.Fatal("unknown or repeated evaluation case")
		}
		seen[id] = true
	}
	model := os.Getenv("ASSISTANT_EVAL_MODEL")
	if model == "" {
		model = preferredAssistantModelID
	}
	report := struct {
		CorpusHash string           `json:"corpus_sha256"`
		At         time.Time        `json:"run_at"`
		FrozenNow  time.Time        `json:"frozen_now"`
		MaxCalls   int              `json:"max_calls"`
		Records    []liveEvalRecord `json:"records"`
	}{CorpusHash: liveEvalHash(corpusBytes), At: time.Now().UTC(), FrozenNow: corpus.Now, MaxCalls: maxCalls}
	transport := &liveEvalTransport{base: http.DefaultTransport, maxCalls: maxCalls}
	for _, id := range selection {
		entry := byID[id]
		request := buildInterpreterRequest(corpus.Now, corpus.Categories, entry.History, entry.Message, entry.Draft)
		if os.Getenv("ASSISTANT_EVAL_BASELINE") == "1" {
			request.SystemInstruction.Parts[0].Text = baselineFinancialInterpreterPrompt(corpus.Now, strings.Join(corpus.Categories, ", "), nil, entry.Draft)
			request.Contents = buildConversationFromMessages(compactFinancialHistory(entry.History), entry.Message)
		}
		prompt := request.SystemInstruction.Parts[0].Text
		schema, _ := json.Marshal(assistantResponseSchema(corpus.Categories))
		record := liveEvalRecord{ID: id, Model: model, PromptHash: liveEvalHash([]byte(prompt)), SchemaHash: liveEvalHash(schema)}
		transport.record = &record
		ctx, cancel := context.WithTimeout(withAssistantMessageBudget(context.Background()), 55*time.Second)
		started := time.Now()
		generator, generateErr := newAssistantGeneratorForModel(ctx, model)
		if generateErr == nil {
			client := generator.(*openRouterClient)
			if len(client.models) == 0 || client.models[0].ID != model {
				cancel()
				t.Fatal("requested evaluation model is absent from the free catalog; no fallback allowed")
			}
			if os.Getenv("ASSISTANT_EVAL_FORMAT") == "json_object" {
				for i := range client.models {
					client.models[i].SupportsStructuredOutputs = false
				}
			}
			client.httpClient = &http.Client{Transport: transport, Timeout: 18 * time.Second}
			var result string
			result, generateErr = generator.generateText(request)
			if generateErr == nil {
				intent, parseErr := parseFinancialIntent(cleanJSONText(result))
				if parseErr != nil {
					record.Reason = "invalid_intent"
				} else {
					record.Actual = liveEvalPlan(intent)
					record.Reason = liveEvalMismatch(entry, intent, corpus.Now)
					record.Passed = record.Reason == ""
				}
			}
			if client.usedModel != "" {
				record.Model = client.usedModel
			}
		}
		cancel()
		record.DurationMillis = time.Since(started).Milliseconds()
		if generateErr != nil {
			code, _, known := openRouterErrorInfo(generateErr)
			if !known {
				code = "evaluation_provider_error"
			}
			record.Reason = code
		}
		report.Records = append(report.Records, record)
		t.Logf("case=%s pass=%t attempts=%d reason=%s", id, record.Passed, record.Attempts, record.Reason)
		if !record.Passed {
			t.Fail()
		}
		// An upstream failure is evidence, not permission to spend more calls
		// retrying the same provider or advancing through a depleted account.
		if generateErr != nil {
			break
		}
	}
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		t.Fatal("cannot prepare artifact directory")
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal("cannot encode evaluation artifact")
	}
	if err := os.WriteFile(output, data, 0600); err != nil {
		t.Fatal("cannot save evaluation artifact")
	}
	if len(report.Records) != len(selection) {
		t.Errorf("evaluation incomplete: %d/%d cases", len(report.Records), len(selection))
	}
}

func liveEvalPlan(intent financialIntent) map[string]any {
	plan := map[string]any{"intent": intent.Intent}
	for _, key := range []string{"query", "description", "amount", "category_name", "payment_source", "month", "year", "day", "type", "installments", "provided_fields", "new_expense"} {
		if value, ok := intent.provided[key]; ok {
			plan[key] = value
		}
	}
	return plan
}

func liveEvalMismatch(entry liveEvalCase, intent financialIntent, now time.Time) string {
	expected := entry.Expect
	switch expected.Kind {
	case "cancel":
		if intent.Intent != "cancel_expense" {
			return "cancellation_not_understood"
		}
	case "balance":
		// The executor prioritizes query whenever present. A correct-looking
		// monthly_summary with a stale expense query would still answer expenses.
		if _, present := intent.provided["query"]; present {
			return "balance_has_query_override"
		}
		if len(expected.Months) == 1 && intent.Intent == "monthly_summary" && intent.Month == expected.Months[0] && intent.Year == expected.Year {
			return ""
		}
		return "balance_not_understood"
	case "query":
		if intent.Intent != "financial_query" {
			return "query_not_understood"
		}
		query, err := parseFinancialReadQuery(intent.provided["query"])
		if err != nil {
			return "query_not_executable"
		}
		if query.Operation != expected.Operation || query.GroupBy != expected.GroupBy {
			return "wrong_query_operation"
		}
		data, _ := json.Marshal(query)
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		return liveEvalQueryScope(raw, expected)
	case "create_partial", "create_recovered":
		if intent.Intent != "create_expense" {
			return "purchase_not_understood"
		}
		args := expenseArgsFromIntent(intent, entry.Draft)
		if !strings.Contains(strings.ToLower(stringValue(args["description"])), "chocolate") || floatValue(args["amount"]) != 15 || stringValue(args["category_name"]) != "Mercado" || normalizeMoneySource(stringValue(args["payment_source"])) != "salario" {
			return "purchase_fields_lost"
		}
		if expected.Kind == "create_partial" {
			for _, key := range []string{"day", "month", "year", "type"} {
				if !emptyDraftPatchValue(args[key]) {
					return "purchase_fields_invented"
				}
			}
		} else if intValue(args["day"]) != now.Day() || intValue(args["month"]) != int(now.Month()) || intValue(args["year"]) != now.Year() || normalizeExpenseTypeForCreate(stringValue(args["type"])) != "Única" {
			return "purchase_followup_lost"
		}
	default:
		return "unknown_expectation"
	}
	return ""
}

func liveEvalQueryScope(raw map[string]any, expected liveEvalExpectation) string {
	if stringValue(raw["category_name"]) != expected.Category || stringValue(raw["payment_source"]) != "" {
		return "residual_or_missing_filter"
	}
	var periods []financialReadPeriod
	data, _ := json.Marshal(raw["periods"])
	if json.Unmarshal(data, &periods) != nil || len(periods) != 1 || periods[0].Year != expected.Year {
		return "wrong_query_year"
	}
	actual := periods[0].Months
	if len(expected.Months) == 0 {
		if len(actual) == 0 {
			return ""
		}
		if len(actual) != 12 {
			return "wrong_query_months"
		}
		for i := 1; i <= 12; i++ {
			if !containsYear(actual, i) {
				return "wrong_query_months"
			}
		}
		return ""
	}
	sort.Ints(actual)
	want := append([]int(nil), expected.Months...)
	sort.Ints(want)
	if !reflect.DeepEqual(actual, want) {
		return "wrong_query_months"
	}
	return ""
}

func TestAssistantEvalCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/live_eval.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus liveEvalCorpus
	if json.Unmarshal(data, &corpus) != nil || len(corpus.Cases) < 18 || corpus.Now.IsZero() {
		t.Fatal("invalid frozen corpus")
	}
	seen := map[string]bool{}
	for _, entry := range corpus.Cases {
		if entry.ID == "" || entry.Message == "" || seen[entry.ID] {
			t.Fatal("empty or duplicate corpus case")
		}
		seen[entry.ID] = true
	}
	if reason := liveEvalMismatch(liveEvalCase{Expect: liveEvalExpectation{Kind: "balance", Year: 2026, Months: []int{10}}}, financialIntent{Intent: "monthly_summary", Month: 10, Year: 2026}, corpus.Now); reason != "" {
		t.Fatal(reason)
	}
	if reason := liveEvalMismatch(liveEvalCase{Expect: liveEvalExpectation{Kind: "balance", Year: 2026, Months: []int{10}}}, financialIntent{Intent: "financial_query", provided: map[string]any{"query": map[string]any{"operation": "sum", "group_by": "month"}}}, corpus.Now); reason == "" {
		t.Fatal("expenses incorrectly accepted as balance")
	}
	for _, operation := range []string{"sum", "balance"} {
		intent := financialIntent{Intent: "monthly_summary", Month: 10, Year: 2026, provided: map[string]any{"query": map[string]any{"operation": operation, "group_by": "month"}}}
		if liveEvalMismatch(liveEvalCase{Expect: liveEvalExpectation{Kind: "balance", Year: 2026, Months: []int{10}}}, intent, corpus.Now) == "" {
			t.Fatal("shadow query incorrectly qualified as an executable balance")
		}
	}
}
