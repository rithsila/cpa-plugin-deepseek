package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// fakeSink records stream chunks instead of pushing them to a host.
type fakeSink struct {
	mu     sync.Mutex
	emits  []pluginapi.ExecutorStreamChunk
	closed []string
	err    error
}

func (f *fakeSink) Emit(_ string, chunk pluginapi.ExecutorStreamChunk) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.emits = append(f.emits, chunk)
	return nil
}

func (f *fakeSink) Close(streamID, errMessage string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, streamID+"|"+errMessage)
	return nil
}

func (f *fakeSink) snapshot() ([]pluginapi.ExecutorStreamChunk, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pluginapi.ExecutorStreamChunk(nil), f.emits...), append([]string(nil), f.closed...)
}

// testBridge satisfies the host bridge without touching the host process.
type testBridge struct{}

func (testBridge) Log(context.Context, string, string, map[string]any) error { return nil }

func (testBridge) ListAuths(context.Context) ([]pluginapi.HostAuthFileEntry, error) {
	return nil, nil
}

func (testBridge) GetAuth(context.Context, pluginapi.HostAuthGetRequest) (pluginapi.HostAuthGetResponse, error) {
	return pluginapi.HostAuthGetResponse{}, nil
}

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
	srv := newServer(testBridge{}, &fakeSink{})
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
	if !caps.ModelProvider || !caps.AuthProvider || !caps.Executor || !caps.UsagePlugin || !caps.ManagementAPI {
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
	srv := newServer(testBridge{}, &fakeSink{})
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
	srv := newServer(testBridge{}, &fakeSink{})
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
	srv := newServer(testBridge{}, &fakeSink{})
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
	srv := newServer(testBridge{}, &fakeSink{})
	if _, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{}`)); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw, err := srv.handle(pluginabi.MethodExecutorCountTokens, []byte(`{}`))
	if err != nil {
		t.Fatalf("handle(executor.count_tokens) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if env.OK || env.Error == nil || env.Error.Code != codeNotImplemented {
		t.Fatalf("envelope = %+v, want not_implemented", env)
	}
}

func TestUnknownMethodIsReported(t *testing.T) {
	srv := newServer(testBridge{}, &fakeSink{})
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

func TestManagementRegisterSerialisesRoutesWithoutHandlers(t *testing.T) {
	srv := newServer(testBridge{}, &fakeSink{})
	if _, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{}`)); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw, err := srv.handle(pluginabi.MethodManagementRegister, []byte(`{}`))
	if err != nil {
		t.Fatalf("handle(management.register) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}
	var reg managementRegistration
	if err := json.Unmarshal(env.Result, &reg); err != nil {
		t.Fatalf("unmarshal registration: %v", err)
	}
	if len(reg.Routes) != 4 {
		t.Fatalf("routes = %d, want 4", len(reg.Routes))
	}
	for _, route := range reg.Routes {
		switch route.Path {
		case "/plugins/deepseek-cpa/keys", "/plugins/deepseek-cpa/models/refresh":
		default:
			t.Errorf("unexpected route path %q", route.Path)
		}
	}
	if len(reg.Resources) != 0 {
		t.Errorf("resources = %d, want none", len(reg.Resources))
	}
}

func TestManagementHandleDispatchesToListKeys(t *testing.T) {
	srv := newServer(testBridge{}, &fakeSink{})
	if _, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{}`)); err != nil {
		t.Fatalf("register: %v", err)
	}
	payload, err := json.Marshal(managementHandleRequest{
		ManagementRequest: pluginapi.ManagementRequest{Method: "GET", Path: "/plugins/deepseek-cpa/keys"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	raw, err := srv.handle(pluginabi.MethodManagementHandle, payload)
	if err != nil {
		t.Fatalf("handle(management.handle) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}
	var response pluginapi.ManagementResponse
	if err := json.Unmarshal(env.Result, &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", response.StatusCode)
	}
}

// statusBridge returns one fixed upstream response so the executor error path
// can be exercised through the RPC envelope.
type statusBridge struct {
	testBridge
	status int
	body   string
}

func (b statusBridge) Do(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	return pluginapi.HTTPResponse{StatusCode: b.status, Body: []byte(b.body)}, nil
}

func TestUpstreamStatusSurvivesTheErrorEnvelope(t *testing.T) {
	const body = `{"error":{"message":"slow down","type":"rate_limit"}}`
	srv := newServer(statusBridge{status: 429, body: body}, &fakeSink{})
	if _, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{}`)); err != nil {
		t.Fatalf("register: %v", err)
	}

	request := pluginapi.ExecutorRequest{
		Model:        "deepseek-cpa/deepseek-v4-pro",
		Format:       "openai-response",
		SourceFormat: "openai-response",
		Payload:      []byte(`{"model":"deepseek-cpa/deepseek-v4-pro"}`),
		StorageJSON:  []byte(`{"type":"deepseek-cpa","api_key":"sk-x"}`),
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	raw, err := srv.handle(pluginabi.MethodExecutorExecute, payload)
	if err != nil {
		t.Fatalf("handle(executor.execute) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if env.OK || env.Error == nil {
		t.Fatalf("envelope = %+v, want an error envelope", env)
	}
	if env.Error.HTTPStatus != 429 {
		t.Errorf("http_status = %d, want 429 so the client sees the upstream status", env.Error.HTTPStatus)
	}
	if env.Error.Message != body {
		t.Errorf("message = %q, want the upstream body preserved", env.Error.Message)
	}
}

// streamBridge serves one fixed upstream chunk sequence.
type streamBridge struct {
	testBridge
	frames []string
}

func (b streamBridge) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	chunks := make(chan pluginapi.HTTPStreamChunk, len(b.frames))
	for _, frame := range b.frames {
		chunks <- pluginapi.HTTPStreamChunk{Payload: []byte(frame)}
	}
	close(chunks)
	return pluginapi.HTTPStreamResponse{StatusCode: 200, Chunks: chunks}, nil
}

