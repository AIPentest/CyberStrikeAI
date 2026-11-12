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

func TestUpdateLegacyPaddedFactSummaryPreservesKeyAndEvidence(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "legacy-handler.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: p.ID, FactKey: "finding/legacy", Summary: "before", Body: "original evidence"})
	if err != nil {
		t.Fatal(err)
	}
	legacyKey := " finding/legacy "
	if _, err := db.Exec("UPDATE project_facts SET fact_key=? WHERE id=?", legacyKey, f.ID); err != nil {
		t.Fatal(err)
	}
	// A historical relationship may refer to the exact stored key. Updating
	// summary/body must not rewrite that evidence reference.
	edge, err := db.AddProjectFactEdge(p.ID, database.ProjectFactEdgeInput{To: "finding/target", Type: "supports"}, "finding/legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE project_fact_edges SET source_fact_key=? WHERE id=?", legacyKey, edge.ID); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []struct{ payload, expectedBody string }{
		{`{"summary":"after"}`, "original evidence"},
		{`{"body":"updated evidence"}`, "updated evidence"},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: p.ID}, {Key: "factId", Value: f.ID}}
		c.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(edit.payload))
		c.Request.Header.Set("Content-Type", "application/json")
		NewProjectHandler(db, zap.NewNop()).UpdateFact(c)
		if w.Code != http.StatusOK {
			t.Fatalf("legacy partial update failed: %d %s", w.Code, w.Body.String())
		}
		stored, err := db.GetProjectFact(f.ID)
		if err != nil || stored.FactKey != legacyKey || stored.Summary != "after" || stored.Body != edit.expectedBody {
			t.Fatalf("legacy edit changed key or evidence: %+v %v", stored, err)
		}
		edges, err := db.ListOutgoingProjectFactEdges(p.ID, legacyKey)
		if err != nil || len(edges) != 1 || edges[0].ID != edge.ID || edges[0].SourceFactKey != legacyKey {
			t.Fatalf("legacy edit changed relationship references: %+v %v", edges, err)
		}
	}
}
