package deepseekcpa

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"github.com/tidwall/gjson"
)

// terminalTracker reports whether the stream already reached a terminal event.
// It reads the SSE wire format line by line: the event name from "event:" lines
// and the payload's own "type" field, so a delta whose text happens to spell a
// terminal name cannot be mistaken for the end of the stream.
type terminalTracker struct {
	format string
	carry  []byte
	seen   bool
}

func newTerminalTracker(format string) *terminalTracker {
	return &terminalTracker{format: format}
}

func (t *terminalTracker) observe(chunk []byte) {
	if t.seen {
		return
	}
	window := append(append([]byte(nil), t.carry...), chunk...)
	lines := bytes.Split(window, []byte("\n"))
	t.carry = append([]byte(nil), lines[len(lines)-1]...)

	for _, line := range lines[:len(lines)-1] {
		line = bytes.TrimSuffix(line, []byte("\r"))
		switch {
		case bytes.HasPrefix(line, []byte("event:")):
			if t.isTerminal(string(bytes.TrimSpace(line[len("event:"):])), nil) {
				t.seen = true
				return
			}
		case bytes.HasPrefix(line, []byte("data:")):
			payload := bytes.TrimSpace(line[len("data:"):])
			if t.isTerminal(gjson.GetBytes(payload, "type").String(), payload) {
				t.seen = true
				return
			}
		}
	}
}

// isTerminal reports whether one SSE frame ends the stream on this surface.
func (t *terminalTracker) isTerminal(eventType string, data []byte) bool {
	switch t.format {
	case formatResponses:
		switch eventType {
		case "response.completed", "response.incomplete", "response.failed":
			return true
		}
	case formatAnthropic:
		switch eventType {
		case "message_stop", "error":
			return true
		}
	case formatChat:
		return string(data) == "[DONE]"
	default:
		panic("terminalTracker: no terminal rules for protocol " + t.format)
	}
	return false
}

// ExecuteStream forwards one streaming request to DeepSeek. Every upstream chunk
// is handed on unchanged, including its framing; a stream that ends without a
// terminal event is closed with a synthesized response.failed so the client can
// tell truncation from a normal finish.
func (p *Plugin) ExecuteStream(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
	call, err := p.prepareCall(req)
	if err != nil {
		return pluginapi.ExecutorStreamResponse{}, err
	}

	upstream, err := p.bridge.DoStream(ctx, pluginapi.HTTPRequest{
		Method:  http.MethodPost,
		URL:     call.endpoint,
		Headers: call.headers,
		Body:    call.body,
	})
	if err != nil {
		return pluginapi.ExecutorStreamResponse{}, fmt.Errorf("executor.execute_stream: %w", err)
	}
	if upstream.StatusCode < 200 || upstream.StatusCode > 299 {
		// The status is authoritative even when the body cannot be read: losing
		// a 401/402/429 here would change how the host treats the credential.
		body, errDrain := drainChunks(upstream.Chunks)
		return pluginapi.ExecutorStreamResponse{}, &upstreamError{
			statusCode: upstream.StatusCode,
			body:       body,
			cause:      errDrain,
		}
	}

	return pluginapi.ExecutorStreamResponse{
		Headers: upstream.Headers,
		Chunks:  p.forwardChunks(ctx, upstream.Chunks, call.format),
	}, nil
}

// forwardChunks hands each upstream chunk to the host untouched and closes the
// stream exactly once, including when the consumer stops reading or the request
// is cancelled (the source is drained so the host can release it).
func (p *Plugin) forwardChunks(ctx context.Context, source <-chan pluginapi.HTTPStreamChunk, format string) <-chan pluginapi.ExecutorStreamChunk {
	out := make(chan pluginapi.ExecutorStreamChunk)
	go func() {
		defer close(out)
		tracker := newTerminalTracker(format)
		unframer := &chatUnframer{}
		for chunk := range source {
			if chunk.Err != nil {
				// A broken upstream stream never produced its own terminal
				// event, so the client gets ours instead of a silent truncation.
				sendChunk(ctx, out, pluginapi.ExecutorStreamChunk{Payload: failureFrame(format, chunk.Err)})
				tracker.seen = true
				continue
			}
			// Chat chunks travel as bare payloads because the host frames them;
			// the other surfaces forward complete SSE frames.
			if format == formatChat {
				payloads, done := unframer.push(chunk.Payload)
				if done {
					tracker.seen = true
				}
				for _, payload := range payloads {
					if !sendChunk(ctx, out, pluginapi.ExecutorStreamChunk{Payload: payload}) {
						drainSource(source)
						return
					}
				}
				continue
			}
			tracker.observe(chunk.Payload)
			if !sendChunk(ctx, out, pluginapi.ExecutorStreamChunk{Payload: chunk.Payload}) {
				drainSource(source)
				return
			}
		}
		if !tracker.seen {
			sendChunk(ctx, out, pluginapi.ExecutorStreamChunk{Payload: failureFrame(format, errStreamEndedWithoutTerminal)})
		}
	}()
	return out
}

var errStreamEndedWithoutTerminal = fmt.Errorf("upstream stream ended before a terminal event")

// sendChunk reports whether the consumer is still there.
func sendChunk(ctx context.Context, out chan<- pluginapi.ExecutorStreamChunk, chunk pluginapi.ExecutorStreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

// drainSource consumes what is left so the host can close its stream. The
// source channel is bound to the same request context, so it always ends.
func drainSource(source <-chan pluginapi.HTTPStreamChunk) {
	for range source {
	}
}

// drainChunks collects an error response body, keeping whatever arrived before a
// read failure so the caller can still forward it.
func drainChunks(source <-chan pluginapi.HTTPStreamChunk) ([]byte, error) {
	var body bytes.Buffer
	for chunk := range source {
		if chunk.Err != nil {
			return body.Bytes(), chunk.Err
		}
		body.Write(chunk.Payload)
	}
	return body.Bytes(), nil
}
