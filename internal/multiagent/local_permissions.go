package multiagent

import (
	"context"
	"cyberstrike-ai/internal/authctx"
	"strings"
)

func isLocalPrivilegeTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "execute", "ls", "read_file", "write_file", "edit_file", "glob", "grep":
		return true
	default:
		return false
	}
}

func localToolPermissionDenied(ctx context.Context, name string) bool {
	if !isLocalPrivilegeTool(name) {
		return false
	}
	principal, ok := authctx.PrincipalFromContext(ctx)
	return !ok || !principal.HasPermission("agent:local-execute")
}
