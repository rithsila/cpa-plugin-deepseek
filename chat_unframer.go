package deepseekcpa

import "bytes"

// chatUnframer turns upstream chat SSE bytes into the bare JSON payloads the
// host expects on that surface.
//
// The three surfaces differ in who frames a chunk: on chat-completions the host
// writes "data: <payload>\n\n" itself and appends its own [DONE], so the executor
// must hand over bare payloads; on responses and anthropic the host forwards the
// executor's bytes untouched, so those chunks stay complete SSE frames. Feeding
// a framed chat chunk here is what produced doubled "data: data:" lines.
type chatUnframer struct {
	carry []byte
}

// push consumes upstream bytes and returns the complete payloads they contain.
// done reports that the upstream terminator arrived, which the host re-emits on
// its own, so it is not returned as a payload.
func (u *chatUnframer) push(chunk []byte) (payloads [][]byte, done bool) {
	window := append(append([]byte(nil), u.carry...), chunk...)
	lines := bytes.Split(window, []byte("\n"))
	u.carry = append([]byte(nil), lines[len(lines)-1]...)

	for _, line := range lines[:len(lines)-1] {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 {
			continue
		}
		if string(payload) == "[DONE]" {
			return payloads, true
		}
		payloads = append(payloads, append([]byte(nil), payload...))
	}
	return payloads, false
}
