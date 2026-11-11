# Coffee shop TCP protocol (version 1)

The server implements Tasks 1–4 of `lab_description.txt`. Client/provider console programs are intentionally not included.

## Transport and framing

Use a TCP connection (IPv4). Every message, in either direction, is a **4-byte unsigned big-endian payload length**, followed by exactly that many bytes of **UTF-8 JSON**. The length excludes the header. Valid payload sizes are 1–65536 bytes. No newline or NUL terminator is used. Receivers must read the full header and then the full payload: TCP can split or combine messages arbitrarily. Writers serialize complete frames and handle partial writes.

Each payload is one JSON object. Unknown fields, unknown commands, invalid JSON/UTF-8, and wrong field types are errors. Product names and command names are case-sensitive. Quantities are positive integers (not strings, decimals, zero, or negatives). An items object must contain at least one of `coffee`, `tea`, `croissant`. Each quantity and resulting stock must be at most 1,000,000.

Requests include a nonempty string `id` (at most 64 bytes), chosen by the sender. Replies echo it; unsolicited events omit it. IDs should be unique among outstanding requests. The server does not deduplicate requests: retrying an order creates another order. Orders have server-generated, increasing numeric `order_id` values, unique for this server process.

A malformed complete frame gets an `error` and the connection remains usable. A zero or oversized length gets an error followed by disconnection: its framing cannot safely be recovered. An incomplete frame at EOF is logged and disconnected. Outgoing queues are bounded (64 messages); an unresponsive peer is disconnected rather than blocking workers or other clients. Socket writes time out after five seconds. TCP keepalive detects otherwise silent broken connections (approximately 90 seconds on supported OSes, not an immediate failure detector). There is no application idle timeout.

## Connection and roles

The server first sends:

```json
{"type":"auth_req","msg":"Identify with auth_res: client or provider"}
```

The peer must identify before issuing commands:

```json
{"type":"auth_res","id":"a1","role":"client"}
```

`role` is `client` or `provider`. A successful response is:

```json
{"type":"auth_ok","id":"a1","role":"client"}
```

There is at most one provider. A second receives `provider_exists` and may retry authentication later or choose the client role. Once authenticated, a connection cannot change roles. This is role selection, **not secure authentication**; use only in a trusted lab network.

## Menu — both roles

Request: `{"type":"menu","id":"m1"}`

Response:

```json
{"type":"menu","id":"m1","stock":{"coffee":20,"tea":30,"croissant":40},"preparation_seconds":{"coffee":4,"tea":3,"croissant":2}}
```

Stock is an atomic snapshot. Each starting quantity is independently randomized in the inclusive range 10–50.

## Order — client only

Request:

```json
{"type":"order","id":"o1","items":{"coffee":1,"tea":1,"croissant":2}}
```

If all quantities are available, they are reserved together immediately:

```json
{"type":"order_accepted","id":"o1","order_id":1,"preparation_seconds":11}
```

If anything is unavailable, nothing is reserved:

```json
{"type":"order_refused","id":"o1","stock":{"coffee":0,"tea":30,"croissant":40},"msg":"Insufficient stock"}
```

The stock snapshot includes all products so the caller knows how many remain. Invalid products/quantities produce an error instead of a stock refusal.

One worker per logical CPU prepares one whole order at a time. Accepted orders enter a FIFO queue; preparation is a cancellable wait for the sum of all item preparation times. The duration in the acceptance message excludes queueing. Workers may complete different orders out of order. After preparation, the server sends:

```json
{"type":"order_ready","order_id":1}
```

Acceptance is always queued before readiness. The connection can issue more requests while waiting. There is no separate acknowledgment of readiness.

## Delivery — provider only

Request:

```json
{"type":"delivery","id":"d1","items":{"coffee":20,"croissant":10}}
```

Response:

```json
{"type":"delivery_accepted","id":"d1","stock":{"coffee":40,"tea":30,"croissant":50}}
```

All additions are atomic. Unknown products, invalid quantities, or exceeding the stock limit reject the entire delivery. Clients attempting a delivery get `forbidden`.

## Low-stock alerts — server to provider

```json
{"type":"low_stock","product":"coffee","quantity":4}
```

An alert is queued when a reservation crosses from at least 5 to below 5. Each currently low product is reported immediately after provider authentication, including shortages that arose while no provider was connected. Further decreases while already low do not generate duplicates. Deliveries and canceled-order refunds can rearm a product by restoring stock to at least 5. Alerts describe the stock at the triggering instant; later menu/delivery replies may show a newer value. Provider disconnection does not stop the shop.

## Errors

```json
{"type":"error","id":"o2","code":"invalid_items","msg":"Quantities must be integers from 1 to 1000000"}
```

`id` is omitted when it cannot be recovered safely. Codes:

| Code | Meaning |
| --- | --- |
| `invalid_frame` | Zero/oversized frame; connection closes after best-effort error |
| `invalid_message` | Invalid JSON/UTF-8, wrong types, missing/unknown fields, invalid request ID |
| `unknown_command` | Unknown `type` |
| `authentication_required` | Identify before sending commands |
| `already_authenticated` | Role cannot be changed |
| `invalid_role` | Role is neither client nor provider |
| `provider_exists` | Another live provider is registered |
| `forbidden` | This role cannot issue the command |
| `invalid_items` | Empty items, unknown product, invalid quantity |
| `stock_limit` | Delivery or refund-reservation accounting would exceed stock capacity |

## Failure and cancellation

On EOF, reset, read/write failure, keepalive failure, or output backpressure, the server cancels **all unfinished orders** for that connection. Waiting orders are removed; active workers are signaled to stop. Each reservation is refunded exactly once, including items whose simulated preparation had partly elapsed. A later order waits its entire preparation time again. Completed orders are not refunded; completion and disconnect are serialized under the shop lock. A successfully queued ready notification marks completion; TCP cannot guarantee that a failed peer actually saw it.

Deliveries also account for outstanding reservations when checking the capacity limit, ensuring subsequent cancellation refunds cannot overflow stock. Different connections' replies/events may interleave in time, but frames on an individual connection never interleave. Events have no global ordering guarantee across connections.

## Example exchange

1. Server sends `auth_req`.
2. Client sends `auth_res`; server sends `auth_ok`.
3. Client requests `menu`, then sends `order`.
4. Server immediately sends `order_accepted` (or refusal/error).
5. Client may request another menu or order while preparation runs.
6. Server later sends `order_ready` without another request.

Shutdown closes all connections and cancels remaining work. Stock and order IDs are in-memory only; persistence is an optional lab bonus, not implemented.
