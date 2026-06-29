# SIN-22: Backend-generated client share QR

## Why

The Graphify snapshot still described the client QR modal as using `FakeQrCode`. Current `main` had already replaced that component with `qrcode.react`, but the modal still encoded the editable subscription URL in the browser. The authenticated client-links API did not return a QR image and the modal did not load the canonical share link.

## What changed

- Extended `GET /api/clients/{id}/links` with `qrPng`, a `data:image/png;base64,...` image generated from the first canonical `shareLink` by the existing `github.com/skip2/go-qrcode` dependency.
- Marked the links response `Cache-Control: no-store` because it contains client connection credentials.
- Changed the client detail modal to fetch the links endpoint when the QR action is opened, surface backend failures through the existing toast system, and render the backend PNG instead of generating a browser-side QR.
- Kept the browser-side QR component for TOTP setup, where it remains appropriate.
- Added English and Russian strings for the QR modal, loading state, alt text, close action, and failure toast.
- Regenerated Swagger output for the expanded response DTO.

## Files touched

- `internal/transport/handler/subscription_handler.go`
- `tests/transport/handler/subscription_handler_test.go`
- `frontend/src/api/clients.ts`
- `frontend/src/components/clients/client-detail-modal.tsx`
- `frontend/src/components/clients/qr-modal.tsx`
- `frontend/src/components/clients/client-detail-modal.test.tsx`
- `frontend/src/lib/i18n.tsx`
- `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`

## Verification

- Handler tests validate VLESS, Hysteria2, and Naive responses, PNG data-URI decoding, PNG signature, `no-store`, and forbidden public subscriptions for disabled or expired clients.
- Frontend regression test verifies that opening the QR action loads `/api/clients/{id}/links` and renders an image whose source is the backend `qrPng`.
- Frontend typecheck passed.
- Full repository checks are run before commit and VPS verification.
