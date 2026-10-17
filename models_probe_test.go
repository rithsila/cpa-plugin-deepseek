package deepseekcpa

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// probeBridge is a fake host that already holds one credential, so the model
// probe has a key to authenticate with.
func probeBridge(modelsBody string, status int) *fakeBridge {
	return &fakeBridge{
		auths: []pluginapi.HostAuthFileEntry{
			{Name: "ours.json", Provider: Provider, AuthIndex: "idx-1"},
		},
		authJSON: map[string][]byte{
			"idx-1": []byte(`{"type":"deepseek-cpa","api_key":"sk-probe"}`),
		},
		doResponse: pluginapi.HTTPResponse{StatusCode: status, Body: []byte(modelsBody)},
	}
}

const upstreamModels = `{"object":"list","data":[
  {"id":"deepseek-flash","object":"model","owned_by":"deepseek","name":"DeepSeek-V4.1-Flash"},
  {"id":"deepseek-v4-pro","object":"model","owned_by":"deepseek","name":"DeepSeek-V4-Pro"}]}`

func TestStaticModelsPublishesTheProbedList(t *testing.T) {
	bridge := probeBridge(upstreamModels, http.StatusOK)
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.StaticModels(context.Background(), pluginapi.StaticModelRequest{})
	if err != nil {
		t.Fatalf("StaticModels() error = %v", err)
	}
	if len(resp.Models) != 2 {
		t.Fatalf("models = %d, want the two upstream models", len(resp.Models))
	}
	if got := resp.Models[0].ID; got != Provider+"/deepseek-flash" {
		t.Errorf("model ID = %q, want the namespaced upstream id", got)
	}
	if got := resp.Models[0].Name; got != "deepseek-flash" {
		t.Errorf("Name = %q, want the bare upstream id", got)
	}
	if got := resp.Models[0].DisplayName; got != "DeepSeek-V4.1-Flash" {
		t.Errorf("DisplayName = %q, want the upstream display name", got)
	}

	sent := bridge.requests[0]
	if sent.Method != http.MethodGet || sent.URL != "https://api.deepseek.com/models" {
		t.Errorf("probe request = %s %s, want GET the models endpoint", sent.Method, sent.URL)
	}
	if got := sent.Headers.Get("Authorization"); got != "Bearer sk-probe" {
		t.Errorf("Authorization = %q, want the stored credential", got)
	}
}

func TestStaticModelsCachesAfterASuccessfulProbe(t *testing.T) {
	bridge := probeBridge(upstreamModels, http.StatusOK)
	plugin := newManagementPlugin(t, bridge)

	for i := 0; i < 3; i++ {
		if _, err := plugin.StaticModels(context.Background(), pluginapi.StaticModelRequest{}); err != nil {
			t.Fatalf("StaticModels() call %d error = %v", i, err)
		}
	}
	if len(bridge.requests) != 1 {
		t.Errorf("upstream probe calls = %d, want 1: the result is cached", len(bridge.requests))
	}
}

func TestStaticModelsPublishesOnlyWhatUpstreamLists(t *testing.T) {
	// The "[1m]" variant works upstream but is not advertised, so it must not
	// appear in the published list.
	bridge := probeBridge(upstreamModels, http.StatusOK)
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.StaticModels(context.Background(), pluginapi.StaticModelRequest{})
	if err != nil {
		t.Fatalf("StaticModels() error = %v", err)
	}
	for _, model := range resp.Models {
		if strings.Contains(model.ID, "[1m]") {
			t.Errorf("published %q, want only models the upstream lists", model.ID)
		}
	}
}

func TestStaticModelsFallsBackToBootstrapWithoutACredential(t *testing.T) {
	bridge := &fakeBridge{}
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.StaticModels(context.Background(), pluginapi.StaticModelRequest{})
	if err != nil {
		t.Fatalf("StaticModels() error = %v, want the bootstrap list instead of a failure", err)
	}
	if len(resp.Models) == 0 {
		t.Fatal("models = 0, want the bootstrap list")
	}
	if len(bridge.requests) != 0 {
		t.Error("upstream was probed with no credential available")
	}
}

