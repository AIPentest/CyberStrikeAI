/**
 * Built-in tool name constant
 * All places in the frontend code that use built-in tool names should use these constants instead of hardcoded strings
 * 
 * Note: these constants must stay in sync with the constants in backend's internal/mcp/builtin/constants.go
 */

// Built-in tool name constant
const BuiltinTools = {
    // Vulnerability management tool
    RECORD_VULNERABILITY: 'record_vulnerability',
    
    // Knowledge base tool
    LIST_KNOWLEDGE_RISK_TYPES: 'list_knowledge_risk_types',
    SEARCH_KNOWLEDGE_BASE: 'search_knowledge_base'
};

// Check if it is a built-in tool
function isBuiltinTool(toolName) {
    return Object.values(BuiltinTools).includes(toolName);
}

// Get the list of all built-in tool names
function getAllBuiltinTools() {
    return Object.values(BuiltinTools);
}

