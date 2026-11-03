//go:build assistant_eval

package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const fastEvalCorpusSize = 4

// This is a defensive tool ceiling, never authorization to make calls. Each
// ledger carries the user's explicitly approved total, including prior profiles.
const fastEvalMaximumAuthorizedTotal = 6
const fastEvalLatencyCeilingMillis int64 = 10000

type fastEvalLedger struct {
	AuthorizedTotal   int      `json:"authorized_total"`
	Used              int      `json:"used"`
	PendingCase       string   `json:"pending_case,omitempty"`
	CompletedCases    []string `json:"completed_cases,omitempty"`
	Halted            bool     `json:"halted,omitempty"`
	HaltReason        string   `json:"halt_reason,omitempty"`
	QualificationHash string   `json:"qualification_sha256,omitempty"`
}

type fastEvalReport struct {
	At                           time.Time      `json:"run_at"`
	FrozenNow                    time.Time      `json:"frozen_now"`
	CaseID                       string         `json:"case_id"`
	RequestedModel               string         `json:"requested_model"`
	CorpusHash                   string         `json:"corpus_sha256,omitempty"`
	ScoringHash                  string         `json:"scoring_sha256,omitempty"`
	PromptHash                   string         `json:"prompt_sha256,omitempty"`
	SnapshotHash                 string         `json:"snapshot_sha256,omitempty"`
	PromptBytes                  int            `json:"prompt_bytes"`
	SnapshotBytes                int            `json:"snapshot_bytes"`
	ResponseFormat               string         `json:"response_format,omitempty"`
	RequestedReasoning           *bool          `json:"requested_reasoning,omitempty"`
	MaxOutputTokens              int            `json:"max_output_tokens"`
	MaxLatencyMillis             int64          `json:"max_latency_ms"`
	DurationMillis               int64          `json:"duration_ms"`
	HTTPStatus                   int            `json:"http_status,omitempty"`
	FinishReason                 string         `json:"finish_reason,omitempty"`
	ReasoningTokens              *int64         `json:"reasoning_tokens,omitempty"`
	Wire                         liveEvalRecord `json:"wire"`
	Score                        fastEvalScore  `json:"score"`
	Passed                       bool           `json:"passed"`
	Reason                       string         `json:"reason,omitempty"`
	LedgerReservationPersisted   bool           `json:"ledger_reservation_persisted"`
	LedgerUsed                   *int           `json:"ledger_used,omitempty"`
	LedgerError                  string         `json:"ledger_error,omitempty"`
	ManualSemanticReviewRequired bool           `json:"manual_semantic_review_required"`
}

// Locks fail closed rather than waiting or guessing whether another process is
// still making its reserved request. A crash leaves a pending case for review.
func fastEvalUpdateLedger(path string, update func(*fastEvalLedger) error) error {
	lockPath := path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("ledger_locked_or_unavailable")
	}
	_ = lock.Close()
	defer os.Remove(lockPath)
	file, err := os.Open(path)
	if err != nil {
		return errors.New("ledger_unavailable")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 8192))
	decoder.DisallowUnknownFields()
	var ledger fastEvalLedger
	decodeErr := decoder.Decode(&ledger)
	var trailing any
	trailingErr := decoder.Decode(&trailing)
	_ = file.Close()
	if decodeErr != nil || trailingErr != io.EOF || ledger.AuthorizedTotal < 1 || ledger.AuthorizedTotal > fastEvalMaximumAuthorizedTotal || ledger.Used < 0 || ledger.Used > ledger.AuthorizedTotal {
		return errors.New("ledger_invalid")
	}
	if err := update(&ledger); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return errors.New("ledger_encoding_failed")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".assistant-fast-ledger-*.tmp")
	if err != nil {
		return errors.New("ledger_write_failed")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err = temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return errors.New("ledger_write_failed")
	}
	if err = temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errors.New("ledger_sync_failed")
	}
	if err = temporary.Close(); err != nil {
		return errors.New("ledger_write_failed")
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return errors.New("ledger_replace_failed")
	}
	return nil
}

