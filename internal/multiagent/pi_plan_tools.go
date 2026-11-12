package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"cyberstrike-ai/internal/agentruntime"
)

var piPlanMu sync.Mutex

func piPlanTools() []agentruntime.Tool {
	str := map[string]any{"type": "string"}
	return []agentruntime.Tool{
		{Name: "TaskCreate", Description: "Create a persisted task on this conversation's progress board", Parameters: piToolSchema(map[string]any{"subject": str, "description": str, "activeForm": str}, "subject")},
		{Name: "TaskUpdate", Description: "Update an existing task's status or description", Parameters: piToolSchema(map[string]any{"taskId": str, "subject": str, "description": str, "activeForm": str, "status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "deleted"}}}, "taskId")},
		{Name: "TaskGet", Description: "Read a task by its conversation-local ID", Parameters: piToolSchema(map[string]any{"taskId": str}, "taskId")},
		{Name: "TaskList", Description: "Read the current conversation progress board", Parameters: piToolSchema(map[string]any{})},
	}
}

func (r *piRun) planDir() (string, error) {
	if r.conversationID == "" || strings.ContainsAny(r.conversationID, "/\\") || r.conversationID == ".." {
		return "", fmt.Errorf("valid conversation ID required for a task board")
	}
	root := r.cfg.SkillsDir
	if root == "" {
		return "", fmt.Errorf("skills directory required for task board")
	}
	rel := r.cfg.MultiAgent.EinoMiddleware.PlantaskRelDir
	if rel == "" {
		rel = ".eino/plantask"
	}
	return filepath.Join(root, rel, r.conversationID), nil
}

func readPiPlan(dir string) ([]map[string]any, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []map[string]any
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".json")); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var task map[string]any
		if err = json.Unmarshal(b, &task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		a, _ := strconv.Atoi(fmt.Sprint(tasks[i]["id"]))
		b, _ := strconv.Atoi(fmt.Sprint(tasks[j]["id"]))
		return a < b
	})
	return tasks, nil
}

func writePiPlanTask(dir string, task map[string]any) error {
	id := fmt.Sprint(task["id"])
	if n, err := strconv.Atoi(id); err != nil || n <= 0 {
		return fmt.Errorf("invalid task ID")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(task)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".task-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, id+".json"))
}

func (r *piRun) executePiPlan(ctx context.Context, call agentruntime.ToolCall) (agentruntime.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolResult{}, err
	}
	dir, err := r.planDir()
	if err != nil {
		return agentruntime.ToolResult{}, err
	}
	piPlanMu.Lock()
	defer piPlanMu.Unlock()
	tasks, err := readPiPlan(dir)
	if err != nil {
		return agentruntime.ToolResult{}, err
	}
	var output any
	if call.Name == "write_todos" {
		todos, ok := call.Arguments["todos"].([]any)
		if !ok {
			return agentruntime.ToolResult{}, fmt.Errorf("todos must be an array")
		}
		var replacement []map[string]any
		for i, entry := range todos {
			todo, ok := entry.(map[string]any)
			if !ok {
				return agentruntime.ToolResult{}, fmt.Errorf("invalid todo")
			}
			subject, _ := todo["content"].(string)
			status, _ := todo["status"].(string)
			if subject == "" || (status != "pending" && status != "in_progress" && status != "completed") {
				return agentruntime.ToolResult{}, fmt.Errorf("invalid todo subject/status")
			}
			replacement = append(replacement, map[string]any{"id": strconv.Itoa(i + 1), "subject": subject, "status": status})
		}
		for _, task := range replacement {
			if err = writePiPlanTask(dir, task); err != nil {
				return agentruntime.ToolResult{}, err
			}
		}
		for _, task := range tasks {
			id, _ := strconv.Atoi(fmt.Sprint(task["id"]))
			if id > len(replacement) {
				task["status"] = "deleted"
				if err = writePiPlanTask(dir, task); err != nil {
					return agentruntime.ToolResult{}, err
				}
			}
		}
		output = map[string]any{"tasks": replacement}
	} else if call.Name == "TaskList" {
		visible := []map[string]any{}
		for _, task := range tasks {
			if task["status"] != "deleted" {
				visible = append(visible, task)
			}
		}
		output = map[string]any{"tasks": visible}
	} else if call.Name == "TaskCreate" {
		subject, _ := call.Arguments["subject"].(string)
		if strings.TrimSpace(subject) == "" {
			return agentruntime.ToolResult{}, fmt.Errorf("subject is required")
		}
		id := 1
		for _, task := range tasks {
			n, _ := strconv.Atoi(fmt.Sprint(task["id"]))
			if n >= id {
				id = n + 1
			}
		}
		task := map[string]any{"id": strconv.Itoa(id), "subject": subject, "status": "pending", "description": call.Arguments["description"], "activeForm": call.Arguments["activeForm"]}
		if err = writePiPlanTask(dir, task); err != nil {
			return agentruntime.ToolResult{}, err
		}
		output = task
	} else {
		id, _ := call.Arguments["taskId"].(string)
		var selected map[string]any
		for _, task := range tasks {
			if fmt.Sprint(task["id"]) == id {
				selected = task
				break
			}
		}
		if selected == nil {
			return agentruntime.ToolResult{}, fmt.Errorf("task does not exist")
		}
		if call.Name == "TaskUpdate" {
			for _, key := range []string{"subject", "description", "activeForm", "status"} {
				if v, ok := call.Arguments[key]; ok {
					value, ok := v.(string)
					if !ok {
						return agentruntime.ToolResult{}, fmt.Errorf("%s must be a string", key)
					}
					if key == "status" && value != "pending" && value != "in_progress" && value != "completed" && value != "deleted" {
						return agentruntime.ToolResult{}, fmt.Errorf("invalid task status")
					}
					selected[key] = v
				}
			}
			if err = writePiPlanTask(dir, selected); err != nil {
				return agentruntime.ToolResult{}, err
			}
		}
		output = selected
	}
	body, err := json.Marshal(output)
	return agentruntime.ToolResult{Content: string(body)}, err
}
