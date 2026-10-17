package deepseekcpa

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Provider is the plugin identifier. It doubles as the plugin config key
// (plugins.configs.deepseek-cpa), the dynamic library name, and the model
// namespace prefix. It must not collide with a built-in provider key.
const Provider = "deepseek-cpa"

// validateConfig rejects plugin configuration the plugin cannot serve. The
// plugin owns no configuration fields: the upstream endpoints are fixed
// DeepSeek URLs and the model list comes from upstream probing, so the only
// thing that can be wrong is that the host handed us something that is not a
// YAML mapping.
func validateConfig(raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}
