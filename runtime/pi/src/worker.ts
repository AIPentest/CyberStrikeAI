import { mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { InMemoryCredentialStore, type Model, type Api, type Message } from "@earendil-works/pi-ai";
import {
  createAgentSession, createExtensionRuntime, ModelRuntime, SessionManager, SettingsManager,
  type AgentSession, type ResourceLoader, type ToolDefinition,
  DefaultResourceLoader, createToolSearchExtension,
} from "@earendil-works/pi-coding-agent";
import { PROTOCOL_VERSION, RecordReader, send } from "./protocol.js";
import { installModelFetchPolicy } from "./network.js";
import { compactToolJSON } from "./tool-output.js";

installModelFetchPolicy();

interface ModelConfig {
  provider: string; api?: "openai-completions" | "openai-responses" | "anthropic-messages";
  id: string; baseUrl: string; apiKey: string; headers?: Record<string, string>;
  contextWindow: number; maxTokens: number; thinking?: string;
  extraFields?: Record<string, unknown>;
  compat?: Record<string, unknown>; inputBudget?: number;
  compactToolJSON?: boolean;
}
interface RunRequest {
  generateOnly?: boolean;
  runId: string; conversationId: string; systemPrompt: string; message: string;
  workingDir?: string;
  model: ModelConfig; tools: { name: string; description: string; parameters: Record<string, unknown> }[];
  sessionDir?: string; history?: Record<string, any>[]; maxTurns: number;
  fallbackModels?: ModelConfig[]; retryMax?: number;
  retryMaxDelayMs?: number;
  toolSearch?: boolean; alwaysVisibleTools?: string[];
  toolSearchMinTools?: number;
}
interface ToolReply { content: string; isError?: boolean; executionId?: string; terminate?: boolean }

let session: AgentSession | undefined;
let started = false;
let canceled = false;
let activeRunId = "";
let toolSequence = 0;
const generationAbort = new AbortController();
let steeringBeforeSession: string[] | undefined = [];
const pending = new Map<string, { resolve: (r: ToolReply) => void; reject: (e: Error) => void }>();
const scratch = mkdtempSync(join(process.env.CYBERSTRIKE_PI_TMP_PARENT || tmpdir(), "cyberstrike-pi-"));

function resources(prompt: string): ResourceLoader {
  return {
    getExtensions: () => ({ extensions: [], errors: [], runtime: createExtensionRuntime() }),
    getSkills: () => ({ skills: [], diagnostics: [] }), getPrompts: () => ({ prompts: [], diagnostics: [] }),
    getThemes: () => ({ themes: [], diagnostics: [] }), getAgentsFiles: () => ({ agentsFiles: [] }),
    getSystemPrompt: () => prompt, getSystemPromptSource: () => undefined,
    getAppendSystemPrompt: () => [], getAppendSystemPromptSources: () => [],
    extendResources: () => {}, reload: async () => {},
  };
}

function hostTool(def: RunRequest["tools"][number]): ToolDefinition {
  return {
    name: def.name, label: def.name, description: def.description, parameters: def.parameters as never,
    executionMode: "sequential",
    execute: async (providerId, args, signal) => {
      // Provider IDs are scoped to a model message and may repeat on later turns.
      // The host's replay key must identify an actual invocation, not that provider label.
      const id = activeRunId + ":tool-" + (++toolSequence);
      if (signal?.aborted) throw new Error("Tool canceled");
      const reply = await new Promise<ToolReply>((resolve, reject) => {
        const abort = () => { pending.delete(id); reject(new Error("Tool canceled")); };
        pending.set(id, {
          resolve: (r) => { signal?.removeEventListener("abort", abort); resolve(r); },
          reject: (e) => { signal?.removeEventListener("abort", abort); reject(e); },
        });
        signal?.addEventListener("abort", abort, { once: true });
        void send({ type: "tool_call", call: { id, providerId, name: def.name, arguments: args } }).catch(reject);
      });
      if (reply.isError) throw new Error(reply.content);
      return { content: [{ type: "text", text: reply.content }], details: { executionId: reply.executionId }, terminate: reply.terminate };
    },
  };
}

const emptyUsage = () => ({ input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0,
  cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } });

