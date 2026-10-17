package deepseekcpa

import (
	"context"
	"errors"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// ErrNotImplemented marks a capability the plugin declares but has not built
// yet. Declared capabilities must eventually do real work; until then the
// failure must be loud rather than a silent empty result.
var ErrNotImplemented = errors.New("not implemented")

// HostBridge is the single seam through which the plugin reaches the host.
// The cgo/ABI layer implements it; tests substitute a fake so no upstream,
// filesystem, or host process is involved.
type HostBridge interface {
	// Log writes through the host logger. Never pass secrets or raw bodies.
	Log(ctx context.Context, level, message string) error
	// SaveAuth writes a credential record through the host.
	SaveAuth(ctx context.Context, req pluginapi.HostAuthSaveRequest) (pluginapi.HostAuthSaveResponse, error)
	// Do performs one non-streaming upstream request through the host client.
	Do(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error)
	// DoStream performs one streaming upstream request through the host client.
	DoStream(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error)
}
