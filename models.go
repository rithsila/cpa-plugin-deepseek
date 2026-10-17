package deepseekcpa

import "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

// defaultModels is the bootstrap list used until an upstream probe succeeds.
// It never requires network access: registration happens long before any
// credential exists.
func defaultModels() []ModelEntry {
	return []ModelEntry{
		{ID: "deepseek-flash", DisplayName: "DeepSeek Flash"},
		{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro"},
		{ID: "deepseek-v4-pro[1m]", DisplayName: "DeepSeek V4 Pro (1M)"},
	}
}

// ModelEntry is one client-visible model. ID is the upstream model name; the
// namespace prefix is added when the list is published.
type ModelEntry struct {
	ID          string
	DisplayName string
}

// modelInfo publishes one model under the plugin namespace. The prefix is part
// of the client-visible ID, so the executor strips it again before calling
// upstream.
func modelInfo(entry ModelEntry) pluginapi.ModelInfo {
	return pluginapi.ModelInfo{
		ID:                         Provider + "/" + entry.ID,
		Object:                     "model",
		OwnedBy:                    Provider,
		DisplayName:                displayName(entry),
		Name:                       entry.ID,
		SupportedGenerationMethods: []string{"chat"},
		UserDefined:                true,
	}
}

func displayName(entry ModelEntry) string {
	if entry.DisplayName != "" {
		return entry.DisplayName
	}
	return entry.ID
}
