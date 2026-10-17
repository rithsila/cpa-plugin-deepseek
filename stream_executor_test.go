package deepseekcpa

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func streamRequest(model, payload, format string) pluginapi.ExecutorRequest {
	req := executeRequest(model, payload, format)
	req.Stream = true
	return req
}

// collect drains a stream and returns each frame's bytes.
func collect(t *testing.T, chunks <-chan pluginapi.ExecutorStreamChunk) []string {
	t.Helper()
	var out []string
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v, want payload chunks only", chunk.Err)
		}
		out = append(out, string(chunk.Payload))
	}
	return out
}

func TestStreamForwardsEveryFrameVerbatim(t *testing.T) {
	frames := []string{
		"event: response.created\ndata: {\"type\":\"response.created\"}\n\n",
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hel\"}\n\n",
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"lo\"}\n\n",
		"event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n",
	}
	bridge := &fakeBridge{stream: streamOf(200, frames...)}
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.ExecuteStream(context.Background(),
		streamRequest(Provider+"/deepseek-v4-pro", `{"model":"`+Provider+`/deepseek-v4-pro","stream":true}`, formatResponses))
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	got := collect(t, resp.Chunks)
	if len(got) != len(frames) {
		t.Fatalf("frames = %d, want %d (a completed stream must not gain a failure frame)", len(got), len(frames))
	}
	for i := range frames {
		if got[i] != frames[i] {
			t.Errorf("frame %d = %q, want %q", i, got[i], frames[i])
		}
	}
}

func TestStreamEndingWithoutTerminalGetsFailureFrame(t *testing.T) {
	frames := []string{
		"event: response.created\ndata: {\"type\":\"response.created\"}\n\n",
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"par\"}\n\n",
	}
	bridge := &fakeBridge{stream: streamOf(200, frames...)}
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.ExecuteStream(context.Background(),
		streamRequest(Provider+"/deepseek-v4-pro", `{"model":"x","stream":true}`, formatResponses))
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	got := collect(t, resp.Chunks)
	if len(got) != len(frames)+1 {
		t.Fatalf("frames = %d, want %d including the synthesized failure", len(got), len(frames)+1)
	}
	for i := range frames {
		if got[i] != frames[i] {
			t.Errorf("frame %d = %q, want the upstream frame untouched", i, got[i])
		}
	}
	last := got[len(got)-1]
	if !strings.HasPrefix(last, "event: response.failed\n") {
		t.Errorf("last frame = %q, want a response.failed event", last)
	}
	if !strings.Contains(last, `"status":"failed"`) {
		t.Errorf("last frame = %q, want status failed", last)
	}
	if !strings.Contains(last, errStreamEndedWithoutTerminal.Error()) {
		t.Errorf("last frame = %q, want the truncation reason", last)
	}
}

func TestStreamIgnoresTerminalNamesInsideDeltaText(t *testing.T) {
	// The model is quoting the marker; that is not the end of the stream.
	frames := []string{
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"response.completed\"}\n\n",
	}
	bridge := &fakeBridge{stream: streamOf(200, frames...)}
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.ExecuteStream(context.Background(),
		streamRequest(Provider+"/deepseek-v4-pro", `{"model":"x","stream":true}`, formatResponses))
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	got := collect(t, resp.Chunks)
	if len(got) != len(frames)+1 {
		t.Fatalf("frames = %d, want the delta plus a failure frame", len(got))
	}
	if !strings.HasPrefix(got[len(got)-1], "event: response.failed\n") {
		t.Errorf("last frame = %q, want a response.failed event", got[len(got)-1])
	}
}

