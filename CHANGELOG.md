# Changelog

## Unreleased

### Added

- Added async conversation APIs for single-agent and multi-agent: requests return a task receipt on acceptance, and client disconnects do not stop execution; supports per-user isolated idempotent submission, persistent state and result queries, explicit cancellation, and interrupted-state recovery after service restarts. Synchronous interfaces remain compatible.

- At startup, all project management tables are checked automatically against the unified latest SQLite schema: missing tables are created and missing columns added, with no per-column migration list to maintain; existing data and extra tables/columns are preserved; a failed structural repair rolls back and blocks startup. HITL and notification tables are included in the unified initialization, and the historical vulnerability foreign-key migration preserves extra columns, indexes, triggers, and dependent records.

### Fixed

- Fixed official OpenAI models known to support `none` still applying server-side default reasoning after reasoning was disabled (#366): the explicit disable parameter is now sent and preserved per provider, endpoint, and model, covering the Planner and forced tool choice; DeepSeek and third-party compatible-gateway behavior is preserved, and the tool-calling limitation that requires Responses is now clearly noted.

- Prevented the built-in admin from losing privileges: deactivating the built-in `admin` or removing its system administrator role is rejected, and on failure the original profile, permissions, and sessions are preserved; the UI disables the corresponding actions accordingly.

- Fixed asset-management intelligence collection failing because the default FOFA request does not support the `province` return field; `region` is now used uniformly; legacy form/API field aliases remain compatible, imported assets keep their province, and the intelligence field default is corrected to `asn/org`.

- Fixed single-agent, multi-agent, role workflow, and bulk tasks mistaking a lower-layer call timeout for a whole-task timeout: only expiry of the task context's own deadline marks a task timed out; lower-layer failures keep their original diagnostics; overall timeout and existing retry policies are unchanged.

- C2 Profile and WebShell deletes now uniformly return 404 for missing objects and a generic 500 message on database failures, without exposing SQL errors; vulnerability and bulk-task lists return `[]` when no records match, with added HTTP contract regression verification.

- Fixed stale drafts overwriting others' edits when multiple people edit call-interception rules: only one save per version succeeds; on conflict the draft is kept and the user is prompted to refresh; API saves must carry the ETag/If-Match from the read.

- Fixed platform role updates/deletes logging every user out: only the sessions of members of the affected role are revoked; sessions stay valid when the name, description, or equivalent permission set changes.

- Fixed Russian and other long strings overflowing dashboard status cards, risk badges, and queue labels; side bars and risk names now wrap properly, and the layout adapts to the card's available width.

- Self-update recovery now strictly verifies the backup record, State, commit, and binary pairing before overwriting files; missing, duplicate, or contradictory evidence is rejected, leaving a recoverable state intact.
- Editing a stopped external MCP no longer inadvertently starts a connection; start/stop, config edits, and application restarts keep a consistent started/stopped state.

- The login password reveal/hide button is now an eye icon inside the input field, with unified input width and height, keyboard focus support, a 44px touch area, accessible labels in Chinese, English, and Russian, and light/dark theme support.
