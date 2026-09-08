package pkg

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	// 注册 pgx stdlib driver（database/sql 供 Ent 使用）。
	_ "github.com/jackc/pgx/v5/stdlib"
)

// DB 持有 PostgreSQL 连接。
// 业务数据访问统一走 Ent（底层为 SQL 的 pgx stdlib driver）。
//   - SQL  为 database/sql 连接（pgx stdlib 驱动），供 Ent 客户端使用；
//   - Pool 仅保留给迁移工具（pkg/migrate.go 需在事务内执行多语句 SQL 文件），
//     不再向业务层 repository 暴露。
type DB struct {
	Pool *pgxpool.Pool
	SQL  *sql.DB
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
	// database/sql 层：pgx stdlib 驱动（Ent 使用）。复用同 DSN，独立连接池。
	sqldb, err := sql.Open("pgx", url)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if err := sqldb.PingContext(pingCtx); err != nil {
		sqldb.Close()
		pool.Close()
		return nil, err
	}
	return &DB{Pool: pool, SQL: sqldb}, nil
}

// Close 关闭连接池。
func (d *DB) Close() {
	if d != nil {
		if d.SQL != nil {
			_ = d.SQL.Close()
		}
		if d.Pool != nil {
			d.Pool.Close()
		}
	}
}
