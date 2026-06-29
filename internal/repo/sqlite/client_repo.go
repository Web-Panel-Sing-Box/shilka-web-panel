package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
)

type ClientRepo struct {
	db *sql.DB
}

func NewClientRepo(db *sql.DB) *ClientRepo { return &ClientRepo{db: db} }

const clientColumns = `id, inbound_id, name, uuid, password, used_up, used_down, total_quota,
	expiry, status, sub_token, start_after_first_use, enabled, first_used_at, last_used_at, node_id, remote_id,
	last_synced_at, created_at, updated_at`

func (r *ClientRepo) Create(ctx context.Context, c *domain.Client) (err error) {
	inbounds := normalizeInboundSet(c.InboundIDs, c.InboundID)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create client tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	result, err := tx.ExecContext(ctx,
		`INSERT INTO clients (inbound_id, name, uuid, password, total_quota, expiry,
		                      status, sub_token, start_after_first_use, enabled, node_id, remote_id, last_synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.InboundID, c.Name, c.UUID, c.Password, c.TotalQuota, c.Expiry,
		c.Status, c.SubToken, c.StartAfterFirstUse, c.Enabled, nullableInt64(c.NodeID), c.RemoteID, c.LastSyncedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return repo.ErrExist
		}
		return fmt.Errorf("insert client: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get client id: %w", err)
	}
	c.ID = id
	if err = replaceClientInbounds(ctx, tx, id, inbounds); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit create client: %w", err)
	}
	c.InboundIDs = inbounds
	return nil
}

func (r *ClientRepo) GetByID(ctx context.Context, id int64) (*domain.Client, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+clientColumns+` FROM clients WHERE id = ?`, id)
	return r.scanWithInbounds(ctx, row)
}

func (r *ClientRepo) GetByRemote(ctx context.Context, nodeID int64, remoteID string) (*domain.Client, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+clientColumns+` FROM clients WHERE node_id = ? AND remote_id = ?`, nodeID, remoteID)
	return r.scanWithInbounds(ctx, row)
}

func (r *ClientRepo) GetBySubToken(ctx context.Context, token string) (*domain.Client, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+clientColumns+` FROM clients WHERE sub_token = ?`, token)
	return r.scanWithInbounds(ctx, row)
}

func (r *ClientRepo) List(ctx context.Context) ([]domain.Client, error) {
	return r.query(ctx, `SELECT `+clientColumns+` FROM clients ORDER BY id DESC`)
}

func (r *ClientRepo) ListByInbound(ctx context.Context, inboundID int64) ([]domain.Client, error) {
	return r.query(ctx, `SELECT `+clientColumns+` FROM clients WHERE inbound_id = ? ORDER BY id DESC`, inboundID)
}

// ListEnabled returns every client whose own flag is on. The generator further
// filters by the inbound's enabled flag.
func (r *ClientRepo) ListEnabled(ctx context.Context) ([]domain.Client, error) {
	return r.query(ctx, `SELECT `+clientColumns+` FROM clients WHERE enabled = 1 AND node_id IS NULL ORDER BY inbound_id`)
}

func (r *ClientRepo) query(ctx context.Context, q string, args ...any) ([]domain.Client, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query clients: %w", err)
	}
	defer rows.Close()

	var out []domain.Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.loadInboundIDsInto(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ClientRepo) Update(ctx context.Context, c *domain.Client) (err error) {
	inbounds := normalizeInboundSet(c.InboundIDs, c.InboundID)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update client tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.ExecContext(ctx,
		`UPDATE clients SET inbound_id = ?, name = ?, uuid = ?, password = ?, total_quota = ?,
		                    expiry = ?, status = ?, start_after_first_use = ?, enabled = ?,
		                    node_id = ?, remote_id = ?, last_synced_at = ?,
		                    updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		c.InboundID, c.Name, c.UUID, c.Password, c.TotalQuota, c.Expiry,
		c.Status, c.StartAfterFirstUse, c.Enabled, nullableInt64(c.NodeID), c.RemoteID, c.LastSyncedAt, c.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return repo.ErrExist
		}
		return fmt.Errorf("update client: %w", err)
	}
	if err = replaceClientInbounds(ctx, tx, c.ID, inbounds); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit update client: %w", err)
	}
	c.InboundIDs = inbounds
	return nil
}

