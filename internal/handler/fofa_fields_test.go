package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestFofaSearchReturnFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("FOFA_API_KEY", "")
	// A contract-checking upstream: unsupported fields are rejected rather than
	// silently accepted by a mock, and values are returned in requested order.
	values := map[string]string{
		"host": "https://example.com", "ip": "192.0.2.1", "port": "443",
		"domain": "example.com", "title": "Example", "protocol": "https",
		"country": "CN", "region": "Beijing", "city": "Beijing", "server": "nginx",
	}
	defaults := "host,ip,port,domain,title,protocol,country,region,city,server"
	for _, tc := range []struct{ name, fields, want string }{
		{"default", "", defaults},
		{"blank default", "  ", defaults},
		{"legacy form", strings.ReplaceAll(defaults, "region", "province"), defaults},
		{"legacy alias and duplicate", " host, province, region, city, province,host ", "host,region,city"},
		{"canonical region", "region,host", "region,host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var received string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received = r.URL.Query().Get("fields")
				row := []string{}
				for _, field := range strings.Split(received, ",") {
					value, ok := values[field]
					if !ok {
						http.Error(w, "unsupported field: "+field, http.StatusBadRequest)
						return
					}
					row = append(row, value)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": false, "size": 1, "page": 1, "results": [][]string{row}})
			}))
			defer server.Close()
			h := NewFofaHandler(&config.Config{FOFA: config.FofaConfig{APIKey: "test-key", BaseURL: server.URL}}, zap.NewNop())
			body, _ := json.Marshal(fofaSearchRequest{Query: `domain="example.com"`, Fields: tc.fields})
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/fofa/search", strings.NewReader(string(body)))
			ctx.Request.Header.Set("Content-Type", "application/json")
			h.Search(ctx)
			if recorder.Code != http.StatusOK {
				t.Fatalf("Search status = %d: %s", recorder.Code, recorder.Body.String())
			}
			if received != tc.want {
				t.Fatalf("upstream fields = %q, want %q", received, tc.want)
			}
			var result fofaSearchResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Fields, strings.Split(tc.want, ",")) || len(result.Results) != 1 {
				t.Fatalf("incorrect result shape: %+v", result)
			}
			for _, field := range result.Fields {
				if result.Results[0][field] != values[field] {
					t.Errorf("result[%s] = %v, want %s", field, result.Results[0][field], values[field])
				}
			}
		})
	}
}
