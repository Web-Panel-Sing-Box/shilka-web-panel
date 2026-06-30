# SMTP and Telegram notifications

## Scope

SIN-14 adds external notifications for client quota and expiry transitions and for sing-box lifecycle failures. The implementation keeps provider credentials outside the generic `settings` table and never returns raw secrets from an API.

## Backend changes

- Migration `000013_create_notifications.sql` adds a singleton `notification_settings` table and persistent `notification_event_state` table.
- `NotificationRepo` stores provider configuration, secret values, persisted test results, and dedupe state.
- `notification.Service` exposes masked settings, applies retain-or-clear secret semantics, validates provider configuration, and dispatches through a bounded queue.
- SMTP supports STARTTLS and implicit TLS, authentication, multiple recipients, and bounded connection deadlines. The default port is 587.
- Telegram uses a configurable API base, rejects non-HTTPS endpoints except loopback HTTP fixtures, forbids redirects, and applies an HTTP timeout.
- Redaction removes configured credentials, SMTP AUTH PLAIN values, credential URL userinfo, Telegram bot tokens, and sensitive query values before notification logs or core API errors are emitted.
- Quota and expiry state records store a configuration fingerprint and warning or terminal level. Warnings fire once per crossing, terminal events fire once, quota or expiry changes begin a new cycle, and dropping below a threshold clears existing state without an unconditional SQLite write on every worker tick.
- Core error state suppresses identical apply, check, reload, start, stop, and restart failures for a five-minute cooldown.
- The stats worker observes active clients before terminal disable. Applier and core lifecycle paths report failures through the notification service.

## API changes

- `GET /api/settings/notifications` returns provider configuration plus `passwordConfigured` and `botTokenConfigured` flags only.
- `PUT /api/settings/notifications` preserves existing secrets when an empty secret is submitted. `clearPassword` and `clearBotToken` explicitly remove them.
- `POST /api/settings/notifications/test` accepts `smtp` or `telegram`, sends synchronously, and persists the sanitized result.
- Swagger artifacts in `docs/` include all three endpoints and their request and response schemas.

## Frontend changes

- Settings now contains SMTP and Telegram sections with enable toggles, controlled inputs, masked secret fields, explicit clear actions, provider timeouts, warning thresholds, and separate test buttons.
- Test actions await settings persistence before sending. Every save and test error is surfaced through a toast.
- The last test result is rendered from backend-persisted state.
- All visible copy was added to both English and Russian dictionaries.

## Verification

- Notification service tests cover secret retain, clear, and masking behavior; quota warning, terminal, reset, and restart dedupe; core cooldown; redaction; and a loopback Telegram HTTP server.
- Repository tests run the real embedded migrations and verify settings and event-state persistence.
- Handler tests verify the API does not expose secrets and that test sends reach an injected provider.
- Stats and core tests verify notification hooks, and the applier test verifies check failures use the correct event.
- Frontend tests verify notification settings are saved before a test send and that persisted test status is displayed.
- Full Go and frontend build, vet, typecheck, and test results are recorded in the SIN-14 Linear completion comment.
