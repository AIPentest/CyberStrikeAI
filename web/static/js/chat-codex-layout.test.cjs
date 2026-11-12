const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');

const chat = fs.readFileSync('web/static/js/chat.js', 'utf8');
const monitor = fs.readFileSync('web/static/js/monitor.js', 'utf8');
const projects = fs.readFileSync('web/static/js/projects.js', 'utf8');
const styles = fs.readFileSync('web/static/css/style.css', 'utf8');
const en = JSON.parse(fs.readFileSync('web/static/i18n/en-US.json', 'utf8'));

test('主对话时间线不再创建用户或助手头像', () => {
    assert.doesNotMatch(chat, /createMessageAvatar/);
    assert.doesNotMatch(monitor, /createMessageAvatar/);
    assert.doesNotMatch(chat, /message-avatar/);
    assert.doesNotMatch(styles, /\.message-avatar/);
});

test('新对话使用无图标的项目欢迎空状态', () => {
    assert.match(chat, /function renderChatWelcomeEmptyState\(\)/);
    assert.match(chat, /chat-welcome-empty-state-title/);
    assert.match(chat, /chat-welcome-empty-state-subtitle/);
    assert.doesNotMatch(chat, /chat-welcome-empty-state-icon/);
    assert.match(styles, /\.chat-welcome-empty-state\s*\{[\s\S]*?justify-content: center/);
    assert.match(styles, /\.chat-welcome-empty-state-title/);
    assert.match(styles, /\.chat-welcome-empty-state-subtitle/);
    assert.match(styles, /\.chat-welcome-project-name\s*\{[\s\S]*?border-bottom: 1px dotted currentColor/);
    assert.match(chat, /projectName\.className = 'chat-welcome-project-name'/);
    assert.match(chat, /title\.replaceChildren\(/);
});

test('欢迎语随项目和无项目状态更新', () => {
    assert.match(chat, /window\.t\('chat\.projectWelcomeMessage', \{ project \}\)/);
    assert.match(chat, /window\.t\('chat\.noProjectWelcomeMessage'\)/);
    assert.match(projects, /window\.refreshChatWelcomeEmptyState\(\)/);
    assert.equal(typeof en.chat.projectWelcomeMessage, 'string');
});

test('会话设置打开时提升整个输入区层级并遮住轮次导航', () => {
    assert.match(chat, /function syncChatSessionSettingsLayerState\(\)/);
    assert.match(chat, /inputBar\.classList\.toggle\('is-session-settings-open', open\)/);
    assert.match(styles, /\.chat-input-container\.is-session-settings-open\s*\{[\s\S]*?z-index:\s*121/);
    assert.match(styles, /\.chat-turn-rail\s*\{[\s\S]*?z-index:\s*20/);
});
