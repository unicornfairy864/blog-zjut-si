package service

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// ---------- 夹具 ----------

// notifySink 是本地投递接收端：记录方法/路径/报文并回放指定状态码。
//
// 为什么必须本地：§13 的投递断言要能离线跑，绝不能真连外网；handler 在别的
// goroutine 上执行，所以字段用互斥锁保护（测试串行，但断言发生在请求之后）。
type notifySink struct {
	mu     sync.Mutex
	hits   int
	method string
	path   string
	ctype  string
	body   []byte
	status int
	reply  string
}

func (s *notifySink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	s.mu.Lock()
	s.hits++
	s.method = r.Method
	s.path = r.URL.Path
	s.ctype = r.Header.Get("Content-Type")
	s.body = body
	status, reply := s.status, s.reply
	s.mu.Unlock()

	if status == 0 {
		status = http.StatusOK
	}
	if reply == "" {
		reply = `{"ok":true}`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(reply))
}

func (s *notifySink) setResponse(status int, reply string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.reply = status, reply
}

func (s *notifySink) hitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

func (s *notifySink) snapshot() (hits int, method, path, ctype string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits, s.method, s.path, s.ctype, append([]byte(nil), s.body...)
}

// startNotifySink 起一个本地接收端，返回夹具与它的根地址。
func startNotifySink(t *testing.T) (*notifySink, string) {
	t.Helper()

	sink := &notifySink{}
	srv := httptest.NewServer(sink)
	t.Cleanup(srv.Close)
	return sink, srv.URL
}

// webhookConfig 造一份"已启用 + 只配 webhook"的配置。
func webhookConfig(url string) NotifyConfig {
	cfg := defaultNotifyConfig()
	cfg.Enabled = true
	cfg.Channel = NotifyChannelWebhook
	cfg.WebhookURL = url
	return cfg
}

// telegramConfig 造一份"已启用 + 只配 telegram"的配置。
func telegramConfig(token, chatID string) NotifyConfig {
	cfg := defaultNotifyConfig()
	cfg.Enabled = true
	cfg.Channel = NotifyChannelTelegram
	cfg.TelegramBotToken = token
	cfg.TelegramChatID = chatID
	return cfg
}

// mustSaveNotify 直接落库一份配置（不走 Update 校验，便于构造边界数据）。
func mustSaveNotify(t *testing.T, cfg NotifyConfig) {
	t.Helper()
	require.NoError(t, NotifyService{}.save(cfg))
}

func mustNotifyConfig(t *testing.T) NotifyConfig {
	t.Helper()

	cfg, err := NotifyService{}.Config()
	require.NoError(t, err)
	return cfg
}

func mustNotifyState(t *testing.T) notifyState {
	t.Helper()

	state, err := NotifyService{}.state()
	require.NoError(t, err)
	return state
}

// roundTripFunc 让测试改写请求目标。
//
// deliverTelegram 的地址是硬编码的 https://api.telegram.org/...（不能改被测代码），
// 所以只能替换进程级 HTTP 客户端的 Transport：既断言了真实 URL 形状，又绝不真连外网。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// recordedRequest 记录被改写前的那次出网请求。
type recordedRequest struct {
	mu   sync.Mutex
	url  string
	body []byte
}

func (r *recordedRequest) set(url string, body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.url = url
	r.body = append([]byte(nil), body...)
}

func (r *recordedRequest) get() (string, []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.url, append([]byte(nil), r.body...)
}

// redirectNotifyHTTPClient 把告警投递的请求转投到本地服务，并返回"真实请求"记录。
func redirectNotifyHTTPClient(t *testing.T, target string) *recordedRequest {
	t.Helper()

	rec := &recordedRequest{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		rec.set(req.URL.String(), raw)

		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(target, "http://")
		req.Host = req.URL.Host
		req.Body = io.NopCloser(bytes.NewReader(raw))
		req.ContentLength = int64(len(raw))
		return http.DefaultTransport.RoundTrip(req)
	})

	original := notifyHTTPClient
	notifyHTTPClient = &http.Client{Transport: transport, Timeout: notifyHTTPTimeout}
	t.Cleanup(func() { notifyHTTPClient = original })
	return rec
}

// writeSelfSignedCert 现造一张自签证书写进临时目录，返回 PEM 路径。
//
// 为什么现造：checkCert 只看 PEM 里的 NotAfter，固定夹具会随时间腐烂。
func writeSelfSignedCert(t *testing.T, notAfter time.Time) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "notify-test.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		DNSNames:     []string{"notify-test.local"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "fullchain.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return path
}

func notifyPtrBool(v bool) *bool    { return &v }
func notifyPtrStr(v string) *string { return &v }
func notifyPtrInt(v int) *int       { return &v }

// ---------- 配置读写 ----------

func TestNotifyService_ConfigDefaultsWithoutRow(t *testing.T) {
	setupEnv(t)

	// 键缺失时退回默认值：关着、事件全勾、阈值按 §13/§14。
	cfg, err := NotifyService{}.Config()
	require.NoError(t, err)
	assert.Equal(t, defaultNotifyConfig(), cfg)
	assert.False(t, cfg.Enabled)
	assert.Equal(t, NotifyChannelWebhook, cfg.Channel)
	assert.True(t, cfg.Events.JobFailed)
	assert.True(t, cfg.Events.DiskHigh)
	assert.True(t, cfg.Events.CertExpiring)
	assert.Equal(t, notifyDefaultDiskPercent, cfg.DiskThresholdPercent)
	assert.Equal(t, notifyDefaultCertDays, cfg.CertExpireDays)
}

func TestNotifyService_ConfigRejectsDirtyJSON(t *testing.T) {
	setupEnv(t)

	mustSaveNotify(t, defaultNotifyConfig())
	putRawSetting(t, model.SettingNotify, "{这不是 JSON")

	// 脏数据要报错，而不是静默当成"没配"。
	cfg, err := NotifyService{}.Config()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "解析告警配置失败")
	assert.Equal(t, defaultNotifyConfig(), cfg, "解析失败时返回默认值")
}

