# Coffee Shop Java Client

Java 21/Maven client and provider for the existing Go coffee-shop server. The Go server is not modified.

## Build and run

```text
cd client
mvn test
mvn package
java -jar target/coffee-client-1.0.0.jar --host 127.0.0.1 --port 8080 --role client
java -jar target/coffee-client-1.0.0.jar --host 127.0.0.1 --port 8080 --role provider
```

Commands are `menu`, `order coffee=2 tea=1`, `delivery coffee=20 croissant=10`, `history`, `help`, and `exit`. Product quantities are positive integers up to 1,000,000. Delivery is available only to providers; orders only to clients.

The client uses standard `java.net.Socket` and a dedicated reader thread. Frames contain a 4-byte unsigned big-endian payload length followed by UTF-8 JSON (1-65536 bytes). Writes are synchronized so complete frames cannot interleave. Asynchronous `order_ready` and `low_stock` messages are displayed immediately.

Start the Go server from `server` with `go run ./cmd --host 0.0.0.0 --port 8080`, then run one provider and any number of clients. For another computer on the same LAN, bind the server to `0.0.0.0` and use the server computer's IPv4 address in `--host`; allow TCP port 8080 through the firewall.

If connection is refused, verify that the server is running and that host/port/firewall settings match. The server's protocol and role errors are printed as JSON diagnostics.
