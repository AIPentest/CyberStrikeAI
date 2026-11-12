/* Offline browser regression for the project header overlap and tab hit targets.
 * Requires Playwright and Chrome (or CHROME_PATH); no server/login/data is used.
 * Run: node --test scripts/project-mobile-layout.test.cjs
 */
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const { chromium } = require('playwright');

const root = path.resolve(__dirname, '..');
const read = file => fs.readFileSync(path.join(root, file), 'utf8');
const template = read('web/templates/index.html');
const start = template.indexOf('<main class="projects-detail"');
assert.ok(start >= 0, 'project detail fixture must exist');
const markup = template.slice(start, template.indexOf('</main>', start) + 7);
const source = read('web/static/js/projects.js');
const switchTab = source.slice(source.indexOf('function switchProjectTab('), source.indexOf('async function loadProjectAssets('));
const tabs = ['facts', 'graph', 'assets', 'conversations', 'vulns', 'settings'];

test('project headers contain their content and all tabs receive mobile taps', async () => {
    const browser = await chromium.launch({
        headless: true,
        ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}),
    });
    try {
        for (const width of [320, 390, 430, 767, 768, 900, 1440]) {
            const context = await browser.newContext({ viewport: { width, height: 900 }, isMobile: width < 768, hasTouch: true });
            try {
                const page = await context.newPage();
                // Keep the real markup and both complete stylesheets, including theme overrides.
                await page.setContent(`<html><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body><div class="container"><header><div class="header-content">CyberStrikeAI</div></header><div id="page-projects" class="page active"><div class="page-content projects-page-layout">${markup}</div></div></div></body></html>`);
                for (const file of ['style.css', 'mobile.css']) await page.addStyleTag({ content: read(`web/static/css/${file}`) });
                // Use the real tab handler; replace only data loaders to keep the test offline.
                await page.addScriptTag({ content: `let currentProjectTab = 'facts'; window.loads = [];
                    ${['Facts', 'FactGraph', 'Assets', 'Conversations', 'Vulnerabilities'].map(name => `function loadProject${name}() { window.loads.push('${name}'); }`).join('\n')}
                    ${switchTab}` });
                for (const locale of ['zh-CN', 'en-US', 'ru-RU']) {
                    const translations = JSON.parse(read(`web/static/i18n/${locale}.json`));
                    await page.evaluate(dict => {
                        document.querySelectorAll('[data-i18n]').forEach(el => {
                            if (el.children.length || el.hasAttribute('data-i18n-attr')) return;
                            const value = el.dataset.i18n.split('.').reduce((obj, key) => obj?.[key], dict);
                            if (typeof value === 'string') el.textContent = value;
                        });
                        document.querySelector('.projects-detail').classList.add('has-project');
                        document.querySelector('#projects-detail-inner').hidden = false;
                        document.querySelector('#projects-detail-title').textContent = '玉米 — Long project name / '.repeat(8);
                        const desc = document.querySelector('#projects-detail-desc');
                        desc.hidden = false;
                        desc.textContent = 'Long project description / 项目描述 '.repeat(20);
                        document.querySelector('#project-stat-sparse').hidden = false;
                        document.querySelector('#projects-detail-meta-time').textContent = '2 days ago';
                    }, translations);
                    for (const theme of ['light', 'dark']) {
                        const label = `${width}px ${locale} ${theme}`;
                        await page.evaluate(theme => { document.documentElement.dataset.theme = theme; }, theme);
                        await page.locator('.projects-detail-header').scrollIntoViewIfNeeded();
                        const layout = await page.evaluate(() => {
                            const header = document.querySelector('.projects-detail-header');
                            const bounds = header.getBoundingClientRect();
                            const children = ['.projects-detail-headline', '.projects-detail-desc', '.projects-detail-header-actions'];
                            return {
                                contained: children.every(selector => {
                                    const r = document.querySelector(selector).getBoundingClientRect();
                                    return r.top >= bounds.top && r.bottom <= bounds.bottom;
                                }),
                                separate: document.querySelector('.projects-tabs').getBoundingClientRect().top >= bounds.bottom,
                                appHeight: document.querySelector('.container > header').getBoundingClientRect().height,
                                overflow: document.documentElement.scrollWidth > innerWidth,
                            };
                        });
                        assert.ok(layout.contained, `${label}: header content escapes its bounds`);
                        assert.ok(layout.separate, `${label}: header overlaps tabs`);
                        assert.equal(layout.overflow, false, `${label}: page overflows horizontally`);
                        if (width < 768) {
                            assert.equal(layout.appHeight, 52, `${label}: app bar height changed`);
                            assert.ok(await page.locator('#project-facts-search').evaluate(input => {
                                const bounds = input.getBoundingClientRect();
                                const icon = document.querySelector('.projects-fact-search-icon').getBoundingClientRect();
                                const textStart = bounds.left + parseFloat(getComputedStyle(input).paddingLeft);
                                return textStart > icon.right && Math.abs((icon.top + icon.bottom - bounds.top - bounds.bottom) / 2) < 1;
                            }), `${label}: search icon overlaps text or is off center`);
                        }
                        for (const tab of [...tabs, 'facts']) {
                            const button = page.locator(`#project-tab-${tab}`);
                            await button.scrollIntoViewIfNeeded();
                            await button.tap({ timeout: 3000 });
                            assert.ok(await button.evaluate(el => {
                                const r = el.getBoundingClientRect();
                                const nav = el.parentElement.getBoundingClientRect();
                                const x = (Math.max(r.left, nav.left, 0) + Math.min(r.right, nav.right, innerWidth)) / 2;
                                return el.contains(document.elementFromPoint(x, r.y + r.height / 2));
                            }), `${label}: ${tab} hit target is covered`);
                            await assert.doesNotReject(() => page.waitForFunction(tab => {
                                return document.querySelector(`#project-tab-${tab}`).classList.contains('is-active') &&
                                    [...document.querySelectorAll('.projects-panel')].every(panel => panel.hidden === (panel.id !== `project-panel-${tab}`));
                            }, tab, { timeout: 1000 }), `${label}: ${tab} failed to switch`);
                        }
                    }

                }
            } finally {
                await context.close();
            }
        }
    } finally {
        await browser.close();
    }
});

