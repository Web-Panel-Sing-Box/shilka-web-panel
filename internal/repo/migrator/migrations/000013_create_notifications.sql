CREATE TABLE notification_settings (
    id                       INTEGER PRIMARY KEY CHECK (id = 1),
    smtp_enabled             INTEGER NOT NULL DEFAULT 0,
    smtp_host                TEXT NOT NULL DEFAULT '',
    smtp_port                INTEGER NOT NULL DEFAULT 587,
    smtp_mode                TEXT NOT NULL DEFAULT 'starttls',
    smtp_from                TEXT NOT NULL DEFAULT '',
    smtp_recipients_json     TEXT NOT NULL DEFAULT '[]',
    smtp_username            TEXT NOT NULL DEFAULT '',
    smtp_password            TEXT NOT NULL DEFAULT '',
    smtp_timeout_sec         INTEGER NOT NULL DEFAULT 10,
    telegram_enabled         INTEGER NOT NULL DEFAULT 0,
    telegram_api_base        TEXT NOT NULL DEFAULT 'https://api.telegram.org',
    telegram_bot_token       TEXT NOT NULL DEFAULT '',
    telegram_chat_id         TEXT NOT NULL DEFAULT '',
    telegram_timeout_sec     INTEGER NOT NULL DEFAULT 10,
    quota_warning_percent    INTEGER NOT NULL DEFAULT 80,
    expiry_warning_hours     INTEGER NOT NULL DEFAULT 24,
    last_test_channel        TEXT NOT NULL DEFAULT '',
    last_test_ok             INTEGER NOT NULL DEFAULT 0,
    last_test_message        TEXT NOT NULL DEFAULT '',
    last_test_at             DATETIME,
    updated_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO notification_settings (id) VALUES (1);

CREATE TABLE notification_event_state (
    subject_type TEXT NOT NULL,
    subject_id   TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    fingerprint  TEXT NOT NULL,
    level        INTEGER NOT NULL,
    last_sent_at DATETIME NOT NULL,
    PRIMARY KEY (subject_type, subject_id, event_type)
);
