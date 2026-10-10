-- Latest SQLite structure. Update these definitions when adding tables or columns.

CREATE TABLE assets (
    id TEXT PRIMARY KEY,
    dedup_key TEXT NOT NULL UNIQUE,
    project_id TEXT,
    host TEXT NOT NULL DEFAULT '',
    ip TEXT NOT NULL DEFAULT '',
    port INTEGER NOT NULL DEFAULT 0,
    domain TEXT NOT NULL DEFAULT '',
    protocol TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    server TEXT NOT NULL DEFAULT '',
    country TEXT NOT NULL DEFAULT '',
    province TEXT NOT NULL DEFAULT '',
    city TEXT NOT NULL DEFAULT '',
    responsible_person TEXT NOT NULL DEFAULT '',
    department TEXT NOT NULL DEFAULT '',
    business_system TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    criticality TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'manual',
    source_query TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    vulnerability_count INTEGER NOT NULL DEFAULT 0,
    risk_score INTEGER NOT NULL DEFAULT 0,
    risk_level TEXT NOT NULL DEFAULT 'unassessed',
    tags_json TEXT NOT NULL DEFAULT '[]',
    first_seen_at DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    owner_user_id TEXT,
    last_scan_at DATETIME,
    last_scan_conversation_id TEXT NOT NULL DEFAULT '',
    last_scan_queue_id TEXT NOT NULL DEFAULT '',
    last_scan_task_id TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL
);

CREATE TABLE attack_chain_edges (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    source_node_id TEXT NOT NULL,
    target_node_id TEXT NOT NULL,
    edge_type TEXT NOT NULL,
    weight INTEGER DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (source_node_id) REFERENCES attack_chain_nodes(id) ON DELETE CASCADE,
    FOREIGN KEY (target_node_id) REFERENCES attack_chain_nodes(id) ON DELETE CASCADE
);

CREATE TABLE attack_chain_nodes (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    node_type TEXT NOT NULL,
    node_name TEXT NOT NULL,
    tool_execution_id TEXT,
    metadata TEXT,
    risk_score INTEGER DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (tool_execution_id) REFERENCES tool_executions(id) ON DELETE SET NULL
);

CREATE TABLE audit_logs (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL,
    level TEXT NOT NULL DEFAULT 'info',
    category TEXT NOT NULL,
    action TEXT NOT NULL,
    result TEXT NOT NULL,
    actor TEXT NOT NULL DEFAULT 'admin',
    session_hint TEXT,
    client_ip TEXT,
    user_agent TEXT,
    resource_type TEXT,
    resource_id TEXT,
    message TEXT NOT NULL,
    detail_json TEXT
);

CREATE TABLE batch_task_queues (
    id TEXT PRIMARY KEY,
    title TEXT,
    role TEXT,
    agent_mode TEXT NOT NULL DEFAULT 'eino_single',
    hitl_policy TEXT NOT NULL DEFAULT '',
    schedule_mode TEXT NOT NULL DEFAULT 'manual',
    cron_expr TEXT,
    next_run_at DATETIME,
    schedule_enabled INTEGER NOT NULL DEFAULT 1,
    last_schedule_trigger_at DATETIME,
    last_schedule_error TEXT,
    last_run_error TEXT,
    project_id TEXT,
    concurrency INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    started_at DATETIME,
    completed_at DATETIME,
    current_index INTEGER NOT NULL DEFAULT 0,
    owner_user_id TEXT
);

CREATE TABLE batch_tasks (
    id TEXT PRIMARY KEY,
    queue_id TEXT NOT NULL,
    message TEXT NOT NULL,
    conversation_id TEXT,
    status TEXT NOT NULL,
    started_at DATETIME,
    completed_at DATETIME,
    error TEXT,
    result TEXT,
    FOREIGN KEY (queue_id) REFERENCES batch_task_queues(id) ON DELETE CASCADE
);

