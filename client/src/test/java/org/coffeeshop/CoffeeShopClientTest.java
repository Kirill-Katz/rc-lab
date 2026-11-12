package org.coffeeshop;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import org.junit.jupiter.api.Test;

import java.net.ServerSocket;
import java.net.Socket;
import java.util.List;
import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.*;

class CoffeeShopClientTest {
    @Test
    void authenticatesWithMatchingIdAndRole() throws Exception {
        try (ServerSocket server = new ServerSocket(0); TcpConnection clientSocket = new TcpConnection("127.0.0.1", server.getLocalPort())) {
            try (Socket peer = server.accept()) {
                MessageCodec codec = new MessageCodec();
                codec.write(peer.getOutputStream(), object("type", "auth_req"));
                CoffeeShopClient client = new CoffeeShopClient(clientSocket, "client");
                Thread serverThread = new Thread(() -> { try { JsonNode auth = codec.read(peer.getInputStream()); codec.write(peer.getOutputStream(), codec.mapper().createObjectNode().put("type", "auth_ok").put("id", auth.path("id").asText()).put("role", "client")); } catch (Exception e) { throw new RuntimeException(e); } });
                serverThread.start();
                assertTrue(client.authenticate());
                serverThread.join(2000);
            }
        }
    }

    @Test
    void rejectsAuthenticationResponseWithWrongId() throws Exception {
        try (ServerSocket server = new ServerSocket(0); TcpConnection clientSocket = new TcpConnection("127.0.0.1", server.getLocalPort())) {
            try (Socket peer = server.accept()) {
                MessageCodec codec = new MessageCodec();
                codec.write(peer.getOutputStream(), object("type", "auth_req"));
                Thread serverThread = new Thread(() -> { try { codec.read(peer.getInputStream()); codec.write(peer.getOutputStream(), codec.mapper().createObjectNode().put("type", "auth_ok").put("id", "wrong").put("role", "client")); } catch (Exception e) { throw new RuntimeException(e); } });
                serverThread.start();
                assertThrows(java.io.IOException.class, () -> new CoffeeShopClient(clientSocket, "client").authenticate());
                serverThread.join(2000);
            }
        }
    }

    @Test
    void tracksMultipleOrdersAndReadyEventsAsynchronously() throws Exception {
        try (ServerSocket server = new ServerSocket(0); TcpConnection clientSocket = new TcpConnection("127.0.0.1", server.getLocalPort())) {
            try (Socket peer = server.accept()) {
                MessageCodec codec = new MessageCodec();
                codec.write(peer.getOutputStream(), object("type", "auth_req"));
                CoffeeShopClient client = new CoffeeShopClient(clientSocket, "client");
                Thread serverThread = new Thread(() -> { try { JsonNode auth = codec.read(peer.getInputStream()); codec.write(peer.getOutputStream(), codec.mapper().createObjectNode().put("type", "auth_ok").put("id", auth.path("id").asText()).put("role", "client")); JsonNode first = codec.read(peer.getInputStream()); JsonNode second = codec.read(peer.getInputStream()); sendAccepted(codec, peer, first, 11); sendAccepted(codec, peer, second, 12); codec.write(peer.getOutputStream(), codec.mapper().createObjectNode().put("type", "order_ready").put("order_id", 12)); codec.write(peer.getOutputStream(), codec.mapper().createObjectNode().put("type", "order_ready").put("order_id", 11)); } catch (Exception e) { throw new RuntimeException(e); } });
                serverThread.start();
                assertTrue(client.authenticate());
                client.startEvents();
                client.command("order coffee=1");
                client.command("order tea=1");
                serverThread.join(2000);
                long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2);
                while (client.orders().stream().anyMatch(order -> !order.status().equals("ready")) && System.nanoTime() < deadline) Thread.yield();
                assertEquals(2, client.orders().size());
                assertTrue(client.orders().stream().allMatch(order -> order.status().equals("ready")));
                client.close();
            }
        }
    }

    @Test
    void closesWhenServerDisconnects() throws Exception {
        try (ServerSocket server = new ServerSocket(0); TcpConnection clientSocket = new TcpConnection("127.0.0.1", server.getLocalPort())) {
            try (Socket peer = server.accept()) {
                MessageCodec codec = new MessageCodec();
                codec.write(peer.getOutputStream(), object("type", "auth_req"));
                CoffeeShopClient client = new CoffeeShopClient(clientSocket, "client");
                Thread serverThread = new Thread(() -> { try { JsonNode auth = codec.read(peer.getInputStream()); codec.write(peer.getOutputStream(), codec.mapper().createObjectNode().put("type", "auth_ok").put("id", auth.path("id").asText()).put("role", "client")); peer.close(); } catch (Exception e) { throw new RuntimeException(e); } });
                serverThread.start();
                assertTrue(client.authenticate());
                client.startEvents();
                client.awaitTermination();
                assertFalse(client.isRunning());
            }
        }
    }

    private static ObjectNode object(String key, String value) { return new MessageCodec().mapper().createObjectNode().put(key, value); }
    private static void sendAccepted(MessageCodec codec, Socket peer, JsonNode request, int orderId) throws Exception { codec.write(peer.getOutputStream(), codec.mapper().createObjectNode().put("type", "order_accepted").put("id", request.path("id").asText()).put("order_id", orderId).put("preparation_seconds", 1)); }
}
