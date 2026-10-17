package deepseekcpa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// keysRoutePath is the single Management API route for credential operations.
// It sits under /v0/management, so the host enforces the management key before
// the handler runs. Method distinguishes list, add, and remove.
const keysRoutePath = "/plugins/" + Provider + "/keys"

// credentialTypeField is the credential JSON field that decides which provider
// owns an auth file. DeepSeek keys carry no other identifying marker.
const credentialTypeField = "type"

// defaultKeyLabel is used when a credential file carries no label of its own.
const defaultKeyLabel = "DeepSeek key"

// ParseAuth recognises DeepSeek credential files. Files belonging to another
// provider must return Handled=false so the host keeps offering them to the
// next parser.
func (p *Plugin) ParseAuth(_ context.Context, req pluginapi.AuthParseRequest) (pluginapi.AuthParseResponse, error) {
	credential, ours, err := decodeCredential(req.RawJSON)
	if err != nil {
		return pluginapi.AuthParseResponse{}, fmt.Errorf("auth.parse: %w", err)
	}
	if !ours {
		return pluginapi.AuthParseResponse{Handled: false}, nil
	}
	if credential.APIKey == "" {
		return pluginapi.AuthParseResponse{}, fmt.Errorf("auth.parse: %s credential is missing api_key", Provider)
	}
	label := strings.TrimSpace(credential.Label)
	if label == "" {
		label = defaultKeyLabel
	}
	return pluginapi.AuthParseResponse{
		Handled: true,
		Auth: pluginapi.AuthData{
			Provider:    Provider,
			FileName:    req.FileName,
			Label:       label,
			StorageJSON: req.RawJSON,
		},
	}, nil
}

// RegisterManagement declares the credential routes. Secrets stay behind the
// management key, so nothing is exposed as an unauthenticated resource page.
func (p *Plugin) RegisterManagement(context.Context, pluginapi.ManagementRegistrationRequest) (pluginapi.ManagementRegistrationResponse, error) {
	handler := p
	return pluginapi.ManagementRegistrationResponse{
		Routes: []pluginapi.ManagementRoute{
			{Method: http.MethodGet, Path: keysRoutePath, Description: "List DeepSeek credentials (never their keys).", Handler: handler},
			{Method: http.MethodPost, Path: keysRoutePath, Description: "Add a DeepSeek credential.", Handler: handler},
			{Method: http.MethodDelete, Path: keysRoutePath, Description: "Remove a DeepSeek credential by name.", Handler: handler},
		},
	}, nil
}

// managementPrefixes are the API prefixes the host mounts plugin routes under.
// The host passes the full request path, so the route match has to strip it.
var managementPrefixes = []string{"/v0/management", "/v8/management"}

