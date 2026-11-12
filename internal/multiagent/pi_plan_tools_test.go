package multiagent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"go.uber.org/zap"
)

func TestPiPlanBoardRemainsReadableByConversationAPI(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{SkillsDir: dir}
	r := &piRun{cfg: cfg, conversationID: "fixture"}
	db, err := database.NewDB(filepath.Join(dir, "fixture.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetEinoConversationDirs(filepath.Join(dir, ".eino", "plantask"), "", "", "")
	created, err := r.executePiPlan(context.Background(), agentruntime.ToolCall{Name: "TaskCreate", Arguments: map[string]any{"subject": "Collect evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	var task map[string]any
	if err = json.Unmarshal([]byte(created.Content), &task); err != nil {
		t.Fatal(err)
	}
	_, err = r.executePiPlan(context.Background(), agentruntime.ToolCall{Name: "TaskUpdate", Arguments: map[string]any{"taskId": task["id"], "status": "completed"}})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := db.ListConversationPlanTasks("fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Status != "completed" || tasks[0].Subject != "Collect evidence" {
		t.Fatalf("API lost plan state: %+v", tasks)
	}
	_, err = r.executePiPlan(context.Background(), agentruntime.ToolCall{Name: "write_todos", Arguments: map[string]any{"todos": []any{map[string]any{"content": "Verify result", "status": "in_progress"}}}})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err = db.ListConversationPlanTasks("fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Subject != "Verify result" || tasks[0].Status != "in_progress" {
		t.Fatalf("replacement board incorrect: %+v", tasks)
	}
}