func TestStaticModelsFallsBackWhenTheProbeFails(t *testing.T) {
	bridge := probeBridge(`{"error":"boom"}`, http.StatusInternalServerError)
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.StaticModels(context.Background(), pluginapi.StaticModelRequest{})
	if err != nil {
		t.Fatalf("StaticModels() error = %v, want the bootstrap list instead of a failure", err)
	}
	if len(resp.Models) == 0 {
		t.Fatal("models = 0, want the bootstrap list")
	}
	if !strings.Contains(strings.Join(bridge.logs, "\n"), "bootstrap") {
		t.Errorf("logs = %v, want a line explaining the fallback", bridge.logs)
	}
	if !strings.Contains(fieldText(bridge), "status 500") {
		t.Errorf("log fields = %v, want the upstream status recorded", bridge.logFields)
	}
}

func TestStaticModelsDoesNotCacheAFailedProbe(t *testing.T) {
	bridge := probeBridge(`{}`, http.StatusBadGateway)
	plugin := newManagementPlugin(t, bridge)

	if _, err := plugin.StaticModels(context.Background(), pluginapi.StaticModelRequest{}); err != nil {
		t.Fatalf("StaticModels() error = %v", err)
	}
	// A later successful probe must still be able to replace the fallback.
	bridge.doResponse = pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(upstreamModels)}
	resp, err := plugin.StaticModels(context.Background(), pluginapi.StaticModelRequest{})
	if err != nil {
		t.Fatalf("StaticModels() error = %v", err)
	}
	if got := resp.Models[0].ID; got != Provider+"/deepseek-flash" {
		t.Errorf("first model = %q, want the probed list after a successful retry", got)
	}
}

func TestRefreshRouteReprobesAndReportsTheList(t *testing.T) {
	bridge := probeBridge(upstreamModels, http.StatusOK)
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{
		Method: http.MethodPost,
		Path:   "/v0/management" + modelsRefreshRoutePath,
	})
	if err != nil {
		t.Fatalf("HandleManagement() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	want := []string{Provider + "/deepseek-flash", Provider + "/deepseek-v4-pro"}
	if len(body.Models) != len(want) || body.Models[0] != want[0] || body.Models[1] != want[1] {
		t.Errorf("models = %v, want %v", body.Models, want)
	}
	if cached := plugin.cachedModels(); len(cached) != 2 {
		t.Errorf("cached models = %d, want the refreshed list", len(cached))
	}
}

func TestRefreshRouteReportsProbeFailure(t *testing.T) {
	bridge := probeBridge(`{}`, http.StatusInternalServerError)
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{
		Method: http.MethodPost,
		Path:   modelsRefreshRoutePath,
	})
	if err != nil {
		t.Fatalf("HandleManagement() error = %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("StatusCode = %d, want 502", resp.StatusCode)
	}
	if !strings.Contains(string(resp.Body), "status 500") {
		t.Errorf("body = %s, want the upstream status in the error", resp.Body)
	}
}

func TestRefreshRouteRejectsOtherMethods(t *testing.T) {
	plugin := newManagementPlugin(t, probeBridge(upstreamModels, http.StatusOK))
	resp, err := plugin.HandleManagement(context.Background(), pluginapi.ManagementRequest{
		Method: http.MethodGet,
		Path:   modelsRefreshRoutePath,
	})
	if err != nil {
		t.Fatalf("HandleManagement() error = %v", err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("StatusCode = %d, want 405", resp.StatusCode)
	}
}

func TestProbeRejectsAPartiallyIdentifiableUpstreamList(t *testing.T) {
	// One bad entry means the payload cannot be trusted as the published list,
	// so the plugin keeps its bootstrap list instead of caching a partial one.
	bridge := probeBridge(`{"data":[{"id":"deepseek-flash"},{"name":"no id here"}]}`, http.StatusOK)
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.StaticModels(context.Background(), pluginapi.StaticModelRequest{})
	if err != nil {
		t.Fatalf("StaticModels() error = %v, want the bootstrap list instead of a failure", err)
	}
	if len(resp.Models) != len(defaultModels()) {
		t.Fatalf("models = %d, want the bootstrap list (%d)", len(resp.Models), len(defaultModels()))
	}
	if cached := plugin.cachedModels(); len(cached) != 0 {
		t.Errorf("cached %d models, want nothing cached from a malformed payload", len(cached))
	}
	if !strings.Contains(fieldText(bridge), "has no id") {
		t.Errorf("log fields = %v, want the probe failure reason recorded", bridge.logFields)
	}
}
