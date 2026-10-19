package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync/atomic"
)

// pepper 是 visitor_hash / ip_hash 的加盐密钥，启动时从 settings.visitor_salt 注入。
var pepper atomic.Value

// SetPepper 注入加盐密钥；必须在开始服务前调用。
func SetPepper(s string) {
	pepper.Store(s)
}

func pepperValue() string {
	v, _ := pepper.Load().(string)
	return v
}

// SHA256Hex 返回 s 的十六进制 SHA-256。
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SHA256HexBytes 返回 b 的十六进制 SHA-256。
func SHA256HexBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// HashIP 对来源 IP 加盐哈希；审计与限流只落哈希（agent.md §9）。
func HashIP(ip string) string {
	mac := hmac.New(sha256.New, []byte(pepperValue()))
	mac.Write([]byte("ip|" + ip))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// HashEmail 对邮箱加盐哈希；先做小写与去空白归一，评论去重与审计只落哈希（agent.md §9）。
func HashEmail(email string) string {
	mac := hmac.New(sha256.New, []byte(pepperValue()))
	mac.Write([]byte("email|" + strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// HashVisitor 对访客标识加盐哈希（PV/UV 去重）。
func HashVisitor(seed string) string {
	mac := hmac.New(sha256.New, []byte(pepperValue()))
	mac.Write([]byte("visitor|" + seed))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// CSRFToken 由会话令牌哈希派生，服务端无需保存明文即可复算校验。
func CSRFToken(tokenHash string) string {
	mac := hmac.New(sha256.New, []byte(pepperValue()))
	mac.Write([]byte("csrf|" + tokenHash))
	return hex.EncodeToString(mac.Sum(nil))
}
