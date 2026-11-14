package typesafe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func testServerReturning(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSystemOneRejectsEmptyAnswers(t *testing.T) {
	srv := testServerReturning(t, `{"model":"jev-latest","answers":{}}`)
	client := NewClient(srv.URL, "ts-key", "", srv.Client())
	_, err := client.SystemOne(context.Background(), "ping", map[string]Question{
		"availability": Noul("Would this destroy availability?", "yes", "no"),
		"decision":     Choice("Approve?", map[string]string{"approve": "yes", "reject": "no"}),
	})
	if err == nil || !strings.Contains(err.Error(), "no answers") {
		t.Fatalf("expected empty-answers error, got %v", err)
	}
}

func TestSystemOneRejectsIncompleteAnswers(t *testing.T) {
	srv := testServerReturning(t, `{"answers":{"availability":{"noul":0.02}}}`)
	client := NewClient(srv.URL, "ts-key", "", srv.Client())
	_, err := client.SystemOne(context.Background(), "ping", map[string]Question{
		"availability": Noul("Would this destroy availability?", "yes", "no"),
		"decision":     Choice("Approve?", map[string]string{"approve": "yes", "reject": "no"}),
	})
	if err == nil || !strings.Contains(err.Error(), "incomplete") || !strings.Contains(err.Error(), "decision") {
		t.Fatalf("expected incomplete-answers error, got %v", err)
	}
}

func TestSystemOneRejectsNonNumericNoul(t *testing.T) {
	srv := testServerReturning(t, `{"answers":{"availability":{"noul":"0.02"}}}`)
	client := NewClient(srv.URL, "ts-key", "", srv.Client())
	_, err := client.SystemOne(context.Background(), "ping", map[string]Question{
		"availability": Noul("Would this destroy availability?", "yes", "no"),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid noul") {
		t.Fatalf("expected invalid-noul error, got %v", err)
	}
}

func TestSystemOneRejectsChoiceWithoutConfidence(t *testing.T) {
	srv := testServerReturning(t, `{"answers":{"decision":{"choice":"reject"}}}`)
	client := NewClient(srv.URL, "ts-key", "", srv.Client())
	_, err := client.SystemOne(context.Background(), "ping", map[string]Question{
		"decision": Choice("Approve?", map[string]string{"approve": "yes", "reject": "no"}),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid confidence") {
		t.Fatalf("expected invalid-confidence error, got %v", err)
	}
}

func TestSystemOneAcceptsCompleteAnswers(t *testing.T) {
	srv := testServerReturning(t, `{"answers":{"availability":{"noul":0.02},"decision":{"choice":"approve","confidence":0.95}}}`)
	client := NewClient(srv.URL, "ts-key", "", srv.Client())
	got, err := client.SystemOne(context.Background(), "ping", map[string]Question{
		"availability": Noul("Would this destroy availability?", "yes", "no"),
		"decision":     Choice("Approve?", map[string]string{"approve": "yes", "reject": "no"}),
	})
	if err != nil {
		t.Fatalf("complete answers should be accepted: %v", err)
	}
	if got.Noul("availability") != 0.02 {
		t.Fatalf("noul = %v", got.Noul("availability"))
	}
}
