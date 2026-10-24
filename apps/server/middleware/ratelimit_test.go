package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// newRateLimitProbe 造一个只挂限流的引擎，返回"请求一次"的函数。
func newRateLimitProbe(burst int) func(remoteAddr string, headers map[string]string) string {
	gin.SetMode(gin.TestMode)
	limiter := utils.NewKeyedLimiter(time.Hour, burst) // 长间隔：只考突发额度，不考补充
	engine := gin.New()
	engine.Use(RateLimit(limiter, nil))
	engine.GET("/x", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	return func(remoteAddr string, headers map[string]string) string {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/x", nil)
		request.RemoteAddr = remoteAddr
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		engine.ServeHTTP(recorder, request)
		return recorder.Body.String()
	}
}

// TestRateLimit_InternalCallerIsExempt 锁住 D19 修掉的缺陷：
// 同容器里的 Next SSR 抓取与访客共用回环桶时，站点访问量一上来就会把自己的
// 页面渲染打成 code 1005（前端抛错 → 整页 500）。
func TestRateLimit_InternalCallerIsExempt(t *testing.T) {
	probe := newRateLimitProbe(3)

	for i := 0; i < 20; i++ {
		body := probe("127.0.0.1:54321", nil)
		require.Equal(t, "ok", body, "第 %d 次内部调用不该被限流", i+1)
	}
}

func TestRateLimit_ExternalCallerIsLimited(t *testing.T) {
	probe := newRateLimitProbe(3)

	// 经 nginx 进来的访客：来源是 docker 网关 + 带转发头，按转发出来的访客 IP 计数。
	visitor := map[string]string{"X-Forwarded-For": "203.0.113.9"}
	for i := 0; i < 3; i++ {
		require.Equal(t, "ok", probe("172.17.0.1:40000", visitor), "第 %d 次应在额度内", i+1)
	}
	assert.Contains(t, probe("172.17.0.1:40000", visitor), `"code":1005`)

	// 换一个访客 IP 就是另一个桶，仍然放行。
	other := map[string]string{"X-Forwarded-For": "203.0.113.10"}
	assert.Equal(t, "ok", probe("172.17.0.1:40000", other))
}

// TestRateLimit_LoopbackWithForwardingHeaderIsNotInternal 回环来源但带了转发头，
// 说明它是被代理过的访客，不能按内部调用放行。
func TestRateLimit_LoopbackWithForwardingHeaderIsNotInternal(t *testing.T) {
	probe := newRateLimitProbe(2)

	forwarded := map[string]string{"X-Forwarded-For": "198.51.100.7"}
	for i := 0; i < 2; i++ {
		require.Equal(t, "ok", probe("127.0.0.1:40000", forwarded))
	}
	assert.Contains(t, probe("127.0.0.1:40000", forwarded), `"code":1005`)

	realIP := map[string]string{"X-Real-IP": "198.51.100.8"}
	for i := 0; i < 2; i++ {
		require.Equal(t, "ok", probe("127.0.0.1:40000", realIP))
	}
	assert.Contains(t, probe("127.0.0.1:40000", realIP), `"code":1005`)
}

func TestIsInternalCaller(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       bool
	}{
		{name: "回环 IPv4", remoteAddr: "127.0.0.1:1234", want: true},
		{name: "回环 IPv6", remoteAddr: "[::1]:1234", want: true},
		{name: "回环不带端口", remoteAddr: "127.0.0.1", want: true},
		{name: "docker 网关", remoteAddr: "172.17.0.1:1234", want: false},
		{name: "公网来源", remoteAddr: "203.0.113.9:1234", want: false},
		{name: "空 RemoteAddr", remoteAddr: "", want: false},
		{name: "无法解析", remoteAddr: "not-an-ip:1234", want: false},
		{
			name:       "回环但带 X-Forwarded-For",
			remoteAddr: "127.0.0.1:1234",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.9"},
			want:       false,
		},
		{
			name:       "回环但带 X-Real-IP",
			remoteAddr: "127.0.0.1:1234",
			headers:    map[string]string{"X-Real-IP": "203.0.113.9"},
			want:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
			c.Request.RemoteAddr = tc.remoteAddr
			for key, value := range tc.headers {
				c.Request.Header.Set(key, value)
			}

			assert.Equal(t, tc.want, IsInternalCaller(c))
		})
	}
}

// TestNewPublicAPILimiter_MatchesContract 公开 API 的额度是 20 r/s（§9）。
func TestNewPublicAPILimiter_MatchesContract(t *testing.T) {
	limiter := NewPublicAPILimiter()

	allowed := 0
	for i := 0; i < 40; i++ {
		if limiter.Allow("203.0.113.9") {
			allowed++
		}
	}
	// 桶容量 20、补充间隔 50ms，一次紧密循环里能拿到的就是 20 上下（不校验补充）。
	assert.GreaterOrEqual(t, allowed, 20)
	assert.Less(t, allowed, 40, "必须真的限流，而不是永远放行")
}
