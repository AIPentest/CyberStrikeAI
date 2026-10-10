package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func rawSchemaDB(t *testing.T) *DB {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:?_foreign_keys=1")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { conn.Close() })
	return &DB{DB: conn, logger: zap.NewNop()}
}
func schemaExec(t *testing.T, db *DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileSchemaUsesDefinitionsWithoutMigrationList(t *testing.T) {
	db := rawSchemaDB(t)
	schemaExec(t, db, `CREATE TABLE "future's (table)" (id INTEGER PRIMARY KEY, payload TEXT, extra TEXT);
 INSERT INTO "future's (table)" VALUES (7, 'old', 'keep');
 CREATE TABLE unrelated (value TEXT); INSERT INTO unrelated VALUES ('untouched');`)
	latest := `CREATE TABLE "future's (table)" (
 id INTEGER PRIMARY KEY, payload TEXT,
 /* punctuation , ( ) */ new_value TEXT NOT NULL DEFAULT 'a,b''c',
 quota DECIMAL(10,2) DEFAULT 1.25 CHECK (quota IN (1.25, 2)),
 "UNIQUE" TEXT DEFAULT 'quoted',
 derived TEXT GENERATED ALWAYS AS (payload || ',x') VIRTUAL
 );
 CREATE TABLE missing_table (id INTEGER PRIMARY KEY, source INTEGER REFERENCES "future's (table)"(id));
 CREATE INDEX new_index ON "future's (table)"(new_value);`
	for i := 0; i < 2; i++ {
		if err := db.reconcileSchema(latest); err != nil {
			t.Fatal(err)
		}
	}
	var payload, extra, value, derived, quoted string
	var quota float64
	if err := db.QueryRow(`SELECT payload,extra,new_value,quota,derived,"UNIQUE" FROM "future's (table)" WHERE id=7`).Scan(&payload, &extra, &value, &quota, &derived, &quoted); err != nil {
		t.Fatal(err)
	}
	if payload != "old" || extra != "keep" || value != "a,b'c" || quota != 1.25 || derived != "old,x" || quoted != "quoted" {
		t.Fatalf("data/defaults: %q %q %q %v %q %q", payload, extra, value, quota, derived, quoted)
	}
	if err := db.QueryRow(`SELECT value FROM unrelated`).Scan(&value); err != nil || value != "untouched" {
		t.Fatalf("unrelated data: %q %v", value, err)
	}
	schemaExec(t, db, `INSERT INTO missing_table VALUES(1,7)`)
	if _, err := db.Exec(`INSERT INTO missing_table VALUES(2,999)`); err == nil {
		t.Fatal("foreign key not enforced")
	}
}

func TestReconcileSchemaFailureRollsBackAllRepairs(t *testing.T) {
	for _, addition := range []string{"required TEXT NOT NULL", "unique_value TEXT UNIQUE", "stamp TEXT DEFAULT CURRENT_TIMESTAMP", "stored TEXT GENERATED ALWAYS AS (payload) STORED"} {
		t.Run(addition, func(t *testing.T) {
			db := rawSchemaDB(t)
			schemaExec(t, db, `CREATE TABLE z_existing (id INTEGER PRIMARY KEY,payload TEXT); INSERT INTO z_existing VALUES(1,'keep')`)
			err := db.reconcileSchema(`CREATE TABLE a_missing (id INTEGER); CREATE TABLE z_existing (id INTEGER PRIMARY KEY,payload TEXT, safe TEXT,` + addition + `);`)
			if err == nil || !strings.Contains(err.Error(), "z_existing.") {
				t.Fatalf("missing contextual failure: %v", err)
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='a_missing'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("table repair not rolled back: %d %v", count, err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('z_existing') WHERE name='safe'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("column repair not rolled back: %d %v", count, err)
			}
			var value string
			if err := db.QueryRow(`SELECT payload FROM z_existing WHERE id=1`).Scan(&value); err != nil || value != "keep" {
				t.Fatalf("original data: %q %v", value, err)
			}
		})
	}
}

func TestReconcileSchemaIndexFailureRollsBack(t *testing.T) {
	db := rawSchemaDB(t)
	schemaExec(t, db, `CREATE TABLE existing (value TEXT); INSERT INTO existing VALUES ('duplicate'),('duplicate')`)
	err := db.reconcileSchema(`CREATE TABLE existing (value TEXT,added TEXT); CREATE UNIQUE INDEX unique_values ON existing(value)`)
	if err == nil || !strings.Contains(err.Error(), "unique_values") {
		t.Fatalf("index failure: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('existing') WHERE name='added'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("repair rollback: %d %v", count, err)
	}
}

func TestStartupRepairsTablesAndUnlistedColumnsPreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.db")
	db, err := NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	conv, err := db.CreateConversation("retained", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	schemaExec(t, db, `ALTER TABLE conversations ADD COLUMN extension TEXT DEFAULT 'keep'; CREATE INDEX extension_index ON conversations(extension);
 CREATE TABLE unrelated(value TEXT); INSERT INTO unrelated VALUES ('untouched');
 DROP TABLE c2_payload_artifacts; DROP TABLE rbac_roles; DROP TABLE notification_reads_by_user;
 ALTER TABLE hitl_interrupts DROP COLUMN payload;
 ALTER TABLE hitl_conversation_configs DROP COLUMN reviewer;
 ALTER TABLE model_token_usage DROP COLUMN cached_tokens;
 ALTER TABLE rbac_users DROP COLUMN display_name;
 ALTER TABLE messages DROP COLUMN reasoning_content;`)
	db.Close()
	for i := 0; i < 2; i++ {
		db, err = NewDB(path, zap.NewNop())
		if err != nil {
			t.Fatal(err)
		}
		reference, err := openSchemaReference(applicationSchema)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := schemaObjects(reference, "table")
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range tables {
			expected, err := schemaColumns(reference, table.name)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := schemaColumns(db, table.name)
			if err != nil {
				t.Fatal(err)
			}
			present := map[string]bool{}
			for _, c := range actual {
				present[c.name] = true
			}
			for _, c := range expected {
				if !present[c.name] {
					t.Errorf("missing %s.%s", table.name, c.name)
				}
			}
		}
		reference.Close()
		var title, extra string
		if err := db.QueryRow(`SELECT title,extension FROM conversations WHERE id=?`, conv.ID).Scan(&title, &extra); err != nil || title != "retained" || extra != "keep" {
			t.Fatalf("data: %q %q %v", title, extra, err)
		}
		var value string
		if err := db.QueryRow(`SELECT value FROM unrelated`).Scan(&value); err != nil || value != "untouched" {
			t.Fatalf("extra table: %q %v", value, err)
		}
		db.Close()
	}
}

func TestOtherSQLiteFileStartupRepairsSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.db")
	db, err := NewKnowledgeDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	schemaExec(t, db, `INSERT INTO knowledge_base_items (id,category,title,file_path,created_at,updated_at) VALUES('keep','cat','title','path','2026-10-10','2026-10-10');
 ALTER TABLE knowledge_base_items DROP COLUMN content;
 ALTER TABLE knowledge_embeddings DROP COLUMN embedding_model;
 DROP TABLE knowledge_retrieval_logs;`)
	db.Close()
	for i := 0; i < 2; i++ {
		db, err = NewKnowledgeDB(path, zap.NewNop())
		if err != nil {
			t.Fatal(err)
		}
		var title string
		if err := db.QueryRow(`SELECT title FROM knowledge_base_items WHERE id='keep'`).Scan(&title); err != nil || title != "title" {
			t.Fatalf("data: %q %v", title, err)
		}
		schemaExec(t, db, `SELECT content FROM knowledge_base_items; SELECT embedding_model FROM knowledge_embeddings; SELECT * FROM knowledge_retrieval_logs`)
		db.Close()
	}
}

func TestHistoricalFKMigrationPreservesExtraAndDependentData(t *testing.T) {
	db := rawSchemaDB(t)
	legacy := strings.Replace(applicationSchema, `FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE SET NULL`, `FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE`, 1)
	schemaExec(t, db, legacy)
	conv, err := db.CreateConversation("source", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	vuln, err := db.CreateVulnerability(&Vulnerability{ConversationID: conv.ID, Title: "keep", Severity: "high", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	schemaExec(t, db, `ALTER TABLE vulnerabilities ADD COLUMN extension TEXT DEFAULT 'keep'; CREATE INDEX vuln_extra_index ON vulnerabilities(extension);
 CREATE TABLE dependent(id TEXT REFERENCES vulnerabilities(id) ON DELETE CASCADE);
 CREATE TABLE trigger_log(value TEXT);
 CREATE TRIGGER vuln_custom AFTER UPDATE ON vulnerabilities BEGIN INSERT INTO trigger_log VALUES (NEW.extension); END;`)
	if _, err := db.Exec(`INSERT INTO dependent VALUES(?)`, vuln.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.initTables(); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := db.QueryRow(`SELECT extension FROM vulnerabilities WHERE id=?`, vuln.ID).Scan(&value); err != nil || value != "keep" {
		t.Fatalf("extra data: %q %v", value, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM dependent`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("dependent rows: %d %v", count, err)
	}
	if _, err := db.Exec(`UPDATE vulnerabilities SET extension='triggered' WHERE id=?`, vuln.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT value FROM trigger_log`).Scan(&value); err != nil || value != "triggered" {
		t.Fatalf("trigger: %q %v", value, err)
	}
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("foreign keys: %d %v", count, err)
	}
	if err := db.DeleteConversation(conv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetVulnerability(vuln.ID); err != nil {
		t.Fatal(err)
	}
}
