package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// RandomToken 生成 n 字节随机数并编码为 2n 位十六进制字符串（会话、CSRF、预览令牌）。
func RandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机令牌失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
