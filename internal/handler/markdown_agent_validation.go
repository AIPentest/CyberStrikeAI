package handler

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"cyberstrike-ai/internal/agents"
	"gopkg.in/yaml.v3"
)

var markdownAgentIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func (h *MarkdownAgentsHandler) validateMarkdownWrite(filename string, content []byte) error {
	front, _, err := agents.SplitFrontMatter(string(content))
	if err != nil {
		return err
	}
	var fm agents.FrontMatter
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return err
	}
	if strings.TrimSpace(fm.Name) == "" {
		return fmt.Errorf("agent name must not be empty")
	}
	if fm.ID != "" && !markdownAgentIDPattern.MatchString(fm.ID) {
		return fmt.Errorf("invalid Agent ID format")
	}
	sub, err := agents.ParseMarkdownSubAgent(filename, string(content))
	if err != nil {
		return err
	}
	if !markdownAgentIDPattern.MatchString(sub.ID) {
		return fmt.Errorf("Agent ID must be 1-64 letters, digits, hyphens, or underscores, starting with a letter or digit")
	}
	if err := validateRoleName(sub.Name); err != nil {
		return fmt.Errorf("invalid agent name: %w", err)
	}
	if strings.TrimSpace(sub.Instruction) == "" {
		return fmt.Errorf("agent instruction must not be empty")
	}
	if sub.MaxIterations < 0 {
		return fmt.Errorf("max iterations must not be negative")
	}
	files, err := agents.LoadMarkdownAgentFiles(h.dir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if filepath.Base(file.Filename) != filepath.Base(filename) && file.Config.ID == sub.ID {
			return fmt.Errorf("Agent ID already used by file %s", file.Filename)
		}
	}
	return nil
}
