package burp;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InterruptedIOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.SocketTimeoutException;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;

final class CyberStrikeAIClient {

    private static final int AUTH_CONNECT_TIMEOUT_MS = 4_000;
    private static final int AUTH_READ_TIMEOUT_MS = 5_000;
    /** login + validate 整段上限，避免两次读超时叠加拖到半分钟 */
    private static final int AUTH_OVERALL_TIMEOUT_MS = 10_000;
    private static final int DEFAULT_READ_TIMEOUT_MS = 15_000;

    private final AtomicReference<HttpURLConnection> activeConnection = new AtomicReference<>();
    private final AtomicReference<Thread> activeThread = new AtomicReference<>();

    static final class Config {
        final String baseUrl; // e.g. http://127.0.0.1:8080
        final String password;

        Config(String baseUrl, String password) {
            this.baseUrl = baseUrl;
            this.password = password;
        }
    }

    enum AgentMode {
        EINO_SINGLE("Eino Single (ADK)", "/api/eino-agent/stream", null),
        DEEP("Deep (DeepAgent)", "/api/multi-agent/stream", "deep"),
        PLAN_EXECUTE("Plan-Execute", "/api/multi-agent/stream", "plan_execute"),
        SUPERVISOR("Supervisor", "/api/multi-agent/stream", "supervisor");

        final String displayName;
        final String streamPath;
        final String orchestration;

        AgentMode(String displayName, String streamPath, String orchestration) {
            this.displayName = displayName;
            this.streamPath = streamPath;
            this.orchestration = orchestration;
        }
    }

    static final class ProjectOption {
        final String id;
        final String label;

        ProjectOption(String id, String label) {
            this.id = id == null ? "" : id;
            this.label = label == null || label.isEmpty() ? this.id : label;
        }

        @Override
        public String toString() {
            return label.isEmpty() ? "(none)" : label;
        }
    }

    static final class RoleOption {
        final String name;
        final String label;

        RoleOption(String name, String label) {
            this.name = name == null ? "" : name;
            this.label = label == null || label.isEmpty() ? (this.name.isEmpty() ? "default" : this.name) : label;
        }

        @Override
        public String toString() {
            return label;
        }
    }

    interface StreamListener {
        void onEvent(String type, String message, String rawJson);
        void onError(String message, Exception e);
        void onDone();
    }

    boolean hasActiveRequest() {
        return activeConnection.get() != null;
    }

    void cancelActiveRequest() {
        HttpURLConnection conn = activeConnection.getAndSet(null);
        if (conn != null) {
            try {
                conn.disconnect();
            } catch (Exception ignored) {
            }
        }
        Thread t = activeThread.getAndSet(null);
        if (t != null) {
            t.interrupt();
        }
    }

    String loginAndValidate(Config cfg) throws IOException {
        Thread worker = Thread.currentThread();
        java.util.Timer deadline = new java.util.Timer("CyberStrikeAI-AuthDeadline", true);
        deadline.schedule(new java.util.TimerTask() {
            @Override
            public void run() {
                worker.interrupt();
                cancelActiveRequest();
            }
        }, AUTH_OVERALL_TIMEOUT_MS);
        try {
            String token = login(cfg.baseUrl, cfg.password);
            if (Thread.interrupted()) {
                throw timeoutIOException();
            }
            validate(cfg.baseUrl, token);
            if (Thread.interrupted()) {
                throw timeoutIOException();
            }
            return token;
        } catch (SocketTimeoutException e) {
            throw timeoutIOException();
        } finally {
            deadline.cancel();
        }
    }

    private static IOException timeoutIOException() {
        return new IOException("Connection timed out (~" + (AUTH_OVERALL_TIMEOUT_MS / 1000)
                + "s). Check host/port and HTTPS checkbox.");
    }

    private void trackConnection(HttpURLConnection conn) {
        activeThread.set(Thread.currentThread());
        activeConnection.set(conn);
    }

    private void releaseConnection(HttpURLConnection conn) {
        if (activeConnection.compareAndSet(conn, null)) {
            activeThread.set(null);
        }
    }

