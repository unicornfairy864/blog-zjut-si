package utils

import (
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	// MinPasswordLength 是管理员口令最小长度（agent.md §9）。
	MinPasswordLength = 16

	argonMemory      = 64 * 1024 // 64 MiB，峰值在 4G 容器与 GOMEMLIMIT 之内
	argonIterations  = 3
	argonParallelism = 2
	argonSaltLength  = 16
	argonKeyLength   = 32
)

// HashPassword 用 argon2id 生成编码串：$argon2id$v=19$m=...,t=...,p=...$salt$hash。
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成口令盐失败: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword 以常量时间比较口令与编码串；编码串非法时返回 false。
func VerifyPassword(encoded, password string) bool {
	parts := splitEncoded(encoded)
	if parts == nil {
		return false
	}
	got := argon2.IDKey([]byte(password), parts.salt, parts.iterations, parts.memory, parts.parallelism, uint32(len(parts.key)))
	return constantTimeEqual(got, parts.key)
}
