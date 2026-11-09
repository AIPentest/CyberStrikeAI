import fs from 'fs';
import path from 'path';

const MAP = {
  '已请求终止当前工具调用；工具返回后本轮推理将继续（与 MCP 监控页终止一致）。': 'termination of the current tool call requested; reasoning continues in this turn once the tool returns (same as terminating from the MCP monitoring page).',
  '已请求终止当前 execute 命令；命令返回后本轮推理将继续。': 'termination of the current execute command requested; reasoning continues in this turn once the command returns.',
  '已请求暂停当前推理；用户补充将合并到上下中并自动继续执行（无需整轮停止）。': 'placeholder',
  '已请求暂停当前推理；用户补充将合并到上下文并自动继续执行（无需整轮停止）。': 'current reasoning paused; user input will be merged into the context and execution continues automatically (no need to stop the whole turn).',
  '配置已应用': 'config applied',
  '请使用微信扫描二维码并确认绑定': 'scan the QR code with WeChat and confirm binding',
  '该微信已绑定过，无需重复绑定': 'this WeChat account was already bound; no need to bind again',
  '绑定成功，微信机器人已启用': 'binding successful, WeChat robot enabled',
};
for (const zh of Object.keys(MAP)) {
  if (MAP[zh] === 'placeholder') continue;
}

let transformed = 0;
const dir = process.argv[2] || '.';
for (const f of fs.readdirSync(path.join(dir, 'internal/handler')).filter(x => x.endsWith('.go') && !x.includes('test') && x !== 'i18n_error.go')) {
  const p = path.join(dir, 'internal/handler', f);
  let s = fs.readFileSync(p, 'utf8');
  // aligned: "message":<spaces>"<zh>",  -> "message":<spaces>i18nText(...)
  const re = /"message":(\s+)"((?:[^"\\]|\\.)*)"/g;
  s = s.replace(re, (m, sp, zh) => {
    if (!MAP[zh]) return m;
    transformed++;
    console.log(`${f}: ${zh.slice(0, 30)}`);
    return `"message":${sp}i18nText(requestWantsEnglish(c), "${zh}", "${MAP[zh]}")`;
  });
  fs.writeFileSync(p, s);
}
console.log('transformed', transformed);
