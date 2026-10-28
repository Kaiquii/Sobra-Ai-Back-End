package assistant

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const openRouterRequestBudget = 45 * time.Second
const openRouterMaxAttempts = 3

// OpenRouterError deliberately contains no upstream message, request, or token.
// Providers can include submitted content in errors; never return or log it.
type OpenRouterError struct {
	Code              string
	RetryAfterSeconds int
	StatusCode        int
	Global            bool
}

func (e OpenRouterError) Error() string { return e.Code }

func openRouterErrorInfo(err error) (string, int, bool) {
	var upstream OpenRouterError
	if errors.As(err, &upstream) {
		return upstream.Code, upstream.RetryAfterSeconds, true
	}
	return "", 0, false
}

type openRouterClient struct {
	ctx        context.Context
	apiKey     string
	account    string
	models     []AssistantModel
	httpClient *http.Client
	usedModel  string
	automatic  bool
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterRequest struct {
	Model               string              `json:"model"`
	Messages            []openRouterMessage `json:"messages"`
	Temperature         *float64            `json:"temperature,omitempty"`
	MaxTokens           int                 `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty"`
	ResponseFormat      map[string]string   `json:"response_format,omitempty"`
	Provider            map[string]any      `json:"provider"`
}

type openRouterResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Code     int            `json:"code"`
		Message  string         `json:"message"`
		Metadata map[string]any `json:"metadata"`
	} `json:"error"`
}

var openRouterCooldowns = struct {
	sync.Mutex
	until map[string]time.Time
}{until: make(map[string]time.Time)}

func newAssistantGeneratorForModel(ctx context.Context, modelID string) (textGenerator, error) {
	modelID = strings.TrimSpace(modelID)
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		return nil, OpenRouterError{Code: "assistant_not_configured", StatusCode: http.StatusServiceUnavailable}
	}
	models, err := listFreeAssistantModels(ctx)
	if err != nil {
		return nil, err
	}
	// Empty means server-owned automatic routing. Explicit legacy selections
	// are prioritized only while present in the verified free catalog; the HTTP
	// handler validates request-supplied IDs before reaching this constructor.
	models = orderedAssistantModels(models, modelID)
	hash := sha256.Sum256([]byte(key))
	return &openRouterClient{
		ctx: ctx, apiKey: key, account: hex.EncodeToString(hash[:]), models: models,
		httpClient: &http.Client{Timeout: 18 * time.Second}, automatic: modelID == "",
	}, nil
}

func (client *openRouterClient) modelUsed() string { return client.usedModel }

func orderedAssistantModels(models []AssistantModel, selected string) []AssistantModel {
	ordered := make([]AssistantModel, 0, len(models))
	seen := make(map[string]bool, len(models))
	appendModel := func(model AssistantModel) {
		if !seen[model.ID] {
			ordered = append(ordered, model)
			seen[model.ID] = true
		}
	}
	for _, model := range models {
		if model.ID == selected {
			appendModel(model)
		}
	}
	for _, model := range models {
		if model.Recommended {
			appendModel(model)
		}
	}
	for _, model := range models {
		appendModel(model)
	}
	return ordered
}

