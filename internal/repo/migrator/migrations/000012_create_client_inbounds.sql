-- SIN-11: a local client may bind to multiple inbounds.
-- client_inbounds is the source of truth for local clients; clients.inbound_id is
-- kept as the backward-compat "primary" binding (also used by the remote-node path).
CREATE TABLE IF NOT EXISTS client_inbounds (
    client_id  INTEGER NOT NULL REFERENCES clients(id)  ON DELETE CASCADE,
    inbound_id INTEGER NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
    PRIMARY KEY (client_id, inbound_id)
);

CREATE INDEX IF NOT EXISTS idx_client_inbounds_inbound ON client_inbounds (inbound_id);

-- Backfill the existing single binding so current clients keep their inbound.
INSERT OR IGNORE INTO client_inbounds (client_id, inbound_id)
SELECT id, inbound_id FROM clients;
