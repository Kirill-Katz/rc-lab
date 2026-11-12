package org.coffeeshop;

import com.fasterxml.jackson.databind.JsonNode;

import java.io.*;
import java.net.Socket;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.function.Consumer;

public final class TcpConnection implements AutoCloseable {
    private final Socket socket;
    private final MessageCodec codec = new MessageCodec();
    private final Object writeLock = new Object();
    private final AtomicBoolean closed = new AtomicBoolean();

    public TcpConnection(String host, int port) throws IOException {
        socket = new Socket(host, port);
    }

    public JsonNode read() throws IOException {
        if (closed.get()) throw new EOFException("Connection is closed");
        return codec.read(socket.getInputStream());
    }

    public void send(JsonNode message) throws IOException {
        synchronized (writeLock) {
            if (closed.get()) throw new EOFException("Connection is closed");
            codec.write(socket.getOutputStream(), message);
        }
    }

    public Thread startReader(Consumer<JsonNode> handler, Consumer<Exception> failure) {
        Thread t = new Thread(() -> {
            try {
                while (!closed.get()) handler.accept(read());
            } catch (Exception e) {
                if (closed.compareAndSet(false, true)) {
                    try {
                        socket.close();
                    } catch (IOException ignored) {
                    }
                    failure.accept(e);
                }
            }
        }, "server-reader");
        t.setDaemon(false);
        t.start();
        return t;
    }

    @Override
    public void close() {
        if (closed.compareAndSet(false, true)) {
            try {
                socket.close();
            } catch (IOException ignored) {
            }
        }
    }
}
