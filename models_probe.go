package deepseekcpa

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// modelsEndpoint lists the models DeepSeek serves to this account.
const modelsEndpoint = "https://api.deepseek.com/models"

// probeModels asks upstream which models exist. It is the only source of truth:
// what DeepSeek does not list is not published, even when the plugin knows the
// name works (for example the "[1m]" context variant).
func (p *Plugin) probeModels(ctx context.Context) ([]ModelEntry, error) {
	credential, err := p.probeCredential(ctx)
	if err != nil {
		return nil, err
	}
	response, err := p.bridge.Do(ctx, pluginapi.HTTPRequest{
		Method:  http.MethodGet,
		URL:     modelsEndpoint,
		Headers: http.Header{"Authorization": {"Bearer " + credential}},
	})
	if err != nil {
		return nil, fmt.Errorf("model probe: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model probe: upstream returned status %d", response.StatusCode)
	}

	var upstream struct {
		Data []struct {
			ID               string   `json:"id"`
			Name             string   `json:"name"`
			ContextWindow    int64    `json:"context_window"`
			MaxOutputTokens  int64    `json:"max_output_tokens"`
			InputModalities  []string `json:"input_modalities"`
			OutputModalities []string `json:"output_modalities"`
			Effort           struct {
				SupportedLevels []string `json:"supported_levels"`
				DefaultLevel    string   `json:"default_level"`
			} `json:"effort"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &upstream); err != nil {
		return nil, fmt.Errorf("model probe: decode upstream list: %w", err)
	}
	entries := make([]ModelEntry, 0, len(upstream.Data))
	for index, model := range upstream.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			// A payload with an unidentifiable entry cannot be trusted as the
			// published list; caching what is left would hide the problem.
			return nil, fmt.Errorf("model probe: upstream entry %d has no id", index)
		}
		entries = append(entries, ModelEntry{
			ID:               id,
			DisplayName:      strings.TrimSpace(model.Name),
			ContextLength:    model.ContextWindow,
			MaxOutputTokens:  model.MaxOutputTokens,
			InputModalities:  model.InputModalities,
			OutputModalities: model.OutputModalities,
			ThinkingLevels:   orderThinkingLevels(model.Effort.SupportedLevels, model.Effort.DefaultLevel),
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("model probe: upstream listed no usable models")
	}
	return entries, nil
}

// probeCredential picks one of our credentials to authenticate the probe. The
// plugin has no other way to reach a key, and any of them can list the models.
func (p *Plugin) probeCredential(ctx context.Context) (string, error) {
	entries, err := p.bridge.ListAuths(ctx)
	if err != nil {
		return "", fmt.Errorf("model probe: %w", err)
	}
	for _, entry := range entries {
		if entry.Provider != Provider {
			continue
		}
		got, err := p.bridge.GetAuth(ctx, pluginapi.HostAuthGetRequest{AuthIndex: entry.AuthIndex})
		if err != nil {
			return "", fmt.Errorf("model probe: read credential %s: %w", entry.Name, err)
		}
		credential, ours, err := decodeCredential(got.JSON)
		if err != nil {
			return "", fmt.Errorf("model probe: %w", err)
		}
		if !ours || strings.TrimSpace(credential.APIKey) == "" {
			return "", fmt.Errorf("model probe: credential %s carries no api_key", entry.Name)
		}
		return strings.TrimSpace(credential.APIKey), nil
	}
	return "", fmt.Errorf("model probe: no %s credential is available yet", Provider)
}

// setModels stores a probe result as the published list.
func (p *Plugin) setModels(entries []ModelEntry) {
	p.modelMu.Lock()
	defer p.modelMu.Unlock()
	p.models = entries
}

// cachedModels returns the published list, or nil when no probe has succeeded.
func (p *Plugin) cachedModels() []ModelEntry {
	p.modelMu.Lock()
	defer p.modelMu.Unlock()
	return p.models
}

// publishModels converts model entries into the host-facing list.
func publishModels(entries []ModelEntry) []pluginapi.ModelInfo {
	models := make([]pluginapi.ModelInfo, 0, len(entries))
	for _, entry := range entries {
		models = append(models, modelInfo(entry))
	}
	return models
}
