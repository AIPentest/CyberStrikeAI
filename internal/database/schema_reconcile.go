package database

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

// These definitions are the single source of truth for startup schema repair.
//
//go:embed schemas/application.sql
var applicationSchema string

//go:embed schemas/knowledge.sql
var knowledgeSchema string

type schemaObject struct{ name, ddl string }
type schemaColumn struct {
	name   string
	hidden int
}
type schemaQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
}

func schemaColumns(q schemaQuerier, table string) ([]schemaColumn, error) {
	rows, err := q.Query(`SELECT name, hidden FROM pragma_table_xinfo(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []schemaColumn
	for rows.Next() {
		var c schemaColumn
		if err := rows.Scan(&c.name, &c.hidden); err != nil {
			return nil, err
		}
		columns = append(columns, c)
	}
	return columns, rows.Err()
}

func schemaObjects(q schemaQuerier, kind string) ([]schemaObject, error) {
	rows, err := q.Query(`SELECT name, sql FROM sqlite_master WHERE type=? AND sql IS NOT NULL AND substr(name,1,7) != 'sqlite_' ORDER BY name`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var objects []schemaObject
	for rows.Next() {
		var o schemaObject
		if err := rows.Scan(&o.name, &o.ddl); err != nil {
			return nil, err
		}
		objects = append(objects, o)
	}
	return objects, rows.Err()
}

func quoteSchemaName(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// reconcileSchema only adds structure. SQLite validates the exact column
// definition; unsafe additions fail instead of weakening constraints or
// inventing values for existing records. The entire repair is transactional.
func (db *DB) reconcileSchema(schema string) error {
	reference, err := openSchemaReference(schema)
	if err != nil {
		return err
	}
	defer reference.Close()
	tables, err := schemaObjects(reference, "table")
	if err != nil {
		return err
	}
	indexes, err := schemaObjects(reference, "index")
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Create every missing table first, so references are available before columns.
	for _, table := range tables {
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=? COLLATE NOCASE`, table.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := tx.Exec(table.ddl); err != nil {
				return fmt.Errorf("补齐表 %s 失败: %w", table.name, err)
			}
		}
	}
	for _, table := range tables {
		expected, err := schemaColumns(reference, table.name)
		if err != nil {
			return err
		}
		actual, err := schemaColumns(tx, table.name)
		if err != nil {
			return err
		}
		present := make(map[string]bool, len(actual))
		for _, c := range actual {
			present[strings.ToLower(c.name)] = true
		}
		definitions, err := columnDefinitions(table.ddl)
		if err != nil {
			return fmt.Errorf("解析表 %s 字段失败: %w", table.name, err)
		}
		for _, c := range expected {
			key := strings.ToLower(c.name)
			if present[key] {
				continue
			}
			definition, ok := definitions[key]
			if !ok {
				return fmt.Errorf("表 %s 字段 %s 缺少定义", table.name, c.name)
			}
			if _, err := tx.Exec("ALTER TABLE " + quoteSchemaName(table.name) + " ADD COLUMN " + definition); err != nil {
				return fmt.Errorf("补齐字段 %s.%s 失败（保留原结构和数据，请检查字段约束及默认值）: %w", table.name, c.name, err)
			}
		}
	}
	for _, index := range indexes {
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name=? COLLATE NOCASE`, index.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := tx.Exec(index.ddl); err != nil {
				return fmt.Errorf("补齐索引 %s 失败: %w", index.name, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 SQLite 结构补齐失败: %w", err)
	}
	return nil
}

type schemaToken struct {
	text       string
	start, end int
	quoted     bool
}

// tokenizeSchema keeps offsets for exact SQL slices, ignoring comments and
// respecting escaped quotes. SQLite itself validates the SQL before this runs.
func tokenizeSchema(s string) ([]schemaToken, error) {
	var tokens []schemaToken
	for i := 0; i < len(s); {
		if strings.ContainsRune(" \t\r\n", rune(s[i])) {
			i++
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "--" {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "/*" {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("未闭合 SQL 注释")
			}
			i += end + 4
			continue
		}
		start := i
		quoted := false
		if strings.ContainsRune("'\"`[", rune(s[i])) {
			quoted = true
			close := s[i]
			if close == '[' {
				close = ']'
			}
			i++
			closed := false
			for i < len(s) {
				if s[i] == close {
					i++
					if close != ']' && i < len(s) && s[i] == close {
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("未闭合 SQL 引号")
			}
		} else if strings.ContainsRune("(),;", rune(s[i])) {
			i++
		} else {
			for i < len(s) && !strings.ContainsRune(" \t\r\n(),;'\"`[", rune(s[i])) {
				if i+1 < len(s) && (s[i:i+2] == "--" || s[i:i+2] == "/*") {
					break
				}
				i++
			}
		}
		tokens = append(tokens, schemaToken{s[start:i], start, i, quoted})
	}
	return tokens, nil
}

func columnDefinitions(ddl string) (map[string]string, error) {
	tokens, err := tokenizeSchema(ddl)
	if err != nil {
		return nil, err
	}
	definitions := make(map[string]string)
	depth, start := 0, -1
	add := func(end int) {
		if start < 0 || start >= end {
			return
		}
		token := tokens[start]
		if !token.quoted {
			switch strings.ToUpper(token.text) {
			case "CONSTRAINT", "PRIMARY", "UNIQUE", "CHECK", "FOREIGN":
				return
			}
		}
		name := token.text
		if token.quoted {
			close := name[len(name)-1:]
			name = name[1 : len(name)-1]
			if close != "]" {
				name = strings.ReplaceAll(name, close+close, close)
			}
		}
		definitions[strings.ToLower(name)] = strings.TrimSpace(ddl[token.start:tokens[end].start])
	}
	for i, t := range tokens {
		if t.quoted {
			continue
		}
		switch t.text {
		case "(":
			depth++
			if depth == 1 {
				start = i + 1
			}
		case ")":
			if depth == 1 {
				add(i)
				return definitions, nil
			}
			depth--
		case ",":
			if depth == 1 {
				add(i)
				start = i + 1
			}
		}
	}
	return nil, fmt.Errorf("缺少 CREATE TABLE 字段定义")
}

func openSchemaReference(schema string) (*sql.DB, error) {
	reference, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, err
	}
	reference.SetMaxOpenConns(1)
	if _, err := reference.Exec(schema); err != nil {
		reference.Close()
		return nil, fmt.Errorf("解析最新 SQLite 结构失败: %w", err)
	}
	return reference, nil
}

// EnsureKnowledgeSchema supports callers holding a plain SQL connection.
// The same definitions and repair engine are used by NewKnowledgeDB.
func EnsureKnowledgeSchema(conn *sql.DB) error {
	if conn == nil {
		return fmt.Errorf("db is nil")
	}
	return (&DB{DB: conn}).reconcileSchema(knowledgeSchema)
}
