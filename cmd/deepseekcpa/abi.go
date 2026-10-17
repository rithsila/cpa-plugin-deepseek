// Package main is the dynamic library entry point for the deepseek-cpa plugin.
// It translates the host's JSON-envelope C ABI into calls on the deepseekcpa
// package, and implements the host callback bridge.
package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// theServer is the single RPC dispatcher for this dynamic library instance.
var theServer = newServer(cgoBridge{})

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	if host != nil {
		C.store_host_api(host)
	}
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}
	var payload []byte
	if request != nil && requestLen > 0 {
		payload = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, err := theServer.handle(C.GoString(method), payload)
	if err != nil {
		writeResponse(response, errorEnvelope("plugin_error", err.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}

// cgoBridge implements deepseekcpa.HostBridge over the host function table.
type cgoBridge struct{}

// Log writes through the host logger. Errors are returned so callers can fail
// loudly instead of losing the message.
func (cgoBridge) Log(_ context.Context, level, message string) error {
	payload, err := json.Marshal(struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}{Level: level, Message: message})
	if err != nil {
		return fmt.Errorf("encode log request: %w", err)
	}
	if _, err := callHost(pluginabi.MethodHostLog, payload); err != nil {
		return err
	}
	return nil
}

// SaveAuth writes a credential record through the host.
func (cgoBridge) SaveAuth(_ context.Context, req pluginapi.HostAuthSaveRequest) (pluginapi.HostAuthSaveResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return pluginapi.HostAuthSaveResponse{}, fmt.Errorf("encode auth save request: %w", err)
	}
	raw, err := callHost(pluginabi.MethodHostAuthSave, payload)
	if err != nil {
		return pluginapi.HostAuthSaveResponse{}, err
	}
	var resp pluginapi.HostAuthSaveResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return pluginapi.HostAuthSaveResponse{}, fmt.Errorf("decode auth save response: %w", err)
	}
	return resp, nil
}

// Do performs one non-streaming upstream request through the host client.
func (cgoBridge) Do(_ context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	payload, err := json.Marshal(hostHTTPRequest{
		Method:      req.Method,
		URL:         req.URL,
		Headers:     req.Headers,
		Body:        req.Body,
		WireProfile: req.WireProfile,
	})
	if err != nil {
		return pluginapi.HTTPResponse{}, fmt.Errorf("encode http request: %w", err)
	}
	raw, err := callHost(pluginabi.MethodHostHTTPDo, payload)
	if err != nil {
		return pluginapi.HTTPResponse{}, err
	}
	var resp hostHTTPResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return pluginapi.HTTPResponse{}, fmt.Errorf("decode http response: %w", err)
	}
	return pluginapi.HTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    http.Header(resp.Headers),
		Body:       resp.Body,
	}, nil
}

// DoStream opens a host HTTP stream and forwards its chunks verbatim.
func (cgoBridge) DoStream(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pumpStream(ctx, cgoStreamOpener{}, req)
}

// cgoStreamOpener opens streams through the host function table.
type cgoStreamOpener struct{}

func (cgoStreamOpener) Open(_ context.Context, req pluginapi.HTTPRequest) (streamHandle, pluginapi.HTTPStreamResponse, error) {
	payload, err := json.Marshal(hostHTTPRequest{
		Method:      req.Method,
		URL:         req.URL,
		Headers:     req.Headers,
		Body:        req.Body,
		WireProfile: req.WireProfile,
	})
	if err != nil {
		return nil, pluginapi.HTTPStreamResponse{}, fmt.Errorf("encode stream request: %w", err)
	}
	raw, err := callHost(pluginabi.MethodHostHTTPDoStream, payload)
	if err != nil {
		return nil, pluginapi.HTTPStreamResponse{}, err
	}
	var opened hostHTTPStreamResponse
	if err := json.Unmarshal(raw, &opened); err != nil {
		return nil, pluginapi.HTTPStreamResponse{}, fmt.Errorf("decode stream response: %w", err)
	}
	handle := &cgoStreamHandle{streamID: opened.StreamID, pending: opened.Chunks}
	response := pluginapi.HTTPStreamResponse{
		StatusCode: opened.StatusCode,
		Headers:    http.Header(opened.Headers),
	}
	return handle, response, nil
}

