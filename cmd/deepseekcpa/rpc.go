package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	deepseekcpa "github.com/xspeed1989/cpa-plugin-deepseek"
)

// Error codes surfaced in the RPC error envelope.
const (
	codeUnknownMethod  = "unknown_method"
	codeInvalidConfig  = "invalid_config"
	codeNotRegistered  = "not_registered"
	codeNotImplemented = "not_implemented"
	codeUnsupported    = "unsupported"
	codeUpstream       = "upstream_error"
	codeInternal       = "internal_error"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// HTTPStatus lets an upstream failure reach the client with its own status
	// instead of the host defaulting to 500.
	HTTPStatus int `json:"http_status,omitempty"`
}

// lifecycleRequest is the payload of plugin.register and plugin.reconfigure.
type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

// registrationCapability mirrors the JSON contract the host decodes at
// registration; pluginapi.Capabilities holds live interfaces, not booleans.
type registrationCapability struct {
	ModelProvider         bool                         `json:"model_provider"`
	AuthProvider          bool                         `json:"auth_provider"`
	Executor              bool                         `json:"executor"`
	ExecutorModelScope    pluginapi.ExecutorModelScope `json:"executor_model_scope"`
	ExecutorInputFormats  []string                     `json:"executor_input_formats,omitempty"`
	ExecutorOutputFormats []string                     `json:"executor_output_formats,omitempty"`
	UsagePlugin           bool                         `json:"usage_plugin"`
	ManagementAPI         bool                         `json:"management_api"`
}

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type identifierResponse struct {
	Identifier string `json:"identifier"`
}

// managementRegistration mirrors the host contract for management.register.
// Handlers are not serialisable: the host supplies its own dispatcher, which
// routes back into management.handle.
type managementRegistration struct {
	Routes    []managementRoute    `json:"routes,omitempty"`
	Resources []managementResource `json:"resources,omitempty"`
}

type managementRoute struct {
	Method      string `json:"Method"`
	Path        string `json:"Path"`
	Menu        string `json:"Menu,omitempty"`
	Description string `json:"Description,omitempty"`
}

type managementResource struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu,omitempty"`
	Description string `json:"Description,omitempty"`
}