func fastEvalReserve(path, caseID, qualificationHash string) (int, error) {
	used := 0
	err := fastEvalUpdateLedger(path, func(ledger *fastEvalLedger) error {
		if ledger.Halted || ledger.PendingCase != "" {
			return errors.New("ledger_halted_or_pending")
		}
		if qualificationHash == "" || (ledger.QualificationHash != "" && ledger.QualificationHash != qualificationHash) || (ledger.Used > 0 && ledger.QualificationHash == "") {
			return errors.New("qualification_changed_or_missing")
		}
		for _, completed := range ledger.CompletedCases {
			if completed == caseID {
				return errors.New("case_already_used")
			}
		}
		if ledger.Used >= ledger.AuthorizedTotal {
			return errors.New("completion_ceiling_reached")
		}
		ledger.Used++
		ledger.QualificationHash = qualificationHash
		ledger.PendingCase = caseID
		used = ledger.Used
		return nil
	})
	return used, err
}

func fastEvalFinish(path, caseID, reason string, passed bool) error {
	return fastEvalUpdateLedger(path, func(ledger *fastEvalLedger) error {
		if ledger.PendingCase != caseID {
			return errors.New("ledger_pending_case_mismatch")
		}
		ledger.PendingCase = ""
		ledger.CompletedCases = append(ledger.CompletedCases, caseID)
		if !passed {
			ledger.Halted, ledger.HaltReason = true, reason
		}
		return nil
	})
}

type fastEvalLedgerTransport struct {
	base              http.RoundTripper
	ledgerPath        string
	caseID            string
	report            *fastEvalReport
	reserved          atomic.Bool
	qualificationHash string
}