func TestExecutorExecuteStreamPushesChunksThroughTheSink(t *testing.T) {
	frames := []string{"event: response.created\ndata: {}\n\n", "event: response.completed\ndata: {}\n\n"}
	sink := &fakeSink{}
	srv := newServer(streamBridge{frames: frames}, sink)
	if _, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{}`)); err != nil {
		t.Fatalf("register: %v", err)
	}

	request := streamExecutorRequest{
		ExecutorRequest: pluginapi.ExecutorRequest{
			Model:        "deepseek-cpa/deepseek-v4-pro",
			Format:       "openai-response",
			SourceFormat: "openai-response",
			Stream:       true,
			Payload:      []byte(`{"model":"deepseek-cpa/deepseek-v4-pro","stream":true}`),
			StorageJSON:  []byte(`{"type":"deepseek-cpa","api_key":"sk-x"}`),
		},
		StreamID: "stream-1",
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	raw, err := srv.handle(pluginabi.MethodExecutorExecuteStream, payload)
	if err != nil {
		t.Fatalf("handle(executor.execute_stream) error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}

	// The response must not try to carry the chunk channel.
	var headers streamHeaders
	if err := json.Unmarshal(env.Result, &headers); err != nil {
		t.Fatalf("unmarshal stream response: %v", err)
	}

	// Chunks are pushed asynchronously; wait for the close that follows them.
	emits, closed := waitForClose(t, sink)
	if len(emits) != len(frames) {
		t.Fatalf("emitted %d chunks, want %d", len(emits), len(frames))
	}
	for i, frame := range frames {
		if string(emits[i].Payload) != frame {
			t.Errorf("chunk %d = %q, want %q", i, emits[i].Payload, frame)
		}
	}
	if closed != "stream-1|" {
		t.Errorf("closed = %q, want stream-1 with no error", closed)
	}
}

func TestExecutorExecuteStreamRequiresAStreamID(t *testing.T) {
	srv := newServer(testBridge{}, &fakeSink{})
	if _, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{}`)); err != nil {
		t.Fatalf("register: %v", err)
	}
	request := streamExecutorRequest{
		ExecutorRequest: pluginapi.ExecutorRequest{
			Model:        "deepseek-cpa/deepseek-v4-pro",
			SourceFormat: "openai-response",
			Payload:      []byte(`{"model":"deepseek-cpa/deepseek-v4-pro"}`),
			StorageJSON:  []byte(`{"type":"deepseek-cpa","api_key":"sk-x"}`),
		},
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	raw, err := srv.handle(pluginabi.MethodExecutorExecuteStream, payload)
	if err != nil {
		t.Fatalf("handle error = %v", err)
	}
	env := decodeEnvelope(t, raw)
	if env.OK || env.Error == nil {
		t.Fatalf("envelope = %+v, want an error for a missing stream id", env)
	}
}

func TestExecutorExecuteStreamClosesWithErrorWhenTheHostRejectsChunks(t *testing.T) {
	sink := &fakeSink{err: errors.New("host stream gone")}
	srv := newServer(streamBridge{frames: []string{"frame"}}, sink)
	if _, err := srv.handle(pluginabi.MethodPluginRegister, []byte(`{}`)); err != nil {
		t.Fatalf("register: %v", err)
	}
	request := streamExecutorRequest{
		ExecutorRequest: pluginapi.ExecutorRequest{
			Model:        "deepseek-cpa/deepseek-v4-pro",
			SourceFormat: "openai-response",
			Payload:      []byte(`{"model":"deepseek-cpa/deepseek-v4-pro"}`),
			StorageJSON:  []byte(`{"type":"deepseek-cpa","api_key":"sk-x"}`),
		},
		StreamID: "stream-2",
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := srv.handle(pluginabi.MethodExecutorExecuteStream, payload); err != nil {
		t.Fatalf("handle error = %v", err)
	}
	_, closed := waitForClose(t, sink)
	if !strings.Contains(closed, "host stream gone") {
		t.Errorf("closed = %q, want it to carry the sink error", closed)
	}
}

// waitForClose waits for a stream to be closed and returns what was emitted.
func waitForClose(t *testing.T, sink *fakeSink) ([]pluginapi.ExecutorStreamChunk, string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		emits, closed := sink.snapshot()
		if len(closed) > 0 {
			return emits, closed[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("stream was never closed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
