const AGENT_MODES = [
  { id: 'eino_single', label: 'Eino Single (ADK)', path: '/api/eino-agent/stream' },
  { id: 'deep', label: 'Deep (DeepAgent)', path: '/api/multi-agent/stream', orchestration: 'deep' },
  { id: 'plan_execute', label: 'Plan-Execute', path: '/api/multi-agent/stream', orchestration: 'plan_execute' },
  { id: 'supervisor', label: 'Supervisor', path: '/api/multi-agent/stream', orchestration: 'supervisor' },
];

function agentModeById(id) {
  return AGENT_MODES.find((m) => m.id === id) || AGENT_MODES[0];
}

async function apiFetch(baseUrl, path, options = {}) {
  let res;
  try {
    res = await fetch(baseUrl + path, options);
  } catch (err) {
    if (err && err.name === 'AbortError') throw err;
    const detail = err instanceof Error ? err.message : String(err);
    const e = new Error(
      `Cannot reach ${baseUrl}. Check network/CORS and trust the HTTPS certificate in this browser` +
      (detail ? ` (${detail})` : '')
    );
    e.network = true;
    e.cause = err;
    throw e;
  }
  const text = await res.text();
  let json = null;
  try {
    json = text ? JSON.parse(text) : null;
  } catch (_) {
    json = null;
  }
  if (!res.ok) {
    const err = new Error((json && json.error) || text || `HTTP ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return json;
}

async function loginAndValidate(baseUrl, password, signal) {
  const login = await apiFetch(baseUrl, '/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ password }),
    signal,
  });
  const token = login && login.token;
  if (!token) throw new Error('Login response missing token');
  await validateTokenSession(baseUrl, token, signal);
  return {
    token,
    expiresAt: (login && login.expires_at) || '',
  };
}

async function validateTokenSession(baseUrl, token, signal) {
  await apiFetch(baseUrl, '/api/auth/validate', {
    method: 'GET',
    headers: { Authorization: `Bearer ${token}` },
    signal,
  });
}

async function fetchProjects(baseUrl, token, signal) {
  const data = await apiFetch(baseUrl, '/api/projects?limit=500', {
    headers: { Authorization: `Bearer ${token}` },
    signal,
  });
  const list = (data && data.projects) || [];
  const out = [{ id: '', label: '(none)' }];
  for (const p of list) {
    if (!p.id) continue;
    let label = p.name || p.id;
    if (p.status === 'archived') label += ' [archived]';
    out.push({ id: p.id, label });
  }
  return out;
}

async function fetchRoles(baseUrl, token, signal) {
  const data = await apiFetch(baseUrl, '/api/roles', {
    headers: { Authorization: `Bearer ${token}` },
    signal,
  });
  const list = (data && data.roles) || [];
  const out = [];
  for (const r of list) {
    if (r.enabled === false) continue;
    if (!r.name) continue;
    // Server default role is named "Default"; map to empty id + English label (UI is EN).
    // Skip it here and prepend a single Default entry below — avoids Default + Default.
    if (r.name === 'Default') continue;
    out.push({ id: r.name, label: r.name });
  }
  out.sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: 'base' }));
  out.unshift({ id: '', label: 'Default' });
  return out;
}

function extractConversationId(ev) {
  if (!ev || typeof ev !== 'object') return '';
  if (ev.conversationId) return String(ev.conversationId).trim();
  if (ev.data && ev.data.conversationId) return String(ev.data.conversationId).trim();
  return '';
}

async function streamTest(baseUrl, token, options, handlers) {
  const mode = agentModeById(options.agentMode);
  const body = {
    message: options.message,
    conversationId: '',
    role: options.role || '',
  };
  if (options.projectId) body.projectId = options.projectId;
  if (mode.orchestration) body.orchestration = mode.orchestration;

  const controller = new AbortController();
  if (handlers.setAbortController) handlers.setAbortController(controller);

  const res = await fetch(baseUrl + mode.path, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(body),
    signal: controller.signal,
  });

  if (!res.ok) {
    const t = await res.text();
    const err = new Error(t || `HTTP ${res.status}`);
    err.status = res.status;
    throw err;
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() || '';
    for (const line of lines) {
      if (!line.startsWith('data:')) continue;
      const json = line.slice(5).trim();
      if (!json) continue;
      let ev;
      try {
        ev = JSON.parse(json);
      } catch (_) {
        continue;
      }
      const type = ev.type || '';
      const message = ev.message || '';
      if (handlers.onEvent) handlers.onEvent(type, message, ev);
      if (type === 'done') {
        if (handlers.onDone) handlers.onDone();
        return;
      }
    }
  }
  if (handlers.onDone) handlers.onDone();
}

async function cancelByConversationId(baseUrl, token, conversationId) {
  await apiFetch(baseUrl, '/api/agent-loop/cancel', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ conversationId }),
  });
}
