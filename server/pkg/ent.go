package pkg

import (
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
)

// NewEntClient 基于 DB 的 database/sql 连接构造 Ent 客户端。
// 过渡期：DB.SQL 与 DB.Pool 指向同一 PostgreSQL，双连接并存。
func NewEntClient(db *DB) *ent.Client {
	return ent.NewClient(ent.Driver(sql.OpenDB(dialect.Postgres, db.SQL)))
}
