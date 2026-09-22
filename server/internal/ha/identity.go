package ha

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// instanceIDNamespace 是主机名派生与状态文件 UUID 生成共用的命名空间。
var instanceIDNamespace = uuid.MustParse("6f6b1c1e-6d1a-4a6a-9e6b-7c2f9d0a1b3c")

// ResolveID 解析本进程的网关实例 ID，按优先级取第一个可用来源：
//
//  1. configured 非空（ha.instance_id / MAPLE_HA_INSTANCE_ID）：直接用，最稳定；
//  2. 状态文件：存在则读回复用（跨重启稳定），不存在则生成一个 12 位 ID 并原子落盘，
//     下次重启即复用同一 ID。路径空则不启用文件持久化；
//  3. 主机名派生：以 hostname 做 UUIDv5 再取短 ID，同一主机名重启保持不变。
//
// 状态文件不可写时降级到主机名派生并告警，不阻断启动。
func ResolveID(configured, statePath string, logger *zap.Logger) string {
	if id := strings.TrimSpace(configured); id != "" {
		return id
	}
	if statePath != "" {
		if id, err := loadOrCreateStateFile(statePath, logger); err == nil {
			return id
		} else if logger != nil {
			logger.Warn("instance state file unavailable, falling back to hostname-derived id",
				zap.String("path", statePath), zap.String("err", err.Error()))
		}
	}
	host, _ := os.Hostname()
	return ShortIDFromSeed(host)
}

// ShortIDFromSeed 以种子（典型为主机名）派生可复现的 12 位实例 ID。
// 主机名为空时退回随机 UUID，避免所有无主机名环境拿到同一 ID。
func ShortIDFromSeed(seed string) string {
	if strings.TrimSpace(seed) == "" {
		return pkg.ShortID(uuid.NewString())
	}
	return pkg.ShortID(uuid.NewSHA1(instanceIDNamespace, []byte(seed)).String())
}

// loadOrCreateStateFile 读取状态文件里的实例 ID；文件不存在则生成并落盘。
func loadOrCreateStateFile(path string, logger *zap.Logger) (string, error) {
	switch raw, err := os.ReadFile(path); {
	case err == nil:
		if id := strings.TrimSpace(string(raw)); id != "" {
			return id, nil
		}
		// 文件存在但为空/损坏：覆盖为新的派生 ID。
	case !os.IsNotExist(err):
		return "", fmt.Errorf("read instance state file: %w", err)
	}

	host, _ := os.Hostname()
	id := ShortIDFromSeed(host)
	if err := writeStateFile(path, id); err != nil {
		return "", err
	}
	if logger != nil {
		logger.Info("instance id persisted to state file",
			zap.String("path", path), zap.String("instance_id", id))
	}
	return id, nil
}

// writeStateFile 原子写出实例 ID：临时文件 + rename，权限收紧到 0600。
func writeStateFile(path, id string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create instance state dir: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(id), 0o600); err != nil {
		return fmt.Errorf("write instance state file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace instance state file: %w", err)
	}
	_ = os.Chmod(path, 0o600)
	return nil
}
