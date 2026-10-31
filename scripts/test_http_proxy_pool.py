"""Local-only integration tests for the exact Python shipped in the tool YAML.

Run: python3 -m unittest discover -s scripts -p 'test_http_proxy_pool.py' -v
Requires: httpx[http2,socks], PyYAML; openssl for the CONNECT/TLS fixture.
"""

import contextlib
import datetime
import email.utils
import http.server
import importlib.util
import io
import json
import os
from pathlib import Path
import select
import shutil
import socket
import socketserver
import ssl
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest import mock

import httpx
import yaml


ROOT = Path(__file__).resolve().parents[1]
TOOL = yaml.safe_load((ROOT / "tools/http-framework-test.yaml").read_text())
SCRIPT = TOOL["args"][1]
NS = {"__name__": "http_framework_tool"}
exec(compile(SCRIPT, "http-framework-test.yaml", "exec"), NS)
Pool = NS["ProxyPoolTransport"]


def relay(connection, upstream):
    while True:
        readable, _, _ = select.select([connection, upstream], [], [], 3)
        if not readable:
            return
        for current in readable:
            data = current.recv(65536)
            if not data:
                return
            (upstream if current is connection else connection).sendall(data)


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def respond(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length) if length else b""
        self.server.requests.append((self.command, self.path, self.headers, body))
        status, headers, body = self.server.reply(self)
        self.send_response(status)
        self.send_header("Content-Length", str(len(body)))
        for key, value in headers.items():
            self.send_header(key, value)
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    do_GET = do_POST = do_HEAD = do_OPTIONS = do_PUT = do_DELETE = respond

    def do_CONNECT(self):
        self.server.requests.append(("CONNECT", self.path, self.headers, b""))
        # Connect only to the local TLS fixture, regardless of the requested host.
        with socket.create_connection(("127.0.0.1", self.server.tunnel_port), timeout=3) as upstream:
            self.send_response(200)
            self.end_headers()
            self.wfile.flush()
            self.close_connection = True
            relay(self.connection, upstream)


class SocksHandler(socketserver.StreamRequestHandler):
    def handle(self):
        version, count = self.rfile.read(2)
        self.rfile.read(count)
        if version != 5:
            return
        self.wfile.write(b"\x05\x00")
        self.wfile.flush()
        version, command, _, address_type = self.rfile.read(4)
        if version != 5 or command != 1 or address_type != 3:
            return
        length = self.rfile.read(1)[0]
        host = self.rfile.read(length).decode("ascii")
        port = int.from_bytes(self.rfile.read(2), "big")
        self.server.requests.append((host, port))
        with socket.create_connection(("127.0.0.1", self.server.tunnel_port), timeout=3) as upstream:
            self.wfile.write(b"\x05\x00\x00\x01\x7f\x00\x00\x01\x00\x00")
            self.wfile.flush()
            relay(self.connection, upstream)


@contextlib.contextmanager
def socks_server(tunnel_port):
    instance = socketserver.ThreadingTCPServer(("127.0.0.1", 0), SocksHandler)
    instance.daemon_threads = True
    instance.tunnel_port = tunnel_port
    instance.requests = []
    instance.url = f"socks5://127.0.0.1:{instance.server_address[1]}"
    worker = threading.Thread(target=instance.serve_forever, kwargs={"poll_interval": 0.02}, daemon=True)
    worker.start()
    try:
        yield instance
    finally:
        instance.shutdown()
        instance.server_close()
        worker.join(timeout=3)


@contextlib.contextmanager
def server(status=200, body=b"ok", headers=None, reply=None, tls=None, tunnel_port=None):
    instance = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    instance.daemon_threads = True
    instance.requests = []
    instance.reply = reply or (lambda _: (status, headers or {}, body))
    instance.tunnel_port = tunnel_port
    if tls:
        instance.socket = tls.wrap_socket(instance.socket, server_side=True)
    instance.url = f"http://127.0.0.1:{instance.server_port}"
    worker = threading.Thread(target=instance.serve_forever, kwargs={"poll_interval": 0.02}, daemon=True)
    worker.start()
    try:
        yield instance
    finally:
        instance.shutdown()
        instance.server_close()
        worker.join(timeout=3)


