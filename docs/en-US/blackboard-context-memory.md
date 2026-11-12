# Retaining blackboard checkpoints after long-context compaction

[中文](../zh-CN/blackboard-context-memory.md)

When a conversation is bound to a project and the project blackboard is enabled, every successful compaction summary automatically saves a continuation record. Retention no longer relies only on the Agent calling fact tools before compaction.

## Saved content and retrieval

Continuation records use `note/context-*` keys. They contain important findings, authorization constraints, progress, unsuccessful attempts, and next steps from the summary, together with the source conversation, agent scope, and snapshot path of the original pre-compaction transcript. The main agent, each sub-agent, and the planner and executor in plan-execute mode maintain separate records. Repeated compactions for the same conversation/agent update the same key.

Records use `category=note` and `confidence=tentative`. They are not automatically pinned and do not overwrite confirmed facts such as `finding/*`. A record is a model-generated working summary, not an independently verified vulnerability or new authorization. Maintain formal facts and vulnerabilities with `upsert_project_fact` and `record_vulnerability`.

The compacted context includes a `get_project_fact(key)` entry point and refreshes the system's blackboard index. Read records on demand, then use `read_file` to inspect the original evidence in segments; the full history need not be injected into every model request.

The new context and blackboard record retain the same continuation summary, incorporating the absolute snapshot path, historical snapshot directory, and retrieval hints. The summary preserves the source target, purpose, and discovery context of key evidence. If a task needs a Cookie, authentication information, exact parameters, or original error text, the Agent can search the relevant snapshot and inspect nearby original text with `read_file` instead of guessing values.

Existing summary sections and the original-user-input ledger remain unchanged. The same summary adds an “original evidence retrieval hints” section with sources and keywords for tool results or key progress. It adds no new summary of user input and does not ask the model to invent filenames or line numbers.

No separate `.index.md` file is generated, no regex or keyword classification scans snapshots, and no additional model call is made. The summary itself provides the evidence retrieval entry point. Details may still be omitted; snapshots remain authoritative for original content.

## User input and system summaries have different provenance

Eino transmits its default continuation summary with the `user` message role, but also marks it with `_eino_summarization_content_type=summary`; retained skill context uses the `skills` marker. The original-user-input ledger excludes internal summaries and skill messages using this metadata. `role=user` alone cannot establish that generated content is original user input. Simulated user ledgers within summaries are not imported as original input, preventing repeated compaction from promoting model-written goals or actions into user instructions. Snapshot and summarization-model text records also identify internal messages as system-generated, not user input or new authorization.

Ledger continuation preferentially reads structured provenance metadata in system messages, rather than extracting similarly named text labels from ordinary assistant or tool responses. JSON checkpoint restoration, classic/native conversion, and system-message merging preserve this provenance. Legacy compatibility accepts only a complete host ledger block at the end of a system message, not fragments embedded in a blackboard or response.

Compacted summaries remain in the model's working context; distinguishing provenance does not discard progress. Main/sub-agent input for the current turn still comes from the original request and chat records. Text keywords such as `<summary>` are not used to filter genuine user input. Regression tests invoke the real `DefaultFinalize`, perform three compaction rounds and classic/native conversions, and check that original authorized targets and the prohibition on testing the local machine remain; generated targets and simulated instructions do not enter the user ledger; summary progress is retained; and user quotations of summary text remain genuine user input.

## Read boundaries for conversation work records and shared project facts

