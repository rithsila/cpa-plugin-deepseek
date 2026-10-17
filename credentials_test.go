package deepseekcpa

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// fakeBridge is the host seam: no host process, no network, no surprise writes.
type fakeBridge struct {
	logs       []string
	auths      []pluginapi.HostAuthFileEntry
	saved      []pluginapi.HostAuthSaveRequest
	requests   []pluginapi.HTTPRequest
	listErr    error
	saveErr    error
	doResponse pluginapi.HTTPResponse
	doErr      error
	stream     pluginapi.HTTPStreamResponse
	streamErr  error
	authJSON   map[string][]byte
}

func (f *fakeBridge) Log(_ context.Context, level, message string) error {
	f.logs = append(f.logs, level+": "+message)
	return nil
}

func (f *fakeBridge) ListAuths(context.Context) ([]pluginapi.HostAuthFileEntry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.auths, nil
}

func (f *fakeBridge) GetAuth(_ context.Context, req pluginapi.HostAuthGetRequest) (pluginapi.HostAuthGetResponse, error) {
	payload, ok := f.authJSON[req.AuthIndex]
	if !ok {
		return pluginapi.HostAuthGetResponse{}, errors.New("fake bridge: no such auth")
	}
	return pluginapi.HostAuthGetResponse{AuthIndex: req.AuthIndex, JSON: payload}, nil
}

func (f *fakeBridge) SaveAuth(_ context.Context, req pluginapi.HostAuthSaveRequest) (pluginapi.HostAuthSaveResponse, error) {
	if f.saveErr != nil {
		return pluginapi.HostAuthSaveResponse{}, f.saveErr
	}
	f.saved = append(f.saved, req)
	return pluginapi.HostAuthSaveResponse{Name: req.Name, Path: "/tmp/" + req.Name}, nil
}

func (f *fakeBridge) Do(_ context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	if f.doErr != nil {
		return pluginapi.HTTPResponse{}, f.doErr
	}
	f.requests = append(f.requests, req)
	return f.doResponse, nil
}

func (f *fakeBridge) DoStream(_ context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	if f.streamErr != nil {
		return pluginapi.HTTPStreamResponse{}, f.streamErr
	}
	f.requests = append(f.requests, req)
	return f.stream, nil
}

// streamOf turns fixed payloads into a closed stream response.
func streamOf(status int, payloads ...string) pluginapi.HTTPStreamResponse {
	chunks := make(chan pluginapi.HTTPStreamChunk, len(payloads))
	for _, payload := range payloads {
		chunks <- pluginapi.HTTPStreamChunk{Payload: []byte(payload)}
	}
	close(chunks)
	return pluginapi.HTTPStreamResponse{StatusCode: status, Chunks: chunks}
}

func newManagementPlugin(t *testing.T, bridge *fakeBridge) *Plugin {
	t.Helper()
	_, plugin, err := Build(nil, bridge)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	return plugin
}

func TestParseAuthRecognisesOwnCredential(t *testing.T) {
	plugin := newManagementPlugin(t, &fakeBridge{})
	raw := []byte(`{"type":"deepseek-cpa","api_key":"sk-deepseek-test"}`)

	resp, err := plugin.ParseAuth(context.Background(), pluginapi.AuthParseRequest{
		FileName: "deepseek-cpa-1.json",
		RawJSON:  raw,
	})
	if err != nil {
		t.Fatalf("ParseAuth() error = %v", err)
	}
	if !resp.Handled {
		t.Fatal("Handled = false, want true")
	}
	if resp.Auth.Provider != Provider {
		t.Errorf("Provider = %q, want %q", resp.Auth.Provider, Provider)
	}
	if resp.Auth.FileName != "deepseek-cpa-1.json" {
		t.Errorf("FileName = %q, want the offered file name", resp.Auth.FileName)
	}
	if string(resp.Auth.StorageJSON) != string(raw) {
		t.Errorf("StorageJSON = %q, want the original payload", resp.Auth.StorageJSON)
	}
}

func TestParseAuthIgnoresOtherProviders(t *testing.T) {
	plugin := newManagementPlugin(t, &fakeBridge{})
	cases := map[string]string{
		"other type":    `{"type":"claude","api_key":"sk-x"}`,
		"other key":     `{"provider":"openai-compatibility","api_key":"sk-x"}`,
		"no type":       `{"api_key":"sk-x"}`,
		"not an object": `"just-a-string"`,
		"malformed":     `{`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			resp, err := plugin.ParseAuth(context.Background(), pluginapi.AuthParseRequest{RawJSON: []byte(raw)})
			if err != nil {
				t.Fatalf("ParseAuth() error = %v, want nil so the next parser runs", err)
			}
			if resp.Handled {
				t.Fatal("Handled = true, want false")
			}
		})
	}
}

