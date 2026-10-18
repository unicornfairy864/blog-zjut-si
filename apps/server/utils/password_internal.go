package utils

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

var b64 = base64.RawStdEncoding

// argonParams 是从编码串里解析出来的 argon2id 参数。
type argonParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	key         []byte
}

// splitEncoded 解析 $argon2id$v=19$m=...,t=...,p=...$salt$hash 格式。
func splitEncoded(encoded string) *argonParams {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return nil
	}

	var (
		memory      uint32
		iterations  uint32
		parallelism uint8
	)
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return nil
	}
	if memory == 0 || iterations == 0 || parallelism == 0 {
		return nil
	}

	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return nil
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return nil
	}
	return &argonParams{memory: memory, iterations: iterations, parallelism: parallelism, salt: salt, key: key}
}

// constantTimeEqual 常量时间比较两段字节。
func constantTimeEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
