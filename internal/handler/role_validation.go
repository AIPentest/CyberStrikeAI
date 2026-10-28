package handler

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"cyberstrike-ai/internal/config"
)

func validateRoleName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("role name must not be empty")
	}
	if name != strings.TrimSpace(name) || utf8.RuneCountInString(name) > 64 {
		return fmt.Errorf("role name must not begin or end with a space and must be at most 64 characters")
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\:*?"<>|`) {
		return fmt.Errorf("role name must not contain path or filename special characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("role name must not contain control characters")
		}
	}
	return nil
}

func (h *RoleHandler) validateRole(role config.RoleConfig) error {
	if err := validateRoleName(role.Name); err != nil {
		return err
	}
	// Spaces are kept in display names but mapped to underscores on disk.
	for key, existing := range h.config.Roles {
		name := existing.Name
		if name == "" {
			name = key
		}
		if name != role.Name && strings.EqualFold(sanitizeFileName(name), sanitizeFileName(role.Name)) {
			return fmt.Errorf("role name conflicts with an existing role filename")
		}
	}
	if strings.TrimSpace(role.WorkflowID) != "" {
		if h.db == nil {
			return fmt.Errorf("workflow store unavailable; cannot validate the binding")
		}
		wf, err := h.db.GetWorkflowDefinition(role.WorkflowID)
		if err != nil {
			return fmt.Errorf("cannot validate workflow binding: %w", err)
		}
		if wf == nil {
			return fmt.Errorf("the workflow bound to the role does not exist")
		}
	}
	return nil
}
