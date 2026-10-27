package io.github.mex040708afk.bitabit.desktop;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;

/**
 * Un servidor de la API v1 de mentira, en un puerto libre de 127.0.0.1. Responde lo que
 * se le diga para cada «MÉTODO ruta» y apunta lo que recibe.
 */
final class FakeServer implements AutoCloseable {

    record Reply(int status, String body, Map<String, String> headers) {

        static Reply json(int status, String body) {
            return new Reply(status, body, Map.of());
        }
    }

    /** {@code query} va ya decodificada, sin el {@code ?}. */
    record Received(String method, String path, Optional<String> query, Optional<String> authorization, String body) {
    }

    private final HttpServer server;
    private final Map<String, Reply> replies = new ConcurrentHashMap<>();
    private final List<Received> received = new CopyOnWriteArrayList<>();

    FakeServer() throws IOException {
        server = HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0);
        server.createContext("/", this::handle);
        server.start();
    }

    /** {@code http://127.0.0.1:puerto}, sin barra final. */
    String address() {
        return "http://127.0.0.1:" + server.getAddress().getPort();
    }

    void reply(String methodAndPath, Reply reply) {
        replies.put(methodAndPath, reply);
    }

    List<Received> received() {
        return received;
    }

    @Override
    public void close() {
        server.stop(0);
    }

    private void handle(HttpExchange exchange) throws IOException {
        try (exchange) {
            String method = exchange.getRequestMethod();
            String path = exchange.getRequestURI().getPath();
            String body = new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
            received.add(new Received(method, path, Optional.ofNullable(exchange.getRequestURI().getQuery()),
                    Optional.ofNullable(exchange.getRequestHeaders().getFirst("Authorization")), body));
            Reply reply = replies.getOrDefault(method + " " + path,
                    Reply.json(404, "{\"error\":\"not_found\",\"message\":\"no existe\"}"));
            reply.headers().forEach((name, value) -> exchange.getResponseHeaders().add(name, value));
            byte[] bytes = reply.body().getBytes(StandardCharsets.UTF_8);
            if (bytes.length > 0) {
                exchange.getResponseHeaders().add("Content-Type", "application/json");
            }
            // -1: sin cuerpo, como un 204.
            exchange.sendResponseHeaders(reply.status(), bytes.length == 0 ? -1 : bytes.length);
            if (bytes.length > 0) {
                try (OutputStream out = exchange.getResponseBody()) {
                    out.write(bytes);
                }
            }
        }
    }
}