func (transport *fastEvalLedgerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "openrouter.ai" || request.Method != http.MethodPost || request.URL.Path != "/api/v1/chat/completions" {
		return nil, errors.New("fast_eval_destination_rejected")
	}
	if request.Context().Err() != nil {
		return nil, errors.New("fast_eval_context_ended_before_post")
	}
	if !transport.reserved.CompareAndSwap(false, true) {
		return nil, errors.New("fast_eval_single_post_limit")
	}
	used, err := fastEvalReserve(transport.ledgerPath, transport.caseID, transport.qualificationHash)
	if err != nil {
		transport.report.LedgerError = fastEvalClosedLedgerReason(err)
		return nil, err
	}
	transport.report.LedgerReservationPersisted, transport.report.LedgerUsed = true, &used
	response, err := transport.base.RoundTrip(request)
	if response != nil {
		transport.report.HTTPStatus = response.StatusCode
		// The shared capture transport has already bounded and buffered the
		// body. Inspect only fixed metadata; never persist an upstream error.
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		response.Body = io.NopCloser(bytes.NewReader(data))
		if readErr == nil {
			var metadata struct {
				Choices []struct {
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Usage *struct {
					CompletionDetails *struct {
						ReasoningTokens int64 `json:"reasoning_tokens"`
					} `json:"completion_tokens_details"`
				} `json:"usage"`
			}
			if json.Unmarshal(data, &metadata) == nil {
				if len(metadata.Choices) > 0 {
					switch metadata.Choices[0].FinishReason {
					case "stop", "length", "content_filter", "error", "tool_calls":
						transport.report.FinishReason = metadata.Choices[0].FinishReason
					default:
						transport.report.FinishReason = "unknown"
					}
				}
				if metadata.Usage != nil && metadata.Usage.CompletionDetails != nil {
					value := metadata.Usage.CompletionDetails.ReasoningTokens
					transport.report.ReasoningTokens = &value
				}
			}
		}
	}
	return response, err
}

func fastEvalClosedLedgerReason(err error) string {
	if err == nil {
		return ""
	}
	switch err.Error() {
	case "ledger_locked_or_unavailable", "ledger_unavailable", "ledger_invalid", "ledger_encoding_failed", "ledger_write_failed", "ledger_sync_failed", "ledger_replace_failed", "ledger_halted_or_pending", "case_already_used", "completion_ceiling_reached", "qualification_changed_or_missing", "ledger_pending_case_mismatch":
		return err.Error()
	default:
		return "ledger_failed"
	}
}

func fastEvalArtifactPath(value string) (string, error) {
	if !filepath.IsAbs(value) || filepath.Ext(value) != ".json" {
		return "", errors.New("artifact_path_invalid")
	}
	working, err := os.Getwd()
	if err != nil {
		return "", errors.New("artifact_workspace_unavailable")
	}
	root, err := filepath.EvalSymlinks(filepath.Join(working, "..", "..", ".artifacts"))
	if err != nil {
		return "", errors.New("artifact_workspace_unavailable")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(filepath.Clean(value)))
	if err != nil {
		return "", errors.New("artifact_parent_unavailable")
	}
	relative, err := filepath.Rel(root, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("artifact_path_outside_workspace")
	}
	return filepath.Join(parent, filepath.Base(value)), nil
}

// Opt-in, one case and one POST only. The human reviews each saved synthetic
// response before explicitly selecting the next case in a separate execution.
func TestAssistantFastEvaluation(t *testing.T) {
	if os.Getenv("ASSISTANT_FAST_EVAL_ENABLE") != "1" {
		t.Skip("fast evaluation requires explicit opt-in")
	}
	outputPath, err := fastEvalArtifactPath(os.Getenv("ASSISTANT_FAST_EVAL_OUTPUT"))
	if err != nil {
		t.Fatal("fast_eval_output_invalid")
	}
	output, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("fast_eval_output_must_be_new")
	}
	report := fastEvalReport{At: time.Now().UTC(), CaseID: os.Getenv("ASSISTANT_FAST_EVAL_CASE"), MaxLatencyMillis: fastEvalLatencyCeilingMillis, ManualSemanticReviewRequired: true}
	wire := liveEvalRecord{ID: report.CaseID}
	var reservation *fastEvalLedgerTransport
	defer func() {
		wire.DurationMillis, wire.Reason, wire.Passed = report.DurationMillis, report.Reason, report.Passed
		report.Wire = wire
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if encoder.Encode(report) != nil || output.Sync() != nil {
			report.Passed, report.Reason = false, "output_write_failed"
			t.Error("fast_eval_output_write_failed")
		}
		// An output failure must leave the durable reservation pending or
		// halted, never record a successful case that cannot be reviewed.
		if reservation != nil && report.LedgerReservationPersisted {
			if err := fastEvalFinish(reservation.ledgerPath, report.CaseID, report.Reason, report.Passed); err != nil {
				report.Passed, report.Reason = false, "ledger_finalize_failed"
				t.Error("fast_eval_ledger_finalize_failed")
				if _, seekErr := output.Seek(0, io.SeekStart); seekErr == nil && output.Truncate(0) == nil {
					_ = encoder.Encode(report)
					_ = output.Sync()
				}
			}
		}
		_ = output.Close()
	}()
	fail := func(reason string) { report.Reason = reason; t.Fatal("fast_eval_failed:" + reason) }
	if report.CaseID == "" {
		fail("case_required")
	}
	if raw := os.Getenv("ASSISTANT_FAST_EVAL_MAX_LATENCY_MS"); raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value < 1 || value > fastEvalLatencyCeilingMillis {
			fail("latency_ceiling_invalid")
		}
		report.MaxLatencyMillis = value
	}
	ledgerPath, err := fastEvalArtifactPath(os.Getenv("ASSISTANT_FAST_EVAL_LEDGER"))
	if err != nil || ledgerPath == outputPath {
		fail("ledger_path_invalid")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" || !strings.HasPrefix(key, "sk-or-") {
		fail("key_not_configured")
	}
	corpusBytes, err := os.ReadFile("testdata/fast_eval.json")
	if err != nil {
		fail("corpus_unavailable")
	}
	var corpus fastEvalCorpus
	if json.Unmarshal(corpusBytes, &corpus) != nil || corpus.Version != 1 || corpus.Now.IsZero() || len(corpus.Cases) != fastEvalCorpusSize {
		fail("corpus_invalid")
	}
	report.CorpusHash, report.FrozenNow = liveEvalHash(corpusBytes), corpus.Now
	scoringBytes, err := os.ReadFile("assistant_fast_eval_scoring_test.go")
	if err != nil {
		fail("scoring_source_unavailable")
	}
	report.ScoringHash = liveEvalHash(scoringBytes)
	var selected *fastEvalCase
	seen := map[string]bool{}
	allowedCases := map[string]bool{"car_exact": true, "car_paraphrase": true, "category_budget": true, "purchase_partial": true}
	for i := range corpus.Cases {
		entry := &corpus.Cases[i]
		if !allowedCases[entry.ID] || seen[entry.ID] {
			fail("corpus_invalid")
		}
		seen[entry.ID] = true
		if entry.ID == report.CaseID {
			selected = entry
		}
	}
	if selected == nil {
		fail("case_unknown")
	}
	facts, err := compactAnalysisFactsForPrompt(corpus.Facts)
	if err != nil {
		fail("snapshot_invalid")
	}
	request, err := interpreterRequestWithFacts(corpus.Now, corpus.Categories, selected.History, selected.Message, facts, selected.Draft)
	if err != nil {
		fail("request_invalid")
	}
	prompt := request.SystemInstruction.Parts[0].Text
	report.PromptHash, report.SnapshotHash = liveEvalHash([]byte(prompt)), liveEvalHash([]byte(analysisFactsJSON(facts)))
	report.PromptBytes, report.SnapshotBytes = len(prompt), len(analysisFactsJSON(facts))
	model := strings.TrimSpace(os.Getenv("ASSISTANT_FAST_EVAL_MODEL"))
	if model == "" {
		model = preferredAssistantModelID
	}
	wire.PromptHash = report.PromptHash
	schema, _ := json.Marshal(assistantResponseSchema(corpus.Categories))
	wire.SchemaHash = liveEvalHash(schema)
	ctx, cancel := context.WithTimeout(withAssistantMessageBudget(context.Background()), 55*time.Second)
	defer cancel()
	started := time.Now()
	generator, err := newAssistantGeneratorForModel(ctx, model)
	if err != nil {
		report.DurationMillis = time.Since(started).Milliseconds()
		fail("catalog_or_configuration_failed")
	}
	client := generator.(*openRouterClient)
	if len(client.models) == 0 || client.models[0].ID != model || client.httpClient.Timeout != openRouterRequestBudget {
		fail("production_client_mismatch")
	}
	report.RequestedModel, wire.Model = model, model
	payload, err := routerPayload(client.models[0], request)
	if err != nil {
		fail("production_payload_invalid")
	}
	if reasoning, present := payload.Reasoning["enabled"]; present {
		report.RequestedReasoning = &reasoning
		if reasoning {
			fail("reasoning_profile_mismatch")
		}
	}
	report.MaxOutputTokens = max(payload.MaxTokens, payload.MaxCompletionTokens)
	if report.MaxOutputTokens != assistantIntentMaxOutputTokens {
		fail("output_profile_mismatch")
	}
	prices, safePrices := payload.Provider["max_price"].(map[string]int)
	if !safePrices || len(prices) != 3 || prices["prompt"] != 0 || prices["completion"] != 0 || prices["request"] != 0 || payload.Provider["data_collection"] != "deny" || payload.Provider["require_parameters"] != true {
		fail("provider_profile_mismatch")
	}
	report.ResponseFormat, _ = payload.ResponseFormat["type"].(string)
	capture := &liveEvalTransport{base: http.DefaultTransport, maxCalls: 1, record: &wire}
	qualificationHash := liveEvalHash([]byte(report.CorpusHash + "\n" + report.ScoringHash + "\n" + model + "\n" + strconv.FormatInt(report.MaxLatencyMillis, 10)))
	reservation = &fastEvalLedgerTransport{base: capture, ledgerPath: ledgerPath, caseID: report.CaseID, report: &report, qualificationHash: qualificationHash}
	client.httpClient.Transport = reservation
	text, generationErr := client.generateText(request)
	if generationErr != nil {
		report.DurationMillis = time.Since(started).Milliseconds()
		if report.LedgerError != "" {
			fail(report.LedgerError)
		}
		if code, _, ok := openRouterErrorInfo(generationErr); ok {
			fail(code)
		}
		fail("generation_failed")
	}
	intent, err := parseInterpretedFinancialIntent(text, corpus.Now)
	if err != nil {
		report.DurationMillis = time.Since(started).Milliseconds()
		fail("intent_invalid")
	}
	wire.Actual = liveEvalPlan(intent)
	if calculations, exists := intent.provided["calculations"]; exists {
		wire.Actual["calculations"] = calculations
	}
	if client.usedModel != "" {
		wire.Model = client.usedModel
	}
	report.Score = scoreFastEval(*selected, intent, facts, corpus.Now)
	report.DurationMillis = time.Since(started).Milliseconds()
	wire.DurationMillis = report.DurationMillis
	if !report.Score.Passed {
		fail(report.Score.Reason)
	}
	if report.DurationMillis > report.MaxLatencyMillis {
		fail("latency_exceeded")
	}
	if wire.Attempts != 1 || !report.LedgerReservationPersisted {
		fail("completion_count_invalid")
	}
	report.Passed, wire.Passed = true, true
}