func (client *openRouterClient) generateText(request GeminiRequest) (string, error) {
	ctx, cancel := context.WithTimeout(client.ctx, openRouterRequestBudget)
	defer cancel()
	if retry := routerCooldownRemaining(client.account); retry > 0 {
		return "", OpenRouterError{Code: "assistant_quota_exceeded", StatusCode: http.StatusTooManyRequests, RetryAfterSeconds: retry, Global: true}
	}
	lastError := OpenRouterError{Code: "assistant_models_unavailable", StatusCode: http.StatusServiceUnavailable, RetryAfterSeconds: 30}
	attempts := 0
	models := client.models
	if client.automatic {
		preferred := openRouterAutoRouting.preferred(client.account, models, time.Now())
		if preferred != "" && routerCooldownRemaining(client.account+":"+preferred) > 0 {
			openRouterAutoRouting.forget(client.account, preferred)
			preferred = ""
		}
		models = orderedAssistantModels(models, preferred)
	}
	for _, model := range models {
		if attempts >= openRouterMaxAttempts {
			break
		}
		if ctx.Err() != nil {
			return "", OpenRouterError{Code: "assistant_request_cancelled", StatusCode: http.StatusRequestTimeout}
		}
		if retry := routerCooldownRemaining(client.account); retry > 0 {
			return "", OpenRouterError{Code: "assistant_quota_exceeded", StatusCode: http.StatusTooManyRequests, RetryAfterSeconds: retry, Global: true}
		}
		cooldownKey := client.account + ":" + model.ID
		if retry := routerCooldownRemaining(cooldownKey); retry > 0 {
			if retry < lastError.RetryAfterSeconds {
				lastError.RetryAfterSeconds = retry
			}
			continue
		}
		text, attempted, err := client.generateWithModel(ctx, model, request)
		if attempted {
			attempts++
		}
		if err == nil {
			if client.automatic {
				openRouterAutoRouting.remember(client.account, client.usedModel, time.Now())
			}
			return text, nil
		}
		var typed OpenRouterError
		if !errors.As(err, &typed) {
			return "", OpenRouterError{Code: "assistant_unavailable", StatusCode: http.StatusServiceUnavailable}
		}
		lastError = typed
		if typed.Global {
			if typed.RetryAfterSeconds > 0 {
				setRouterCooldown(client.account, typed.RetryAfterSeconds)
			}
			return "", typed
		}
		if typed.Code == "assistant_request_cancelled" || typed.Code == "assistant_invalid_request" ||
			typed.Code == "assistant_user_limit" || typed.Code == "assistant_configuration_error" {
			return "", typed
		}
		openRouterAutoRouting.forget(client.account, model.ID)
		if attempted {
			setRouterCooldown(cooldownKey, max(typed.RetryAfterSeconds, 15))
		}
	}
	return "", lastError
}

func (client *openRouterClient) generateWithModel(ctx context.Context, model AssistantModel, req GeminiRequest) (string, bool, error) {
	payload, err := routerPayload(model, req)
	if err != nil {
		return "", false, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", false, OpenRouterError{Code: "assistant_invalid_request", StatusCode: http.StatusBadRequest}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterBaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", false, OpenRouterError{Code: "assistant_invalid_request", StatusCode: http.StatusBadRequest}
	}
	request.Header.Set("Authorization", "Bearer "+client.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-OpenRouter-Title", "SobraAi")
	if ctx.Err() != nil {
		return "", false, OpenRouterError{Code: "assistant_request_cancelled", StatusCode: http.StatusRequestTimeout}
	}
	// Another in-flight request can discover an account-wide limit after this
	// generation started. Recheck at the actual attempt boundary so its fallback
	// neither spends another user's allowance nor sends another provider call.
	if retry := routerCooldownRemaining(client.account); retry > 0 {
		return "", false, OpenRouterError{Code: "assistant_quota_exceeded", StatusCode: http.StatusTooManyRequests, RetryAfterSeconds: retry, Global: true}
	}
	if err := reserveAssistantCall(ctx); err != nil {
		return "", false, err
	}
	start := time.Now()
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", true, OpenRouterError{Code: "assistant_request_cancelled", StatusCode: http.StatusRequestTimeout}
		}
		return "", true, OpenRouterError{Code: "assistant_unavailable", StatusCode: http.StatusServiceUnavailable, RetryAfterSeconds: 15}
	}
	defer response.Body.Close()
	var result openRouterResponse
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result)
	log.Printf("OpenRouter stage=%s model=%s status=%d duration=%s", safeRouterStage(req.DebugStage), model.ID, response.StatusCode, time.Since(start).Round(time.Millisecond))
	if response.StatusCode >= 400 || result.Error != nil {
		return "", true, classifyRouterError(response, result)
	}
	if decodeErr != nil || len(result.Choices) == 0 || result.Choices[0].FinishReason == "length" {
		return "", true, OpenRouterError{Code: "assistant_invalid_response", StatusCode: http.StatusBadGateway, RetryAfterSeconds: 15}
	}
	text := strings.TrimSpace(result.Choices[0].Message.Content)
	if text == "" || !client.responseModelAllowed(model.ID, result.Model) {
		return "", true, OpenRouterError{Code: "assistant_invalid_response", StatusCode: http.StatusBadGateway, RetryAfterSeconds: 15}
	}
	if req.DebugStage == "assistant_intent" {
		// JSON parsing happens before success so even models without native JSON
		// formatting can fall back safely without executing an invalid intent.
		intent, err := parseFinancialIntent(cleanJSONText(text))
		if err != nil || strings.TrimSpace(intent.Intent) == "" {
			return "", true, OpenRouterError{Code: "assistant_invalid_response", StatusCode: http.StatusBadGateway, RetryAfterSeconds: 15}
		}
	}
	client.usedModel = model.ID
	if result.Model != "" {
		client.usedModel = result.Model
	}
	return text, true, nil
}

