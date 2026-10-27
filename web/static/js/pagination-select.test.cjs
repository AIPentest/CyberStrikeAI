const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function setup() {
    const source = fs.readFileSync(path.join(__dirname, 'settings.js'), 'utf8');
    const context = vm.createContext({ window: { innerWidth: 800, innerHeight: 600 } });
    vm.runInContext(source.slice(0, source.indexOf('function getRobotStatus(')), context);
    vm.runInContext('globalThis.selects = settingsCustomSelects;', context);
    return context;
}

function menuAt(rect) {
    return {
        wrapper: { classList: { contains: () => true } },
        trigger: { getBoundingClientRect: () => rect },
        menu: { style: {} },
    };
}

test('a pagination menu near the viewport bottom opens above its trigger', () => {
    const context = setup();
    const reg = menuAt({ left: 700, top: 550, bottom: 584, width: 76 });
    context.positionSettingsCustomSelectMenu(reg);
    assert.equal(reg.menu.style.top, 'auto');
    assert.equal(reg.menu.style.bottom, '56px');
    assert.ok(parseInt(reg.menu.style.left) + parseInt(reg.menu.style.width) <= 788);
});

test('a menu near the viewport top opens below instead', () => {
    const context = setup();
    const reg = menuAt({ left: 20, top: 20, bottom: 54, width: 76 });
    context.positionSettingsCustomSelectMenu(reg);
    assert.equal(reg.menu.style.top, '60px');
    assert.equal(reg.menu.style.bottom, 'auto');
});

test('replacing pagination DOM removes its detached floating menu', () => {
    const context = setup();
    let removed = false;
    let synced = null;
    const stale = { isConnected: false };
    const current = { isConnected: true };
    const reg = {
        wrapper: { classList: { remove() {} }, appendChild(menu) { menu.parentNode = this; } },
        trigger: { setAttribute() {} },
        menu: { style: {}, classList: { remove() {} }, remove() { removed = true; } },
    };
    context.selects.set(stale, reg);
    context.selects.set(current, {});
    context.syncSettingsCustomSelect = select => { synced = select; };
    context.refreshSettingsCustomSelects();
    assert.equal(removed, true);
    assert.equal(context.selects.has(stale), false);
    assert.equal(context.selects.has(current), true);
    assert.equal(synced, current);
});