type fastEvalRoundTripperFunc func(*http.Request) (*http.Response, error)

func (function fastEvalRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func fastEvalTestLedger(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.json")
	if os.WriteFile(path, []byte(`{"authorized_total":4,"used":0}`), 0600) != nil {
		t.Fatal("cannot prepare synthetic ledger")
	}
	return path
}

func fastEvalReadTestLedger(t *testing.T, path string) fastEvalLedger {
	t.Helper()
	data, err := os.ReadFile(path)
	var ledger fastEvalLedger
	if err != nil || json.Unmarshal(data, &ledger) != nil {
		t.Fatal("cannot read synthetic ledger")
	}
	return ledger
}

func TestFastEvalLedgerPersistsReservationBeforePOST(t *testing.T) {
	path := fastEvalTestLedger(t)
	var calls atomic.Int32
	wire := liveEvalRecord{}
	report := fastEvalReport{}
	base := fastEvalRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		ledger := fastEvalReadTestLedger(t, path)
		if ledger.Used != 1 || ledger.PendingCase != "car_exact" {
			t.Fatal("POST reached the transport before its durable reservation")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responseDiagnosticEnvelope("test/synthetic:free", "stop", `{"intent":"general","reply":"Sintetico"}`)))}, nil
	})
	capture := &liveEvalTransport{base: base, maxCalls: 1, record: &wire}
	transport := &fastEvalLedgerTransport{base: capture, ledgerPath: path, caseID: "car_exact", qualificationHash: "frozen", report: &report}
	request, _ := http.NewRequest(http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", strings.NewReader(`{"model":"test/synthetic:free","messages":[]}`))
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal("synthetic transport failed")
	}
	_ = response.Body.Close()
	if calls.Load() != 1 || wire.Attempts != 1 || !report.LedgerReservationPersisted || report.LedgerUsed == nil || *report.LedgerUsed != 1 || wire.SyntheticResponse == "" {
		t.Fatal("reservation or shared capture lost the single-POST contract")
	}
	if _, err := transport.RoundTrip(request); err == nil || calls.Load() != 1 {
		t.Fatal("one execution sent a second POST")
	}
	otherWire := liveEvalRecord{}
	other := &fastEvalLedgerTransport{base: &liveEvalTransport{base: base, maxCalls: 1, record: &otherWire}, ledgerPath: path, caseID: "car_paraphrase", qualificationHash: "frozen", report: &fastEvalReport{}}
	if _, err := other.RoundTrip(request); err == nil || otherWire.Attempts != 0 || calls.Load() != 1 {
		t.Fatal("a pending case allowed another POST or counted a rejected reservation")
	}
}

