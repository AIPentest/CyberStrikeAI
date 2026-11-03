// Run: node --test plugins/browser-extension/cyberstrikeai-browser-extension/tests/auth.test.cjs
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const http = require('node:http');
const path = require('node:path');
const root = path.join(__dirname, '..');

test('RBAC login sends username and exact password, validates token, and reports failures', async () => {
  const requests = [];
  const server = http.createServer(async (req, res) => {
    let body = '';
    for await (const chunk of req) body += chunk;
    const data = body ? JSON.parse(body) : null;
    requests.push({ url: req.url, data, authorization: req.headers.authorization });
    res.setHeader('Content-Type', 'application/json');
    if (req.url === '/api/auth/login') {
      if (!data.username || data.password === 'wrong') {
        res.writeHead(401).end(JSON.stringify({ error: 'invalid credentials' }));
      } else res.end(JSON.stringify({ token: 'test-token', expires_at: '2099-01-01T00:00:00Z' }));
    } else res.end('{}');
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    const ctx = vm.createContext({ fetch });
    vm.runInContext(fs.readFileSync(path.join(root, 'lib/api.js'), 'utf8'), ctx);
    const base = `http://127.0.0.1:${server.address().port}`;
    for (const username of ['admin', 'analyst', '研究员"\\']) {
      const password = '  p"\\\n密码  ';
      const result = await ctx.loginAndValidate(base, ` ${username} `, password);
      assert.equal(result.token, 'test-token');
      assert.deepEqual(requests.at(-2).data, { username, password });
      assert.equal(requests.at(-1).authorization, 'Bearer test-token');
    }
    const before = requests.length;
    for (const username of ['', '  ', '𠮷'.repeat(65)]) {
      await assert.rejects(ctx.loginAndValidate(base, username, 'x'), /Username is required/);
    }
    assert.equal(requests.length, before, 'invalid usernames never reach server');
    await assert.rejects(ctx.loginAndValidate(base, 'admin', 'wrong'), /invalid credentials/);
    assert.equal(requests.length, before + 1, 'failed login never validates a token');
  } finally { await new Promise(resolve => server.close(resolve)); }
});

test('existing settings default to admin; custom username persists without password', async () => {
  const local = {}, session = {};
  const store = data => ({ get: (_, cb) => cb({ ...data }), set: (obj, cb) => { Object.assign(data, obj); cb(); } });
  const ctx = vm.createContext({
    CSAI_DEFAULT_INSTRUCTION: '', SESSION_TOKEN_EXPIRY_KEY: 'expiry',
    chrome: { runtime: { id: 'test' }, storage: { local: store(local), session: store(session) } },
  });
  vm.runInContext(fs.readFileSync(path.join(root, 'lib/storage.js'), 'utf8'), ctx);
  assert.equal((await ctx.loadConfig()).username, 'admin');
  await ctx.saveConfig({ username: 'analyst', password: 'do-not-store', token: 'session' });
  assert.equal((await ctx.loadConfig()).username, 'analyst');
  assert.equal(JSON.stringify({ local, session }).includes('do-not-store'), false);
  await ctx.saveConfig({ token: '', tokenExpiresAt: '' });
  assert.equal((await ctx.loadConfig()).token, '');
});