// managementHandleRequest is what the host sends to management.handle.
type managementHandleRequest struct {
	pluginapi.ManagementRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

// streamSink is how executor stream chunks reach the host. The host opens a
// stream before calling execute_stream and expects chunks through callbacks, so
// the response itself carries headers only.
type streamSink interface {
	Emit(streamID string, chunk pluginapi.ExecutorStreamChunk) error
	Close(streamID, errMessage string) error
}

// server holds the plugin instance the host registered and dispatches RPC
// methods to it.
type server struct {
	bridge deepseekcpa.HostBridge
	stream streamSink

	mu       sync.Mutex
	instance *deepseekcpa.Plugin
}

func newServer(bridge deepseekcpa.HostBridge, stream streamSink) *server {
	return &server{bridge: bridge, stream: stream}
}

// handle turns one RPC method plus its raw payload into an envelope. Protocol
// failures become error envelopes; only marshaling failures return a Go error.
func (s *server) handle(method string, payload []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return s.register(payload)
	case pluginabi.MethodPluginShutdown:
		s.mu.Lock()
		s.instance = nil
		s.mu.Unlock()
		return okEnvelope(map[string]any{})
	case pluginabi.MethodPluginQuiesce:
		return okEnvelope(map[string]any{})
	}

	instance := s.current()
	if instance == nil {
		return errorEnvelope(codeNotRegistered, "plugin is not registered"), nil
	}

	ctx := context.Background()
	switch method {
	case pluginabi.MethodModelStatic:
		var req pluginapi.StaticModelRequest
		return decodeInto(payload, &req, func() (any, error) { return instance.StaticModels(ctx, req) })
	case pluginabi.MethodModelForAuth:
		var req pluginapi.AuthModelRequest
		return decodeInto(payload, &req, func() (any, error) { return instance.ModelsForAuth(ctx, req) })
	case pluginabi.MethodAuthIdentifier, pluginabi.MethodExecutorIdentifier:
		return okEnvelope(identifierResponse{Identifier: instance.Identifier()})
	case pluginabi.MethodAuthParse:
		var req pluginapi.AuthParseRequest
		return decodeInto(payload, &req, func() (any, error) { return instance.ParseAuth(ctx, req) })
	case pluginabi.MethodAuthLoginStart:
		var req pluginapi.AuthLoginStartRequest
		return decodeInto(payload, &req, func() (any, error) { return instance.StartLogin(ctx, req) })
	case pluginabi.MethodAuthLoginPoll:
		var req pluginapi.AuthLoginPollRequest
		return decodeInto(payload, &req, func() (any, error) { return instance.PollLogin(ctx, req) })
	case pluginabi.MethodAuthRefresh:
		var req pluginapi.AuthRefreshRequest
		return decodeInto(payload, &req, func() (any, error) { return instance.RefreshAuth(ctx, req) })
	case pluginabi.MethodExecutorExecute:
		var req pluginapi.ExecutorRequest
		return decodeInto(payload, &req, func() (any, error) { return instance.Execute(ctx, req) })
	case pluginabi.MethodExecutorExecuteStream:
		return s.executorExecuteStream(ctx, instance, payload)
	case pluginabi.MethodExecutorCountTokens:
		var req pluginapi.ExecutorRequest
		return decodeInto(payload, &req, func() (any, error) { return instance.CountTokens(ctx, req) })
	case pluginabi.MethodExecutorHTTPRequest:
		var req pluginapi.ExecutorHTTPRequest
		return decodeInto(payload, &req, func() (any, error) { return instance.HttpRequest(ctx, req) })
	case pluginabi.MethodUsageHandle:
		var record pluginapi.UsageRecord
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, fmt.Errorf("decode usage record: %w", err)
		}
		instance.HandleUsage(ctx, record)
		return okEnvelope(map[string]any{})
	case pluginabi.MethodManagementRegister:
		var req pluginapi.ManagementRegistrationRequest
		return decodeInto(payload, &req, func() (any, error) {
			registered, err := instance.RegisterManagement(ctx, req)
			return describeManagement(registered), err
		})
	case pluginabi.MethodManagementHandle:
		var req managementHandleRequest
		return decodeInto(payload, &req, func() (any, error) {
			return instance.HandleManagement(ctx, req.ManagementRequest)
		})
	default:
		return errorEnvelope(codeUnknownMethod, "unknown method: "+method), nil
	}
}

// executorExecuteStream starts a streaming execution and hands the chunk stream
// to the host through the stream sink. The RPC response only carries headers:
// the host cannot receive a channel.
func (s *server) executorExecuteStream(ctx context.Context, instance *deepseekcpa.Plugin, payload []byte) ([]byte, error) {
	var req streamExecutorRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("decode stream request: %w", err)
		}
	}
	if req.StreamID == "" {
		return errorEnvelope(codeInternal, "executor.execute_stream: host supplied no stream id"), nil
	}

	response, err := instance.ExecuteStream(ctx, req.ExecutorRequest)
	if err != nil {
		return errorEnvelopeFor(err), nil
	}
	go s.pumpStream(ctx, req.StreamID, response.Chunks)
	return okEnvelope(streamHeaders{Headers: response.Headers})
}

// pumpStream forwards every chunk to the host and closes the stream exactly
// once, including when the host stops accepting chunks.
func (s *server) pumpStream(ctx context.Context, streamID string, chunks <-chan pluginapi.ExecutorStreamChunk) {
	closeStream := func(errMessage string) {
		if err := s.stream.Close(streamID, errMessage); err != nil {
			// The stream is already gone, so the host log is the last place this
			// can surface; if the log call fails too there is nothing left to
			// report to.
			_ = s.bridge.Log(ctx, "warn", "stream close failed", map[string]any{"error": err.Error()})
		}
	}
	for chunk := range chunks {
		if err := s.stream.Emit(streamID, chunk); err != nil {
			closeStream(err.Error())
			return
		}
	}
	closeStream("")
}

