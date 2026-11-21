/**
 * 会话与权限：Bearer 令牌保存在 localStorage（与历史版本同一存储键），
 * 启动时调用 /api/auth/validate 刷新角色与权限，401 时整体退出。
 */
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { api, readStoredAuth, setAuthToken, writeStoredAuth } from './http';

const AuthContext = createContext(null);

function normalizeSession(raw, token) {
    if (!raw) return null;
    return {
        token: token || raw.token,
        expiresAt: raw.expires_at || raw.expiresAt || '',
        appVersion: typeof raw.app_version === 'string' ? raw.app_version.trim() : '',
        user: raw.user || null,
        roles: Array.isArray(raw.roles) ? raw.roles : [],
        permissions: Array.isArray(raw.permissions) ? raw.permissions : [],
        scope: raw.scope || ''
    };
}

export function AuthProvider({ children }) {
    const stored = useRef(readStoredAuth()).current;
    const [session, setSession] = useState(stored);
    const [checking, setChecking] = useState(Boolean(stored));

    useEffect(() => {
        setAuthToken(stored?.token || null);
        if (!stored?.token) { setChecking(false); return; }
        let cancelled = false;
        api.get('/auth/validate')
            .then(payload => {
                if (cancelled) return;
                const next = normalizeSession(payload, stored.token);
                setSession(next);
                writeStoredAuth(next);
            })
            .catch(() => {
                if (cancelled) return;
                setSession(null);
                writeStoredAuth(null);
                setAuthToken(null);
            })
            .finally(() => { if (!cancelled) setChecking(false); });
        return () => { cancelled = true; };
    }, []);

    const clear = useCallback(() => {
        setSession(null);
        setAuthToken(null);
        writeStoredAuth(null);
    }, []);

    useEffect(() => {
        const onUnauthorized = () => clear();
        window.addEventListener('csai:unauthorized', onUnauthorized);
        return () => window.removeEventListener('csai:unauthorized', onUnauthorized);
    }, [clear]);

    const login = useCallback(async (username, password) => {
        const payload = await api.post('/auth/login', { username: String(username || '').trim(), password });
        const next = normalizeSession(payload, payload?.token);
        setAuthToken(next?.token || null);
        writeStoredAuth(next);
        setSession(next);
        return next;
    }, []);

    const logout = useCallback(async () => {
        try {
            await api.post('/auth/logout', {});
        } catch (error) {
            /* 令牌已失效也继续清理本地会话 */
        }
        clear();
    }, [clear]);

    const permissions = useMemo(() => new Set(session?.permissions || []), [session]);
    const hasPermission = useCallback(permission => {
        if (!permission) return true;
        return String(permission).split(/\s+/).some(item => permissions.has(item));
    }, [permissions]);
    const hasAnyPermission = useCallback(list => {
        const items = Array.isArray(list) ? list : String(list || '').split(/\s+/).filter(Boolean);
        if (!items.length) return true;
        return items.some(item => permissions.has(item));
    }, [permissions]);
    const refresh = useCallback(async () => {
        if (!session?.token) return null;
        const payload = await api.get('/auth/validate');
        const next = normalizeSession(payload, session.token);
        setSession(next);
        writeStoredAuth(next);
        return next;
    }, [session?.token]);

    const value = useMemo(() => ({
        session,
        user: session?.user || null,
        roles: session?.roles || [],
        permissions,
        hasPermission,
        hasAnyPermission,
        checking,
        authenticated: Boolean(session?.token),
        login,
        logout,
        refresh
    }), [session, permissions, hasPermission, hasAnyPermission, checking, login, logout, refresh]);

    return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
    const context = useContext(AuthContext);
    if (!context) throw new Error('useAuth 必须在 AuthProvider 内使用');
    return context;
}
