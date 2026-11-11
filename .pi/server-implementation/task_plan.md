# Coffee server implementation

## Goal
Document the protocol first; finish and verify Tasks 1–4, server only.

## Phases
- [x] Inspect starter and write server/PROTOCOL.md.
- [x] Implement direct-socket server and concurrent shop state.
- [x] Add automated tests, race checks, and run instructions.

## Next Step
Report completed server implementation and verification results.

## Findings
Starter already uses Unix sockets and four-byte JSON framing, but passes the listening FD to handlers, strips payload bytes twice, uses nil maps, and has no shop behavior. Preserve module and directory naming. Working tree already contains user moves from repository root into server/; do not revert them.

## Errors Encountered
- Planning helper cannot initialize because memory localPath is not configured. Use project-local .pi/server-implementation files instead of changing global settings.
- First race-test run found an incorrect test expectation: refund stock includes the provider's earlier delivery (9, not 6). Corrected expected stock and subsequent threshold-crossing quantity; no implementation failure identified.
