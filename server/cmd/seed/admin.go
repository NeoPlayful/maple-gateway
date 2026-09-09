package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	entadmin "github.com/NeoPlayful/maple-gateway/server/ent/admin"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// seedAdmin 创建默认管理员（幂等：已存在则跳过）。
// 邮箱/密码来自环境变量，缺省用开发默认值。
func seedAdmin(ctx context.Context, client *ent.Client) error {
	email := os.Getenv("MAPLE_ADMIN_EMAIL")
	if email == "" {
		email = "admin@maple.com"
	}
	password := os.Getenv("MAPLE_ADMIN_PASSWORD")
	if password == "" {
		password = "admin123"
	}

	exists, err := client.Admin.Query().Where(entadmin.EmailEQ(email)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check admin exists: %w", err)
	}
	if exists {
		pkg.Log().Info("default admin already exists, skip", zap.String("email", email))
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	now := time.Now()
	if _, err := client.Admin.Create().
		SetEmail(email).
		SetPasswordHash(string(hash)).
		SetName("admin").
		SetStatus("active").
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx); err != nil {
		return fmt.Errorf("insert admin: %w", err)
	}
	pkg.Log().Info("default admin seeded", zap.String("email", email))
	return nil
}