    private static boolean isCancelled(Throwable e) {
        if (e == null) {
            return Thread.currentThread().isInterrupted();
        }
        if (Thread.currentThread().isInterrupted()) {
            return true;
        }
        if (e instanceof InterruptedIOException) {
            return true;
        }
        if (e instanceof SocketTimeoutException) {
            return false;
        }
        Throwable cause = e.getCause();
        if (cause != null && cause != e) {
            return isCancelled(cause);
        }
        String msg = e.getMessage();
        return msg != null && (
                msg.toLowerCase().contains("cancel")
                        || msg.toLowerCase().contains("abort")
                        || msg.toLowerCase().contains("closed")
        );
    }

    private String login(String baseUrl, String password) throws IOException {
        URL url = new URL(baseUrl + "/api/auth/login");
        HttpURLConnection conn = SslTrustAll.open(url, AUTH_CONNECT_TIMEOUT_MS, AUTH_READ_TIMEOUT_MS);
        trackConnection(conn);
        try {
        conn.setRequestMethod("POST");
        conn.setDoOutput(true);
        conn.setRequestProperty("Content-Type", "application/json");
        conn.setRequestProperty("Accept", "application/json");
        String body = "{\"password\":\"" + escapeJson(password) + "\"}";
        try (OutputStream os = conn.getOutputStream()) {
            os.write(body.getBytes(StandardCharsets.UTF_8));
        }
        int code = conn.getResponseCode();
        String contentType = conn.getHeaderField("Content-Type");
        String resp = readAll(code >= 200 && code < 300 ? conn.getInputStream() : conn.getErrorStream());

        // Friendly diagnosis: HTML usually means wrong host/port (e.g., hit Burp UI/proxy page).
        if (looksLikeHtml(resp) || (contentType != null && contentType.toLowerCase().contains("text/html"))) {
            throw new IOException("Login failed: server returned HTML, not API JSON. Check IP/Port and ensure you point to CyberStrikeAI backend.");
        }

        String serverError = SimpleJson.extractStringField(resp, "error");
        if (code < 200 || code >= 300) {
            if (!serverError.isEmpty()) {
                throw new IOException("Login failed (" + code + "): " + serverError);
            }
            throw new IOException("Login failed (" + code + ").");
        }

        if (!serverError.isEmpty()) {
            throw new IOException("Login failed: " + serverError);
        }

        String token = SimpleJson.extractStringField(resp, "token");
        if (token.isEmpty()) {
            throw new IOException("Login response missing token. Check backend address and credentials.");
        }
        return token;
        } finally {
            releaseConnection(conn);
        }
    }

    private void validate(String baseUrl, String token) throws IOException {
        URL url = new URL(baseUrl + "/api/auth/validate");
        HttpURLConnection conn = SslTrustAll.open(url, AUTH_CONNECT_TIMEOUT_MS, AUTH_READ_TIMEOUT_MS);
        trackConnection(conn);
        try {
        conn.setRequestMethod("GET");
        conn.setRequestProperty("Authorization", "Bearer " + token);
        int code = conn.getResponseCode();
        String resp = readAll(code >= 200 && code < 300 ? conn.getInputStream() : conn.getErrorStream());
        if (code < 200 || code >= 300) {
            throw new IOException("Validate failed (" + code + "): " + resp);
        }
        } finally {
            releaseConnection(conn);
        }
    }

    List<ProjectOption> fetchProjects(Config cfg, String token) throws IOException {
        String resp = authorizedGet(cfg.baseUrl, token, "/api/projects?limit=500");
        List<ProjectOption> out = new ArrayList<>();
        out.add(new ProjectOption("", "(none)"));
        for (String obj : SimpleJson.extractObjectArray(resp, "projects")) {
            String id = SimpleJson.extractStringField(obj, "id");
            if (id.isEmpty()) continue;
            String name = SimpleJson.extractStringField(obj, "name");
            String status = SimpleJson.extractStringField(obj, "status");
            String label = name.isEmpty() ? id : name;
            if ("archived".equalsIgnoreCase(status)) {
                label = label + " [archived]";
            }
            out.add(new ProjectOption(id, label));
        }
        return out;
    }

