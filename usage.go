package deepseekcpa

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// usageMessage prefixes the one log line each completed request produces.
const usageMessage = "request usage"

// usageEvent is the structured record written for a completed request.
//
// It travels as JSON inside the log message rather than as native log fields:
// the host's log formatter renders only a fixed whitelist of field names
// (provider, model, error, reason, ...), so token counts, status and latency are
// dropped when passed as fields. A JSON body survives intact and stays
// machine-parseable.
type usageEvent struct {
	Model           string `json:"model"`
	ResponseModel   string `json:"response_model,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Stream          bool   `json:"stream"`
	Failed          bool   `json:"failed"`
	Status          int    `json:"status"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	ReasoningTokens int64  `json:"reasoning_tokens"`
	CachedTokens    int64  `json:"cached_tokens"`
	TotalTokens     int64  `json:"total_tokens"`
	LatencyMS       int64  `json:"latency_ms"`
	TTFTMS          int64  `json:"ttft_ms"`
	RequestID       string `json:"request_id,omitempty"`
	TraceID         string `json:"trace_id,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// HandleUsage logs one structured record per completed request.
//
// The protocol surface cannot be logged: the host's usage record carries no
// format field, and its request id is minted by the host's usage reporter rather
// than correlated with the executor request, so the surface is not recoverable
// here.
//
// Never logged: the client API key identifier, the upstream failure body, or any
// credential material.
func (p *Plugin) HandleUsage(ctx context.Context, record pluginapi.UsageRecord) {
	status := 200
	if record.Failed {
		status = record.Failure.StatusCode
	}
	event := usageEvent{
		Model:           record.Model,
		ResponseModel:   record.ResponseModel,
		Provider:        record.Provider,
		Stream:          record.Stream,
		Failed:          record.Failed,
		Status:          status,
		InputTokens:     record.Detail.InputTokens,
		OutputTokens:    record.Detail.OutputTokens,
		ReasoningTokens: record.Detail.ReasoningTokens,
		CachedTokens:    record.Detail.CachedTokens,
		TotalTokens:     record.Detail.TotalTokens,
		LatencyMS:       record.Latency.Milliseconds(),
		TTFTMS:          record.TTFT.Milliseconds(),
		RequestID:       record.RequestID,
		TraceID:         record.TraceID,
		ReasoningEffort: record.ReasoningEffort,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		panic("usageEvent: marshaling a struct of scalars cannot fail: " + err.Error())
	}
	// A failed log call has no receiver: the record is the last step of the
	// request and the host logger is the only sink.
	_ = p.bridge.Log(ctx, "info", fmt.Sprintf("%s %s", usageMessage, payload), nil)
}
