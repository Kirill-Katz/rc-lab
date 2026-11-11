# Findings

- Lab requires raw socket calls; no net.Listener/net.Conn wrappers will be used in production.
- Preserve length-prefixed JSON but specify strict schema, request correlation and notifications.
- One shop mutex will protect stock, reservations, FIFO, order completion and provider registration.
- Per-connection bounded writers prevent slow peers holding shared state locks during network IO.
- Refund-aware delivery limit avoids overflow when outstanding orders are canceled.
- Tests may use standard net package as test peers; no console client is being implemented.
