package openai

import "reasonix/internal/provider"

func testProviderConfig(name, baseURL, model, apiKey string, extra map[string]any) provider.Config {
	if extra == nil {
		extra = map[string]any{}
	}
	return provider.Config{
		Name:    name,
		BaseURL: baseURL,
		Model:   model,
		APIKey:  apiKey,
		Extra:   extra,
	}
}