function importHistory(manager: SessionManager, history: Record<string, any>[], model: ModelConfig): void {
  const replies = new Set(history.filter(m => m.role === "tool").map(m => m.tool_call_id));
  const calls = new Set<string>();
  for (const message of history) {
    const timestamp = Date.now();
    if (message.role === "user") manager.appendMessage({ role: "user", content: message.content ?? "", timestamp });
    else if (message.role === "assistant") {
      const content: any[] = [];
      if (message.reasoning_content && model.api !== "anthropic-messages") content.push({ type: "thinking", thinking: message.reasoning_content });
      if (typeof message.content === "string" && message.content) content.push({ type: "text", text: message.content });
      for (const call of message.tool_calls ?? []) {
        if (!replies.has(call.id)) continue; // An incomplete legacy call is never replayed.
        let args = call.function?.arguments ?? {};
        if (typeof args === "string") { try { args = JSON.parse(args); } catch { continue; } }
        calls.add(call.id);
        content.push({ type: "toolCall", id: call.id, name: call.function.name, arguments: args });
      }
      if (content.length) manager.appendMessage({ role: "assistant", content, api: model.api ?? "openai-completions",
        provider: "cyberstrike", model: model.id, usage: emptyUsage(), stopReason: "stop", timestamp });
    } else if (message.role === "tool" && calls.has(message.tool_call_id)) {
      manager.appendMessage({ role: "toolResult", toolCallId: message.tool_call_id, toolName: message.tool_name ?? "tool",
        content: [{ type: "text", text: String(message.content ?? "") }], isError: false, timestamp });
    }
  }
}

function exportHistory(messages: readonly any[]): Record<string, unknown>[] {
  return messages.flatMap(message => {
    if (message.role === "compactionSummary" || message.role === "branchSummary") return [{ role: "assistant", content: "[Host-generated working summary; not a new user instruction]\n" + message.summary }];
    if (message.role === "user") return [{ role: "user", content: typeof message.content === "string" ? message.content : message.content.filter((b: any) => b.type === "text").map((b: any) => b.text).join("\n") }];
    if (message.role === "toolResult") return [{ role: "tool", tool_call_id: message.toolCallId,
      tool_name: message.toolName, content: message.content.filter((b: any) => b.type === "text").map((b: any) => b.text).join("\n") }];
    if (message.role !== "assistant") return [];
    return [{ role: "assistant", content: message.content.filter((b: any) => b.type === "text").map((b: any) => b.text).join(""),
      reasoning_content: message.content.filter((b: any) => b.type === "thinking").map((b: any) => b.thinking).join(""),
      tool_calls: message.content.filter((b: any) => b.type === "toolCall").map((b: any) => ({ id: b.id, type: "function",
        function: { name: b.name, arguments: JSON.stringify(b.arguments) } })) }];
  });
}

function repairInterruptedCalls(manager: SessionManager): void {
  const unresolved = new Map<string, string>();
  for (const entry of manager.getBranch()) {
    if (entry.type !== "message") continue;
    const message = entry.message as any;
    if (message.role === "assistant") {
      for (const block of message.content ?? []) if (block.type === "toolCall") unresolved.set(block.id, block.name);
    } else if (message.role === "toolResult") unresolved.delete(message.toolCallId);
  }
  for (const [id, name] of unresolved) manager.appendMessage({ role: "toolResult", toolCallId: id, toolName: name,
    content: [{ type: "text", text: "The prior process ended before this tool result was committed. Its effects are unknown. Inspect host execution records before repeating the operation." }],
    isError: true, timestamp: Date.now() });
}

