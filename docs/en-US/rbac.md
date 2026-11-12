# CyberStrikeAI RBAC Administration Guide

[中文](../zh-CN/rbac.md)

CyberStrikeAI can execute Agents, MCP tools, WebShell operations, C2 actions, and batch jobs. RBAC therefore applies beyond navigation visibility: it is enforced across HTTP APIs, resource queries, Agent contexts, built-in and external MCP tools, background jobs, and chatbot execution.

---

## 1. Two different kinds of roles

| Concept | Management location | Purpose |
|---------|---------------------|---------|
| **Platform role (RBAC Role)** | **Platform permissions** | Controls which features and resources a user may access |
| **AI testing role (Agent Role)** | **Roles** / `roles/*.yaml` | Controls Agent prompts, methodology, and candidate tools |

An AI testing role is not an authorization boundary. Selecting a penetration-testing role does not grant platform permissions, and granting RBAC permissions does not change the Agent prompt.

---

## 2. Authorization model

An operation is allowed only when all relevant checks pass:

```text
enabled account
  + required permission for the route/tool
  + scope attached to that permission
  + resource owner / explicit assignment / parent inheritance
  + additional rules for process-global operations
```

Request flow:

1. Login issues a Bearer token whose session contains user, roles, permissions, and per-permission scopes.
2. HTTP middleware maps the route to a permission, for example `GET /api/projects` → `project:read`.
3. Resource-ID requests also check ownership, explicit assignments, or supported parent inheritance.
4. Agent execution receives an immutable Principal through `context.Context`.
5. Built-in MCP tools authorize both the tool and resource IDs in tool arguments. External MCP has separate restrictions.
6. Denials are written to RBAC/audit logs.

Frontend button hiding is only a usability feature. The server is the security boundary.

---

## 3. Built-in platform roles

| Role | Scope | Default capability |
|------|-------|--------------------|
| **Administrator `admin`** | `all` | Every known permission, including RBAC, configuration, terminal, audit deletion, and global definition management |
| **Operator `operator`** | `assigned` | Normal read/write/execute work; excludes RBAC, core configuration, terminal, audit management, external MCP execution, and several global definition writes |
| **Auditor `auditor`** | `all` | Read permissions across modules plus `audit:read`; no writes |
| **Viewer `viewer`** | `assigned` | Read-only access within authorized resources |

System roles cannot be edited or deleted. Their grants are rebuilt from the current permission catalog during upgrade, preventing stale grants from older versions. Create custom roles for different job functions.

The built-in `admin` account is the permanent management fallback. It cannot be disabled, deleted, or stripped of its system `admin` role. These updates are rejected through both the UI and API, preserving the account details, permissions, and valid sessions. Display name and password updates, and role changes that retain `admin`, remain available. Other accounts can still be disabled or have their roles changed. This protection does not automatically repair previously missing role bindings.

An account without a role can still authenticate but has almost no business capability; do not treat “no role” as a complete job profile.

---

## 4. Permission catalog

Permissions use `module:action`. Common actions are `read`, `write`, `delete`, and `execute`. The authoritative catalog for the running build is available in Platform permissions or `GET /api/rbac/metadata`.

| Module | Permissions |
|--------|-------------|
| Account | `auth:self` |
| Dashboard | `dashboard:read` |
| Chat | `chat:read`, `chat:write`, `chat:delete` |
| Agent | `agent:execute`, `agent:local-execute` |
| HITL | `hitl:read`, `hitl:write` |
| Tasks | `tasks:read`, `tasks:write`, `tasks:delete` |
| Projects | `project:read`, `project:write`, `project:delete` |
| Vulnerabilities | `vulnerability:read`, `vulnerability:write`, `vulnerability:delete` |
| WebShell | `webshell:read`, `webshell:write`, `webshell:delete` |
| C2 | `c2:read`, `c2:write`, `c2:delete` |
| MCP | `mcp:read`, `mcp:execute`, `mcp:write`, `mcp:external:execute` |
| One-click update (local source) | `update:read`, `update:apply` |
| Knowledge | `knowledge:read`, `knowledge:write`, `knowledge:delete` |
| Skills | `skills:read`, `skills:write`, `skills:delete` |
| Markdown Agents | `agents:read`, `agents:write`, `agents:delete` |
| AI testing roles | `roles:read`, `roles:write`, `roles:delete` |
| Workflows | `workflow:read`, `workflow:execute`, `workflow:write`, `workflow:delete` |
| Configuration | `config:read`, `config:write` |
| Terminal | `terminal:execute` |
| Audit | `audit:read`, `audit:delete` |
| RBAC | `rbac:read`, `rbac:write` |
| Notifications | `notification:read`, `notification:write` |
| Robots | `robot:read`, `robot:write` |
| Files | `files:read`, `files:write`, `files:delete` |
| Attack chain | `attackchain:read`, `attackchain:write` |
| Network-space search / Reconnaissance | `fofa:execute` |
| OpenAPI | `openapi:read` |
| Monitor | `monitor:read`, `monitor:write`, `monitor:delete` |

