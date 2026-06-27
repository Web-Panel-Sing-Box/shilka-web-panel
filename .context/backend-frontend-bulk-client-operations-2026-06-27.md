# SIN-13 Bulk Client Operations

## Summary

Implemented ordered bulk delete, status, and traffic-reset operations for local and remote clients. The API reports one result per requested client, preserves request order, de-duplicates IDs, and keeps failures isolated by client and remote node. The Clients page now supports filtered selection, an indeterminate select-all control, bulk actions, destructive confirmations, and visible partial failures.

## Why

Client mutations previously required one request and one UI interaction per client. This was slow for large client lists, could schedule repeated sing-box config applies, and made it difficult to distinguish successful remote mutations from failures on other nodes.

## Backend Changes

- Added `POST /api/clients/bulk/delete`, `POST /api/clients/bulk/set-status`, and `POST /api/clients/bulk/reset-traffic`.
- Added matching authenticated node API routes under `/api/node/v1/clients/bulk/*`.
- Added a stable response envelope containing ordered `{ id, ok, error }` results.
- Added strict request validation for non-empty positive IDs and `active`/`disabled` bulk statuses.
- Added transactional SQLite batch methods for delete, status, and traffic reset. A missing row rolls back the entire local write group.
- Added client-service preflight classification. Local delete/status batches trigger one debounced config apply; traffic reset does not trigger an apply.
- Added remote grouping by `nodeId`. Each node receives one HTTP bulk request, and transport or item failures affect only that node's clients.
- Added batched master-cache updates after successful remote mutations.
- Kept all existing single-client endpoints unchanged.
- Regenerated Swagger definitions and routes.

## Frontend Changes

- Added typed bulk client request and response functions.
- Added store actions that await each mutation, refresh shared data once, and return per-client results to the page.
- Added row checkboxes and a filtered select-all checkbox with an indeterminate state.
- Selection is cleared when filters change, and checkbox interaction does not open the client detail modal.
- Added Enable, Disable, Reset traffic, Delete, and Clear actions.
- Delete and traffic reset require confirmation.
- Successful IDs leave the selection; failed IDs remain selected and are listed with client names and backend errors.
- Added English and Russian translations for every new UI string.

## Main Files Touched

- `internal/services/client/service.go`
- `internal/repo/sqlite/client_repo.go`
- `internal/services/node/client.go`
- `internal/services/node/service.go`
- `internal/transport/handler/client_handler.go`
- `internal/transport/handler/node_handler.go`
- `frontend/src/api/clients.ts`
- `frontend/src/lib/store.tsx`
- `frontend/src/pages/ClientsPage.tsx`
- `frontend/src/components/clients/clients-table.tsx`
- `docs/`

## Verification

- Targeted Go tests cover transaction rollback, one-trigger local batching, remote ID mapping, one HTTP request per node, mixed local/remote partial failures, and request validation.
- Targeted Clients page tests cover filtered select-all, indeterminate state, checkbox isolation, delete/reset confirmation, and retained partial failures.
- `go test ./tests/...` passed.
- `go vet ./...` passed.
- `go build ./...` passed.
- `pnpm typecheck` passed.
- `pnpm test` passed: 13 files, 21 tests.
- `pnpm build` passed.
- SIN-13 frontend tests passed: 4/4.

## Deployment Safety

The production rollout must replace only `/opt/shilka/bin/shilka`, preserve `/etc/shilka/prod.yaml`, the database, and TLS files, and compare TLS file hashes before and after restart. Do not use the installer or any certificate command. A working sudo credential is required because the supplied password was rejected during read-only rollout preparation.
