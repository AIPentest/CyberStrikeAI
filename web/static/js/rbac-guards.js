/**
 * Global write-operation permission guard: uniformly wrap the window/C2 methods bound to each page's onclick with a layer of requirePermission.
 * Executes after all business scripts have finished loading (see the reference order in index.html).
 */
(function () {
    'use strict';

    const GLOBAL_WRITE_HANDLER_PERMISSIONS = {
        // dialogue
        sendMessage: 'chat:write',
        startNewConversation: 'chat:write',
        deleteConversation: 'chat:delete',
        deleteConversationTurnFromUI: 'chat:delete',
        deleteConversationFromContext: 'chat:delete',
        showBatchManageModal: 'chat:delete',
        deleteSelectedConversations: 'chat:delete',
        renameConversation: 'chat:write',
        pinConversation: 'chat:write',

        // human-machine collaboration
        applyHitlSidebarConfig: 'hitl:write',
        saveHitlPageWhitelist: 'hitl:write',
        saveHitlAuditStrategy: 'hitl:write',
        saveHitlConversationConfig: 'hitl:write',
        submitHitlDecision: 'hitl:write',
        submitHitlDecisionWithPayload: 'hitl:write',
        submitWorkflowHitlDecisionFromPage: 'hitl:write',
        submitWorkflowHitlDecision: 'hitl:write',
        dismissHitlItem: 'hitl:write',
        batchDeleteHitlLogs: 'hitl:write',
        clearHitlLogs: 'hitl:write',

        // project
        showNewProjectModal: 'project:write',
        showNewProjectModalFromChat: 'project:write',
        showNewProjectModalFromChatSidebar: 'project:write',
        showNewProjectModalFromWebshellAi: 'project:write',
        showEditProjectModal: 'project:write',
        saveProjectModal: 'project:write',
        saveProjectSettings: 'project:write',
        archiveCurrentProject: 'project:write',
        deleteCurrentProject: 'project:delete',
        deleteProjectFromListMenu: 'project:delete',
        toggleProjectFactGraphConnectMode: 'project:write',
        editProjectFromListMenu: 'project:write',
        toggleProjectArchiveFromListMenu: 'project:write',
        showAddFactModal: 'project:write',
        showEditFactModal: 'project:write',
        editSelectedGraphFact: 'project:write',
        editFactFromDetail: 'project:write',
        saveFactModal: 'project:write',
        deleteProjectFactEdge: 'project:delete',
        deprecateProjectFactByKey: 'project:write',
        restoreProjectFactByKey: 'project:write',
        promoteConversationAttackChain: 'attackchain:write',
        linkFactToExistingVulnerability: 'project:write',
        createVulnerabilityFromCurrentFact: 'vulnerability:write',
        unbindConversationFromProject: 'project:write',

        // loophole
        showAddVulnerabilityModal: 'vulnerability:write',
        saveVulnerability: 'vulnerability:write',
        deleteVulnerability: 'vulnerability:delete',
        batchDeleteVulnerabilityReports: 'vulnerability:delete',
        exportVulnerabilityReports: 'vulnerability:read',
        changeVulnerabilityStatus: 'vulnerability:write',
        bindVulnerabilityProject: 'vulnerability:write',

        // Role / Skills / Agents
        showAddRoleModal: 'roles:write',
        saveRole: 'roles:write',
        deleteRole: 'roles:delete',
        showAddSkillModal: 'skills:write',
        saveSkill: 'skills:write',
        deleteSkill: 'skills:delete',
        showAddMarkdownAgentModal: 'agents:write',
        saveMarkdownAgent: 'agents:write',
        deleteMarkdownAgent: 'agents:delete',

        // Knowledge base
        buildKnowledgeIndex: 'knowledge:write',
        rebuildKnowledgeIndexFull: 'knowledge:write',
        showAddKnowledgeItemModal: 'knowledge:write',
        saveKnowledgeItem: 'knowledge:write',
        editKnowledgeItem: 'knowledge:write',
        deleteKnowledgeItem: 'knowledge:delete',
        deleteRetrievalLog: 'knowledge:delete',

        // Settings / MCP
        saveToolGuardConfig: 'config:write',
        addToolGuardRule: 'config:write',
        resetToolGuardConfig: 'config:write',
        changeToolGuardEnabled: 'config:write',
        applySettings: 'config:write',
        saveToolsConfig: 'config:write',
        saveExternalMCP: 'mcp:write',
        showAddExternalMCPModal: 'mcp:write',
        deleteExternalMCP: 'mcp:write',
        toggleExternalMCP: 'mcp:write',
        changePassword: 'auth:self',
        testOpenAIConnection: 'config:write',
        testVisionConnection: 'config:write',
        testHitlAuditModelConnection: 'config:write',
        submitMcpToolAbortModal: 'monitor:write',
        cancelMCPToolExecution: 'monitor:write',

        // FOFA / Information Gathering
        submitFofaSearch: 'fofa:execute',
        scanFofaRow: 'fofa:execute',
        batchScanSelectedFofaRows: 'fofa:execute',
        exportFofaResults: 'fofa:execute',
        importSelectedFofaAssets: 'asset:write',
        importFofaRowAsset: 'asset:write',
        openAssetImport: 'asset:write',
        submitAssetImport: 'asset:write',
        saveAsset: 'asset:write',
        deleteAsset: 'asset:delete',

        // Task queue
        showBatchImportModal: 'tasks:write',
        deleteBatchQueue: 'tasks:delete',
        deleteBatchQueueFromList: 'tasks:delete',
        createBatchQueue: 'tasks:write',
        saveAddBatchTask: 'tasks:write',
        saveInlineTask: 'tasks:write',
        deleteBatchTask: 'tasks:delete',
        deleteBatchTaskFromElement: 'tasks:delete',
        saveInlineTitle: 'tasks:write',
        saveInlineRole: 'tasks:write',
        saveInlineAgentMode: 'tasks:write',
        saveInlineConcurrency: 'tasks:write',
        saveInlineSchedule: 'tasks:write',
        startBatchQueue: 'tasks:write',
        pauseBatchQueue: 'tasks:write',
        rerunBatchQueue: 'tasks:write',
        runSingleBatchTask: 'tasks:write',
        editBatchTaskFromElement: 'tasks:write',
        batchCancelTasks: 'tasks:write',
        cancelTask: 'tasks:write',
        cancelActiveTask: 'tasks:write',
        cancelProgressTask: 'tasks:write',

        // workflow
        saveWorkflowDraft: 'workflow:write',
        applyWorkflowMetaModal: 'workflow:write',
        deleteCurrentWorkflow: 'workflow:delete',
        deleteWorkflowSelection: 'workflow:delete',
        dryRunWorkflowDraft: 'workflow:execute',
        toggleWorkflowEnabled: 'workflow:write',
        addWorkflowNodeFromPalette: 'workflow:write',
        toggleWorkflowConnectMode: 'workflow:write',
        addWorkflowCustomField: 'workflow:write',

        // File Management
        saveChatFilesEdit: 'files:write',
        deleteChatFile: 'files:delete',
        deleteChatFileIdx: 'files:delete',
        deleteChatFolderFromBrowse: 'files:delete',
        submitChatFilesRename: 'files:write',
        submitChatFilesMkdir: 'files:write',
        chatFilesOpenUploadPicker: 'files:write',
        chatFilesUploadFiles: 'files:write',
        onChatFilesUploadPick: 'files:write',
        chatFilesUploadToFolderClick: 'files:write',
        chatFilesDeleteFolderFromBtn: 'files:delete',

        // Monitoring
        deleteExecution: 'monitor:delete',
        batchDeleteExecutions: 'monitor:delete',

        // attack chain
        regenerateAttackChain: 'attackchain:write',
        exportAttackChain: 'attackchain:read',

        // Notification
        markAllNotificationsSeen: 'notification:write',

        // RBAC
        saveRbacUser: 'rbac:write',
        deleteSelectedRbacUser: 'rbac:write',
        saveRbacRole: 'rbac:write',
        deleteRbacRole: 'rbac:write',
        createRbacAssignment: 'rbac:write',
        deleteRbacAssignment: 'rbac:write',
        saveSelectedUserRoles: 'rbac:write',

        // WebShell
        showAddWebshellModal: 'webshell:write',
        showEditWebshellModal: 'webshell:write',
        saveWebshellConnection: 'webshell:write',
        deleteWebshell: 'webshell:delete',
        testWebshellConnection: 'webshell:write',

        // robot
        openRobotCreateModal: 'robot:write',
        openRobotEditor: 'robot:write',
        startWechatRobotBind: 'robot:write',
        submitWechatVerifyCode: 'robot:write',

        // Audit
        exportAuditLogs: 'audit:read',
        exportAuditLogsCsv: 'audit:read',
        runAuditExport: 'audit:read',

        // Agent interrupt
        submitUserInterruptContinue: 'agent:execute',
        submitUserInterruptHardCancel: 'agent:execute',

        // Terminal (multi-session)
        addTerminalTab: 'terminal:execute',
        removeTerminalTab: 'terminal:execute',
    };

    const NAMESPACE_WRITE_HANDLER_PERMISSIONS = {
        C2: {
            showCreateListenerModal: 'c2:write',
            createListener: 'c2:write',
            saveListener: 'c2:write',
            editListener: 'c2:write',
            startListener: 'c2:write',
            stopListener: 'c2:write',
            deleteListener: 'c2:delete',
            deleteSessionRecord: 'c2:delete',
            deleteSelectedSessions: 'c2:delete',
            deleteFilteredSessions: 'c2:delete',
            killSession: 'c2:write',
            setSessionSleep: 'c2:write',
            submitSessionSleep: 'c2:write',
            uploadFileToImplant: 'c2:write',
            onC2FileUploadPick: 'c2:write',
            openFileUploadPicker: 'c2:write',
            deleteTaskById: 'c2:delete',
            deleteSelectedTasks: 'c2:delete',
            cancelTask: 'c2:write',
            buildBeacon: 'c2:write',
            generateOneliner: 'c2:write',
            createProfile: 'c2:write',
            showCreateProfileModal: 'c2:write',
            deleteProfile: 'c2:delete',
            deleteEventById: 'c2:delete',
            deleteSelectedEvents: 'c2:delete',
            runTerminalCommand: 'terminal:execute',
            executeInTerminal: 'terminal:execute',
        },
    };

    function wrapHandlerWithPermission(fn, permission) {
        if (typeof fn !== 'function') return fn;
        if (fn.__rbacGuarded) return fn;
        const wrapped = function rbacGuardedHandler(...args) {
            if (typeof requirePermission === 'function' && !requirePermission(permission)) {
                return undefined;
            }
            return fn.apply(this, args);
        };
        wrapped.__rbacGuarded = true;
        wrapped.__rbacOriginal = fn;
        return wrapped;
    }

    function installWriteHandlerGuards() {
        Object.entries(GLOBAL_WRITE_HANDLER_PERMISSIONS).forEach(([name, permission]) => {
            if (typeof window[name] === 'function') {
                window[name] = wrapHandlerWithPermission(window[name], permission);
            }
        });
        Object.entries(NAMESPACE_WRITE_HANDLER_PERMISSIONS).forEach(([ns, methods]) => {
            const root = window[ns];
            if (!root || typeof root !== 'object') return;
            Object.entries(methods).forEach(([method, permission]) => {
                if (typeof root[method] === 'function') {
                    root[method] = wrapHandlerWithPermission(root[method], permission);
                }
            });
        });
    }

    window.installWriteHandlerGuards = installWriteHandlerGuards;
    installWriteHandlerGuards();
})();
