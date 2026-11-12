package database

import (
	"database/sql"
	"errors"
	"strings"
)

// validateBuiltinAdminUpdate runs before any account writes in the same
// transaction. The built-in administrator is the permanent management fallback.
func validateBuiltinAdminUpdate(tx *sql.Tx, userID string, enabled *bool, roleIDs *[]string) error {
	var builtin bool
	if err := tx.QueryRow(`SELECT is_builtin FROM rbac_users WHERE id = ?`, userID).Scan(&builtin); err != nil {
		return err
	}
	if !builtin && userID != "admin" {
		return nil
	}
	if enabled != nil && !*enabled {
		return errors.New("内置管理员不能停用")
	}
	if roleIDs != nil {
		for _, roleID := range *roleIDs {
			if strings.TrimSpace(roleID) == RBACSystemRoleAdmin {
				return nil
			}
		}
		return errors.New("内置管理员必须保留管理员角色")
	}
	return nil
}
