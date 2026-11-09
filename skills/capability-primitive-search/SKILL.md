---
name: capability-primitive-search
description: >-
  Capability primitives + state-space search: make RCE equation A-F out of read/write/exec/ssrf etc. primitives, low-severity mapping, forward/backward search, cross-domain cashing-in. Use when no single RCE, chaining low-severity vulns, or deriving novel attack chains.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Capability Primitives + State-Space Search

```
Mindset: RCE is not one "vulnerability", it is the emergence when a set of "capabilities" is completed. Without a single big hole, several info-level/low-severity ones still assemble into code execution.
  Ingrained thinking (to break): "the scan finds no RCE/deserialization/upload → this site cannot be attacked". Your job is not "find an RCE vulnerability", it is "complete the primitives needed for execution".
  Abstract each Fact into a capability primitive (do not record "discovered vulnerability X", record "what capability I now hold + constraints"):
    read(path) write(path) exec(cmd) ssrf(url) sqli redirect(url) eval_expr idor(id) cred(svc,priv) coerce_auth write_acl

RCE only needs any one of these equations satisfied; decompose into obtainable primitives and complete it:
  A. Can write a file + the file is executed as code          = RCE
  B. Can control config/env + the config points to your code  = RCE
  C. Can enter the admin panel + the panel has a built-in execution feature = RCE (not a vulnerability, it's a feature!)
  D. Hold credentials + the service has a legitimate execution entry = RCE (abuse of legitimate function)
  E. Can read arbitrarily + read out credentials + the credentials can log into an execution point = RCE
  F. Can control data + the data flows into a dangerous sink (eval/template/SQL) = RCE

Low-severity → primitive mapping (translate "chili-knuckle vulnerabilities" into puzzle pieces):
  info leak (.git/backups/stack traces) → source/paths/keys → feeds B/E/F | LFI/arbitrary read → read config keys → E, or log poisoning → A
  SSRF (even GET-only) → hit internal-network Redis/Consul/K8s/cloud metadata → C; cloud metadata grabs temporary credentials → D
  weak/default/reused credentials → enter a backend with task-schedule/plugin/webhook/CI functions → C | CORS/CSRF/XSS → hijack an administrator's browser to call execution-type functions → C
  controllable upload (even with extension restrictions) → combine path traversal/parsing differences/.htaccess → A | config write → modify templates/logs/connection strings → B
  SQLi (even read-only) → read hashes/keys → E, or OUTFILE → A | controllable template → SSTI → F | prototype pollution → pollute downstream properties → F

State-space search (search yourself when no ready-made chain): state = the current capability set, action = use a capability to unlock new ones, goal = Goal.
  Forward: for each capability ask "what can it unlock?"; for each pair of capabilities ask "what does the combination produce?"
    (read+write = modify config; ssrf+internal redis = RCE; sqli+FILE = webshell; coerce_auth+relay = domain SYSTEM; idor+massassign = modify someone else's admin)
  Reverse (main weapon when stuck): lock Goal = execute a command → pick the equation closest to the present state as a template → set whichever primitive is missing as a sub-goal →
    which low-severity/function/info leak in hand can complete it? → if it cannot be completed, recursively decompose/switch equations → forward and reverse meet in the middle = the complete chain emerges → verify each segment

Openers (do not walk past as shut when you see one):
  ·"function equals primitive": the backend's task scheduler/plugins/template editor/SQL console/file manager/import-export/webhook —— legitimate functions, but logged in they are ready-made execution/read-write primitives. In the eyes of a pentester there is no "function/vulnerability" distinction, only "capability".
  ·Cross-protocol hop: SSRF's gopher/dict/file turns "can only send HTTP" into "hit Redis/send SMTP/read files".
  ·Credential reuse is universal glue: a password/key obtained in any one place is by default reused network-wide, spray it all. Lateral is often faster than vertical.
  ·Parsing differences: upload validation/routing/rereverse proxy three parties understand it inconsistently → the gap holds bypasses (double extensions/encoding/Host confusion).
  ·Time dimension: TOCTOU/predictable tokens/cache poisoning turn "occasionally" into "stable", and unexploitable into exploitable.
  ·Cross-domain cashing-in: for every capability you gain ask "how much is it worth in another domain" — capabilities are a universal currency. Web SSRF → into cloud metadata to take over an account; APK hardcoded → direct-connect internal API bypassing front-edge authorization; supply chain → take CI keys into production.
  ·Creation modes (when known combinations are exhausted): re-review the capability boundary (only reads /var/log? what about /proc/self/environ) | find sinks equivalent to RCE (write crontab/.bashrc/CI config/LD_PRELOAD/authorized_keys/systemd unit all = RCE) | treat information (error messages/timing/response length) as a side channel | assumption negation: list "I thought impossible" and ask of each "why exactly impossible"
> A derived chain is a hypothesis (can record note/chain as tentative); only after actual execution + evidence is a confirmed Fact / record_vulnerability written. A whole chain holds only when every step has been verified.
```
