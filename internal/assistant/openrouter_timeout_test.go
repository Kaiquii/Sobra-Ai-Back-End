package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenRouterBodyReadTimeoutIsReportedAsTimeout(t *testing.T) {
	const privateBody = "PRIVATE_TIMEOUT_BODY_384752"
	const privateModel = "PRIVATE_TIMEOUT_MODEL_48319"
	var calls atomic.Int32
	var handlers sync.WaitGroup
	release := make(chan struct{})
	var releaseOnce sync.Once
	ctx, cancel := context.WithCancel(withAssistantMessageBudget(context.Background()))
	setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls.Add(1)
		handlers.Add(1)
		defer handlers.Done()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"model":"`+privateModel+`","choices":[{"message":{"content":"`+privateBody)
		w.(http.Flusher).Flush()
		select {
		case <-request.Context().Done():
		case <-release:
		}
	})
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		handlers.Wait()
	})
	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousOutput) })
	generator, err := newAssistantGeneratorForModel(ctx, "test/first:free")
	if err != nil {
		t.Fatal(err)
	}
	client := generator.(*openRouterClient)
	client.httpClient.Timeout = 200 * time.Millisecond
	text, err := client.generateText(GeminiRequest{DebugStage: "assistant_intent", Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "Consulta ficticia"}}}}})
	var typed OpenRouterError
	if text != "" || !errors.As(err, &typed) || typed.Code != "assistant_request_timeout" || typed.StatusCode != http.StatusGatewayTimeout || typed.RetryAfterSeconds != 15 || typed.Global {
		t.Fatalf("HTTP200 body timeout was mistaken for an invalid model response: text=%q error=%+v", text, typed)
	}
	if calls.Load() != 1 || client.modelUsed() != "" {
		t.Fatalf("timeout retried a completion or published success: calls=%d model=%q", calls.Load(), client.modelUsed())
	}
	output := logs.String()
	if !strings.Contains(output, "request_interrupted stage=assistant_intent reason=request_timeout") || strings.Contains(output, "response_decode_failed") {
		t.Fatalf("timeout diagnostic was not a closed transport reason: %s", output)
	}
	for _, secret := range []string{privateBody, privateModel, "test-openrouter-secret", "Authorization", "Bearer", "context deadline exceeded", "Client.Timeout", "127.0.0.1"} {
		if strings.Contains(output, secret) {
			t.Fatalf("timeout diagnostic leaked raw transport/provider data: marker=%s", secret)
		}
	}
}

type timeoutTestReadNotifier struct {
	io.ReadCloser
	once   sync.Once
	onRead func()
}

func (body *timeoutTestReadNotifier) Read(buffer []byte) (int, error) {
	body.once.Do(body.onRead)
	return body.ReadCloser.Read(buffer)
}

type timeoutTestReadTransport struct {
	base   http.RoundTripper
	onRead func()
}

func (transport timeoutTestReadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err == nil {
		response.Body = &timeoutTestReadNotifier{ReadCloser: response.Body, onRead: transport.onRead}
	}
	return response, err
}

func TestOpenRouterTransportInterruptionsKeepTimeoutAndCancellationDistinct(t *testing.T) {
	for _, test := range []struct {
		name, code, reason string
		headers            bool
		deadline           bool
		cancelOnRead       bool
		messageBudget      bool
		status, retry      int
	}{
		{"before headers client timeout", "assistant_request_timeout", "request_timeout", false, false, false, true, http.StatusGatewayTimeout, 15},
		{"after headers request deadline", "assistant_request_timeout", "request_timeout", true, true, false, true, http.StatusGatewayTimeout, 15},
		{"after headers explicit cancellation", "assistant_request_cancelled", "request_cancelled", true, false, true, true, http.StatusRequestTimeout, 0},
		{"client timeout without message budget", "assistant_request_timeout", "request_timeout", true, false, false, false, http.StatusGatewayTimeout, 15},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			var handlers sync.WaitGroup
			release := make(chan struct{})
			var releaseOnce sync.Once
			ctx, cancel := context.WithCancel(context.Background())
			if test.messageBudget {
				ctx = withAssistantMessageBudget(ctx)
			}
			setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					writeTestCatalog(w)
					return
				}
				calls.Add(1)
				handlers.Add(1)
				defer handlers.Done()
				if test.headers {
					w.WriteHeader(http.StatusOK)
					_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"PRIVATE_INTERRUPTED_BODY_9317`)
					w.(http.Flusher).Flush()
				}
				select {
				case <-request.Context().Done():
				case <-release:
				}
			})
			t.Cleanup(func() {
				cancel()
				releaseOnce.Do(func() { close(release) })
				handlers.Wait()
			})
			var logs bytes.Buffer
			previousOutput := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previousOutput) })
			generator, err := newAssistantGeneratorForModel(ctx, "test/first:free")
			if err != nil {
				t.Fatal(err)
			}
			client := generator.(*openRouterClient)
			client.httpClient.Timeout = 200 * time.Millisecond
			if test.deadline {
				deadlineContext, deadlineCancel := context.WithTimeout(ctx, 200*time.Millisecond)
				t.Cleanup(deadlineCancel)
				client.ctx = deadlineContext
				client.httpClient.Timeout = time.Second
			}
			if test.cancelOnRead {
				client.httpClient.Transport = timeoutTestReadTransport{base: http.DefaultTransport, onRead: cancel}
			}
			text, err := client.generateText(GeminiRequest{DebugStage: "assistant_intent"})
			var typed OpenRouterError
			if text != "" || !errors.As(err, &typed) || typed.Code != test.code || typed.StatusCode != test.status || typed.RetryAfterSeconds != test.retry || typed.Global {
				t.Fatalf("transport interruption was misclassified: text=%q error=%+v", text, typed)
			}
			if calls.Load() != 1 || client.modelUsed() != "" {
				t.Fatalf("interruption retried even without message budget or published success: calls=%d model=%q", calls.Load(), client.modelUsed())
			}
			output := logs.String()
			if !strings.Contains(output, "request_interrupted stage=assistant_intent reason="+test.reason) {
				t.Fatalf("interruption did not use its closed diagnostic: %s", output)
			}
			for _, forbidden := range []string{"PRIVATE_INTERRUPTED_BODY", "test-openrouter-secret", "Bearer", "Authorization", "context deadline exceeded", "context canceled", "Client.Timeout", "127.0.0.1", "response_decode_failed"} {
				if strings.Contains(output, forbidden) {
					t.Fatalf("interruption log exposed raw data or wrong diagnosis: marker=%s", forbidden)
				}
			}
		})
	}
}

