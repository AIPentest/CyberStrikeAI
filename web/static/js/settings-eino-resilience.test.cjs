const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = process.cwd();
const settings = fs.readFileSync(path.join(root, 'web/static/js/settings.js'), 'utf8');
const template = fs.readFileSync(path.join(root, 'web/templates/index.html'), 'utf8');
const en = JSON.parse(fs.readFileSync(path.join(root, 'web/static/i18n/en-US.json'), 'utf8'));

test('Eino 模型 retry/failover 设置页读写链路完整', () => {
    [
        'eino-model-retry-max-retries',
        'eino-model-retry-max-backoff-sec',
        'eino-model-failover-channels',
        'eino-model-failover-max-retries'
    ].forEach((id) => {
        assert.match(template, new RegExp(`id="${id}"`));
        assert.match(settings, new RegExp(`getElementById\\('${id}'\\)`));
    });

    [
        'model_retry_max_retries',
        'model_retry_max_backoff_sec',
        'model_failover_channels',
        'model_failover_max_retries'
    ].forEach((key) => {
        assert.match(settings, new RegExp(`${key}`));
    });

    assert.match(settings, /failoverChannelsRaw\.split\(\s*\/\[\\n,，\]\//);
    assert.match(settings, /Array\.from\(new Set\(/);
});

test('Eino 模型 retry/failover 设置项有中英文文案', () => {
    [
        'einoModelRetryMaxRetries',
        'einoModelRetryMaxRetriesHint',
        'einoModelRetryMaxBackoffSec',
        'einoModelRetryMaxBackoffSecHint',
        'einoModelFailoverChannels',
        'einoModelFailoverChannelsPlaceholder',
        'einoModelFailoverChannelsHint',
        'einoModelFailoverMaxRetries',
        'einoModelFailoverMaxRetriesHint'
    ].forEach((key) => {
        assert.equal(typeof en.settingsBasic[key], 'string', `en ${key}`);
        assert.ok(en.settingsBasic[key].length > 0, `en ${key} is empty`);
    });
});

test('AI 通道保存前会自动识别 DeepSeek 官方线路', () => {
    assert.match(settings, /function\s+isOfficialDeepSeekBaseURL/);
    assert.match(settings, /api\.deepseek\.com/);
    assert.match(settings, /profile:\s*'deepseek'/);
    assert.match(settings, /normalizeAIChannelProviderProfile\(\{/);
    assert.match(settings, /normalizeAIConfigProviderProfiles\(currentConfig\.ai\)/);
    assert.match(template, /<option value="deepseek">deepseek<\/option>/);
});

test('切换或新增 AI 通道时会刷新推理线路下拉显示', () => {
    assert.match(settings, /const profileEl = document\.getElementById\('openai-reasoning-profile'\);/);
    assert.match(settings, /syncSettingsCustomSelect\(profileEl\);/);
    assert.match(settings, /reasoning:\s*\{\s*mode:\s*'auto',\s*effort:\s*'',\s*profile:\s*'auto'/);
});
