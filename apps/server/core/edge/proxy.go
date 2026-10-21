// Package edge 实现两条 listener 的边缘裁决：哪些路径本地处理、哪些转 Next、哪些必须 404。
package edge

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
)

// Proxy 把未命中的页面请求转发给 Next 进程。
type Proxy struct {
	upstream *url.URL
	handler  *httputil.ReverseProxy
}

// NewProxy 构造到 Next 的反向代理。
func NewProxy(upstream string, logger *slog.Logger) (*Proxy, error) {
	target, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}

	p := &Proxy{upstream: target}
	p.handler = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
			// 保留原始 Host，Next 生成绝对 URL 时才不会指向 127.0.0.1。
			pr.Out.Host = pr.In.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Error("转发到 Next 失败", "path", r.URL.Path, "err", err)
			// 连接已经建立（请求到了 Go），所以按契约回 HTTP 200 + code 1006：
			// 状态只存在信封里，基础设施故障也走同一个口径（agent.md §5）。
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":1006,"data":null,"msg":"页面服务暂不可用"}`))
		},
	}
	return p, nil
}

// Serve 在 gin 上下文里执行转发。
func (p *Proxy) Serve(c *gin.Context) {
	p.handler.ServeHTTP(c.Writer, c.Request)
}
