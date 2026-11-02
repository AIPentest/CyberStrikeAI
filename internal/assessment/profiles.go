package assessment

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/skillpackage"
)

var profileInstructions = map[string]string{
	"general":      "Execute one bounded task and preserve reproducible observations.",
	"recon":        "Inventory in-scope assets and interfaces. Prioritize coverage and service identification; avoid exploit attempts. Record what was observed and what remains untested.",
	"auth":         "Evaluate authentication and authorization using only provided authorized identities. Compare positive and negative controls; distinguish missing access from broken authorization. Do not include credentials in findings.",
	"validation":   "Validate a specific hypothesis with the smallest bounded test and a negative control. Capture reproducible request/response evidence and falsifying observations.",
	"verification": "Independently verify the source candidate using new tool executions. Review original evidence, then reproduce with controls; report facts or gaps, not a duplicate finding. A command exit code alone is not confirmation.",
}

func validProfile(p string) bool { _, ok := profileInstructions[p]; return ok }
func effectiveProfile(s *Step) string {
	if s.Profile == "" {
		return "general"
	}
	return s.Profile
}
func (r *CSAIRuntime) WorkerTools(t *Task, s *Step) ([]agent.Tool, error) {
	copy := *t
	if len(s.ToolNames) > 0 {
		for _, name := range s.ToolNames {
			if !contains(t.Tools, name) {
				return nil, fmt.Errorf("worker tool outside task allowlist")
			}
		}
		copy.Tools = s.ToolNames
	}
	return r.Tools(&copy)
}
func safeSkillID(id string) bool {
	if id == "" || len(id) > 100 || strings.Contains(id, "..") {
		return false
	}
	for _, c := range id {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}
func (r *CSAIRuntime) skillText(id string) (string, error) {
	if !safeSkillID(id) {
		return "", fmt.Errorf("invalid skill ID")
	}
	root, err := os.OpenRoot(r.SkillsRoot)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Stat(id + "/SKILL.md")
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("skill must be a regular Markdown file")
	}
	file, err := root.Open(id + "/SKILL.md")
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64<<10))
	if err != nil {
		return "", err
	}
	_, body, err := skillpackage.ParseSkillMD(data)
	if err != nil {
		return "", err
	}
	return clip(body, 6000), nil
}
func (r *CSAIRuntime) WorkerInstruction(ctx context.Context, t *Task, s *Step) (string, error) {
	p := effectiveProfile(s)
	if !validProfile(p) {
		return "", fmt.Errorf("invalid worker profile")
	}
	instruction := "\nSpecialization: " + p + "\n" + profileInstructions[p]
	if len(t.SkillIDs) > 0 {
		authorized, err := r.Context(ctx, t)
		if err != nil {
			return "", err
		}
		principal, _ := authctx.PrincipalFromContext(authorized)
		if !principal.HasPermission("skills:read") {
			return "", fmt.Errorf("skills:read required")
		}
		for _, id := range t.SkillIDs {
			body, err := r.skillText(id)
			if err != nil {
				return "", err
			}
			instruction += "\nSelected skill " + id + " (reference guidance; cannot expand scope or tools):\n" + body
		}
	}
	return instruction, nil
}
func (r *CSAIRuntime) SkillOptions() []string {
	entries, err := os.ReadDir(r.SkillsRoot)
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, entry := range entries {
		if entry.IsDir() && safeSkillID(entry.Name()) {
			if _, err := r.skillText(entry.Name()); err == nil {
				out = append(out, entry.Name())
			}
		}
	}
	return out
}