def send(client, method="GET", url="http://target.invalid/a/../b?x=%2F", **kwargs):
    return NS["send_path_as_is"](client, method=method, url=url, headers=httpx.Headers(), **kwargs)


class ProxyPoolTests(unittest.TestCase):
    def setUp(self):
        self.diagnostics = io.StringIO()
        self.redirect_stderr = contextlib.redirect_stderr(self.diagnostics)
        self.redirect_stderr.__enter__()

    def tearDown(self):
        self.redirect_stderr.__exit__(None, None, None)

    def test_parse_formats_validate_and_deduplicate(self):
        parse = NS["parse_proxy_list"]
        self.assertEqual(parse("LOCALHOST:8080\nhttp://localhost:8080/"), ["http://localhost:8080"])
        self.assertEqual(parse('{"data":["socks5://[::1]:1080"]}'), ["socks5://[::1]:1080"])
        self.assertEqual(parse('{"proxies":["https://user:p%40ss@EXAMPLE.COM:443"]}'),
                         ["https://user:p%40ss@example.com:443"])
        for value in ["http://user:secret@host:bad", "http://host:80/path", "file://host:80",
                      "http://host:0", "http://host:65536", "http://host:80?q=secret", "[invalid",
                      {"data": [{"ip": "127.0.0.1"}]}, ["host:80"] * 257]:
            with self.subTest(value=type(value)):
                with self.assertRaises(ValueError) as caught:
                    parse(value)
                self.assertNotIn("secret", str(caught.exception))

    def test_configuration_bounds(self):
        for kwargs in [dict(max_attempts=0), dict(max_attempts=11), dict(cooldown=-1),
                       dict(cooldown=float("nan")), dict(cooldown=float("inf")),
                       dict(fail_status="200"), dict(fail_status="oops")]:
            with self.subTest(kwargs=kwargs), self.assertRaises(ValueError):
                Pool("localhost:8080", **kwargs)

    def test_failover_and_sticky_connection_preserve_raw_path(self):
        with server(403, b"blocked") as blocked, server() as good:
            pool = Pool(json.dumps([blocked.url, good.url]), cooldown=60)
            with httpx.Client(transport=pool, trust_env=False) as client:
                for _ in range(2):
                    response, *_ = send(client)
                    with contextlib.closing(response):
                        self.assertEqual(response.read(), b"ok")
                        self.assertFalse(response.extensions["proxy_pool"]["failed"])
            self.assertEqual(len(blocked.requests), 1)
            self.assertEqual(len(good.requests), 2)
            self.assertEqual(good.requests[0][1], "http://target.invalid/a/../b?x=%2F")

    def test_network_failure_switches_and_never_retries_same_proxy(self):
        # Reserve then close a local port: connection refused, no Internet access.
        sock = socket.socket()
        sock.bind(("127.0.0.1", 0))
        dead = f"http://127.0.0.1:{sock.getsockname()[1]}"
        sock.close()
        with server() as good:
            with httpx.Client(transport=Pool(json.dumps([dead, good.url])), trust_env=False, timeout=1) as client:
                response, *_ = send(client)
                with contextlib.closing(response):
                    self.assertEqual(response.read(), b"ok")
        with httpx.Client(transport=Pool(dead, cooldown=0, max_attempts=10), trust_env=False, timeout=1) as client:
            with self.assertRaisesRegex(httpx.ProxyError, "exhausted"):
                send(client)

    def test_no_replacement_preserves_block_response_and_skips_direct_probe(self):
        with server(403, b"evidence") as blocked:
            with httpx.Client(transport=Pool(blocked.url), trust_env=False) as client:
                response, *_ = send(client)
                with contextlib.closing(response):
                    self.assertEqual(response.read(), b"evidence")
                    self.assertTrue(response.extensions["proxy_pool"]["failed"])
                with self.assertRaisesRegex(httpx.ProxyError, "direct fallback disabled"):
                    send(client)
            self.assertEqual(len(blocked.requests), 1)

    def test_attempt_budget(self):
        with server(403) as first, server(403) as second, server() as third:
            with httpx.Client(transport=Pool(json.dumps([first.url, second.url, third.url]), max_attempts=2), trust_env=False) as client:
                response, *_ = send(client)
                with contextlib.closing(response):
                    response.read()
                    self.assertTrue(response.extensions["proxy_pool"]["failed"])
            self.assertEqual(len(third.requests), 0)

    def test_post_put_delete_never_replayed(self):
        for method in ["POST", "PUT", "DELETE"]:
            with self.subTest(method=method), server(403) as first, server() as second:
                with httpx.Client(transport=Pool(json.dumps([first.url, second.url])), trust_env=False) as client:
                    response, *_ = send(client, method, content=b"write once")
                    with contextlib.closing(response):
                        response.read()
                        self.assertEqual(response.status_code, 403)
                self.assertEqual(len(second.requests), 0)
                self.assertEqual(first.requests[0][3], b"write once")

    def test_post_transport_error_never_replayed(self):
        pool = Pool("localhost:8001\nlocalhost:8002")
        first = mock.Mock()
        first.handle_request.side_effect = httpx.ReadTimeout("private credentials")
        pool.transports[pool.proxies[0]] = first
        with httpx.Client(transport=pool, trust_env=False) as client:
            with self.assertRaisesRegex(httpx.ProxyError, "automatic replay stopped") as caught:
                send(client, "POST", content=b"once")
            self.assertNotIn("private", str(caught.exception))
        self.assertEqual(len(pool.transports), 1)

    def test_retry_after_delta_and_date_stop_replay(self):
        date = email.utils.format_datetime(datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(minutes=5))
        for value in ["120", date]:
            with self.subTest(value=value), server(429, headers={"Retry-After": value}) as first, server() as second:
                with httpx.Client(transport=Pool(json.dumps([first.url, second.url])), trust_env=False) as client:
                    response, *_ = send(client)
                    with contextlib.closing(response):
                        response.read()
                        self.assertEqual(response.status_code, 429)
                    with self.assertRaisesRegex(httpx.ProxyError, "Retry-After is still active"):
                        send(client)
                self.assertEqual(len(second.requests), 0)
        self.assertEqual(NS["retry_after_seconds"]("9" * 5000), 86400)
        self.assertEqual(NS["retry_after_seconds"]("0" * 5000 + "1"), 1)
        self.assertEqual(NS["retry_after_seconds"]("²"), 0)
        self.assertEqual(NS["retry_after_seconds"]("garbage"), 0)

    def test_429_without_retry_after_and_safe_methods_switch(self):
        for method in ["GET", "HEAD", "OPTIONS"]:
            with self.subTest(method=method), server(429) as first, server() as second:
                with httpx.Client(transport=Pool(json.dumps([first.url, second.url])), trust_env=False) as client:
                    response, *_ = send(client, method)
                    with contextlib.closing(response):
                        response.read()
                        self.assertEqual(response.status_code, 200)
                self.assertEqual(len(second.requests), 1)

    def test_body_stream_failure_is_not_replayed(self):
        class BrokenBody(httpx.SyncByteStream):
            def __iter__(self):
                yield b"partial"
                raise httpx.ReadError("connection lost during body")
        pool = Pool("localhost:8001\nlocalhost:8002")
        first = mock.Mock()
        first.handle_request.return_value = httpx.Response(200, stream=BrokenBody())
        pool.transports[pool.proxies[0]] = first
        with httpx.Client(transport=pool, trust_env=False) as client:
            response, *_ = send(client)
            with contextlib.closing(response), self.assertRaises(httpx.ReadError):
                response.read()
        self.assertEqual(len(pool.transports), 1)

    def test_status_policy_is_configurable(self):
        with server(403) as first, server() as second:
            with httpx.Client(transport=Pool(json.dumps([first.url, second.url]), fail_status="none"), trust_env=False) as client:
                response, *_ = send(client)
                with contextlib.closing(response):
                    response.read()
                    self.assertFalse(response.extensions["proxy_pool"]["failed"])
            self.assertEqual(len(second.requests), 0)

    def test_cooldown_expiry(self):
        pool = Pool("localhost:8001\nlocalhost:8002", cooldown=10)
        pool.cooling[pool.proxies[0]] = 110
        pool.cooling[pool.proxies[1]] = 110
        with mock.patch.object(NS["time"], "monotonic", return_value=100):
            self.assertIsNone(pool.select(set()))
        with mock.patch.object(NS["time"], "monotonic", return_value=111):
            self.assertEqual(pool.select(set()), pool.proxies[0])

    def test_source_refresh_has_no_target_credentials_or_environment_proxy(self):
        with server(403) as blocked, server() as good:
            def reply(handler):
                proxies = [blocked.url] if len(handler.server.requests) == 1 else [good.url]
                return 200, {}, json.dumps({"proxies": proxies}).encode()
            with server(reply=reply) as provider:
                with mock.patch.dict(os.environ, {"HTTP_PROXY": "http://127.0.0.1:1", "NO_PROXY": ""}):
                    pool = Pool("", source=provider.url + "/?key=source-secret")
                    with httpx.Client(transport=pool, trust_env=False, cookies={"session": "target-secret"}) as client:
                        response, *_ = NS["send_path_as_is"](
                            client, method="GET", url="http://target.invalid/",
                            headers=httpx.Headers({"Authorization": "Bearer target-secret"}))
                        with contextlib.closing(response):
                            self.assertEqual(response.read(), b"ok")
                self.assertEqual(len(provider.requests), 2)
                for _, _, headers, _ in provider.requests:
                    self.assertNotIn("Cookie", headers)
                    self.assertNotIn("Authorization", headers)
                    self.assertNotIn("Proxy-Authorization", headers)
                self.assertNotIn("secret", self.diagnostics.getvalue())

    def test_source_validation_limits_and_errors_redact_url(self):
        for body in [b'[{"ip":"127.0.0.1"}]', b"", b"x" * (1024 * 1024 + 1)]:
            with self.subTest(size=len(body)), server(body=body) as provider:
                with self.assertRaises(ValueError) as caught:
                    Pool("", source=provider.url + "/?key=secret")
                self.assertNotIn("secret", str(caught.exception))
        with server(302, headers={"Location": "http://127.0.0.1:1/?key=secret"}) as provider:
            with self.assertRaisesRegex(ValueError, "HTTP 302"):
                Pool("", source=provider.url)
        with self.assertRaisesRegex(ValueError, "HTTP\\(S\\)"):
            Pool("", source="file:///secret")

    def test_source_exhaustion_refresh_is_bounded(self):
        with server(403) as blocked, server(body=blocked.url.encode()) as provider:
            with httpx.Client(transport=Pool("", source=provider.url, cooldown=0), trust_env=False) as client:
                response, *_ = send(client)
                with contextlib.closing(response):
                    response.read()
                    self.assertEqual(response.status_code, 403)
            self.assertEqual(len(provider.requests), 2)
            self.assertEqual(len(blocked.requests), 1)

    def test_proxy_auth_never_in_diagnostics(self):
        with server() as good:
            proxy = good.url.replace("http://", "http://private-user:private-password@")
            with httpx.Client(transport=Pool(proxy), trust_env=False) as client:
                response, *_ = send(client)
                with contextlib.closing(response):
                    response.read()
            self.assertIn("Proxy-Authorization", good.requests[0][2])
            self.assertNotIn("private", self.diagnostics.getvalue())

    def test_redirects_keep_literal_path(self):
        def reply(handler):
            if handler.path.endswith("/start"):
                return 302, {"Location": "/x/../final"}, b""
            return 200, {}, b"redirected"
        with server(reply=reply) as proxy:
            with httpx.Client(transport=Pool(proxy.url), trust_env=False) as client:
                response, final_url, _, history = send(client, url="http://target.invalid/start", follow_redirects=True)
                with contextlib.closing(response):
                    self.assertEqual(response.read(), b"redirected")
                    self.assertEqual(final_url, "http://target.invalid/x/../final")
                for item in history:
                    item.close()
            self.assertEqual(proxy.requests[-1][1], "http://target.invalid/x/../final")

    @unittest.skipUnless(shutil.which("openssl"), "openssl required for local TLS fixture")
    def test_https_connect_switches_on_decrypted_status(self):
        with tempfile.TemporaryDirectory() as directory:
            cert = str(Path(directory) / "cert.pem")
            key = str(Path(directory) / "key.pem")
            subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                            "-subj", "/CN=target.invalid", "-keyout", key, "-out", cert],
                           check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            tls.load_cert_chain(cert, key)
            with server(403, b"tls-blocked", tls=tls) as blocked, server(tls=tls) as good:
                with server(tunnel_port=blocked.server_port) as first, server(tunnel_port=good.server_port) as second:
                    pool = Pool(json.dumps([first.url, second.url]), transport_kwargs={"verify": False})
                    with httpx.Client(transport=pool, trust_env=False) as client:
                        response, *_ = send(client, url="https://target.invalid/a/../b")
                        with contextlib.closing(response):
                            self.assertEqual(response.read(), b"ok")
                    self.assertEqual(blocked.requests[0][1], "/a/../b")
                    self.assertEqual(good.requests[0][1], "/a/../b")
                    self.assertEqual(first.requests[0][0], "CONNECT")
                    self.assertEqual(second.requests[0][0], "CONNECT")

    @unittest.skipUnless(importlib.util.find_spec("socksio"), "install httpx[socks] for SOCKS fixture")
    def test_socks5_switches_and_preserves_path(self):
        with server(403) as blocked, server() as good:
            with socks_server(blocked.server_port) as first, socks_server(good.server_port) as second:
                with httpx.Client(transport=Pool(json.dumps([first.url, second.url])), trust_env=False) as client:
                    response, *_ = send(client)
                    with contextlib.closing(response):
                        self.assertEqual(response.read(), b"ok")
                self.assertEqual(first.requests, [("target.invalid", 80)])
                self.assertEqual(second.requests, [("target.invalid", 80)])
                self.assertEqual(good.requests[0][1], "/a/../b?x=%2F")

    def run_cli(self, *args):
        return subprocess.run([sys.executable, "-c", SCRIPT, *args], text=True, capture_output=True,
                              timeout=15, env={**os.environ, "HTTP_PROXY": "http://127.0.0.1:1"})

    def test_cli_pool_exhaustion_exit_code_and_no_direct_probe(self):
        with server(403, b"blocked evidence") as proxy:
            result = self.run_cli("--url", "http://target.invalid/", "--proxy-pool", proxy.url)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("blocked evidence", result.stdout)
        self.assertIn("direct fallback disabled", result.stdout)
        self.assertNotIn("DNS", result.stdout)

    def test_cli_single_proxy_and_no_pool_remain_compatible(self):
        with server(body=b"single-proxy") as proxy:
            result = self.run_cli("--url", "http://target.invalid/", "--proxy", proxy.url)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("single-proxy", result.stdout)
        with server(body=b"direct") as target:
            result = self.run_cli("--url", target.url, "--additional-args", "trust_env=false")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("direct", result.stdout)

    def test_cli_conflicting_proxy_options_fail_before_network(self):
        result = self.run_cli("--url", "http://target.invalid", "--proxy", "localhost:8000",
                              "--proxy-pool", "localhost:8001")
        self.assertEqual(result.returncode, 2)
        self.assertIn("mutually exclusive", result.stderr)

    def test_cli_pool_flags(self):
        # Exercise all public pool flags through the real CLI parser.
        with server() as proxy:
            result = self.run_cli("--url", "http://target.invalid/", "--proxy-pool", proxy.url,
                                  "--proxy-max-attempts", "1", "--proxy-cooldown", "0",
                                  "--proxy-fail-status", "none")
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