    List<RoleOption> fetchRoles(Config cfg, String token) throws IOException {
        String resp = authorizedGet(cfg.baseUrl, token, "/api/roles");
        List<RoleOption> out = new ArrayList<>();
        out.add(new RoleOption("", "default"));
        for (String obj : SimpleJson.extractObjectArray(resp, "roles")) {
            if (!SimpleJson.extractBooleanField(obj, "enabled", true)) {
                continue;
            }
            String name = SimpleJson.extractStringField(obj, "name");
            if (name.isEmpty()) continue;
            out.add(new RoleOption(name, name));
        }
        return out;
    }

    private String authorizedGet(String baseUrl, String token, String path) throws IOException {
        URL url = new URL(baseUrl + path);
        HttpURLConnection conn = SslTrustAll.open(url, AUTH_CONNECT_TIMEOUT_MS, AUTH_READ_TIMEOUT_MS);
        try {
            conn.setRequestMethod("GET");
            conn.setRequestProperty("Accept", "application/json");
            conn.setRequestProperty("Authorization", "Bearer " + token);
            int code = conn.getResponseCode();
            String resp = readAll(code >= 200 && code < 300 ? conn.getInputStream() : conn.getErrorStream());
            if (code < 200 || code >= 300) {
                throw new IOException("GET " + path + " failed (" + code + "): " + resp);
            }
            return resp;
        } finally {
            conn.disconnect();
        }
    }

    void streamTest(Config cfg, String token, String message, StreamListener listener) {
        streamTest(cfg, token, message, "", "", AgentMode.EINO_SINGLE, listener);
    }

    void streamTest(Config cfg, String token, String message, String role, String projectId,
                    AgentMode agentMode, StreamListener listener) {
        AgentMode mode = agentMode != null ? agentMode : AgentMode.EINO_SINGLE;
        String urlStr = cfg.baseUrl + mode.streamPath;

        Map<String, Object> payload = new HashMap<>();
        payload.put("message", message);
        payload.put("conversationId", "");
        payload.put("role", role == null ? "" : role);
        if (projectId != null && !projectId.trim().isEmpty()) {
            payload.put("projectId", projectId.trim());
        }
        if (mode.orchestration != null) {
            payload.put("orchestration", mode.orchestration);
        }

        Thread worker = new Thread(() -> {
            HttpURLConnection conn = null;
            try {
                URL url = new URL(urlStr);
                conn = SslTrustAll.open(url, AUTH_CONNECT_TIMEOUT_MS, 0);
                trackConnection(conn);
                conn.setRequestMethod("POST");
                conn.setDoOutput(true);
                conn.setRequestProperty("Content-Type", "application/json");
                conn.setRequestProperty("Accept", "text/event-stream");
                conn.setRequestProperty("Authorization", "Bearer " + token);

                String body = toJson(payload);
                try (OutputStream os = conn.getOutputStream()) {
                    os.write(body.getBytes(StandardCharsets.UTF_8));
                }

                int code = conn.getResponseCode();
                InputStream is = (code >= 200 && code < 300) ? conn.getInputStream() : conn.getErrorStream();
                if (is == null) {
                    throw new IOException("No response body (HTTP " + code + ")");
                }

                try (BufferedReader br = new BufferedReader(new InputStreamReader(is, StandardCharsets.UTF_8))) {
                    String line;
                    while ((line = br.readLine()) != null) {
                        if (Thread.currentThread().isInterrupted()) {
                            break;
                        }
                        // SSE format: "data: {json}"
                        if (line.startsWith("data:")) {
                            String json = line.substring("data:".length()).trim();
                            if (!json.isEmpty()) {
                                String type = SimpleJson.extractStringField(json, "type");
                                String msg = SimpleJson.extractStringField(json, "message");
                                listener.onEvent(type, msg, json);
                                if ("done".equals(type)) {
                                    break;
                                }
                            }
                        }
                    }
                }
                if (Thread.currentThread().isInterrupted()) {
                    listener.onError("Cancelled.", null);
                } else {
                    listener.onDone();
                }
            } catch (Exception e) {
                if (isCancelled(e)) {
                    listener.onError("Cancelled.", e);
                } else {
                    listener.onError(e.getMessage(), e);
                }
            } finally {
                if (conn != null) {
                    releaseConnection(conn);
                    conn.disconnect();
                }
            }
        }, "CyberStrikeAI-Stream");
        worker.start();
    }

