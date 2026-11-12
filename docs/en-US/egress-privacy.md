# Egress Privacy Gateway — hide the operator's real IP

The egress privacy gateway routes **all outbound traffic** CyberStrikeAI makes
through an anonymizing proxy (Tor, a SOCKS5 proxy, or an HTTP proxy), so the
operator's real IP is never exposed while the platform is active. When the
upstream proxy is unreachable and **fail-closed** is on, traffic is blocked
rather than leaking out on the real interface.

> Use only against systems you own or are explicitly authorized to test.

---

## Threat model

Without the gateway, every outbound connection — LLM API calls, recon HTTP
clients, FOFA queries, WebShell traffic to a target, bot-platform APIs, and
spawned CLI tools — leaves on your real IP. The gateway closes those leaks in
three layers:

| Layer | Covers | Mechanism |
|-------|--------|-----------|
| 1. HTTP transport | Every Go `http.Client` in the platform | `egress.Default().Transport()` routes dials through the proxy |
| 2. Proxy env | Spawned tools that honor `HTTP_PROXY`/`ALL_PROXY` (curl, wget, python-requests, go) | Injected into the tool environment |
| 3. proxychains | Raw-socket tools that ignore env (nmap, masscan, many scanners) | `proxychains` wraps the process; forces TCP `connect()` through the proxy |

---

## Configuration

Add an `egress:` block to your `config.yaml`:

```yaml
egress:
  enabled: true              # master switch
  mode: tor                  # socks5 | tor | http | https | direct
  address: "127.0.0.1:9050"  # proxy host:port (empty + mode=tor => 127.0.0.1:9050)
  username: ""               # optional proxy username
  password: ""               # optional proxy password
  fail_closed: true          # kill-switch: block outbound if the proxy is down
  dns_through_proxy: true    # resolve hostnames at the proxy (SOCKS5/Tor) so DNS does not leak
  dial_timeout_sec: 30       # per-dial timeout in seconds (0 => default 30s)
  proxychains_mode: auto     # wrap raw-socket tools with proxychains: auto (default) | off
```

On startup the server logs the gateway state and runs a **leak self-test** in
the background:

```
egress privacy gateway active   mode=tor fail_closed=true
egress leak self-test passed     detail=OK: exit IP 185.x.x.x differs from real IP 203.x.x.x
```

If it cannot confirm anonymization you will instead see
`egress leak self-test: REAL IP MAY BE EXPOSED`.

---

## Important limits (read before you scan)

**proxychains tunnels TCP `connect()` only.** It cannot tunnel what the kernel
emits as raw packets:

- **nmap SYN scan (`-sS`), ping host-discovery (default), and OS detection
  (`-O`) use raw sockets and bypass the proxy — they WILL leak your real IP.**
  To stay anonymous, force a TCP connect scan with no ping and remote DNS:

  ```bash
  nmap -sT -Pn -n -p 80,443 <target>
  ```

  `-sT` connect scan (goes through proxychains) · `-Pn` no ping · `-n` no local
  DNS. Prefer giving targets by IP.
- **UDP and ICMP are not tunneled.** UDP scans, `ping`, and traceroute leak.
- For a guarantee that holds **regardless of tool behavior**, add an OS-level
  kill-switch (below). The application-level gateway reduces leaks; the firewall
  makes a leak impossible.

### OS-level kill-switch (belt and suspenders)

Drop every outbound packet that is not destined for the Tor/proxy process. On
Linux, route the box through Tor's `TransPort`/`DNSPort`, or with a simple
`iptables` policy allow only the proxy and loopback:

```bash
# Allow loopback and established, allow only the 'debian-tor' user's traffic out,
# drop everything else. Adjust the user/uid to your Tor service account.
sudo iptables -F OUTPUT
sudo iptables -A OUTPUT -o lo -j ACCEPT
sudo iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
sudo iptables -A OUTPUT -m owner --uid-owner debian-tor -j ACCEPT
sudo iptables -P OUTPUT DROP
```

With this in place, any tool that tries to send a raw packet directly simply
fails instead of leaking.

---

## Running on Kali Linux under WSL (Windows 11 host)

This is the recommended setup: **Windows 11** as the host OS, **Kali Linux in
WSL2**, CyberStrikeAI and Tor both running inside the Kali WSL instance.

### 1. Install Kali on WSL

In **PowerShell (Windows 11)**:

```powershell
wsl --install -d kali-linux
wsl --set-default-version 2
```

Launch Kali, create your user, then update:

```bash
sudo apt update && sudo apt full-upgrade -y
```

### 2. Install Tor and proxychains inside Kali

```bash
sudo apt install -y tor proxychains4 curl
```

Start Tor (it listens on `127.0.0.1:9050` by default):

```bash
sudo service tor start
sudo service tor status      # confirm it is running
```

Verify Tor works and gives you a different IP:

```bash
# Real IP (direct):
curl -s https://api.ipify.org ; echo
# IP through Tor:
curl -s --socks5-hostname 127.0.0.1:9050 https://api.ipify.org ; echo
```

The two must differ. `--socks5-hostname` also resolves DNS at the proxy.

### 3. Build and configure CyberStrikeAI

Install Go (1.25+) if needed, then clone and build inside Kali:

```bash
git clone https://github.com/AIPentest/CyberStrikeAI.git
cd CyberStrikeAI
cp config.example.yaml config.yaml
```

Edit `config.yaml` and enable the gateway:

```yaml
egress:
  enabled: true
  mode: tor
  address: "127.0.0.1:9050"
  fail_closed: true
  dns_through_proxy: true
  proxychains_mode: auto
```

Build and run:

```bash
go build -o cyberstrike ./cmd/server
./cyberstrike            # or: ./run.sh
```

Watch the startup log for:

```
egress privacy gateway active   mode=tor fail_closed=true
egress leak self-test passed
```

### 4. Use it from Windows 11

WSL2 forwards `localhost`, so open the web console from your Windows browser at
the address the server prints (e.g. `http://localhost:8080`). All work you drive
from the console now exits through Tor.

### 5. (Recommended) Add the kill-switch

Apply the `iptables` policy from the section above inside Kali so that even a
misbehaving tool cannot bypass Tor. Note: under WSL2 the default kernel ships
with iptables; if a rule module is missing, run
`sudo update-alternatives --set iptables /usr/sbin/iptables-legacy` first.

### 6. nmap through the gateway

Because nmap's default scans use raw sockets, always run connect scans:

```bash
nmap -sT -Pn -n -p 1-1000 <target-ip>
```

When CyberStrikeAI spawns nmap for you, it is already wrapped with proxychains;
keep your scan arguments to `-sT -Pn -n` so nothing leaks.

---

## Verifying no leak at any time

- **Startup log:** the self-test runs automatically and reports the exit IP.
- **Manual (inside Kali):**
  ```bash
  curl -s --socks5-hostname 127.0.0.1:9050 https://api.ipify.org ; echo   # should be the Tor IP
  curl -s https://api.ipify.org ; echo                                     # should be blocked by the kill-switch
  ```
- If `fail_closed: true` and Tor is stopped, platform outbound requests fail
  (by design) instead of falling back to your real IP.

---

## Rotating your exit IP

With Tor, request a new circuit by reloading Tor (or via the control port):

```bash
sudo service tor reload
```

Each reload gives the platform a fresh exit IP on new connections.
