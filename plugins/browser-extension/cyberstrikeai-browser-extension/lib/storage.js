/* global chrome */

const STORAGE_KEYS = {
  username: 'csai_username',
  host: 'csai_host',
  port: 'csai_port',
  https: 'csai_https',
  lastProjectId: 'csai_last_project',
  lastRole: 'csai_last_role',
  lastAgentMode: 'csai_last_agent_mode',
  lastInstruction: 'csai_last_instruction',
  filterApiOnly: 'csai_filter_api_only',
  captureEnabled: 'csai_capture_enabled',
  renderMarkdown: 'csai_render_markdown',
  showDebugEvents: 'csai_show_debug',
};

const SESSION_TOKEN_KEY = 'csai_token';

const DEFAULTS = {
  username: 'admin',
  host: '127.0.0.1',
  port: '8080',
  https: true,
  lastInstruction: CSAI_DEFAULT_INSTRUCTION,
  filterApiOnly: true,
  captureEnabled: true,
  renderMarkdown: true,
  showDebugEvents: false,
};

function extensionContextAlive() {
  try {
    return !!(typeof chrome !== 'undefined' && chrome.runtime && chrome.runtime.id);
  } catch (_) {
    return false;
  }
}

function extensionContextError() {
  const err = new Error('Extension context invalidated');
  err.name = 'ExtensionContextError';
  err.contextInvalidated = true;
  return err;
}

function isExtensionContextError(err) {
  if (!err) return false;
  if (err.contextInvalidated || err.name === 'ExtensionContextError') return true;
  return /extension context invalidated/i.test(String(err.message || err));
}

function normalizeStorageError(err) {
  if (isExtensionContextError(err)) return extensionContextError();
  return err instanceof Error ? err : new Error(String(err));
}

function rejectLastError(reject) {
  const msg = chrome.runtime.lastError && chrome.runtime.lastError.message;
  if (!msg) return false;
  reject(/invalidated/i.test(msg) ? extensionContextError() : new Error(msg));
  return true;
}

function localGet(keys) {
  return new Promise((resolve, reject) => {
    try {
      if (!extensionContextAlive()) {
        reject(extensionContextError());
        return;
      }
      chrome.storage.local.get(keys, (data) => {
        if (rejectLastError(reject)) return;
        resolve(data);
      });
    } catch (err) {
      reject(normalizeStorageError(err));
    }
  });
}

function localSet(obj) {
  return new Promise((resolve, reject) => {
    try {
      if (!extensionContextAlive()) {
        reject(extensionContextError());
        return;
      }
      chrome.storage.local.set(obj, () => {
        if (rejectLastError(reject)) return;
        resolve();
      });
    } catch (err) {
      reject(normalizeStorageError(err));
    }
  });
}

function sessionGet(keys) {
  return new Promise((resolve, reject) => {
    try {
      if (!extensionContextAlive()) {
        reject(extensionContextError());
        return;
      }
      const store = chrome.storage.session || chrome.storage.local;
      store.get(keys, (data) => {
        if (rejectLastError(reject)) return;
        resolve(data);
      });
    } catch (err) {
      reject(normalizeStorageError(err));
    }
  });
}

function sessionSet(obj) {
  return new Promise((resolve, reject) => {
    try {
      if (!extensionContextAlive()) {
        reject(extensionContextError());
        return;
      }
      const store = chrome.storage.session || chrome.storage.local;
      store.set(obj, () => {
        if (rejectLastError(reject)) return;
        resolve();
      });
    } catch (err) {
      reject(normalizeStorageError(err));
    }
  });
}

