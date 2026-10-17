package main

import (
	"context"
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// fakeHandle replays a scripted sequence of reads and records the close.
type fakeHandle struct {
	reads     [][]byte
	readErr   error
	closeErr  error
	closed    int
	readIndex int
}

func (h *fakeHandle) Read(context.Context) ([]byte, bool, error) {
	if h.readErr != nil {
		return nil, false, h.readErr
	}
	if h.readIndex >= len(h.reads) {
		return nil, true, nil
	}
	payload := h.reads[h.readIndex]
	h.readIndex++
	return payload, h.readIndex >= len(h.reads), nil
}

func (h *fakeHandle) Close(context.Context) error {
	h.closed++
	return h.closeErr
}

type fakeOpener struct {
	handle   *fakeHandle
	openErr  error
	response pluginapi.HTTPStreamResponse
}

func (o fakeOpener) Open(context.Context, pluginapi.HTTPRequest) (streamHandle, pluginapi.HTTPStreamResponse, error) {
	if o.openErr != nil {
		return nil, pluginapi.HTTPStreamResponse{}, o.openErr
	}
	return o.handle, o.response, nil
}

func drain(t *testing.T, chunks <-chan pluginapi.HTTPStreamChunk) (payloads [][]byte, errs []error) {
	t.Helper()
	for chunk := range chunks {
		if chunk.Err != nil {
			errs = append(errs, chunk.Err)
			continue
		}
		payloads = append(payloads, chunk.Payload)
	}
	return payloads, errs
}

func TestPumpStreamForwardsEveryChunkAndCloses(t *testing.T) {
	handle := &fakeHandle{reads: [][]byte{[]byte("event: a\n\n"), []byte("event: b\n\n")}}
	opener := fakeOpener{handle: handle, response: pluginapi.HTTPStreamResponse{StatusCode: 200}}

	response, err := pumpStream(context.Background(), opener, pluginapi.HTTPRequest{})
	if err != nil {
		t.Fatalf("pumpStream() error = %v", err)
	}
	if response.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", response.StatusCode)
	}

	payloads, errs := drain(t, response.Chunks)
	if len(errs) != 0 {
		t.Fatalf("stream errors = %v, want none", errs)
	}
	if len(payloads) != 2 || string(payloads[0]) != "event: a\n\n" || string(payloads[1]) != "event: b\n\n" {
		t.Fatalf("payloads = %q, want the two read chunks in order", payloads)
	}
	if handle.closed != 1 {
		t.Errorf("Close() calls = %d, want 1", handle.closed)
	}
}

func TestPumpStreamReportsReadErrorAndStillCloses(t *testing.T) {
	readErr := errors.New("upstream reset")
	handle := &fakeHandle{readErr: readErr}

	response, err := pumpStream(context.Background(), fakeOpener{handle: handle}, pluginapi.HTTPRequest{})
	if err != nil {
		t.Fatalf("pumpStream() error = %v", err)
	}
	payloads, errs := drain(t, response.Chunks)
	if len(payloads) != 0 {
		t.Errorf("payloads = %q, want none", payloads)
	}
	if len(errs) != 1 || !errors.Is(errs[0], readErr) {
		t.Fatalf("stream errors = %v, want the read error", errs)
	}
	if handle.closed != 1 {
		t.Errorf("Close() calls = %d, want 1", handle.closed)
	}
}

func TestPumpStreamReportsCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	handle := &fakeHandle{reads: [][]byte{[]byte("x")}, closeErr: closeErr}

	response, err := pumpStream(context.Background(), fakeOpener{handle: handle}, pluginapi.HTTPRequest{})
	if err != nil {
		t.Fatalf("pumpStream() error = %v", err)
	}
	_, errs := drain(t, response.Chunks)
	if len(errs) != 1 || !errors.Is(errs[0], closeErr) {
		t.Fatalf("stream errors = %v, want the close error", errs)
	}
}

func TestPumpStreamPropagatesOpenFailure(t *testing.T) {
	openErr := errors.New("dial failed")
	if _, err := pumpStream(context.Background(), fakeOpener{openErr: openErr}, pluginapi.HTTPRequest{}); !errors.Is(err, openErr) {
		t.Fatalf("pumpStream() error = %v, want the open error", err)
	}
}
