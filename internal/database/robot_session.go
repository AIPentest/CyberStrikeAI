package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RobotSessionBinding is one robot session binding row.
type RobotSessionBinding struct {
	SessionKey     string
	ConversationID string
	RoleName       string
	AgentMode      string
	UpdatedAt      time.Time
}

// GetRobotSessionBinding loads one robot session binding by session_key.
func (db *DB) GetRobotSessionBinding(sessionKey string) (*RobotSessionBinding, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, nil
	}
	var b RobotSessionBinding
	var updatedAt string
	err := db.QueryRow(
		"SELECT session_key, conversation_id, role_name, agent_mode, updated_at FROM robot_user_sessions WHERE session_key = ?",
		sessionKey,
	).Scan(&b.SessionKey, &b.ConversationID, &b.RoleName, &b.AgentMode, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query robot session binding: %w", err)
	}
	if t, e := time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt); e == nil {
		b.UpdatedAt = t
	} else if t, e := time.Parse("2006-01-02 15:04:05", updatedAt); e == nil {
		b.UpdatedAt = t
	} else {
		b.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	}
	if strings.TrimSpace(b.RoleName) == "" {
		b.RoleName = "Default"
	}
	if strings.TrimSpace(b.AgentMode) == "" {
		b.AgentMode = "eino_single"
	}
	return &b, nil
}

// UpsertRobotSessionBinding writes or updates one robot session binding (including role).
func (db *DB) UpsertRobotSessionBinding(sessionKey, conversationID, roleName, agentMode string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	conversationID = strings.TrimSpace(conversationID)
	roleName = strings.TrimSpace(roleName)
	agentMode = strings.TrimSpace(agentMode)
	if sessionKey == "" || conversationID == "" {
		return nil
	}
	if roleName == "" {
		roleName = "Default"
	}
	if agentMode == "" {
		agentMode = "eino_single"
	}
	_, err := db.Exec(`
		INSERT INTO robot_user_sessions (session_key, conversation_id, role_name, agent_mode, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(session_key) DO UPDATE SET
			conversation_id = excluded.conversation_id,
			role_name = excluded.role_name,
			agent_mode = excluded.agent_mode,
			updated_at = excluded.updated_at
	`, sessionKey, conversationID, roleName, agentMode, time.Now())
	if err != nil {
		return fmt.Errorf("write robot session binding: %w", err)
	}
	return nil
}

// DeleteRobotSessionBinding deletes one robot session binding.
func (db *DB) DeleteRobotSessionBinding(sessionKey string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	if _, err := db.Exec("DELETE FROM robot_user_sessions WHERE session_key = ?", sessionKey); err != nil {
		return fmt.Errorf("delete robot session binding: %w", err)
	}
	return nil
}
