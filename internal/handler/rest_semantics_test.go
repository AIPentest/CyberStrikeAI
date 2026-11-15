package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cyberstrike-ai/internal/c2"
	"cyberstrike-ai/internal/database"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func newRESTSemanticsDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.NewDB(filepath.Join(t.TempDir(), "rest.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func restSemanticsRequest(r http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestRESTDeleteSemantics(t *testing.T) {
	for _, module := range []string{"profile", "webshell"} {
		t.Run(module, func(t *testing.T) {
			db := newRESTSemanticsDB(t)
			r := gin.New()
			table, missing, failure, success := "c2_profiles", "profile not found", "failed to delete profile", `{"deleted":true}`
			if module == "profile" {
				h := NewC2Handler(c2.NewManager(db, zap.NewNop(), t.TempDir()), zap.NewNop())
				r.DELETE("/objects/:id", h.DeleteProfile)
				if err := db.CreateC2Profile(&database.C2Profile{ID: "existing", Name: "test", CreatedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			} else {
				table, missing, failure, success = "webshell_connections", "connection not found", "failed to delete connection", `{"ok":true}`
				h := NewWebShellHandler(zap.NewNop(), db)
				r.DELETE("/objects/:id", h.DeleteConnection)
				if err := db.CreateWebshellConnection(&database.WebShellConnection{ID: "existing", URL: "https://example.invalid", CreatedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			for _, tc := range []struct {
				id   string
				code int
				body string
			}{
				{"missing", http.StatusNotFound, `{"error":"` + missing + `"}`},
				{"existing", http.StatusOK, success},
				{"existing", http.StatusNotFound, `{"error":"` + missing + `"}`},
			} {
				w := restSemanticsRequest(r, http.MethodDelete, "/objects/"+tc.id)
				if w.Code != tc.code || w.Body.String() != tc.body {
					t.Fatalf("delete %s: %d %s, want %d %s", tc.id, w.Code, w.Body.String(), tc.code, tc.body)
				}
			}
			// Break only the temporary database: genuine SQL failures must remain 500,
			// with no table name or driver error in the HTTP response.
			if _, err := db.Exec("DROP TABLE " + table); err != nil {
				t.Fatal(err)
			}
			w := restSemanticsRequest(r, http.MethodDelete, "/objects/missing")
			if w.Code != http.StatusInternalServerError || w.Body.String() != `{"error":"`+failure+`"}` {
				t.Fatalf("SQL failure: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func assertRESTList(t *testing.T, r http.Handler, path, field string, count int) {
	t.Helper()
	w := restSemanticsRequest(r, http.MethodGet, path)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body[field], &items); err != nil || items == nil || len(items) != count {
		t.Fatalf("%s: %s=%s, want array of length %d (err=%v)", path, field, body[field], count, err)
	}
	var total int
	if err := json.Unmarshal(body["total"], &total); err != nil || total != count {
		t.Fatalf("%s: total=%s, want %d", path, body["total"], count)
	}
}

func TestRESTVulnerabilityListSemantics(t *testing.T) {
	db := newRESTSemanticsDB(t)
	h := NewVulnerabilityHandler(db, zap.NewNop())
	r := gin.New()
	r.GET("/api/vulnerabilities", h.ListVulnerabilities)
	assertRESTList(t, r, "/api/vulnerabilities", "vulnerabilities", 0)
	if _, err := db.CreateVulnerability(&database.Vulnerability{Title: "test finding", Status: "open", Severity: "low"}); err != nil {
		t.Fatal(err)
	}
	assertRESTList(t, r, "/api/vulnerabilities?status=__none__", "vulnerabilities", 0)
	assertRESTList(t, r, "/api/vulnerabilities?status=open", "vulnerabilities", 1)
	if _, err := db.Exec("DROP TABLE vulnerabilities"); err != nil {
		t.Fatal(err)
	}
	w := restSemanticsRequest(r, http.MethodGet, "/api/vulnerabilities")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("SQL failure became empty success: %d %s", w.Code, w.Body.String())
	}
}

func TestRESTBatchListSemantics(t *testing.T) {
	for _, withDB := range []bool{true, false} {
		name := "memory"
		if withDB {
			name = "database"
		}
		t.Run(name, func(t *testing.T) {
			m := NewBatchTaskManager(zap.NewNop())
			var db *database.DB
			if withDB {
				db = newRESTSemanticsDB(t)
				m.SetDB(db)
			}
			h := &AgentHandler{batchTaskManager: m, logger: zap.NewNop()}
			r := gin.New()
			r.GET("/api/batch-tasks", h.ListBatchQueues)
			assertRESTList(t, r, "/api/batch-tasks", "queues", 0)
			if _, err := m.CreateBatchQueue("test queue", "", "single", "manual", "", "", nil, 1, []string{"test"}); err != nil {
				t.Fatal(err)
			}
			assertRESTList(t, r, "/api/batch-tasks?status=__none__", "queues", 0)
			assertRESTList(t, r, "/api/batch-tasks", "queues", 1)
			if withDB {
				if _, err := db.Exec("DROP TABLE batch_task_queues"); err != nil {
					t.Fatal(err)
				}
				w := restSemanticsRequest(r, http.MethodGet, "/api/batch-tasks")
				if w.Code != http.StatusInternalServerError {
					t.Fatalf("SQL failure became empty success: %d %s", w.Code, w.Body.String())
				}
			}
		})
	}
}
