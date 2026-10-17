package deepseekcpa

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// usageEventOf finds the usage line and decodes its JSON payload.
func usageEventOf(t *testing.T, bridge *fakeBridge) (map[string]any, string) {
	t.Helper()
	for _, message := range bridge.logs {
		if !strings.HasPrefix(message, "info: "+usageMessage) {
			continue
		}
		raw := strings.TrimPrefix(message, "info: "+usageMessage+" ")
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("usage payload is not valid JSON: %v (raw=%s)", err, raw)
		}
		return decoded, raw
	}
	t.Fatalf("no %q log line; logs = %v", usageMessage, bridge.logs)
	return nil, ""
}

func TestHandleUsageLogsTheAgreedFields(t *testing.T) {
	bridge := &fakeBridge{}
	plugin := newManagementPlugin(t, bridge)

	plugin.HandleUsage(context.Background(), pluginapi.UsageRecord{
		RequestID:       "req-1",
		TraceID:         "trace-1",
		Provider:        Provider,
		Model:           "deepseek-cpa/deepseek-v4-pro",
		ResponseModel:   "deepseek-v4-pro",
		ReasoningEffort: "high",
		Stream:          true,
		RequestedAt:     time.Now(),
		Latency:         1500 * time.Millisecond,
		TTFT:            250 * time.Millisecond,
		Detail: pluginapi.UsageDetail{
			InputTokens:     100,
			OutputTokens:    40,
			ReasoningTokens: 10,
			CachedTokens:    7,
			TotalTokens:     140,
		},
	})

	event, _ := usageEventOf(t, bridge)
	// The key set is the contract: a renamed or dropped field fails here.
	want := map[string]any{
		"model":            "deepseek-cpa/deepseek-v4-pro",
		"response_model":   "deepseek-v4-pro",
		"provider":         Provider,
		"stream":           true,
		"failed":           false,
		"status":           float64(200),
		"input_tokens":     float64(100),
		"output_tokens":    float64(40),
		"reasoning_tokens": float64(10),
		"cached_tokens":    float64(7),
		"total_tokens":     float64(140),
		"latency_ms":       float64(1500),
		"ttft_ms":          float64(250),
		"request_id":       "req-1",
		"trace_id":         "trace-1",
		"reasoning_effort": "high",
	}
	if len(event) != len(want) {
		t.Errorf("field count = %d, want %d: %v", len(event), len(want), event)
	}
	for key, expected := range want {
		if got := event[key]; got != expected {
			t.Errorf("field %q = %#v, want %#v", key, got, expected)
		}
	}
}

func TestHandleUsageLogsTheRealStatusForFailures(t *testing.T) {
	bridge := &fakeBridge{}
	plugin := newManagementPlugin(t, bridge)

	plugin.HandleUsage(context.Background(), pluginapi.UsageRecord{
		Model:  "deepseek-cpa/deepseek-v4-pro",
		Failed: true,
		Failure: pluginapi.UsageFailure{
			StatusCode: 429,
			Body:       `{"error":{"message":"slow down"}}`,
		},
	})

	event, _ := usageEventOf(t, bridge)
	if got := event["status"]; got != float64(429) {
		t.Errorf("status = %#v, want the upstream status 429", got)
	}
	if got := event["failed"]; got != true {
		t.Errorf("failed = %#v, want true", got)
	}
}

func TestHandleUsageNeverLogsSecretsOrBodies(t *testing.T) {
	const secret = "sk-should-never-appear"
	const body = "prompt-and-completion-body-that-must-not-be-logged"
	bridge := &fakeBridge{}
	plugin := newManagementPlugin(t, bridge)

	plugin.HandleUsage(context.Background(), pluginapi.UsageRecord{
		Model:          "deepseek-cpa/deepseek-v4-pro",
		APIKey:         secret,
		AuthID:         "auth-id",
		AuthIndex:      "idx",
		BaseURL:        "https://api.deepseek.com",
		AccessProvider: "config-inline",
		Failed:         true,
		Failure:        pluginapi.UsageFailure{StatusCode: 401, Body: body},
	})

	event, raw := usageEventOf(t, bridge)
	for _, forbidden := range []string{secret, body} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("usage line leaks %q: %s", forbidden, raw)
		}
	}
	for key := range event {
		switch key {
		case "api_key", "body", "failure_body", "storage_json", "credential", "auth_index":
			t.Errorf("usage field %q must not be emitted", key)
		}
	}
}

func TestHandleUsageOmitsEmptyOptionalFields(t *testing.T) {
	bridge := &fakeBridge{}
	plugin := newManagementPlugin(t, bridge)

	plugin.HandleUsage(context.Background(), pluginapi.UsageRecord{Model: "deepseek-cpa/deepseek-v4-pro"})

	event, _ := usageEventOf(t, bridge)
	for _, key := range []string{"request_id", "trace_id", "response_model", "reasoning_effort", "provider"} {
		if _, present := event[key]; present {
			t.Errorf("field %q was emitted although the record had no value for it", key)
		}
	}
}
