package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestBuiltinAdminProtectionPreservesAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.NewDB(filepath.Join(t.TempDir(), "admin-guard.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	auth := security.NewAuthManager(12)
	password, err := auth.AttachRBACStore(db)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.Authenticate("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	h := NewRBACHandler(db, zap.NewNop())
	h.SetAuthManager(auth)
	router := gin.New()
	router.Use(security.AuthMiddleware(auth), security.RBACMiddleware(db))
	router.POST("/api/rbac/users", h.CreateUser)
	router.PUT("/api/rbac/users/:id", h.UpdateUser)
	router.DELETE("/api/rbac/users/:id", h.DeleteUser)
	router.GET("/api/rbac/users", h.ListUsers)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	// The reported trigger must not change the administrator's authorization.
	w := request(http.MethodPost, "/api/rbac/users", `{"username":"new-operator","password":"operator-password","roles":["operator"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create operator: %d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{
		`{"display_name":"must not persist","roles":[],"password":"changed-password"}`,
		`{"roles":["operator"]}`,
		`{"enabled":false}`,
		`{"enabled":false,"roles":["admin"]}`,
	} {
		w := request(http.MethodPut, "/api/rbac/users/admin", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("update %s: %d %s", body, w.Code, w.Body.String())
		}
		if w := request(http.MethodGet, "/api/rbac/users", ""); w.Code != http.StatusOK {
			t.Fatalf("rejection revoked admin access: %d %s", w.Code, w.Body.String())
		}
	}
	if w := request(http.MethodDelete, "/api/rbac/users/admin", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("delete builtin: %d %s", w.Code, w.Body.String())
	}
	user, err := db.GetRBACUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "管理员" || !user.Enabled {
		t.Fatalf("rejected update mutated admin: %#v", user)
	}
	newToken, _, err := auth.Authenticate("admin", password)
	if err != nil {
		t.Fatalf("original password no longer works: %v", err)
	}
	for _, tok := range []string{token, newToken} {
		session, ok := auth.ValidateToken(tok)
		if !ok || !session.Permissions["rbac:write"] || session.Scope != database.RBACScopeAll {
			t.Fatalf("admin authorization lost: %#v valid=%v", session, ok)
		}
	}
}
