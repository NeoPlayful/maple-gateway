package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrator 按文件名顺序执行 migrations/*.sql，每次成功记录到
// schema_migrations 表，保证可重复执行与幂等。
type Migrator struct {
	pool *pgxpool.Pool
	dir  string
}

// NewMigrator 构造迁移器。
func NewMigrator(pool *pgxpool.Pool, dir string) *Migrator {
	return &Migrator{pool: pool, dir: dir}
}

// Run 执行目录内所有尚未应用的迁移。
func (m *Migrator) Run(ctx context.Context) error {
	if err := m.ensureTable(ctx); err != nil {
		return err
	}

	applied, err := m.applied(ctx)
	if err != nil {
		return err
	}

	files, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	for _, f := range files {
		name := f.Name()
		if f.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		if applied[name] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(m.dir, name))
		if err != nil {
			return err
		}

		// 每个迁移在独立事务中执行，失败即回滚并中止。
		tx, err := m.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(raw)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations(name, applied_at) VALUES($1, now())`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *Migrator) ensureTable(ctx context.Context) error {
	_, err := m.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name       TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	return err
}

func (m *Migrator) applied(ctx context.Context) (map[string]bool, error) {
	rows, err := m.pool.Query(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}