// HandleManagement dispatches one Management API request to the key routes.
func (p *Plugin) HandleManagement(ctx context.Context, req pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	if managementSubPath(req.Path) != keysRoutePath {
		return jsonResponse(http.StatusNotFound, map[string]string{"error": "unknown route"})
	}
	switch req.Method {
	case http.MethodGet:
		return p.listKeys(ctx)
	case http.MethodPost:
		return p.addKey(ctx, req.Body)
	case http.MethodDelete:
		return p.removeKey(ctx, req.Query["name"])
	default:
		return jsonResponse(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func managementSubPath(path string) string {
	for _, prefix := range managementPrefixes {
		if strings.HasPrefix(path, prefix) {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

type keySummary struct {
	Name      string `json:"name"`
	Label     string `json:"label,omitempty"`
	Status    string `json:"status,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	Available bool   `json:"available"`
}

func (p *Plugin) listKeys(ctx context.Context) (pluginapi.ManagementResponse, error) {
	entries, err := p.bridge.ListAuths(ctx)
	if err != nil {
		return pluginapi.ManagementResponse{}, fmt.Errorf("management.list_keys: %w", err)
	}
	keys := make([]keySummary, 0, len(entries))
	for _, entry := range entries {
		if entry.Provider != Provider {
			continue
		}
		keys = append(keys, keySummary{
			Name:      entry.Name,
			Label:     entry.Label,
			Status:    entry.Status,
			Disabled:  entry.Disabled,
			Available: !entry.Disabled && !entry.Unavailable,
		})
	}
	return jsonResponse(http.StatusOK, map[string]any{"keys": keys})
}

func (p *Plugin) addKey(ctx context.Context, body []byte) (pluginapi.ManagementResponse, error) {
	// The host rewrites the stored credential through its own metadata model,
	// which drops any label we put in the file, so adding a key takes only the
	// secret. The host owns the label shown in its credential list.
	var request struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "body must be a JSON object with api_key"})
	}
	apiKey := strings.TrimSpace(request.APIKey)
	if apiKey == "" {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "api_key is required"})
	}

	stored := map[string]string{
		credentialTypeField: Provider,
		"api_key":           apiKey,
	}
	payload, err := json.Marshal(stored)
	if err != nil {
		return pluginapi.ManagementResponse{}, fmt.Errorf("management.add_key: %w", err)
	}

	name := credentialFileName(apiKey)
	saved, err := p.bridge.SaveAuth(ctx, pluginapi.HostAuthSaveRequest{Name: name, JSON: payload})
	if err != nil {
		return pluginapi.ManagementResponse{}, fmt.Errorf("management.add_key: %w", err)
	}
	if err := p.bridge.Log(ctx, "info", "credential stored as "+saved.Name); err != nil {
		return pluginapi.ManagementResponse{}, fmt.Errorf("management.add_key: %w", err)
	}
	return jsonResponse(http.StatusCreated, map[string]string{"name": saved.Name})
}

func (p *Plugin) removeKey(ctx context.Context, names []string) (pluginapi.ManagementResponse, error) {
	if len(names) == 0 {
		return jsonResponse(http.StatusNotFound, map[string]string{"error": "name query parameter is required"})
	}
	name := strings.TrimSpace(names[0])
	entries, err := p.bridge.ListAuths(ctx)
	if err != nil {
		return pluginapi.ManagementResponse{}, fmt.Errorf("management.remove_key: %w", err)
	}
	for _, entry := range entries {
		if entry.Name != name || entry.Provider != Provider {
			continue
		}
		if entry.Path == "" {
			return pluginapi.ManagementResponse{}, fmt.Errorf("management.remove_key: host reported no path for %s", name)
		}
		// No host callback deletes credentials, so removal targets the exact
		// file the host attributed to this provider; the host auth-dir watcher
		// picks the removal up.
		//
		// The host's credential list lags the auth directory, so a name can
		// still be listed after its file is gone. Removal is idempotent: an
		// already-absent file means the credential is already gone.
		if err := os.Remove(entry.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return pluginapi.ManagementResponse{}, fmt.Errorf("management.remove_key: %w", err)
		}
		// The file name is user-controlled and could itself be a key, so the
		// log line carries no identifier.
		if err := p.bridge.Log(ctx, "info", "credential removed"); err != nil {
			return pluginapi.ManagementResponse{}, fmt.Errorf("management.remove_key: %w", err)
		}
		return pluginapi.ManagementResponse{StatusCode: http.StatusNoContent}, nil
	}
	return jsonResponse(http.StatusNotFound, map[string]string{"error": "credential not found: " + name})
}

// credential is the subset of a DeepSeek auth file the plugin understands.
type credential struct {
	APIKey string `json:"api_key"`
	Label  string `json:"label"`
}

// decodeCredential reports whether the payload belongs to this provider, and
// fails loudly when one of our own files is malformed. Attribution is decided
// from the raw JSON first, so a broken field in our own credential surfaces as
// an error instead of being mistaken for someone else's file.
func decodeCredential(raw []byte) (credential, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return credential{}, false, nil
	}
	if declaredType(fields) != Provider {
		return credential{}, false, nil
	}
	var parsed credential
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return credential{}, true, fmt.Errorf("%s credential is malformed: %w", Provider, err)
	}
	return parsed, true, nil
}

// declaredType reads the credential's self-declared owner, accepting either
// spelling DeepSeek auth files use.
func declaredType(fields map[string]json.RawMessage) string {
	for _, key := range []string{credentialTypeField, "provider"} {
		var value string
		if w := fields[key]; w != nil && json.Unmarshal(w, &value) == nil {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// credentialFileName derives a stable file name from the key, so adding the
// same key twice updates one credential instead of piling up duplicates. The
// key itself never appears in the name.
func credentialFileName(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return Provider + "-" + hex.EncodeToString(sum[:])[:12] + ".json"
}

func jsonResponse(status int, body any) (pluginapi.ManagementResponse, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return pluginapi.ManagementResponse{}, fmt.Errorf("management response: %w", err)
	}
	return pluginapi.ManagementResponse{
		StatusCode: status,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       raw,
	}, nil
}
