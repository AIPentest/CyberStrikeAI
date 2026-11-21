package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestConversationHumanApproval(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "conversation-approval.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateRBACUser("reviewer", "Reviewer", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := &AgentHandler{db: db, logger: zap.NewNop(), hitlManager: NewHITLManager(db, zap.NewNop())}
	if err := h.hitlManager.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	session := security.Session{
		UserID: user.ID, Scope: database.RBACScopeAll,
		Permissions: map[string]bool{"chat:write": true, "hitl:write": true},
	}

	t.Run("new conversation saves the policy for subsequent turns", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/agent/stream", nil)
		ctx.Set(security.ContextSessionKey, session)
		req := &ChatRequest{Message: "Please summarize this example.", RequireHumanApproval: true}
		prep, err := h.prepareMultiAgentSession(req, ctx, "agent_stream")
		if err != nil {
			t.Fatal(err)
		}
		if req.Hitl == nil || !req.Hitl.Enabled || req.Hitl.Mode != "approval" || req.Hitl.Reviewer != "human" {
			t.Fatalf("initial turn did not require human approval: %+v", req.Hitl)
		}

		// A fresh runtime handling the same conversation reads its saved policy.
		h.hitlManager = NewHITLManager(db, zap.NewNop())
		h.activateHITLForConversation(prep.ConversationID, nil)
		policy, needsApproval := h.hitlManager.shouldInterrupt(prep.ConversationID, "fixture_operation")
		if !needsApproval || policy.Mode != "approval" || policy.Reviewer != "human" {
			t.Fatalf("subsequent turn did not inherit human approval: %+v", policy)
		}
	})

	t.Run("new conversation saves the complete session policy", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/agent/stream", nil)
		ctx.Set(security.ContextSessionKey, session)
		req := &ChatRequest{Message: "Review edit policy fixture", Hitl: &HITLRequest{
			Enabled: true, Mode: "review_edit", Reviewer: "audit_agent",
			SensitiveTools: []string{"fixture_read_only"}, TimeoutSeconds: 120,
		}}
		prep, err := h.prepareMultiAgentSession(req, ctx, "agent_stream")
		if err != nil {
			t.Fatal(err)
		}
		saved, err := h.hitlManager.LoadConversationConfig(prep.ConversationID)
		if err != nil {
			t.Fatal(err)
		}
		if !saved.Enabled || saved.Mode != "review_edit" || saved.Reviewer != "audit_agent" ||
			saved.TimeoutSeconds != 120 || !slices.Equal(saved.SensitiveTools, req.Hitl.SensitiveTools) {
			t.Fatalf("unexpected saved session policy: %+v", saved)
		}
	})

	t.Run("existing conversation saves human approval and retains its settings", func(t *testing.T) {
		conv, err := db.CreateConversation("Existing conversation", database.ConversationCreateMeta{})
		if err != nil {
			t.Fatal(err)
		}
		exempt := []string{"fixture_read_only"}
		if err := h.hitlManager.SaveConversationConfig(conv.ID, &HITLRequest{
			Enabled: true, Mode: "approval", Reviewer: "audit_agent",
			SensitiveTools: exempt, TimeoutSeconds: 90,
		}); err != nil {
			t.Fatal(err)
		}
		save := func(session security.Session) int {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Params = gin.Params{{Key: "conversationId", Value: conv.ID}}
			ctx.Set(security.ContextSessionKey, session)
			h.RequireHITLConversationHumanApproval(ctx)
			return recorder.Code
		}
		readOnly := session
		readOnly.Permissions = map[string]bool{"chat:write": true, "hitl:read": true}
		if code := save(readOnly); code != http.StatusForbidden {
			t.Fatalf("read-only session could change approval: %d", code)
		}
		if code := save(session); code != http.StatusOK {
			t.Fatalf("failed to save human approval: %d", code)
		}
		saved, err := h.hitlManager.LoadConversationConfig(conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !saved.Enabled || saved.Mode != "approval" || saved.Reviewer != "human" ||
			saved.TimeoutSeconds != 90 || !slices.Equal(saved.SensitiveTools, exempt) {
			t.Fatalf("unexpected saved approval policy: %+v", saved)
		}
	})

	t.Run("failed approval write also rolls back creation", func(t *testing.T) {
		if _, err := db.Exec(`CREATE TRIGGER reject_fixture_approval BEFORE INSERT ON hitl_conversation_configs
			BEGIN SELECT RAISE(ABORT, 'test approval write failed'); END`); err != nil {
			t.Fatal(err)
		}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/agent/stream", nil)
		ctx.Set(security.ContextSessionKey, session)
		req := &ChatRequest{Message: "Approval rollback fixture", RequireHumanApproval: true}
		if _, err := h.prepareMultiAgentSession(req, ctx, "agent_stream"); err == nil {
			t.Fatal("conversation preparation succeeded without saving approval")
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM conversations WHERE title = ?", req.Message).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed approval write left %d conversations: %v", count, err)
		}
	})
}