func TestFastEvalLedgerKeepsFourCallCeilingAcrossExecutions(t *testing.T) {
	path := fastEvalTestLedger(t)
	for _, caseID := range []string{"car_exact", "car_paraphrase", "category_budget", "purchase_partial"} {
		if _, err := fastEvalReserve(path, caseID, "frozen"); err != nil {
			t.Fatal("valid reservation failed")
		}
		if fastEvalFinish(path, caseID, "", true) != nil {
			t.Fatal("valid finalization failed")
		}
	}
	if _, err := fastEvalReserve(path, "fifth", "frozen"); err == nil || fastEvalReadTestLedger(t, path).Used != 4 {
		t.Fatal("another execution bypassed the fixed four-POST ceiling")
	}
	if _, err := fastEvalReserve(path, "car_exact", "frozen"); err == nil {
		t.Fatal("a completed case can be repeated")
	}
}

func TestFastEvalLedgerCarriesApprovedTotalAcrossModelProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "continued-ledger.json")
	if os.WriteFile(path, []byte(`{"authorized_total":6,"used":3,"completed_cases":["profile-1/car_exact","profile-2/car_exact","profile-3/car_exact"],"qualification_sha256":"new-profile"}`), 0600) != nil {
		t.Fatal("cannot prepare carried-over authorization")
	}
	for _, caseID := range []string{"car_exact", "car_paraphrase", "category_budget"} {
		if _, err := fastEvalReserve(path, caseID, "new-profile"); err != nil {
			t.Fatal(err)
		}
		if err := fastEvalFinish(path, caseID, "", true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fastEvalReserve(path, "unapproved-seventh", "new-profile"); err == nil || fastEvalReadTestLedger(t, path).Used != 6 {
		t.Fatal("a model change reset or enlarged the approved total")
	}
	if os.WriteFile(path, []byte(`{"authorized_total":7,"used":0}`), 0600) != nil {
		t.Fatal("cannot prepare invalid tool ceiling")
	}
	if _, err := fastEvalReserve(path, "car_exact", "new-profile"); err == nil {
		t.Fatal("ledger accepted a total above the tool ceiling")
	}
}

func TestFastEvalLedgerStopsAfterFailureOrChangedExpectations(t *testing.T) {
	t.Run("first failure", func(t *testing.T) {
		path := fastEvalTestLedger(t)
		if _, err := fastEvalReserve(path, "car_exact", "frozen"); err != nil {
			t.Fatal("reserve failed")
		}
		if fastEvalFinish(path, "car_exact", "latency_exceeded", false) != nil {
			t.Fatal("finish failed")
		}
		if _, err := fastEvalReserve(path, "car_paraphrase", "frozen"); err == nil {
			t.Fatal("failure did not stop later POSTs")
		}
		ledger := fastEvalReadTestLedger(t, path)
		if !ledger.Halted || ledger.Used != 1 || ledger.HaltReason != "latency_exceeded" {
			t.Fatal("failure lost its persistent stop")
		}
	})
	t.Run("changed corpus or scorer", func(t *testing.T) {
		path := fastEvalTestLedger(t)
		if _, err := fastEvalReserve(path, "car_exact", "frozen"); err != nil {
			t.Fatal("reserve failed")
		}
		if fastEvalFinish(path, "car_exact", "", true) != nil {
			t.Fatal("finish failed")
		}
		if _, err := fastEvalReserve(path, "car_paraphrase", "changed"); err == nil || fastEvalReadTestLedger(t, path).Used != 1 {
			t.Fatal("expectations changed after the first captured result")
		}
	})
}

func TestFastEvalLedgerConcurrentReservationFailsClosed(t *testing.T) {
	path := fastEvalTestLedger(t)
	var group sync.WaitGroup
	var accepted atomic.Int32
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := fastEvalReserve(path, "car_exact", "frozen"); err == nil {
				accepted.Add(1)
			}
		}()
	}
	group.Wait()
	if accepted.Load() != 1 || fastEvalReadTestLedger(t, path).Used != 1 {
		t.Fatal("concurrent reservations overspent or reset the ledger")
	}
}
