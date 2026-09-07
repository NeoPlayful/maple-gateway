package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// seedDefaultAdmin 创建默认管理员（幂等：已存在则跳过）。
// 邮箱/密码来自环境变量，缺省用开发默认值。
func seedDefaultAdmin(ctx context.Context, pool *pgxpool.Pool) error {
	email := os.Getenv("MAPLE_ADMIN_EMAIL")
	if email == "" {
		email = "admin@maple.local"
	}
	password := os.Getenv("MAPLE_ADMIN_PASSWORD")
	if password == "" {
		password = "admin123"
	}

	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM admins WHERE email=$1)`, email).Scan(&exists); err != nil {
		return fmt.Errorf("check admin exists: %w", err)
	}
	if exists {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO admins(email, password_hash, name, status) VALUES($1, $2, 'admin', 'active')`,
		email, string(hash)); err != nil {
		return fmt.Errorf("insert admin: %w", err)
	}
	return nil
}
