package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/c2"
	"cyberstrike-ai/internal/database"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// C2HITLBridge implements the C2 Manager's HITLBridge interface, bridging dangerous tasks to the existing HITL approval flow.
// Approval records are written to the hitl_interrupts table, sharing the frontend approval UI with the existing HITL system.
type C2HITLBridge struct {
	db        *database.DB
	logger    *zap.Logger
	timeout   time.Duration
	getConvID func() string
}

// NewC2HITLBridge creates a C2 HITL bridge
func NewC2HITLBridge(db *database.DB, logger *zap.Logger) *C2HITLBridge {
	return &C2HITLBridge{
		db:        db,
		logger:    logger,
		timeout:   5 * time.Minute,
		getConvID: func() string { return "" },
	}
}

// SetConversationIDGetter sets the function to get the current conversation ID
func (b *C2HITLBridge) SetConversationIDGetter(fn func() string) {
	b.getConvID = fn
}

// SetTimeout sets the approval timeout (0 means no timeout)
func (b *C2HITLBridge) SetTimeout(d time.Duration) {
	b.timeout = d
}

// RequestApproval implements the HITLBridge interface: writes to the hitl_interrupts table and polls while waiting for the approval result
func (b *C2HITLBridge) RequestApproval(ctx context.Context, req c2.HITLApprovalRequest) error {
	interruptID := "hitl_c2_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14]
	now := time.Now()

	convID := req.ConversationID
	if convID == "" {
		convID = b.getConvID()
	}
	if convID == "" {
		convID = "c2_system"
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"task_id":      req.TaskID,
		"session_id":   req.SessionID,
		"task_type":    req.TaskType,
		"payload":      req.PayloadJSON,
		"source":       req.Source,
		"reason":       req.Reason,
		"c2_operation": true,
	})

	_, err := b.db.Exec(`INSERT INTO hitl_interrupts
		(id, conversation_id, message_id, mode, tool_name, tool_call_id, payload, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?)`,
		interruptID, convID, "", "approval",
		c2.MCPToolC2Task, req.TaskID,
		string(payload), now,
	)
	if err != nil {
		b.logger.Error("C2 HITL: failed to create the approval record, refusing to execute", zap.Error(err))
		return fmt.Errorf("C2 HITL approval record creation failed; refusing to execute for safety: %w", err)
	}

	b.logger.Info("C2 HITL: waiting for human approval",
		zap.String("interrupt_id", interruptID),
		zap.String("task_id", req.TaskID),
		zap.String("task_type", req.TaskType),
	)

	// Poll DB waiting for decision
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var deadline <-chan time.Time
	if b.timeout > 0 {
		timer := time.NewTimer(b.timeout)
		defer timer.Stop()
		deadline = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			_, _ = b.db.Exec(`UPDATE hitl_interrupts SET status='cancelled', decision='reject',
				decision_comment='context cancelled', decided_at=? WHERE id=? AND status='pending'`,
				time.Now(), interruptID)
			return ctx.Err()

		case <-deadline:
			_, _ = b.db.Exec(`UPDATE hitl_interrupts SET status='timeout', decision='reject',
				decision_comment='C2 HITL timeout auto-reject for safety', decided_at=? WHERE id=? AND status='pending'`,
				time.Now(), interruptID)
			b.logger.Warn("C2 HITL: approval timed out, refusing to execute for safety", zap.String("interrupt_id", interruptID))
			return fmt.Errorf("C2 HITL approval timed out; the dangerous task has been automatically rejected")

		case <-ticker.C:
			var status, decision string
			err := b.db.QueryRow(`SELECT status, COALESCE(decision, '') FROM hitl_interrupts WHERE id = ?`,
				interruptID).Scan(&status, &decision)
			if err != nil {
				if err == sql.ErrNoRows {
					return nil
				}
				continue
			}
			switch status {
			case "decided", "timeout":
				if decision == "reject" {
					return fmt.Errorf("C2 dangerous task was manually rejected")
				}
				return nil
			case "cancelled":
				return fmt.Errorf("C2 approval was canceled")
			case "pending":
				continue
			default:
				continue
			}
		}
	}
}

// C2HooksConfig configures the Hooks of the C2 Manager
type C2HooksConfig struct {
	DB                *database.DB
	Logger            *zap.Logger
	AttackChainRecord func(session *database.C2Session, phase string, description string)
	VulnRecord        func(session *database.C2Session, title string, severity string)
}

// SetupC2Hooks sets up the business hooks of the C2 Manager
func SetupC2Hooks(cfg *C2HooksConfig) c2.Hooks {
	return c2.Hooks{
		OnSessionFirstSeen: func(session *database.C2Session) {
			// New session comes online
			cfg.Logger.Info("C2 Session first seen",
				zap.String("session_id", session.ID),
				zap.String("hostname", session.Hostname),
				zap.String("os", session.OS),
				zap.String("arch", session.Arch),
			)

			// Record the vulnerability (initial access point)
			if cfg.VulnRecord != nil {
				cfg.VulnRecord(session, fmt.Sprintf("C2 Session Established: %s@%s", session.Username, session.Hostname), "high")
			}

			// Record the attack chain (Initial Access)
			if cfg.AttackChainRecord != nil {
				cfg.AttackChainRecord(session, "initial-access", fmt.Sprintf("Implant beacon from %s/%s", session.Hostname, session.InternalIP))
			}
		},
		OnTaskCompleted: func(task *database.C2Task, sessionID string) {
			// Task complete
			cfg.Logger.Debug("C2 Task completed",
				zap.String("task_id", task.ID),
				zap.String("task_type", task.TaskType),
				zap.String("status", task.Status),
			)

			// Record the attack chain based on the task type
			if cfg.AttackChainRecord != nil {
				session, _ := cfg.DB.GetC2Session(sessionID)
				if session != nil {
					phase := taskToAttackPhase(task.TaskType)
					if phase != "" {
						cfg.AttackChainRecord(session, phase, fmt.Sprintf("Task %s: %s", task.TaskType, task.Status))
					}
				}
			}
		},
	}
}

// taskToAttackPhase maps the task type to an ATT&CK phase
func taskToAttackPhase(taskType string) string {
	switch taskType {
	case "exec", "shell":
		return "execution"
	case "upload":
		return "persistence"
	case "download":
		return "exfiltration"
	case "screenshot":
		return "collection"
	case "kill_proc":
		return "impact"
	case "port_fwd", "socks_start":
		return "lateral-movement"
	case "load_assembly":
		return "defense-evasion"
	case "persist":
		return "persistence"
	case "self_delete":
		return "defense-evasion"
	default:
		return "execution"
	}
}

// SetupC2HITLBridgeWithAgent sets up the HITL bridge
// This function is called by App to inject the necessary dependencies
func SetupC2HITLBridgeWithAgent(db *database.DB, logger *zap.Logger) c2.HITLBridge {
	return &C2HITLBridge{
		db:        db,
		logger:    logger,
		timeout:   5 * time.Minute,
		getConvID: func() string { return "" },
	}
}
