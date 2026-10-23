# Asynchronous conversations

Submit to `POST /api/eino-agent/runs` or `POST /api/multi-agent/runs` with the corresponding synchronous request body and a required `Idempotency-Key` header (1–200 bytes). Multi-agent execution must be enabled. Omit conversationId to create a conversation, or supply an existing accessible conversation.

A 202 receipt contains runId, conversationId, assistantMessageId, status and statusUrl. The Location header points to statusUrl. After acceptance the client may disconnect; execution has its own 600-minute budget. Individual model and tool limits still apply.

Poll statusUrl with authentication to read durable status, result and timestamps. Result contains the original execution response or error. Read saved messages and process details through the existing conversation/message endpoints. The event subscription endpoint only delivers subsequent events, without replay.

Keys are scoped to the authenticated user. Repeating the same key and request returns the original receipt; a different request with the same key returns 409. Retry lost receipts with the original key and body. Use a new key for a new invocation. Concurrent submissions to an occupied conversation return 409 before appending messages.

Explicitly cancel the current conversation task with `POST /api/agent-loop/cancel` and `{"conversationId":"..."}`. Cancellation is conversation-scoped: verify that the active run still matches the intended receipt before cancelling.

Submission requires agent:execute and chat:write; queries require endpoint permission and conversation access. Background execution retains the authenticated principal. Existing synchronous endpoints retain their response contract and request-bound cancellation behavior.

Running, completed, failed, cancelled, timeout and interrupted states are persisted, along with finalization/cleanup outcomes. Check finalized=true in the execution response for successful final delivery; a token usage summary is not a success signal.

The current runtime assumes one server instance owns its database. On startup, unfinished running receipts become interrupted and remain queryable. Tools are not automatically replayed after a restart. Database write failures during acceptance return an error rather than a receipt.
