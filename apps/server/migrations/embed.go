// Package migrations 以只读文件系统形式内嵌全部版本化 SQL。
//
// 迁移脚本随二进制发布，容器内无需额外挂载；版本号与文件名遵循
// golang-migrate 的 {version}_{name}.{up|down}.sql 约定。
package migrations

import "embed"

// FS 保存 migrations 目录下的全部 .sql 文件。
//
//go:embed *.sql
var FS embed.FS
