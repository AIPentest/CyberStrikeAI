package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// This is a historical foreign-key semantic migration, not a column repair
// list. Rebuild from the latest definition while retaining extra columns,
// indexes, triggers and dependent rows in old installations.
func (db *DB) migrateVulnerabilitiesConversationFK() error {
	ok, err := vulnerabilitiesConversationFKOnDeleteSetNull(db.DB)
	if err != nil || ok {
		return err
	}
	reference, err := openSchemaReference(applicationSchema)
	if err != nil {
		return err
	}
	defer reference.Close()
	var latest string
	if err := reference.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='vulnerabilities'`).Scan(&latest); err != nil {
		return err
	}
	createNew := "CREATE TABLE vulnerabilities_new " + latest[strings.Index(latest, "("):]

	// Changing foreign_keys inside a transaction is ineffective. Pin one
	// connection and disable it before the transaction to avoid cascading drops.
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(createNew); err != nil {
		return err
	}
	var original string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='vulnerabilities'`).Scan(&original); err != nil {
		return err
	}
	definitions, err := columnDefinitions(original)
	if err != nil {
		return err
	}
	oldColumns, err := schemaColumns(tx, "vulnerabilities")
	if err != nil {
		return err
	}
	newColumns, err := schemaColumns(tx, "vulnerabilities_new")
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, c := range newColumns {
		present[strings.ToLower(c.name)] = true
	}
	var names, values []string
	for _, c := range oldColumns {
		key := strings.ToLower(c.name)
		if !present[key] {
			if _, err := tx.Exec("ALTER TABLE vulnerabilities_new ADD COLUMN " + definitions[key]); err != nil {
				return fmt.Errorf("保留漏洞额外字段 %s 失败: %w", c.name, err)
			}
		}
		if c.hidden != 0 {
			continue
		}
		quoted := quoteSchemaName(c.name)
		names = append(names, quoted)
		if key == "retest_log" {
			values = append(values, "COALESCE("+quoted+", '')")
		} else {
			values = append(values, quoted)
		}
	}
	rows, err := tx.Query(`SELECT sql FROM sqlite_master WHERE tbl_name='vulnerabilities' AND type IN ('index','trigger') AND sql IS NOT NULL ORDER BY type, name`)
	if err != nil {
		return err
	}
	var objects []string
	for rows.Next() {
		var ddl string
		if err := rows.Scan(&ddl); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, ddl)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	copyRows := "INSERT INTO vulnerabilities_new (" + strings.Join(names, ",") + ") SELECT " + strings.Join(values, ",") + " FROM vulnerabilities"
	if _, err := tx.Exec(copyRows); err != nil {
		return fmt.Errorf("保留漏洞数据失败: %w", err)
	}
	if _, err := tx.Exec("DROP TABLE vulnerabilities"); err != nil {
		return err
	}
	if _, err := tx.Exec("ALTER TABLE vulnerabilities_new RENAME TO vulnerabilities"); err != nil {
		return err
	}
	for _, ddl := range objects {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	rows, err = tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	violation := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if violation {
		return fmt.Errorf("漏洞外键迁移校验失败，已保留原数据")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	db.logger.Info("vulnerabilities 表已迁移：删除对话时保留漏洞记录", zap.Int("preserved_columns", len(oldColumns)))
	return nil
}

func vulnerabilitiesConversationFKOnDeleteSetNull(db *sql.DB) (bool, error) {
	rows, err := db.Query(`PRAGMA foreign_key_list(vulnerabilities)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return false, err
		}
		if from == "conversation_id" {
			found = true
			if !strings.EqualFold(onDelete, "SET NULL") {
				return false, nil
			}
		}
	}
	return found, rows.Err()
}
