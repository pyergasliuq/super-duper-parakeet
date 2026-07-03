// Package audit logs admin actions for accountability.
//
// Every /ban, /unban, /givesub, /kotek, /send, /sub, /addchannel,
// /delchannel call is recorded with: timestamp, admin id, action, target,
// reason/text. Stored in the audit_log table.
package audit

import (
	"context"
	"database/sql"
	"time"
)

// Log records one admin action.
func Log(db *sql.DB, ctx context.Context, adminID int64, action, target, detail string) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		admin_id INTEGER NOT NULL,
		action TEXT NOT NULL,
		target TEXT,
		detail TEXT,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO audit_log (admin_id, action, target, detail, created_at) VALUES (?, ?, ?, ?, ?)`,
		adminID, action, target, detail, time.Now().Format(time.RFC3339))
	return err
}

// Entry is one audit log row.
type Entry struct {
	ID        int64
	AdminID   int64
	Action    string
	Target    string
	Detail    string
	CreatedAt string
}

// Recent returns the last N audit entries.
func Recent(db *sql.DB, ctx context.Context, limit int) ([]Entry, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, admin_id, action, COALESCE(target,''), COALESCE(detail,''), created_at
		 FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.AdminID, &e.Action, &e.Target, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
