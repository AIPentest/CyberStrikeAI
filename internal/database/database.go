package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

const (
	// SQLite 在 WAL 模式下建议使用较保守的连接数，降低长读快照导致 checkpoint 饥饿的概率。
	sqliteMaxOpenConns = 25
	sqliteMaxIdleConns = 5
	// 以页为单位的自动 checkpoint 触发阈值（默认 1000 页，约 4MB @ 4KB/page）。
	sqliteWALAutoCheckpointPages = 1000
	// 控制 WAL 目标上限，避免异常场景持续膨胀（256MB）。
	sqliteJournalSizeLimitBytes = 256 * 1024 * 1024
	// 定时执行 PASSIVE checkpoint，平滑推进 WAL 回收。
	sqlitePassiveCheckpointInterval = 300 * time.Second
)

// configureDBPool 设置 SQLite 连接池参数，提升并发稳定性
func configureDBPool(db *sql.DB) {
	// SQLite 同一时间只允许一个写入者；过高连接数会放大锁竞争和 WAL 回收延迟。
	db.SetMaxOpenConns(sqliteMaxOpenConns)
	db.SetMaxIdleConns(sqliteMaxIdleConns)
	db.SetConnMaxLifetime(30 * time.Minute)
}

// configureSQLitePragmas 调整 WAL 回收行为，降低 -wal 文件长期膨胀风险。
func configureSQLitePragmas(db *sql.DB) error {
	if _, err := db.Exec(fmt.Sprintf("PRAGMA wal_autocheckpoint=%d", sqliteWALAutoCheckpointPages)); err != nil {
		return fmt.Errorf("设置 wal_autocheckpoint 失败: %w", err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA journal_size_limit=%d", sqliteJournalSizeLimitBytes)); err != nil {
		return fmt.Errorf("设置 journal_size_limit 失败: %w", err)
	}
	return nil
}

// DB 数据库连接
type DB struct {
	*sql.DB
	logger                   *zap.Logger
	conversationArtifactsDir string
	einoPlantaskBaseDir      string // skills_dir + plantask_rel_dir (per-conversation subdirs)
	einoCheckpointBaseDir    string // checkpoint_dir root (per-conversation subdirs)
	einoReductionRootDir     string // reduction_root_dir or default tmp/reduction (conversations/<id> subdirs)
	einoWorkspaceRootDir     string // workspace_root_dir or default tmp/workspace (projects|conversations/<id> subdirs)
	chatUploadsDir           string // chat_uploads root (<date>/<conversationID> subdirs)
	checkpointLoopName       string
	checkpointStop           chan struct{}
	checkpointDone           chan struct{}
	closeOnce                sync.Once
	closeErr                 error
	vulnerabilityCreatedHook func(*Vulnerability)
}

// startPassiveCheckpointLoop 启动后台 PASSIVE checkpoint 循环。
func (db *DB) startPassiveCheckpointLoop(name string) {
	if sqlitePassiveCheckpointInterval <= 0 || db == nil || db.DB == nil {
		return
	}
	db.checkpointLoopName = strings.TrimSpace(name)
	db.checkpointStop = make(chan struct{})
	db.checkpointDone = make(chan struct{})

	go func() {
		defer close(db.checkpointDone)
		ticker := time.NewTicker(sqlitePassiveCheckpointInterval)
		defer ticker.Stop()

		// 启动后先尝试一次，尽快回收已有 WAL 堆积。
		db.runPassiveCheckpoint("startup")
		for {
			select {
			case <-db.checkpointStop:
				return
			case <-ticker.C:
				db.runPassiveCheckpoint("ticker")
			}
		}
	}()
}

// runPassiveCheckpoint 执行一次 PRAGMA wal_checkpoint(PASSIVE)。
func (db *DB) runPassiveCheckpoint(trigger string) {
	if db == nil || db.DB == nil {
		return
	}
	startAt := time.Now()
	var busy, logFrames, checkpointed int
	err := db.QueryRow("PRAGMA wal_checkpoint(PASSIVE)").Scan(&busy, &logFrames, &checkpointed)
	if db.logger == nil {
		return
	}
	fields := []zap.Field{
		zap.String("db", db.checkpointLoopName),
		zap.String("trigger", trigger),
		zap.Int("busy", busy),
		zap.Int("log_frames", logFrames),
		zap.Int("checkpointed_frames", checkpointed),
		zap.Int64("elapsed_ms", time.Since(startAt).Milliseconds()),
	}
	if err != nil {
		db.logger.Warn("SQLite PASSIVE checkpoint 完成（失败）",
			append(fields, zap.Error(err))...,
		)
		return
	}
	if busy > 0 {
		db.logger.Debug("SQLite PASSIVE checkpoint 完成（部分推进）", fields...)
		return
	}
	db.logger.Debug("SQLite PASSIVE checkpoint 完成（成功）", fields...)
}

// NewDB 创建数据库连接
func NewDB(dbPath string, logger *zap.Logger) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=1&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	configureDBPool(db)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	if err := configureSQLitePragmas(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("配置数据库 PRAGMA 失败: %w", err)
	}

	database := &DB{
		DB:     db,
		logger: logger,
	}
	// Keep conversation-scoped artifacts near database files, so cleanup can follow conversation lifecycle.
	baseDir := filepath.Join(filepath.Dir(dbPath), "conversation_artifacts")
	if mkErr := os.MkdirAll(baseDir, 0o755); mkErr == nil {
		database.conversationArtifactsDir = baseDir
	} else if logger != nil {
		logger.Warn("创建 conversation artifacts 目录失败", zap.String("dir", baseDir), zap.Error(mkErr))
	}

	// 初始化表
	if err := database.initTables(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化表失败: %w", err)
	}
	if err := database.migrateLegacyToolGuardBlocks(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("迁移历史安全拦截记录失败: %w", err)
	}
	database.startPassiveCheckpointLoop("conversations")

	return database, nil
}

