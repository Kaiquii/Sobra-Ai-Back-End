package assistant

import (
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const openRouterBaseURL = "https://openrouter.ai/api/v1"
const modelCatalogTTL = 5 * time.Minute
const modelCatalogFailureTTL = 5 * time.Second

const preferredAssistantModelID = "dots-studio/dots-3-note-preview:free"

// AssistantModel is a server-approved, currently zero-priced text model.
type AssistantModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Recommended bool   `json:"recommended"`

	SupportsResponseFormat      bool `json:"-"`
	SupportsTemperature         bool `json:"-"`
	SupportsMaxTokens           bool `json:"-"`
	SupportsMaxCompletionTokens bool `json:"-"`
	ContextLength               int  `json:"-"`
}

type catalogModel struct {
	ID                  string                     `json:"id"`
	Name                string                     `json:"name"`
	Description         string                     `json:"description"`
	ContextLength       int                        `json:"context_length"`
	Pricing             map[string]json.RawMessage `json:"pricing"`
	SupportedParameters []string                   `json:"supported_parameters"`
	Architecture        struct {
		Tokenizer        string   `json:"tokenizer"`
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
}

var assistantModelCatalog = struct {
	sync.Mutex
	expiresAt time.Time
	models    []AssistantModel
	lastError error
	retryAt   time.Time
	fetching  chan struct{}
}{}

func listFreeAssistantModels(ctx context.Context) ([]AssistantModel, error) {
	for {
		if ctx.Err() != nil {
			return nil, catalogUnavailable()
		}
		assistantModelCatalog.Lock()
		now := time.Now()
		if now.Before(assistantModelCatalog.expiresAt) && len(assistantModelCatalog.models) != 0 {
			models := append([]AssistantModel(nil), assistantModelCatalog.models...)
			assistantModelCatalog.Unlock()
			return models, nil
		}
		if assistantModelCatalog.lastError != nil && now.Before(assistantModelCatalog.retryAt) {
			err := assistantModelCatalog.lastError
			assistantModelCatalog.Unlock()
			return nil, err
		}
		ready := assistantModelCatalog.fetching
		if ready == nil {
			ready = make(chan struct{})
			assistantModelCatalog.fetching = ready
			go refreshAssistantModelCatalog(ready)
		}
		assistantModelCatalog.Unlock()
		select {
		case <-ctx.Done():
			return nil, catalogUnavailable()
		case <-ready:
		}
	}
}

// Refreshes are shared and bounded independently of any one caller. A cancelled
// request stops waiting immediately without cancelling another user's refresh.
func refreshAssistantModelCatalog(ready chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	models, err := fetchFreeAssistantModels(ctx)
	assistantModelCatalog.Lock()
	defer assistantModelCatalog.Unlock()
	if err != nil {
		assistantModelCatalog.models = nil
		assistantModelCatalog.expiresAt = time.Time{}
		assistantModelCatalog.lastError = err
		assistantModelCatalog.retryAt = time.Now().Add(modelCatalogFailureTTL)
	} else {
		assistantModelCatalog.models = models
		assistantModelCatalog.expiresAt = time.Now().Add(modelCatalogTTL)
		assistantModelCatalog.lastError = nil
		assistantModelCatalog.retryAt = time.Time{}
	}
	assistantModelCatalog.fetching = nil
	close(ready)
}

func fetchFreeAssistantModels(ctx context.Context) ([]AssistantModel, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterBaseURL+"/models", nil)
	if err != nil {
		return nil, catalogUnavailable()
	}
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, catalogUnavailable()
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, catalogUnavailable()
	}
	var catalog struct {
		Data []catalogModel `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 12<<20)).Decode(&catalog); err != nil {
		return nil, catalogUnavailable()
	}
	models := make([]AssistantModel, 0)
	seen := make(map[string]bool)
	for _, entry := range catalog.Data {
		if !eligibleFreeModel(entry) || seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		name := cleanModelCatalogText(entry.Name, 160)
		if name == "" {
			name = entry.ID
		}
		description := cleanModelCatalogText(entry.Description, 500)
		if description == "" {
			description = "Gratuito. Conversa e consultas financeiras no SobraAí."
		}
		models = append(models, AssistantModel{
			ID: entry.ID, Name: name, Description: description,
			SupportsResponseFormat:      slices.Contains(entry.SupportedParameters, "response_format"),
			SupportsTemperature:         slices.Contains(entry.SupportedParameters, "temperature"),
			SupportsMaxTokens:           slices.Contains(entry.SupportedParameters, "max_tokens"),
			SupportsMaxCompletionTokens: slices.Contains(entry.SupportedParameters, "max_completion_tokens"),
			ContextLength:               max(entry.ContextLength, 0),
		})
	}
	if len(models) == 0 {
		return nil, catalogUnavailable()
	}
	sort.Slice(models, func(i, j int) bool {
		leftPreferred, rightPreferred := models[i].ID == preferredAssistantModelID, models[j].ID == preferredAssistantModelID
		if leftPreferred != rightPreferred {
			return leftPreferred
		}
		left, right := strings.ToLower(models[i].Name), strings.ToLower(models[j].Name)
		if left == right {
			return models[i].ID < models[j].ID
		}
		return left < right
	})
	models[0].Recommended = true
	return models, nil
}

func catalogUnavailable() error {
	return OpenRouterError{Code: "assistant_models_unavailable", StatusCode: http.StatusServiceUnavailable, RetryAfterSeconds: 30}
}

func eligibleFreeModel(entry catalogModel) bool {
	// Routers can list zero gateway fees while selecting paid underlying models.
	// The free router is the exception: it only chooses zero-priced models.
	opaqueRouter := entry.ID == "openrouter/auto" ||
		(strings.EqualFold(entry.Architecture.Tokenizer, "Router") && entry.ID != "openrouter/free")
	return !opaqueRouter && entry.ID != "" && len(entry.ID) <= 160 &&
		strings.TrimSpace(entry.ID) == entry.ID && strings.Contains(entry.ID, "/") &&
		!strings.ContainsFunc(entry.ID, unicode.IsSpace) && !strings.ContainsFunc(entry.ID, unicode.IsControl) &&
		!strings.ContainsAny(entry.ID, "?#\\") &&
		slices.Contains(entry.Architecture.InputModalities, "text") &&
		len(entry.Architecture.OutputModalities) == 1 && entry.Architecture.OutputModalities[0] == "text" &&
		freeModelPricing(entry.Pricing)
}

func cleanModelCatalogText(value string, maximum int) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
	characters := []rune(strings.Join(strings.Fields(value), " "))
	if len(characters) > maximum {
		characters = characters[:maximum]
	}
	return string(characters)
}

func freeModelPricing(pricing map[string]json.RawMessage) bool {
	if len(pricing) == 0 || !zeroPrice(pricing["prompt"]) || !zeroPrice(pricing["completion"]) {
		return false
	}
	for key, value := range pricing {
		if key == "overrides" {
			var overrides []map[string]json.RawMessage
			if err := json.Unmarshal(value, &overrides); err != nil {
				return false
			}
			for _, override := range overrides {
				for field, price := range override {
					if field == "min_prompt_tokens" || field == "max_prompt_tokens" {
						continue
					}
					if !zeroPrice(price) {
						return false
					}
				}
			}
			continue
		}
		if !zeroPrice(value) {
			return false
		}
	}
	return true
}

func zeroPrice(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	if len(value) > 0 && value[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return false
		}
	}
	price, ok := new(big.Rat).SetString(value)
	return ok && price.Sign() == 0
}
