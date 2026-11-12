package org.coffeeshop;

import com.fasterxml.jackson.databind.node.JsonNodeFactory;
import org.junit.jupiter.api.Test;

import java.io.EOFException;
import java.io.IOException;
import java.net.ServerSocket;
import java.net.Socket;
import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.*;

class TcpConnectionTest {
    @Test
    void closesReaderWhenPeerCloses() throws Exception {
        try (ServerSocket server = new ServerSocket(0); TcpConnection connection = new TcpConnection("127.0.0.1", server.getLocalPort())) {
            try (Socket peer = server.accept()) {
                peer.close();
                var failure = new java.util.concurrent.CountDownLatch(1);
                connection.startReader(message -> fail("Unexpected message"), error -> failure.countDown());
                assertTrue(failure.await(2, TimeUnit.SECONDS));
            }
        }
    }

    @Test
    void serializesAndReadsMessagesThroughSocket() throws Exception {
        try (ServerSocket server = new ServerSocket(0); TcpConnection connection = new TcpConnection("127.0.0.1", server.getLocalPort())) {
            try (Socket peer = server.accept()) {
                MessageCodec codec = new MessageCodec();
                codec.write(peer.getOutputStream(), JsonNodeFactory.instance.objectNode().put("type", "auth_req"));
                assertEquals("auth_req", connection.read().path("type").asText());
                connection.send(JsonNodeFactory.instance.objectNode().put("type", "menu").put("id", "m1"));
                assertEquals("menu", codec.read(peer.getInputStream()).path("type").asText());
            }
        }
    }

    @Test
    void rejectsUnexpectedEof() {
        MessageCodec codec = new MessageCodec();
        assertThrows(EOFException.class, () -> codec.read(new java.io.ByteArrayInputStream(new byte[]{0, 0})));
        assertThrows(EOFException.class, () -> codec.read(new java.io.ByteArrayInputStream(new byte[]{0, 0, 0, 3, '{'})));
    }
}
