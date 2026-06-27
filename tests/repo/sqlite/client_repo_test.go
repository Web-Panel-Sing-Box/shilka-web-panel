package sqlite_test

import (
	"context"
	"database/sql"
	"testing"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	sqliterepo "sing-box-web-panel/internal/repo/sqlite"
)

func TestClientBatchMutationRollsBackWhenAnyIDIsMissing(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE clients (
		id INTEGER PRIMARY KEY,
		status TEXT NOT NULL,
		enabled INTEGER NOT NULL,
		used_up INTEGER NOT NULL DEFAULT 0,
		used_down INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO clients (id, status, enabled) VALUES (1, 'active', 1)`); err != nil {
		t.Fatal(err)
	}

	repository := sqliterepo.NewClientRepo(db)
	err = repository.SetStatusMany(context.Background(), []int64{1, 999}, domain.ClientStatusDisabled, false)
	if err != repo.ErrNotFound {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	var status string
	var enabled bool
	if err := db.QueryRow(`SELECT status, enabled FROM clients WHERE id = 1`).Scan(&status, &enabled); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.ClientStatusActive) || !enabled {
		t.Fatalf("transaction was not rolled back: status=%s enabled=%v", status, enabled)
	}
}