func (r *ClientRepo) UpsertRemote(ctx context.Context, nodeID int64, remoteID string, inboundID int64, c *domain.Client) error {
	if remoteID == "" {
		return fmt.Errorf("remote client id is required")
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO clients (node_id, remote_id, inbound_id, name, uuid, password, used_up, used_down,
		                      total_quota, expiry, status, sub_token, start_after_first_use, enabled,
		                      first_used_at, last_synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(node_id, remote_id) WHERE node_id IS NOT NULL AND remote_id <> ''
		 DO UPDATE SET inbound_id = excluded.inbound_id,
		               name = excluded.name,
		               uuid = excluded.uuid,
		               password = excluded.password,
		               used_up = excluded.used_up,
		               used_down = excluded.used_down,
		               total_quota = excluded.total_quota,
		               expiry = excluded.expiry,
		               status = excluded.status,
		               sub_token = excluded.sub_token,
		               start_after_first_use = excluded.start_after_first_use,
		               enabled = excluded.enabled,
		               first_used_at = excluded.first_used_at,
		               last_synced_at = CURRENT_TIMESTAMP,
		               updated_at = CURRENT_TIMESTAMP`,
		nodeID, remoteID, inboundID, c.Name, c.UUID, c.Password, c.UsedUp, c.UsedDown, c.TotalQuota,
		c.Expiry, c.Status, c.SubToken, c.StartAfterFirstUse, c.Enabled, c.FirstUsedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert remote client: %w", err)
	}
	return nil
}

func (r *ClientRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM clients WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete client: %w", err)
	}
	return nil
}

func (r *ClientRepo) SetStatus(ctx context.Context, id int64, status domain.ClientStatus, enabled bool) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE clients SET status = ?, enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status, enabled, id)
	if err != nil {
		return fmt.Errorf("set client status: %w", err)
	}
	return nil
}

func (r *ClientRepo) ResetTraffic(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE clients SET used_up = 0, used_down = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("reset client traffic: %w", err)
	}
	return nil
}

func (r *ClientRepo) DeleteMany(ctx context.Context, ids []int64) error {
	return r.execClientBatch(ctx, "delete clients", `DELETE FROM clients WHERE id = ?`, ids, func(id int64) []any {
		return []any{id}
	})
}

func (r *ClientRepo) SetStatusMany(ctx context.Context, ids []int64, status domain.ClientStatus, enabled bool) error {
	return r.execClientBatch(ctx, "set client statuses",
		`UPDATE clients SET status = ?, enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		ids, func(id int64) []any { return []any{status, enabled, id} })
}

func (r *ClientRepo) ResetTrafficMany(ctx context.Context, ids []int64) error {
	return r.execClientBatch(ctx, "reset client traffic",
		`UPDATE clients SET used_up = 0, used_down = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		ids, func(id int64) []any { return []any{id} })
}

func (r *ClientRepo) execClientBatch(
	ctx context.Context,
	op string,
	query string,
	ids []int64,
	args func(int64) []any,
) (err error) {
	if len(ids) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin %s transaction: %w", op, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare %s: %w", op, err)
	}
	defer stmt.Close()
	for _, id := range ids {
		result, execErr := stmt.ExecContext(ctx, args(id)...)
		if execErr != nil {
			return fmt.Errorf("%s: %w", op, execErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("%s rows affected: %w", op, rowsErr)
		}
		if rows == 0 {
			return repo.ErrNotFound
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", op, err)
	}
	return nil
}

func (r *ClientRepo) SetFirstUsed(ctx context.Context, id int64, at any) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE clients SET first_used_at = ? WHERE id = ? AND first_used_at IS NULL`, at, id)
	if err != nil {
		return fmt.Errorf("set client first used: %w", err)
	}
	return nil
}

func (r *ClientRepo) SetLastUsedAt(ctx context.Context, id int64, at time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE clients SET last_used_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, at, id)
	if err != nil {
		return fmt.Errorf("set client last used: %w", err)
	}
	return nil
}

// AddTraffic applies a batch of per-client counter increments in one transaction.
func (r *ClientRepo) AddTraffic(ctx context.Context, deltas []domain.TrafficDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin traffic tx: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx,
		`UPDATE clients SET used_up = used_up + ?, used_down = used_down + ?,
		                    last_used_at = CURRENT_TIMESTAMP,
		                    updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare traffic update: %w", err)
	}
	defer stmt.Close()

	for _, d := range deltas {
		if d.Up == 0 && d.Down == 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, d.Up, d.Down, d.ClientID); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply traffic delta: %w", err)
		}
	}
	return tx.Commit()
}

func (r *ClientRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count clients: %w", err)
	}
	return n, nil
}

// CountOnline returns the number of clients whose last_used_at is at or after
// `since` — i.e. the heartbeat-based "online now" gauge.
func (r *ClientRepo) CountOnline(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM clients WHERE last_used_at IS NOT NULL AND last_used_at >= ?`,
		since.UTC()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count online clients: %w", err)
	}
	return n, nil
}