CREATE TABLE c2_events (
    id TEXT PRIMARY KEY,
    level TEXT NOT NULL DEFAULT 'info',
    category TEXT NOT NULL,
    session_id TEXT,
    task_id TEXT,
    message TEXT NOT NULL,
    data_json TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE c2_files (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    task_id TEXT,
    direction TEXT NOT NULL,
    remote_path TEXT NOT NULL,
    local_path TEXT NOT NULL,
    size_bytes INTEGER DEFAULT 0,
    sha256 TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES c2_sessions(id) ON DELETE CASCADE
);

CREATE TABLE c2_http_session_auth (
    implant_uuid TEXT PRIMARY KEY,
    listener_id TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    FOREIGN KEY (listener_id) REFERENCES c2_listeners(id) ON DELETE CASCADE
);

CREATE TABLE c2_listeners (
    id TEXT PRIMARY KEY,
    project_id TEXT,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    bind_host TEXT NOT NULL DEFAULT '127.0.0.1',
    bind_port INTEGER NOT NULL,
    profile_id TEXT,
    encryption_key TEXT NOT NULL DEFAULT '',
    implant_token TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'stopped',
    config_json TEXT NOT NULL DEFAULT '{}',
    remark TEXT NOT NULL DEFAULT '',
    owner_user_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME,
    last_error TEXT
);

CREATE TABLE c2_payload_artifacts (
    filename TEXT PRIMARY KEY,
    payload_id TEXT NOT NULL,
    listener_id TEXT NOT NULL,
    owner_user_id TEXT NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE TABLE c2_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    user_agent TEXT,
    uris_json TEXT NOT NULL DEFAULT '[]',
    request_headers_json TEXT,
    response_headers_json TEXT,
    body_template TEXT,
    jitter_min_ms INTEGER DEFAULT 0,
    jitter_max_ms INTEGER DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE c2_sessions (
    id TEXT PRIMARY KEY,
    listener_id TEXT NOT NULL,
    implant_uuid TEXT NOT NULL UNIQUE,
    hostname TEXT,
    username TEXT,
    os TEXT,
    arch TEXT,
    pid INTEGER DEFAULT 0,
    process_name TEXT,
    is_admin INTEGER DEFAULT 0,
    internal_ip TEXT,
    external_ip TEXT,
    user_agent TEXT,
    sleep_seconds INTEGER NOT NULL DEFAULT 5,
    jitter_percent INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_check_in DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    metadata_json TEXT DEFAULT '{}',
    note TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (listener_id) REFERENCES c2_listeners(id) ON DELETE CASCADE
);

CREATE TABLE c2_tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    task_type TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'queued',
    result_text TEXT,
    result_blob_path TEXT,
    error TEXT,
    source TEXT NOT NULL DEFAULT 'manual',
    conversation_id TEXT,
    approval_status TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at DATETIME,
    started_at DATETIME,
    completed_at DATETIME,
    duration_ms INTEGER DEFAULT 0,
    FOREIGN KEY (session_id) REFERENCES c2_sessions(id) ON DELETE CASCADE
);

CREATE TABLE chat_upload_artifacts (
    relative_path TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    owner_user_id TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);

CREATE TABLE conversations (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    role_name TEXT NOT NULL DEFAULT '默认',
    agent_mode TEXT NOT NULL DEFAULT 'eino_single',
    last_react_input TEXT,
    last_react_output TEXT,
    pinned INTEGER DEFAULT 0,
    webshell_connection_id TEXT,
    project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
    owner_user_id TEXT
);

CREATE TABLE hitl_conversation_configs (
    conversation_id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    mode TEXT NOT NULL DEFAULT 'off',
    sensitive_tools TEXT NOT NULL DEFAULT '[]',
    timeout_seconds INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL,
    reviewer TEXT NOT NULL DEFAULT 'human'
);

CREATE TABLE hitl_interrupts (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    message_id TEXT,
    mode TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    tool_call_id TEXT,
    payload TEXT,
    status TEXT NOT NULL,
    reviewer TEXT NOT NULL DEFAULT 'human',
    decision TEXT,
    decision_comment TEXT,
    created_at DATETIME NOT NULL,
    decided_at DATETIME,
    decided_by TEXT NOT NULL DEFAULT 'human'
);

CREATE TABLE knowledge_retrieval_logs (
    id TEXT PRIMARY KEY,
    conversation_id TEXT,
    message_id TEXT,
    query TEXT NOT NULL,
    risk_type TEXT,
    retrieved_items TEXT,
    created_at DATETIME NOT NULL,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE SET NULL,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE SET NULL
);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    mcp_execution_ids TEXT,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    reasoning_content TEXT,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);

