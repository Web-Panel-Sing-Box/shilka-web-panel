# SIN-10: VLESS HTTPUpgrade transport support

## Why

The frontend exposed `httpupgrade`, but the backend domain only accepted TCP, WebSocket, and gRPC. The inbound service silently normalized HTTPUpgrade to TCP, so the generated sing-box configuration did not match the administrator's selection. The existing WebSocket path and gRPC service-name controls were also visual-only and never reached the API.

## What changed

- Added the `httpupgrade` transmission to the backend domain and VLESS normalization path.
- Added persisted HTTPUpgrade path and host settings, with a generated `/short-id` path when none is supplied.
- Passed WebSocket path, gRPC service name, and HTTPUpgrade path/host through local handlers, remote-node requests, frontend DTOs, and the inbound form.
- Updated create and update flows so missing transport defaults are generated and stored after transport changes.
- Added sing-box server and client transport generation using `type`, `host`, and `path` fields from the official V2Ray transport schema.
- Added HTTPUpgrade parameters to VLESS share links and JSON subscriptions.
- Converted transport controls to controlled inputs and added English/Russian labels.
- Regenerated Swagger output for the new request and response fields.

## Files touched

- Backend domain, inbound service, node transport, HTTP handler, sing-box schema/generator, and subscription builders.
- Frontend inbound API types, `useInboundForm`, inbound form modal, tests, and i18n.
- Backend service, generator, handler, subscription, node, and real sing-box integration tests.
- Generated Swagger files.

## Verification

- Service tests cover create and update normalization plus generated default paths.
- Handler test covers HTTPUpgrade API round-trip persistence.
- Generator and subscription tests verify server config, VLESS URI parameters, and client JSON transport.
- Integration test generated an HTTPUpgrade config and passed `sing-box check` against local sing-box 1.13.12.
- Frontend test selected HTTPUpgrade, entered path/host, and verified the exact create payload.
- Full repository checks are run before commit and VPS verification.
