## CyberStrikeAI Burp Suite Extension

中文说明见：`README.zh-CN.md`

### What it does

- Configure **Host / Port / HTTPS / Username / Password** and choose an agent mode
- Click **Validate** to login (`POST /api/auth/login`) and verify token (`GET /api/auth/validate`)
- Right-click any HTTP message in Burp and send it to CyberStrikeAI for **streaming web pentest** (agent modes: **Eino Single**, Deep, Plan-Execute, Supervisor — maps to `/api/eino-agent/stream` or `/api/multi-agent/stream`)
- Keep a **test history sidebar** (searchable) so you can revisit previous runs
- Output is split into **collapsible Progress** + **Final Response** (Markdown rendering supported)
- View captured **Request / Response** for each run
- **Stop** a running task (calls `/api/agent-loop/cancel` once `conversationId` is available)

### Build

Requirements:

- JDK 11+
- Maven (recommended) OR Burp Extender API jar (offline mode)

#### Option A (recommended): Maven build (no need to locate Burp)

```bash
cd plugins/burp-suite/cyberstrikeai-burp-extension
./build-mvn.sh
```

Output:

- `dist/cyberstrikeai-burp-extension.jar`

#### Option B: Offline build with `build.sh` (needs Burp API jar)

1) Create `lib/` and copy Burp's API jar into it:

```bash
mkdir -p lib
# copy from your Burp installation, for example:
# cp "/path/to/burp-extender-api.jar" lib/
```

2) Build:

```bash
cd plugins/burp-suite/cyberstrikeai-burp-extension
./build.sh
```

Output:

- `dist/cyberstrikeai-burp-extension.jar`

#### Option C: Gradle (optional)

If you already have Gradle available, you can still use `build.gradle` to build.

### Load in Burp Suite

- Burp Suite → **Extensions** → **Installed** → **Add**
- Extension type: **Java**
- Select the jar above

### Notes

- Default connection is `https://127.0.0.1:8080` (**HTTPS** checked). Self-signed / local certs are trusted automatically (no import).
- Uncheck **HTTPS** only if your server runs plain HTTP.
- It uses **Bearer Token** authentication obtained from the configured password.


### RBAC login

Use the same username and password as the web UI. Username defaults to `admin`; enter your own username for a custom account. Changing connection settings or credentials clears the cached token and requires Validate again. Passwords are not persisted.

If the server reports a missing username and the plugin only has Password, rebuild and reload the JAR with the Username field. Entering `username:password` in Password does not work.

### Authentication regression tests

From the repository root, run `bash scripts/test-plugin-auth.sh` (Node.js and JDK 11+). This tests both clients against local HTTP fixtures, username validation, credential escaping, settings migration, and Burp account-switch invalidation. The script downloads the compile-only Burp API; set `BURP_API_JAR` to an existing JAR for offline use.
