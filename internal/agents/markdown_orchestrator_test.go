package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMarkdownAgentsDir_OrchestratorExcludedFromSubs(t *testing.T) {
	dir := t.TempDir()
	orch := filepath.Join(dir, OrchestratorMarkdownFilename)
	if err := os.WriteFile(orch, []byte(`---
id: cyberstrike-deep
name: Main
description: Test desc
---

Hello orchestrator
`), 0644); err != nil {
		t.Fatal(err)
	}
	subPath := filepath.Join(dir, "worker.md")
	if err := os.WriteFile(subPath, []byte(`---
id: worker
name: Worker
description: W
---

Do work
`), 0644); err != nil {
		t.Fatal(err)
	}
	load, err := LoadMarkdownAgentsDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if load.Orchestrator == nil || load.Orchestrator.AgentName != "cyberstrike-deep" {
		t.Fatalf("orchestrator: %+v", load.Orchestrator)
	}
	if len(load.SubAgents) != 1 || load.SubAgents[0].ID != "worker" {
		t.Fatalf("subs: %+v", load.SubAgents)
	}
	if len(load.FileEntries) != 2 {
		t.Fatalf("file entries: %d", len(load.FileEntries))
	}
	var orchFile *FileAgent
	for i := range load.FileEntries {
		if load.FileEntries[i].IsOrchestrator {
			orchFile = &load.FileEntries[i]
			break
		}
	}
	if orchFile == nil || orchFile.Filename != OrchestratorMarkdownFilename {
		t.Fatal("missing orchestrator file entry")
	}
}

func TestLoadMarkdownAgentsDir_DuplicateOrchestrator(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, OrchestratorMarkdownFilename), []byte("---\nname: A\n---\n\nx\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: B\nkind: orchestrator\n---\n\ny\n"), 0644)
	_, err := LoadMarkdownAgentsDir(dir)
	if err == nil {
		t.Fatal("expected duplicate orchestrator error")
	}
}

func TestLoadMarkdownAgentsDir_ModeOrchestratorsCoexist(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(OrchestratorMarkdownFilename, "---\nname: Deep\n---\n\ndeep\n")
	write(OrchestratorPlanExecuteMarkdownFilename, "---\nname: PE\n---\n\npe\n")
	write(OrchestratorSupervisorMarkdownFilename, "---\nname: SV\n---\n\nsv\n")
	write("worker.md", "---\nid: worker\nname: Worker\n---\n\nw\n")

	load, err := LoadMarkdownAgentsDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if load.Orchestrator == nil || load.Orchestrator.Instruction != "deep" {
		t.Fatalf("deep: %+v", load.Orchestrator)
	}
	if load.OrchestratorPlanExecute == nil || load.OrchestratorPlanExecute.Instruction != "pe" {
		t.Fatalf("pe: %+v", load.OrchestratorPlanExecute)
	}
	if load.OrchestratorSupervisor == nil || load.OrchestratorSupervisor.Instruction != "sv" {
		t.Fatalf("sv: %+v", load.OrchestratorSupervisor)
	}
	if len(load.SubAgents) != 1 || load.SubAgents[0].ID != "worker" {
		t.Fatalf("subs: %+v", load.SubAgents)
	}
}
