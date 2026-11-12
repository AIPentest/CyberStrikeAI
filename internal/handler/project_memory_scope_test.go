package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestProjectBlackboardInitialInjectionUsesCurrentConversation(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "injection.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "same project"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.CreateConversation("a", database.ConversationCreateMeta{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateConversation("b", database.ConversationCreateMeta{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []*database.ProjectFact{
		{ProjectID: p.ID, FactKey: "note/context-a", SourceConversationID: a.ID, Summary: "own continuation"},
		{ProjectID: p.ID, FactKey: "note/context-b", SourceConversationID: b.ID, Summary: "other continuation"},
		{ProjectID: p.ID, FactKey: "finding/shared", Category: "finding", Summary: "shared result", Confidence: "confirmed"},
	} {
		if _, err := db.UpsertProjectFact(f); err != nil {
			t.Fatal(err)
		}
	}
	h := &AgentHandler{db: db, config: &config.Config{Project: config.ProjectConfig{Enabled: true}}, logger: zap.NewNop()}
	block := h.projectBlackboardBlock(a.ID)
	if !strings.Contains(block, "note/context-a") || !strings.Contains(block, "finding/shared") || strings.Contains(block, "note/context-b") {
		t.Fatalf("initial prompt mixed sessions: %s", block)
	}
}

func TestConsoleCannotCreateOrRewriteReservedCheckpointKeys(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "reserved.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "reserved"})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := db.UpsertProjectFact(&database.ProjectFact{
		ProjectID: p.ID, FactKey: "note/context-victim", SourceConversationID: "victim",
		Summary: "original progress", Body: "ORIGINAL_BODY",
	})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: p.ID, FactKey: "finding/ordinary", Summary: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	h := NewProjectHandler(db, zap.NewNop())
	request := func(method, factID, payload string) (*httptest.ResponseRecorder, *gin.Context) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: p.ID}, {Key: "factId", Value: factID}}
		c.Request = httptest.NewRequest(method, "/", strings.NewReader(payload))
		c.Request.Header.Set("Content-Type", "application/json")
		return w, c
	}

	w, c := request(http.MethodPost, "", `{"fact_key":"note/context-forged","summary":"forged current work"}`)
	h.CreateFact(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("console created a reserved key: %d %s", w.Code, w.Body.String())
	}

	w, c = request(http.MethodPut, ordinary.ID, `{"fact_key":"note/context-forged"}`)
	h.UpdateFact(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("console renamed a fact into the reserved prefix: %d %s", w.Code, w.Body.String())
	}

	w, c = request(http.MethodPut, checkpoint.ID, `{"summary":"rewritten","body":"REWRITTEN"}`)
	h.UpdateFact(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("console rewrote another conversation's checkpoint: %d %s", w.Code, w.Body.String())
	}

	stored, err := db.GetProjectFact(checkpoint.ID)
	if err != nil || stored.Summary != "original progress" || stored.Body != "ORIGINAL_BODY" || stored.SourceConversationID != "victim" {
		t.Fatalf("rejected requests still modified the checkpoint: %+v %v", stored, err)
	}
	if renamed, err := db.GetProjectFact(ordinary.ID); err != nil || renamed.FactKey != "finding/ordinary" {
		t.Fatalf("rejected rename still applied: %+v %v", renamed, err)
	}
}