Important distinctions:

- `agent:execute` runs Agents but does not grant local filesystem, shell, or arbitrary configured command access.
- `agent:local-execute` is the local execution fallback and should be limited to trusted operators.
- `mcp:execute` protects the authenticated MCP HTTP entry point.
- `mcp:external:execute` allows Agent calls to external MCP tools and currently also requires `all` scope.
- `fofa:execute` is kept for backward compatibility, but it now protects the Reconnaissance page for FOFA, ZoomEye, Quake, and Shodan searches.
- `mcp:write` manages external MCP configuration; it is separate from external tool execution.
- One-click update is two permissions: `update:read` shows the installed version and the gap against
  the update source, while `update:apply` pulls the configured source repository's default branch
  (the official repository by default), rebuilds the platform binary and swaps it. The mutating side additionally requires the session to hold that permission
  with `all` scope - one machine has one source tree, so an `assigned`/`own` session must not be able
  to move the code everybody else is running.
- `robot:write` manages robot configuration and the test endpoint. Chatbot conversations use the bound user or configured service account's business permissions.

---

## 5. Resource scopes

Each role has one scope:

| Scope | Meaning | Typical use |
|-------|---------|-------------|
| `all` | All resources covered by the permission | Administrator, global auditor |
| `assigned` | Explicitly assigned resources and supported parent-resource inheritance | Project member, assigned asset operator |
| `own` | Primarily resources created by/owned by the user; some resource types also support explicit assignment or parent inheritance | Personal workspace, isolated robot identity |

Users may have multiple roles. Permissions are unioned, while scopes are merged **for the same permission only**:

```text
all > assigned > own
```

Example:

```text
Global audit role:  project:read  + all
Personal editor:    project:write + own

Effective:
project:read  → all
project:write → own
```

A global read role does not widen an unrelated write permission. Authorization code must use `ScopeFor(permission)`, not the user's broadest display scope.

### Process-global restrictions

Some definitions have no owner. Their mutations require the corresponding permission with `all` scope even if the user has a `write` key:

- AI testing roles, Skills, and Markdown Agents.
- External MCP configuration.
- Robot configuration.
- Workflow definitions.
- Knowledge mutations other than search.
- Global HITL allowlist, reviewer, and audit policy.
- C2 Profile mutations.
- One-click update mutations (`check`/`apply`/`rollback`, i.e. `update:apply`).
- Some global monitor statistics.

---

## 6. Ownership, assignments, and inheritance

Use Platform permissions → Member details → Resource assignments. Directly assignable resource types include:

- `project`
- `conversation`
- `vulnerability`
- `webshell`
- `batch_task`
- `c2_listener`

A batch request accepts at most 100 resources. Duplicate grants are skipped.

Supported inheritance includes:

| Child resource | Parent access source |
|----------------|----------------------|
| Conversation | Project |
| Vulnerability | Project or related conversation |
| Message, process detail, attack chain | Conversation |
| C2 Session | Listener |
| C2 Task/file/event | Session, Task, or Listener chain |

Assigning a project therefore usually avoids assigning each conversation and vulnerability separately. The concrete route/tool server check remains authoritative.

---

## 7. Web administration workflow

### Create a user

1. Sign in as an administrator and open **Platform permissions**.
2. Create a user with username, display name, an eight-character-or-longer password, and enabled status.
3. Assign one or more platform roles.
4. For `assigned` roles, configure resource assignments.
5. Have the user sign in again and verify roles, permission count, and scope in the top-right user menu.

### Create a custom role

1. Give the role a job-oriented name and description.
2. Select `all`, `assigned`, or `own`.
3. Select only required permissions.
4. Test list, detail, mutation, deletion, Agent, and tool behavior with a test account.
5. Assign it to production users only after verification.

System roles are immutable; create a custom role instead of modifying them.

### When changes take effect

- Updating a user, password, enabled state, or role membership revokes that user's sessions; they must sign in again.
- Changing a custom role's permissions or scope, or deleting it, revokes only its members' sessions; those users must sign in again. Unrelated users remain signed in. Name, description, and permission-order changes do not revoke sessions, and deleting a role with no members leaves existing sessions valid.
- Robots resolve the bound user/service account on every message, so disablement and role changes affect the next message.
- Background batch jobs resolve a Principal from the task owner rather than trusting frontend state.

---

## 8. Suggested role templates

### Read-only project member

```text
Scope: assigned
dashboard:read
chat:read
project:read
vulnerability:read
files:read
attackchain:read
```

### Daily security operator

```text
Scope: assigned
agent:execute
chat:read / chat:write
project:read / project:write
vulnerability:read / vulnerability:write
tasks:read / tasks:write
files:read / files:write
hitl:read / hitl:write
```