CREATE TABLE model_token_usage (
    id TEXT PRIMARY KEY,
    process_detail_id TEXT NOT NULL UNIQUE,
    message_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    project_id TEXT,
    source TEXT NOT NULL DEFAULT '',
    orchestration TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    model_calls INTEGER NOT NULL DEFAULT 0,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    FOREIGN KEY (process_detail_id) REFERENCES process_details(id) ON DELETE CASCADE,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL
);

CREATE TABLE notification_reads_by_user (
    user_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    read_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(user_id, event_id)
);

CREATE TABLE process_details (
    id TEXT PRIMARY KEY,
    message_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    message TEXT,
    data TEXT,
    created_at DATETIME NOT NULL,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);

CREATE TABLE project_fact_edges (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    target_fact_key TEXT NOT NULL,
    edge_type TEXT NOT NULL,
    confidence TEXT NOT NULL DEFAULT 'tentative',
    source_conversation_id TEXT,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE(project_id, source_fact_key, target_fact_key, edge_type)
);

CREATE TABLE project_facts (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    fact_key TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'note',
    summary TEXT NOT NULL DEFAULT '',
    body TEXT,
    confidence TEXT NOT NULL DEFAULT 'tentative',
    source_conversation_id TEXT,
    source_message_id TEXT,
    pinned INTEGER NOT NULL DEFAULT 0,
    related_vulnerability_id TEXT,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE(project_id, fact_key)
);

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    scope_json TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    pinned INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    owner_user_id TEXT
);

CREATE TABLE rbac_permissions (
    key TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL
);

CREATE TABLE rbac_resource_assignments (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    FOREIGN KEY (user_id) REFERENCES rbac_users(id) ON DELETE CASCADE,
    UNIQUE(user_id, resource_type, resource_id)
);

CREATE TABLE rbac_role_permissions (
    role_id TEXT NOT NULL,
    permission_key TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    PRIMARY KEY (role_id, permission_key),
    FOREIGN KEY (role_id) REFERENCES rbac_roles(id) ON DELETE CASCADE,
    FOREIGN KEY (permission_key) REFERENCES rbac_permissions(key) ON DELETE CASCADE
);