// Unlike the header-only fixture above, this uses the entire project page inside
// the actual flex shell, including its fixed heading, project list and bottom bar.
test('mobile project page scrolls its final row above the bottom navigation', async () => {
    const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}) });
    try {
        for (const [width, height, visibleHeight] of [[320, 568, 568], [393, 852, 700], [430, 932, 780], [767, 600, 500]]) {
            const context = await browser.newContext({ viewport: { width, height }, isMobile: true, hasTouch: true });
            try {
                const page = await context.newPage();
                const projectPage = await page.evaluate(template => new DOMParser().parseFromString(template, 'text/html').querySelector('#page-projects').outerHTML, template);
                await page.setContent(`<html><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body><div class="container"><header><div class="header-content">CyberStrikeAI</div></header><div class="main-layout"><div class="content-area">${projectPage}</div></div></div><nav id="m-tabbar">底部导航</nav></body></html>`);
                for (const file of ['style.css', 'mobile.css']) await page.addStyleTag({ content: read(`web/static/css/${file}`) });
                await page.evaluate(visibleHeight => {
                    document.documentElement.style.setProperty('--m-app-height', `${visibleHeight}px`);
                    document.documentElement.style.setProperty('--m-safe-bottom', '20px');
                    document.querySelector('#page-projects').classList.add('active');
                    document.querySelector('.projects-detail').classList.add('has-project');
                    document.querySelector('#projects-detail-inner').hidden = false;
                    document.querySelector('#projects-detail-title').textContent = 'Long project name / 项目名称 '.repeat(6);
                    document.querySelector('#projects-list').innerHTML = '<div class="projects-list-item">项目一</div>'.repeat(3);
                    document.querySelector('#projects-pagination').textContent = '1–3/3　1/1　每页 50';
                }, visibleHeight);
                await page.addScriptTag({ content: `let currentProjectTab = 'facts';
                    ${['Facts', 'FactGraph', 'Assets', 'Conversations', 'Vulnerabilities'].map(name => `function loadProject${name}() {}`).join('\n')}
                    ${switchTab}` });
                for (const theme of ['light', 'dark']) {
                    await page.evaluate(theme => { document.documentElement.dataset.theme = theme; }, theme);
                    for (const rowCount of [1, 35]) {
                        for (const tab of ['facts', 'assets', 'conversations', 'vulns']) {
                            const label = `${width}x${height}/${visibleHeight} ${theme} ${tab} ${rowCount} rows`;
                            await page.evaluate(({tab, rowCount}) => {
                                window.switchProjectTab(tab);
                                const panel = document.querySelector(`#project-panel-${tab}`);
                                const cells = panel.querySelectorAll('thead th').length;
                                panel.querySelector('tbody').innerHTML = Array.from({length: rowCount}, (_, i) => `<tr><td><button type="button" ${i === rowCount - 1 ? 'data-last-row' : ''} onclick="window.lastRowClicked=true">${i + 1}</button></td>${'<td>test</td>'.repeat(cells - 1)}</tr>`).join('');
                                const scroller = document.querySelector('.projects-page-layout');
                                scroller.scrollTop = scroller.scrollHeight;
                            }, {tab, rowCount});
                            if (rowCount === 35 && tab === 'conversations' && theme === 'light') {
                                await page.evaluate(() => { document.querySelector('.projects-page-layout').scrollTop = 0; });
                                const bounds = await page.locator('.projects-page-layout').boundingBox();
                                await page.mouse.move(bounds.x + bounds.width / 2, bounds.y + bounds.height - 20);
                                await page.mouse.wheel(0, 500);
                                await page.waitForFunction(() => document.querySelector('.projects-page-layout').scrollTop > 0);
                                await page.evaluate(() => {
                                    const scroller = document.querySelector('.projects-page-layout');
                                    scroller.scrollTop = scroller.scrollHeight;
                                });
                            }
                            const result = await page.evaluate(tab => {
                                const scroller = document.querySelector('.projects-page-layout');
                                const row = document.querySelector(`#project-panel-${tab} [data-last-row]`);
                                const r = row.getBoundingClientRect();
                                const area = scroller.getBoundingClientRect();
                                const bar = document.querySelector('#m-tabbar').getBoundingClientRect();
                                return {areaBottom: area.bottom, barTop: bar.top, rowTop: r.top, rowBottom: r.bottom, areaTop: area.top,
                                    hit: row.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2))};
                            }, tab);
                            assert.ok(result.areaBottom <= result.barTop + 1, `${label}: page scroller extends under bottom bar ${JSON.stringify(result)}`);
                            assert.ok(result.rowTop >= result.areaTop && result.rowBottom <= result.areaBottom, `${label}: final row is clipped ${JSON.stringify(result)}`);
                            assert.ok(result.hit, `${label}: final row cannot receive a tap`);
                            await page.evaluate(() => { window.lastRowClicked = false; });
                            await page.locator(`#project-panel-${tab} [data-last-row]`).tap();
                            assert.equal(await page.evaluate(() => window.lastRowClicked), true, `${label}: final row tap failed`);
                        }
                    }
                    for (const tab of ['graph', 'settings']) {
                        const result = await page.evaluate(tab => {
                            window.switchProjectTab(tab);
                            const scroller = document.querySelector('.projects-page-layout');
                            scroller.scrollTop = scroller.scrollHeight;
                            const footer = document.querySelector(tab === 'graph' ? '.project-fact-graph-footer' : '.projects-settings-footer');
                            const r = footer.getBoundingClientRect();
                            const area = scroller.getBoundingClientRect();
                            return {visible: r.top >= area.top && r.bottom <= area.bottom,
                                graphHeight: document.querySelector('.project-fact-graph-layout').getBoundingClientRect().height};
                        }, tab);
                        assert.ok(result.visible, `${width}px ${theme} ${tab}: footer is clipped`);
                        if (tab === 'graph') assert.ok(result.graphHeight >= 240, `${width}px ${theme}: graph canvas collapsed`);
                    }
                }
            } finally { await context.close(); }
        }
    } finally { await browser.close(); }
});

