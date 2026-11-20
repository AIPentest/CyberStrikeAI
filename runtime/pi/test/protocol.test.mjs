import assert from "node:assert/strict";
import { test } from "node:test";
import { spawn } from "node:child_process";
import { once } from "node:events";

test("the worker flushes a fatal record for a truncated input at EOF", { timeout: 10000 }, async t => {
  const child = spawn(process.execPath, ["dist/worker.js"], { stdio: ["pipe", "pipe", "pipe"] });
  t.after(() => child.kill());
  const closed = once(child, "close");
  const chunks = [];
  child.stdout.on("data", chunk => chunks.push(chunk));
  await once(child.stdout, "data");
  child.stdin.end('{"type":');
  const [code] = await closed;
  const output = Buffer.concat(chunks).toString();
  assert.equal(code, 1);
  assert.ok(output.endsWith("\n"));
  const records = output.trimEnd().split("\n").map(line => JSON.parse(line));
  assert.deepEqual(records.at(-1), { type: "fatal", error: "Truncated protocol record" });
});

test("send waits for the write callback even below the stream high water mark", async () => {
  const child = spawn(process.execPath, ["--input-type=module", "-e", `
    import { send } from './dist/protocol.js';
    const pending = [];
    process.stdout.write = (line, callback) => { pending.push({line, callback}); return true; };
    let settled = false;
    const first = send({type:'event',text:'中文'.repeat(10000)}).then(() => { settled = true; });
    const second = send({type:'result',text:'last'});
    await new Promise(resolve => setImmediate(resolve));
    if (settled || pending.length !== 1 || typeof pending[0].callback !== 'function') process.exit(2);
    pending[0].callback();
    await first;
    await new Promise(resolve => setImmediate(resolve));
    if (pending.length !== 2 || !pending[1].line.endsWith('\\n')) process.exit(3);
    pending[1].callback();
    await second;
    process.exit(0);
  `], { stdio: ["ignore", "pipe", "pipe"] });
  let stderr = "";
  child.stderr.on("data", chunk => stderr += chunk);
  const [code] = await once(child, "close");
  assert.equal(code, 0, stderr || "send resolved before buffered output was flushed");
});

test("a slow reader receives the complete final JSON before an immediate exit", async () => {
  const child = spawn(process.execPath, ["--input-type=module", "-e", `
    import { send } from './dist/protocol.js';
    process.stdout._writableState.highWaterMark = 8 * 1024 * 1024;
    await send({type:'result',text:'x'.repeat(4 * 1024 * 1024)});
    process.exit(0);
  `], { stdio: ["ignore", "pipe", "pipe"] });
  let stderr = "";
  child.stderr.on("data", chunk => stderr += chunk);
  const chunks = [];
  child.stdout.on("data", chunk => chunks.push(chunk));
  child.stdout.pause();
  const resume = setTimeout(() => child.stdout.resume(), 100);
  const [code] = await once(child, "close");
  clearTimeout(resume);
  assert.equal(code, 0, stderr);
  const body = Buffer.concat(chunks).toString();
  assert.ok(body.endsWith("\n"), "final JSON record was truncated");
  assert.equal(JSON.parse(body).text.length, 4 * 1024 * 1024);
});
