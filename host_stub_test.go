package deepseekcpa

import (
	"context"
	"errors"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// stubBridge is the test seam: nothing leaves the process.
type stubBridge struct{}

func (stubBridge) Log(context.Context, string, string) error { return nil }

func (stubBridge) SaveAuth(context.Context, pluginapi.HostAuthSaveRequest) (pluginapi.HostAuthSaveResponse, error) {
	return pluginapi.HostAuthSaveResponse{}, errors.New("stub bridge: SaveAuth not expected")
}

func (stubBridge) Do(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	return pluginapi.HTTPResponse{}, errors.New("stub bridge: Do not expected")
}

func (stubBridge) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pluginapi.HTTPStreamResponse{}, errors.New("stub bridge: DoStream not expected")
}
