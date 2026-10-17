package main

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// streamHandle is one open host HTTP stream.
type streamHandle interface {
	// Read returns the next payload bytes, or done when the stream ended.
	Read(ctx context.Context) (payload []byte, done bool, err error)
	// Close releases the host stream.
	Close(ctx context.Context) error
}

// streamOpener opens a host HTTP stream.
type streamOpener interface {
	Open(ctx context.Context, req pluginapi.HTTPRequest) (streamHandle, pluginapi.HTTPStreamResponse, error)
}

// pumpStream opens an upstream stream and forwards every chunk the host hands
// back, closing the stream exactly once on every exit path. The caller owns the
// returned channel and must drain it.
func pumpStream(ctx context.Context, opener streamOpener, req pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	handle, response, err := opener.Open(ctx, req)
	if err != nil {
		return pluginapi.HTTPStreamResponse{}, err
	}

	chunks := make(chan pluginapi.HTTPStreamChunk)
	go func() {
		defer close(chunks)
		for {
			payload, done, errRead := handle.Read(ctx)
			if errRead != nil {
				chunks <- pluginapi.HTTPStreamChunk{Err: errRead}
				break
			}
			if len(payload) > 0 {
				chunks <- pluginapi.HTTPStreamChunk{Payload: payload}
			}
			if done {
				break
			}
		}
		if errClose := handle.Close(ctx); errClose != nil {
			chunks <- pluginapi.HTTPStreamChunk{Err: fmt.Errorf("close host stream: %w", errClose)}
		}
	}()

	response.Chunks = chunks
	return response, nil
}
