// Package deepseekcpa implements the deepseek-cpa provider plugin for
// CLIProxyAPI. It serves DeepSeek's three protocol surfaces by forwarding the
// client's original payload to the matching DeepSeek endpoint, without
// translating between protocols. See docs/adr/0001.
package deepseekcpa

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// pluginVersion is the release version reported at registration.
var pluginVersion = "0.1.0"

// Version returns the plugin release version reported to the host.
func Version() string { return pluginVersion }

// Plugin implements every capability this plugin declares. One value backs all
// of them so the ABI layer keeps a single pointer.
type Plugin struct {
	bridge HostBridge
}

// Build validates plugins.configs.deepseek-cpa and describes the plugin to the
// host. It fails fast: an invalid configuration must not register a half-usable
// provider.
func Build(configYAML []byte, bridge HostBridge) (pluginapi.Plugin, *Plugin, error) {
	if bridge == nil {
		return pluginapi.Plugin{}, nil, fmt.Errorf("build: host bridge is required")
	}
	if err := validateConfig(configYAML); err != nil {
		return pluginapi.Plugin{}, nil, err
	}
	p := &Plugin{bridge: bridge}
	if err := p.bridge.Log(context.Background(), "info", "deepseek-cpa registered"); err != nil {
		return pluginapi.Plugin{}, nil, fmt.Errorf("build: host log: %w", err)
	}

	return pluginapi.Plugin{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             Provider,
			Version:          pluginVersion,
			Author:           "ahoo",
			GitHubRepository: "https://github.com/xspeed1989/cpa-plugin-deepseek",
		},
		Capabilities: pluginapi.Capabilities{
			ModelProvider:      p,
			AuthProvider:       p,
			Executor:           p,
			ExecutorModelScope: pluginapi.ExecutorModelScopeStatic,
			ExecutorInputFormats: []string{
				"chat-completions", "responses", "anthropic",
			},
			ExecutorOutputFormats: []string{
				"chat-completions", "responses", "anthropic",
			},
			ThinkingApplier: p,
			UsagePlugin:     p,
			ManagementAPI:   p,
		},
	}, p, nil
}

// Identifier returns the provider key handled by every capability.
func (p *Plugin) Identifier() string { return Provider }

// StaticModels publishes the bootstrap model list under the plugin namespace.
// It never touches the network; upstream probing replaces the list later.
func (p *Plugin) StaticModels(context.Context, pluginapi.StaticModelRequest) (pluginapi.ModelResponse, error) {
	entries := defaultModels()
	models := make([]pluginapi.ModelInfo, 0, len(entries))
	for _, entry := range entries {
		models = append(models, modelInfo(entry))
	}
	return pluginapi.ModelResponse{Provider: Provider, Models: models}, nil
}

// ModelsForAuth is part of the model provider capability but unused: the plugin
// registers static models only (executor_model_scope: static).
func (p *Plugin) ModelsForAuth(context.Context, pluginapi.AuthModelRequest) (pluginapi.ModelResponse, error) {
	return pluginapi.ModelResponse{}, notImplemented("model.for_auth")
}

// StartLogin reports that DeepSeek keys have no interactive login flow. This is
// permanent, not a gap: a DeepSeek key is a static string with no OAuth or
// device-code exchange to run.
func (p *Plugin) StartLogin(context.Context, pluginapi.AuthLoginStartRequest) (pluginapi.AuthLoginStartResponse, error) {
	return pluginapi.AuthLoginStartResponse{}, unsupported("auth.login.start", "DeepSeek API keys are static and have no interactive login flow")
}

// PollLogin reports that DeepSeek keys have no interactive login flow.
func (p *Plugin) PollLogin(context.Context, pluginapi.AuthLoginPollRequest) (pluginapi.AuthLoginPollResponse, error) {
	return pluginapi.AuthLoginPollResponse{}, unsupported("auth.login.poll", "DeepSeek API keys are static and have no interactive login flow")
}

// RefreshAuth reports that DeepSeek keys never expire, so there is nothing to
// refresh.
func (p *Plugin) RefreshAuth(context.Context, pluginapi.AuthRefreshRequest) (pluginapi.AuthRefreshResponse, error) {
	return pluginapi.AuthRefreshResponse{}, unsupported("auth.refresh", "DeepSeek API keys do not expire, so there is nothing to refresh")
}

// CountTokens is not backed by an upstream endpoint.
func (p *Plugin) CountTokens(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	return pluginapi.ExecutorResponse{}, notImplemented("executor.count_tokens")
}

// HttpRequest bridges executor-owned raw HTTP through the host client.
func (p *Plugin) HttpRequest(context.Context, pluginapi.ExecutorHTTPRequest) (pluginapi.ExecutorHTTPResponse, error) {
	return pluginapi.ExecutorHTTPResponse{}, notImplemented("executor.http_request")
}

// ApplyThinking writes the host-normalised thinking configuration into the
// provider payload. See docs/adr/0003.
func (p *Plugin) ApplyThinking(context.Context, pluginapi.ThinkingApplyRequest) (pluginapi.PayloadResponse, error) {
	return pluginapi.PayloadResponse{}, notImplemented("thinking.apply")
}

// HandleUsage receives completed usage records from the host. Structured usage
// logging lands with the usage ticket; the capability is declared now so the
// host routes records to this plugin from the first release.
func (p *Plugin) HandleUsage(context.Context, pluginapi.UsageRecord) {}

func notImplemented(method string) error {
	return fmt.Errorf("%s: %w", method, ErrNotImplemented)
}

func unsupported(method, reason string) error {
	return fmt.Errorf("%s: %w: %s", method, ErrUnsupported, reason)
}

var (
	_ pluginapi.AuthProvider      = (*Plugin)(nil)
	_ pluginapi.ModelProvider     = (*Plugin)(nil)
	_ pluginapi.ProviderExecutor  = (*Plugin)(nil)
	_ pluginapi.ThinkingApplier   = (*Plugin)(nil)
	_ pluginapi.UsagePlugin       = (*Plugin)(nil)
	_ pluginapi.ManagementAPI     = (*Plugin)(nil)
	_ pluginapi.ManagementHandler = (*Plugin)(nil)
)
