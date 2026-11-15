# CyberStrikeAI Robot / Chatbot Guide

[中文](../zh-CN/robot.md)

This guide covers **Personal WeChat, WeCom, DingTalk, Lark, Telegram, Slack, Discord, and QQ Bot**, including platform connectivity, RBAC identity binding, service-account allowlists, commands, verification, and troubleshooting.

---

## 1. Where to configure in CyberStrikeAI

1. Log in to the CyberStrikeAI web UI.
2. Open **System Settings** in the left sidebar.
3. Click **Robot settings** (between “Basic” and “Security”).
4. Configure per platform:
   - **Personal WeChat**: Open **WeChat / iLink** → **Generate QR code and bind**, then scan with WeChat (see [Section 3.4](#34-personal-wechat-wechat--ilink))
   - **DingTalk**: Enable and fill in Client ID / Client Secret
   - **Lark**: Enable and fill in App ID / App Secret
5. Click **Apply configuration** to save and automatically restart the corresponding bot connection. WeChat binding saves and enables automatically on success.

Settings are written to the `robots` section of `config.yaml`; you can also edit the file directly. Web-based **Apply configuration** restarts the corresponding connection automatically. Restart the CyberStrikeAI process only when editing YAML directly. Personal WeChat binding automatically writes `robots.wechat` and restarts the iLink long poll.

### Shortest path to first use

After the platform connection works, configure the business identity before sending normal prompts:

- **Multiple users**: choose User binding → each user generates a code from the top-right Web user menu → sends the bind command to the bot → runs `whoami` to verify.
- **Only you**: run `whoami` first and copy the sender ID → choose Service account → set User ID to `admin` or another RBAC user → paste the exact sender allowlist → apply configuration → run `whoami` again.

Start normal AI chat only after the response shows an authorized status and the expected effective identity.

---

## 2. Supported platforms (long-lived / callback)

| Platform       | Description |
|----------------|-------------|
| Personal WeChat| WeChat iLink protocol; scan QR in the web UI to bind, then long-poll for messages—**no public callback URL needed** |
| DingTalk       | Stream long-lived connection; the app connects to DingTalk to receive messages |
| Lark (Feishu)  | Long-lived connection; the app connects to Lark to receive messages |
| WeCom (Qiye WX)| HTTP callback to receive messages; CyberStrikeAI replies via WeCom’s message sending API |
| Telegram       | Bot API long polling (`getUpdates`); **no public callback URL needed** |
| Slack          | Socket Mode (outbound WebSocket); **no public callback URL needed** |
| Discord        | Gateway WebSocket; **no public callback URL needed** |
| QQ Bot         | QQ Open Platform WebSocket (C2C / group @); **no public callback URL needed** |

Section 3 below describes, per platform, what to do in the developer console and which fields to copy into CyberStrikeAI.

---

## 3. Configuration and step-by-step setup

### 3.1 DingTalk

**Important: two types of DingTalk bots**

| Type | Where it’s created | Can do “user sends message → bot replies”? | Supported here? |
|------|-------------------|-------------------------------------------|------------------|
| **Custom bot (Webhook)** | In a DingTalk group: Group settings → Add robot → Custom (Webhook) | No; you can only post to the group | No |
| **Enterprise internal app bot** | [DingTalk Open Platform](https://open.dingtalk.com): create an app and enable the bot | Yes | Yes |

If you only have a **custom bot** Webhook URL (`oapi.dingtalk.com/robot/send?access_token=...`) and sign secret (`SEC...`), **do not** put them into CyberStrikeAI. You must create an **enterprise internal app** in the open platform and obtain **Client ID** and **Client Secret** as below.

---

**DingTalk setup (in order)**

1. **Open DingTalk Open Platform**  
   Go to [https://open.dingtalk.com](https://open.dingtalk.com) and log in with an **enterprise admin** account.

2. **Create or select an app**  
   In the left menu: **Application development** → **Enterprise internal development** → **Create application** (or choose an existing app). Fill in the app name and create.

3. **Get Client ID and Client Secret**  
   - In the left menu open **Credentials and basic info** (under “Basic information”).  
   - Copy **Client ID (formerly AppKey)** and **Client Secret (formerly AppSecret)**.  
   - Use copy/paste; avoid typing by hand. Watch for **0** vs **o** and **1** vs **l** (e.g. `ding9gf9tiozuc504aer` has the digits **504**, not 5o4).

4. **Enable the bot and choose Stream mode**  
   - Left menu: **Application capabilities** → **Robot**.  
   - Turn on “Robot configuration”.  
   - Fill in robot name, description, etc. as required.  
   - **Critical**: set message reception to **“Stream mode”** (流式接入). If you only enable “HTTP callback” or do not select Stream, CyberStrikeAI will not receive messages.  
   - Save.

5. **Permissions and release**  
   - Left menu: **Permission management** — search for “robot”, “message”, etc., and enable **receive message**, **send message**, and other bot-related permissions; confirm.  
   - Left menu: **Version management and release** — if there are unpublished changes, click **Release new version** / **Publish**; otherwise changes do not take effect.

6. **Fill in CyberStrikeAI**  
   - In CyberStrikeAI: System settings → Robot settings → DingTalk.  
   - Enable “Enable DingTalk robot”.  
   - Paste the Client ID and Client Secret from step 3.  
   - Click **Apply configuration**; CyberStrikeAI restarts the DingTalk connection automatically.

---

**Field mapping (DingTalk)**

| Field in CyberStrikeAI | Source in DingTalk Open Platform |
|------------------------|----------------------------------|
| Enable DingTalk robot | Check to enable |
| Client ID (AppKey) | Credentials and basic info → **Client ID (formerly AppKey)** |
| Client Secret | Credentials and basic info → **Client Secret (formerly AppSecret)** |

---

### 3.2 Lark (Feishu)

| Field | Description |
|-------|-------------|
| Enable Lark robot | Check to start the Lark long-lived connection |
| App ID | From Lark open platform app credentials |
| App Secret | From Lark open platform app credentials |
| Verify Token | Optional; for event subscription |

**Lark setup in short**: Log in to [Lark Open Platform](https://open.feishu.cn) → Create an enterprise app → In “Credentials and basic info” get **App ID** and **App Secret** → In “Application capabilities” enable **Robot** and the right permissions → Add **event subscription** and **permissions** below → Publish the app → Enter App ID and App Secret in CyberStrikeAI robot settings → **Apply configuration**.

**Event subscription**  
The long-lived connection only receives message events if you subscribe to them. In the app’s **Events and callbacks** (事件与回调) → **Event subscription** (事件订阅), add the event **Receive message** (**im.message.receive_v1**). Without it, the connection succeeds but no message events are delivered (no logs when users send messages).

**Lark permissions (required)**  
In **Permission management** (权限管理), enable the following (names and identifiers match the Lark console). After changes, **publish a new version** in Version management and release so they take effect.

| Permission name (as shown in console) | Identifier | Notes |
|--------------------------------------|------------|-------|
| 获取与发送单聊、群组消息 (Get and send direct & group messages) | `im:message` | Base permission for sending and receiving; **required**. |
| 接收群聊中@机器人消息事件 (Receive @bot messages in group chat) | `im:message.group_at_msg:readonly` | Required for group chat when users @ the bot. |
| 读取用户发给机器人的单聊消息 (Read direct messages from users to bot) | `im:message.p2p_msg:readonly` | **Required** for 1:1 chat; otherwise no response in private chat. |
| 获取单聊、群组消息 (Get direct & group messages) | `im:message:readonly` | **Required** to read message content. |

**Event subscription** (configured separately): In **Event subscription** (事件订阅), add **Receive message** (**im.message.receive_v1**). Without it, the long-lived connection will not receive message events.

- **1:1 chat**: Open the bot’s private chat in Lark and send e.g. “帮助” or “help”; no @ needed.  
- **Group chat**: Only messages that **@ the bot** are received and replied to.

---

### 3.3 WeCom (Enterprise WeChat)

> WeCom uses a **“HTTP callback + active message send API”** model:  
> - User sends a message → WeCom sends an **encrypted XML callback** to your server (CyberStrikeAI’s `/api/robot/wecom`).  
> - CyberStrikeAI decrypts it, calls the AI, then uses WeCom’s `message/send` API to **actively push the reply** to the user.

**Configuration overview:**

- In the WeCom admin console, create or select a **custom app** (自建应用).
- In that app’s settings, configure the message **callback URL**, **Token**, and **EncodingAESKey**.
- In CyberStrikeAI’s `config.yaml`, fill in:
  - `robots.wecom.corp_id`: your CorpID (企业 ID)
  - `robots.wecom.agent_id`: the app’s AgentId
  - `robots.wecom.token`: the Token used for message callbacks
  - `robots.wecom.encoding_aes_key`: the EncodingAESKey used for callbacks
  - `robots.wecom.secret`: the app’s Secret (used when calling WeCom APIs to send messages)

> **Important: IP allowlist (errcode 60020)**  
> CyberStrikeAI calls `https://qyapi.weixin.qq.com/cgi-bin/message/send` to actively send AI replies.  
> If logs show `errcode 60020 not allow to access from your ip`:
>
> - Your server’s outbound IP is **not in WeCom’s IP allowlist**.  
> - In the WeCom admin console, open the custom app’s **Security / IP allowlist** settings (name may vary slightly), and add the public IP of the machine running CyberStrikeAI (e.g. `110.xxx.xxx.xxx`).  
> - Save and wait for it to take effect, then test again.
>
> If the IP is not whitelisted, WeCom will reject active message sending. You will see that `/api/robot/wecom` receives and processes callbacks, but users **never see AI replies**, and logs contain `not allow to access from your ip`.

---

### 3.4 Personal WeChat (WeChat / iLink)

> Personal WeChat uses **“web QR binding + iLink long polling”**:  
> - Generate a QR code in the CyberStrikeAI web UI → scan and confirm with **WeChat on your phone**;  
> - On success, `robots.wechat` in `config.yaml` is updated automatically and iLink long polling starts (the app connects outbound to `ilinkai.weixin.qq.com`);  
> - **No** public callback URL on your server and **no** WeChat Open Platform app registration required.

**Personal WeChat vs WeCom**

| Item | Personal WeChat (iLink) | WeCom (Enterprise WeChat) |
|------|-------------------------|---------------------------|
| Use case | Private chat in personal WeChat | Custom app in WeCom |
| Setup | QR scan in web UI | Admin console callback URL + Token |
| Public IP needed? | No (outbound long poll only) | Yes (HTTPS callback reachable by WeCom) |
| Config key | `robots.wechat` | `robots.wecom` |

**Binding steps (in order)**

1. **Log in to CyberStrikeAI web UI**  
   **System settings** → **Robot settings** → click the **WeChat / iLink** card.

2. **(Optional) Enable “Enable WeChat robot”**  
   You can skip this on first bind; it is checked automatically after a successful bind.

3. **Generate QR code**  
   Click **“Generate QR code and bind”**. The QR code is valid for about **5 minutes**; regenerate if it expires.

4. **Scan and confirm in WeChat**  
   - Scan the QR code with WeChat on your phone;  
   - Complete confirmation on the phone;  
   - If WeChat shows a **pairing code**, enter it on the web page and click **Submit** (only some accounts need this).

5. **Wait for binding to complete**  
   When the page shows “Binding successful, WeChat robot enabled”, you’re done. `bot_token`, `ilink_bot_id`, etc. are saved to `config.yaml` and the iLink poll restarts automatically—**usually no manual service restart**.

6. **Test in WeChat**  
   Open the **private chat** with the CyberStrikeAI bot in WeChat and send “帮助” (help) or any text.

**Field reference (WeChat)**

| Field | Description |
|-------|-------------|
| Enable WeChat robot | Starts iLink long polling when checked; auto-enabled after bind |
| Generate QR code and bind | Starts the scan-to-bind flow |
| **Advanced** (defaults are fine) | |
| API Base URL | Default `https://ilinkai.weixin.qq.com` |
| Bot Type | Default `3` |
| Bot Agent | Default `CyberStrikeAI/1.0` |
| iLink Bot ID | Filled automatically after bind (read-only) |

**How to use**

- **Private chat only**—send text directly; **no @ needed**.  
- Group @-bot is **not** supported (unlike DingTalk/Lark groups).  
- **Text messages only**; images, voice, etc. are ignored or not supported.

**Re-bind**

- To bind a different WeChat account, click **“Re-bind”** on the robot settings page and scan again.  
- If you see “This WeChat account is already bound”, that account was bound before.

**Common issues**

| Symptom | What to do |
|---------|------------|
| QR code expired | Click “Generate QR code and bind” again (~5 min TTL) |
| Phone asks for a pairing code | Enter the digits shown in WeChat on the web page |
| Bound but no replies | Check logs for `微信 iLink 长轮询已启动` and `微信收到消息`; ensure “Enable WeChat robot” is on |
| No reply after sleep / network drop | Auto-reconnect in ~5–60 s; restart CyberStrikeAI if still stuck |
| Cannot generate QR code | Ensure outbound HTTPS to `https://ilinkai.weixin.qq.com` |

---

### 3.5 Telegram

> Telegram uses **Bot API long polling** (`getUpdates`): the app connects outbound to `api.telegram.org`—**no public callback URL needed**.

1. Create a bot via **@BotFather** (`/newbot`) and copy the **Bot Token**.  
2. CyberStrikeAI → **System settings** → **Robot settings** → **Telegram**.  
3. Enable, paste the token, optionally allow group @ mentions → **Apply configuration**.

---

### 3.6 Slack

> Slack uses **Socket Mode** (outbound WebSocket): requires **Bot Token (xoxb-)** and **App-Level Token (xapp-)** with `connections:write`.

1. Create an app at [api.slack.com](https://api.slack.com/apps) → enable **Socket Mode**.  
2. Create an App-Level Token; install the app to get a Bot Token.  
3. Subscribe to `message.im` and `app_mention` events.  
4. Paste both tokens in CyberStrikeAI → **Apply configuration**.

---

### 3.7 Discord

> Discord uses **Gateway WebSocket**—**no public callback URL needed**.

1. [Discord Developer Portal](https://discord.com/developers/applications) → create app → **Bot** → copy **Token**.  
2. Enable **Message Content Intent** under Privileged Gateway Intents.  
3. Invite the bot with `Send Messages` permission.  
4. Paste token in CyberStrikeAI; optionally allow guild @ mentions → **Apply configuration**.

---

### 3.8 QQ Bot

> QQ Bot uses **QQ Open Platform WebSocket** (official `botgo` SDK) for C2C and group @—**no public callback URL needed**.

1. Create a bot at [q.qq.com](https://q.qq.com) → get **App ID** and **Client Secret**.  
2. Add sandbox testers before going live.  
3. Subscribe to C2C and group @ events (WebSocket).  
4. Fill in CyberStrikeAI; use **Sandbox** for testing → **Apply configuration**.

---

## 4. RBAC authorization and bot commands

Platform credentials and callback signatures authenticate the messaging platform. CyberStrikeAI RBAC determines what the sender can actually do. Each bot instance uses one authorization mode.

### 4.1 Choose an authorization mode

| Scenario | Recommended mode | Identity and data behavior |
|----------|------------------|----------------------------|
| Shared WeCom, Lark, DingTalk, or Slack bot | `user_binding` | Each sender binds their own Web user; permissions and resources remain isolated |
| Personal WeChat, single-user bot, fixed automation entry | `service_account` | Allowlisted senders share the configured RBAC user's permissions and owned resources |

Both modes resolve user status, roles, per-permission scope, and resource assignments before every message. Basic AI chat requires:

```text
agent:execute
chat:read
chat:write
```

Grant project, role, local execution, WebShell, C2, or MCP permissions only when those features are required. Conversation deletion also requires `chat:delete`.

### 4.2 User-binding mode (default)

Administrator:

1. Open System settings → Robot settings → select a platform.
2. Set Authorization policy to `user_binding` and apply the configuration.

Each user:

1. Sign in to the Web UI and open the top-right user menu → **Bind robot account**.
2. Generate a binding code; a five-minute countdown starts.
3. Send the full command to the target bot, for example `bind 7C6E-BD4C`.
4. Send `whoami` and confirm the effective RBAC identity is their own Web user.

Codes are stored only as hashes and are single-use. When the countdown ends, the UI marks the code expired, disables copying, and refreshes the binding list; the server also rejects it. Generating a new code immediately invalidates the previous unused code. Users can send `unbind` or revoke a binding from the Web dialog.

### 4.3 Service-account mode

1. Connect the bot to its messaging platform.
2. Have each intended sender run `whoami` and copy the exact sender ID. For Personal WeChat it usually resembles `xxxx@im.wechat`; never substitute `ilink_bot_id` or configured `ilink_user_id`.
3. In Robot settings, select `service_account`.
4. Enter the RBAC **User ID**, not its display name. `admin` is allowed; every allowlisted sender then receives full platform permissions and the UI shows a red warning.
5. Add one exact sender ID per line. Matching is case-sensitive and `*` wildcards are rejected.
6. Apply configuration and run `whoami` again to verify the effective user, roles, and scope.

Example:

```yaml
robots:
  wechat:
    auth:
      mode: service_account
      service_user_id: admin
      allowed_external_users:
        - "o9cq806s32Sm2_kyOmkyaV7Rn1lU@im.wechat"
```

Service-account mode rejects `bind` and `unbind`. All allowlisted senders share conversations, projects, and other resources owned by the service account. Use `user_binding` when that sharing is undesirable.

### 4.4 Inspect the effective identity

Send `whoami`. The response includes platform, exact sender ID, authorization mode and status, effective RBAC user and ID, roles, scope, and permission count. A non-allowlisted sender sees only the denial status and no service-account details.

### 4.5 Command list

Send these **text commands** to the bot on any connected platform (text only):

| Command | Description |
|---------|-------------|
| **绑定 \<code\>** or **bind \<code\>** | Bind the verified platform sender to the RBAC user that generated the code |
| **解绑** or **unbind** | Remove the current platform identity binding |
| **身份** or **whoami** | Show sender ID, authorization mode, binding status, and the effective RBAC user, roles, and scope |
| **帮助** (help) | Show command help |
| **列表** or **对话列表** (list) | List all conversation titles and IDs |
| **切换 \<conversationID\>** or **继续 \<conversationID\>** | Continue in the given conversation |
| **新对话** (new) | Start a new conversation |
| **清空** (clear) | Clear current context (same effect as new conversation) |
| **当前** (current) | Show current conversation ID and title |
| **停止** (stop) | Abort the currently running task |
| **角色** or **角色列表** (roles) | List all available roles (penetration testing, CTF, Web scan, etc.) |
| **角色 \<roleName\>** or **切换角色 \<roleName\>** | Switch to the specified role |
| **删除 \<conversationID\>** | Delete the specified conversation |
| **版本** (version) | Show current CyberStrikeAI version |

Any other text is sent to the AI as a user message, same as in the web UI (e.g. penetration testing, security analysis).

Group messages are authorized as the actual sender, never as a group ID. In service-account mode, explicitly allowlisted senders intentionally share the configured account.

---

## 5. How to use (do I need to @ the bot?)

- **Personal WeChat**: Send directly in the **private chat** with the bot; **no @ needed** (group chat not supported).  
- **DingTalk / Lark direct chat (recommended)**: **Search for the bot and open a direct chat**. Type “帮助” or any message; **no @ needed**.  
- **DingTalk / Lark group chat**: If the bot is in a group, only messages that **@ the bot** are received and answered; other group messages are ignored.

Summary: **Personal WeChat and direct chat**—just send; **DingTalk/Lark in a group**—@ the bot first, then send.

---

## 6. Recommended flow (so you don’t skip steps)

**Personal WeChat (simplest—no open platform)**

1. CyberStrikeAI web UI → System settings → Robot settings → **WeChat / iLink** → **Generate QR code and bind**.  
2. Scan with WeChat and confirm (enter pairing code on the web page if prompted).  
3. Send `whoami` in the WeChat private chat and copy the sender ID.
4. Choose `user_binding`, or configure `service_account` with the RBAC user and exact sender allowlist.
5. Apply configuration, run `whoami` again, then send a normal message.

**DingTalk / Lark**

1. **In the open platform**: Complete app creation, copy credentials, enable the bot (DingTalk: **Stream mode**), set permissions, and publish (Section 3).  
2. **In CyberStrikeAI**: System settings → Robot settings → Enable the platform, paste Client ID/App ID and Client Secret/App Secret → **Apply configuration**.  
3. **Choose authorization**: use `user_binding` for multiple users, or configure a service account and exact allowlist for a dedicated bot.
4. **Apply configuration**; the Web UI restarts the corresponding connection automatically.
5. **On your phone**: Open the bot, run `whoami` first, then send a normal message.

If the bot does not respond, see **Section 9 (troubleshooting)** and **Section 10 (common pitfalls)**.

---

## 7. Config file example

Example `robots` section in `config.yaml`:

```yaml
robots:
  wechat: # Personal WeChat iLink (auto-filled after QR bind; usually no manual edit)
    enabled: true
    auth:
      mode: service_account
      service_user_id: admin
      allowed_external_users:
        - "exact sender ID copied from whoami"
    bot_token: "your_bot_token@im.bot:..."
    ilink_bot_id: "your_bot_id@im.bot"
    ilink_user_id: "your_user_id@im.wechat"
    base_url: "https://ilinkai.weixin.qq.com"
    bot_type: "3"
    bot_agent: "CyberStrikeAI/1.0"
  dingtalk:
    enabled: true
    auth:
      mode: user_binding
    client_id: "your_dingtalk_app_key"
    client_secret: "your_dingtalk_app_secret"
  lark:
    enabled: true
    auth:
      mode: user_binding
    app_id: "your_lark_app_id"
    app_secret: "your_lark_app_secret"
    verify_token: ""
  wecom:
    enabled: false
    corp_id: ""
    agent_id: 0
    token: ""
    encoding_aes_key: ""
    secret: ""
  telegram:
    enabled: false
    bot_token: ""
    allow_group_messages: false
  slack:
    enabled: false
    bot_token: ""
    app_token: ""
  discord:
    enabled: false
    bot_token: ""
    allow_guild_messages: false
  qq:
    enabled: false
    app_id: ""
    client_secret: ""
    sandbox: true
```

Authorization is configured independently per platform; omitting `auth` defaults to `user_binding`. **Apply configuration** restarts the corresponding connections. Restart the process only after editing YAML directly. Personal WeChat QR binding saves and restarts automatically.

---

## 8. Testing without DingTalk/Lark installed

You can verify bot logic with the **test API** (no DingTalk/Lark client needed):

1. Sign in with an account that has global `robot:write` permission and obtain a Bearer token.
2. Call the test endpoint with curl:

```bash
# Adjust the URL, username, and password for your deployment
TOKEN=$(curl -s -X POST "http://localhost:8080/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"YOUR_PASSWORD"}' | jq -r '.token')

curl -X POST "http://localhost:8080/api/robot/test" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"platform":"dingtalk","user_id":"test_user","text":"帮助"}'
```

If the JSON response contains `"reply":"【CyberStrikeAI 机器人命令】..."`, command handling works. `help`, `version`, and `whoami` work before binding. `list`, `current`, and normal AI messages enforce RBAC: the test `platform + user_id` must already be bound or exactly match the service-account allowlist.

API: `POST /api/robot/test` (requires global `robot:write`). Body: `{"platform":"optional","user_id":"optional","text":"required"}`. Response: `{"reply":"..."}`. This endpoint simulates bot business logic only; it does not validate a third-party callback signature or long-lived connection.

---

## 9. Troubleshooting: no response when sending messages

### 9.1 Personal WeChat

Check in this order:

1. **Binding completed?**  
   Robot settings should show “Connected” or a bound Bot ID; `robots.wechat.bot_token` in `config.yaml` must not be empty.

2. **Enabled?**  
   Confirm “Enable WeChat robot” is checked and click **Apply configuration** if you just changed settings.

3. **Application logs**  
   - On startup: `微信 iLink 长轮询已启动`;  
   - After sending a message: `微信收到消息`; if missing, binding may have failed or `bot_token` is invalid—try **Re-bind**.  
   - `微信 iLink 长轮询异常，将自动重连`: wait for auto-reconnect or restart.

4. **Network**  
   The server must reach `https://ilinkai.weixin.qq.com` (outbound HTTPS). If QR generation fails, check this first.

5. **After sleep or network drop**  
   Same as DingTalk/Lark: **auto-reconnect** in ~5–60 s; restart if still no response.

### 9.2 DingTalk

Check in this order:

0. **After laptop sleep or network drop**  
   DingTalk and Lark both use long-lived connections; they break when the machine sleeps or the network drops. The app **auto-reconnects** (retries within about 5–60 seconds). After wake or network recovery, wait a moment before sending; if there is still no response, restart the CyberStrikeAI process.

1. **Client ID / Client Secret match the open platform exactly**  
   Copy from “Credentials and basic info”; avoid typing. Watch **0** vs **o** and **1** vs **l** (e.g. `ding9gf9tiozuc504aer` has **504**, not 5o4).

2. **Did you apply the configuration?**  
   Web changes require **Apply configuration**, which restarts the corresponding connection automatically. Restart the process only after editing `config.yaml` directly.

3. **Application logs**  
   - On startup you should see: `钉钉 Stream 正在连接…`, `钉钉 Stream 已启动（无需公网），等待收消息`.  
   - If you see `钉钉 Stream 长连接退出` with an error, it’s usually wrong **Client ID / Client Secret** or **Stream not enabled** in the open platform.  
   - After sending a message in DingTalk, you should see `钉钉收到消息` in the logs; if not, the platform is not pushing to this app (check that the bot is enabled and **Stream mode** is selected).

4. **Open platform**  
   The app must be **published**. Under “Robot” you must enable **Stream** for receiving messages (HTTP callback only is not enough). Permission management must include robot receive/send message permissions.

### 9.3 Reply says unbound, sender denied, or permission missing

1. Run `whoami` and inspect the authorization mode and status.
2. In `user_binding`, generate a code from the top-right Web user menu and send the complete bind command from the same platform identity. Regenerate expired or already-used codes.
3. In `service_account`, copy the exact sender ID from `whoami` into that platform's allowlist. Preserve case, tenant prefixes, and suffixes such as `@im.wechat`.
4. If an effective user is shown but permissions are missing, grant at least `agent:execute`, `chat:read`, and `chat:write` for normal AI chat.
5. A missing or disabled service user is rejected when applying configuration.
6. If `admin` is denied, the usual cause is an allowlist mismatch—not insufficient admin permissions.

---

## 10. Common pitfalls

- **Personal WeChat vs WeCom**: Personal WeChat uses `robots.wechat` + web QR bind; WeCom uses `robots.wecom` + admin callback URL—they are completely different.  
- **WeChat QR expired**: QR codes last ~5 minutes; regenerate instead of reusing an old one.  
- **Wrong bot type**: The “Custom” bot added in a DingTalk **group** (Webhook + sign secret) **cannot** be used for two-way chat. Only the **enterprise internal app** bot from the open platform is supported.  
- **Configuration not applied**: Click **Apply configuration** after Web changes; connections restart automatically. A process restart is needed only for direct YAML edits.  
- **Bot ID used as sender ID**: Copy the sender ID from `whoami`; do not use `ilink_bot_id`, configured `ilink_user_id`, a group ID, or a display name.  
- **Reusing an expired code**: Codes last five minutes and are single-use; generating a new code immediately invalidates the old one.  
- **Assuming service-account users are isolated**: All allowlisted senders share that account's conversations and owned resources. Use `user_binding` for isolation.  
- **Assuming admin removes the allowlist**: It does not. The sender must still match exactly, but every matching sender gets full permissions.  
- **Client ID typo**: If the platform shows `504`, use `504` (not `5o4`); prefer copy/paste.  
- **DingTalk: only HTTP callback, no Stream**: This app receives messages via **Stream**. In the open platform, message reception must be **Stream mode**.  
- **App not published**: After changing the bot or permissions in the open platform, **publish a new version** under “Version management and release”, or changes won’t apply.

---

## 11. Notes

- All platforms: **text messages only**; other types (e.g. image, voice) are not supported and may be ignored.  
- Personal WeChat: **private chat only**—group @-bot is not supported.  
- Bot data is shared with the web UI: under `user_binding` it belongs to the bound user; under `service_account` it belongs to the service account and is shared by allowlisted senders.  
- Bot execution uses the same **Pi single/multi-agent** path as the web UI (`ProcessMessageForRobot`, with progress callbacks and process details stored in the DB); only the final reply is sent back to personal WeChat/DingTalk/Lark/WeCom in one message (no SSE). Default: `robot_default_agent_mode: single`.
