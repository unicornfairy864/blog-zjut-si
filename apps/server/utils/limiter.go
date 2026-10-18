package utils

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// KeyedLimiter 是按 key（用户名、来源 IP）计数的进程内令牌桶。
//
// 单实例部署，不需要 Redis（C2/C6）。桶空闲 30 分钟后回收，避免 map 无界增长。
type KeyedLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	limit     rate.Limit
	burst     int
	ttl       time.Duration
	lastSweep time.Time
}

type bucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

// NewKeyedLimiter 构造限流器：每 every 补 1 个令牌，最多堆积 burst 个。
func NewKeyedLimiter(every time.Duration, burst int) *KeyedLimiter {
	if every <= 0 {
		every = time.Second
	}
	if burst < 1 {
		burst = 1
	}
	return &KeyedLimiter{
		buckets:   make(map[string]*bucket),
		limit:     rate.Every(every),
		burst:     burst,
		ttl:       30 * time.Minute,
		lastSweep: time.Now(),
	}
}

// Allow 报告该 key 当前是否还有配额。
func (l *KeyedLimiter) Allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	return b.limiter.Allow()
}

// sweepLocked 回收长时间空闲的桶；调用方必须持锁。
func (l *KeyedLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.seen) > l.ttl {
			delete(l.buckets, k)
		}
	}
}
