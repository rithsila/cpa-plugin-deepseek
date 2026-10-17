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

// ErrUnsupported marks a capability that is declared but permanently does not
// apply to this provider, as opposed to one that is still being built. Callers
// must be able to tell the two apart from the response alone.
var ErrUnsupported = errors.New("unsupported")

// HostBridge is the single seam through which the plugin reaches the host.
// The cgo/ABI layer implements it; tests substitute a fake so no upstream,
// filesystem, or host process is involved.
type HostBridge interface {
	// Log writes through the host logger. Never pass secrets or raw bodies.
	Log(ctx context.Context, level, message string) error
	// ListAuths returns the credential records the host currently knows about.
	ListAuths(ctx context.Context) ([]pluginapi.HostAuthFileEntry, error)
	// SaveAuth writes a credential record through the host.
	SaveAuth(ctx context.Context, req pluginapi.HostAuthSaveRequest) (pluginapi.HostAuthSaveResponse, error)
	// Do performs one non-streaming upstream request through the host client.
	Do(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error)
	// DoStream performs one streaming upstream request through the host client.
	DoStream(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error)
}
