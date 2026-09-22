package typesafe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSystemOneParsesNoulAndChoice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ts-key" {
			t.Fatalf("auth = %q", got)
		}
		var req systemOneRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "jev-latest" {
			t.Fatalf("model = %q", req.Model)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "jev-1.13.0",
			"answers": map[string]any{
				"ok": map[string]any{"type": "noul", "noul": 0.91},
				"act": map[string]any{
					"type":       "choice",
					"choice":     "approve",
					"confidence": 0.8,
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "ts-key", "", srv.Client())
	got, err := client.SystemOne(context.Background(), "ping", map[string]Question{
		"ok": Noul("Is this a ping?", "yes", "no"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Noul("ok") != 0.91 {
		t.Fatalf("noul = %v", got.Noul("ok"))
	}
	choice, conf := got.Choice("act")
	if choice != "approve" || conf != 0.8 {
		t.Fatalf("choice=%q conf=%v", choice, conf)
	}
}

func TestSystemOneEmptyAPIKey(t *testing.T) {
	client := NewClient("", "", "", nil)
	if _, err := client.SystemOne(context.Background(), "ping", map[string]Question{"q": Noul("x", "", "")}); err == nil {
		t.Fatal("expected error")
	}
}
