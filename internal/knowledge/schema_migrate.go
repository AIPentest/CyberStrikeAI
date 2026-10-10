package knowledge

import (
	"cyberstrike-ai/internal/database"
	"database/sql"
)

// EnsureKnowledgeEmbeddingsSchema checks against the canonical SQLite schema;
// callers no longer maintain an independent list of embedding columns.
func EnsureKnowledgeEmbeddingsSchema(db *sql.DB) error {
	return database.EnsureKnowledgeSchema(db)
}

// ensureKnowledgeEmbeddingsSubIndexesColumn is retained for compatibility.
func ensureKnowledgeEmbeddingsSubIndexesColumn(db *sql.DB) error {
	return EnsureKnowledgeEmbeddingsSchema(db)
}