// cgoStreamHandle reads a host stream either from the chunks bundled into the
// open response or, for a live stream, one read call at a time.
type cgoStreamHandle struct {
	streamID string
	pending  []pluginapi.HTTPStreamChunk
}

func (h *cgoStreamHandle) Read(_ context.Context) ([]byte, bool, error) {
	if len(h.pending) > 0 {
		chunk := h.pending[0]
		h.pending = h.pending[1:]
		if chunk.Err != nil {
			return nil, false, chunk.Err
		}
		return chunk.Payload, len(h.pending) == 0, nil
	}
	payload, err := json.Marshal(hostStreamRequest{StreamID: h.streamID})
	if err != nil {
		return nil, false, fmt.Errorf("encode stream read request: %w", err)
	}
	raw, err := callHost(pluginabi.MethodHostHTTPStreamRead, payload)
	if err != nil {
		return nil, false, err
	}
	var read hostStreamReadResponse
	if err := json.Unmarshal(raw, &read); err != nil {
		return nil, false, fmt.Errorf("decode stream read response: %w", err)
	}
	if read.Error != "" {
		return nil, false, errors.New(read.Error)
	}
	return read.Payload, read.Done, nil
}

func (h *cgoStreamHandle) Close(_ context.Context) error {
	if h.streamID == "" {
		return nil
	}
	payload, err := json.Marshal(hostStreamRequest{StreamID: h.streamID})
	if err != nil {
		return fmt.Errorf("encode stream close request: %w", err)
	}
	_, err = callHost(pluginabi.MethodHostHTTPStreamClose, payload)
	return err
}

type hostHTTPStreamResponse struct {
	StatusCode int                         `json:"status_code"`
	Headers    http.Header                 `json:"headers,omitempty"`
	StreamID   string                      `json:"stream_id,omitempty"`
	Chunks     []pluginapi.HTTPStreamChunk `json:"chunks,omitempty"`
}

type hostStreamRequest struct {
	StreamID string `json:"stream_id"`
}

type hostStreamReadResponse struct {
	Payload []byte `json:"payload,omitempty"`
	Error   string `json:"error,omitempty"`
	Done    bool   `json:"done,omitempty"`
}

type hostHTTPRequest struct {
	Method      string                     `json:"method,omitempty"`
	URL         string                     `json:"url,omitempty"`
	Headers     http.Header                `json:"headers,omitempty"`
	Body        []byte                     `json:"body,omitempty"`
	WireProfile *pluginapi.HTTPWireProfile `json:"wire_profile,omitempty"`
}

type hostHTTPResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers,omitempty"`
	Body       []byte      `json:"body,omitempty"`
}

// callHost invokes one host callback and returns its raw result bytes.
func callHost(method string, payload []byte) ([]byte, error) {
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))

	var response C.cliproxy_buffer
	var request *C.uint8_t
	if len(payload) > 0 {
		request = (*C.uint8_t)(C.CBytes(payload))
		defer C.free(unsafe.Pointer(request))
	}
	if C.call_host_api(cMethod, request, C.size_t(len(payload)), &response) != 0 {
		return nil, fmt.Errorf("host callback %s failed", method)
	}
	if response.ptr == nil {
		return nil, fmt.Errorf("host callback %s returned no response envelope", method)
	}
	raw := C.GoBytes(unsafe.Pointer(response.ptr), C.int(response.len))
	C.free_host_buffer(unsafe.Pointer(response.ptr), response.len)

	var decoded envelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", method, err)
	}
	if !decoded.OK {
		message := "host callback failed"
		if decoded.Error != nil && decoded.Error.Message != "" {
			message = decoded.Error.Message
		}
		return nil, fmt.Errorf("%s: %s", method, message)
	}
	return decoded.Result, nil
}