func TestParseAuthRejectsOwnCredentialWithoutKey(t *testing.T) {
	plugin := newManagementPlugin(t, &fakeBridge{})
	_, err := plugin.ParseAuth(context.Background(), pluginapi.AuthParseRequest{
		RawJSON: []byte(`{"type":"deepseek-cpa"}`),
	})
	if err == nil {
		t.Fatal("ParseAuth() error = nil, want a missing api_key error")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Errorf("error = %q, want it to mention api_key", err.Error())
	}
}

func TestParseAuthFailsLoudlyOnBrokenOwnCredential(t *testing.T) {
	plugin := newManagementPlugin(t, &fakeBridge{})
	// Ours by declaration, but api_key has the wrong type: this must not be
	// mistaken for another provider's file.
	_, err := plugin.ParseAuth(context.Background(), pluginapi.AuthParseRequest{
		RawJSON: []byte(`{"type":"deepseek-cpa","api_key":123}`),
	})
	if err == nil {
		t.Fatal("ParseAuth() error = nil, want a malformed-credential error")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("error = %q, want it to say the credential is malformed", err.Error())
	}
}

func TestLoginAndRefreshReportNoInteractiveFlow(t *testing.T) {
	plugin := newManagementPlugin(t, &fakeBridge{})
	ctx := context.Background()

	loginWants := "no interactive login flow"
	refreshWants := "do not expire"

	cases := []struct {
		name string
		call func() error
		want string
	}{
		{"start login", func() error {
			_, err := plugin.StartLogin(ctx, pluginapi.AuthLoginStartRequest{})
			return err
		}, loginWants},
		{"poll login", func() error {
			_, err := plugin.PollLogin(ctx, pluginapi.AuthLoginPollRequest{})
			return err
		}, loginWants},
		{"refresh", func() error {
			_, err := plugin.RefreshAuth(ctx, pluginapi.AuthRefreshRequest{})
			return err
		}, refreshWants},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("error = %v, want ErrUnsupported: this is permanent, not pending work", err)
			}
			if errors.Is(err, ErrNotImplemented) {
				t.Errorf("error = %v, must not claim to be unimplemented", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to explain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestRegisterManagementDeclaresKeyRoutes(t *testing.T) {
	plugin := newManagementPlugin(t, &fakeBridge{})
	resp, err := plugin.RegisterManagement(context.Background(), pluginapi.ManagementRegistrationRequest{})
	if err != nil {
		t.Fatalf("RegisterManagement() error = %v", err)
	}
	want := map[string]string{
		http.MethodGet + " " + keysRoutePath:           "list keys",
		http.MethodPost + " " + keysRoutePath:          "add key",
		http.MethodDelete + " " + keysRoutePath:        "remove key",
		http.MethodPost + " " + modelsRefreshRoutePath: "refresh models",
	}
	if len(resp.Routes) != len(want) {
		t.Fatalf("routes = %d, want %d", len(resp.Routes), len(want))
	}
	for _, route := range resp.Routes {
		if _, ok := want[route.Method+" "+route.Path]; !ok {
			t.Errorf("unexpected route %s %s", route.Method, route.Path)
		}
		if route.Handler == nil {
			t.Errorf("route %s has no handler", route.Method)
		}
	}
	if len(resp.Resources) != 0 {
		t.Errorf("Resources = %d, want none: keys must sit behind the management key", len(resp.Resources))
	}
}

func TestListKeysFiltersByProviderAndHidesSecrets(t *testing.T) {
	bridge := &fakeBridge{auths: []pluginapi.HostAuthFileEntry{
		{ID: "a", Name: "ours-1.json", Provider: Provider, Label: "main", Status: "active"},
		{ID: "b", Name: "claude.json", Provider: "claude"},
	}}
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{Method: http.MethodGet, Path: keysRoutePath})
	if err != nil {
		t.Fatalf("HandleManagement() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Keys []struct {
			Name  string `json:"name"`
			Label string `json:"label"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(body.Keys) != 1 || body.Keys[0].Name != "ours-1.json" {
		t.Fatalf("keys = %+v, want only the deepseek-cpa entry", body.Keys)
	}
	if strings.Contains(string(resp.Body), "claude.json") {
		t.Error("body mentions another provider's credential")
	}
}

func TestAddKeySavesCredentialThroughHost(t *testing.T) {
	bridge := &fakeBridge{}
	plugin := newManagementPlugin(t, bridge)
	const secret = "sk-deepseek-secret"

	body, err := json.Marshal(map[string]string{"api_key": secret})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{
		Method: http.MethodPost,
		Path:   keysRoutePath,
		Body:   body,
	})
	if err != nil {
		t.Fatalf("HandleManagement() error = %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("StatusCode = %d, want 201", resp.StatusCode)
	}
	if len(bridge.saved) != 1 {
		t.Fatalf("SaveAuth calls = %d, want 1", len(bridge.saved))
	}
	saved := bridge.saved[0]
	if !strings.HasSuffix(saved.Name, ".json") {
		t.Errorf("saved name = %q, want a .json file name", saved.Name)
	}
	var stored map[string]any
	if err := json.Unmarshal(saved.JSON, &stored); err != nil {
		t.Fatalf("unmarshal saved JSON: %v", err)
	}
	if stored["api_key"] != secret || stored["type"] != Provider {
		t.Errorf("saved JSON = %v, want type %q and the provided api_key", stored, Provider)
	}
	for _, line := range bridge.logs {
		if strings.Contains(line, secret) {
			t.Errorf("log line leaks the key: %q", line)
		}
	}
}

func TestAddKeyRejectsMissingAPIKey(t *testing.T) {
	bridge := &fakeBridge{}
	plugin := newManagementPlugin(t, bridge)
	resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{
		Method: http.MethodPost,
		Path:   keysRoutePath,
		Body:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("HandleManagement() error = %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want 400", resp.StatusCode)
	}
	if len(bridge.saved) != 0 {
		t.Error("SaveAuth was called for an invalid request")
	}
}

func TestDeleteKeyRemovesOnlyOurAuthFile(t *testing.T) {
	dir := t.TempDir()
	ours := filepath.Join(dir, "ours.json")
	if err := os.WriteFile(ours, []byte(`{"type":"deepseek-cpa","api_key":"sk-x"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	other := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(other, []byte(`{"type":"claude"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	bridge := &fakeBridge{auths: []pluginapi.HostAuthFileEntry{
		{Name: "ours.json", Provider: Provider, Path: ours},
		{Name: "claude.json", Provider: "claude", Path: other},
	}}
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{
		Method: http.MethodDelete,
		Path:   keysRoutePath,
		Query:  map[string][]string{"name": {"ours.json"}},
	})
	if err != nil {
		t.Fatalf("HandleManagement() error = %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("StatusCode = %d, want 204", resp.StatusCode)
	}
	if _, err := os.Stat(ours); !os.IsNotExist(err) {
		t.Error("our auth file still exists, want it removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("another provider's auth file was touched")
	}
}

func TestDeleteKeyRefusesUnknownOrForeignName(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(other, []byte(`{"type":"claude"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	bridge := &fakeBridge{auths: []pluginapi.HostAuthFileEntry{
		{Name: "claude.json", Provider: "claude", Path: other},
	}}
	plugin := newManagementPlugin(t, bridge)

	for _, name := range []string{"", "missing.json", "claude.json"} {
		t.Run("name="+name, func(t *testing.T) {
			resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{
				Method: http.MethodDelete,
				Path:   keysRoutePath,
				Query:  map[string][]string{"name": {name}},
			})
			if err != nil {
				t.Fatalf("HandleManagement() error = %v", err)
			}
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("StatusCode = %d, want 404", resp.StatusCode)
			}
			if _, err := os.Stat(other); err != nil {
				t.Fatal("another provider's auth file was removed")
			}
		})
	}
}

func TestHandleManagementAcceptsTheFullHostPath(t *testing.T) {
	bridge := &fakeBridge{auths: []pluginapi.HostAuthFileEntry{
		{Name: "ours.json", Provider: Provider},
	}}
	plugin := newManagementPlugin(t, bridge)
	resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{
		Method: http.MethodGet,
		Path:   "/v0/management" + keysRoutePath,
	})
	if err != nil {
		t.Fatalf("HandleManagement() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200 for the full host path", resp.StatusCode)
	}
}

func TestDeleteKeyIsIdempotentWhenTheFileIsAlreadyGone(t *testing.T) {
	// The host's credential list lags the auth directory, so a name can still
	// be listed after its file disappeared.
	bridge := &fakeBridge{auths: []pluginapi.HostAuthFileEntry{
		{Name: "gone.json", Provider: Provider, Path: filepath.Join(t.TempDir(), "gone.json")},
	}}
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{
		Method: http.MethodDelete,
		Path:   keysRoutePath,
		Query:  map[string][]string{"name": {"gone.json"}},
	})
	if err != nil {
		t.Fatalf("HandleManagement() error = %v, want nil for an already-removed credential", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("StatusCode = %d, want 204", resp.StatusCode)
	}
}

func TestUnknownManagementRouteIsNotFound(t *testing.T) {
	plugin := newManagementPlugin(t, &fakeBridge{})
	resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{
		Method: http.MethodGet,
		Path:   "/plugins/deepseek-cpa/nope",
	})
	if err != nil {
		t.Fatalf("HandleManagement() error = %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want 404", resp.StatusCode)
	}
}
