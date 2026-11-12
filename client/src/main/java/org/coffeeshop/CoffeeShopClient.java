package org.coffeeshop;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;

import java.io.IOException;
import java.util.*;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.CountDownLatch;

public final class CoffeeShopClient implements AutoCloseable {
    private final TcpConnection connection;
    private final MessageCodec codec = new MessageCodec();
    private final String role;
    private final AtomicLong ids = new AtomicLong();
    private final Map<Long, Order> orders = new ConcurrentHashMap<>();
    private volatile boolean authenticated;
    private volatile boolean running = true;
    private final CountDownLatch terminated = new CountDownLatch(1);
    public CoffeeShopClient(TcpConnection connection, String role) {
        this.connection = connection;
        this.role = role;
    }

    public boolean authenticate() throws IOException {
        JsonNode req = connection.read();
        requireType(req, "auth_req");
        String requestId = id("auth");
        ObjectNode auth = codec.mapper().createObjectNode().put("type", "auth_res").put("id", requestId).put("role", role);
        connection.send(auth);
        JsonNode response = connection.read();
        String responseType = requiredText(response, "type");
        if ("auth_ok".equals(responseType)
                && requestId.equals(requiredText(response, "id"))
                && role.equals(requiredText(response, "role"))) {
            authenticated = true;
            System.out.println("Authenticated as " + role + ". Type help for commands.");
            return true;
        }
        if ("error".equals(responseType) || "provider_exists".equals(responseType)
                || "invalid_role".equals(responseType) || "authentication_required".equals(responseType)) {
            print(response);
            return false;
        }
        throw new IOException("Unexpected authentication response: " + response);
    }

    public void startEvents() {
        connection.startReader(this::print, e -> {
            running = false;
            terminated.countDown();
            if (!(e instanceof java.net.SocketException) || e.getMessage() != null) {
                System.out.println("Server connection closed: " + e.getMessage());
            }
        });
    }

    public boolean isRunning() {
        return running;
    }

    public void awaitTermination() throws InterruptedException {
        terminated.await();
    }

    public List<Order> orders() {
        return List.copyOf(orders.values());
    }

    public void command(String line) throws IOException {
        String[] p = line.trim().split("\\s+");
        if (p.length == 0 || p[0].isBlank()) return;
        switch (p[0].toLowerCase(Locale.ROOT)) {
            case "help" -> help();
            case "menu" -> sendSimple("menu");
            case "history" -> history();
            case "order" -> sendItems("order", p);
            case "delivery" -> sendItems("delivery", p);
            case "exit" -> {
                System.out.println("Closing connection; unfinished orders will be canceled by the server.");
                close();
            }
            default -> System.out.println("Unknown command. Type help.");
        }
    }

    private void sendSimple(String type) throws IOException {
        ObjectNode n = codec.mapper().createObjectNode().put("type", type).put("id", id(type));
        connection.send(n);
    }

    private void sendItems(String type, String[] parts) throws IOException {
        if ((type.equals("order") && !role.equals("client")) || (type.equals("delivery") && !role.equals("provider"))) {
            System.out.println("This command is not available for your role.");
            return;
        }
        if (parts.length < 2) {
            System.out.println("Use: " + type + " coffee=2 tea=1 croissant=1");
            return;
        }
        ObjectNode items = codec.mapper().createObjectNode();
        for (int i = 1; i < parts.length; i++) {
            String[] kv = parts[i].split("=", 2);
            if (kv.length != 2 || !Set.of("coffee", "tea", "croissant").contains(kv[0])) {
                System.out.println("Use valid product=positive_quantity pairs.");
                return;
            }
            try {
                int q = Integer.parseInt(kv[1]);
                if (q < 1 || q > 1_000_000) throw new NumberFormatException();
                items.put(kv[0], q);
            } catch (NumberFormatException e) {
                System.out.println("Quantities must be integers from 1 to 1000000.");
                return;
            }
        }
        ObjectNode n = codec.mapper().createObjectNode().put("type", type).put("id", id(type));
        n.set("items", items);
        connection.send(n);
    }