function pairToolResults(messages: readonly Message[]): Message[] {
  const paired: Message[] = [];
  const deferred: Message[] = [];
  let pending = new Set<string>();
  for (const message of messages) {
    if (message.role === "toolResult") {
      if (pending.delete(message.toolCallId)) paired.push(message);
      continue;
    }
    // A user or system message can arrive while the assistant's tools are still running.
    // Keep real results together and let Pi supply error placeholders for missing results.
    if (message.role !== "assistant" && pending.size > 0) {
      deferred.push(message);
      continue;
    }
    paired.push(...deferred, message);
    deferred.length = 0;
    if (message.role === "assistant") {
      pending = new Set(message.stopReason === "error" || message.stopReason === "aborted" ? [] :
        message.content.flatMap(block => block.type === "toolCall" ? [block.id] : []));
    }
  }
  paired.push(...deferred);
  return paired;
}

function openSession(workingDir: string, sessionDir?: string): SessionManager {
  if (!sessionDir) return SessionManager.inMemory(workingDir);
  const dir = resolve(sessionDir);
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  let file = "session.jsonl";
  // Earlier workers selected a timestamped Pi file via this pointer. Keep
  // opening that file in place so existing evidence links remain valid.
  try {
    file = readFileSync(join(dir, "current-session"), "utf8").trim();
    if (!/^\d{4}.*\.jsonl$/.test(file) || file.includes("/") || file.includes("\\")) throw new Error("Invalid session pointer");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
  }
  return SessionManager.open(join(dir, file), dir, workingDir);
}

