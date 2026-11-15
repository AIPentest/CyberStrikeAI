# Pi context and project checkpoints

Pi SessionManager retains the full JSONL session and compaction entries. The current model context is a projection of that history, not the only copy of it.

When project memory is enabled and a conversation is bound to a project, a successful `compaction_end` saves a `note/context-*` checkpoint containing the summary, conversation/agent scope, session path and real entry ID. SQLite enforces `note`, `tentative` and unpinned status, and checks project binding atomically. Rebinding a conversation prevents stale runs from updating its former project's memory.

Generated summaries are not new user instructions. Generic trace exports represent them as explicitly labeled assistant working context. Legacy imports restore user intent only from trusted structured host metadata; summary/template content and text that merely resembles a ledger are not promoted to user input. Literal `<summary>` text supplied by a user remains user content.

Conversation deletion removes its Pi session scopes without touching other conversations. Uncommitted tool effects after a crash remain uncertain and require execution-record reconciliation. Final findings continue through the existing evidence/review flow.

Implementation and tests: `runtime/pi/src/worker.ts`, `internal/multiagent/pi_memory.go`, and the database checkpoint/session tests. See [Pi runtime](pi-runtime.md).
