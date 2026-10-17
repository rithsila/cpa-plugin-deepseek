package deepseekcpa

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestBuildDeclaresOnlyImplementedCapabilities(t *testing.T) {
	descriptor, _, err := Build(nil, stubBridge{})
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}

	caps := descriptor.Capabilities
	declared := map[string]bool{
		"model_registrar":            caps.ModelRegistrar != nil,
		"model_provider":             caps.ModelProvider != nil,
		"auth_provider":              caps.AuthProvider != nil,
		"frontend_auth_provider":     caps.FrontendAuthProvider != nil,
		"scheduler":                  caps.Scheduler != nil,
		"model_router":               caps.ModelRouter != nil,
		"executor":                   caps.Executor != nil,
		"request_translator":         caps.RequestTranslator != nil,
		"request_normalizer":         caps.RequestNormalizer != nil,
		"response_translator":        caps.ResponseTranslator != nil,
		"response_before_translator": caps.ResponseBeforeTranslator != nil,
		"response_after_translator":  caps.ResponseAfterTranslator != nil,
		"request_interceptor":        caps.RequestInterceptor != nil,
		"response_interceptor":       caps.ResponseInterceptor != nil,
		"stream_chunk_interceptor":   caps.StreamChunkInterceptor != nil,
		"thinking_applier":           caps.ThinkingApplier != nil,
		"usage_plugin":               caps.UsagePlugin != nil,
		"command_line_plugin":        caps.CommandLinePlugin != nil,
		"management_api":             caps.ManagementAPI != nil,
	}
	want := map[string]bool{
		"model_provider":   true,
		"auth_provider":    true,
		"executor":         true,
		"thinking_applier": true,
		"usage_plugin":     true,
		"management_api":   true,
	}
	for name, got := range declared {
		if got != want[name] {
			t.Errorf("capability %s declared = %v, want %v", name, got, want[name])
		}
	}
}

func TestBuildRegistersProtocolAlignedExecutor(t *testing.T) {
	descriptor, _, err := Build(nil, stubBridge{})
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}

	wantFormats := []string{"chat-completions", "responses", "anthropic"}
	if got := descriptor.Capabilities.ExecutorInputFormats; !equalStrings(got, wantFormats) {
		t.Errorf("ExecutorInputFormats = %v, want %v", got, wantFormats)
	}
	if got := descriptor.Capabilities.ExecutorOutputFormats; !equalStrings(got, wantFormats) {
		t.Errorf("ExecutorOutputFormats = %v, want %v", got, wantFormats)
	}
	if got := descriptor.Capabilities.ExecutorModelScope; got != pluginapi.ExecutorModelScopeStatic {
		t.Errorf("ExecutorModelScope = %q, want %q", got, pluginapi.ExecutorModelScopeStatic)
	}
	if got := descriptor.SchemaVersion; got != pluginabi.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got, pluginabi.SchemaVersion)
	}
	if got := descriptor.Metadata.Name; got != Provider {
		t.Errorf("Metadata.Name = %q, want %q", got, Provider)
	}
}

func TestStaticModelsAreNamespacedAndOffline(t *testing.T) {
	descriptor, _, err := Build(nil, stubBridge{})
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}

	resp, err := descriptor.Capabilities.ModelProvider.StaticModels(context.Background(), pluginapi.StaticModelRequest{})
	if err != nil {
		t.Fatalf("StaticModels() error = %v, want nil", err)
	}
	if resp.Provider != Provider {
		t.Errorf("Provider = %q, want %q", resp.Provider, Provider)
	}
	if len(resp.Models) == 0 {
		t.Fatal("Models is empty, want built-in defaults")
	}
	for _, m := range resp.Models {
		if !strings.HasPrefix(m.ID, Provider+"/") {
			t.Errorf("model ID = %q, want %q prefix", m.ID, Provider+"/")
		}
		if strings.TrimPrefix(m.ID, Provider+"/") == "" {
			t.Errorf("model ID = %q, want a non-empty upstream name after the prefix", m.ID)
		}
	}
}

func TestUnimplementedCapabilitiesFailLoudly(t *testing.T) {
	descriptor, _, err := Build(nil, stubBridge{})
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}
	ctx := context.Background()

	if _, err := descriptor.Capabilities.AuthProvider.ParseAuth(ctx, pluginapi.AuthParseRequest{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("ParseAuth error = %v, want ErrNotImplemented", err)
	}
	if _, err := descriptor.Capabilities.Executor.Execute(ctx, pluginapi.ExecutorRequest{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Execute error = %v, want ErrNotImplemented", err)
	}
	if _, err := descriptor.Capabilities.ThinkingApplier.ApplyThinking(ctx, pluginapi.ThinkingApplyRequest{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("ApplyThinking error = %v, want ErrNotImplemented", err)
	}
	if _, err := descriptor.Capabilities.ManagementAPI.RegisterManagement(ctx, pluginapi.ManagementRegistrationRequest{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("RegisterManagement error = %v, want ErrNotImplemented", err)
	}
}

func TestIdentifiersUsePluginID(t *testing.T) {
	descriptor, _, err := Build(nil, stubBridge{})
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}
	if got := descriptor.Capabilities.AuthProvider.Identifier(); got != Provider {
		t.Errorf("auth Identifier() = %q, want %q", got, Provider)
	}
	if got := descriptor.Capabilities.Executor.Identifier(); got != Provider {
		t.Errorf("executor Identifier() = %q, want %q", got, Provider)
	}
	if got := descriptor.Capabilities.ThinkingApplier.Identifier(); got != Provider {
		t.Errorf("thinking Identifier() = %q, want %q", got, Provider)
	}
}

func TestBuildRejectsInvalidConfig(t *testing.T) {
	if _, _, err := Build([]byte("models: ["), stubBridge{}); err == nil {
		t.Fatal("Build() error = nil, want config validation error")
	}
}

func TestBuildRequiresBridge(t *testing.T) {
	if _, _, err := Build(nil, nil); err == nil {
		t.Fatal("Build(nil bridge) error = nil, want error")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