The design distinguishes conversation state from cross-conversation memory rather than treating every automatic summary as a shared fact. The [official LangChain memory concepts](https://docs.langchain.com/oss/python/concepts/memory) describe working state as thread-scoped and cross-conversation memory as using a separate namespace. This project retains its existing fact structure and uses the reserved `note/context-*` prefix plus source conversation identity for the corresponding read scope, without a database migration or additional model extraction.

| Content | Automatic injection and default fact tools | Explicit historical queries |
| --- | --- | --- |
| Ordinary project facts, including confirmed findings and manual notes | Existing project-sharing behavior | Existing behavior |
| Automatic continuation records from the current conversation, including its sub-agents/planner/executor | Visible and labeled as current conversation work records | Visible |
| Automatic continuation records from other conversations in the same project | Not injected; absent from default lists/searches; direct get requires the history flag | Readable with `include_history=true`, labeled as historical reference |
| Legacy continuation records without a source conversation | Not returned as current work records | Readable through explicit historical queries, labeled as unknown origin |
| Records in other projects | Unreadable through current-project tools | The history flag cannot expand project scope or bypass `project:read` |

`list_project_facts`, `search_project_facts`, and `get_project_fact` default to the current conversation scope. The first two support `source_conversation_id` to narrow queries. Filtering occurs before database pagination, preventing pinned historical records from filling a page only to be discarded later. Initial main-agent injection, sub-agent blackboard supplements, and classic/native post-compaction index refreshes explicitly pass the current conversation ID. Missing conversation identity does not fall back to injecting every automatic continuation record.

Relationship overviews, inline relationship hints, and relation edges returned by get/upsert also filter automatic continuation keys from other conversations to prevent a scope bypass through edges. The mirrored “related” section in a fact body is returned unchanged by `get_project_fact` and is not reader-filtered, so no automatic continuation keys are written into that section. Structured relation edges still respect caller scope; the current conversation's own continuation records remain available. Historical bodies include source conversation and update time. Reading them does not promote them into confirmed facts or new task authorization. `note/context-*` is a system-reserved prefix: fact-writing tools and console create/update APIs cannot create, overwrite, or rename ordinary facts into it. Only compaction writes continuation records. Deprecation/restoration tools cannot modify other conversations' automatic records. Save verified project knowledge under separate fact keys with its evidence.

These are default context-injection and fact-retrieval scopes, not a new filesystem permission boundary. They do not delete snapshots, change existing records, or remove content already present in old conversation history. Authorized users can still view all project data through project management and existing database queries without scope parameters. Existing project permissions remain enforced by tool authorization.

Validation uses two conversations in one project, another project, and legacy records of unknown origin. It covers default/explicit queries, source filters, pagination, pinned history, relation edges, reserved-key write protection, permission denial, initial main-agent injection, and index refresh after actual classic/native compaction saves. Existing blackboard-budget and original-user-ledger tests ensure scope markers do not exceed length limits or alter user input.

## Original evidence survives later compactions

Previously, every compaction wrote the same `transcript.txt`, overwriting earlier records. Each pre-compaction history is now saved to `snapshots/transcript-<first-5-MD5-characters>-<timestamp>.txt`, for example `transcript-a1b2c-20261012-010203.000000004+0800.txt`. The timestamp includes nanoseconds and timezone and records the first save. Retries with identical content compare the complete file and reuse the snapshot without changing its time or path. The short MD5 is a label, not the uniqueness check; prefix or timestamp collisions do not overwrite files. Older SHA-256 filenames remain readable and reusable without renaming, preserving references in older summaries. `transcript.txt` remains a compatibility entry point for the latest record, while continuation summaries reference specific snapshots. Snapshots use the existing conversation transcript format, retaining user, assistant, and tool records while omitting unchanged system prompts and tool/skill indexes.

Snapshots remain under `conversation_artifacts/<conversationId>/summarization/` and follow the conversation-artifact lifecycle. Sub-agents use separate scope subdirectories. Deleting a conversation removes its artifacts through existing cleanup; snapshots are not independent post-deletion backups. Files are written through atomic replacement using temporary files, with mode 0600.

With the default database at `data/conversations.db`, the main-agent layout is as follows. Summaries provide absolute paths to the Agent:

```text
data/conversation_artifacts/<conversationId>/summarization/
├── transcript.txt
└── snapshots/
    └── transcript-<first-5-MD5-characters>-<timestamp>.txt
```

Filename timestamps can order snapshots. A snapshot may be saved before summary generation, including failed attempts, and retries with identical history reuse files. File order therefore does not strictly correspond to the number of successful compactions.

When the database path is empty, temporary snapshots use `<temporary-directory>/cyberstrike-summarization/<conversationId>/summarization/snapshots/` instead of sharing one directory across conversations.

Sub-agents, planners, and executors use `summarization/scopes/<scopeHash>/snapshots/`. Summaries provide historical snapshot directories; the Agent can locate `transcript-*.txt` with `glob` and read older evidence on demand. Legacy index files are unused by the new process and are not automatically deleted on upgrade.

The system reuses the compaction summary already generated, without an additional model extraction call. Original history is snapshotted before summary generation, preserving a source even when the summary omits an exact detail. Blackboard or snapshot save failures log warnings and continue with the available summary without claiming that saving succeeded.

## Configuration and boundaries

```yaml
project:
  enabled: true
  context_checkpoints: true
```

`context_checkpoints` defaults to enabled when omitted. Setting it to false disables automatic blackboard continuation records without affecting original-evidence snapshots. Unbound conversations still retain local snapshots and never write their contents into another project.

Before saving automatically, the system checks the conversation's current project binding. The actual SQLite upsert checks the binding again in the same write statement, preventing a project switch between checking and writing from saving an old run into the old project. A mismatch inserts no row and changes no existing record. This adds no cross-project search. Existing blackboard permissions and fact-tool conversation scopes still apply.

Automatic records are persisted only when compaction occurs; they do not replace immediate registration of new findings. Summaries may omit details; snapshots remain the source for original text. Snapshots consume local storage until their conversation artifacts are cleaned up.

## Index budgets and provenance

Automatic injection still includes only keys, short summaries, and relation edges; complete bodies stay in the database for on-demand reads. Index queries no longer load bodies that will not be injected. The character budget includes headings, boundary markers, retrieval hints, omission notices, and relationship overviews. Very small budgets use a short retrieval hint or omit the index rather than truncating HTML boundaries.

The blackboard needs no additional model compaction. Each conversation/agent's automatic continuation body is updated to the current summary; short labels are capped at 200 characters. Neither full bodies nor original snapshots are automatically injected. Retrieval entry points are attached only to continuation summaries and blackboard bodies; no full history is copied and no extra evidence-category index is maintained. Original history stays in files, and updating continuation records does not alter confirmed facts.

If snapshot writing fails, the framework's default `transcript.txt` path hint is not injected, preventing reads of a nonexistent or previous-round file. Plain-text summaries, `<summary>`-tagged text, and multipart messages retain the same retrieval entry point without duplicating it in the body.

Legacy oversized summaries are shortened to one line for index display, leaving full summaries/bodies in the database unchanged. Index-row category and confidence values are also reduced to single lines: persistence supplies defaults but does not validate these fields, so embedded newlines could otherwise inject fake rows into the system index. Relationship overviews use only facts actually included in the index, preventing out-of-budget facts from consuming context through path views. An empty index also has an empty relationship overview; an empty set does not disable filtering. Existing pinning and fact-type priorities remain unchanged.

## Implementation basis and validation

Eino compaction supports summary processing and context rewriting in `Finalize`; `Callback` runs after postprocessing. This project saves the record, attaches snapshot references, and refreshes the index within its existing custom Finalize. Ordinary messages and native AgenticMessage paths use the same retention function. See [Eino Summarization](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/eino_adk_chatmodelagentmiddleware/middleware_summarization/).

Regression coverage includes early evidence surviving repeated compaction, identical-snapshot idempotency, atomic file permissions, main/sub-agent isolation, unchanged confirmed facts, disabling the setting, project mismatch, no false save-success claims, multipart summaries, automatic persistence/refresh for ordinary and native middleware, hard index budgets, and no body injection.

## Additional code-review validation

Further fixes address tool/assistant text impersonating the original-user ledger, shared temporary directories without database paths, and races between checking and writing project bindings. Regression coverage includes forged labels in tool/assistant messages, fragments embedded in system blackboards, structured-provenance JSON restoration and system normalization, rejection of invalid provenance metadata, isolated conversation directories for classic/native paths without database paths, and project switching/unbinding that rejects inserts and updates without changing existing evidence. Historical snapshots are not migrated or rewritten; production updates should correspond to the validated code revision.

## Restoration and boundary regressions

Agent traces are restored through one parser, carrying only known `summary` / `skills` provenance markers and the structured original-user ledger from system messages. `agent.ChatMessage` carries these as runtime fields. Ordinary model-request JSON excludes internal metadata; trace-specific serialization retains allowed provenance fields. Restoration does not replay old system prompts and rerenders the host ledger within the current budget. Classic/native model entry points preserve structured provenance when merging system messages, and compatibility text cannot overwrite an existing structured ledger. Ordinary user quotations of summaries or labels remain original input. Multipart summary restoration preserves progress and provenance. Tests use the same Chinese ADK settings as production and cover actual database loading and three rounds of compaction → persistence → restoration → recompaction, including classic/native conversions. These fixes cannot recover provenance already lost in old traces and do not rewrite historical records.

After an immutable snapshot succeeds, its path remains in the summary and blackboard even if updating the `transcript.txt` compatibility entry point fails; logs report that separate failure. A failed snapshot itself is never reported as saved.

Fact-key writes reject surrounding whitespace. Default SQL scope filters recognize Unicode whitespace in existing keys consistently with Go checks. Reading does not bulk-rename or migrate older records. A successfully built empty index removes the old index block while preserving other system content and the user ledger; a database read failure retains the original index.

Blackboard short labels prefer evidence retrieval hints already generated in summary section 8, falling back to the previous label method when unavailable. Only the index label changes, not summary sections 1–7, the complete body, or the original-user ledger; no model call is added. The `Context memory` CI runs concurrency regressions for agent/attackchain/app/database/handler/multiagent/project/projectprompt on macOS, Ubuntu, and Windows. Windows does not check unsupported Unix permission bits.

## Pre-release scenario acceptance

User-role messages for planning and replanning are system-generated templates containing plans and model/tool results, not new user input. Templates use a separate allowlisted provenance field. Before construction, a bounded user ledger is saved from the original uncombined messages, followed by rewriting/compaction. Role and provenance remain visible in input text and snapshots. Trace restoration and framework user-message blocks do not import templates as original user input, while their working progress remains available to the model. Ordinary user quotations of plans or summaries remain genuine input.

Restarts after context overflow or transient failure remove old instructions and indexes but preserve and rebuild the structured original-user ledger within the current budget. Budgeting the first long input includes numbering, separators, and ellipses, so available space is not wasted by dropping the whole entry. Only bounded excerpts of original text are kept, without model-generated summaries of user input. Budgets cannot retain unlimited original text; retrieve exact details from snapshots when needed.

Facts with existing surrounding-whitespace keys can have summaries/bodies updated using their original ID, project, and complete key, without renaming or changing historical relation references. Creating or renaming to noncanonical keys remains rejected. Actual HTTP partial-update regression tests cover this compatibility path.

Acceptance also covers saving long tool output as an original-text file, obtaining its file reference from snapshots after compaction/restoration, and reading the exact Cookie through the `read_file` backend in segments. A synthetic Cookie sits in the middle of 2,001 output lines and appears in neither the truncated preview nor the summary; both classic/native paths recover it completely. This guarantees evidence and retrieval entry points are available, not that a model will choose to read them in every task.

Chat input, approval interactions, and API routes retain their existing flow. New visible content consists of automatic continuation records, short retrieval hints, and a summary entry point in the attack-chain sidebar. Automatic work records from other conversations in the project are not injected by default; ordinary shared facts and manual notes retain their existing read behavior. Local snapshots accumulate for distinct compacted content and follow the conversation-artifact deletion lifecycle.

## Summaries and evidence hints in attack chains

Attack-chain analysis input uses persisted provenance metadata to distinguish actual user input, the host-retained original-user ledger, system-generated summaries, skill context, and execution templates. Generated summaries and templates are working hints, not new authorization or independent proof of vulnerabilities or tool execution. Nodes still require actual tool records. User quotations of summary text remain genuine user input; provenance is not inferred from body keywords.

Actual tool-record grounding is enforced in code, not only in prompts. `action` and `vulnerability` nodes may include `tool_call_id`, copied exactly from a host-issued call ID in the trace. Referencing an ID absent from the trace is treated as fabrication; the node and its incident edges are dropped. `target` nodes are exempt. Whether references are mandatory depends on ID visibility to the model: if all actual IDs remain visible after budget trimming, a missing reference is also dropped. If some IDs were trimmed, missing references are unmatched rather than fabricated. Multi-agent supplemental process details are dumped as raw event JSON, whose IDs are absent from the parsed trace; validation is skipped in that case, as in the `messages` table fallback, to avoid discarding real nodes. Valid references are saved in node `metadata.tool_call_id`, not `attack_chain_nodes.tool_execution_id`: that column references `tool_executions`, whereas model references are call IDs.

The attack-chain sidebar offers an expandable “Compaction summaries and evidence hints” section showing complete bodies with summary provenance from the current conversation trace. It retains immutable snapshot paths and supports copying text for source checks. Reading an existing cached attack chain also returns summaries from the current trace without regenerating nodes. Summaries are displayed separately and do not become attack nodes or affect node counts, filters, or graph exports. A conversation without tool execution can show summaries with an empty graph.

This entry point reads only the current conversation's agent trace, not a shared-blackboard aggregation of other conversations. It adds no snapshot-download or arbitrary-file-reading API. Older traces without provenance metadata are not guessed to be summaries. Older summaries removed by subsequent compaction remain retrievable through snapshot directories. Updating code does not rewrite cached nodes; choose **Regenerate** to reanalyze a chain using the new provenance labels.

Attack-chain requests discard late responses based on conversation and request order. Switching, closing, and errors clear old summaries, preventing stale content from replacing a new view. A busy concurrent regeneration returns 409 without first deleting the cached graph. Cached empty nodes and edges are always arrays, and a single node with no edges displays correctly. Regressions cover real model HTTP request input, current-conversation versus same-project isolation, cached-summary update/clearing, summary HTML rendered as text, copying and failure fallbacks, and out-of-order requests within and across conversations.
