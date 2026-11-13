/* Offline regression. Requires Playwright and Chrome (or CHROME_PATH).
 * Run: node --test scripts/chat-scroll-browser.test.cjs
 */
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { test } = require('node:test');
const { chromium } = require('playwright');

const read = file => fs.readFileSync(path.join(__dirname, '..', 'web/static', file), 'utf8');

async function scrollState(page) {
    return page.evaluate(() => {
        const chat = document.getElementById('chat-messages');
        const timeline = document.querySelector('.progress-timeline');
        return {
            outerGap: chat.scrollHeight - chat.clientHeight - chat.scrollTop,
            innerGap: timeline.scrollHeight - timeline.clientHeight - timeline.scrollTop,
            innerTop: timeline.scrollTop,
            outerFollowing: window.captureScrollPinState(),
            pruned: !!timeline.querySelector('.timeline-live-pruned-marker')
        };
    });
}

test('long live and restored conversations stay at the latest iteration without stealing manual scroll', async () => {
    const browser = await chromium.launch({
        ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}),
    });
    try {
        for (const width of [1440, 390]) {
            for (const restored of [false, true]) {
                const label = `${width}px ${restored ? 'restored' : 'live'}`;
                const page = await browser.newPage({ viewport: { width, height: 820 } });
                try {
                    await page.route('**/*', route => route.request().url() === 'http://chat.test/'
                        ? route.fulfill({
                            contentType: 'text/html',
                            body: `<!doctype html><html data-theme="dark"><body>
                              <div class="chat-container" style="height: 700px">
                                <div id="chat-messages" class="chat-messages"></div>
                                <button id="chat-return-latest" hidden></button>
                              </div></body></html>`
                        })
                        : route.abort());
                    await page.goto('http://chat.test/');
                    for (const file of ['style.css', 'mobile.css']) {
                        await page.addStyleTag({ content: read('css/' + file) });
                    }
                    // Supply only the chat globals required by the real progress renderer.
                    await page.addScriptTag({ content: `
                        let messageCounter = 0, currentConversationId = '';
                        const escapeHtml = text => escapeHtmlLocal(text);
                    ` });
                    for (const file of ['chat-scroll.js', 'monitor.js']) {
                        await page.addScriptTag({ content: read('js/' + file) });
                    }
                    await page.evaluate(async restored => {
                        const chat = document.getElementById('chat-messages');
                        for (let i = 0; i < 30; i++) {
                            const row = document.createElement('div');
                            row.className = 'message user';
                            row.innerHTML = '<div class="message-bubble">Synthetic history</div>';
                            chat.append(row);
                        }
                        if (restored) {
                            chat.insertAdjacentHTML('beforeend', `
                              <div class="message assistant assistant-turn-with-process">
                                <div class="message-content">
                                  <div id="process-details-restored" class="process-details-container is-streaming">
                                    <div class="process-details-content"><div class="progress-timeline expanded"></div></div>
                                  </div>
                                </div>
                              </div>`);
                            startProcessDetailsLatestFollow('restored');
                        } else {
                            addProgressMessage();
                        }
                        const timeline = document.querySelector('.progress-timeline');
                        for (let i = 0; i < (restored ? 50 : 156); i++) {
                            addTimelineItem(timeline, 'reasoning_chain', {
                                title: 'Synthetic reasoning ' + i,
                                message: 'Example reasoning text.\n'.repeat(i % 11 === 0 ? 65 : i % 9 + 1)
                            });
                            if (i % 10 === 0) await new Promise(requestAnimationFrame);
                        }
                    }, restored);
                    await page.waitForFunction(() => {
                        const tl = document.querySelector('.progress-timeline');
                        const chat = document.getElementById('chat-messages');
                        return tl.scrollHeight - tl.clientHeight - tl.scrollTop <= 2 &&
                            chat.scrollHeight - chat.clientHeight - chat.scrollTop <= 2;
                    }, null, { timeout: 2000 });
                    const latest = await scrollState(page);
                    assert.equal(latest.pruned, !restored, label);

                    const box = await page.locator('.progress-timeline').boundingBox();
                    await page.mouse.move(box.x + 40, box.y + box.height / 2);
                    await page.mouse.wheel(0, -420);
                    await page.waitForFunction(() => processDetailsDistanceFromLatest(
                        document.querySelector('.progress-timeline')) > 100);
                    const reading = await scrollState(page);
                    assert.equal(reading.outerFollowing, true, label);
                    assert.ok(reading.outerGap <= 2, label);

                    if (restored) {
                        // Upgrading the restore follower to the live subscription preserves reading position.
                        await page.evaluate(() => startProcessDetailsLatestFollow('restored', { persistent: true }));
                    }
                    await page.evaluate(async () => {
                        const row = document.querySelector('.progress-timeline .timeline-item:last-child .timeline-item-content');
                        scheduleStreamPlainTextUpdate(row, row.textContent + 'A new line.\n'.repeat(20));
                        await new Promise(requestAnimationFrame);
                        await new Promise(requestAnimationFrame);
                        await new Promise(requestAnimationFrame);
                    });
                    const afterUpdate = await scrollState(page);
                    assert.equal(afterUpdate.innerTop, reading.innerTop, label);
                    assert.ok(afterUpdate.innerGap > reading.innerGap, label);

                    if (restored) {
                        await page.locator('.process-details-return-latest').click();
                    } else {
                        await page.mouse.wheel(0, 100000);
                    }
                    await page.waitForFunction(() => processDetailsDistanceFromLatest(
                        document.querySelector('.progress-timeline')) <= 2);
                    await page.evaluate(async () => {
                        const timeline = document.querySelector('.progress-timeline');
                        addTimelineItem(timeline, 'reasoning_chain', {
                            title: 'Latest iteration', message: 'More output.\n'.repeat(50)
                        });
                        await new Promise(requestAnimationFrame);
                        await new Promise(requestAnimationFrame);
                        await new Promise(requestAnimationFrame);
                    });
                    const atBottom = await scrollState(page);
                    assert.ok(atBottom.innerGap <= 2 && atBottom.outerGap <= 2, label);
                } finally {
                    await page.close();
                }
            }
        }
    } finally {
        await browser.close();
    }
});