// CountByInbound returns a map of inbound id -> client count.
func (r *ClientRepo) CountByInbound(ctx context.Context) (map[int64]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT inbound_id, COUNT(*) FROM clients GROUP BY inbound_id`)
	if err != nil {
		return nil, fmt.Errorf("count clients by inbound: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]int)
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan client count: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}

// SumTraffic returns the cumulative up/down byte totals across all clients.
func (r *ClientRepo) SumTraffic(ctx context.Context) (up int64, down int64, err error) {
	err = r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(used_up), 0), COALESCE(SUM(used_down), 0) FROM clients`).Scan(&up, &down)
	if err != nil {
		return 0, 0, fmt.Errorf("sum client traffic: %w", err)
	}
	return up, down, nil
}

func scanClient(s rowScanner) (*domain.Client, error) {
	var c domain.Client
	var nodeID sql.NullInt64
	var lastSyncedAt sql.NullTime
	err := s.Scan(
		&c.ID, &c.InboundID, &c.Name, &c.UUID, &c.Password, &c.UsedUp, &c.UsedDown, &c.TotalQuota,
		&c.Expiry, &c.Status, &c.SubToken, &c.StartAfterFirstUse, &c.Enabled, &c.FirstUsedAt,
		&c.LastUsedAt, &nodeID, &c.RemoteID, &lastSyncedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repo.ErrNotFound
		}
		return nil, fmt.Errorf("scan client: %w", err)
	}
	c.NodeID = ptrFromNullInt64(nodeID)
	c.LastSyncedAt = ptrFromNullTime(lastSyncedAt)
	return &c, nil
}

// scanWithInbounds scans a single client row and hydrates its InboundIDs from
// the client_inbounds join table.
func (r *ClientRepo) scanWithInbounds(ctx context.Context, s rowScanner) (*domain.Client, error) {
	c, err := scanClient(s)
	if err != nil {
		return nil, err
	}
	if err := r.fillInbounds(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// fillInbounds populates c.InboundIDs from the join table, falling back to the
// primary inbound_id when no join rows exist (e.g. remote clients).
func (r *ClientRepo) fillInbounds(ctx context.Context, c *domain.Client) error {
	if c == nil {
		return nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT inbound_id FROM client_inbounds WHERE client_id = ? ORDER BY inbound_id`, c.ID)
	if err != nil {
		return fmt.Errorf("load client inbounds: %w", err)
	}
	defer rows.Close()
	var set []int64
	for rows.Next() {
		var iid int64
		if err := rows.Scan(&iid); err != nil {
			return fmt.Errorf("scan client inbound: %w", err)
		}
		set = append(set, iid)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.InboundIDs = fallbackInboundSet(set, c.InboundID)
	return nil
}

// loadInboundIDsInto hydrates InboundIDs for a batch of clients in one query to
// avoid N+1 lookups.
func (r *ClientRepo) loadInboundIDsInto(ctx context.Context, clients []domain.Client) error {
	if len(clients) == 0 {
		return nil
	}
	placeholders := make([]string, len(clients))
	args := make([]any, len(clients))
	for i := range clients {
		placeholders[i] = "?"
		args[i] = clients[i].ID
	}
	q := `SELECT client_id, inbound_id FROM client_inbounds WHERE client_id IN (` +
		strings.Join(placeholders, ",") + `) ORDER BY inbound_id`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("load client inbounds: %w", err)
	}
	defer rows.Close()
	byClient := make(map[int64][]int64, len(clients))
	for rows.Next() {
		var cid, iid int64
		if err := rows.Scan(&cid, &iid); err != nil {
			return fmt.Errorf("scan client inbound: %w", err)
		}
		byClient[cid] = append(byClient[cid], iid)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range clients {
		clients[i].InboundIDs = fallbackInboundSet(byClient[clients[i].ID], clients[i].InboundID)
	}
	return nil
}

// replaceClientInbounds rewrites a client's join rows to exactly inboundIDs.
func replaceClientInbounds(ctx context.Context, tx *sql.Tx, clientID int64, inboundIDs []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM client_inbounds WHERE client_id = ?`, clientID); err != nil {
		return fmt.Errorf("clear client inbounds: %w", err)
	}
	for _, iid := range inboundIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO client_inbounds (client_id, inbound_id) VALUES (?, ?)`,
			clientID, iid); err != nil {
			return fmt.Errorf("insert client inbound: %w", err)
		}
	}
	return nil
}

// normalizeInboundSet dedupes ids (order-preserving) and falls back to the
// primary single binding when the set is empty.
func normalizeInboundSet(ids []int64, primary int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 && primary != 0 {
		out = append(out, primary)
	}
	return out
}

// fallbackInboundSet returns set, or [primary] when set is empty.
func fallbackInboundSet(set []int64, primary int64) []int64 {
	if len(set) > 0 {
		return set
	}
	if primary != 0 {
		return []int64{primary}
	}
	return nil
}