func TestNotifyService_ConfigNormalizesDirtyValues(t *testing.T) {
	setupEnv(t)

	mustSaveNotify(t, defaultNotifyConfig())
	putRawSetting(t, model.SettingNotify, `{
		"enabled": true,
		"channel": "  TELEGRAM  ",
		"webhookUrl": "  https://example.test/hook  ",
		"telegramBotToken": "  bot-token  ",
		"telegramChatId": "  -100200  ",
		"diskThresholdPercent": 0,
		"certExpireDays": 99999,
		"events": {"jobFailed": false, "diskHigh": true, "certExpiring": false}
	}`)

	cfg := mustNotifyConfig(t)
	assert.Equal(t, NotifyChannelTelegram, cfg.Channel, "渠道要去空白并转小写")
	assert.Equal(t, "https://example.test/hook", cfg.WebhookURL)
	assert.Equal(t, "bot-token", cfg.TelegramBotToken)
	assert.Equal(t, "-100200", cfg.TelegramChatID)
	assert.Equal(t, notifyDefaultDiskPercent, cfg.DiskThresholdPercent, "越界阈值回落到默认值")
	assert.Equal(t, notifyDefaultCertDays, cfg.CertExpireDays, "越界阈值回落到默认值")
	assert.False(t, cfg.Events.JobFailed)
	assert.True(t, cfg.Events.DiskHigh)

	// 空渠道也要回落成 webhook。
	putRawSetting(t, model.SettingNotify, `{"channel":"","diskThresholdPercent":99,"certExpireDays":1}`)
	cfg = mustNotifyConfig(t)
	assert.Equal(t, NotifyChannelWebhook, cfg.Channel)
	assert.Equal(t, 99, cfg.DiskThresholdPercent, "边界内的值要保留")
	assert.Equal(t, 1, cfg.CertExpireDays)
}

func TestNotifyService_ViewMasksSecrets(t *testing.T) {
	setupEnv(t)

	const webhookSecret = "https://hooks.example.test/T00/B00/s3cr3t-hook-id"
	const botToken = "123456:AA-s3cr3t-bot-token"

	mustSaveNotify(t, NotifyConfig{
		Enabled:              true,
		Channel:              NotifyChannelWebhook,
		WebhookURL:           webhookSecret,
		TelegramBotToken:     botToken,
		TelegramChatID:       "-100200",
		Events:               NotifyEvents{JobFailed: true, DiskHigh: false, CertExpiring: true},
		DiskThresholdPercent: 90,
		CertExpireDays:       7,
	})

	view, err := NotifyService{}.View()
	require.NoError(t, err)

	// 密钥只回"配没配"，原文永不出现在回显里。
	assert.True(t, view.WebhookURLSet)
	assert.True(t, view.TelegramBotTokenSet)
	assert.Equal(t, "-100200", view.TelegramChatID, "chatId 不是密钥，按契约回显")
	assert.Equal(t, 90, view.DiskThresholdPercent)
	assert.Equal(t, 7, view.CertExpireDays)
	assert.Equal(t, global.Config.TLS.CertFile, view.CertFile)

	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "s3cr3t-hook-id", "webhook 地址不能回显")
	assert.NotContains(t, string(encoded), "s3cr3t-bot-token", "bot token 不能回显")

	// 没配的通道回 false。
	mustSaveNotify(t, defaultNotifyConfig())
	view, err = NotifyService{}.View()
	require.NoError(t, err)
	assert.False(t, view.WebhookURLSet)
	assert.False(t, view.TelegramBotTokenSet)
	assert.Empty(t, view.LastAlerts)
}

func TestNotifyService_ViewLastAlertsOrderAndFiltering(t *testing.T) {
	setupEnv(t)

	mustSaveNotify(t, defaultNotifyConfig())
	require.NoError(t, NotifyService{}.saveState(notifyState{
		NotifyEventCertExpiring: {At: 3, OK: false},
		NotifyEventJobFailed:    {At: 1, OK: true},
		NotifyEventDiskHigh:     {At: 2, OK: true},
		"unknown_event":         {At: 9, OK: true},
	}))

	view, err := NotifyService{}.View()
	require.NoError(t, err)

	// 顺序固定（job_failed → disk_high → cert_expiring），未知事件不回显。
	require.Len(t, view.LastAlerts, 3)
	assert.Equal(t, NotifyEventJobFailed, view.LastAlerts[0].Event)
	assert.Equal(t, int64(1), view.LastAlerts[0].At)
	assert.True(t, view.LastAlerts[0].OK)
	assert.Equal(t, NotifyEventDiskHigh, view.LastAlerts[1].Event)
	assert.Equal(t, NotifyEventCertExpiring, view.LastAlerts[2].Event)
	assert.False(t, view.LastAlerts[2].OK)
}

func TestNotifyService_ViewReportsBrokenState(t *testing.T) {
	setupEnv(t)

	mustSaveNotify(t, defaultNotifyConfig())
	require.NoError(t, NotifyService{}.saveState(notifyState{}))
	putRawSetting(t, model.SettingNotifyState, "not-json")

	_, err := NotifyService{}.View()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "解析告警状态失败")
}