async function run(req: RunRequest): Promise<void> {
  let status = "completed";
  let failure = "";
  let manager: SessionManager | undefined;
  let directResult: Record<string, unknown> | undefined;
  try {
    if (!req.runId || !req.model?.id || !Array.isArray(req.tools)) throw new Error("Invalid run request");
    activeRunId = req.runId;
    const workingDir = req.workingDir ? resolve(req.workingDir) : scratch;
    const names = req.tools.map(t => t.name);
    if (new Set(names).size !== names.length || names.some(n => !/^[a-zA-Z0-9_.-]+$/.test(n))) throw new Error("Invalid tool catalog");
    const runtime = await ModelRuntime.create({ credentials: new InMemoryCredentialStore(), modelsPath: null,
      modelsStorePath: join(scratch, "models.json"), allowModelNetwork: false, refreshOnCreate: false });
    const configs = [req.model, ...(req.fallbackModels ?? [])];
    const physical: Model<Api>[] = [];
    const modelConfigs = new Map<string, ModelConfig>();
    for (const [index, config] of configs.entries()) {
      const provider = index === 0 ? "cyberstrike" : "cyberstrike-" + index;
      const api = config.api ?? (config.provider === "claude" ? "anthropic-messages" : "openai-completions");
      runtime.registerProvider(provider, {
      api, baseUrl: config.baseUrl, headers: config.headers,
      models: [{ id: config.id, name: config.id, api, reasoning: config.thinking !== "off" && !!config.thinking,
        input: ["text", "image"], contextWindow: config.contextWindow || 128000, maxTokens: config.maxTokens || 16384, compat: config.compat,
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 } }],
      });
      await runtime.setRuntimeApiKey(provider, config.apiKey || "local-no-key");
      const registered = runtime.getModel(provider, config.id);
      if (!registered) throw new Error("Model registration failed");
      physical.push(registered);
      modelConfigs.set(provider, config);
    }
    const streamSimple = runtime.streamSimple.bind(runtime);
    runtime.streamSimple = (m, context, options) => {
      let messages = context.messages;
      if (modelConfigs.get(m.provider)?.compactToolJSON) {
        messages = messages.map(message => message.role === "toolResult" ? {
          ...message,
          content: message.content.map(block => block.type === "text" ? { ...block, text: compactToolJSON(block.text) } : block),
        } : message);
      }
      if (m.api === "openai-completions") messages = pairToolResults(messages);
      return streamSimple(m, { ...context, messages }, {
        ...options,
        transport: "sse",
        onPayload: async (original, currentModel) => {
          const transformed = (await options?.onPayload?.(original, currentModel)) ?? original;
          if (!transformed || typeof transformed !== "object") return transformed;
          const payload = { ...transformed, ...modelConfigs.get(currentModel.provider)?.extraFields } as Record<string, unknown>;
          for (const key of ["model", "messages", "tools", "stream"]) {
            if (key in transformed) payload[key] = (transformed as Record<string, unknown>)[key];
          }
          return payload;
        },
      });
    };
    let model = physical[0];
    if (physical.length > 1) {
      runtime.registerVirtualModel({ provider: "cyberstrike-router", id: "auto", name: "CyberStrikeAI channels",
        contextWindow: model.contextWindow, maxTokens: model.maxTokens, thinkingLevels: ["off", "minimal", "low", "medium", "high", "xhigh", "max"],
        route: request => {
          let index = request.reason === "user" ? 0 : Math.min(Math.max(Number(request.state) || 0, 0), physical.length - 1);
          if (request.reason === "retry" && request.failed && !/context|token limit|401|403|unauthorized|forbidden|quota/i.test(request.failed.message.errorMessage ?? "")) {
            const next = Math.min(index + 1, physical.length - 1);
            if (next !== index) void send({ type: "event", event: { type: "model_failover", data: { model: physical[next].id, channel: next } } });
            index = next;
          }
          return { model: physical[index], thinkingLevel: (configs[index].thinking || "off") as any, state: index };
        },
      });
      model = runtime.getModel("cyberstrike-router", "auto")!;
    }
    if (!model) throw new Error("Model registration failed");
    if (req.generateOnly) {
      const input = SessionManager.inMemory(scratch);
      importHistory(input, req.history ?? [], req.model);
      const answer = await runtime.completeSimple(physical[0], {
        systemPrompt: req.systemPrompt,
        messages: input.buildSessionContext().messages as Message[],
        tools: req.tools,
      }, { maxTokens: req.model.maxTokens, reasoning: (req.model.thinking && req.model.thinking !== "off" ? req.model.thinking : undefined) as any, signal: generationAbort.signal });
      if (answer.stopReason === "error" || answer.stopReason === "aborted") throw new Error(answer.errorMessage || answer.stopReason);
      directResult = { response: answer.content.filter(b => b.type === "text").map(b => b.text).join(""), history: exportHistory([answer]), totalTokens: answer.usage.totalTokens };
      return;
    }
    manager = openSession(workingDir, req.sessionDir);
    if (manager.getEntryCount() === 0) importHistory(manager, req.history ?? [], req.model);
    repairInterruptedCalls(manager);
    const searchEnabled = req.toolSearch === true && names.length >= (req.toolSearchMinTools || 20) && !names.includes("tool_search");
    const always = new Set(["task", "exit", "transfer_to_agent", "write_todos", "TaskCreate", "TaskUpdate", "TaskGet", "TaskList", "skill", "read_file", "execute", "wait_tool_execution", "get_tool_execution", "cancel_tool_execution", ...(req.alwaysVisibleTools ?? [])]);
    const loader = resources(req.systemPrompt + (searchEnabled ? "\nDiscover additional task tools with tool_search. Search results enable matching tools for subsequent calls." : ""));
    if (searchEnabled) {
      const extensions = new DefaultResourceLoader({ cwd: scratch, agentDir: scratch, settingsManager: SettingsManager.inMemory(),
        noExtensions: true, noSkills: true, noPromptTemplates: true, noThemes: true, noContextFiles: true,
        extensionFactories: [createToolSearchExtension()], disabledBuiltinExtensions: ["mcp", "codemode"],
      });
      await extensions.reload();
      loader.getExtensions = () => extensions.getExtensions();
      if (loader.getExtensions().errors.length) throw new Error("Pi tool search extension failed to initialize");
    }
    const inputBudget = Math.max(1024, Math.min(req.model.inputBudget || model.contextWindow - model.maxTokens, model.contextWindow - model.maxTokens));
    ({ session } = await createAgentSession({ cwd: workingDir, agentDir: scratch, modelRuntime: runtime, model,
      thinkingLevel: (req.model.thinking || "off") as any, sessionManager: manager,
      settingsManager: SettingsManager.inMemory({ retry: { enabled: true, maxRetries: req.retryMax ?? Math.max(2, configs.length - 1), ...(req.retryMaxDelayMs ? { maxAgentDelayMs: req.retryMaxDelayMs } : {}) }, compaction: { enabled: true, reserveTokens: model.contextWindow - inputBudget, keepRecentTokens: Math.min(20000, Math.floor(inputBudget / 2)) } }),
      noTools: "builtin", tools: searchEnabled ? ["+tool_search"] : names, customTools: req.tools.map(def => ({ ...hostTool(def), exposure: searchEnabled && !always.has(def.name) ? "deferred" : "direct" })),
      resourceLoader: loader,
    }));
    if (searchEnabled) await session.bindExtensions({});
    session.agent.toolExecution = "sequential";
    let turns = 0;
    const previousFinish = session.agent.finishTurn;
    session.agent.finishTurn = async (turn, signal) => {
      turns++;
      if (turns >= (req.maxTurns > 0 ? req.maxTurns : 30) && turn.toolResults.length > 0) { status = "iteration_limit"; return { action: "end" }; }
      return (await previousFinish?.(turn, signal)) || undefined;
    };
    session.subscribe(event => {
      const e = event as any;
      let data: unknown = e;
      if (e.type === "compaction_end") data = { ...e, sessionFile: manager?.getSessionFile(), sessionEntryId: manager?.getLeafId() };
      if (e.type === "message_update") {
        const { partial: _partial, ...delta } = e.assistantMessageEvent;
        data = { assistantMessageEvent: delta };
      }
      if (e.type === "agent_end") data = {};
      void send({ type: "event", event: { type: e.type, data } }).catch(() => session?.abort());
    });
    // Pi accepts steering before prompt() and drains it during the run.
    for (const note of steeringBeforeSession ?? []) await session.steer(note);
    steeringBeforeSession = undefined;
    if (canceled) throw new Error("Run canceled before start");
    await session.prompt(req.message, { expandPromptTemplates: false });
    const last = [...session.messages].reverse().find(m => m.role === "assistant") as any;
    if (canceled || last?.stopReason === "aborted") status = "canceled";
    else if (last?.stopReason === "error") { status = "failed"; failure = last.errorMessage || "Model request failed"; }
  } catch (error) {
    status = canceled ? "canceled" : "failed";
    failure = error instanceof Error ? error.message : String(error);
  } finally {
    const result = { response: session?.getLastAssistantText() ?? "",
      history: exportHistory(session?.messages ?? []), ...directResult, status, error: failure, sessionFile: manager?.getSessionFile() };
    session?.dispose();
    rmSync(scratch, { recursive: true, force: true });
    await send({ type: "result", result });
    process.exit(0);
  }
}