CREATE TABLE rbac_roles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL DEFAULT 'assigned',
    is_system INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE rbac_user_roles (
    user_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    PRIMARY KEY (user_id, role_id),
    FOREIGN KEY (user_id) REFERENCES rbac_users(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES rbac_roles(id) ON DELETE CASCADE
);

CREATE TABLE rbac_users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    is_builtin INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE robot_binding_codes (
    code_hash TEXT PRIMARY KEY,
    rbac_user_id TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,
    created_at DATETIME NOT NULL,
    FOREIGN KEY (rbac_user_id) REFERENCES rbac_users(id) ON DELETE CASCADE
);

CREATE TABLE robot_user_bindings (
    id TEXT PRIMARY KEY,
    platform TEXT NOT NULL,
    external_user_id TEXT NOT NULL,
    rbac_user_id TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    FOREIGN KEY (rbac_user_id) REFERENCES rbac_users(id) ON DELETE CASCADE,
    UNIQUE(platform, external_user_id)
);

CREATE TABLE robot_user_sessions (
    session_key TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    role_name TEXT NOT NULL DEFAULT '默认',
    agent_mode TEXT NOT NULL DEFAULT 'eino_single',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);

CREATE TABLE skill_stats (
    skill_name TEXT PRIMARY KEY,
    total_calls INTEGER NOT NULL DEFAULT 0,
    success_calls INTEGER NOT NULL DEFAULT 0,
    failed_calls INTEGER NOT NULL DEFAULT 0,
    last_call_time DATETIME,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tool_executions (
    id TEXT PRIMARY KEY,
    tool_name TEXT NOT NULL,
    arguments TEXT NOT NULL,
    status TEXT NOT NULL,
    result TEXT,
    error TEXT,
    start_time DATETIME NOT NULL,
    end_time DATETIME,
    duration_ms INTEGER,
    partial_output TEXT,
    partial_output_bytes INTEGER NOT NULL DEFAULT 0,
    partial_output_truncated INTEGER NOT NULL DEFAULT 0,
    partial_output_updated_at DATETIME,
    owner_user_id TEXT,
    conversation_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tool_stats (
    tool_name TEXT PRIMARY KEY,
    total_calls INTEGER NOT NULL DEFAULT 0,
    success_calls INTEGER NOT NULL DEFAULT 0,
    failed_calls INTEGER NOT NULL DEFAULT 0,
    last_call_time DATETIME,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE vulnerabilities (
    id TEXT PRIMARY KEY,
    conversation_id TEXT,
    conversation_tag TEXT,
    task_tag TEXT,
    title TEXT NOT NULL,
    description TEXT,
    severity TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    vulnerability_type TEXT,
    target TEXT,
    preconditions TEXT,
    reproduction_steps TEXT,
    evidence TEXT,
    impact TEXT,
    recommendation TEXT,
    retest_notes TEXT,
    retest_log TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    project_id TEXT,
    owner_user_id TEXT,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE SET NULL
);

CREATE TABLE vulnerability_alert_deliveries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vulnerability_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    platform TEXT NOT NULL,
    external_user_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(vulnerability_id, platform, external_user_id),
    FOREIGN KEY (vulnerability_id) REFERENCES vulnerabilities(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES rbac_users(id) ON DELETE CASCADE
);

CREATE TABLE vulnerability_alert_subscriptions (
    user_id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    min_severity TEXT NOT NULL DEFAULT 'high',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES rbac_users(id) ON DELETE CASCADE
);

CREATE TABLE vulnerability_retests (
    conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    source_conversation_id TEXT NOT NULL,
    vulnerability_id TEXT NOT NULL,
    source_project_id TEXT NOT NULL DEFAULT '',
    pending_prompt TEXT NOT NULL DEFAULT ''
);

CREATE TABLE webshell_connection_states (
    connection_id TEXT PRIMARY KEY,
    state_json TEXT NOT NULL DEFAULT '{}',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (connection_id) REFERENCES webshell_connections(id) ON DELETE CASCADE
);

CREATE TABLE webshell_connections (
    id TEXT PRIMARY KEY,
    project_id TEXT,
    url TEXT NOT NULL,
    password TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL DEFAULT 'php',
    method TEXT NOT NULL DEFAULT 'post',
    cmd_param TEXT NOT NULL DEFAULT '',
    remark TEXT NOT NULL DEFAULT '',
    encoding TEXT NOT NULL DEFAULT '',
    os TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    owner_user_id TEXT
);

CREATE TABLE workflow_definitions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    graph_json TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE workflow_node_runs (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    status TEXT NOT NULL,
    input_json TEXT,
    output_json TEXT,
    error TEXT,
    started_at DATETIME NOT NULL,
    finished_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
);

CREATE TABLE workflow_package_imports (
    id TEXT PRIMARY KEY,
    inspection_id TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('create','keep_existing','overwrite','rename')),
    source_workflow_id TEXT NOT NULL,
    target_workflow_id TEXT NOT NULL,
    resulting_workflow_id TEXT,
    result TEXT NOT NULL CHECK (result IN ('created','overwritten','renamed','kept_existing','skipped_identical','failed')),
    error_code TEXT,
    error_message TEXT,
    created_at DATETIME NOT NULL,
    applied_at DATETIME,
    FOREIGN KEY (inspection_id) REFERENCES workflow_package_inspections(id)
);

CREATE TABLE workflow_package_inspections (
    id TEXT PRIMARY KEY,
    package_hash TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    workflow_payload_json TEXT NOT NULL,
    inspection_json TEXT NOT NULL,
    source_workflow_id TEXT NOT NULL,
    source_revision INTEGER NOT NULL,
    source_content_hash TEXT NOT NULL,
    source_graph_hash TEXT NOT NULL,
    local_conflict_state TEXT NOT NULL CHECK (local_conflict_state IN ('none','identical','id_conflict')),
    local_workflow_id TEXT,
    local_content_hash TEXT,
    local_graph_hash TEXT,
    created_by TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('ready','consumed','expired')),
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    consumed_at DATETIME
);

CREATE TABLE workflow_runs (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    workflow_version INTEGER NOT NULL DEFAULT 1,
    conversation_id TEXT,
    project_id TEXT,
    role_id TEXT,
    status TEXT NOT NULL,
    input_json TEXT,
    output_json TEXT,
    error TEXT,
    pending_hitl_node_id TEXT,
    pending_hitl_json TEXT,
    started_at DATETIME NOT NULL,
    finished_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE SET NULL
);

CREATE INDEX idx_assets_domain ON assets(domain);

CREATE INDEX idx_assets_ip ON assets(ip);

CREATE INDEX idx_assets_last_scan ON assets(last_scan_at);

CREATE INDEX idx_assets_last_seen ON assets(last_seen_at);

CREATE INDEX idx_assets_owner ON assets(owner_user_id);

CREATE INDEX idx_assets_project ON assets(project_id);

CREATE INDEX idx_assets_risk_level ON assets(risk_level);

CREATE INDEX idx_assets_risk_score ON assets(risk_score);

CREATE INDEX idx_assets_status ON assets(status);

CREATE INDEX idx_assets_vulnerability_count ON assets(vulnerability_count);

CREATE INDEX idx_audit_logs_action ON audit_logs(action);

CREATE INDEX idx_audit_logs_category ON audit_logs(category);

CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);

CREATE INDEX idx_audit_logs_result ON audit_logs(result);

CREATE INDEX idx_batch_task_queues_created_at ON batch_task_queues(created_at);

CREATE INDEX idx_batch_task_queues_title ON batch_task_queues(title);

CREATE INDEX idx_batch_tasks_queue_id ON batch_tasks(queue_id);

CREATE INDEX idx_c2_events_category ON c2_events(category);

CREATE INDEX idx_c2_events_created_at ON c2_events(created_at);

CREATE INDEX idx_c2_events_session ON c2_events(session_id);

CREATE INDEX idx_c2_files_session ON c2_files(session_id);

CREATE INDEX idx_c2_listeners_created_at ON c2_listeners(created_at);

CREATE INDEX idx_c2_listeners_project_id ON c2_listeners(project_id);

CREATE INDEX idx_c2_listeners_status ON c2_listeners(status);

CREATE INDEX idx_c2_payload_artifacts_listener ON c2_payload_artifacts(listener_id);

CREATE INDEX idx_c2_sessions_last_check_in ON c2_sessions(last_check_in);

CREATE INDEX idx_c2_sessions_listener ON c2_sessions(listener_id);

CREATE INDEX idx_c2_sessions_status ON c2_sessions(status);

CREATE INDEX idx_c2_tasks_conversation ON c2_tasks(conversation_id);

CREATE INDEX idx_c2_tasks_created_at ON c2_tasks(created_at);

CREATE INDEX idx_c2_tasks_session ON c2_tasks(session_id);

CREATE INDEX idx_c2_tasks_status ON c2_tasks(status);

CREATE INDEX idx_chain_edges_conversation ON attack_chain_edges(conversation_id);

CREATE INDEX idx_chain_edges_source ON attack_chain_edges(source_node_id);

CREATE INDEX idx_chain_edges_target ON attack_chain_edges(target_node_id);

CREATE INDEX idx_chain_nodes_conversation ON attack_chain_nodes(conversation_id);

CREATE INDEX idx_chat_upload_artifacts_conversation ON chat_upload_artifacts(conversation_id);

CREATE INDEX idx_chat_upload_artifacts_owner ON chat_upload_artifacts(owner_user_id);

CREATE INDEX idx_conversations_pinned ON conversations(pinned);

CREATE INDEX idx_conversations_project_id ON conversations(project_id);

CREATE INDEX idx_conversations_updated_at ON conversations(updated_at);

CREATE INDEX idx_knowledge_retrieval_logs_conversation ON knowledge_retrieval_logs(conversation_id);

CREATE INDEX idx_knowledge_retrieval_logs_created_at ON knowledge_retrieval_logs(created_at);

CREATE INDEX idx_knowledge_retrieval_logs_message ON knowledge_retrieval_logs(message_id);

CREATE INDEX idx_messages_conversation_id ON messages(conversation_id);

CREATE INDEX idx_model_token_usage_conversation ON model_token_usage(conversation_id);

CREATE INDEX idx_model_token_usage_created_at ON model_token_usage(created_at);

CREATE INDEX idx_model_token_usage_model ON model_token_usage(model);

CREATE INDEX idx_model_token_usage_project ON model_token_usage(project_id);

CREATE INDEX idx_notification_reads_user_read_at ON notification_reads_by_user(user_id, read_at DESC);

CREATE INDEX idx_process_details_conversation_id ON process_details(conversation_id);

CREATE INDEX idx_process_details_message_id ON process_details(message_id);

CREATE INDEX idx_project_fact_edges_project ON project_fact_edges(project_id);

CREATE INDEX idx_project_fact_edges_source ON project_fact_edges(project_id, source_fact_key);

CREATE INDEX idx_project_fact_edges_target ON project_fact_edges(project_id, target_fact_key);

CREATE INDEX idx_project_facts_confidence ON project_facts(confidence);

CREATE INDEX idx_project_facts_project_id ON project_facts(project_id);

CREATE INDEX idx_project_facts_related_vuln ON project_facts(related_vulnerability_id);

CREATE INDEX idx_projects_status ON projects(status);

CREATE INDEX idx_projects_updated_at ON projects(updated_at);

CREATE INDEX idx_rbac_assignments_resource ON rbac_resource_assignments(resource_type, resource_id);

CREATE INDEX idx_rbac_assignments_user_resource ON rbac_resource_assignments(user_id, resource_type, resource_id);

CREATE INDEX idx_rbac_role_permissions_role ON rbac_role_permissions(role_id);

CREATE INDEX idx_rbac_user_roles_user ON rbac_user_roles(user_id);

CREATE INDEX idx_robot_binding_codes_expiry ON robot_binding_codes(expires_at);

CREATE INDEX idx_robot_user_bindings_user ON robot_user_bindings(rbac_user_id);

CREATE INDEX idx_robot_user_sessions_updated_at ON robot_user_sessions(updated_at);

CREATE INDEX idx_tool_executions_conversation ON tool_executions(conversation_id);

CREATE INDEX idx_tool_executions_owner ON tool_executions(owner_user_id);

CREATE INDEX idx_tool_executions_start_time ON tool_executions(start_time);

CREATE INDEX idx_tool_executions_status ON tool_executions(status);

CREATE INDEX idx_tool_executions_tool_name ON tool_executions(tool_name);

CREATE INDEX idx_vulnerabilities_conversation_id ON vulnerabilities(conversation_id);

CREATE INDEX idx_vulnerabilities_conversation_tag ON vulnerabilities(conversation_tag);

CREATE INDEX idx_vulnerabilities_created_at ON vulnerabilities(created_at);

CREATE INDEX idx_vulnerabilities_project_id ON vulnerabilities(project_id);

CREATE INDEX idx_vulnerabilities_severity ON vulnerabilities(severity);

CREATE INDEX idx_vulnerabilities_status ON vulnerabilities(status);

CREATE INDEX idx_vulnerabilities_task_tag ON vulnerabilities(task_tag);

CREATE INDEX idx_webshell_connection_states_updated_at ON webshell_connection_states(updated_at);

CREATE INDEX idx_webshell_connections_created_at ON webshell_connections(created_at);

CREATE INDEX idx_webshell_connections_project_id ON webshell_connections(project_id);

CREATE INDEX idx_workflow_definitions_enabled ON workflow_definitions(enabled);

CREATE INDEX idx_workflow_definitions_updated_at ON workflow_definitions(updated_at);

CREATE INDEX idx_workflow_node_runs_run ON workflow_node_runs(run_id);

CREATE INDEX idx_workflow_package_inspections_creator_expiry ON workflow_package_inspections(created_by, expires_at);

CREATE INDEX idx_workflow_runs_conversation ON workflow_runs(conversation_id);

CREATE INDEX idx_workflow_runs_status ON workflow_runs(status);

CREATE INDEX idx_workflow_runs_workflow ON workflow_runs(workflow_id);

CREATE UNIQUE INDEX uq_workflow_package_imports_actor_key ON workflow_package_imports(actor_user_id, idempotency_key);

CREATE UNIQUE INDEX uq_workflow_package_imports_inspection_success ON workflow_package_imports(inspection_id) WHERE result IN ('created','overwritten','renamed','kept_existing','skipped_identical');