Add `agent:local-execute` or `terminal:execute` only when local commands are required. Add individual `:delete` permissions only when deletion is part of the job.

### Robot service account

```text
Scope: own (isolated workspace) or assigned (specific projects)
agent:execute
chat:read / chat:write
optional project, vulnerability, and knowledge permissions
```

`admin` can be used as a robot service account, but exact sender allowlisting still applies. Every allowlisted sender receives full permissions and shares admin-owned data. See the [Robot guide](robot.md).

---

## 9. Agent, MCP, and robot boundaries

### Agent

The HTTP user becomes an immutable Principal propagated to single-agent, multi-agent, workflow, and tool contexts. A long-running task may survive an SSE disconnect while retaining that identity.

### Built-in MCP

Every built-in tool requires an explicit authorization policy. WebShell tools check both `webshell:read/write/delete` and the target `connection_id`; project, vulnerability, task, and C2 tools validate resource arguments as well. An unregistered built-in policy fails closed. Other local/configured tools require `agent:local-execute`.

### External MCP

Agent calls to external MCP require `mcp:external:execute` with `all` scope because an external service's resource model is not protected by local ownership and assignments.

### Robots

- `user_binding`: each platform sender binds their own RBAC user.
- `service_account`: exact allowlisted senders share one RBAC user.
- Platform signature verification authenticates message origin, not business authorization.
- Run `whoami` to inspect the effective Principal.

---

## 10. RBAC API

All requests use:

```http
Authorization: Bearer <token>
```

Management routes require `rbac:read` or `rbac:write`; the resource picker requires `rbac:write`.

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/rbac/me` | Current user, roles, permissions, overall scope, per-permission scopes |
| GET | `/api/rbac/metadata` | Permission catalog, roles, grants, and scopes |
| GET/POST | `/api/rbac/users` | List/create users |
| PUT/DELETE | `/api/rbac/users/:id` | Update/delete a user |
| GET/POST | `/api/rbac/roles` | List/create roles |
| PUT/DELETE | `/api/rbac/roles/:id` | Update/delete a custom role |
| GET | `/api/rbac/resources?type=project&q=...` | Search assignable resources |
| GET/POST | `/api/rbac/resource-assignments` | List/create assignments |
| DELETE | `/api/rbac/resource-assignments/:id` | Revoke an assignment |

Create a user:

```bash
curl -X POST http://localhost:8080/api/rbac/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "operator01",
    "display_name": "Security Operator 01",
    "password": "change-me-123",
    "enabled": true,
    "roles": ["operator"]
  }'
```

Create a custom role:

```bash
curl -X POST http://localhost:8080/api/rbac/roles \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Project Auditor",
    "description": "Read assigned projects",
    "scope": "assigned",
    "permissions": ["chat:read", "project:read", "vulnerability:read"]
  }'
```

Assign projects:

```bash
curl -X POST http://localhost:8080/api/rbac/resource-assignments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "USER_ID",
    "resource_type": "project",
    "resource_ids": ["PROJECT_ID_1", "PROJECT_ID_2"]
  }'
```

---

## 11. Audit and operations recommendations

- Use individual administrator accounts instead of sharing one password.
- Name custom roles by job function and document purpose/owner.
- Review high-risk permissions separately: `terminal:execute`, `agent:local-execute`, `c2:write/delete`, `webshell:write/delete`, `rbac:write`, and `config:write`.
- Periodically review `all` roles, service accounts, robot allowlists, and dormant users.
- On offboarding, disable the account first, then revoke robot bindings, assignments, and sessions.
- Monitor RBAC denials, user/role changes, resource assignments, and robot service-account execution in audit logs.
- Pair RBAC with HITL for dangerous tools; permission to invoke does not bypass approval policy.

---

## 12. Troubleshooting

### A button is missing

The frontend hides actions based on `/api/rbac/me`. Verify the required permission. Direct API calls are still rejected server-side.

### Permission exists but the resource is denied

Inspect the scope for that specific permission, not only the overall display scope. Then check owner, explicit assignment, and parent assignment.

### Role changed but the user sees old access

Role permission or scope changes revoke that role's members' sessions. Affected users must sign in again. Robots resolve again on the next message.

### The built-in `admin` password is lost

Prefer resetting it from another administrator account with `rbac:write`. If no administrator session is available, follow the [administrator password recovery procedure](troubleshooting.md#recover-a-forgotten-admin-password) on the server.

### A global mutation is denied despite `write`

Process-global definitions require the corresponding permission with `all` scope. Create a dedicated global administration role instead of widening unrelated permissions.

### Agent chat works but commands fail

`agent:execute` and `agent:local-execute` are separate. Grant local execution only when necessary and combine it with HITL, tool allowlists, and audit.

### External MCP requires global scope

The user needs `mcp:external:execute`, and that permission's scope must be `all`.
