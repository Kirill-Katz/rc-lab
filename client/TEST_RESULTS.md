# Java client: lab requirement test results

## Summary

Tested locally on macOS/ARM64 using Java 21, Maven 3.9.9, Python 3, and Go 1.27.4 against `server/lab_description.txt` and `server/PROTOCOL.md`.

- `mvn -f client/pom.xml clean verify`: **11 tests passed**, no failures/errors/skips; executable JAR built successfully.
- `CoffeeShopClientTest` repeated in 10 separate Maven invocations: **all passed** (4 tests per invocation).
- `python3 client/tests/test_lab_integration.py --server /tmp/coffee-java-test/coffee-server`: **11 integration tests passed**, 45.495 seconds.
- No production Java or Go code was changed.

Maven was missing from PATH. Maven 3.9.9 was downloaded from Maven Central into `/tmp/coffee-java-test/`; verification used `/tmp/coffee-java-test/apache-maven-3.9.9/bin/mvn`. The Go server was built from this repository into the same temporary directory.

## Coverage against the task description

| Requirement | Check and result |
| --- | --- |
| Direct sockets; interactive client and provider; address/port parameters | Source inspection confirms `java.net.Socket`, separate console/receiver threads, and `--host`, `--port`, `--role`. Integration launches both roles using a dynamically selected TCP port. Passed. |
| Task 1: menu | All three stock values and 4/3/2-second product times displayed. Initial stock checked in 10–50 range. Passed. |
| Task 1: accepted order and delayed readiness | Two croissants, one tea, one coffee accepted promptly with order ID and 11-second estimate. Readiness arrived after the preparation interval without another request. Passed. |
| Task 1: refusal and input validation | Excess-stock multi-product order refused with stock details; no stock deducted. Unknown products and invalid/missing quantities produced console diagnostics without changing stock. Passed. |
| Task 2: requests during preparation | Menu and second order issued while first order was unfinished; both ready events reflected in history. Passed. |
| Task 2: more clients than workers | Server worker count read from startup log; count + 1 Java clients submitted 4-second orders. All accepted promptly; last completion required a second preparation wave. This machine has 18 logical CPUs. Passed. |
| Task 2: last-item race | Two Java clients released together against one remaining croissant: exactly one acceptance, one refusal, stock zero. Passed. |
| Task 3: provider delivery | Delivery increased coffee and croissant stock by exact amounts visible to another Java client. Passed. |
| Task 3: unsolicited alert | Reservation reduced coffee to four; idle provider displayed low-stock event. Passed. |
| Task 3: disconnected provider | Client continued ordering after provider exit; replacement provider received current shortage immediately after authentication. Passed. |
| Task 3: role separation | Client delivery and provider order rejected locally. Second provider received server error and exited. Passed. |
| Task 4: failed client | A Java client submitted worker count + 2 long orders, then was killed. Observer verified all active/queued reservations refunded. Another order subsequently completed after its full preparation interval. Passed. |
| Task 4: asynchronous console output | Incomplete `men` held in stdin without newline; ready event still displayed, then input was completed and processed. Passed. |
| Task 4: server disappearance | Server killed while client and provider waited for console input. Both displayed disconnect and exited with code zero within three seconds. Passed. |
| Task 4: normal shutdown | `exit` and console EOF both terminated cleanly. Passed. |
| Cross-language bonus | Real Java client/provider interoperated with the Go server over TCP. Passed on one machine. |

Existing unit tests additionally check split/coalesced framing, UTF-8 decoding, malformed JSON shapes/trailing data, truncated frames, authentication request IDs, and socket EOF.

## Test defect found and corrected

The original `tracksMultipleOrdersAndReadyEventsAsynchronously` failed in both initial Maven runs. Its wait predicate was:

```java
client.orders().stream().anyMatch(order -> !order.status().equals("ready"))
```

An empty history makes this predicate false, so the assertion could run before the reader processed the two acceptances. This was a test synchronization defect, not evidence that the client dropped an order. The test now waits for a snapshot containing exactly two orders, both ready, with the existing two-second deadline and a short sleep. The full suite and ten repeated client-test runs passed afterward.

## Scope limits and remaining observations

- Tests used loopback TCP, not two separate LAN computers. Interactive input was supplied through pipes, including an incomplete line; visual behavior in a real terminal was not assessed.
- Silent network blackholes/keepalive latency, every possible malformed wire input, slow-peer backpressure, exact FIFO scheduling, and server race detection were not comprehensively retested here. These are mainly server concerns; this run does not replace the Go server suite.
- The CLI intentionally rejects invalid product/quantity and role commands before sending them, so these scenarios verify user-facing diagnostics, not the server's malformed-request handling.
- Framing tests do not exhaustively exercise every payload-size boundary.
- Root `README.md` and `server/PROTOCOL.md` still say no client exists; these pre-existing statements are stale. Build/run/test instructions for the implemented Java client are in `client/README.md`.
- Maven emits shading warnings for Jackson module descriptors and overlapping manifest/license resources; packaging and execution succeeded.

## Reproduce

From the repository root:

```sh
mvn -f client/pom.xml clean verify
(cd server && go build -o /tmp/coffee-server ./cmd)
python3 client/tests/test_lab_integration.py --server /tmp/coffee-server
```

The integration suite uses only Python's standard library and cleans up its processes and sockets.
