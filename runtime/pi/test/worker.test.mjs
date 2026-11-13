import assert from "node:assert/strict";
import { test } from "node:test";
import { createServer } from "node:http";
import { connect } from "node:net";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, rm, readFile } from "node:fs/promises";
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

const fixtureEnv = { ...process.env, NO_PROXY: "127.0.0.1", no_proxy: "127.0.0.1" };

async function invoke(t, request, onRecord = () => {}, env = fixtureEnv) {
  const worker = spawn(process.execPath, ["dist/worker.js"], { stdio: ["pipe", "pipe", "pipe"], env });
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

test("OpenAI-compatible requests keep historical tool results in their own assistant batch", { timeout: 30000 }, async t => {
  const fixture = await startFixture(t, (body, res) => {
    let pending = new Set();
    for (const message of body.messages) {
      if (message.role === "tool" ? !pending.delete(message.tool_call_id) : pending.size > 0) {
        res.writeHead(400, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: { message: "Invalid tool result sequence" } }));
        return;
      }
      if (message.role === "assistant") pending = new Set(message.tool_calls?.map(call => call.id));
    }
    sse(res, { role: "assistant", content: "verified history" }, "stop");
  });
  const call = id => ({ id, function: { name: "echo", arguments: "{}" } });
  const result = (id, content) => ({ role: "tool", tool_call_id: id, tool_name: "echo", content });
  const req = request(fixture.url);
  req.generateOnly = true;
  req.history = [
    { role: "user", content: "Use echo" },
    { role: "assistant", tool_calls: [call("reused"), call("second")] },
    result("reused", "first result"),
    { role: "user", content: "An interruption" },
    result("second", "second result"),
    result("second", "duplicate result"),
    { role: "assistant", content: "Continuing" },
    result("reused", "orphaned result"),
    { role: "assistant", tool_calls: [call("reused")] },
    result("reused", "new batch result"),
    { role: "user", content: "Summarize" },
  ];
  const run = await invoke(t, req);
  assert.equal(run.result.status, "completed", run.result.error);
  assert.equal(run.result.response, "verified history");
  assert.deepEqual(fixture.requests[0].messages.filter(m => m.role === "tool" || m.content === "An interruption").map(m => m.content),
    ["first result", "second result", "An interruption", "new batch result"]);

  const dir = await mkdtemp(join(tmpdir(), "csai-pi-history-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const seeded = await invoke(t, { ...req, generateOnly: false, sessionDir: dir });
  assert.equal(seeded.result.status, "completed", seeded.result.error);
  const resumed = await invoke(t, { ...request(fixture.url, dir), runId: "resume-history", message: "Continue" });
  assert.equal(resumed.result.status, "completed", resumed.result.error);
  assert.deepEqual(fixture.requests[2].messages.filter(m => m.role === "tool" || m.content === "An interruption").map(m => m.content),
    ["first result", "second result", "An interruption", "new batch result"]);
});

test("model transport honors HTTP_PROXY and NO_PROXY", { timeout: 30000 }, async t => {
  const fixture = await startFixture(t, (_, res) => sse(res, { role: "assistant", content: "transport verified" }, "stop"));
  const target = new URL(fixture.url);
  const tunnels = [];
  const sockets = new Set();
  const proxy = createServer();
  proxy.on("connect", (req, socket, head) => {
    tunnels.push(req.url);
    assert.equal(req.url, target.host);
    const upstream = connect(Number(target.port), target.hostname, () => {
      socket.write("HTTP/1.1 200 Connection Established\r\n\r\n");
      if (head.length) upstream.write(head);
      socket.pipe(upstream).pipe(socket);
    });
    sockets.add(socket);
    socket.on("close", () => { sockets.delete(socket); upstream.destroy(); });
    socket.on("error", () => upstream.destroy());
    upstream.on("close", () => socket.destroy());
    upstream.on("error", () => socket.destroy());
  });
  proxy.listen(0, "127.0.0.1");
  await once(proxy, "listening");
  t.after(() => { for (const socket of sockets) socket.destroy(); proxy.close(); });
  const proxyURL = "http://127.0.0.1:" + proxy.address().port;
  const env = { ...fixtureEnv, HTTP_PROXY: proxyURL, http_proxy: proxyURL, NO_PROXY: "", no_proxy: "", NODE_USE_ENV_PROXY: "0" };
  const req = request(fixture.url);
  const throughProxy = await invoke(t, { ...req, generateOnly: true, history: [{ role: "user", content: req.message }] }, undefined, env);
  assert.equal(throughProxy.result.response, "transport verified", throughProxy.result.error);
  assert.deepEqual(tunnels, [target.host]);
  const direct = await invoke(t, req, undefined, { ...env, NO_PROXY: target.hostname, no_proxy: target.hostname });
  assert.equal(direct.result.response, "transport verified", direct.result.error);
  assert.equal(fixture.requests.length, 2);
  assert.deepEqual(tunnels, [target.host]);
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
  const worker = spawn(process.execPath, ["dist/worker.js"], { stdio: ["pipe", "pipe", "ignore"], env: fixtureEnv });
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

test("economy compacts persisted tool JSON while preserving reasoning and original session evidence", { timeout: 30000 }, async t => {
  const raw = '{\n  "id": 9007199254740993,\n  "evidence": "keep  spaces\\n"\n}';
  const fixture = await startFixture(t, (_, res, count) => {
    if (count === 1) sse(res, { role: "assistant", reasoning_content: "inspect fixture", tool_calls: [{ index: 0, id: "inspect", type: "function", function: { name: "echo", arguments: '{"text":"fixture"}' } }] }, "tool_calls");
    else sse(res, { role: "assistant", content: "evidence inspected" }, "stop");
  });
  const dir = await mkdtemp(join(tmpdir(), "csai-pi-economy-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const req = request(fixture.url, dir);
  req.tools.push(...Array.from({ length: 30 }, (_, i) => ({ name: "old_unused_" + i, description: "Unused fixture operation", parameters: { type: "object", properties: {} } })));
  req.model.thinking = "high";
  req.model.compat = { thinkingFormat: "deepseek", requiresReasoningContentOnAssistantMessages: true };
  req.model.extraFields = { thinking: { type: "enabled" }, reasoning_effort: "high" };
  const first = await invoke(t, req, (r, send) => {
    if (r.type === "tool_call") send({ type: "tool_result", id: r.call.id, result: { content: raw } });
  });
  assert.equal(first.result.status, "completed", first.result.error);
  assert.equal(fixture.requests[1].messages.find(m => m.role === "tool").content, raw);

  req.model.compactToolJSON = true;
  const resumed = await invoke(t, { ...req, toolSearch: true, alwaysVisibleTools: ["echo"], runId: "economy-resume", message: "Continue" });
  assert.equal(resumed.result.status, "completed", resumed.result.error);
  const body = fixture.requests[2];
  assert.equal(fixture.requests[0].tools.length, 31);
  assert.deepEqual(body.tools.map(t => t.function.name).sort(), ["echo", "tool_search"]);
  assert.equal(body.messages.find(m => m.role === "tool").content, '{"id":9007199254740993,"evidence":"keep  spaces\\n"}');
  assert.ok(body.messages.some(m => m.reasoning_content === "inspect fixture"));
  assert.equal(body.reasoning_effort, "high");
  assert.equal(resumed.result.history.find(m => m.role === "tool").content, raw);
  const entries = (await readFile(first.result.sessionFile, "utf8")).trim().split("\n").map(line => JSON.parse(line));
  assert.ok(entries.some(e => e.message?.role === "toolResult" && e.message.content.some(b => b.text === raw)));
});

test("provider custom credentials never cross a redirect origin", { timeout: 30000 }, async t => {
  let targetCalls = 0;
  const target = createServer((_, res) => { targetCalls++; res.end("unexpected"); });
  target.listen(0, "127.0.0.1"); await once(target, "listening");
  t.after(() => { target.closeAllConnections(); target.close(); });
  const fixture = await startFixture(t, (_, res) => {
    res.writeHead(307, { location: "http://127.0.0.1:" + target.address().port + "/capture" }); res.end();
  });
  const req = request(fixture.url);
  req.model.headers = { "X-Private-Key": "fixture-private-value" };
  const { result } = await invoke(t, req);
  assert.equal(result.status, "failed");
  assert.match(result.error, /cross-origin redirect blocked/);
  assert.equal(targetCalls, 0);
});

test("same-origin model redirects retain method, payload and configured headers", { timeout: 30000 }, async t => {
  let finalCalls = 0;
  const server = createServer(async (req, res) => {
    if (req.url !== "/final") { res.writeHead(307, { location: "/final" }); res.end(); return; }
    finalCalls++;
    const chunks = []; for await (const chunk of req) chunks.push(chunk);
    assert.equal(req.method, "POST");
    assert.equal(req.headers["x-private-key"], "fixture-private-value");
    assert.equal(JSON.parse(Buffer.concat(chunks)).model, "fixture");
    sse(res, { role: "assistant", content: "redirect verified" }, "stop");
  });
  server.listen(0, "127.0.0.1"); await once(server, "listening");
  t.after(() => { server.closeAllConnections(); server.close(); });
  const req = request("http://127.0.0.1:" + server.address().port + "/v1");
  req.model.headers = { "X-Private-Key": "fixture-private-value" };
  const { result } = await invoke(t, req);
  assert.equal(result.status, "completed", result.error);
  assert.equal(result.response, "redirect verified");
  assert.equal(finalCalls, 1);
});

test("native compaction retains provenance and exposes a real session entry for project memory", { timeout: 30000 }, async t => {
  const fixture = await startFixture(t, (_, res, count) => sse(res, { role: "assistant", content: count === 1 ? "condensed working summary" : "final reply" }, "stop"));
  const dir = await mkdtemp(join(tmpdir(), "csai-pi-compaction-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const req = request(fixture.url, dir);
  req.model.contextWindow = 6000; req.model.inputBudget = 4000;
  req.history = Array.from({ length: 16 }, (_, i) => ({ role: i % 2 ? "assistant" : "user", content: ("Recorded context " + i + ": ").repeat(150) }));
  const { result, records } = await invoke(t, req);
  assert.equal(result.status, "completed", result.error);
  const event = records.find(r => r.event?.type === "compaction_end" && r.event.data.result)?.event.data;
  assert.ok(event, "compaction did not run");
  assert.ok(event.sessionFile.startsWith(dir));
  assert.ok(event.sessionEntryId);
  const entries = (await readFile(event.sessionFile, "utf8")).trim().split("\n").map(line => JSON.parse(line));
  assert.ok(entries.some(entry => entry.id === event.sessionEntryId && entry.type === "compaction"));
  assert.ok(result.history.some(m => m.role === "assistant" && m.content.includes("Host-generated working summary")));
  assert.ok(!result.history.some(m => m.role === "user" && m.content === "condensed working summary"));
});

test("interleaved OpenAI tool argument chunks retain their call identities", { timeout: 30000 }, async t => {
  const fixture = await startFixture(t, (_, res, count) => {
    if (count > 1) { sse(res, { role: "assistant", content: "paired" }, "stop"); return; }
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    const deltas = [
      { role: "assistant", tool_calls: [{ index: 0, id: "call-a", type: "function", function: { name: "echo", arguments: '{"text":"' } }, { index: 1, id: "call-b", type: "function", function: { name: "echo", arguments: '{"text":"' } }] },
      { tool_calls: [{ index: 1, function: { arguments: 'B"}' } }] },
      { tool_calls: [{ index: 0, function: { arguments: 'A"}' } }] },
    ];
    for (const delta of deltas) res.write("data: " + JSON.stringify({ id: "fixture", object: "chat.completion.chunk", model: "fixture", choices: [{ index: 0, delta, finish_reason: null }] }) + "\n\n");
    res.end("data: " + JSON.stringify({ id: "fixture", object: "chat.completion.chunk", model: "fixture", choices: [{ index: 0, delta: {}, finish_reason: "tool_calls" }] }) + "\n\ndata: [DONE]\n\n");
  });
  const calls = [];
  const { result } = await invoke(t, request(fixture.url), (r, send) => {
    if (r.type === "tool_call") { calls.push([r.call.providerId, r.call.arguments.text]); send({ type: "tool_result", id: r.call.id, result: { content: r.call.arguments.text } }); }
  });
  assert.equal(result.status, "completed", result.error);
  assert.deepEqual(calls, [["call-a", "A"], ["call-b", "B"]]);
});

test("OpenAI Responses returns native tool calls and consumes their results", { timeout: 30000 }, async t => {
  const fixture = await startFixture(t, (body, res, count) => {
    assert.ok(body.tools.some(tool => tool.type === "function" && tool.name === "echo"));
    if (count === 2) assert.ok(body.input.some(item => item.type === "function_call_output" && item.call_id === "responses-call"));
    const output = count === 1
      ? [{ type: "function_call", id: "fc-1", call_id: "responses-call", name: "echo", arguments: '{"text":"responses"}', status: "completed" }]
      : [{ type: "message", id: "msg-1", role: "assistant", status: "completed", content: [{ type: "output_text", text: "responses verified", annotations: [] }] }];
    const response = { id: "resp-" + count, object: "response", model: "fixture", status: "completed", output,
      usage: { input_tokens: 20, output_tokens: 10, total_tokens: 30, input_tokens_details: { cached_tokens: 0 }, output_tokens_details: { reasoning_tokens: 0 } } };
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    res.write("event: response.created\ndata: " + JSON.stringify({ type: "response.created", response: { ...response, status: "in_progress", output: [] } }) + "\n\n");
    const event = data => res.write("event: " + data.type + "\ndata: " + JSON.stringify(data) + "\n\n");
    event({ type: "response.output_item.added", output_index: 0, item: count === 1 ? { ...output[0], arguments: "" } : { ...output[0], content: [] } });
    if (count === 1) event({ type: "response.function_call_arguments.delta", output_index: 0, item_id: "fc-1", delta: output[0].arguments });
    else event({ type: "response.output_text.delta", output_index: 0, item_id: "msg-1", content_index: 0, delta: "responses verified" });
    event({ type: "response.output_item.done", output_index: 0, item: output[0] });
    res.end("event: response.completed\ndata: " + JSON.stringify({ type: "response.completed", response }) + "\n\n");
  });
  const req = request(fixture.url); req.model.api = "openai-responses";
  let calls = 0;
  const { result } = await invoke(t, req, (r, send) => {
    if (r.type === "tool_call") { calls++; send({ type: "tool_result", id: r.call.id, result: { content: "responses" } }); }
  });
  assert.equal(result.status, "completed", result.error);
  assert.equal(result.response, "responses verified");
  assert.equal(calls, 1);
});