func TestNotifyService_UpdateMergeSemantics(t *testing.T) {
	setupEnv(t)

	base := webhookConfig("https://example.test/hook")
	base.TelegramBotToken = "keep-token"
	base.TelegramChatID = "-100200"
	mustSaveNotify(t, base)

	// nil = 保持原值：整份 Update 都不该改动任何字段。
	view, err := NotifyService{}.Update(NotifyUpdate{})
	require.NoError(t, err)
	after := mustNotifyConfig(t)
	assert.Equal(t, base, after)
	assert.True(t, view.WebhookURLSet)

	// "" = 清空（契约）。
	_, err = NotifyService{}.Update(NotifyUpdate{TelegramChatID: notifyPtrStr("")})
	require.NoError(t, err)
	after = mustNotifyConfig(t)
	assert.Empty(t, after.TelegramChatID)
	assert.Equal(t, "keep-token", after.TelegramBotToken, "只清被显式传空串的那一项")

	_, err = NotifyService{}.Update(NotifyUpdate{TelegramBotToken: notifyPtrStr("   ")})
	require.NoError(t, err)
	assert.Empty(t, mustNotifyConfig(t).TelegramBotToken, "只有空白也等于清空")

	// 启用状态下 webhook 渠道必须填地址：只清地址会被校验拦下（契约）。
	_, err = NotifyService{}.Update(NotifyUpdate{WebhookURL: notifyPtrStr("")})
	assert.Equal(t, errs.ErrorValidation, appErrorAny(t, err).Code)

	_, err = NotifyService{}.Update(NotifyUpdate{Enabled: notifyPtrBool(false), WebhookURL: notifyPtrStr("   ")})
	require.NoError(t, err)
	assert.Empty(t, mustNotifyConfig(t).WebhookURL)

	// 渠道名归一：去空白 + 转小写。
	_, err = NotifyService{}.Update(NotifyUpdate{Channel: notifyPtrStr("  TELEGRAM  ")})
	require.NoError(t, err)
	assert.Equal(t, NotifyChannelTelegram, mustNotifyConfig(t).Channel)

	// Events 是整体替换（指针指向的结构体原样覆盖）；顺带把 telegram 配齐再启用。
	_, err = NotifyService{}.Update(NotifyUpdate{
		Enabled:          notifyPtrBool(true),
		Channel:          notifyPtrStr(NotifyChannelTelegram),
		TelegramBotToken: notifyPtrStr("bot-token"),
		TelegramChatID:   notifyPtrStr("-100200"),
		Events:           &NotifyEvents{JobFailed: true},
	})
	require.NoError(t, err)
	after = mustNotifyConfig(t)
	assert.True(t, after.Enabled)
	assert.True(t, after.Events.JobFailed)
	assert.False(t, after.Events.DiskHigh, "Events 是整体替换，未勾选的项会被关掉")
	assert.False(t, after.Events.CertExpiring)

	// 阈值可写。
	_, err = NotifyService{}.Update(NotifyUpdate{DiskThresholdPercent: notifyPtrInt(70), CertExpireDays: notifyPtrInt(30)})
	require.NoError(t, err)
	after = mustNotifyConfig(t)
	assert.Equal(t, 70, after.DiskThresholdPercent)
	assert.Equal(t, 30, after.CertExpireDays)
}

func TestNotifyService_UpdateValidation(t *testing.T) {
	setupEnv(t)

	longURL := "https://example.test/" + strings.Repeat("a", notifyURLMaxLen)

	cases := []struct {
		name        string
		update      NotifyUpdate
		wantField   string
		wantMessage string
	}{
		{
			name:        "渠道非法",
			update:      NotifyUpdate{Channel: notifyPtrStr("email")},
			wantField:   "channel",
			wantMessage: "只能是 webhook 或 telegram",
		},
		{
			name:        "webhook 不是 http 地址",
			update:      NotifyUpdate{WebhookURL: notifyPtrStr("ftp://example.test/hook")},
			wantField:   "webhookUrl",
			wantMessage: "必须是 http:// 或 https:// 开头的地址",
		},
		{
			name:        "webhook 超长",
			update:      NotifyUpdate{WebhookURL: notifyPtrStr(longURL)},
			wantField:   "webhookUrl",
			wantMessage: "长度不能超过 500 个字符",
		},
		{
			name:        "磁盘阈值下界越界",
			update:      NotifyUpdate{DiskThresholdPercent: notifyPtrInt(0)},
			wantField:   "diskThresholdPercent",
			wantMessage: "必须在 1–99 之间",
		},
		{
			name:        "磁盘阈值上界越界",
			update:      NotifyUpdate{DiskThresholdPercent: notifyPtrInt(100)},
			wantField:   "diskThresholdPercent",
			wantMessage: "必须在 1–99 之间",
		},
		{
			name:        "证书阈值越界",
			update:      NotifyUpdate{CertExpireDays: notifyPtrInt(notifyCertDaysMax + 1)},
			wantField:   "certExpireDays",
			wantMessage: "必须在 1–3650 之间",
		},
		{
			name:        "启用但没填 webhook",
			update:      NotifyUpdate{Enabled: notifyPtrBool(true)},
			wantField:   "webhookUrl",
			wantMessage: "启用告警前必须填 webhook 地址",
		},
		{
			name:        "启用 telegram 但没填 token",
			update:      NotifyUpdate{Enabled: notifyPtrBool(true), Channel: notifyPtrStr(NotifyChannelTelegram)},
			wantField:   "telegramBotToken",
			wantMessage: "启用告警前必须填 bot token",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := mustNotifyConfig(t)

			_, err := NotifyService{}.Update(tc.update)
			appErr := appErrorAny(t, err)
			assert.Equal(t, errs.ErrorValidation, appErr.Code)

			// 字段错误的 data 形状是 []request.FieldError（§5）。
			fields, ok := appErr.Details.([]request.FieldError)
			require.True(t, ok, "Details 应是 []request.FieldError，实际 %T", appErr.Details)

			var hit bool
			for _, f := range fields {
				if f.Field == tc.wantField {
					hit = true
					assert.Contains(t, f.Message, tc.wantMessage)
				}
			}
			assert.True(t, hit, "期望字段 %s 报错，实际 %+v", tc.wantField, fields)

			// 校验失败必须整笔不落库。
			assert.Equal(t, before, mustNotifyConfig(t), "校验失败不该写入配置")
		})
	}
}

