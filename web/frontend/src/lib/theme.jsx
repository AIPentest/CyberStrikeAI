/**
 * 主题：沿用既有偏好键 cyberstrike-theme（system/light/dark），
 * 通过 body[theme-mode] 驱动 Semi Design 暗色变量，同时同步 html[data-theme]。
 */
import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useState } from 'react';

const STORAGE_KEY = 'cyberstrike-theme';
const OPTIONS = ['system', 'light', 'dark'];

function readPreference() {
    try {
        const stored = localStorage.getItem(STORAGE_KEY);
        return OPTIONS.includes(stored) ? stored : 'system';
    } catch (error) {
        return 'system';
    }
}

function systemPrefersDark() {
    return typeof window !== 'undefined' && window.matchMedia
        ? window.matchMedia('(prefers-color-scheme: dark)').matches
        : false;
}

export function resolveTheme(preference) {
    if (preference === 'dark' || preference === 'light') return preference;
    return systemPrefersDark() ? 'dark' : 'light';
}

function apply(preference) {
    const resolved = resolveTheme(preference);
    const root = document.documentElement;
    root.setAttribute('data-theme', resolved);
    root.setAttribute('data-theme-preference', preference);
    root.style.colorScheme = resolved;
    document.body?.setAttribute('theme-mode', resolved === 'dark' ? 'dark' : 'light');
    return resolved;
}

const ThemeContext = createContext({ preference: 'system', theme: 'light', setPreference: () => {}, toggle: () => {} });

export function ThemeProvider({ children }) {
    const [preference, setPreferenceState] = useState(readPreference);
    const [theme, setTheme] = useState(() => resolveTheme(readPreference()));

    useLayoutEffect(() => {
        setTheme(apply(preference));
        try { localStorage.setItem(STORAGE_KEY, preference); } catch (error) { /* ignore */ }
        if (preference !== 'system' || !window.matchMedia) return undefined;
        const media = window.matchMedia('(prefers-color-scheme: dark)');
        const onChange = () => setTheme(apply(preference));
        media.addEventListener('change', onChange);
        return () => media.removeEventListener('change', onChange);
    }, [preference]);

    useEffect(() => { document.body?.setAttribute('theme-mode', theme === 'dark' ? 'dark' : 'light'); }, [theme]);

    const setPreference = useCallback(next => {
        if (OPTIONS.includes(next)) setPreferenceState(next);
    }, []);

    const toggle = useCallback(() => {
        setPreferenceState(current => {
            const resolved = resolveTheme(current);
            return resolved === 'dark' ? 'light' : 'dark';
        });
    }, []);

    const value = useMemo(() => ({ preference, theme, setPreference, toggle }), [preference, theme, setPreference, toggle]);
    return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
    return useContext(ThemeContext);
}
