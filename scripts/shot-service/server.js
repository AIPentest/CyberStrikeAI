'use strict';
/**
 * CyberStrikeAI 站点截图渲染服务
 *
 * 独立进程、以非 root 用户运行，只负责「给一个 URL，渲染出 PNG」。
 * 主服务通过 localhost HTTP 调用，避免以 root 身份直接渲染不可信站点。
 *
 * 接口：
 *   GET  /health            -> {ok:true}
 *   POST /shot              -> {ok,path,status_code,title,width,height,error}
 *        body: {url, path, timeout_ms, width, height}
 */

const http = require('http');
const fs = require('fs');
const path = require('path');
const { chromium } = require('playwright');

const HOST = process.env.SHOT_HOST || '127.0.0.1';
const PORT = Number(process.env.SHOT_PORT || 8099);
const MAX_CONCURRENCY = Number(process.env.SHOT_MAX_CONCURRENCY || 3);
const DEFAULT_TIMEOUT = Number(process.env.SHOT_TIMEOUT_MS || 30000);

let browserPromise = null;
let active = 0;
const waiters = [];

function log(...args) {
  console.log(new Date().toISOString(), ...args);
}

async function launchBrowser() {
  const baseArgs = [
    '--disable-dev-shm-usage',
    '--disable-gpu',
    '--ignore-certificate-errors',
    '--disable-background-networking',
    '--disable-features=Translate,MediaRouter',
    '--no-first-run',
  ];
  // 先按 Chromium 默认沙箱启动；宿主机若禁用非特权 user namespace，
  // 会抛错，此时才降级为 --no-sandbox（进程本身已是非 root 用户，风险可控）。
  try {
    return await chromium.launch({ headless: true, args: baseArgs });
  } catch (err) {
    log('沙箱模式启动失败，降级 --no-sandbox:', err.message);
    return await chromium.launch({ headless: true, args: baseArgs.concat(['--no-sandbox']) });
  }
}

async function getBrowser() {
  if (!browserPromise) {
    browserPromise = launchBrowser().catch((err) => {
      browserPromise = null;
      throw err;
    });
  }
  const browser = await browserPromise;
  if (!browser.isConnected()) {
    browserPromise = null;
    return getBrowser();
  }
  return browser;
}

function acquire() {
  if (active < MAX_CONCURRENCY) {
    active += 1;
    return Promise.resolve();
  }
  return new Promise((resolve) => waiters.push(resolve));
}

function release() {
  const next = waiters.shift();
  if (next) {
    next();
    return;
  }
  active = Math.max(0, active - 1);
}

function validateTarget(rawUrl) {
  let parsed;
  try {
    parsed = new URL(rawUrl);
  } catch (err) {
    throw new Error('URL 解析失败: ' + err.message);
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    throw new Error('仅支持 http/https，收到: ' + parsed.protocol);
  }
  if (!parsed.hostname) {
    throw new Error('URL 缺少主机名');
  }
  return parsed.toString();
}

async function shoot(req) {
  const url = validateTarget(String(req.url || ''));
  const outPath = String(req.path || '');
  if (!outPath || !path.isAbsolute(outPath)) {
    throw new Error('path 必须是绝对路径');
  }
  const timeoutMs = Math.min(Number(req.timeout_ms) || DEFAULT_TIMEOUT, 120000);
  const width = Number(req.width) || 1280;
  const height = Number(req.height) || 800;

  await acquire();
  let context = null;
  try {
    const browser = await getBrowser();
    context = await browser.newContext({
      viewport: { width, height },
      ignoreHTTPSErrors: true,
      userAgent:
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36 CyberStrikeAI-Screenshot',
    });
    const page = await context.newPage();
    page.setDefaultTimeout(timeoutMs);

    let mainStatus = 0;
    page.on('response', (response) => {
      if (!mainStatus && response.request().isNavigationRequest()) {
        mainStatus = response.status();
      }
    });

    let navError = null;
    try {
      await page.goto(url, { waitUntil: 'domcontentloaded', timeout: timeoutMs });
    } catch (err) {
      // 页面超时/证书错误等：已经渲染出来的内容仍然值得截一张，记录错误后继续。
      navError = err.message;
    }
    // 给前端 JS/字体/图片一点渲染时间；等待网络空闲失败也不影响截图。
    try {
      await page.waitForLoadState('networkidle', { timeout: Math.min(5000, timeoutMs) });
    } catch (err) {
      /* ignore */
    }
    await page.waitForTimeout(800);

    fs.mkdirSync(path.dirname(outPath), { recursive: true });
    await page.screenshot({ path: outPath, type: 'png', fullPage: false });

    let title = '';
    try {
      title = (await page.title()) || '';
    } catch (err) {
      /* ignore */
    }
    const stat = fs.statSync(outPath);
    if (!stat.size) {
      throw new Error('截图文件为空');
    }
    return {
      ok: true,
      path: outPath,
      status_code: mainStatus,
      title: title.slice(0, 300),
      width,
      height,
      error: navError ? '页面加载有告警: ' + navError.slice(0, 300) : '',
    };
  } finally {
    if (context) {
      try {
        await context.close();
      } catch (err) {
        /* ignore */
      }
    }
    release();
  }
}

const server = http.createServer((req, res) => {
  const send = (code, obj) => {
    const body = JSON.stringify(obj);
    res.writeHead(code, {
      'Content-Type': 'application/json; charset=utf-8',
      'Content-Length': Buffer.byteLength(body),
    });
    res.end(body);
  };

  if (req.method === 'GET' && req.url === '/health') {
    send(200, { ok: true, active, max: MAX_CONCURRENCY });
    return;
  }
  if (req.method !== 'POST' || req.url !== '/shot') {
    send(404, { ok: false, error: 'not found' });
    return;
  }

  let body = '';
  let tooLarge = false;
  req.on('data', (chunk) => {
    body += chunk;
    if (body.length > 64 * 1024) {
      tooLarge = true;
      req.destroy();
    }
  });
  req.on('end', async () => {
    if (tooLarge) {
      send(413, { ok: false, error: '请求体过大' });
      return;
    }
    let payload;
    try {
      payload = JSON.parse(body || '{}');
    } catch (err) {
      send(400, { ok: false, error: 'JSON 解析失败' });
      return;
    }
    const startedAt = Date.now();
    try {
      const result = await shoot(payload);
      log('shot ok', payload.url, 'took', Date.now() - startedAt, 'ms', result.status_code);
      send(200, result);
    } catch (err) {
      log('shot failed', payload && payload.url, err.message);
      send(500, { ok: false, error: err.message });
    }
  });
});

server.listen(PORT, HOST, () => {
  log(`screenshot service listening on http://${HOST}:${PORT}`);
});

process.on('SIGTERM', async () => {
  log('SIGTERM, shutting down');
  server.close();
  try {
    const browser = await browserPromise;
    if (browser) await browser.close();
  } catch (err) {
    /* ignore */
  }
  process.exit(0);
});