func (client *openRouterClient) responseModelAllowed(requested, actual string) bool {
	if actual == "" || actual == requested {
		return true
	}
	if requested != "openrouter/free" {
		return false
	}
	for _, model := range client.models {
		if model.ID == actual {
			return true
		}
	}
	return false
}

func routerPayload(model AssistantModel, req GeminiRequest) (openRouterRequest, error) {
	payload := openRouterRequest{
		Model: model.ID, Messages: routerMessages(req),
		Provider: map[string]any{
			"allow_fallbacks": true, "require_parameters": true, "data_collection": "deny",
			"max_price": map[string]int{"prompt": 0, "completion": 0, "request": 0},
		},
	}
	if model.SupportsTemperature {
		temperature := routerTemperature(req.GenerationConfig)
		payload.Temperature = &temperature
	}
	if req.DebugStage == "assistant_intent" && model.SupportsResponseFormat {
		payload.ResponseFormat = map[string]string{"type": "json_object"}
	}
	if req.DebugStage == "assistant_intent" && !model.SupportsResponseFormat {
		const instruction = "Responda exclusivamente com um objeto JSON valido com a intencao e os campos pedidos, sem cercas de codigo nem texto fora do objeto. A string reply pode conter Markdown simples, com quebras de linha escapadas como \\n."
		if len(payload.Messages) > 0 && payload.Messages[0].Role == "system" {
			payload.Messages[0].Content += "\n" + instruction
		} else {
			payload.Messages = append([]openRouterMessage{{Role: "system", Content: instruction}}, payload.Messages...)
		}
	}
	// Bytes are a conservative token bound, including multilingual input. Keep
	// all system instructions and the current message; discard only older turns.
	// A model too small for those mandatory messages safely falls back instead.
	if model.ContextLength <= 512 {
		return openRouterRequest{}, OpenRouterError{Code: "assistant_model_context_exceeded", StatusCode: http.StatusRequestEntityTooLarge}
	}
	outputBudget := min(2048, (model.ContextLength-256)/4)
	inputBudget := model.ContextLength - outputBudget - 256
	messageSize := func(messages []openRouterMessage) int {
		total := 0
		for _, message := range messages {
			total += len(message.Content) + 16
		}
		return total
	}
	for messageSize(payload.Messages) > inputBudget {
		oldest := 0
		for oldest < len(payload.Messages) && payload.Messages[oldest].Role == "system" {
			oldest++
		}
		if oldest >= len(payload.Messages)-1 {
			return openRouterRequest{}, OpenRouterError{Code: "assistant_model_context_exceeded", StatusCode: http.StatusRequestEntityTooLarge}
		}
		payload.Messages = append(payload.Messages[:oldest], payload.Messages[oldest+1:]...)
	}
	if model.SupportsMaxTokens {
		payload.MaxTokens = outputBudget
	} else if model.SupportsMaxCompletionTokens {
		payload.MaxCompletionTokens = outputBudget
	}
	return payload, nil
}

