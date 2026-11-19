/** 通用异步数据装载：统一 loading / error / reload 语义。 */
import { useCallback, useEffect, useRef, useState } from 'react';

export function useResource(loader, deps = [], { initial = null, skip = false } = {}) {
    const [data, setData] = useState(initial);
    const [loading, setLoading] = useState(!skip);
    const [error, setError] = useState(null);
    const seq = useRef(0);

    const reload = useCallback(async (options = {}) => {
        if (skip) return null;
        const current = ++seq.current;
        if (!options.silent) setLoading(true);
        setError(null);
        try {
            const result = await loader();
            if (current === seq.current) setData(result);
            return result;
        } catch (err) {
            if (current === seq.current) setError(err);
            return null;
        } finally {
            if (current === seq.current) setLoading(false);
        }
    }, deps); // eslint-disable-line react-hooks/exhaustive-deps

    useEffect(() => {
        if (skip) { setLoading(false); return; }
        reload();
        return () => { seq.current += 1; };
    }, [reload, skip]);

    return { data, setData, loading, error, reload };
}

export function useDebounced(value, delay = 300) {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const timer = setTimeout(() => setDebounced(value), delay);
        return () => clearTimeout(timer);
    }, [value, delay]);
    return debounced;
}

export function useInterval(callback, delay) {
    const saved = useRef(callback);
    useEffect(() => { saved.current = callback; }, [callback]);
    useEffect(() => {
        if (!delay) return undefined;
        const timer = setInterval(() => saved.current(), delay);
        return () => clearInterval(timer);
    }, [delay]);
}

export function useLocalStorage(key, initial) {
    const [value, setValue] = useState(() => {
        try {
            const raw = localStorage.getItem(key);
            return raw === null ? initial : JSON.parse(raw);
        } catch (error) {
            return initial;
        }
    });
    useEffect(() => {
        try { localStorage.setItem(key, JSON.stringify(value)); } catch (error) { /* ignore */ }
    }, [key, value]);
    return [value, setValue];
}