func TestNotifyService_SaveAndStateRoundTrip(t *testing.T) {
	setupEnv(t)

	// state 无行 → 空表，不报错。
	assert.Empty(t, mustNotifyState(t))

	// saveState → state 往返。
	require.NoError(t, NotifyService{}.saveState(notifyState{
		NotifyEventDiskHigh: {At: 123, OK: true},
	}))
	state := mustNotifyState(t)
	require.Contains(t, state, NotifyEventDiskHigh)
	assert.Equal(t, int64(123), state[NotifyEventDiskHigh].At)
	assert.True(t, state[NotifyEventDiskHigh].OK)

	// 脏 JSON → 报错。
	putRawSetting(t, model.SettingNotifyState, "{{{")
	_, err := NotifyService{}.state()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "解析告警状态失败")

	// "{}" → 空表而不是 nil（dispatch 会往里写，nil map 会 panic）。
	putRawSetting(t, model.SettingNotifyState, "{}")
	assert.Empty(t, mustNotifyState(t))

	// save → Config 往返。
	cfg := webhookConfig("https://example.test/round-trip")
	mustSaveNotify(t, cfg)
	assert.Equal(t, cfg, mustNotifyConfig(t))
}

// ---------- 纯函数 ----------

func TestNotifyConfig_ChannelConfiguredAndEventEnabled(t *testing.T) {
	cases := []struct {
		name           string
		cfg            NotifyConfig
		wantConfigured bool
	}{
		{"只配 webhook", NotifyConfig{Channel: NotifyChannelWebhook, WebhookURL: "https://x.test/h"}, true},
		{"webhook 空地址", NotifyConfig{Channel: NotifyChannelWebhook}, false},
		{"只配 telegram", NotifyConfig{Channel: NotifyChannelTelegram, TelegramBotToken: "t", TelegramChatID: "c"}, true},
		{"telegram 缺 token", NotifyConfig{Channel: NotifyChannelTelegram, TelegramChatID: "c"}, false},
		{"telegram 缺 chatId", NotifyConfig{Channel: NotifyChannelTelegram, TelegramBotToken: "t"}, false},
		{"都没配", NotifyConfig{}, false},
		{"都没配（渠道 telegram）", NotifyConfig{Channel: NotifyChannelTelegram}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantConfigured, tc.cfg.channelConfigured())
		})
	}

	all := NotifyEvents{JobFailed: true, DiskHigh: false, CertExpiring: true}
	cfg := NotifyConfig{Events: all}
	assert.True(t, cfg.eventEnabled(NotifyEventJobFailed))
	assert.False(t, cfg.eventEnabled(NotifyEventDiskHigh))
	assert.True(t, cfg.eventEnabled(NotifyEventCertExpiring))
	assert.True(t, cfg.eventEnabled(NotifyEventTest), "未知事件（自检）一律放行")
	assert.True(t, cfg.eventEnabled("something-else"))
}

func TestNotifyShouldSendWindow(t *testing.T) {
	now := time.Now()
	state := notifyState{}

	// 没记录 → 立刻发。
	assert.True(t, notifyShouldSend(state, NotifyEventJobFailed, now))

	// 发成功 → 24h 静默。
	state[NotifyEventJobFailed] = notifyStateEntry{At: now.Add(-time.Hour).Unix(), OK: true}
	assert.False(t, notifyShouldSend(state, NotifyEventJobFailed, now))
	state[NotifyEventJobFailed] = notifyStateEntry{At: now.Add(-25 * time.Hour).Unix(), OK: true}
	assert.True(t, notifyShouldSend(state, NotifyEventJobFailed, now))

	// 发失败 → 15min 后重试。
	state[NotifyEventJobFailed] = notifyStateEntry{At: now.Add(-5 * time.Minute).Unix(), OK: false}
	assert.False(t, notifyShouldSend(state, NotifyEventJobFailed, now))
	state[NotifyEventJobFailed] = notifyStateEntry{At: now.Add(-16 * time.Minute).Unix(), OK: false}
	assert.True(t, notifyShouldSend(state, NotifyEventJobFailed, now))
}

