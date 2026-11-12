import { EnvHttpProxyAgent, install, setGlobalDispatcher } from "undici";

// Model credentials may use arbitrary custom header names. Fetch's default
// redirect handling only protects selected standard headers, so the isolated
// worker follows redirects explicitly and only within the original origin.
const redirects = new Set([301, 302, 303, 307, 308]);

function blocked(message: string): Response {
  return new Response(JSON.stringify({ error: { type: "invalid_request_error", message: "Local model transport: " + message } }),
    { status: 400, headers: { "Content-Type": "application/json" } });
}

async function modelFetch(upstreamFetch: typeof fetch, input: string | URL | Request, init?: RequestInit): Promise<Response> {
  let request = new Request(input, { ...init, redirect: "manual" });
  const origin = new URL(request.url).origin;
  const body = request.body ? await request.clone().arrayBuffer() : undefined;
  for (let hop = 0; hop <= 10; hop++) {
    const response = await upstreamFetch(request);
    if (!redirects.has(response.status)) return response;
    const location = response.headers.get("location");
    if (!location) return response;
    const target = new URL(location, request.url);
    await response.body?.cancel();
    if (target.origin !== origin) return blocked("cross-origin redirect blocked; use the final trusted model endpoint");
    if (hop === 10) return blocked("redirect limit exceeded");
    const dropBody = response.status === 303 || ((response.status === 301 || response.status === 302) && request.method === "POST");
    const method = dropBody && request.method !== "HEAD" ? "GET" : request.method;
    const headers = new Headers(request.headers);
    if (dropBody) { headers.delete("content-type"); headers.delete("content-length"); }
    request = new Request(target, { method, headers, body: method === "GET" || method === "HEAD" ? undefined : body,
      signal: request.signal, redirect: "manual" });
  }
  return blocked("redirect limit exceeded");
}

export function installModelFetchPolicy(): void {
  setGlobalDispatcher(new EnvHttpProxyAgent({
    allowH2: false,
    proxyTunnel: true,
    headersTimeout: 300_000,
    bodyTimeout: 300_000,
    connect: { autoSelectFamilyAttemptTimeout: 2_000 },
  }));
  // The Pi SDK does not run its CLI dispatcher setup. Install npm Undici's
  // fetch and matching web types before wrapping fetch for the model SDKs.
  install();
  const upstreamFetch = globalThis.fetch;
  globalThis.fetch = (input, init) => modelFetch(upstreamFetch, input, init);
}
