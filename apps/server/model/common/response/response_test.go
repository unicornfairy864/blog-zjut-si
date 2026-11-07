package response

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
)

// newTestContext 造一个带请求 id 的 gin 上下文与它的响应记录器。
func newTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/media", nil)
	ctx.Header("X-Request-Id", "rid-for-test")
	return ctx, recorder
}

// decodeBody 把响应体解成信封。
func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) Body {
	t.Helper()

	var body Body
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

// 业务错误照常按 code 出去，且不写日志（1000/1002/… 是预期内的结果）。
func TestFailWithErrorKeepsBusinessCode(t *testing.T) {
	ctx, recorder := newTestContext(t)

	var logged bytes.Buffer
	global.Log = slog.New(slog.NewTextHandler(&logged, nil))

	FailWithError(ctx, errs.WithDetails(errs.ErrorValidation, "参数校验失败",
		[]request.FieldError{{Field: "seriesId", Message: "系列不存在"}}))

	assert.Equal(t, http.StatusOK, recorder.Code, "业务错误也必须是 HTTP 200（§5）")
	body := decodeBody(t, recorder)
	assert.Equal(t, errs.ErrorValidation, body.Code)
	assert.Equal(t, "参数校验失败", body.Msg)
	assert.Empty(t, logged.String(), "预期内的业务错误不该刷日志")
}

// 没预料到的错误统一 1006，并留下带原因、请求 id 与路径的日志。
//
// 这条断言是排障能力的契约：线上只看到一个 1006 信封时，日志是唯一线索
// （D19 上传失败就卡在这里——之前这一层什么都不记）。
func TestFailWithErrorLogsUnexpectedCause(t *testing.T) {
	ctx, recorder := newTestContext(t)

	var logged bytes.Buffer
	global.Log = slog.New(slog.NewTextHandler(&logged, nil))

	FailWithError(ctx, fmt.Errorf("保存原图失败: %w", errors.New("permission denied")))

	assert.Equal(t, http.StatusOK, recorder.Code)
	body := decodeBody(t, recorder)
	assert.Equal(t, errs.ErrorInternal, body.Code)
	assert.Equal(t, "服务器内部错误", body.Msg)

	entry := logged.String()
	assert.Contains(t, entry, "rid-for-test", "日志要能对上响应头里的请求 id")
	assert.Contains(t, entry, "/api/admin/media")
	assert.Contains(t, entry, "保存原图失败")
	assert.Contains(t, entry, "permission denied", "根因必须进日志，否则排障只能靠猜")
}

// 日志句柄没初始化（部分测试与工具路径）时不能 panic：宁可少一条日志。
func TestFailWithErrorWithoutLogger(t *testing.T) {
	ctx, recorder := newTestContext(t)

	previous := global.Log
	global.Log = nil
	t.Cleanup(func() { global.Log = previous })

	FailWithError(ctx, errors.New("boom"))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, errs.ErrorInternal, decodeBody(t, recorder).Code)
}