func TestNotifyEvent_TelegramTextAndLevelLabel(t *testing.T) {
	event := notifyEvent{
		Event:   NotifyEventJobFailed,
		Level:   "error",
		Message: "任务 backup #7 失败（已尝试 3 次）：磁盘满",
		Details: map[string]any{"type": "backup", "jobId": 7},
		At:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	text := event.telegramText()
	assert.Contains(t, text, "[zjut.si 博客]")
	assert.Contains(t, text, "告警")
	assert.Contains(t, text, event.Message, "正文要带上告警内容")
	assert.Contains(t, text, "· jobId: 7", "details 按 key 排序逐行展开")
	assert.Contains(t, text, "· type: backup")
	assert.Contains(t, text, "2026-01-02 11:04:05", "时间按站点时区（UTC+8）渲染")

	// 疑似缺陷：telegramText 里没有任何地方写入 event.Event，
	// 所以纯文本消息看不出事件名（job_failed / disk_high / cert_expiring），
	// 只能靠正文猜；运维排查时缺少稳定的可检索字段。
	assert.NotContains(t, text, NotifyEventJobFailed)

	assert.Equal(t, "提示", notifyEvent{Level: "info"}.levelLabel())
	assert.Equal(t, "告警", notifyEvent{Level: "error"}.levelLabel())
	assert.Equal(t, "告警", notifyEvent{Level: ""}.levelLabel(), "未知级别按告警处理")

	// 没有 details 时不留空行。
	bare := notifyEvent{Level: "info", Message: "只有正文", At: event.At}
	assert.NotContains(t, bare.telegramText(), "· ")
}

func TestCertificateExpiry(t *testing.T) {
	notAfter := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	path := writeSelfSignedCert(t, notAfter)

	got, err := certificateExpiry(path)
	require.NoError(t, err)
	assert.WithinDuration(t, notAfter, got, time.Second)

	// 不是 PEM 的文件。
	badPath := filepath.Join(t.TempDir(), "not-a-cert.pem")
	require.NoError(t, os.WriteFile(badPath, []byte("hello"), 0o600))
	_, err = certificateExpiry(badPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不是 PEM 格式")

	// 文件不存在。
	_, err = certificateExpiry(filepath.Join(t.TempDir(), "missing.pem"))
	require.Error(t, err)

	// PEM 块类型对但内容不是证书。
	bogus := filepath.Join(t.TempDir(), "bogus.pem")
	require.NoError(t, os.WriteFile(
		bogus,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not-a-der-cert")}),
		0o600,
	))
	_, err = certificateExpiry(bogus)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "不是 PEM 格式", "这里是解析证书失败，不是格式识别失败")
}

// ---------- 投递 ----------

func TestNotifyService_DeliverWebhook(t *testing.T) {
	sink, url := startNotifySink(t)

	cfg := webhookConfig(url + "/hook")
	event := notifyEvent{
		Event:   NotifyEventJobFailed,
		Level:   "error",
		Message: "任务 backup #7 失败",
		Details: map[string]any{"jobId": 7, "attempts": 3},
		At:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	require.NoError(t, NotifyService{}.deliverWebhook(context.Background(), cfg, event))

	hits, method, path, ctype, body := sink.snapshot()
	assert.Equal(t, 1, hits)
	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, "/hook", path)
	assert.Equal(t, "application/json", ctype)

	var payload notifyPayload
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Equal(t, "blog", payload.Source)
	assert.Equal(t, NotifyEventJobFailed, payload.Event)
	assert.Equal(t, "error", payload.Level)
	assert.Equal(t, event.Message, payload.Message)
	assert.NotEmpty(t, payload.At)
	assert.Equal(t, float64(7), payload.Details["jobId"])

	// 任何非 2xx 都要报错，且错误文案里不能带出通道地址。
	sink.setResponse(http.StatusServiceUnavailable, "nope")
	err := NotifyService{}.deliverWebhook(context.Background(), cfg, event)
	require.Error(t, err)

	var statusErr *notifyHTTPError
	require.True(t, errors.As(err, &statusErr))
	assert.Equal(t, http.StatusServiceUnavailable, statusErr.Status)
	assert.Equal(t, "通道返回 HTTP 503", err.Error())
	assert.NotContains(t, err.Error(), url, "错误文案不能回显通道地址")

	// 2xx 边界：204 也算成功。
	sink.setResponse(http.StatusNoContent, "")
	assert.NoError(t, NotifyService{}.deliverWebhook(context.Background(), cfg, event))
}

func TestNotifyService_DeliverRoutesUnknownChannelToWebhook(t *testing.T) {
	sink, url := startNotifySink(t)

	cfg := webhookConfig(url + "/fallback")
	cfg.Channel = "carrier-pigeon" // 非 telegram 一律走 webhook（§13：只有两个渠道）

	require.NoError(t, NotifyService{}.deliver(context.Background(), cfg, notifyEvent{
		Event: NotifyEventTest, Level: "info", Message: "路由测试", At: time.Now(),
	}))
	assert.Equal(t, 1, sink.hitCount())
}

func TestNotifyService_DeliverTelegram(t *testing.T) {
	sink, url := startNotifySink(t)
	rec := redirectNotifyHTTPClient(t, url)

	const token = "123456:AA-s3cr3t-bot-token"
	cfg := telegramConfig(token, "-100200")
	event := notifyEvent{
		Event:   NotifyEventCertExpiring,
		Level:   "error",
		Message: "TLS 证书还有 3 天到期",
		Details: map[string]any{"daysLeft": 3},
		At:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	require.NoError(t, NotifyService{}.deliver(context.Background(), cfg, event))

	gotURL, gotBody := rec.get()
	assert.Equal(t, "https://api.telegram.org/bot"+token+"/sendMessage", gotURL, "bot token 走 URL 路径")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	assert.Equal(t, "-100200", payload["chat_id"])
	assert.Equal(t, true, payload["disable_web_page_preview"])
	text, ok := payload["text"].(string)
	require.True(t, ok)
	assert.Contains(t, text, event.Message)

	// Telegram 出错时也回 200，所以要额外看报文里的 ok。
	sink.setResponse(http.StatusOK, `{"ok":false,"description":"chat not found"}`)
	err := NotifyService{}.deliver(context.Background(), cfg, event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "telegram 拒绝投递")
	assert.Contains(t, err.Error(), "chat not found")
	assert.NotContains(t, err.Error(), token, "错误文案不能泄漏 bot token")

	// 报文不是 JSON。
	sink.setResponse(http.StatusOK, "definitely not json")
	err = NotifyService{}.deliver(context.Background(), cfg, event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "解析 Telegram 响应失败")

	// 非 2xx：走 notifyHTTPError。
	sink.setResponse(http.StatusBadGateway, "bad gateway")
	err = NotifyService{}.deliver(context.Background(), cfg, event)
	var statusErr *notifyHTTPError
	require.True(t, errors.As(err, &statusErr))
	assert.Equal(t, http.StatusBadGateway, statusErr.Status)
	assert.NotContains(t, err.Error(), token)

	// ok=true 正常返回。
	sink.setResponse(http.StatusOK, `{"ok":true}`)
	assert.NoError(t, NotifyService{}.deliver(context.Background(), cfg, event))
}

func TestNotifyDeliveryHint(t *testing.T) {
	// 非 2xx 直接回报状态码。
	assert.Equal(t, "通道返回 HTTP 502", notifyDeliveryHint(&notifyHTTPError{Status: 502}))
	// 超时。
	assert.Equal(t, "请求超时", notifyDeliveryHint(context.DeadlineExceeded))
	assert.Equal(t, "请求超时", notifyDeliveryHint(os.ErrDeadlineExceeded))
	// 其余一律归成"连不上"，不把底层错误文本透给前端（§7）。
	assert.Equal(t, "连不上告警通道（检查地址、DNS 与容器出网）", notifyDeliveryHint(errors.New("dial tcp: connect: connection refused")))
}

// ---------- Test（发送测试告警） ----------

func TestNotifyService_TestIgnoresEnabledAndAudits(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	cfg := webhookConfig(url + "/test")
	cfg.Enabled = false // 契约：Test 不看 enabled，只看通道配没配齐
	mustSaveNotify(t, cfg)

	actor := testActor(t)
	require.NoError(t, NotifyService{}.Test(context.Background(), actor))

	hits, method, path, _, body := sink.snapshot()
	assert.Equal(t, 1, hits, "enabled=false 也要投递")
	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, "/test", path)

	var payload notifyPayload
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Equal(t, NotifyEventTest, payload.Event)
	assert.Equal(t, "info", payload.Level)
	assert.Contains(t, payload.Message, "测试告警")

	// 审计要记在发起人名下。
	entry := auditLogByAction(t, AuditActionNotifyTest)
	assert.Equal(t, actor.AdminID, entry.AdminID)
	assert.Equal(t, "settings", entry.TargetType)
	assert.Equal(t, settingsTargetID, entry.TargetID)
	assert.Equal(t, utils.HashIP(actor.IP), entry.IPHash)
	assert.Equal(t, `{"channel":"webhook"}`, entry.MetaJSON)

	// Test 不走 dispatch，所以不写静默状态。
	assert.NotContains(t, mustNotifyState(t), NotifyEventTest)
}

func TestNotifyService_TestRequiresConfiguredChannel(t *testing.T) {
	setupEnv(t)

	actor := testActor(t)

	// 什么都没配。
	err := NotifyService{}.Test(context.Background(), actor)
	appErr := appErrorAny(t, err)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	assert.Contains(t, appErr.Message, "还没配好告警通道")

	// 选了 telegram 但缺 chatId。
	mustSaveNotify(t, NotifyConfig{
		Channel:              NotifyChannelTelegram,
		TelegramBotToken:     "token",
		Events:               NotifyEvents{JobFailed: true, DiskHigh: true, CertExpiring: true},
		DiskThresholdPercent: notifyDefaultDiskPercent,
		CertExpireDays:       notifyDefaultCertDays,
	})
	err = NotifyService{}.Test(context.Background(), actor)
	assert.Equal(t, errs.ErrorValidation, appErrorAny(t, err).Code)
	assert.Zero(t, countRows(t, "audit_logs", "action = ?", AuditActionNotifyTest))
}

func TestNotifyService_TestReportsDeliveryFailure(t *testing.T) {
	setupEnv(t)

	// 起一个立刻关掉的服务：连接必然被拒，且不需要真连外网。
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	mustSaveNotify(t, webhookConfig(deadURL+"/dead"))
	actor := testActor(t)

	err := NotifyService{}.Test(context.Background(), actor)
	appErr := appErrorAny(t, err)
	assert.Equal(t, errs.ErrorInternal, appErr.Code)
	assert.Contains(t, appErr.Message, "告警发送失败")
	assert.NotContains(t, appErr.Message, deadURL, "不能把通道地址透给前端")

	// 投递失败不写审计。
	assert.Zero(t, countRows(t, "audit_logs", "action = ?", AuditActionNotifyTest))
}

// ---------- alert / clearAlert / dispatch ----------

func TestNotifyService_AlertSilenceAndClear(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	mustSaveNotify(t, webhookConfig(url+"/alert"))

	job := &model.Job{ID: 7, Type: model.JobTypeBackup, Attempts: 3}

	NotifyService{}.JobFailed(job, "磁盘满")
	assert.Equal(t, 1, sink.hitCount())

	state := mustNotifyState(t)
	require.Contains(t, state, NotifyEventJobFailed)
	assert.True(t, state[NotifyEventJobFailed].OK)

	// 24h 静默：同一事件在窗口内不重复投递。
	NotifyService{}.JobFailed(job, "磁盘满")
	assert.Equal(t, 1, sink.hitCount(), "静默期内不应重复打扰")

	// 条件恢复：clearAlert 后该事件从状态里消失，下次出问题能立刻告警。
	NotifyService{}.clearAlert(NotifyEventJobFailed)
	assert.NotContains(t, mustNotifyState(t), NotifyEventJobFailed)

	NotifyService{}.JobFailed(job, "磁盘满")
	assert.Equal(t, 2, sink.hitCount(), "恢复后再出问题要立刻投递")

	// clearAlert 对不存在的键是幂等的。
	assert.NotPanics(t, func() { NotifyService{}.clearAlert("never-seen") })
}

func TestNotifyService_AlertRetriesAfterFailure(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	sink.setResponse(http.StatusInternalServerError, "boom")
	mustSaveNotify(t, webhookConfig(url+"/failing"))

	job := &model.Job{ID: 9, Type: model.JobTypeCleanup, Attempts: 1}

	NotifyService{}.JobFailed(job, "boom")
	assert.Equal(t, 1, sink.hitCount())

	state := mustNotifyState(t)
	require.Contains(t, state, NotifyEventJobFailed)
	assert.False(t, state[NotifyEventJobFailed].OK, "失败要落 OK=false，15 分钟后才重试")

	// 失败后的重试间隔是 15min，窗口内不重投。
	NotifyService{}.JobFailed(job, "boom")
	assert.Equal(t, 1, sink.hitCount())
}

func TestNotifyService_AlertSkipsDisabledEventOrChannel(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	job := &model.Job{ID: 11, Type: model.JobTypeBackup, Attempts: 1}

	// 事件被关掉。
	cfg := webhookConfig(url + "/off")
	cfg.Events.JobFailed = false
	mustSaveNotify(t, cfg)

	NotifyService{}.JobFailed(job, "boom")
	assert.Zero(t, sink.hitCount())
	assert.Empty(t, mustNotifyState(t), "事件被关掉时连状态都不该写")

	// 总开关关掉。
	cfg = webhookConfig(url + "/disabled")
	cfg.Enabled = false
	mustSaveNotify(t, cfg)

	NotifyService{}.JobFailed(job, "boom")
	assert.Zero(t, sink.hitCount())

	// 未知事件（自检类）不受事件开关约束。
	cfg = webhookConfig(url + "/custom")
	mustSaveNotify(t, cfg)
	NotifyService{}.alert(notifyEvent{
		Event: "custom_event", Level: "info", Message: "自定义事件", At: time.Now(),
	})
	assert.Equal(t, 1, sink.hitCount())
	assert.Contains(t, mustNotifyState(t), "custom_event")
}

func TestNotifyService_AlertSurvivesBrokenStateAndConfig(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	job := &model.Job{ID: 13, Type: model.JobTypeBackup, Attempts: 1}

	// 状态是脏 JSON：dispatch 读不出状态，只记日志，不 panic 也不投递。
	mustSaveNotify(t, webhookConfig(url+"/broken-state"))
	require.NoError(t, NotifyService{}.saveState(notifyState{}))
	putRawSetting(t, model.SettingNotifyState, "{broken")

	assert.NotPanics(t, func() { NotifyService{}.JobFailed(job, "boom") })
	assert.Zero(t, sink.hitCount())

	// clearAlert 遇到同样的脏状态也不能 panic。
	assert.NotPanics(t, func() { NotifyService{}.clearAlert(NotifyEventJobFailed) })

	// 配置是脏 JSON：alert 读不出配置，同样只记日志。
	putRawSetting(t, model.SettingNotify, "{broken")
	assert.NotPanics(t, func() { NotifyService{}.JobFailed(job, "boom") })
	assert.Zero(t, sink.hitCount())
}

func TestNotifyService_AlertSurvivesNullStateJSON(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	mustSaveNotify(t, webhookConfig(url+"/null-state"))
	require.NoError(t, NotifyService{}.saveState(notifyState{}))
	putRawSetting(t, model.SettingNotifyState, "null")

	// 库里存着字面量 null 时，state() 反序列化出来的 map 是 nil（encoding/json 的既定行为），
	// 随后 `state[event.Event] = entry` 会 "assignment to entry in nil map" panic；
	// Watch 跑在 initialize 的 errgroup goroutine 里，panic 会拖垮整个进程。
	// D18 修复：state() 把 nil 归一成空表，脏数据只该退化成"没有历史状态"。
	assert.NotPanics(t, func() {
		NotifyService{}.JobFailed(&model.Job{ID: 1, Type: model.JobTypeBackup, Attempts: 1}, "boom")
	})
	assert.Equal(t, 1, sink.hitCount(), "脏状态不该拦住投递")

	// 状态被重写成了合法对象，后续读取不再依赖兜底。
	state, err := NotifyService{}.state()
	require.NoError(t, err)
	assert.NotNil(t, state)
	assert.Contains(t, state, NotifyEventJobFailed)
}

func TestNotifyService_SettingsTableFailures(t *testing.T) {
	t.Run("读配置失败", func(t *testing.T) {
		setupEnv(t)
		require.NoError(t, global.DB.Exec("DROP TABLE settings").Error)

		// 表都读不到时是内部错误（不是"没配"，不能静默退回默认值）。
		_, err := NotifyService{}.Config()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "读取告警配置失败")

		_, err = NotifyService{}.View()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "读取告警配置失败")

		_, err = NotifyService{}.Update(NotifyUpdate{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "读取告警配置失败")

		err = NotifyService{}.Test(context.Background(), PostActor{AdminID: 1, IP: "203.0.113.9"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "读取告警配置失败")
	})

	t.Run("读状态失败只记日志", func(t *testing.T) {
		setupEnv(t)
		require.NoError(t, global.DB.Exec("DROP TABLE settings").Error)

		_, err := NotifyService{}.state()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "读取告警状态失败")

		// 巡检与告警都不允许因为读不到状态就 panic。
		assert.NotPanics(t, func() { NotifyService{}.clearAlert(NotifyEventJobFailed) })
		assert.NotPanics(t, func() { NotifyService{}.checkConditions() })
	})
}

