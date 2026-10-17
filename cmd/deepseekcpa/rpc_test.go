package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// testBridge satisfies the host bridge without touching the host process.
type testBridge struct{}

func (testBridge) Log(context.Context, string, string) error { return nil }

func (testBridge) SaveAuth(context.Context, pluginapi.HostAuthSaveRequest) (pluginapi.HostAuthSaveResponse, error) {
	return pluginapi.HostAuthSaveResponse{}, nil
}

func (testBridge) Do(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	return pluginapi.HTTPResponse{}, nil
}

func (testBridge) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pluginapi.HTTPStreamResponse{}, nil
}

func decodeEnvelope(t *testing.T, raw []byte) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v (raw=%s)", err, raw)
	}
	return env
}

func TestRegisterReturnsCapabilityDeclaration(t *testing.T) {
	srv := newServer(testBridge{})
	raw, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{"config_yaml":""}`))
	if err != nil {
		t.Fatalf("handle(register) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}

	var reg registration
	if err := json.Unmarshal(env.Result, &reg); err != nil {
		t.Fatalf("unmarshal registration: %v", err)
	}
	if reg.Metadata.Name != "deepseek-cpa" {
		t.Errorf("Metadata.Name = %q, want deepseek-cpa", reg.Metadata.Name)
	}
	if reg.SchemaVersion != pluginabi.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", reg.SchemaVersion, pluginabi.SchemaVersion)
	}
	caps := reg.Capabilities
	if !caps.ModelProvider || !caps.AuthProvider || !caps.Executor || !caps.ThinkingApplier || !caps.UsagePlugin || !caps.ManagementAPI {
		t.Errorf("missing capability in declaration: %+v", caps)
	}
	if caps.ExecutorModelScope != pluginapi.ExecutorModelScopeStatic {
		t.Errorf("ExecutorModelScope = %q, want static", caps.ExecutorModelScope)
	}
	if len(caps.ExecutorInputFormats) != 3 || len(caps.ExecutorOutputFormats) != 3 {
		t.Errorf("formats = %v/%v, want three each", caps.ExecutorInputFormats, caps.ExecutorOutputFormats)
	}
}

func TestRegisterRejectsUnparseableConfig(t *testing.T) {
	srv := newServer(testBridge{})
	payload, err := json.Marshal(lifecycleRequest{ConfigYAML: []byte("models: [")})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	raw, err := srv.handle(pluginabi.MethodPluginRegister, payload)
	if err != nil {
		t.Fatalf("handle(register) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if env.OK {
		t.Fatal("ok = true, want failure for unparseable config")
	}
	if env.Error == nil || env.Error.Code != codeInvalidConfig {
		t.Fatalf("error = %+v, want code %q", env.Error, codeInvalidConfig)
	}
	if !strings.Contains(env.Error.Message, "config") {
		t.Errorf("error message = %q, want it to name config", env.Error.Message)
	}
}

func TestCallsBeforeRegisterAreRejected(t *testing.T) {
	srv := newServer(testBridge{})
	raw, err := srv.handle(pluginabi.MethodModelStatic, nil)
	if err != nil {
		t.Fatalf("handle(model.static) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if env.OK || env.Error == nil || env.Error.Code != codeNotRegistered {
		t.Fatalf("envelope = %+v, want not_registered", env)
	}
}

func TestIdentifierAndStaticModelsAfterRegister(t *testing.T) {
	srv := newServer(testBridge{})
	if _, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{}`)); err != nil {
		t.Fatalf("register: %v", err)
	}

	raw, err := srv.handle(pluginabi.MethodAuthIdentifier, nil)
	if err != nil {
		t.Fatalf("handle(auth.identifier) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	var ident identifierResponse
	if err := json.Unmarshal(env.Result, &ident); err != nil {
		t.Fatalf("unmarshal identifier: %v", err)
	}
	if ident.Identifier != "deepseek-cpa" {
		t.Errorf("Identifier = %q, want deepseek-cpa", ident.Identifier)
	}

	raw, err = srv.handle(pluginabi.MethodModelStatic, nil)
	if err != nil {
		t.Fatalf("handle(model.static) error = %v", err)
	}
	env = decodeEnvelope(t, raw)
	var models pluginapi.ModelResponse
	if err := json.Unmarshal(env.Result, &models); err != nil {
		t.Fatalf("unmarshal models: %v", err)
	}
	if models.Provider != "deepseek-cpa" || len(models.Models) == 0 {
		t.Errorf("models = %+v, want provider deepseek-cpa with models", models)
	}
}

func TestUnbuiltCapabilityReturnsNotImplementedCode(t *testing.T) {
	srv := newServer(testBridge{})
	if _, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{}`)); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw, err := srv.handle(pluginabi.MethodExecutorExecute, []byte(`{}`))
	if err != nil {
		t.Fatalf("handle(executor.execute) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if env.OK || env.Error == nil || env.Error.Code != codeNotImplemented {
		t.Fatalf("envelope = %+v, want not_implemented", env)
	}
}

func TestUnknownMethodIsReported(t *testing.T) {
	srv := newServer(testBridge{})
	if _, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{}`)); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw, err := srv.handle("does.not.exist", nil)
	if err != nil {
		t.Fatalf("handle(unknown) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if env.OK || env.Error == nil || env.Error.Code != codeUnknownMethod {
		t.Fatalf("envelope = %+v, want unknown_method", env)
	}
}
