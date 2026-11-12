package openai

// eino_sse_sanitizer.go Resolves the case where Eino goes through the meguminnnnnnnnn/go-openai SDK,
// A relay station's heartbeat/SSE control lines accumulating to > 300 lines triggers ErrTooManyEmptyStreamMessages
// (Error message text: "stream has sent too many empty messages")the issue of.
//
// Trigger path:
// einoopenai.NewChatModel
// → eino-ext/libs/acl/openai → meguminnnnnnnnn/go-openai
// → streamReader.processLines() For all non- "data:" Line count; it throws once it exceeds 300.
//
// Non-data: lines common to relay stations (valid SSE but not accepted by the SDK):
// ":" / ": keepalive" / ": ping" / "event: ping" / "retry: 3000"
// and the large number of heartbeats interleaved during a reasoning model's prefill.
//
// Fallback strategy: wrap the response Body in a reader at the HTTP transport layer, passing through only "data:"
// lines, swallowing heartbeat/comment/event-type lines in place. The downstream SDK never sees a non-data: line,
// The counter is always 0, so this error can no longer occur.
//
// This layer is completely transparent to the caller:
// - Only intervenes when the response Content-Type is text/event-stream; plain JSON responses are passed through verbatim
// - data: payload Only fills in the DeepSeek cache-usage field; everything else is passed through verbatim
// - A genuine upstream stream end (EOF / connection reset / context cancel) is passed through verbatim

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strings"
)

const (
	// einoSSEReaderBufSize Gives bufio a large initial buffer, to avoid a single large line JSON chunk
	// (containing tool-call arguments / reasoning_content) frequently triggers buffer growth.
	einoSSEReaderBufSize = 64 * 1024
)

// einoSSESanitizingRoundTripper Wraps the downstream RoundTripper, performing line-level cleaning on SSE responses.
type einoSSESanitizingRoundTripper struct {
	base http.RoundTripper
}

func (rt *einoSSESanitizingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if !isSSEResponse(resp) {
		return resp, nil
	}
	resp.Body = newEinoSSESanitizingBody(resp.Body)
	return resp, nil
}

// isSSEResponse Only for 200 + text/event-stream cleans the response of;
// Error responses are handled by the separate einoSSEErrorRoundTripper; this layer does not modify them.
func isSSEResponse(resp *http.Response) bool {
	if resp.StatusCode != http.StatusOK {
		return false
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		return false
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	// Compatible "text/event-stream", "text/event-stream; charset=utf-8" etc..
	return strings.HasPrefix(ct, "text/event-stream")
}

// einoSSESanitizingBody is the wrapped response body: only data: lines are passed through, other lines are swallowed.
type einoSSESanitizingBody struct {
	upstream io.ReadCloser
	reader   *bufio.Reader
	pending  []byte // The cleaned bytes to be returned downstream (always ending with \n a complete data: line ending with)
	err      error  // upstream A terminal-state error (io.EOF or a network error)
}

func newEinoSSESanitizingBody(body io.ReadCloser) *einoSSESanitizingBody {
	return &einoSSESanitizingBody{
		upstream: body,
		reader:   bufio.NewReaderSize(body, einoSSEReaderBufSize),
	}
}

func (b *einoSSESanitizingBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(b.pending) > 0 {
		n := copy(p, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}

	// Reads from upstream until it accumulates a data: line or reaches a terminal state.
	// A single loop may discard any number of heartbeat lines, but passes through at most one data: line and then exits,
	// Avoids a single Read blocking too long / the pending buffer growing too large.
	for b.err == nil {
		line, err := b.reader.ReadBytes('\n')
		if len(line) > 0 {
			if isPassThroughSSELine(line) {
				if line[len(line)-1] != '\n' {
					line = append(line, '\n')
				}
				payload := bytes.TrimSpace(bytes.TrimLeft(line, " \t")[5:])
				normalized := normalizeEinoUsagePayload(payload)
				if !bytes.Equal(payload, normalized) {
					line = append(append([]byte("data: "), normalized...), '\n')
				}
				b.pending = line
				if err != nil {
					b.err = err
				}
				break
			}
			// a non-data: line (an empty line / ":" comments / event: / retry: / id: / any bare text)
			// Swallows them all, does not pass them downstream, and continues the loop to read the next line.
		}
		if err != nil {
			b.err = err
			break
		}
	}

	if len(b.pending) > 0 {
		n := copy(p, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}
	return 0, b.err
}

func (b *einoSSESanitizingBody) Close() error {
	return b.upstream.Close()
}

// isPassThroughSSELine Decides whether the line should be passed through verbatim to downstream SDK.
// only "data:" (case-insensitive, with any leading whitespace) lines must be kept.
// Note: you cannot use TrimSpace to strip the trailing newline before checking, otherwise " data: x" would be misjudged;
// We only trim leading whitespace, matching the SDK's internal TrimSpace-then-regex ^data:\s* has consistent semantics with.
func isPassThroughSSELine(line []byte) bool {
	trimmed := bytes.TrimLeft(line, " \t")
	if len(trimmed) < 5 {
		return false
	}
	// Case-insensitively compares whether the first 5 bytes are "data:".SSE The spec requires field names to be lowercase,
	// but lenient matching accommodates the non-standard implementations of certain relay stations.
	return (trimmed[0] == 'd' || trimmed[0] == 'D') &&
		(trimmed[1] == 'a' || trimmed[1] == 'A') &&
		(trimmed[2] == 't' || trimmed[2] == 'T') &&
		(trimmed[3] == 'a' || trimmed[3] == 'A') &&
		trimmed[4] == ':'
}
