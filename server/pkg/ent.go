package pkg

import (
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/NeoPlayful/maple-gateway/server/ent"
)

// NewEntClient 基于 DB 的 database/sql 连接构造 Ent 客户端（业务层唯一数据访问入口）。
func NewEntClient(db *DB) *ent.Client {
	return ent.NewClient(ent.Driver(sql.OpenDB(dialect.Postgres, db.SQL)))
}