func TestOpenRouterSlowQuotaBodyPreservesKnownHTTPStatus(t *testing.T) {
	var calls atomic.Int32
	var handlers sync.WaitGroup
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(withAssistantMessageBudget(context.Background()))
	setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls.Add(1)
		handlers.Add(1)
		defer handlers.Done()
		w.Header().Set("X-RateLimit-Limit", "50")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"PRIVATE_QUOTA_BODY_3841`)
		w.(http.Flusher).Flush()
		select {
		case <-request.Context().Done():
		case <-release:
		}
	})
	t.Cleanup(func() { cancel(); close(release); handlers.Wait() })
	generator, err := newAssistantGeneratorForModel(ctx, "test/first:free")
	if err != nil {
		t.Fatal(err)
	}
	client := generator.(*openRouterClient)
	client.httpClient.Timeout = 200 * time.Millisecond
	text, err := client.generateText(GeminiRequest{DebugStage: "assistant_intent"})
	var typed OpenRouterError
	if text != "" || !errors.As(err, &typed) || typed.Code != "assistant_quota_exceeded" || typed.StatusCode != http.StatusTooManyRequests || typed.RetryAfterSeconds != 60 || !typed.Global || calls.Load() != 1 {
		t.Fatalf("known quota status was hidden by slow error-body decoding: text=%q calls=%d error=%+v", text, calls.Load(), typed)
	}
}

func TestOpenRouterTimeoutChangeKeepsGenuineJSONErrorsDistinct(t *testing.T) {
	var calls atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices": invalid JSON}`)
	})
	generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "test/first:free")
	if err != nil {
		t.Fatal(err)
	}
	text, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent"})
	var typed OpenRouterError
	if text != "" || !errors.As(err, &typed) || typed.Code != "assistant_invalid_response" || typed.StatusCode != http.StatusBadGateway || typed.RetryAfterSeconds != 15 || typed.Global || calls.Load() != 1 {
		t.Fatalf("genuine malformed JSON was misclassified as a timeout: text=%q calls=%d error=%+v", text, calls.Load(), typed)
	}
}

func TestOpenRouterConfiguredTimeoutAndFastSuccessPreserveSafety(t *testing.T) {
	var calls atomic.Int32
	setupRouterTest(t, func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			writeTestCatalog(w)
			return
		}
		calls.Add(1)
		var payload openRouterRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		prices, _ := payload.Provider["max_price"].(map[string]any)
		if payload.MaxTokens != 2048 || payload.Provider["data_collection"] != "deny" || payload.Provider["require_parameters"] != true || prices["prompt"] != float64(0) || prices["completion"] != float64(0) || prices["request"] != float64(0) {
			t.Error("timeout change weakened output, privacy or free-price controls")
		}
		writeRouterReply(w, `{"intent":"general","reply":"Ola"}`)
	})
	generator, err := newAssistantGeneratorForModel(withAssistantMessageBudget(context.Background()), "test/first:free")
	if err != nil {
		t.Fatal(err)
	}
	client := generator.(*openRouterClient)
	if client.httpClient.Timeout != 45*time.Second || calls.Load() != 0 {
		t.Fatalf("constructor timeout is not the stage budget or spent a completion: timeout=%s calls=%d", client.httpClient.Timeout, calls.Load())
	}
	text, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent"})
	if err != nil || !strings.Contains(text, "Ola") || calls.Load() != 1 || client.modelUsed() != "test/first:free" {
		t.Fatalf("fast valid response changed: text=%q calls=%d model=%q err=%v", text, calls.Load(), client.modelUsed(), err)
	}
	if _, err := generator.generateText(GeminiRequest{DebugStage: "assistant_intent"}); err == nil || calls.Load() != 1 {
		t.Fatalf("longer timeout changed the single-completion budget: calls=%d err=%v", calls.Load(), err)
	}
}
