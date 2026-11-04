package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The original no-collection policy is enforced on every completion by
// data_collection=deny. The additional ZDR discovery restriction remains the
// default; disabling it requires an explicit configuration choice. The router
// still enforces deny even when that optional prefilter is disabled.
func assistantRequiresZDR() (bool, error) {
	value := strings.TrimSpace(os.Getenv("OPENROUTER_REQUIRE_ZDR"))
	if value == "" {
		return true, nil
	}
	required, err := strconv.ParseBool(value)
	if err != nil {
		return false, catalogUnavailable()
	}
	return required, nil
}

type privateFreeEndpoint struct {
	ModelID         string                     `json:"model_id"`
	Tag             string                     `json:"tag"`
	Status          *int                       `json:"status"`
	SafetyRetention *bool                      `json:"safety_retention"`
	Pricing         map[string]json.RawMessage `json:"pricing"`
}

// /models?zdr=true alone can match a paid deployment of a free model family.
// Intersect exact IDs AND zero endpoint prices, and never guess missing policy.
func fetchPrivateFreeProviders(ctx context.Context) (map[string][]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterBaseURL+"/endpoints/zdr", nil)
	if err != nil {
		return nil, catalogUnavailable()
	}
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	if err != nil {
		return nil, catalogUnavailable()
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, catalogUnavailable()
	}
	var catalog struct {
		Data []privateFreeEndpoint `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 12<<20)).Decode(&catalog) != nil {
		return nil, catalogUnavailable()
	}
	providers := make(map[string][]string)
	for _, endpoint := range catalog.Data {
		if endpoint.Status == nil || *endpoint.Status != 0 || endpoint.SafetyRetention == nil || *endpoint.SafetyRetention || !freeModelPricing(endpoint.Pricing) || !validProviderTag(endpoint.Tag) {
			continue
		}
		if !slices.Contains(providers[endpoint.ModelID], endpoint.Tag) {
			providers[endpoint.ModelID] = append(providers[endpoint.ModelID], endpoint.Tag)
		}
	}
	return providers, nil
}

func validProviderTag(tag string) bool {
	return tag != "" && len(tag) <= 160 && !strings.ContainsFunc(tag, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-_/.", r)
	})
}

func copyAssistantModels(models []AssistantModel) []AssistantModel {
	cloned := append([]AssistantModel(nil), models...)
	for i := range cloned {
		cloned[i].privateProviders = slices.Clone(models[i].privateProviders)
	}
	return cloned
}
