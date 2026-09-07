package pkg

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB 持有 pgx 连接池。
type DB struct {
	Pool *pgxpool.Pool
}

// NewDB 建立 PostgreSQL 连接池。
func NewDB(ctx context.Context, url string) (*DB, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	// 启动探测，确认 DB 可用。
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

// Close 关闭连接池。
func (d *DB) Close() {
	if d != nil && d.Pool != nil {
		d.Pool.Close()
	}
}
