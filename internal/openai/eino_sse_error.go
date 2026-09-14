package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

const einoSSEErrorMaxBytes = 64 * 1024

// Some gateways send SSE even for HTTP errors (occasionally labeled JSON).
// The SDK decodes non-2xx responses as JSON. Unwrap only a validated error
// event, preserving HTTP status and error fields for retry classification.
type einoSSEErrorRoundTripper struct {
	base http.RoundTripper
}

func (rt *einoSSEErrorRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil || resp.StatusCode < 400 {
		return resp, err
	}
	upstream := resp.Body
	body, readErr := io.ReadAll(io.LimitReader(upstream, einoSSEErrorMaxBytes+1))
	// Replay all consumed bytes and any read failure if normalization is unsafe.
	readers := []io.Reader{bytes.NewReader(body)}
	if readErr != nil {
		readers = append(readers, sseErrorReadFailure{readErr})
	}
	readers = append(readers, upstream)
	resp.Body = &sseErrorReplayBody{Reader: io.MultiReader(readers...), Closer: upstream}
	if readErr != nil || len(body) > einoSSEErrorMaxBytes {
		return resp, nil
	}
	payload := extractSSEError(body)
	if payload == nil {
		return resp, nil
	}
	resp.Body = &sseErrorReplayBody{Reader: bytes.NewReader(payload), Closer: upstream}
	resp.Header = resp.Header.Clone()
	if resp.Header == nil {
		resp.Header = make(http.Header)
	}
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Set("Content-Length", strconv.Itoa(len(payload)))
	resp.Header.Del("Transfer-Encoding")
	resp.ContentLength = int64(len(payload))
	resp.TransferEncoding = nil
	return resp, nil
}

type sseErrorReplayBody struct {
	io.Reader
	io.Closer
}

type sseErrorReadFailure struct{ err error }

func (r sseErrorReadFailure) Read([]byte) (int, error) { return 0, r.err }

// Honor SSE event boundaries and multiline data. Unknown/plain/invalid bodies
// are left intact, rather than replacing useful diagnostics with guesses.
func extractSSEError(body []byte) []byte {
	var data []byte
	validate := func() []byte {
		var envelope struct {
			Error *struct {
				Message json.RawMessage `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Error != nil && len(envelope.Error.Message) > 0 && !bytes.Equal(envelope.Error.Message, []byte("null")) {
			return bytes.TrimSpace(data)
		}
		return nil
	}
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 {
			if payload := validate(); payload != nil {
				return payload
			}
			data = nil
			continue
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			value := bytes.TrimPrefix(line[5:], []byte(" "))
			data = append(data, value...)
			data = append(data, '\n')
		}
	}
	return validate() // Gateways sometimes omit the final blank line.
}
