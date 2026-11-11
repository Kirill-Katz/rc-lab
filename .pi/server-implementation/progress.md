# Progress

- Protocol written before implementation in server/PROTOCOL.md.
- Replaced incomplete connection scaffolding with direct-socket server, bounded output queues, JSON validation, shared shop state and CPU-sized worker pool.
- Added Darwin and Linux socket options/send implementations; listener shutdown uses poll and context cancellation.
- Reviewed reservation/refund accounting, writer failure path, canceled FIFO compaction and descriptor ownership. Accepted notification is enqueued under the stock lock before worker wakeup; no network writes hold that lock.
- Initial checks: `cd server && go test ./... && go vet ./...` passed (tests not yet added).
- Planning helper failed due absent memory configuration; project-local plan files used without modifying global settings.
- Added framing, protocol validation, roles, alerts, delivery limits, FIFO/readiness, multi-worker, last-item contention, concurrent delivery/order, cancellation/refund, backpressure, maximum-sized invalid-input, and real TCP/shutdown tests.
- First race-test run failed because the test incorrectly expected refunds to discard an earlier provider delivery. Corrected expected stock from 6 to 9 and adjusted the next threshold-crossing order.
- Verification review caught an oversized-error edge case: echoing a nearly maximum-sized unknown command/product could exceed the output framing limit. Bounded error detail to 256 UTF-8 bytes and added regression coverage.
- Hardened accept failures to retry temporary resource exhaustion/reset errors without taking down existing clients. Renamed the old client_manager.go scaffold to shop.go to match its responsibility; go mod tidy marked x/sys as a direct dependency.
- Added root README.md with build/run commands, protocol link, requirement mapping, tests, supported platforms and intentional server-only limitations.
- Passed: go test -race ./..., go vet ./..., GOOS=linux GOARCH=amd64 go build, and two separate five-run race-test passes. Race-enabled coverage: 88.1% for the connection/shop package.
- CLI smoke check passed: invalid port exits with usage, configured port starts, SIGTERM exits successfully.
- git diff --check passed; client/ has no files. Existing user moves/deletions and prior task metadata were not reverted.
