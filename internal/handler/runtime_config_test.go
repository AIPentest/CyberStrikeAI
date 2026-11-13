package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestRuntimeSessionDirectoryUpdateCleansOldNewAndDefaultRoots(t *testing.T) {
	t.Chdir(t.TempDir())
	db, err := database.NewDB("sessions.db", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("root switch", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.AgentRuntime.SessionDir = "old"
	if err := os.WriteFile("config.yaml", []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &ConfigHandler{config: cfg, db: db, logger: zap.NewNop(), configPath: "config.yaml"}
	router := gin.New()
	router.POST("/config", h.UpdateConfig)
	var paths []string
	seed := func() {
		path := filepath.Join(cfg.AgentSessionDir(), agentruntime.ConversationStorageKey(conv.ID), "single", "session.jsonl")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	seed()
	for _, dir := range []string{"new", ""} {
		body, _ := json.Marshal(map[string]any{"agent_runtime": map[string]string{"session_dir": dir}})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/config", bytes.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("update: %d %s", w.Code, w.Body.String())
		}
		seed()
	}
	if cfg.AgentSessionDir() != filepath.Join("data", "pi-sessions") {
		t.Fatal("empty database path selected the wrong default")
	}
	if err := db.DeleteConversation(conv.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("session left after directory switch: %s %v", path, err)
		}
	}
}

func TestDeleteTurnRejectsRunningConversation(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "sessions.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("running", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := db.AddMessage(conv.ID, "user", "still running", nil)
	if err != nil {
		t.Fatal(err)
	}
	h := NewConversationHandler(db, zap.NewNop())
	h.SetTaskStateProvider(staticConversationTaskState{running: true})
	router := gin.New()
	router.POST("/conversations/:id/delete-turn", h.DeleteConversationTurn)
	body, _ := json.Marshal(DeleteTurnRequest{MessageID: msg.ID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/conversations/"+conv.ID+"/delete-turn", bytes.NewReader(body)))
	if w.Code != http.StatusConflict {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	messages, err := db.GetMessages(conv.ID)
	if err != nil || len(messages) != 1 {
		t.Fatalf("running history changed: %v %v", messages, err)
	}
}

func TestPiRuntimeSettingsSurviveSaveAndPartialUpdate(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(`
agent_runtime:
  node_path: /custom/node
  worker_path: /custom/worker.js
  session_dir: /private/sessions
multi_agent:
  eino_middleware:
    tool_search_enable: true
  eino_skills:
    disable: false
  eino_callbacks:
    enabled: true
`), &doc); err != nil {
		t.Fatal(err)
	}
	var original config.Config
	if err := doc.Decode(&original); err != nil {
		t.Fatal(err)
	}
	cfg := original.AgentRuntime
	nodePath := "/updated/node"
	applyAgentRuntimeUpdate(&cfg, &AgentRuntimeUpdate{NodePath: &nodePath})
	updateAgentRuntimeConfig(&doc, cfg)
	updateMultiAgentConfig(&doc, config.MultiAgentConfig{})
	var restored config.Config
	if err := doc.Decode(&restored); err != nil {
		t.Fatal(err)
	}
	if restored.AgentRuntime.Engine != "pi" || restored.AgentRuntime.NodePath != nodePath || restored.AgentRuntime.WorkerPath != "/custom/worker.js" || restored.AgentRuntime.SessionDir != "/private/sessions" {
		t.Fatalf("lost runtime settings: %+v", restored.AgentRuntime)
	}
	if !restored.MultiAgent.Runtime.ToolSearchEnable {
		t.Fatal("legacy runtime settings were discarded")
	}
	var raw map[string]any
	if err := doc.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	ma := raw["multi_agent"].(map[string]any)
	for _, key := range []string{"eino_middleware", "eino_skills", "eino_callbacks"} {
		if _, ok := ma[key]; ok {
			t.Fatalf("retired key emitted: %s", key)
		}
	}
}
