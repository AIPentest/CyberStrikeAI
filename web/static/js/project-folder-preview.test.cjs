const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');

const projects = fs.readFileSync('web/static/js/projects.js', 'utf8');
const styles = fs.readFileSync('web/static/css/style.css', 'utf8');
const chat = fs.readFileSync('web/static/js/chat.js', 'utf8');
const html = fs.readFileSync('web/templates/index.html', 'utf8');
const rbac = fs.readFileSync('web/static/js/rbac-guards.js', 'utf8');
const zh = fs.readFileSync('web/static/i18n/zh-CN.json', 'utf8');
const en = fs.readFileSync('web/static/i18n/en-US.json', 'utf8');

function functionSource(source, name, nextName) {
    const start = source.indexOf(`function ${name}(`);
    const end = source.indexOf(`function ${nextName}(`, start);
    assert.notEqual(start, -1, `${name} should exist`);
    assert.notEqual(end, -1, `${nextName} should follow ${name}`);
    return source.slice(start, end);
}

function cssBlock(source, selector) {
    const match = source.match(new RegExp(`${selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s*\\{[^}]*\\}`));
    assert.ok(match, `${selector} style block should exist`);
    return match[0];
}

test('无项目文件夹与普通项目共用悬浮和键盘聚焦预览', () => {
    const source = functionSource(projects, 'appendChatProjectFolderItem', 'appendChatProjectConversationItem');

    assert.match(source, /row\.addEventListener\('mouseenter', \(\) => scheduleShowProjectFolderPreview/);
    assert.match(source, /button\.addEventListener\('focus', \(\) => scheduleShowProjectFolderPreview/);
    assert.doesNotMatch(
        source,
        /if \(!isUnassigned\) \{\s*row\.addEventListener\('mouseenter', \(\) => scheduleShowProjectFolderPreview/
    );
});

test('无项目预览隐藏测试范围和编辑入口', () => {
    const source = functionSource(projects, 'showProjectFolderPreview', 'scheduleShowProjectFolderPreview');

    assert.match(source, /preview\.classList\.toggle\('is-unassigned', isUnassigned\)/);
    assert.match(source, /scopeRow\.hidden = isUnassigned \|\| !scope/);
    assert.match(source, /editButton\.hidden = isUnassigned/);
    assert.match(styles, /\.project-folder-preview\.is-unassigned \.project-folder-preview-edit\s*\{\s*display: none !important;/);
    assert.match(styles, /\.project-folder-preview\.is-unassigned \.project-folder-preview-details\s*\{\s*border-bottom: 0;/);
});

test('项目标题提供受权限保护的新建项目入口', () => {
    const source = functionSource(projects, 'showNewProjectModalFromChatSidebar', 'saveProjectModal');

    assert.match(html, /class="add-group-btn project-folders-add-btn"[\s\S]*?onclick="showNewProjectModalFromChatSidebar\(\)"/);
    assert.match(chat, /projectHeader\.querySelector\('\.project-folders-add-btn'\)/);
    assert.match(source, /window\._projectModalFromChat = false/);
    assert.match(source, /window\._projectModalFromChatSidebar = true/);
    assert.match(rbac, /showNewProjectModalFromChatSidebar: 'project:write'/);
});

test('对话项目归属尚未加载时不会误展开无项目', () => {
    const resolver = functionSource(projects, 'resolveChatProjectFolderSelection', 'renderChatProjectFolders');
    const render = functionSource(projects, 'renderChatProjectFolders', 'refreshChatProjectFolders');

    assert.match(resolver, /if \(!chatProjectFolderContext\.ready\) return null/);
    assert.match(resolver, /if \(!conversation\) return null/);
    assert.match(resolver, /conversation\.projectId \|\| conversation\.project_id \|\| ''/);
    assert.match(render, /const selectedId = resolveChatProjectFolderSelection\(\)/);
    assert.match(render, /selectedId !== null && chatProjectFolderLastSelectionId !== selectedId/);
});

test('项目按展开状态切换 Codex 风格的打开和关闭文件夹', () => {
    const icon = functionSource(projects, 'projectFolderIconMarkup', 'clampProjectPreviewText');
    const folder = functionSource(projects, 'appendChatProjectFolderItem', 'appendChatProjectConversationItem');

    assert.match(icon, /const path = isExpanded/);
    assert.match(icon, /M3\.5 18V6\.5/);
    assert.match(icon, /M3\.5 7a2 2 0 0 1 2-2h4l2 2/);
    assert.match(folder, /icon\.className = 'project-folder-icon';/);
    assert.match(folder, /icon\.innerHTML = projectFolderIconMarkup\(isExpanded\);/);
});

test('项目名仅在界面按 12 个 Unicode 字符省略并保留完整悬浮信息', () => {
    const formatterSource = functionSource(chat, 'formatProjectNameForDisplay', 'applyProjectNameDisplay');
    const formatter = new Function(
        'PROJECT_NAME_DISPLAY_MAX_CHARACTERS',
        `${formatterSource}; return formatProjectNameForDisplay;`
    )(12);
    const folder = functionSource(projects, 'appendChatProjectFolderItem', 'appendChatProjectConversationItem');
    const picker = functionSource(projects, 'appendChatProjectPanelItem', 'appendChatProjectPanelMessage');
    const button = functionSource(projects, 'updateChatProjectButtonLabel', 'renderChatProjectPanel');

    assert.equal(formatter('十二字符以内'), '十二字符以内');
    assert.equal(formatter('这是一个非常非常长的项目名称'), '这是一个非常非常长的项目…');
    assert.equal(formatter('😀😀😀😀😀😀😀😀😀😀😀😀😀'), '😀😀😀😀😀😀😀😀😀😀😀😀…');
    assert.match(chat, /const PROJECT_NAME_DISPLAY_MAX_CHARACTERS = 12/);
    assert.match(folder, /applyProjectNameDisplay\(title, project\.name/);
    assert.match(projects, /applyProjectNameDisplay\(titleEl, text\)/);
    assert.match(picker, /title="\$\{escapeAttr\(fullName\)\}"/);
    assert.match(picker, /setAttribute\('aria-label', fullName\)/);
    assert.match(button, /applyProjectNameDisplay/);
    assert.match(styles, /\.project-selector-wrapper \.role-selector-text\s*\{[\s\S]*?max-width: 13em/);
});

test('项目文件夹首批显示 6 个并通过加载更多按批追加', () => {
    const loadMore = functionSource(projects, 'loadMoreChatProjectFolders', 'renderChatProjectFolders');
    const render = functionSource(projects, 'renderChatProjectFolders', 'refreshChatProjectFolders');
    const search = functionSource(projects, 'handleProjectFolderSearch', 'clearProjectFolderSearch');

    assert.match(projects, /const CHAT_PROJECT_FOLDER_PAGE_SIZE = 6/);
    assert.match(loadMore, /chatProjectFolderVisibleCount \+= CHAT_PROJECT_FOLDER_PAGE_SIZE/);
    assert.match(render, /const visibleFolders = folders\.slice\(0, chatProjectFolderVisibleCount\)/);
    assert.match(render, /appendChatProjectFoldersLoadMore\(list, folders\.length - visibleFolders\.length\)/);
    assert.match(render, /chatProjectFolderVisibleCount = selectedIndex \+ 1/);
    assert.match(search, /renderChatProjectFolders\(projectsCacheAll\)/);
    assert.match(styles, /\.project-folders-load-more\s*\{/);
    assert.match(zh, /"projectFoldersLoadMoreRemaining": "加载更多，剩余 \{\{count\}\} 个项目"/);
    assert.match(en, /"projectFoldersLoadMoreRemaining": "Load more, \{\{count\}\} projects remaining"/);
});

test('对话悬浮预览显示本地年月日时分', () => {
    const age = functionSource(projects, 'formatProjectConversationPreviewAge', 'getProjectConversationModeLabel');

    assert.match(age, /date\.getFullYear\(\)/);
    assert.match(age, /date\.getMonth\(\) \+ 1/);
    assert.match(age, /date\.getDate\(\)/);
    assert.match(age, /date\.getHours\(\)/);
    assert.match(age, /date\.getMinutes\(\)/);
    assert.match(age, /chat\.conversationPreviewDateTime/);
    assert.doesNotMatch(age, /elapsedMs|conversationPreviewDays|conversationPreviewHours/);
    assert.match(zh, /"conversationPreviewDateTime": "\{\{year\}\}年\{\{month\}\}月\{\{day\}\}日 \{\{hour\}\}:\{\{minute\}\}"/);
    assert.match(en, /"conversationPreviewDateTime": "\{\{year\}\}-\{\{month\}\}-\{\{day\}\} \{\{hour\}\}:\{\{minute\}\}"/);
});

test('对话悬浮预览标题与时间分行显示并保留更多标题内容', () => {
    const titleStyles = cssBlock(styles, '.project-conversation-preview-title');

    assert.match(styles, /\.conversation-sidebar\s*\{[\s\S]*?width: 320px;/);
    assert.match(styles, /\.project-conversation-preview\s*\{[\s\S]*?width: min\(340px, calc\(100vw - 32px\)\);/);
    assert.match(styles, /\.project-conversation-preview-header\s*\{[\s\S]*?flex-direction: column;[\s\S]*?align-items: flex-start;/);
    assert.match(titleStyles, /-webkit-line-clamp: 2;/);
    assert.doesNotMatch(titleStyles, /white-space: nowrap;/);
});

test('项目预览任务统计使用闭合圆环，避免刷新箭头在小尺寸下变成毛刺', () => {
    const source = functionSource(projects, 'ensureProjectFolderPreview', 'positionProjectFolderPreview');

    assert.match(source, /class="project-folder-preview-stats"/);
    assert.match(source, /<circle cx="12" cy="12" r="8" stroke="currentColor" stroke-width="1\.5"\/>/);
    assert.doesNotMatch(source, /H21v5l-2-2/);
    assert.match(styles, /\.project-folder-preview-stats svg \{\s*width: 16px;\s*height: 16px;\s*overflow: visible;/);
});

test('对话悬浮预览使用美化后的代理模式徽标', () => {
    const projects = fs.readFileSync('web/static/js/projects.js', 'utf8');

    assert.match(projects, /function getProjectConversationModeIconClass\(conversation\)/);
    assert.match(projects, /project-conversation-preview-mode-icon agent-mode-logo agent-mode-logo--default/);
    assert.match(projects, /agent-mode-logo__svg/);
    assert.match(projects, /<rect x="3" y="11" width="18" height="10" rx="2"/);
    assert.match(projects, /agent-mode-logo--' \+ getProjectConversationModeIconClass\(conversation\)/);
    assert.match(styles, /\.agent-mode-logo\s*\{[\s\S]*?background: transparent;/);
    assert.match(styles, /\.agent-mode-logo__svg\s*\{[\s\S]*?stroke: currentColor;[\s\S]*?stroke-width: 1\.9;/);
    assert.match(styles, /\.project-conversation-preview-mode-icon\.agent-mode-logo\s*\{[\s\S]*?width: 16px;/);
    assert.doesNotMatch(cssBlock(styles, '.agent-mode-logo'), /linear-gradient|box-shadow: 0 5px/);
});