func TestStreamTreatsEachProtocolTerminatorAsComplete(t *testing.T) {
	cases := []struct {
		name   string
		format string
		frames []string
	}{
		{
			name:   "responses",
			format: formatResponses,
			frames: []string{"event: response.created\ndata: {\"type\":\"response.created\"}\n\n",
				"event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"},
		},
		{
			name:   "anthropic",
			format: formatAnthropic,
			frames: []string{"event: message_start\ndata: {\"type\":\"message_start\"}\n\n",
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"},
		},
		{
			name:   "chat",
			format: formatChat,
			frames: []string{"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n",
				"data: [DONE]\n\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bridge := &fakeBridge{stream: streamOf(200, tc.frames...)}
			plugin := newManagementPlugin(t, bridge)
			resp, err := plugin.ExecuteStream(context.Background(),
				streamRequest(Provider+"/deepseek-v4-pro", `{"model":"x","stream":true}`, tc.format))
			if err != nil {
				t.Fatalf("ExecuteStream() error = %v", err)
			}
			got := collect(t, resp.Chunks)
			if len(got) != len(tc.frames) {
				t.Fatalf("frames = %d, want %d: a normally terminated stream must not gain a failure frame (%q)",
					len(got), len(tc.frames), got)
			}
		})
	}
}

func TestStreamTruncatedChatEndsWithErrorAndDone(t *testing.T) {
	bridge := &fakeBridge{stream: streamOf(200, "data: {\"choices\":[]}\n\n")}
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.ExecuteStream(context.Background(),
		streamRequest(Provider+"/deepseek-v4-pro", `{"model":"x","stream":true}`, formatChat))
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	got := collect(t, resp.Chunks)
	last := got[len(got)-1]
	if !strings.Contains(last, `"error"`) || !strings.HasSuffix(last, "data: [DONE]\n\n") {
		t.Errorf("last frame = %q, want an error chunk followed by [DONE]", last)
	}
}

func TestStreamDetectsTerminalSplitAcrossChunks(t *testing.T) {
	// The marker is cut in two by the chunk boundary.
	frames := []string{
		"event: response.output_text.delta\ndata: {\"delta\":\"x\"}\n\nevent: response.comp",
		"leted\ndata: {\"type\":\"response.completed\"}\n\n",
	}
	bridge := &fakeBridge{stream: streamOf(200, frames...)}
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.ExecuteStream(context.Background(),
		streamRequest(Provider+"/deepseek-v4-pro", `{"model":"x","stream":true}`, formatResponses))
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	got := collect(t, resp.Chunks)
	if len(got) != len(frames) {
		t.Fatalf("frames = %d, want %d: a split terminal marker must still be seen", len(got), len(frames))
	}
}

func TestStreamUpstreamErrorBecomesFailureFrame(t *testing.T) {
	upstreamErr := errors.New("upstream reset")
	chunks := make(chan pluginapi.HTTPStreamChunk, 2)
	chunks <- pluginapi.HTTPStreamChunk{Payload: []byte("event: response.created\ndata: {}\n\n")}
	chunks <- pluginapi.HTTPStreamChunk{Err: upstreamErr}
	close(chunks)

	bridge := &fakeBridge{stream: pluginapi.HTTPStreamResponse{StatusCode: 200, Chunks: chunks}}
	plugin := newManagementPlugin(t, bridge)

	resp, err := plugin.ExecuteStream(context.Background(),
		streamRequest(Provider+"/deepseek-v4-pro", `{"model":"x","stream":true}`, formatResponses))
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	got := collect(t, resp.Chunks)
	if len(got) != 2 {
		t.Fatalf("frames = %d, want the prefix frame plus one failure frame", len(got))
	}
	if !strings.HasPrefix(got[1], "event: response.failed\n") {
		t.Errorf("frame = %q, want a response.failed event", got[1])
	}
	if !strings.Contains(got[1], upstreamErr.Error()) {
		t.Errorf("frame = %q, want it to carry the upstream cause", got[1])
	}
}

func TestStreamNonSuccessfulStatusReturnsUpstreamError(t *testing.T) {
	const body = `{"error":{"message":"invalid key","type":"authentication_error"}}`
	bridge := &fakeBridge{stream: streamOf(401, body)}
	plugin := newManagementPlugin(t, bridge)

	_, err := plugin.ExecuteStream(context.Background(),
		streamRequest(Provider+"/deepseek-v4-pro", `{"model":"x","stream":true}`, formatResponses))
	if err == nil {
		t.Fatal("ExecuteStream() error = nil, want the upstream failure")
	}
	var status interface{ StatusCode() int }
	if !errors.As(err, &status) || status.StatusCode() != 401 {
		t.Fatalf("error = %v, want StatusCode() == 401", err)
	}
	if err.Error() != body {
		t.Errorf("error message = %q, want the upstream body", err.Error())
	}
}

func TestStreamRejectsUnprefixedModelBeforeCallingUpstream(t *testing.T) {
	bridge := &fakeBridge{}
	plugin := newManagementPlugin(t, bridge)

	_, err := plugin.ExecuteStream(context.Background(),
		streamRequest("deepseek-v4-pro", `{"model":"deepseek-v4-pro"}`, formatResponses))
	if err == nil {
		t.Fatal("ExecuteStream() error = nil, want a missing-prefix error")
	}
	if len(bridge.requests) != 0 {
		t.Error("upstream was called for a model this plugin does not own")
	}
}

func TestStreamClosesWhenTheClientGoesAway(t *testing.T) {
	// The consumer never reads; cancelling must still end the stream and let the
	// source drain, or the host stream leaks.
	source := make(chan pluginapi.HTTPStreamChunk, 4)
	source <- pluginapi.HTTPStreamChunk{Payload: []byte("a")}
	bridge := &fakeBridge{stream: pluginapi.HTTPStreamResponse{StatusCode: 200, Chunks: source}}
	plugin := newManagementPlugin(t, bridge)

	ctx, cancel := context.WithCancel(context.Background())
	resp, err := plugin.ExecuteStream(ctx,
		streamRequest(Provider+"/deepseek-v4-pro", `{"model":"x"}`, formatResponses))
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	cancel()
	// Closing the source lets the forwarder finish; it must not block forever.
	close(source)
	for range resp.Chunks {
	}
	// Nothing may be left in the source: an abandoned host stream never closes.
	if remaining := len(source); remaining != 0 {
		t.Fatalf("source still holds %d chunk(s), want a full drain", remaining)
	}
}
