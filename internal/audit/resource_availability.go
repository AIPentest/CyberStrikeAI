package audit

import (
	"strings"

	"cyberstrike-ai/internal/database"
)

var auditActionsResourceRemoved = map[string]bool{
	"delete":                  true,
	"item_delete":             true,
	"connection_delete":       true,
	"listener_delete":         true,
	"session_delete":          true,
	"task_delete":             true,
	"execution_delete":        true,
	"execution_delete_batch":  true,
	"delete_queue":            true,
	"delete_batch_task":       true,
	"markdown_delete":         true,
}

// ApplyResourceAvailability sets log.ResourceAvailable when the linked resource can be checked.
func ApplyResourceAvailability(db *database.DB, log *database.AuditLog) {
	if log == nil || strings.TrimSpace(log.ResourceID) == "" {
		return
	}
	if auditActionsResourceRemoved[log.Action] {
		f := false
		log.ResourceAvailable = &f
		return
	}
	if db == nil {
		return
	}
	available, known := resourceStillExists(db, log.ResourceType, log.ResourceID)
	if known {
		log.ResourceAvailable = &available
	}
}

func resourceStillExists(db *database.DB, resourceType, resourceID string) (bool, bool) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return false, false
	}
	t := strings.TrimSpace(resourceType)
	if t == "" {
		if len(resourceID) > 8 && !strings.HasPrefix(resourceID, "c2_") {
			t = "conversation"
		} else {
			return false, false
		}
	}
	switch t {
	case "conversation":
		ok, err := db.ConversationExists(resourceID)
		return ok, err == nil
	case "vulnerability":
		_, err := db.GetVulnerability(resourceID)
		if err != nil {
			return false, strings.Contains(err.Error(), "not found")
		}
		return true, true
	case "batch_queue":
		_, err := db.GetBatchQueue(resourceID)
		return err == nil, true
	case "c2_listener":
		_, err := db.GetC2Listener(resourceID)
		return err == nil, true
	case "c2_session":
		_, err := db.GetC2Session(resourceID)
		return err == nil, true
	case "c2_task":
		_, err := db.GetC2Task(resourceID)
		return err == nil, true
	case "webshell_connection":
		c, err := db.GetWebshellConnection(resourceID)
		return err == nil && c != nil, true
	case "tool_execution":
		_, err := db.GetToolExecution(resourceID)
		return err == nil, true
	default:
		return false, false
	}
}