async function loadConfig() {
  const data = await localGet(Object.values(STORAGE_KEYS));
  const sess = await sessionGet([SESSION_TOKEN_KEY, SESSION_TOKEN_EXPIRY_KEY]);
  return {
    username: data[STORAGE_KEYS.username] ?? DEFAULTS.username,
    host: data[STORAGE_KEYS.host] || DEFAULTS.host,
    port: data[STORAGE_KEYS.port] || DEFAULTS.port,
    https: data[STORAGE_KEYS.https] !== false,
    token: sess[SESSION_TOKEN_KEY] || '',
    tokenExpiresAt: sess[SESSION_TOKEN_EXPIRY_KEY] || '',
    lastProjectId: data[STORAGE_KEYS.lastProjectId] || '',
    lastRole: data[STORAGE_KEYS.lastRole] || '',
    lastAgentMode: data[STORAGE_KEYS.lastAgentMode] || 'eino_single',
    lastInstruction: data[STORAGE_KEYS.lastInstruction] || DEFAULTS.lastInstruction,
    filterApiOnly: data[STORAGE_KEYS.filterApiOnly] !== false,
    captureEnabled: data[STORAGE_KEYS.captureEnabled] !== false,
    renderMarkdown: data[STORAGE_KEYS.renderMarkdown] !== false,
    showDebugEvents: data[STORAGE_KEYS.showDebugEvents] === true,
  };
}

async function saveConfig(partial) {
  const localMap = {};
  if (partial.username != null) localMap[STORAGE_KEYS.username] = partial.username;
  if (partial.host != null) localMap[STORAGE_KEYS.host] = partial.host;
  if (partial.port != null) localMap[STORAGE_KEYS.port] = partial.port;
  if (partial.https != null) localMap[STORAGE_KEYS.https] = partial.https;
  if (partial.lastProjectId != null) localMap[STORAGE_KEYS.lastProjectId] = partial.lastProjectId;
  if (partial.lastRole != null) localMap[STORAGE_KEYS.lastRole] = partial.lastRole;
  if (partial.lastAgentMode != null) localMap[STORAGE_KEYS.lastAgentMode] = partial.lastAgentMode;
  if (partial.lastInstruction != null) localMap[STORAGE_KEYS.lastInstruction] = partial.lastInstruction;
  if (partial.filterApiOnly != null) localMap[STORAGE_KEYS.filterApiOnly] = partial.filterApiOnly;
  if (partial.captureEnabled != null) localMap[STORAGE_KEYS.captureEnabled] = partial.captureEnabled;
  if (partial.renderMarkdown != null) localMap[STORAGE_KEYS.renderMarkdown] = partial.renderMarkdown;
  if (partial.showDebugEvents != null) localMap[STORAGE_KEYS.showDebugEvents] = partial.showDebugEvents;
  if (Object.keys(localMap).length) await localSet(localMap);
  const sessionMap = {};
  if (partial.token != null) sessionMap[SESSION_TOKEN_KEY] = partial.token;
  if (partial.tokenExpiresAt != null) sessionMap[SESSION_TOKEN_EXPIRY_KEY] = partial.tokenExpiresAt;
  if (Object.keys(sessionMap).length) await sessionSet(sessionMap);
}

function baseUrlFrom(cfg) {
  const scheme = cfg.https ? 'https' : 'http';
  return `${scheme}://${cfg.host}:${cfg.port}`;
}

/**
 * Request optional host permission for the configured CyberStrikeAI origin.
 *
 * Keep chrome.permissions.request() synchronous with the caller's click event.
 * Awaiting permissions.contains() first can consume Chrome's transient user
 * activation and make the request fail without showing a permission prompt.
 */
function ensureHostPermission(baseUrl) {
  if (!extensionContextAlive()) throw extensionContextError();
  if (!chrome.permissions || !chrome.permissions.request) return Promise.resolve();
  let origin;
  try {
    origin = new URL(baseUrl).origin + '/*';
  } catch (_) {
    throw new Error('Invalid Host/Port');
  }

  // Do not add an await before this call. Chrome requires a user gesture for
  // optional permission requests. Requesting an already granted origin is
  // idempotent and resolves true without prompting again.
  return chrome.permissions.request({ origins: [origin] }).then((granted) => {
    if (granted) return;
    const err = new Error(`Allow extension access to ${new URL(baseUrl).origin} to continue`);
    err.code = 'HOST_PERMISSION_DENIED';
    throw err;
  });
}
