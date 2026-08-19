// Package migration 将版本化 SQL 迁移内嵌进二进制，使 migrate 命令无需外部文件。
package migration

import "embed"

// Files 存放 migrations/ 目录下的全部迁移 SQL 文件。
//
//go:embed migrations/*.sql
var Files embed.FS
