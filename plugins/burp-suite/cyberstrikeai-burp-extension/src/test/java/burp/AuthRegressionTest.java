package burp;

import com.sun.net.httpserver.HttpServer;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import javax.swing.JTextField;
import javax.swing.SwingUtilities;

/** Standalone regression test; see README for commands. No test framework required. */
public final class AuthRegressionTest {
    private static void check(boolean value, String message) {
        if (!value) throw new AssertionError(message);
    }
    public static void main(String[] args) throws Exception {
        AtomicReference<String> request = new AtomicReference<>();
        AtomicReference<String> authorization = new AtomicReference<>();
        AtomicInteger logins = new AtomicInteger();
        HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/api/auth/login", exchange -> {
            String body = new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
            request.set(body);
            logins.incrementAndGet();
            boolean wrong = body.contains("wrong");
            byte[] response = (wrong ? "{\"error\":\"invalid credentials\"}" : "{\"token\":\"test-token\"}").getBytes(StandardCharsets.UTF_8);
            exchange.getResponseHeaders().set("Content-Type", "application/json");
            exchange.sendResponseHeaders(wrong ? 401 : 200, response.length);
            exchange.getResponseBody().write(response);
            exchange.close();
        });
        server.createContext("/api/auth/validate", exchange -> {
            authorization.set(exchange.getRequestHeaders().getFirst("Authorization"));
            byte[] response = "{}".getBytes(StandardCharsets.UTF_8);
            exchange.sendResponseHeaders(200, response.length);
            exchange.getResponseBody().write(response);
            exchange.close();
        });
        server.start();
        try {
            CyberStrikeAIClient client = new CyberStrikeAIClient();
            String base = "http://127.0.0.1:" + server.getAddress().getPort();
            for (String username : new String[]{"admin", "analyst", "研究员\"\\"}) {
                String password = "  p\"\\\n密码  ";
                String token = client.loginAndValidate(new CyberStrikeAIClient.Config(base, " " + username + " ", password));
                check("test-token".equals(token), "login token");
                check(username.equals(SimpleJson.extractStringField(request.get(), "username")), "username roundtrip");
                check(password.equals(SimpleJson.extractStringField(request.get(), "password")), "exact password roundtrip");
                check("Bearer test-token".equals(authorization.get()), "token validation");
            }
            int before = logins.get();
            for (String username : new String[]{"", "  ", "𠮷".repeat(65)}) {
                try {
                    client.loginAndValidate(new CyberStrikeAIClient.Config(base, username, "x"));
                    throw new AssertionError("invalid username accepted");
                } catch (java.io.IOException expected) {
                    check(expected.getMessage().contains("Username is required"), "username error");
                }
            }
            check(logins.get() == before, "invalid username sent to server");
            authorization.set(null);
            try {
                client.loginAndValidate(new CyberStrikeAIClient.Config(base, "admin", "wrong"));
                throw new AssertionError("wrong password accepted");
            } catch (java.io.IOException expected) {
                check(expected.getMessage().contains("invalid credentials"), "server error preserved");
            }
            check(authorization.get() == null, "failed login validated token");
            SwingUtilities.invokeAndWait(() -> {
                try {
                    CyberStrikeAITab tab = new CyberStrikeAITab();
                    check("admin".equals(tab.currentConfig().username), "default admin");
                    java.lang.reflect.Field tokenField = CyberStrikeAITab.class.getDeclaredField("tokenRef");
                    tokenField.setAccessible(true);
                    @SuppressWarnings("unchecked") AtomicReference<String> token = (AtomicReference<String>) tokenField.get(tab);
                    token.set("old-session");
                    java.lang.reflect.Field userField = CyberStrikeAITab.class.getDeclaredField("usernameField");
                    userField.setAccessible(true);
                    ((JTextField) userField.get(tab)).setText("analyst");
                    check(tab.getToken().isEmpty(), "account switch must clear old token");
                    check("analyst".equals(tab.currentConfig().username), "UI username reaches config");
                } catch (ReflectiveOperationException e) { throw new RuntimeException(e); }
            });
            System.out.println("PASS: Burp RBAC login, errors, escaping, and account switch");
        } finally { server.stop(0); }
    }
}