func TestPostJSONLocalFailures(t *testing.T) {
	// 地址非法：在构造请求阶段就失败，不会走到网络。
	_, err := postJSON(context.Background(), "://bad-endpoint", map[string]any{"a": 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "构造告警请求失败")

	// 报文里带函数：序列化失败，同样不发出请求。
	_, err = postJSON(context.Background(), "https://example.test/hook", map[string]any{"fn": func() {}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "序列化告警报文失败")
}

func TestNotifyService_JobFailedPayload(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	mustSaveNotify(t, webhookConfig(url+"/job"))

	job := &model.Job{ID: 42, Type: model.JobTypeLinkCheck, Attempts: 2}
	NotifyService{}.JobFailed(job, "上游超时")

	_, _, _, _, body := sink.snapshot()
	var payload notifyPayload
	require.NoError(t, json.Unmarshal(body, &payload))

	assert.Equal(t, NotifyEventJobFailed, payload.Event)
	assert.Equal(t, "error", payload.Level)
	assert.Contains(t, payload.Message, "link_check")
	assert.Contains(t, payload.Message, "#42")
	assert.Contains(t, payload.Message, "上游超时")
	assert.Equal(t, float64(42), payload.Details["jobId"])
	assert.Equal(t, "link_check", payload.Details["type"])
	assert.Equal(t, float64(2), payload.Details["attempts"])
	assert.Equal(t, "上游超时", payload.Details["lastError"])
}

func TestNotifyService_JobFailedIgnoresNilJob(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	mustSaveNotify(t, webhookConfig(url+"/nil"))

	// nil job 直接返回：不 panic、不投递、不写状态。
	assert.NotPanics(t, func() { NotifyService{}.JobFailed(nil, "boom") })
	assert.Zero(t, sink.hitCount())
	assert.Empty(t, mustNotifyState(t))
}

// ---------- 巡检 ----------

func TestNotifyService_CheckConditions(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	mustSaveNotify(t, webhookConfig(url+"/check"))

	// 证书留空 → checkCert 完全跳过；磁盘在 Windows 上不可控（见下一个用例）。
	global.Config.TLS.CertFile = ""
	NotifyService{}.checkConditions()
	assert.Zero(t, sink.hitCount())

	// 配置脏 → 只记日志。
	putRawSetting(t, model.SettingNotify, "{broken")
	assert.NotPanics(t, func() { NotifyService{}.checkConditions() })

	// 总开关关掉 → 两个检查都不跑。
	mustSaveNotify(t, webhookConfig(url+"/off"))
	global.Config.TLS.CertFile = writeSelfSignedCert(t, time.Now().Add(time.Hour))
	cfg := mustNotifyConfig(t)
	cfg.Enabled = false
	mustSaveNotify(t, cfg)
	assert.NotPanics(t, func() { NotifyService{}.checkConditions() })
	assert.Zero(t, sink.hitCount())
}

func TestNotifyService_CheckDiskIsUncontrollableOnWindows(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	mustSaveNotify(t, webhookConfig(url+"/disk"))

	svc := NotifyService{}
	cfg := mustNotifyConfig(t)

	// 为什么只断言"不 panic + 不改状态"：DiskUsagePercent 是 build tag 决定的包级函数
	// （utils/diskspace_windows.go 在 Windows 上恒返回 -1，不做统计），测试里没法替换它，
	// 所以"超过阈值"和"低于阈值"两条分支在本机都不可达。
	used, err := utils.DiskUsagePercent(global.Config.Upload.Dir)
	require.NoError(t, err)

	if used < 0 {
		cfg.DiskThresholdPercent = 1 // 阈值压到最低也触发不了：used < 0 直接返回
		assert.NotPanics(t, func() { svc.checkDisk(cfg) })
		assert.Zero(t, sink.hitCount())
		assert.Empty(t, mustNotifyState(t))
		return
	}

	// 非 Windows：真实磁盘占用不可控，只要求不 panic 且状态与阈值自洽。
	cfg.DiskThresholdPercent = 1
	assert.NotPanics(t, func() { svc.checkDisk(cfg) })
	if used >= cfg.DiskThresholdPercent {
		assert.Contains(t, mustNotifyState(t), NotifyEventDiskHigh)
	} else {
		assert.NotContains(t, mustNotifyState(t), NotifyEventDiskHigh)
	}
}

func TestNotifyService_CheckCertAlertsThenClears(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	mustSaveNotify(t, webhookConfig(url+"/cert"))

	svc := NotifyService{}
	cfg := mustNotifyConfig(t)
	require.Equal(t, notifyDefaultCertDays, cfg.CertExpireDays)

	// 快到期（还剩 2 小时 → 0 天）→ 触发。
	global.Config.TLS.CertFile = writeSelfSignedCert(t, time.Now().Add(2*time.Hour))
	svc.checkCert(cfg)
	assert.Equal(t, 1, sink.hitCount())

	_, _, _, _, body := sink.snapshot()
	var payload notifyPayload
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Equal(t, NotifyEventCertExpiring, payload.Event)
	assert.Contains(t, payload.Message, "还有 0 天到期")
	assert.Contains(t, payload.Message, "阈值 14 天")
	assert.Equal(t, global.Config.TLS.CertFile, payload.Details["certFile"])
	assert.Equal(t, float64(14), payload.Details["thresholdDays"])

	state := mustNotifyState(t)
	require.Contains(t, state, NotifyEventCertExpiring)
	assert.True(t, state[NotifyEventCertExpiring].OK)

	// 还早（100 天）→ 条件恢复，清掉静默状态且不重复投递。
	global.Config.TLS.CertFile = writeSelfSignedCert(t, time.Now().Add(100*24*time.Hour))
	svc.checkCert(cfg)
	assert.Equal(t, 1, sink.hitCount())
	assert.NotContains(t, mustNotifyState(t), NotifyEventCertExpiring, "条件恢复即清零")
}

func TestNotifyService_CheckCertExpiredAndUnreadable(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	mustSaveNotify(t, webhookConfig(url+"/cert2"))

	svc := NotifyService{}
	cfg := mustNotifyConfig(t)

	// 已过期 → 文案换成"已过期 N 天"。
	global.Config.TLS.CertFile = writeSelfSignedCert(t, time.Now().Add(-48*time.Hour))
	svc.checkCert(cfg)
	assert.Equal(t, 1, sink.hitCount())

	_, _, _, _, body := sink.snapshot()
	var payload notifyPayload
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Contains(t, payload.Message, "TLS 证书已过期 2 天")

	// 文件不存在 → 告警而不是 panic（路径打错是最常见的现场故障）。
	NotifyService{}.clearAlert(NotifyEventCertExpiring)
	global.Config.TLS.CertFile = filepath.Join(t.TempDir(), "missing.pem")
	assert.NotPanics(t, func() { svc.checkCert(cfg) })
	assert.Equal(t, 2, sink.hitCount())

	_, _, _, _, body = sink.snapshot()
	require.NoError(t, json.Unmarshal(body, &payload))
	assert.Contains(t, payload.Message, "读取 TLS 证书失败")
	assert.Contains(t, mustNotifyState(t), NotifyEventCertExpiring)
}

func TestNotifyService_CheckCertSkipsWhenCertFileEmpty(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	mustSaveNotify(t, webhookConfig(url+"/cert3"))

	global.Config.TLS.CertFile = ""
	cfg := mustNotifyConfig(t)

	// 留空 = 不检查（本地开发与非 TLS 部署）。
	assert.NotPanics(t, func() { NotifyService{}.checkCert(cfg) })
	assert.Zero(t, sink.hitCount())
	assert.Empty(t, mustNotifyState(t))

	// 只有空白也算留空。
	global.Config.TLS.CertFile = "   "
	assert.NotPanics(t, func() { NotifyService{}.checkCert(cfg) })
	assert.Zero(t, sink.hitCount())
}

func TestNotifyService_WatchReturnsOnContextCancel(t *testing.T) {
	setupEnv(t)

	sink, url := startNotifySink(t)
	mustSaveNotify(t, webhookConfig(url+"/watch"))
	global.Config.TLS.CertFile = ""

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- NotifyService{}.Watch(ctx) }()

	// 巡检 tick 间隔是 1 分钟，远长于 ctx 生命周期：这里只断言取消后立刻正常返回，不会挂住测试。
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Watch 未在 ctx 取消后返回")
	}

	// 启动时先跑过一次 checkConditions；证书留空所以没有投递。
	assert.Zero(t, sink.hitCount())
}
