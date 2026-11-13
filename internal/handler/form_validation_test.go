package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/database"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestProjectUpdateRejectsBlankNameWithoutPartialSave(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.NewDB(filepath.Join(t.TempDir(), "project.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	project, err := db.CreateProject(&database.Project{Name: "original", Description: "old", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.PUT("/projects/:id", NewProjectHandler(db, zap.NewNop()).UpdateProject)
	for _, body := range []string{`{"name":"","description":"new"}`, `{"name":"  "}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/projects/"+project.ID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		saved, err := db.GetProject(project.ID)
		if err != nil || saved.Name != "original" || saved.Description != "old" {
			t.Fatalf("partial save: %#v %v", saved, err)
		}
	}
	// An omitted name remains a valid partial update.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/projects/"+project.ID, strings.NewReader(`{"description":"updated"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial update: %d %s", rec.Code, rec.Body)
	}
}

func TestBatchConcurrencyEditRejectsOutOfRangeWithoutMutation(t *testing.T) {
	m := NewBatchTaskManager(zap.NewNop())
	q, err := m.CreateBatchQueue("original", "", "single", "manual", "", "", nil, 3, []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{-1, 0, 9, 99} {
		if err := m.UpdateQueueMetadata(q.ID, "changed", "", "single", &n); err == nil {
			t.Fatalf("accepted %d", n)
		}
		saved, _ := m.GetBatchQueue(q.ID)
		if saved.Concurrency != 3 || saved.Title != "original" {
			t.Fatalf("mutated queue: %#v", saved)
		}
	}
	for _, n := range []int{1, 8} {
		if err := m.UpdateQueueMetadata(q.ID, "original", "", "single", &n); err != nil {
			t.Fatal(err)
		}
		saved, _ := m.GetBatchQueue(q.ID)
		if saved.Concurrency != n {
			t.Fatalf("saved %d, want %d", saved.Concurrency, n)
		}
	}
}