    private void print(JsonNode n) {
        if (n == null || !n.isObject()) throw new IllegalArgumentException("Server message is not a JSON object");
        String type = requiredText(n, "type");
        switch (type) {
            case "menu" ->
                    printMenu(n);
            case "order_accepted" -> {
                requireFields(n, "id", "order_id", "preparation_seconds");
                long oid = n.path("order_id").asLong();
                if (oid < 1 || n.path("preparation_seconds").asInt() < 0) throw new IllegalArgumentException("Invalid order_accepted response");
                orders.put(oid, new Order(n.path("id").asText(), oid, "accepted", n.path("preparation_seconds").asInt()));
                System.out.println("Order accepted: server order ID=" + oid + ", preparation=" + n.path("preparation_seconds").asInt() + "s");
            }
            case "order_ready" -> {
                requireFields(n, "order_id");
                long oid = n.path("order_id").asLong();
                if (oid < 1) throw new IllegalArgumentException("Invalid order_ready response");
                Order o = orders.get(oid);
                if (o == null) System.out.println("Order ready: unknown server order ID=" + oid);
                else { orders.put(oid, new Order(o.requestId(), oid, "ready", o.preparationSeconds())); System.out.println("Order ready: server order ID=" + oid + ", request ID=" + o.requestId()); }
            }
            case "order_refused" -> { requireFields(n, "id", "stock", "msg"); System.out.println("Order refused [" + n.path("id").asText() + "]: " + n.path("msg").asText() + ". Stock: " + n.path("stock")); }
            case "delivery_accepted" -> { requireFields(n, "id", "stock"); System.out.println("Delivery accepted [" + n.path("id").asText() + "]. Stock: " + n.path("stock")); }
            case "low_stock" -> { requireFields(n, "product", "quantity"); System.out.println("LOW STOCK: " + n.path("product").asText() + " remaining=" + n.path("quantity").asInt()); }
            case "error" -> { requireFields(n, "code", "msg"); System.out.println("Server error" + (n.has("id") ? " [" + n.path("id").asText() + "]" : "") + ": " + n.path("code").asText() + " - " + n.path("msg").asText()); }
            case "auth_ok" -> { requireFields(n, "id", "role"); System.out.println("Authentication confirmed as " + n.path("role").asText()); }
            default -> System.out.println("Server message: " + n);
        }
    }

    private void printMenu(JsonNode n) { requireFields(n, "id", "stock", "preparation_seconds"); System.out.println("Product | Available | Preparation time"); for (String product : List.of("coffee", "tea", "croissant")) { JsonNode stock=n.path("stock").path(product); JsonNode preparation=n.path("preparation_seconds").path(product); if (!stock.isIntegralNumber() || !preparation.isIntegralNumber()) throw new IllegalArgumentException("Invalid menu response"); System.out.println(product + " | " + stock.asInt() + " | " + preparation.asInt() + "s"); } }
    private static void requireType(JsonNode node, String type) { if (node == null || !node.isObject() || !type.equals(requiredText(node, "type"))) throw new IllegalArgumentException("Expected " + type); }
    private static void requireFields(JsonNode node, String... fields) { for (String field : fields) { if (!node.has(field) || node.get(field).isNull()) throw new IllegalArgumentException("Missing field: " + field); } }
    private static String requiredText(JsonNode node, String field) { requireFields(node, field); JsonNode value=node.get(field); if (!value.isTextual() || value.asText().isBlank()) throw new IllegalArgumentException("Field must be a nonempty string: " + field); return value.asText(); }

    private void history() {
        if (orders.isEmpty()) System.out.println("No orders in this connection.");
        else
            orders.values().forEach(o -> System.out.println(o.orderId() + " | " + o.status() + " | " + o.preparationSeconds() + "s"));
    }

    private void help() {
        System.out.println("menu - show stock; order coffee=2 tea=1 - place an order; delivery coffee=20 - provider delivery; history - show local orders; exit - close; help - show this help");
    }

    private String id(String prefix) {
        return prefix + "-" + ids.incrementAndGet();
    }

    @Override
    public void close() {
        if (running) { running = false; connection.close(); terminated.countDown(); }
    }

    public record Order(String requestId, long orderId, String status, int preparationSeconds) {
    }
}
