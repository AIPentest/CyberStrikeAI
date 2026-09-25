package security

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Platform permissions use module:action naming. They are intentionally
// separate from AI testing roles under roles/.
var PermissionCatalog = map[string]string{
	"auth:self":            "Manage own session and password",
	"dashboard:read":       "View dashboard summaries",
	"chat:read":            "View conversations",
	"chat:write":           "Create and update conversations",
	"chat:delete":          "Delete conversations and turns",
	"agent:execute":        "Run AI agents and workflows",
	"agent:local-execute":  "Use local filesystem, shell, and configured command tools from an agent",
	"hitl:read":            "View HITL queues and logs",
	"hitl:write":           "Approve, dismiss, and configure HITL",
	"tasks:read":           "View task queues",
	"tasks:write":          "Create and run task queues",
	"tasks:delete":         "Delete task queues",
	"project:read":         "View projects and project facts",
	"project:write":        "Create and update projects and facts",
	"project:delete":       "Delete projects and facts",
	"vulnerability:read":   "View vulnerabilities",
	"vulnerability:write":  "Create and update vulnerabilities",
	"vulnerability:delete": "Delete vulnerabilities",
	"asset:read":           "View managed assets and asset summaries",
	"asset:write":          "Create, import, and update assets",
	"asset:delete":         "Delete managed assets",
	"webshell:read":        "View WebShell connections",
	"webshell:write":       "Manage and use WebShell connections",
	"webshell:delete":      "Delete WebShell connections",
	"c2:read":              "View C2 listeners, sessions, tasks, events, and profiles",
	"c2:write":             "Operate C2 listeners, sessions, tasks, payloads, files, and profiles",
	"c2:delete":            "Delete C2 objects",
	"mcp:read":             "View MCP status and external MCP configuration",
	"mcp:execute":          "Invoke the authenticated MCP endpoint",
	"mcp:external:execute": "Invoke tools exposed by configured external MCP servers",
	"mcp:write":            "Manage external MCP server configuration and lifecycle",
	"knowledge:read":       "View knowledge base and retrieval logs",
	"knowledge:write":      "Create, update, index, and scan knowledge base",
	"knowledge:delete":     "Delete knowledge items and retrieval logs",
	"skills:read":          "View skills and skill stats",
	"skills:write":         "Create and update skills",
	"skills:delete":        "Delete skills and stats",
	"agents:read":          "View markdown agents",
	"agents:write":         "Create and update markdown agents",
	"agents:delete":        "Delete markdown agents",
	"roles:read":           "View AI testing roles",
	"roles:write":          "Create and update AI testing roles",
	"roles:delete":         "Delete AI testing roles",
	"workflow:read":        "View workflow definitions and runs",
	"workflow:execute":     "Validate, dry-run, and resume authorized workflow runs",
	"workflow:write":       "Create and update workflow definitions",
	"workflow:delete":      "Delete workflows",
	"config:read":          "View system configuration",
	"config:write":         "Update and apply system configuration",
	"storage:read":         "View runtime storage usage and reclaimable space",
	"storage:write":        "Run runtime storage cleanup and change its retention policy",
	"terminal:execute":     "Run terminal commands",
	"audit:read":           "View and export audit logs",
	"audit:delete":         "Delete audit logs",
	"rbac:read":            "View users, platform roles, permissions, and assignments",
	"rbac:write":           "Manage users, platform roles, permissions, and assignments",
	"notification:read":    "View notifications",
	"notification:write":   "Mark notifications as read",
	"robot:read":           "View robot binding status",
	"robot:write":          "Manage robot bindings and test robot callbacks",
	"files:read":           "View chat uploads",
	"files:write":          "Upload, edit, and rename chat files",
	"files:delete":         "Delete chat files",
	"attackchain:read":     "View attack chains",
	"attackchain:write":    "Regenerate attack chains",
	"fofa:execute":         "Run FOFA searches and query parsing",
	"openapi:read":         "Read OpenAPI aggregation results",
	"monitor:read":         "View execution monitor",
	"monitor:write":        "Cancel monitor executions",
	"monitor:delete":       "Delete monitor executions",
}

func HashPassword(password string) (string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return "", fmt.Errorf("password is empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func VerifyPasswordHash(password, encoded string) bool {
	if strings.HasPrefix(encoded, "$2a$") || strings.HasPrefix(encoded, "$2b$") || strings.HasPrefix(encoded, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(strings.TrimSpace(password))) == nil
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "sha256" {
		return false
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	sum := sha256.Sum256(append(salt, []byte(strings.TrimSpace(password))...))
	return subtle.ConstantTimeCompare(sum[:], expected) == 1
}
