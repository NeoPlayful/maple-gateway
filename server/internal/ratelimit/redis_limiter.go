package ratelimit

import (
	"context"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
)

// RedisLimiter 用 Redis INCR+EXPIRE 实现固定窗口限流，跨 Gateway 实例共享配额。
// 内存实现（Limiter）为单机降级路径；二者都实现 Backend 接口。
//
// 窗口键 = 固定时间戳桶（now/winSec * winSec），窗口推进自然换 key + EXPIRE 兜底清理，
// 避免每请求 Lua/事务；容忍时钟在秒级一致（同库多实例时钟通常一致）。
type RedisLimiter struct {
	redis *pkg.Redis
}

var _ Backend = (*RedisLimiter)(nil)

// NewRedisLimiter 构造。redis 为空时返回 nil（不可用）。
func NewRedisLimiter(redis *pkg.Redis) *RedisLimiter {
	if redis == nil {
		return nil
	}
	return &RedisLimiter{redis: redis}
}

// Allow 判定是否放行。Redis 异常时 fail-open（放行）——限流是辅助组件，
// 不得阻塞数据平面主链路（对齐"Redis 断连降级不中断服务"原则）。
func (r *RedisLimiter) Allow(key string, limit, windowSec, burst int, now time.Time) Allowance {
	if limit <= 0 {
		return Allowance{Allowed: false, Limit: limit, Window: windowSec}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	bucketKey := fixedWindowKey(key, windowSec, now)
	pipe := r.redis.Client.Pipeline()
	incr := pipe.Incr(ctx, bucketKey)
	pipe.Expire(ctx, bucketKey, time.Duration(windowSec)*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		// Redis 不可用 → 放行（fail-open）。
		return Allowance{Allowed: true, Limit: limit, Window: windowSec, Remain: limit}
	}
	count := incr.Val()
	// burst 突发：瞬间可到 burst；硬上限取 burst（>=limit）。
	hard := burst
	if hard <= 0 || hard < limit {
		hard = limit
	}
	if count > int64(hard) {
		ttl, _ := r.redis.Client.TTL(ctx, bucketKey).Result()
		retry := 1
		if ttl > 0 {
			retry = int((ttl + time.Second - 1) / time.Second)
		}
		return Allowance{Allowed: false, Limit: limit, Window: windowSec, Remain: 0, Retry: retry}
	}
	remain := hard - int(count)
	if remain < 0 {
		remain = 0
	}
	return Allowance{Allowed: true, Limit: limit, Window: windowSec, Remain: remain}
}

// fixedWindowKey 生成固定窗口 Redis 键：当前窗口起点秒作为桶。
func fixedWindowKey(key string, windowSec int, now time.Time) string {
	bucket := now.Unix() / int64(windowSec)
	return "maple:rl:" + key + ":" + itoa64(bucket)
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [24]byte
	pos := len(b)
	for v > 0 {
		pos--
		b[pos] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
