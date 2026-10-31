// Managed HTTP-tool policy. Model transports and unrelated tool processes are separate.
let proxySettingsLoaded = false;
async function loadProxySettings() {
    proxySettingsLoaded = false;
    document.getElementById("proxy-settings-save").disabled = true;
    try {
        const response = await apiFetch('/api/config');
        if (!response.ok) throw new Error(window.t('proxySettings.loadError'));
        const saved = (await response.json()).proxy || {};
        const cfg = saved.mode ? saved : {};
        const defaults = {mode: 'manual', proxy_url: '', proxy_pool: '', proxy_source: '', strategy: 'sticky', max_attempts: 3, cooldown: 60, min_switch_interval: 60};
        for (const [key, fallback] of Object.entries(defaults)) {
            document.getElementById(`proxy-setting-${key}`).value = cfg[key] === undefined || cfg[key] === '' || (key === 'max_attempts' && cfg[key] === 0) ? fallback : cfg[key];
        }
        syncProxySettingsMode();
        proxySettingsLoaded = true;
        document.getElementById("proxy-settings-save").disabled = false;
    } catch (error) { alert(error.message); }
}
function syncProxySettingsMode() {
    const mode = document.getElementById('proxy-setting-mode').value;
    document.querySelectorAll('[data-proxy-mode]').forEach(el => { el.hidden = el.dataset.proxyMode !== mode; });
}
async function saveProxySettings() {
    if (!proxySettingsLoaded) return;
    const button = document.getElementById('proxy-settings-save');
    button.disabled = true;
    try {
        const proxy = {};
        for (const key of ['mode', 'proxy_url', 'proxy_pool', 'proxy_source', 'strategy']) proxy[key] = document.getElementById(`proxy-setting-${key}`).value.trim();
        for (const key of ['max_attempts', 'cooldown', 'min_switch_interval']) {
            const input = document.getElementById(`proxy-setting-${key}`);
            if (!input.checkValidity() || input.value === '') throw new Error(window.t('proxySettings.invalidNumber'));
            proxy[key] = Number(input.value);
        }
        const response = await apiFetch('/api/config', {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({proxy})});
        if (!response.ok) throw new Error((await response.json()).error || window.t('proxySettings.saveError'));
        await loadProxySettings();
        alert(window.t('proxySettings.saved'));
    } catch (error) { alert(error.message); }
    finally { button.disabled = !proxySettingsLoaded; }
}
