# Coffee shop — laboratory work 2

A Go TCP **server** implementing the required server behavior from Tasks 1–4 in [`server/lab_description.txt`](server/lab_description.txt). No interactive client or provider program is implemented yet.

## Build and run

Requirements: **Go 1.27.3 or newer**, macOS or Linux. The implementation calls Unix socket APIs directly through `golang.org/x/sys/unix`; it does not use HTTP, RPC, `net.Listener`, or `net.Conn` in production. The standard `net` package appears only in tests as a simulated peer.

From the repository root:

```sh
cd server
go mod download
go build -o coffee-server ./cmd
./coffee-server -host 127.0.0.1 -port 8080
```

The defaults are `-host 127.0.0.1 -port 8080`. The port must be between 1 and 65535. To accept connections from another computer in a trusted local network:

```sh
./coffee-server -host 0.0.0.0 -port 8080
```

Allow the selected TCP port through the host firewall, and have future clients connect to the server's LAN IPv4 address (not `0.0.0.0`). Role selection is not password authentication or encryption, so do not expose this lab server to the public Internet.

Stop with **Ctrl+C** or SIGTERM. Shutdown closes connections, cancels unfinished preparation, and joins handlers and workers. Startup logs show the worker count and initial stock; subsequent logs include client endpoints, accepted/refused/ready/canceled orders, deliveries, and errors.

## Protocol

See [`server/PROTOCOL.md`](server/PROTOCOL.md) for the full message description: framing, fields, examples, roles, errors, event ordering, and failure semantics. It was defined before completing the implementation.

Messages are **four-byte big-endian length-prefixed UTF-8 JSON**, not newline-delimited text; ordinary interactive `nc` input does not generate valid frames. A future client/provider must read responses and unsolicited events independently of console input.

## Implemented behavior

| Task | Server behavior |
| --- | --- |
| 1: One client | Random initial stock of 10–50 per product; menu; atomic reservations; immediate acceptance with order ID and preparation time; refusal with remaining stock; asynchronous readiness; input validation. |
| 2: Many clients | Concurrent connection handlers; one whole-order worker per logical CPU; FIFO waiting queue; further requests while orders prepare; shared-state locking and serialized socket writes. |
| 3: Provider | Explicit role handshake; one active provider; atomic deliveries; role restrictions; unsolicited alerts on crossing below five; current shortages reported when the provider reconnects. |
| 4: Failures | EOF/reset/read/write failure handling; cancellation of queued and active orders; exactly-once refunds; prompt worker interruption; malformed-input errors; bounded messages/output queues; slow peers do not block stock operations; TCP keepalive. |

Preparation takes 4 seconds per coffee, 3 per tea, and 2 per croissant. Quantities and stock are capped at 1,000,000 per product, including refundable reservations for delivery capacity checks.

Optional stock persistence and cross-language clients are not implemented. Restarting randomizes the stock again and restarts order IDs. The interactive client/provider requirements and demonstrations will need those programs in a later step.

## Tests

Run from `server/`:

```sh
go test ./...
go test -race ./...
go vet ./...
# Repeat concurrency and lifecycle checks:
go test -race -count=5 ./interal/connection
```

The automated test peers are test-only, not a client application. Tests cover:

- Split/coalesced frames, partial writes, truncated input, and frame size limits.
- Authentication, role restrictions, malformed JSON/UTF-8, wrong/missing/extra fields, unknown products, invalid quantities, and recoverable large invalid input.
- Atomic refusals and deliveries, provider reconnect alerts, threshold rearming, and refundable-reservation capacity checks.
- Preparation totals, asynchronous readiness, FIFO scheduling, multiple busy workers, and requests during preparation.
- Concurrent competition for the last item and concurrent delivery/reservation accounting.
- Queued/active disconnect cancellation, refunds, completed-order non-refunds, and slow-peer isolation.
- Real TCP connections through the raw socket listener and shutdown while a peer has sent only part of a frame.

## Code layout

- `server/cmd/main.go`: command-line flags and signal handling.
- `server/interal/connection/server.go`: raw listening socket and connection lifecycle.
- `server/interal/connection/connection.go`: protocol validation, per-connection reader/writer, role dispatch.
- `server/interal/connection/shop.go`: stock, provider, FIFO queue, workers, and cancellation.
- `server/interal/connection/tcp_utils.go`: exact framed reads/writes.
- `server/interal/connection/socket_{darwin,linux}.go`: platform-specific keepalive and safe sends.

The original `interal` directory/module import spelling is retained to avoid an unrelated package-path migration.