test('mobile facts keep badges and actions inside their columns while scrolling horizontally', async () => {
    const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}) });
    try {
        for (const width of [320, 393, 767]) {
            const context = await browser.newContext({viewport: {width, height: 900}, isMobile: true, hasTouch: true});
            try {
                const page = await context.newPage();
                await page.setContent(`<html><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body><div class="container"><header>CyberStrikeAI</header><div id="page-projects" class="page active"><div class="page-content projects-page-layout">${markup}</div></div></div></body></html>`);
                for (const file of ['style.css', 'mobile.css']) await page.addStyleTag({content: read(`web/static/css/${file}`)});
                await page.addScriptTag({content: source});
                const mobileSource = read('web/static/js/mobile-ui.js');
                const wrapTables = mobileSource.slice(mobileSource.indexOf('    function wrapTables('), mobileSource.indexOf('    /* ---------------------------------------------------------------------\n       11.'));
                await page.addScriptTag({content: `const isMobile = () => true;
                    const $$ = (selector, root) => Array.from(root.querySelectorAll(selector));
                    const el = (tag, cls) => Object.assign(document.createElement(tag), {className: cls});
                    ${wrapTables}
                    wrapTables(document);`});
                await page.evaluate(() => {
                    currentProjectId = 'fixture-project';
                    document.querySelector('.projects-detail').classList.add('has-project');
                    document.querySelector('#projects-detail-inner').hidden = false;
                    window.apiFetch = async () => ({ok: true, json: async () => [
                        {id:'note', fact_key:'note/automatic-continuation', category:'note', summary:'自动续接的项目记录 '.repeat(10), body:'详细记录', confidence:'tentative', updated_at:'2026-10-12T10:00:00Z'},
                        {id:'finding', fact_key:'finding/8080/exposure', category:'finding', summary:'8080 / exposed service', body:'## Steps\n1. curl http://target:8080\n## Request\nGET /', confidence:'confirmed', updated_at:'2026-10-12T10:00:00Z', link_counts:{incoming:2,outgoing:10}},
                    ]});
                    refreshProjectHeaderStats = () => {};
                    window.showEditFactModal = () => { window.actionClicked = 'edit'; };
                    window.viewProjectFactBody = () => { window.actionClicked = 'details'; };
                });
                for (const locale of ['zh-CN', 'en-US', 'ru-RU']) {
                    await page.evaluate(dict => {
                        window.t = key => key.split('.').reduce((v, k) => v?.[k], dict) || key;
                        document.querySelectorAll('thead [data-i18n]').forEach(el => { el.textContent = window.t(el.dataset.i18n); });
                    }, JSON.parse(read(`web/static/i18n/${locale}.json`)));
                    await page.evaluate(() => loadProjectFacts());
                    for (const theme of ['light', 'dark']) {
                        const label = `${width}px ${locale} ${theme}`;
                        await page.evaluate(theme => { document.documentElement.dataset.theme = theme; }, theme);
                        const layout = await page.evaluate(() => {
                            const table = document.querySelector('#project-panel-facts table');
                            return {
                                width:table.getBoundingClientRect().width,
                                badCells: [...table.querySelectorAll('thead th, tbody td')].filter(cell => {
                                    const r = cell.getBoundingClientRect();
                                    return [...cell.children].some(child => {
                                        const c = child.getBoundingClientRect();
                                        return c.left < r.left - 1 || c.right > r.right + 1;
                                    });
                                }).map(cell => cell.className || cell.textContent),
                                sticky: getComputedStyle(table.querySelector('td.col-actions')).position,
                            };
                        });
                        assert.ok(layout.width >= 1200, `${label}: eight columns are squeezed to the phone width`);
                        assert.deepEqual(layout.badCells, [], `${label}: badges or actions escape their cells`);
                        assert.notEqual(layout.sticky, 'sticky', `${label}: action column overlays fact data`);
                        const scroller = page.locator('#project-panel-facts .projects-table-wrap');
                        await scroller.evaluate(el => { el.scrollLeft = el.scrollWidth; });
                        for (const [selector, action] of [['.projects-action-btn--edit', 'edit'], ['.projects-action-btn--view', 'details']]) {
                            await page.locator(`#project-facts-tbody ${selector}`).first().tap({timeout: 3000});
                            assert.equal(await page.evaluate(() => window.actionClicked), action, `${label}: ${action} is unreachable`);
                        }
                        await scroller.evaluate(el => { el.scrollLeft = 0; });
                    }
                }
            } finally { await context.close(); }
        }
    } finally { await browser.close(); }
});
