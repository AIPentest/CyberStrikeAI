# Changelog

## Unreleased

### Added

- AI channels now support custom HTTP request headers, environment variables, and session ID placeholders; saved authentication headers can be safely used for connection testing, an independent audit identity isolates the primary channel's request headers, and custom headers do not follow cross-origin redirects.

- Support for configuring single-agent, multi-agent, workflow, and async task execution timeouts via `agent.task_timeout_minutes`: defaults to 600 minutes when unset, `0` means no limit, and negative values are rejected at load time; resume preserves the re-timing behavior, and per-call tool timeouts still apply independently.

- The single-item download button in the vulnerability list now has a format selection dialog supporting Markdown and Word (.docx); Word download reuses vulnerability read permissions and includes only the selected vulnerability (#365).

- Added single-agent and multi-agent async conversation APIs: they return a task receipt on acceptance and execution does not stop when the client disconnects; supports per-user isolated idempotent submission, persistent state and result queries, explicit cancellation, and interrupted status after a service restart. The synchronous interfaces remain compatible.

- On startup, all project-management tables are automatically checked against a unified, latest SQLite definition—creating missing tables and adding missing fields—without maintaining a field-by-field migration list; existing data and extra tables/fields are preserved, and schema completion failures roll back and block startup. The HITL and notification tables are included in the unified initialization, and the historical vulnerability foreign-key migration preserves extra columns, indexes, triggers, and dependent records.

### Fixed

- Fixed an issue where WebShell batch liveness checks stayed stuck on "Checking" due to a request-construction exception; the connection's own ID is now used to read saved credentials, the button is restored after a failed check, and other connections continue to be processed.

- Fixed an issue where the audit Agent failed when sending requests to compatibility gateways that do not support the thinking field; a standalone compatibility profile is now supported, and default behavior is detected by model and address.

- Fixed an issue where official OpenAI models known to support `none` still used the server-side default reasoning after reasoning was turned off (#366): the explicit disable parameter is now sent and preserved per provider, endpoint, and model, covering the Planner and forced tool selection; DeepSeek and third-party compatibility gateway behavior is preserved, and the tool-call limitation requiring Responses is clearly noted.

- Prevent the built-in administrator from losing privileges: deactivating the built-in `admin` or removing its system administrator role is now refused, preserving the original profile, permissions, and sessions on failure; the UI disables the corresponding actions accordingly.

- Fixed an issue where asset-management information gathering failed because it requested the unsupported `province` return field from FOFA by default; it now uniformly uses `region`, is compatible with legacy form/API field aliases, preserves the province of imported assets, and corrects the intelligence field preset to `asn/org`.

- Fixed an issue where single-agent, multi-agent, role workflow, and batch tasks misjudged a lower-level call timeout as a whole-task timeout: a task is marked timed out only when the task context's own deadline expires, and lower-level failures preserve the original diagnostics; the overall time limit and existing retry policy remain unchanged.

- Deleting a nonexistent C2 Profile or WebShell object now uniformly returns 404, and a database failure during deletion returns a generic 500 message instead of exposing SQL errors; vulnerability and batch task lists uniformly return `[]` when no matching records exist, with added HTTP contract regression tests.

- Fixed an issue where, when multiple people edited call-blocking rules, an old draft overwrote others' changes: only one save succeeds per version, conflicts preserve the draft and prompt for a refresh; API saves must carry the ETag/If-Match from read time.

- Fixed an issue where platform role updates/deletions logged out all users: only the sessions of affected role members are now revoked; name, description, and equivalent permission-set updates keep sessions valid.

- Fixed dashboard status card, risk badge, and queue label overflow caused by long text such as Russian; the sidebar and risk names now wrap fully, and the layout adapts to the card's available width.

- Self-update recovery now strictly validates recovery records, State, commit, and binary pairing before rewriting files; missing, duplicate, or contradictory evidence is rejected, preserving a recoverable state.
- Editing a stopped external MCP no longer accidentally starts a connection; start/stop, config editing, and app restart maintain a consistent start/stop state.

- The login password show/hide button is now an eye icon inside the input field, with unified input width and height, keyboard focus support, a 44px touch target, accessibility labels in Chinese/English/Russian, and light/dark theme support.
