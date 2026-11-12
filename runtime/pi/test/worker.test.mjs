import assert from "node:assert/strict";
import { test } from "node:test";
import { createServer } from "node:http";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { RecordReader } from "../dist/protocol.js";

test("JSONL preserves Unicode separators and fragmented UTF-8", () => {
  const body = Buffer.from(JSON.stringify({ text: "中文\u2028\u2029" }) + "\n");
  const reader = new RecordReader();
  const values = [];
  for (const byte of body) values.push(...reader.push(Buffer.from([byte])));
  reader.finish();
  assert.deepEqual(values, [{ text: "中文\u2028\u2029" }]);
});

async function startFixture(t, reply) {
  const requests = [];
  const server = createServer(async (req, res) => {
    const chunks = [];
    for await (const c of req) chunks.push(c);
    const body = JSON.parse(Buffer.concat(chunks));
    requests.push(body);
    await reply(body, res, requests.length);
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  t.after(() => { server.closeAllConnections(); server.close(); });
  return { requests, url: "http://127.0.0.1:" + server.address().port + "/v1" };
}

function sse(res, delta, reason) {
  res.writeHead(200, { "Content-Type": "text/event-stream" });
  const chunk = (d, finish_reason) => ({ id: "test", object: "chat.completion.chunk", model: "fixture", choices: [{ index: 0, delta: d, finish_reason }] });
  res.write("data: " + JSON.stringify(chunk(delta, null)) + "\n\n");
  res.write("data: " + JSON.stringify(chunk({}, reason)) + "\n\n");
  res.end("data: [DONE]\n\n");
}

async function invoke(t, request, onRecord = () => {}) {
  const worker = spawn(process.execPath, ["dist/worker.js"], { stdio: ["pipe", "pipe", "pipe"] });
  t.after(() => { worker.kill(); });
  const exit = once(worker, "exit");
  const records = [];
  let stderr = "";
  worker.stderr.on("data", c => stderr += c);
  const reader = new RecordReader();
  const send = record => worker.stdin.write(JSON.stringify(record) + "\n");
  worker.stdout.on("data", chunk => {
    for (const r of reader.push(chunk)) {
      records.push(r);
      if (r.type === "ready") send({ type: "run", version: 1, request });
      onRecord(r, send);
    }
  });
  const [code] = await exit;
  assert.equal(code, 0, stderr + JSON.stringify(records));
  const result = records.find(r => r.type === "result")?.result;
  assert.ok(result, stderr + JSON.stringify(records));
  return { records, result };
}

function request(url, sessionDir) {
  return { runId: "run-1", conversationId: "conversation-1", systemPrompt: "Use the provided echo tool then answer.", message: "echo hello", history: [],
    model: { provider: "openai", id: "fixture", baseUrl: url, apiKey: "test-key", contextWindow: 32000, maxTokens: 1024, thinking: "off" },
    tools: [{ name: "echo", description: "Return text", parameters: { type: "object", properties: { text: { type: "string" } }, required: ["text"] } }],
    maxTurns: 8, sessionDir };
}

test("real Pi SDK streams, invokes only host tools and restores persisted sessions", { timeout: 30000 }, async t => {
  const fixture = await startFixture(t, (body, res, count) => {
    if (count === 1) sse(res, { role: "assistant", tool_calls: [{ index: 0, id: "call-1", type: "function", function: { name: "echo", arguments: '{"text":"hello"}' } }] }, "tool_calls");
    else sse(res, { role: "assistant", content: "verified hello" }, "stop");
  });
  const dir = await mkdtemp(join(tmpdir(), "csai-pi-test-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  let calls = 0;
  const req = request(fixture.url, dir);
  const first = await invoke(t, { ...req, profile: { enabled: true, instruction: "Retired guidance" } }, (r, send) => {
    if (r.type === "tool_call") { calls++; send({ type: "tool_result", id: r.call.id, result: { content: "hello", executionId: "exec-1" } }); }
  });
  assert.equal(first.result.status, "completed", first.result.error);
  assert.equal(first.result.response, "verified hello");
  assert.equal(calls, 1);
  assert.equal(fixture.requests.length, 2);
  const systemPrompt = fixture.requests[0].messages.filter(m => m.role === "system").map(m => m.content).join("\n");
  assert.ok(systemPrompt.startsWith(req.systemPrompt));
  assert.doesNotMatch(systemPrompt, /CyberStrikeAI execution protocol|Retired guidance/);
  assert.deepEqual(fixture.requests[0].tools.map(t => t.function.name), ["echo"]);
  assert.ok(fixture.requests[1].messages.some(m => m.role === "tool" && m.content === "hello"));
  assert.ok(first.records.some(r => r.event?.type === "message_update"));
  assert.ok(first.records.filter(r => r.event?.type === "message_update").every(r => !Object.hasOwn(r.event.data.assistantMessageEvent, "partial")));
  const second = await invoke(t, { ...request(fixture.url, dir), runId: "run-2", message: "summarize prior result" });
  assert.equal(second.result.status, "completed", second.result.error);
  assert.ok(fixture.requests[2].messages.some(m => m.role === "tool" && m.content === "hello"));
});

test("cancel rejects a pending host tool and settles without another model call", { timeout: 30000 }, async t => {
  const fixture = await startFixture(t, (_, res) => sse(res, { role: "assistant", tool_calls: [{ index: 0, id: "call-wait", type: "function", function: { name: "echo", arguments: '{"text":"wait"}' } }] }, "tool_calls"));
  const { result } = await invoke(t, request(fixture.url), (r, send) => {
    if (r.type === "tool_call") send({ type: "cancel" });
  });
  assert.equal(result.status, "canceled");
  assert.equal(fixture.requests.length, 1);
});

test("native virtual model routes a transient retry to the configured fallback", { timeout: 30000 }, async t => {
  const primary = await startFixture(t, (_, res) => {
    res.writeHead(503, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: { message: "server overloaded" } }));
  });
  const backup = await startFixture(t, (_, res) => sse(res, { role: "assistant", content: "backup verified" }, "stop"));
  const req = request(primary.url);
  const { result, records } = await invoke(t, { ...req, fallbackModels: [{ ...req.model, id: "backup", baseUrl: backup.url }] });
  assert.equal(result.status, "completed", result.error);
  assert.equal(result.response, "backup verified");
  assert.equal(backup.requests.length, 1);
  assert.ok(records.some(r => r.event?.type === "model_failover"));
});

test("process loss preserves the session and marks uncertain tools without replaying them", { timeout: 30000 }, async t => {
  const fixture = await startFixture(t, (_, res, count) => {
    if (count === 1) sse(res, { role: "assistant", tool_calls: [{ index: 0, id: "uncertain", type: "function", function: { name: "echo", arguments: '{"text":"unknown"}' } }] }, "tool_calls");
    else sse(res, { role: "assistant", content: "recovered" }, "stop");
  });
  const dir = await mkdtemp(join(tmpdir(), "csai-pi-crash-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const worker = spawn(process.execPath, ["dist/worker.js"], { stdio: ["pipe", "pipe", "ignore"] });
  t.after(() => worker.kill());
  const ended = once(worker, "exit");
  const reader = new RecordReader();
  worker.stdout.on("data", chunk => {
    for (const r of reader.push(chunk)) {
      if (r.type === "ready") worker.stdin.write(JSON.stringify({ type: "run", version: 1, request: request(fixture.url, dir) }) + "\n");
      if (r.type === "tool_call") worker.kill("SIGKILL");
    }
  });
  await ended;
  const { result } = await invoke(t, { ...request(fixture.url, dir), runId: "after-crash", message: "Inspect the prior result before continuing" });
  assert.equal(result.status, "completed", result.error);
  assert.equal(fixture.requests.length, 2);
  assert.ok(fixture.requests[1].messages.some(m => m.role === "tool" && /effects are unknown/.test(m.content)));
});

test("native tool search loads a deferred host tool without exposing the full catalog", { timeout: 30000 }, async t => {
  const fixture = await startFixture(t, (body, res, count) => {
    const names = body.tools.map(t => t.function.name);
    if (count === 1) {
      assert.deepEqual(names, ["tool_search"]);
      sse(res, { role: "assistant", tool_calls: [{ index: 0, id: "search-1", type: "function", function: { name: "tool_search", arguments: '{"query":"echo","limit":1}' } }] }, "tool_calls");
    } else if (count === 2) {
      assert.ok(names.includes("echo"), JSON.stringify(body.messages));
      sse(res, { role: "assistant", tool_calls: [{ index: 0, id: "echo-1", type: "function", function: { name: "echo", arguments: '{"text":"found"}' } }] }, "tool_calls");
    } else sse(res, { role: "assistant", content: "discovery verified" }, "stop");
  });
  const req = request(fixture.url);
  req.tools.push(...Array.from({ length: 45 }, (_, i) => ({ name: "unrelated_" + i, description: "Unrelated operation", parameters: { type: "object", properties: {} } })));
  let hostCalls = 0;
  const { result } = await invoke(t, { ...req, toolSearch: true }, (r, send) => {
    if (r.type === "tool_call") { hostCalls++; assert.equal(r.call.name, "echo"); send({ type: "tool_result", id: r.call.id, result: { content: "found" } }); }
  });
  assert.equal(result.status, "completed", result.error);
  assert.equal(result.response, "discovery verified");
  assert.equal(hostCalls, 1);
});
