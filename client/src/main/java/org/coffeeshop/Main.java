package org.coffeeshop;

import java.io.*;
import java.util.*;

public final class Main {
    public static void main(String[] args) {
        try {
            Map<String, String> options = parse(args);
            String host = options.getOrDefault("host", "127.0.0.1");
            int port = Integer.parseInt(options.getOrDefault("port", "8080"));
            String role = options.getOrDefault("role", "client");
            if (port < 1 || port > 65535 || !Set.of("client", "provider").contains(role))
                throw new IllegalArgumentException("Use --host IPv4 --port 1..65535 --role client|provider");
            try (TcpConnection tcp = new TcpConnection(host, port); CoffeeShopClient client = new CoffeeShopClient(tcp, role)) {
                if (!client.authenticate()) return;
                client.startEvents();
                Thread console = new Thread(() -> {
                    BufferedReader input = new BufferedReader(new InputStreamReader(System.in));
                    while (client.isRunning()) {
                        System.out.print("> ");
                        try {
                            String line = input.readLine();
                            if (line == null) { client.close(); return; }
                            client.command(line);
                        } catch (IOException e) {
                            if (client.isRunning()) System.out.println("Console error: " + e.getMessage());
                            client.close();
                            return;
                        }
                    }
                }, "console-reader");
                console.setDaemon(true);
                console.start();
                try {
                    client.awaitTermination();
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                    client.close();
                }
            }
        } catch (IllegalArgumentException e) {
            System.out.println("Invalid arguments: " + e.getMessage());
        } catch (IOException e) {
            System.out.println("Connection error: " + e.getMessage());
        }
    }

    private static Map<String, String> parse(String[] args) {
        Map<String, String> m = new HashMap<>();
        for (int i = 0; i < args.length; i++) {
            if (!args[i].startsWith("--") || i + 1 >= args.length)
                throw new IllegalArgumentException("Options require values: --host, --port, --role");
            m.put(args[i].substring(2), args[++i]);
        }
        if (m.keySet().stream().anyMatch(k -> !Set.of("host", "port", "role").contains(k)))
            throw new IllegalArgumentException("Unknown option");
        return m;
    }
}
