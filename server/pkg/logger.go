package pkg

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	log       *zap.Logger
	logAtomic *zap.AtomicLevel
)

// InitLogger 初始化全局 logger。pretty=true 时输出人类可读格式。
func InitLogger(level string, pretty bool) error {
	lvl := levelToZap(level)
	a := zap.NewAtomicLevelAt(lvl)
	logAtomic = &a

	var encoder zapcore.Encoder
	if pretty {
		encoder = zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	} else {
		encoder = zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	}
	log = zap.New(zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), logAtomic))
	zap.ReplaceGlobals(log)
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
		a := zap.NewAtomicLevelAt(zapcore.InfoLevel)
		logAtomic = &a
		encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
		log = zap.New(zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), logAtomic))
	}
	return log
}

// SetLogLevel 运行时切换全局日志输出级别（无需重启进程）。
// debug=true 打开详细日志（Debug 及以上）；false 恢复默认 Info 级别。
func SetLogLevel(debug bool) {
	if logAtomic == nil {
		return
	}
	if debug {
		logAtomic.SetLevel(zapcore.DebugLevel)
	} else {
		logAtomic.SetLevel(zapcore.InfoLevel)
	}
}

// SyncLogger 在进程退出前调用，刷新缓冲。
func SyncLogger() {
	if log != nil {
		_ = log.Sync()
	}
}
