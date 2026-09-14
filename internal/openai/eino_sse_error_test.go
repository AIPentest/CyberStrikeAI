package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

func TestSSEErrorThroughEinoModel(t *testing.T) {
	for _, status := range []int{400, 429, 503} {
		for _, ct := range []string{"text/event-stream", "application/json", ""} {
			t.Run(http.StatusText(status)+"/"+ct, func(t *testing.T) {
				srv := newSSEServer(t, `data:{"error":{"code":"400","message":"Invalid request parameters","param":"tool_choice","type":"BadRequestError"}}`, ct, status)
				defer srv.Close()
				m, err := einoopenai.NewChatModel(context.Background(), &einoopenai.ChatModelConfig{APIKey: "local-test", BaseURL: srv.URL, Model: "local-test", HTTPClient: NewEinoHTTPClient(nil, srv.Client())})
				if err != nil {
					t.Fatal(err)
				}
				_, err = m.Stream(context.Background(), []*schema.Message{schema.UserMessage("test")})
				var apiErr *einoopenai.APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("expected structured API error, got %v", err)
				}
				if apiErr.HTTPStatusCode != status || apiErr.Message != "Invalid request parameters" || apiErr.Type != "BadRequestError" || apiErr.Code != "400" || apiErr.Param == nil || *apiErr.Param != "tool_choice" {
					t.Fatalf("error fields changed: %+v", apiErr)
				}
			})
		}
	}
}

func TestSSEErrorBodyCompatibility(t *testing.T) {
	payload := `{"error":{"message":"rejected","extra":"preserved"}}`
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"plain JSON", payload, payload, 400},
		{"HTML", "<html>bad gateway</html>", "<html>bad gateway</html>", 502},
		{"malformed", "data:{broken}", "data:{broken}", 400},
		{"not error", "data:{\"choices\":[]}", "data:{\"choices\":[]}", 400},
		{"null error", "data:{\"error\":null}", "data:{\"error\":null}", 400},
		{"SSE", "data:" + payload, payload, 400},
		{"CRLF heartbeat", ": ping\r\nevent: error\r\ndata: " + payload + "\r\n\r\ndata: [DONE]\r\n", payload, 400},
		{"multiline", "data: {\"error\":\ndata: {\"message\":\"rejected\",\"extra\":\"preserved\"}}\n\n", "{\"error\":\n{\"message\":\"rejected\",\"extra\":\"preserved\"}}", 400},
		{"success", "data:" + payload, "data:" + payload, 200},
		{"oversized", "data:" + payload + strings.Repeat(" ", einoSSEErrorMaxBytes), "data:" + payload + strings.Repeat(" ", einoSSEErrorMaxBytes), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newSSEServer(t, tc.body, "text/event-stream", tc.status)
			defer srv.Close()
			client := &http.Client{Transport: &einoSSEErrorRoundTripper{base: http.DefaultTransport}}
			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			got := readAll(t, resp.Body)
			if got != tc.want {
				t.Fatalf("body differs: got %q want %q", got, tc.want)
			}
			if resp.StatusCode != tc.status {
				t.Fatal("status changed")
			}
			if tc.want != tc.body && (resp.Header.Get("Content-Type") != "application/json" || resp.ContentLength != int64(len(got))) {
				t.Fatal("incorrect normalized headers")
			}
		})
	}
}

type errorResponseTransport struct{ body io.ReadCloser }

func (rt errorResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 400, Header: http.Header{"X-Request-Id": []string{"test-id"}}, Body: rt.body}, nil
}

type failingErrorBody struct {
	closed bool
	err    error
}

func (b *failingErrorBody) Read(p []byte) (int, error) {
	return copy(p, `data:{"error":{"message":"partial"}}`), b.err
}
func (b *failingErrorBody) Close() error { b.closed = true; return nil }
func TestSSEErrorPreservesReadFailureAndClose(t *testing.T) {
	sentinel := errors.New("upstream disconnected")
	body := &failingErrorBody{err: sentinel}
	rt := &einoSSEErrorRoundTripper{base: errorResponseTransport{body: body}}
	req, _ := http.NewRequest("GET", "http://local.test", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if !errors.Is(err, sentinel) || string(got) != `data:{"error":{"message":"partial"}}` {
		t.Fatalf("read failure lost: %q %v", got, err)
	}
	if resp.Header.Get("X-Request-Id") != "test-id" {
		t.Fatal("request ID lost")
	}
	resp.Body.Close()
	if !body.closed {
		t.Fatal("upstream not closed")
	}
}
