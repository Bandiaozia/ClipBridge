package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddress      string
	DatabasePath       string
	JWTSecret          string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	PairingTokenTTL    time.Duration
	MaxBodyBytes       int64
	MaxCiphertextBytes int
	MaxQueuedMessages  int
	MaxQueuedBytes     int64
	AllowedOrigin      string
	LogLevel           string
	CleanupInterval    time.Duration
	RateLimitPerMinute int
	TrustProxy         bool
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddress:      env("CLIPBRIDGE_LISTEN", ":8080"),
		DatabasePath:       env("CLIPBRIDGE_DATABASE_PATH", "./data/clipbridge.db"),
		JWTSecret:          os.Getenv("CLIPBRIDGE_JWT_SECRET"),
		AccessTokenTTL:     durationEnv("CLIPBRIDGE_ACCESS_TTL", 15*time.Minute),
		RefreshTokenTTL:    durationEnv("CLIPBRIDGE_REFRESH_TTL", 30*24*time.Hour),
		PairingTokenTTL:    durationEnv("CLIPBRIDGE_PAIRING_TTL", 5*time.Minute),
		MaxBodyBytes:       int64Env("CLIPBRIDGE_MAX_BODY_BYTES", 64*1024),
		MaxCiphertextBytes: intEnv("CLIPBRIDGE_MAX_CIPHERTEXT_BYTES", 1024*1024),
		MaxQueuedMessages:  intEnv("CLIPBRIDGE_MAX_QUEUED_MESSAGES", 1000),
		MaxQueuedBytes:     int64Env("CLIPBRIDGE_MAX_QUEUED_BYTES", 10*1024*1024),
		AllowedOrigin:      env("CLIPBRIDGE_ALLOWED_ORIGIN", ""),
		LogLevel:           env("CLIPBRIDGE_LOG_LEVEL", "info"),
		CleanupInterval:    durationEnv("CLIPBRIDGE_CLEANUP_INTERVAL", time.Minute),
		RateLimitPerMinute: intEnv("CLIPBRIDGE_RATE_LIMIT_PER_MINUTE", 120),
		TrustProxy:         env("CLIPBRIDGE_TRUST_PROXY", "false") == "true",
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("CLIPBRIDGE_JWT_SECRET 必须至少包含 32 个字符")
	}
	if cfg.MaxBodyBytes < 1024 || cfg.MaxCiphertextBytes < 1024 {
		return Config{}, errors.New("请求体或密文限制过小")
	}
	if cfg.RateLimitPerMinute < 10 {
		return Config{}, errors.New("速率限制不得低于每分钟 10 次")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func intEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func int64Env(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func (c Config) DSN() string {
	return fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL", c.DatabasePath)
}
