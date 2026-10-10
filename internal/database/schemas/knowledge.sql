-- Latest SQLite structure. Update these definitions when adding tables or columns.

CREATE TABLE knowledge_base_items (
    id TEXT PRIMARY KEY,
    category TEXT NOT NULL,
    title TEXT NOT NULL,
    file_path TEXT NOT NULL,
    content TEXT,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE knowledge_embeddings (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL,
    chunk_index INTEGER NOT NULL,
    chunk_text TEXT NOT NULL,
    embedding TEXT NOT NULL,
    sub_indexes TEXT NOT NULL DEFAULT '',
    embedding_model TEXT NOT NULL DEFAULT '',
    embedding_dim INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    FOREIGN KEY (item_id) REFERENCES knowledge_base_items(id) ON DELETE CASCADE
);

CREATE TABLE knowledge_retrieval_logs (
    id TEXT PRIMARY KEY,
    conversation_id TEXT,
    message_id TEXT,
    query TEXT NOT NULL,
    risk_type TEXT,
    retrieved_items TEXT,
    created_at DATETIME NOT NULL
);

CREATE INDEX idx_knowledge_embeddings_item_id ON knowledge_embeddings(item_id);

CREATE INDEX idx_knowledge_items_category ON knowledge_base_items(category);

CREATE INDEX idx_knowledge_retrieval_logs_conversation ON knowledge_retrieval_logs(conversation_id);

CREATE INDEX idx_knowledge_retrieval_logs_created_at ON knowledge_retrieval_logs(created_at);

CREATE INDEX idx_knowledge_retrieval_logs_message ON knowledge_retrieval_logs(message_id);
