// Maple Gateway 种子数据填充命令。
//
// 独立于 gateway server 进程运行：连接数据库 → 自动建表（复用 SQL 迁移器，
// 幂等）→ 填充各实体种子数据（已存在则跳过/回填，不覆盖运营数据）。
//
// 用法：
//
//	go run ./cmd/seed
//
// 依赖 MAPLE_DATABASE_URL（缺省回退开发默认值）；admin 邮箱/密码可经
// MAPLE_ADMIN_EMAIL / MAPLE_ADMIN_PASSWORD 覆盖。
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/NeoPlayful/maple-gateway/server/pkg"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	if err := run(); err != nil {
		log.Fatalf("seed: %v", err)
	}
}

func run() error {
	ctx := context.Background()

	url := os.Getenv("MAPLE_DATABASE_URL")
	if url == "" {
		url = "postgres://maple:maple@localhost:5432/maple?sslmode=disable"
	}

	// 1. 连接数据库。
	db, err := pkg.NewDB(ctx, url)
	if err != nil {
		return err
	}
	defer db.Close()

	// 2. 自动建表：执行 migrations/*.sql。对全新库建齐所有表；
	//    对已存在库靠 schema_migrations 幂等跳过，不 diff 现有结构。
	mig := pkg.NewMigrator(db.Pool, resolveMigrationsDir())
	if err := mig.Run(ctx); err != nil {
		return err
	}

	client := pkg.NewEntClient(db)

	// 3. 填充种子数据（各 seedXxx 内部幂等）。
	return seedAll(ctx, client)
}

// seedAll 依次填充各实体种子数据。
func seedAll(ctx context.Context, client *ent.Client) error {
	if err := seedAdmin(ctx, client); err != nil {
		return err
	}
	pkg.Log().Info("seed data filled")
	return nil
}

// resolveMigrationsDir 解析 migrations 目录：优先可执行文件同目录（便于把
// 二进制放到别处运行），再回退仓库 server/migrations。
func resolveMigrationsDir() string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "migrations")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}
	for _, rel := range []string{"migrations", "../migrations"} {
		if st, err := os.Stat(filepath.Join(rel, "0001_init.sql")); err == nil && !st.IsDir() {
			if abs, aerr := filepath.Abs(rel); aerr == nil {
				return abs
			}
			return rel
		}
	}
	return "migrations"
}
