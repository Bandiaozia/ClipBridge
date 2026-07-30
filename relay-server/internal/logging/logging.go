package logging

import (
	"log/slog"
	"os"
)

func New(level string) *slog.Logger {
	var configured slog.Level
	switch level {
	case "debug":
		configured = slog.LevelDebug
	case "warn":
		configured = slog.LevelWarn
	case "error":
		configured = slog.LevelError
	default:
		configured = slog.LevelInfo
	}
	// JSON 日志只记录标识和结果。调用方不得传密码、Token、私钥、nonce、密文或正文。
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: configured}))
}
