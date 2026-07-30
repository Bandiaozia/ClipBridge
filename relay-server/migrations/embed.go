package migrations

import "embed"

// Files 将人工审阅过的 SQL 迁移嵌入二进制，避免生产启动依赖当前工作目录。
//
//go:embed *.sql
var Files embed.FS
