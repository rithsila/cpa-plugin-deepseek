package deepseekcpa

import (
	"encoding/json"
	"fmt"
)

// failureFrame renders the protocol's own terminal error event, so a client can
// tell a broken stream from a finished one. Upstream events are never re-framed;
// this is the one frame the plugin has to author itself.
func failureFrame(format string, cause error) []byte {
	message := cause.Error()
	switch format {
	case formatResponses:
		return sseEvent("response.failed", map[string]any{
			"type": "response.failed",
			"response": map[string]any{
				"object": "response",
				"status": "failed",
				"error": map[string]any{
					"code":    "upstream_stream_error",
					"type":    "server_error",
					"message": message,
				},
			},
		})
	case formatAnthropic:
		return sseEvent("error", map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "api_error",
				"message": message,
			},
		})
	case formatChat:
		// Chat completions has no error event: the stream carries an error
		// payload and then the usual terminator.
		body := jsonData(map[string]any{
			"error": map[string]any{
				"type":    "server_error",
				"message": message,
			},
		})
		return append(body, []byte("data: [DONE]\n\n")...)
	default:
		panic(fmt.Sprintf("failureFrame: no failure framing for protocol %q", format))
	}
}

// sseEvent renders one SSE frame the way the host's own translators do.
func sseEvent(event string, payload map[string]any) []byte {
	out := make([]byte, 0, 64)
	out = append(out, "event: "...)
	out = append(out, event...)
	out = append(out, '\n')
	out = append(out, jsonData(payload)...)
	return out
}

func jsonData(payload map[string]any) []byte {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic("jsonData: marshaling a map of scalars cannot fail: " + err.Error())
	}
	out := make([]byte, 0, len(raw)+8)
	out = append(out, "data: "...)
	out = append(out, raw...)
	return append(out, '\n', '\n')
}
