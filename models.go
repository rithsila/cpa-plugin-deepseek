package deepseekcpa

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// defaultModels is the bootstrap list used until an upstream probe succeeds.
// It never requires network access: registration happens long before any
// credential exists. Only the identifiers are listed; every capability detail
// (context length, output limit, modalities, reasoning levels) comes from the
// upstream probe, so nothing here can drift from what DeepSeek actually serves.
func defaultModels() []ModelEntry {
	return []ModelEntry{
		{ID: "deepseek-flash", DisplayName: "DeepSeek Flash"},
		{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro"},
		{ID: "deepseek-v4-pro[1m]", DisplayName: "DeepSeek V4 Pro (1M)"},
	}
}

// ModelEntry is one client-visible model. ID is the upstream model name; the
// namespace prefix is added when the list is published. The remaining fields are
// whatever upstream advertised for it.
type ModelEntry struct {
	ID               string
	DisplayName      string
	ContextLength    int64
	MaxOutputTokens  int64
	InputModalities  []string
	OutputModalities []string
	// ThinkingLevels is ordered with the upstream default level first.
	ThinkingLevels []string
}

// modelInfo publishes one model under the plugin namespace. The prefix is part
// of the client-visible ID, so the executor strips it again before calling
// upstream. Capability fields are only set when upstream reported them: the host
// substitutes its own defaults for anything left empty, which would misreport
// DeepSeek's limits.
func modelInfo(entry ModelEntry) pluginapi.ModelInfo {
	info := pluginapi.ModelInfo{
		ID:                         Provider + "/" + entry.ID,
		Object:                     "model",
		OwnedBy:                    Provider,
		DisplayName:                displayName(entry),
		Name:                       entry.ID,
		SupportedGenerationMethods: []string{"chat"},
		ContextLength:              entry.ContextLength,
		MaxCompletionTokens:        entry.MaxOutputTokens,
		SupportedInputModalities:   entry.InputModalities,
		SupportedOutputModalities:  entry.OutputModalities,
		UserDefined:                true,
	}
	if len(entry.ThinkingLevels) > 0 {
		info.Thinking = &pluginapi.ThinkingSupport{Levels: entry.ThinkingLevels}
	}
	return info
}

func displayName(entry ModelEntry) string {
	if entry.DisplayName != "" {
		return entry.DisplayName
	}
	return entry.ID
}

// orderThinkingLevels puts the upstream default level first. The host derives a
// model's default reasoning effort from the level list, preferring "medium" when
// it is offered and otherwise taking the first non-"none" level. DeepSeek offers
// low/high/max with no medium, so leading with the upstream default is what makes
// the host land on it; a list that offered medium would ignore the ordering.
func orderThinkingLevels(levels []string, defaultLevel string) []string {
	defaultLevel = strings.ToLower(strings.TrimSpace(defaultLevel))
	ordered := make([]string, 0, len(levels))
	for _, level := range levels {
		level = strings.ToLower(strings.TrimSpace(level))
		if level != "" && level == defaultLevel {
			ordered = append(ordered, level)
		}
	}
	for _, level := range levels {
		level = strings.ToLower(strings.TrimSpace(level))
		if level != "" && level != defaultLevel {
			ordered = append(ordered, level)
		}
	}
	return ordered
}