type streamExecutorRequest struct {
	pluginapi.ExecutorRequest
	StreamID string `json:"stream_id"`
}

// streamHeaders is the execute_stream response: no chunks, they travel through
// the sink.
type streamHeaders struct {
	Headers http.Header `json:"Headers,omitempty"`
}

func (s *server) register(payload []byte) ([]byte, error) {
	var req lifecycleRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("decode lifecycle request: %w", err)
		}
	}
	descriptor, instance, err := deepseekcpa.Build(req.ConfigYAML, s.bridge)
	if err != nil {
		return errorEnvelope(codeInvalidConfig, err.Error()), nil
	}
	describe := describeRegistration(descriptor)
	s.mu.Lock()
	s.instance = instance
	s.mu.Unlock()
	return okEnvelope(describe)
}

func (s *server) current() *deepseekcpa.Plugin {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instance
}

func describeRegistration(descriptor pluginapi.Plugin) registration {
	caps := descriptor.Capabilities
	return registration{
		SchemaVersion: descriptor.SchemaVersion,
		Metadata:      descriptor.Metadata,
		Capabilities: registrationCapability{
			ModelProvider:         caps.ModelProvider != nil,
			AuthProvider:          caps.AuthProvider != nil,
			Executor:              caps.Executor != nil,
			ExecutorModelScope:    caps.ExecutorModelScope,
			ExecutorInputFormats:  caps.ExecutorInputFormats,
			ExecutorOutputFormats: caps.ExecutorOutputFormats,
			UsagePlugin:           caps.UsagePlugin != nil,
			ManagementAPI:         caps.ManagementAPI != nil,
		},
	}
}

// describeManagement drops the non-serialisable handlers from the registration.
func describeManagement(registered pluginapi.ManagementRegistrationResponse) managementRegistration {
	out := managementRegistration{
		Routes:    make([]managementRoute, 0, len(registered.Routes)),
		Resources: make([]managementResource, 0, len(registered.Resources)),
	}
	for _, route := range registered.Routes {
		out.Routes = append(out.Routes, managementRoute{
			Method:      route.Method,
			Path:        route.Path,
			Menu:        route.Menu,
			Description: route.Description,
		})
	}
	for _, resource := range registered.Resources {
		out.Resources = append(out.Resources, managementResource{
			Path:        resource.Path,
			Menu:        resource.Menu,
			Description: resource.Description,
		})
	}
	return out
}

// decodeInto unmarshals the payload, runs the capability call, and converts a
// "not implemented" failure into its own error code.
func decodeInto[T any](payload []byte, target *T, call func() (any, error)) ([]byte, error) {
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, target); err != nil {
			return nil, fmt.Errorf("decode request: %w", err)
		}
	}
	result, err := call()
	if err != nil {
		return errorEnvelopeFor(err), nil
	}
	return okEnvelope(result)
}

func okEnvelope(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

// errorEnvelopeFor maps a plugin failure onto the wire envelope, preserving an
// upstream HTTP status when the error carries one.
func errorEnvelopeFor(err error) []byte {
	var statusCoder interface{ StatusCode() int }
	if errors.As(err, &statusCoder) && statusCoder.StatusCode() > 0 {
		return errorEnvelopeWithStatus(codeUpstream, err.Error(), statusCoder.StatusCode())
	}
	if errors.Is(err, deepseekcpa.ErrNotImplemented) {
		return errorEnvelope(codeNotImplemented, err.Error())
	}
	if errors.Is(err, deepseekcpa.ErrUnsupported) {
		return errorEnvelope(codeUnsupported, err.Error())
	}
	return errorEnvelope(codeInternal, err.Error())
}

func errorEnvelopeWithStatus(code, message string, status int) []byte {
	raw, err := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message, HTTPStatus: status}})
	if err != nil {
		panic("errorEnvelopeWithStatus: marshaling a struct of strings cannot fail: " + err.Error())
	}
	return raw
}

func errorEnvelope(code, message string) []byte {
	return errorEnvelopeWithStatus(code, message, 0)
}