async function command(record: any): Promise<void> {
  if (record.type === "run") {
    if (started || record.version !== PROTOCOL_VERSION) throw new Error("Invalid protocol state/version");
    started = true;
    void run(record.request);
  } else if (record.type === "tool_result") {
    const call = pending.get(record.id);
    if (call) { pending.delete(record.id); call.resolve(record.result); }
  } else if (record.type === "cancel") {
    canceled = true;
    generationAbort.abort();
    for (const p of pending.values()) p.reject(new Error("Run canceled"));
    pending.clear();
    await session?.abort();
  } else if (record.type === "steer") {
    if (canceled || typeof record.message !== "string") throw new Error("Invalid steering request");
    if (steeringBeforeSession) steeringBeforeSession.push(record.message);
    else await session!.steer(record.message);
  } else throw new Error("Unknown protocol command");
}

const reader = new RecordReader();
process.stdin.on("data", (chunk: Buffer) => {
  try { for (const record of reader.push(chunk)) void command(record).catch(fatal); } catch (e) { void fatal(e); }
});
process.stdin.on("end", () => { if (started) void command({ type: "cancel" }); else process.exit(0); });
async function fatal(error: unknown): Promise<void> {
  await send({ type: "fatal", error: error instanceof Error ? error.message : String(error) });
  session?.dispose();
  rmSync(scratch, { recursive: true, force: true });
  process.exit(1);
}
await send({ type: "ready", version: PROTOCOL_VERSION, runtime: "pi", sdkVersion: "1.5.2" });
