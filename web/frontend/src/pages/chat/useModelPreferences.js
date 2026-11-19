import { useState } from 'react';
import { useResource } from '../../components/hooks';
import { api } from '../../lib/http';

export const REASONING_MODES = ['default', 'off', 'on', 'auto'];
export const REASONING_EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'];

// The legacy composer stored plain strings under these keys.
function useChatPreference(key, fallback) {
    const [value, setValue] = useState(() => {
        try { return localStorage.getItem(key) ?? fallback; } catch { return fallback; }
    });

    return [value, next => {
        setValue(next);
        try { localStorage.setItem(key, next); } catch { /* storage unavailable */ }
    }];
}

export function useModelPreferences(canReadConfig) {
    const [storedChannel, setChannel] = useChatPreference('cyberstrike-chat-ai-channel', '');
    const [storedMode, setMode] = useChatPreference('cyberstrike-chat-reasoning-mode', 'default');
    const [storedEffort, setEffort] = useChatPreference('cyberstrike-chat-reasoning-effort', '');
    const config = useResource(async () => (await api.get('/config'))?.ai, [canReadConfig], { skip: !canReadConfig });
    const channels = Object.entries(config.data?.channels || {}).sort(([a], [b]) => a.localeCompare(b));
    const normalizedChannel = storedChannel.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    const channel = channels.find(([id]) => id === storedChannel)?.[0]
        || channels.find(([id]) => id === normalizedChannel)?.[0] || '';
    const mode = REASONING_MODES.includes(storedMode) ? storedMode : 'default';
    const effort = REASONING_EFFORTS.includes(storedEffort) ? storedEffort : '';
    const reasoning = {
        mode: mode === 'default' ? undefined : mode,
        effort: mode === 'off' ? undefined : effort || undefined
    };

    return {
        channel, channels, setChannel, config, mode, setMode, effort, setEffort,
        waitingForChannel: Boolean(canReadConfig && storedChannel && config.loading),
        request: {
            aiChannelId: canReadConfig ? channel || undefined : undefined,
            reasoning: reasoning.mode || reasoning.effort ? reasoning : undefined
        }
    };
}