func classifyRouterError(response *http.Response, result openRouterResponse) OpenRouterError {
	status := response.StatusCode
	if result.Error != nil && result.Error.Code >= 400 {
		status = result.Error.Code
	}
	retry := routerRetryAfter(response.Header.Get("Retry-After"))
	switch status {
	case http.StatusPaymentRequired:
		return OpenRouterError{Code: "assistant_quota_exceeded", StatusCode: status, RetryAfterSeconds: max(retry, 60), Global: true}
	case http.StatusUnauthorized, http.StatusForbidden:
		return OpenRouterError{Code: "assistant_configuration_error", StatusCode: status, RetryAfterSeconds: max(retry, 30), Global: true}
	case http.StatusTooManyRequests:
		global := response.Header.Get("X-RateLimit-Limit") != "" || response.Header.Get("X-RateLimit-Reset") != ""
		if result.Error != nil {
			metadata := result.Error.Metadata
			provider, _ := metadata["provider_name"].(string)
			source, _ := metadata["limit_source"].(string)
			typeName, _ := metadata["error_type"].(string)
			message := strings.ToLower(result.Error.Message)
			global = global || strings.HasPrefix(source, "openrouter") ||
				(provider == "" && typeName == "rate_limit_exceeded") ||
				strings.Contains(message, "per-day") || strings.Contains(message, "per day") ||
				strings.Contains(message, "daily limit") || strings.Contains(message, "account limit")
		}
		if global {
			retry = max(retry, routerRateLimitReset(response.Header.Get("X-RateLimit-Reset")))
		}
		return OpenRouterError{Code: "assistant_quota_exceeded", StatusCode: status, RetryAfterSeconds: max(retry, 30), Global: global}
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return OpenRouterError{Code: "assistant_invalid_request", StatusCode: status}
	default:
		return OpenRouterError{Code: "assistant_models_unavailable", StatusCode: status, RetryAfterSeconds: max(retry, 15)}
	}
}

func routerMessages(req GeminiRequest) []openRouterMessage {
	result := make([]openRouterMessage, 0, len(req.Contents)+1)
	if req.SystemInstruction != nil {
		result = append(result, openRouterMessage{Role: "system", Content: routerContent(*req.SystemInstruction)})
	}
	for _, content := range req.Contents {
		role := "user"
		if content.Role == "model" || content.Role == "assistant" {
			role = "assistant"
		}
		result = append(result, openRouterMessage{Role: role, Content: routerContent(content)})
	}
	return result
}

func routerContent(content GeminiContent) string {
	parts := make([]string, 0, len(content.Parts))
	for _, part := range content.Parts {
		if strings.TrimSpace(part.Text) != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func routerTemperature(config map[string]any) float64 {
	switch value := config["temperature"].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return 0.2
	}
}

func safeRouterStage(stage string) string {
	switch stage {
	case "assistant_intent", "assistant_chat", "assistant_summarize_tool":
		return stage
	default:
		return "assistant"
	}
}

func routerRetryAfter(value string) int {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		if date, dateErr := http.ParseTime(value); dateErr == nil {
			seconds = int(time.Until(date).Seconds()) + 1
		}
	}
	return min(max(seconds, 0), 86400)
}

func routerRateLimitReset(value string) int {
	unix, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || unix <= 0 {
		return routerRetryAfter(value)
	}
	// Gateways expose reset instants in Unix seconds or milliseconds.
	if unix > 1e12 {
		unix /= 1000
	}
	seconds := int(time.Until(time.Unix(unix, 0)).Seconds()) + 1
	return min(max(seconds, 0), 86400)
}

func routerCooldownRemaining(key string) int {
	openRouterCooldowns.Lock()
	defer openRouterCooldowns.Unlock()
	until := openRouterCooldowns.until[key]
	if !time.Now().Before(until) {
		delete(openRouterCooldowns.until, key)
		return 0
	}
	return int(time.Until(until).Seconds()) + 1
}

func setRouterCooldown(key string, seconds int) {
	openRouterCooldowns.Lock()
	defer openRouterCooldowns.Unlock()
	until := time.Now().Add(time.Duration(min(seconds, 86400)) * time.Second)
	if until.After(openRouterCooldowns.until[key]) {
		openRouterCooldowns.until[key] = until
	}
}
