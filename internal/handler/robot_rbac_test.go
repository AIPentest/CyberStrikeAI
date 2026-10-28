package handler

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"go.uber.org/zap"
)

func TestRobotUsersAreResourceIsolated(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "robot-rbac.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := &config.Config{}
	cfg.Project.Enabled = true
	h := NewRobotHandler(cfg, db, nil, zap.NewNop())
	if err := db.BootstrapRBAC("hash", security.PermissionCatalog); err != nil {
		t.Fatal(err)
	}
	alice, err := db.CreateRBACUser("robot-alice", "Robot Alice", "hash", true, []string{database.RBACSystemRoleOperator})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := db.CreateRBACUser("robot-bob", "Robot Bob", "hash", true, []string{database.RBACSystemRoleOperator})
	if err != nil {
		t.Fatal(err)
	}
	for externalID, user := range map[string]*database.RBACUser{"alice": alice, "bob": bob} {
		code := "TEST-" + strings.ToUpper(externalID)
		if err := db.CreateRobotBindingCode(user.ID, hashRobotBindingCode(code), time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ConsumeRobotBindingCode("wecom", externalID, hashRobotBindingCode(code)); err != nil {
			t.Fatal(err)
		}
	}
	aliceAccess, err := db.ResolveRobotRBACAccess("wecom", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got := h.HandleMessage("wecom", "alice", "身份"); !strings.Contains(got, "Robot Alice") || !strings.Contains(got, alice.ID) || !strings.Contains(got, "user_binding") {
		t.Fatalf("bound identity output is incomplete: %s", got)
	}

	conversationID, _ := h.getOrCreateConversation("wecom", "alice", "alice conversation", aliceAccess)
	if conversationID == "" {
		t.Fatal("alice conversation was not created")
	}
	if got := h.cmdList("wecom", "bob"); strings.Contains(got, conversationID) {
		t.Fatalf("bob listed alice conversation: %s", got)
	}
	if got := h.cmdSwitch("wecom", "bob", conversationID); !strings.Contains(got, "not found") && !strings.Contains(got, "no access") {
		t.Fatalf("bob switched to alice conversation: %s", got)
	}
	if got := h.cmdDelete("wecom", "bob", conversationID); !strings.Contains(got, "no access") {
		t.Fatalf("bob deleted alice conversation: %s", got)
	}
	if _, err := db.GetConversation(conversationID); err != nil {
		t.Fatalf("alice conversation was deleted: %v", err)
	}

	createReply := h.cmdNewProject("wecom", "alice", "alice project")
	if !strings.Contains(createReply, "Project created") {
		t.Fatalf("create project reply: %s", createReply)
	}
	if got := h.cmdProjects("wecom", "bob"); strings.Contains(got, "alice project") {
		t.Fatalf("bob listed alice project: %s", got)
	}
}

func TestWecomReplayGuardRequiresFreshUniqueRequest(t *testing.T) {
	h := NewRobotHandler(&config.Config{}, nil, nil, zap.NewNop())
	timestamp := time.Now().Unix()
	if !h.acceptFreshWecomRequest(fmt.Sprintf("%d", timestamp), "nonce", "signature") {
		t.Fatal("fresh request was rejected")
	}
	if h.acceptFreshWecomRequest(fmt.Sprintf("%d", timestamp), "nonce", "signature") {
		t.Fatal("duplicate request was accepted")
	}
	if h.acceptFreshWecomRequest(fmt.Sprintf("%d", timestamp-600), "old", "signature") {
		t.Fatal("stale request was accepted")
	}
}

func TestRobotServiceAccountRequiresExactSenderAllowlist(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "robot-service.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.BootstrapRBAC("hash", security.PermissionCatalog); err != nil {
		t.Fatal(err)
	}
	serviceUser, err := db.CreateRBACUser("robot-service-user", "Robot Service", "hash", true, []string{database.RBACSystemRoleOperator})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Robots.Lark.Auth = config.RobotAuthorizationConfig{
		Mode: config.RobotAuthModeServiceAccount, ServiceUserID: serviceUser.ID,
		AllowedExternalUsers: []string{"t:tenant|u:allowed"},
	}
	h := NewRobotHandler(cfg, db, nil, zap.NewNop())
	if got := h.HandleMessage("lark", "t:tenant|u:allowed", "列表"); strings.Contains(got, "allowlist") || strings.Contains(got, "not bound") {
		t.Fatalf("allowed service account sender was denied: %s", got)
	}
	if got := h.HandleMessage("lark", "t:tenant|u:denied", "列表"); !strings.Contains(got, "allowlist") {
		t.Fatalf("non-allowlisted sender was not denied: %s", got)
	}
	if got := h.HandleMessage("lark", "t:tenant|u:allowed", "绑定 ABCD-1234"); !strings.Contains(got, "service account mode") {
		t.Fatalf("service-account robot accepted user binding: %s", got)
	}
	if got := h.HandleMessage("lark", "t:tenant|u:allowed", "whoami"); !strings.Contains(got, "Robot Service") || !strings.Contains(got, serviceUser.ID) || !strings.Contains(got, "service_account") {
		t.Fatalf("service-account identity output is incomplete: %s", got)
	}
	if got := h.HandleMessage("lark", "t:tenant|u:denied", "whoami"); !strings.Contains(got, "Authorization status: denied") || strings.Contains(got, serviceUser.ID) {
		t.Fatalf("denied identity output leaked or omitted status: %s", got)
	}

	adminCfg := &config.Config{}
	adminCfg.Robots.Lark.Auth = config.RobotAuthorizationConfig{
		Mode: config.RobotAuthModeServiceAccount, ServiceUserID: "admin",
		AllowedExternalUsers: []string{"t:tenant|u:owner"},
	}
	adminHandler := NewRobotHandler(adminCfg, db, nil, zap.NewNop())
	if got := adminHandler.HandleMessage("lark", "t:tenant|u:owner", "身份"); !strings.Contains(got, "admin") || !strings.Contains(got, "Authorization status: authorized") {
		t.Fatalf("allowlisted admin service account was denied: %s", got)
	}
}
