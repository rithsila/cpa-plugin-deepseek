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

// ExecuteStream forwards one streaming request to DeepSeek. The bytes and their
// order are preserved on every surface; only the chunk boundaries differ. On
// responses and anthropic they are cut on SSE line boundaries, on chat the
// chunks become bare payloads because the host frames that surface. A stream
// that ends without a terminal event is closed with the protocol's own failure
// event so the client can tell truncation from a normal finish.
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

// forwardChunks hands each upstream chunk to the host and closes the stream
// exactly once, including when the consumer stops reading or the request is
// cancelled (the source is drained so the host can release it). Responses and
// anthropic chunks are re-cut on line boundaries; chat chunks travel as bare
// payloads because the host does the framing for that surface.
func (p *Plugin) forwardChunks(ctx context.Context, source <-chan pluginapi.HTTPStreamChunk, format string) <-chan pluginapi.ExecutorStreamChunk {
	out := make(chan pluginapi.ExecutorStreamChunk)
	go func() {
		defer close(out)
		tracker := newTerminalTracker(format)
		unframer := &chatUnframer{}
		framer := &lineFramer{}
		for chunk := range source {
			if chunk.Err != nil {
				// A broken upstream stream never produced its own terminal
				// event, so the client gets ours instead of a silent truncation.
				// Whatever part of the last line did arrive still goes first: the
				// terminal frame ends the stream, so anything sent after it can be
				// dropped by the client.
				if trailing := framer.take(); len(trailing) > 0 {
					sendChunk(ctx, out, pluginapi.ExecutorStreamChunk{Payload: trailing})
				}
				sendChunk(ctx, out, pluginapi.ExecutorStreamChunk{Payload: failureFrame(format, chunk.Err)})
				tracker.seen = true
				continue
			}
			// Chat chunks travel as bare payloads because the host frames them;
			// the other surfaces forward complete SSE lines.
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
			if payload := framer.push(chunk.Payload); len(payload) > 0 {
				if !sendChunk(ctx, out, pluginapi.ExecutorStreamChunk{Payload: payload}) {
					drainSource(source)
					return
				}
			}
		}
		if trailing := framer.take(); len(trailing) > 0 {
			sendChunk(ctx, out, pluginapi.ExecutorStreamChunk{Payload: trailing})
		}
		if !tracker.seen {
			sendChunk(ctx, out, pluginapi.ExecutorStreamChunk{Payload: failureFrame(format, errStreamEndedWithoutTerminal)})
		}
	}()
	return out
}

var errStreamEndedWithoutTerminal = fmt.Errorf("upstream stream ended before a terminal event")

// lineFramer emits only whole SSE lines. The host assembles plugin chunks into
// lines but caps that buffer at 64KB and drops whatever it holds once the cap is
// exceeded. A Responses terminal frame echoes instructions and tools back, so it
// routinely exceeds 64KB: split across chunks it would lose the usage block it
// carries. Sent as one line-terminated chunk the host consumes it in the same
// pass and the cap never applies.
type lineFramer struct {
	carry []byte
}

// push returns the longest newline-terminated prefix of the bytes seen so far.
func (f *lineFramer) push(payload []byte) []byte {
	f.carry = append(f.carry, payload...)
	idx := bytes.LastIndexByte(f.carry, '\n')
	if idx < 0 {
		return nil
	}
	out := append([]byte(nil), f.carry[:idx+1]...)
	f.carry = append(f.carry[:0], f.carry[idx+1:]...)
	return out
}

// take returns whatever is left once the upstream stream ended. A conforming
// stream leaves nothing behind; a truncated one keeps its last partial frame.
func (f *lineFramer) take() []byte {
	out := f.carry
	f.carry = nil
	return out
}

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