// SetEinoConversationDirs configures best-effort filesystem cleanup on DeleteConversation.
// plantaskBase is skills_root/plantask_rel (no conversation id); checkpointBase is checkpoint_dir root.
// reductionRoot is reduction_root_dir from config; empty uses tmp/reduction (conversation-scoped subdirs only).
// workspaceRoot is agent.workspace_root_dir from config; empty uses tmp/workspace.
func (db *DB) SetEinoConversationDirs(plantaskBase, checkpointBase, reductionRoot, workspaceRoot string) {
	if db == nil {
		return
	}
	db.einoPlantaskBaseDir = strings.TrimSpace(plantaskBase)
	db.einoCheckpointBaseDir = strings.TrimSpace(checkpointBase)
	db.einoReductionRootDir = strings.TrimSpace(reductionRoot)
	db.einoWorkspaceRootDir = strings.TrimSpace(workspaceRoot)
}

// SetChatUploadsDir configures the chat_uploads root so DeleteConversation can remove
// uploaded attachment files. Their chat_upload_artifacts rows already disappear via
// ON DELETE CASCADE; without this the files themselves would linger forever.
func (db *DB) SetChatUploadsDir(dir string) {
	if db == nil {
		return
	}
	db.chatUploadsDir = strings.TrimSpace(dir)
}

// initTables 初始化数据库表
func (db *DB) initTables() error {
	if err := db.reconcileSchema(applicationSchema); err != nil {
		return err
	}
	// Data migrations are separate from schema repair.
	if _, err := db.Exec("UPDATE messages SET updated_at = created_at WHERE updated_at IS NULL OR updated_at = ''"); err != nil {
		return fmt.Errorf("回填消息更新时间失败: %w", err)
	}
	if err := db.migrateVulnerabilitiesConversationFK(); err != nil {
		return fmt.Errorf("迁移漏洞会话外键失败: %w", err)
	}
	if err := db.BackfillModelTokenUsageFromProcessDetails(); err != nil {
		return fmt.Errorf("回填模型Token用量失败: %w", err)
	}
	db.logger.Debug("数据库结构检查及补齐完成")
	return nil
}

// NewKnowledgeDB 创建知识库数据库连接（只包含知识库相关的表）
func NewKnowledgeDB(dbPath string, logger *zap.Logger) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=1&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("打开知识库数据库失败: %w", err)
	}

	configureDBPool(sqlDB)

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("连接知识库数据库失败: %w", err)
	}
	if err := configureSQLitePragmas(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("配置知识库数据库 PRAGMA 失败: %w", err)
	}

	database := &DB{
		DB:     sqlDB,
		logger: logger,
	}

	// 初始化知识库表
	if err := database.initKnowledgeTables(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("初始化知识库表失败: %w", err)
	}
	database.startPassiveCheckpointLoop("knowledge")

	return database, nil
}

// initKnowledgeTables 初始化知识库数据库表（只包含知识库相关的表）
func (db *DB) initKnowledgeTables() error {
	if err := db.reconcileSchema(knowledgeSchema); err != nil {
		return err
	}
	db.logger.Info("数据库结构检查及补齐完成")
	return nil
}

// Close 关闭数据库连接
func (db *DB) Close() error {
	if db == nil {
		return nil
	}
	db.closeOnce.Do(func() {
		if db.checkpointStop != nil {
			close(db.checkpointStop)
			if db.checkpointDone != nil {
				<-db.checkpointDone
			}
		}
		if db.DB != nil {
			db.closeErr = db.DB.Close()
		}
	})
	return db.closeErr
}
