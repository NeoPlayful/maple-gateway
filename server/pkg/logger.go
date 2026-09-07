package pkg

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var log *zap.Logger

// InitLogger 初始化全局 logger。pretty=true 时输出人类可读格式。
func InitLogger(level string, pretty bool) error {
	lvl := levelToZap(level)

	var cfg zap.Config
	if pretty {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	cfg.OutputPaths = []string{"stdout"}

	l, err := cfg.Build()
	if err != nil {
		return err
	}
	log = l
	zap.ReplaceGlobals(l)
	return nil
}

func levelToZap(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// Log 返回全局 logger；未初始化时回退到 stdout 的 production logger。
func Log() *zap.Logger {
	if log == nil {
		cfg := zap.NewProductionConfig()
		cfg.OutputPaths = []string{"stdout"}
		log, _ = cfg.Build()
	}
	return log
}

// SyncLogger 在进程退出前调用，刷新缓冲。
func SyncLogger() {
	if log != nil {
		_ = log.Sync()
	}
}
