/**
 * HTTP 访问层：统一注入会话令牌、解析错误、处理 401 失效。
 * 所有页面只通过这里访问 /api，不直接调用 fetch。
 */
export const AUTH_STORAGE_KEY = 'cyberstrike-auth';

export function readStoredAuth() {
    try {
        const raw = localStorage.getItem(AUTH_STORAGE_KEY);
        if (!raw) return null;
        const stored = JSON.parse(raw);
        if (!stored?.token || !stored?.expiresAt) return null;
        const expiry = new Date(stored.expiresAt);
        if (!Number.isFinite(expiry.getTime()) || expiry.getTime() <= Date.now()) return null;
        return {
            token: stored.token,
            expiresAt: expiry.toISOString(),
            user: stored.user || null,
            roles: Array.isArray(stored.roles) ? stored.roles : [],
            permissions: Array.isArray(stored.permissions) ? stored.permissions : [],
            scope: stored.scope || ''
        };
    } catch (error) {
        console.warn('读取会话失败:', error);
        return null;
    }
}

export function writeStoredAuth(session) {
    if (!session) {
        localStorage.removeItem(AUTH_STORAGE_KEY);
        return;
    }
    localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(session));
}

let currentToken = null;

export function setAuthToken(token) {
    currentToken = token || null;
}

export function getAuthToken() {
    return currentToken;
}

export class ApiError extends Error {
    constructor(message, { status = 0, payload = null, path = '' } = {}) {
        super(message);
        this.name = 'ApiError';
        this.status = status;
        this.payload = payload;
        this.path = path;
    }
}

function buildUrl(path, query) {
    const url = path.startsWith('http') ? path : `/api${path.startsWith('/') ? path : `/${path}`}`;
    if (!query) return url;
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(query)) {
        if (value === undefined || value === null || value === '') continue;
        if (Array.isArray(value)) {
            value.forEach(item => params.append(key, item));
        } else {
            params.append(key, String(value));
        }
    }
    const qs = params.toString();
    return qs ? `${url}${url.includes('?') ? '&' : '?'}${qs}` : url;
}

async function readError(response) {
    const contentType = response.headers.get('content-type') || '';
    try {
        if (contentType.includes('application/json')) {
            const payload = await response.json();
            return { message: payload.error || payload.message || `请求失败（HTTP ${response.status}）`, payload };
        }
        const text = await response.text();
        return { message: text?.slice(0, 300) || `请求失败（HTTP ${response.status}）`, payload: text };
    } catch (error) {
        return { message: `请求失败（HTTP ${response.status}）`, payload: null };
    }
}

export async function request(path, options = {}) {
    const { method = 'GET', body, query, headers = {}, signal, raw = false, timeout = 60000 } = options;
    const finalHeaders = { Accept: 'application/json', ...headers };
    let payload = body;
    if (body !== undefined && body !== null && !(body instanceof FormData) && typeof body !== 'string' && !(body instanceof Blob)) {
        finalHeaders['Content-Type'] = 'application/json';
        payload = JSON.stringify(body);
    }
    if (currentToken && !finalHeaders.Authorization) {
        finalHeaders.Authorization = `Bearer ${currentToken}`;
    }
    const controller = new AbortController();
    const timer = timeout ? setTimeout(() => controller.abort(), timeout) : null;
    const abort = () => controller.abort();
    if (signal) {
        if (signal.aborted) controller.abort();
        else signal.addEventListener('abort', abort, { once: true });
    }
    let response;
    try {
        response = await fetch(buildUrl(path, query), { method, headers: finalHeaders, body: payload, signal: controller.signal });
    } catch (error) {
        if (error.name === 'AbortError') throw new ApiError('请求已取消', { path });
        throw new ApiError('网络连接失败，请检查服务状态', { path });
    } finally {
        if (timer) clearTimeout(timer);
        if (signal) signal.removeEventListener('abort', abort);
    }
    if (response.status === 401) {
        window.dispatchEvent(new CustomEvent('csai:unauthorized'));
    }
    if (!response.ok) {
        const { message, payload: errorPayload } = await readError(response);
        throw new ApiError(message, { status: response.status, payload: errorPayload, path });
    }
    if (raw) return response;
    if (response.status === 204) return null;
    const contentType = response.headers.get('content-type') || '';
    if (contentType.includes('application/json')) return response.json();
    return response.text();
}

export const api = {
    get: (path, query, options = {}) => request(path, { ...options, query }),
    post: (path, body, options = {}) => request(path, { ...options, method: 'POST', body }),
    put: (path, body, options = {}) => request(path, { ...options, method: 'PUT', body }),
    patch: (path, body, options = {}) => request(path, { ...options, method: 'PATCH', body }),
    del: (path, options = {}) => request(path, { ...options, method: 'DELETE' }),
    raw: (path, options = {}) => request(path, { ...options, raw: true })
};

/** 下载二进制响应并触发浏览器保存。 */
export async function downloadFile(path, { query, filename, method = 'GET', body } = {}) {
    const response = await request(path, { method, body, query, raw: true, timeout: 120000 });
    const blob = await response.blob();
    const disposition = response.headers.get('content-disposition') || '';
    const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i);
    const plain = disposition.match(/filename="?([^";]+)"?/i);
    const name = filename || (encoded ? decodeURIComponent(encoded[1]) : plain?.[1]) || 'download.bin';
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = name;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    return name;
}

/**
 * 消费服务端 SSE 流。服务端每行以 `data: ` 前缀发送 JSON 对象。
 * onEvent(payload) 逐条回调，返回 abort 函数。
 */
export function postStream(path, body, { onEvent, onError, onClose, signal } = {}) {
    const controller = new AbortController();
    if (signal) {
        if (signal.aborted) controller.abort();
        else signal.addEventListener('abort', () => controller.abort(), { once: true });
    }
    const headers = { 'Content-Type': 'application/json', Accept: 'text/event-stream' };
    if (currentToken) headers.Authorization = `Bearer ${currentToken}`;

    (async () => {
        try {
            const response = await fetch(buildUrl(path), {
                method: 'POST',
                headers,
                body: JSON.stringify(body || {}),
                signal: controller.signal
            });
            if (response.status === 401) window.dispatchEvent(new CustomEvent('csai:unauthorized'));
            if (!response.ok) {
                const { message } = await readError(response);
                throw new ApiError(message, { status: response.status, path });
            }
            if (!response.body) throw new ApiError('服务端未返回流式响应', { path });
            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';
            let closing = false;
            while (!closing) {
                const { done, value } = await reader.read();
                if (done) break;
                buffer += decoder.decode(value, { stream: true });
                let index;
                while ((index = buffer.indexOf('\n')) >= 0) {
                    const line = buffer.slice(0, index).replace(/\r$/, '');
                    buffer = buffer.slice(index + 1);
                    if (!line.startsWith('data:')) continue;
                    const data = line.slice(5).trim();
                    if (!data) continue;
                    if (data === '[DONE]') { closing = true; break; }
                    let parsed;
                    try {
                        parsed = JSON.parse(data);
                    } catch (error) {
                        continue;
                    }
                    onEvent?.(parsed);
                }
            }
            onClose?.();
        } catch (error) {
            if (error.name === 'AbortError') { onClose?.(); return; }
            onError?.(error);
        }
    })();

    return () => controller.abort();
}

export function errorMessage(error, fallback = '操作失败') {
    if (!error) return fallback;
    if (typeof error === 'string') return error;
    return error.message || fallback;
}
