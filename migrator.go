package main

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"
)

// migrationFS embeds every .sql file under migrations/. Files are named
// "NNN_description.sql"; the numeric prefix is the version. SQL migrations are
// applied in ascending version order, each in its own transaction. Some
// migrations need Go code (e.g. JSON-blob backfill into normalized tables) and
// register themselves in goMigrations below.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// goMigrations holds Go-only or mixed Go+SQL migrations keyed by version.
// Each runs inside the migrator's transaction; the migrator stamps
// schema_version on success.
var goMigrations = map[int]func(*sql.Tx) error{}

func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version    INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	pending, err := pendingMigrations(db)
	if err != nil {
		return err
	}
	for _, m := range pending {
		if err := applyMigration(db, m); err != nil {
			return fmt.Errorf("migration %03d (%s): %w", m.version, m.name, err)
		}
	}
	return nil
}

type migration struct {
	version int
	name    string
	body    string
	goFunc  func(*sql.Tx) error
}

func pendingMigrations(db *sql.DB) ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	byVersion := make(map[int]migration)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		v, err := parseVersion(name)
		if err != nil {
			return nil, err
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		byVersion[v] = migration{version: v, name: name, body: string(body)}
	}
	for v, fn := range goMigrations {
		m := byVersion[v]
		m.version = v
		m.goFunc = fn
		if m.name == "" {
			m.name = fmt.Sprintf("%03d_go.go", v)
		}
		byVersion[v] = m
	}

	all := make([]migration, 0, len(byVersion))
	for _, m := range byVersion {
		all = append(all, m)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].version < all[j].version })

	current, err := currentSchemaVersion(db)
	if err != nil {
		return nil, err
	}
	pending := all[:0]
	for _, m := range all {
		if m.version > current {
			pending = append(pending, m)
		}
	}
	return pending, nil
}

func currentSchemaVersion(db *sql.DB) (int, error) {
	var v sql.NullInt64
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("current schema_version: %w", err)
	}
	return int(v.Int64), nil
}

func parseVersion(filename string) (int, error) {
	underscore := strings.Index(filename, "_")
	if underscore < 1 {
		return 0, fmt.Errorf("migration filename %q must start with NNN_", filename)
	}
	var v int
	if _, err := fmt.Sscanf(filename[:underscore], "%d", &v); err != nil {
		return 0, fmt.Errorf("migration filename %q: cannot parse version: %w", filename, err)
	}
	return v, nil
}

func applyMigration(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if m.body != "" {
		if _, err := tx.Exec(m.body); err != nil {
			return err
		}
	}
	if m.goFunc != nil {
		if err := m.goFunc(tx); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`,
		m.version, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return err
	}
	return tx.Commit()
}