    void cancelByConversationId(String baseUrl, String token, String conversationId) throws IOException {
        if (conversationId == null || conversationId.trim().isEmpty()) {
            throw new IOException("Missing conversationId.");
        }
        URL url = new URL(baseUrl + "/api/agent-loop/cancel");
        HttpURLConnection conn = SslTrustAll.open(url, AUTH_CONNECT_TIMEOUT_MS, AUTH_READ_TIMEOUT_MS);
        conn.setRequestMethod("POST");
        conn.setDoOutput(true);
        conn.setRequestProperty("Content-Type", "application/json");
        conn.setRequestProperty("Accept", "application/json");
        conn.setRequestProperty("Authorization", "Bearer " + token);

        String body = "{\"conversationId\":\"" + escapeJson(conversationId.trim()) + "\"}";
        try (OutputStream os = conn.getOutputStream()) {
            os.write(body.getBytes(StandardCharsets.UTF_8));
        }

        int code = conn.getResponseCode();
        String resp = readAll(code >= 200 && code < 300 ? conn.getInputStream() : conn.getErrorStream());
        if (code < 200 || code >= 300) {
            String serverError = SimpleJson.extractStringField(resp, "error");
            if (!serverError.isEmpty()) {
                throw new IOException("Cancel failed (" + code + "): " + serverError);
            }
            throw new IOException("Cancel failed (" + code + ").");
        }
    }

    private static String toJson(Map<String, Object> payload) {
        String message = payload.get("message") != null ? String.valueOf(payload.get("message")) : "";
        String conversationId = payload.get("conversationId") != null ? String.valueOf(payload.get("conversationId")) : "";
        String role = payload.get("role") != null ? String.valueOf(payload.get("role")) : "";
        StringBuilder sb = new StringBuilder();
        sb.append("{");
        sb.append("\"message\":\"").append(escapeJson(message)).append("\",");
        sb.append("\"conversationId\":\"").append(escapeJson(conversationId)).append("\",");
        sb.append("\"role\":\"").append(escapeJson(role)).append("\"");
        if (payload.containsKey("projectId") && payload.get("projectId") != null) {
            sb.append(",\"projectId\":\"").append(escapeJson(String.valueOf(payload.get("projectId")))).append("\"");
        }
        if (payload.containsKey("orchestration") && payload.get("orchestration") != null) {
            sb.append(",\"orchestration\":\"").append(escapeJson(String.valueOf(payload.get("orchestration")))).append("\"");
        }
        sb.append("}");
        return sb.toString();
    }

    private static String escapeJson(String s) {
        if (s == null) return "";
        StringBuilder sb = new StringBuilder(s.length() + 16);
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '\\': sb.append("\\\\"); break;
                case '"': sb.append("\\\""); break;
                case '\n': sb.append("\\n"); break;
                case '\r': sb.append("\\r"); break;
                case '\t': sb.append("\\t"); break;
                default:
                    if (c < 0x20) {
                        sb.append(String.format("\\u%04x", (int) c));
                    } else {
                        sb.append(c);
                    }
            }
        }
        return sb.toString();
    }

    private static String readAll(InputStream is) throws IOException {
        if (is == null) return "";
        try (BufferedReader br = new BufferedReader(new InputStreamReader(is, StandardCharsets.UTF_8))) {
            StringBuilder sb = new StringBuilder();
            String line;
            while ((line = br.readLine()) != null) {
                sb.append(line).append('\n');
            }
            return sb.toString().trim();
        }
    }

    private static boolean looksLikeHtml(String s) {
        if (s == null) return false;
        String t = s.trim().toLowerCase();
        return t.startsWith("<!doctype html") || t.startsWith("<html") || t.contains("<head>") || t.contains("<body");
    }
}

