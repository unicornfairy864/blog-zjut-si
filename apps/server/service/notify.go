package service

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// NotifyService 是告警通道（agent.md §13 / §14）。
//
// 触发点只有三个，都由本服务自己决定"要不要发"：
//   - 任务进入 failed（调度器通过 task.SetJobFailedHook 回调 JobFailed）；
//   - 磁盘占用达到阈值（Watch 每次 tick 看一次）；
//   - TLS 证书剩余天数不足（Watch 每次 tick 看一次）。
//
// 配置存 settings.notify（管理端可读写，密钥不回显），投递状态存 settings.notify_state。
// 不新增表、不新增 goroutine 以外的组件：Watch 由 initialize 用 errgroup 托管。
type NotifyService struct{}

// 告警事件名；同时是 settings.notify_state 的键与 webhook 报文里的 event 字段。
const (
	NotifyEventJobFailed    = "job_failed"
	NotifyEventDiskHigh     = "disk_high"
	NotifyEventCertExpiring = "cert_expiring"
	// NotifyEventTest 只用于"发送测试"，不参与事件勾选。
	NotifyEventTest = "test"
)

// 告警渠道；同一时刻只有一个生效（§13：任选其一）。
const (
	NotifyChannelWebhook  = "webhook"
	NotifyChannelTelegram = "telegram"
)

// 告警的默认值、边界与节奏。
const (
	// notifyDefaultDiskPercent / notifyDefaultCertDays 是界面默认值。
	notifyDefaultDiskPercent = 85
	notifyDefaultCertDays    = 14
	// notifyRepeatInterval 是同一事件的静默期：发送成功后 24h 内不重复打扰。
	notifyRepeatInterval = 24 * time.Hour
	// notifyRetryInterval 是投递失败后的重试间隔（比静默期短，避免一次网络抖动吞掉告警）。
	notifyRetryInterval = 15 * time.Minute
	// notifyHTTPTimeout 是单次投递超时；调度器是单 goroutine，不能在这里挂太久。
	notifyHTTPTimeout = 5 * time.Second
	// notifyWatchInterval 是磁盘/证书检查节奏。
	notifyWatchInterval = time.Minute
	// notifyURLMaxLen 是 webhook 地址长度上限。
	notifyURLMaxLen = 500
	// notifyCertDaysMax 是证书剩余天数阈值的上限（用于自测时故意放大）。
	notifyCertDaysMax = 3650
)

// notifyMu 保护"读配置 → 判断静默期 → 投递 → 写状态"这段临界区。
//
// 触发点分布在两个 goroutine（调度器回调与 Watch 巡检），不加锁会出现重复发送。
var notifyMu sync.Mutex

// notifyHTTPClient 复用连接；超时与 §13 的单 goroutine 调度器匹配。
var notifyHTTPClient = &http.Client{Timeout: notifyHTTPTimeout}

// NotifyEvents 是三类事件各自的开关。
type NotifyEvents struct {
	JobFailed    bool `json:"jobFailed"`
	DiskHigh     bool `json:"diskHigh"`
	CertExpiring bool `json:"certExpiring"`
}

// NotifyConfig 是 settings.notify 的完整结构（含密钥，只在服务端内存与库里出现）。
type NotifyConfig struct {
	Enabled              bool         `json:"enabled"`
	Channel              string       `json:"channel"`
	WebhookURL           string       `json:"webhookUrl"`
	TelegramBotToken     string       `json:"telegramBotToken"`
	TelegramChatID       string       `json:"telegramChatId"`
	Events               NotifyEvents `json:"events"`
	DiskThresholdPercent int          `json:"diskThresholdPercent"`
	CertExpireDays       int          `json:"certExpireDays"`
}

// NotifyLastAlert 是某事件最近一次投递结果（只读回显，便于排查"到底发没发出去"）。
type NotifyLastAlert struct {
	Event string `json:"event"`
	At    int64  `json:"at"`
	OK    bool   `json:"ok"`
}

// NotifyView 是 GET /api/admin/settings 里 notify 字段的回显形态。
//
// 密钥（webhookUrl、telegramBotToken）永不回显，只给"配没配"的布尔值（§9）。
type NotifyView struct {
	Enabled              bool              `json:"enabled"`
	Channel              string            `json:"channel"`
	WebhookURLSet        bool              `json:"webhookUrlSet"`
	TelegramBotTokenSet  bool              `json:"telegramBotTokenSet"`
	TelegramChatID       string            `json:"telegramChatId"`
	Events               NotifyEvents      `json:"events"`
	DiskThresholdPercent int               `json:"diskThresholdPercent"`
	CertExpireDays       int               `json:"certExpireDays"`
	CertFile             string            `json:"certFile"`
	LastAlerts           []NotifyLastAlert `json:"lastAlerts"`
}

// NotifyUpdate 是 PUT /api/admin/settings 里 notify 字段的入参。
//
// 指针为 nil 表示"不改这一项"；密钥项给空串表示"清空"（非密钥项同理）。
type NotifyUpdate struct {
	Enabled              *bool         `json:"enabled"`
	Channel              *string       `json:"channel"`
	WebhookURL           *string       `json:"webhookUrl"`
	TelegramBotToken     *string       `json:"telegramBotToken"`
	TelegramChatID       *string       `json:"telegramChatId"`
	Events               *NotifyEvents `json:"events"`
	DiskThresholdPercent *int          `json:"diskThresholdPercent"`
	CertExpireDays       *int          `json:"certExpireDays"`
}

// notifyStateEntry 记录某事件最近一次投递的时间与结果。
type notifyStateEntry struct {
	At int64 `json:"at"`
	OK bool  `json:"ok"`
}

// notifyState 是 settings.notify_state 的结构：事件名 → 最近一次投递。
type notifyState map[string]notifyStateEntry

// notifyEvent 是一条待投递的告警。
type notifyEvent struct {
	Event   string
	Level   string
	Message string
	Details map[string]any
	At      time.Time
}

// notifyPayload 是 webhook 收到的报文（runbook §20.1）。
type notifyPayload struct {
	Source  string         `json:"source"`
	Event   string         `json:"event"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	At      string         `json:"at"`
	Details map[string]any `json:"details,omitempty"`
}

// notifyHTTPError 表示通道返回了非 2xx。
type notifyHTTPError struct {
	Status int
}

// Error 实现 error；不回显响应体，避免把第三方页面内容写进日志。
func (e *notifyHTTPError) Error() string { return fmt.Sprintf("通道返回 HTTP %d", e.Status) }

// defaultNotifyConfig 返回未配置时的默认值：关着，但事件全勾上，阈值按 §13 / §14。
func defaultNotifyConfig() NotifyConfig {
	return NotifyConfig{
		Channel:              NotifyChannelWebhook,
		Events:               NotifyEvents{JobFailed: true, DiskHigh: true, CertExpiring: true},
		DiskThresholdPercent: notifyDefaultDiskPercent,
		CertExpireDays:       notifyDefaultCertDays,
	}
}

// Config 读取告警配置；键缺失或解析失败时退回默认值。
func (s NotifyService) Config() (NotifyConfig, error) {
	cfg := defaultNotifyConfig()

	setting, err := model.GetSetting(global.DB, model.SettingNotify)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("读取告警配置失败: %w", err)
	}
	if err := json.Unmarshal([]byte(setting.ValueJSON), &cfg); err != nil {
		return defaultNotifyConfig(), fmt.Errorf("解析告警配置失败: %w", err)
	}
	cfg.normalize()
	return cfg, nil
}

// normalize 收敛非法取值，避免脏数据让巡检逻辑算错。
func (c *NotifyConfig) normalize() {
	c.Channel = strings.ToLower(strings.TrimSpace(c.Channel))
	if c.Channel == "" {
		c.Channel = NotifyChannelWebhook
	}
	c.WebhookURL = strings.TrimSpace(c.WebhookURL)
	c.TelegramBotToken = strings.TrimSpace(c.TelegramBotToken)
	c.TelegramChatID = strings.TrimSpace(c.TelegramChatID)
	if c.DiskThresholdPercent < 1 || c.DiskThresholdPercent > 99 {
		c.DiskThresholdPercent = notifyDefaultDiskPercent
	}
	if c.CertExpireDays < 1 || c.CertExpireDays > notifyCertDaysMax {
		c.CertExpireDays = notifyDefaultCertDays
	}
}

// eventEnabled 判断某事件是否被勾选；未知事件（自检）一律放行。
func (c NotifyConfig) eventEnabled(event string) bool {
	switch event {
	case NotifyEventJobFailed:
		return c.Events.JobFailed
	case NotifyEventDiskHigh:
		return c.Events.DiskHigh
	case NotifyEventCertExpiring:
		return c.Events.CertExpiring
	default:
		return true
	}
}

// channelConfigured 判断当前渠道的必填项是否齐了。
func (c NotifyConfig) channelConfigured() bool {
	if c.Channel == NotifyChannelTelegram {
		return c.TelegramBotToken != "" && c.TelegramChatID != ""
	}
	return c.WebhookURL != ""
}

// View 返回脱敏后的告警配置与最近投递状态。
func (s NotifyService) View() (NotifyView, error) {
	notifyMu.Lock()
	defer notifyMu.Unlock()

	cfg, err := s.Config()
	if err != nil {
		return NotifyView{}, err
	}
	return s.view(cfg)
}

// view 组装回显；调用方需已持锁（或确认无并发写）。
func (s NotifyService) view(cfg NotifyConfig) (NotifyView, error) {
	state, err := s.state()
	if err != nil {
		return NotifyView{}, err
	}

	alerts := make([]NotifyLastAlert, 0, len(state))
	for _, event := range []string{NotifyEventJobFailed, NotifyEventDiskHigh, NotifyEventCertExpiring} {
		if entry, ok := state[event]; ok {
			alerts = append(alerts, NotifyLastAlert{Event: event, At: entry.At, OK: entry.OK})
		}
	}

	return NotifyView{
		Enabled:              cfg.Enabled,
		Channel:              cfg.Channel,
		WebhookURLSet:        cfg.WebhookURL != "",
		TelegramBotTokenSet:  cfg.TelegramBotToken != "",
		TelegramChatID:       cfg.TelegramChatID,
		Events:               cfg.Events,
		DiskThresholdPercent: cfg.DiskThresholdPercent,
		CertExpireDays:       cfg.CertExpireDays,
		CertFile:             global.Config.TLS.CertFile,
		LastAlerts:           alerts,
	}, nil
}

// Update 合并写入告警配置并返回新回显；只写 settings.notify，不动 content_version。
func (s NotifyService) Update(in NotifyUpdate) (NotifyView, error) {
	notifyMu.Lock()
	defer notifyMu.Unlock()

	cfg, err := s.Config()
	if err != nil {
		return NotifyView{}, err
	}

	if in.Enabled != nil {
		cfg.Enabled = *in.Enabled
	}
	if in.Channel != nil {
		cfg.Channel = strings.ToLower(strings.TrimSpace(*in.Channel))
	}
	if in.WebhookURL != nil {
		cfg.WebhookURL = strings.TrimSpace(*in.WebhookURL)
	}
	if in.TelegramBotToken != nil {
		cfg.TelegramBotToken = strings.TrimSpace(*in.TelegramBotToken)
	}
	if in.TelegramChatID != nil {
		cfg.TelegramChatID = strings.TrimSpace(*in.TelegramChatID)
	}
	if in.Events != nil {
		cfg.Events = *in.Events
	}
	if in.DiskThresholdPercent != nil {
		cfg.DiskThresholdPercent = *in.DiskThresholdPercent
	}
	if in.CertExpireDays != nil {
		cfg.CertExpireDays = *in.CertExpireDays
	}

	if err := validateNotifyConfig(cfg); err != nil {
		return NotifyView{}, err
	}
	if err := s.save(cfg); err != nil {
		return NotifyView{}, err
	}
	return s.view(cfg)
}

// Test 立刻投递一条测试告警（不受 enabled 与静默期影响，只看通道配没配齐）。
func (s NotifyService) Test(ctx context.Context, actor PostActor) error {
	notifyMu.Lock()
	defer notifyMu.Unlock()

	cfg, err := s.Config()
	if err != nil {
		return err
	}
	if !cfg.channelConfigured() {
		return errs.New(errs.ErrorValidation, "还没配好告警通道：webhook 要填地址，Telegram 要填 bot token 与 chatId")
	}

	sendCtx, cancel := context.WithTimeout(ctx, notifyHTTPTimeout)
	defer cancel()

	event := notifyEvent{
		Event:   NotifyEventTest,
		Level:   "info",
		Message: "这是一条测试告警，收到即说明通道可用。",
		At:      time.Now(),
	}
	if err := s.deliver(sendCtx, cfg, event); err != nil {
		global.Log.Warn("测试告警投递失败", "channel", cfg.Channel, "err", err)
		return errs.New(errs.ErrorInternal, "告警发送失败："+notifyDeliveryHint(err))
	}

	global.Log.Info("测试告警已发送", "channel", cfg.Channel)
	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionNotifyTest,
		TargetType: "settings",
		TargetID:   settingsTargetID,
		Meta:       map[string]any{"channel": cfg.Channel},
		IP:         actor.IP,
	})
	return nil
}

// JobFailed 是任务进入 failed 时的回调（由 task.Scheduler 经 task.SetJobFailedHook 调用）。
//
// 刻意用 Background 而不是调度器的 ctx：进程正在退出时也要把这条告警发出去。
func (s NotifyService) JobFailed(job *model.Job, cause string) {
	if job == nil {
		return
	}
	message := fmt.Sprintf("任务 %s #%d 失败（已尝试 %d 次）：%s", job.Type, job.ID, job.Attempts, cause)
	s.alert(notifyEvent{
		Event:   NotifyEventJobFailed,
		Level:   "error",
		Message: message,
		Details: map[string]any{
			"jobId":     job.ID,
			"type":      job.Type,
			"attempts":  job.Attempts,
			"lastError": cause,
		},
		At: time.Now(),
	})
}

// Watch 阻塞巡检磁盘与证书，直到 ctx 取消；由 initialize 用 errgroup 托管。
func (s NotifyService) Watch(ctx context.Context) error {
	global.Log.Info("告警巡检启动", "interval", notifyWatchInterval.String())

	s.checkConditions()
	ticker := time.NewTicker(notifyWatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			global.Log.Info("告警巡检停止")
			return nil
		case <-ticker.C:
			s.checkConditions()
		}
	}
}

// checkConditions 看一遍磁盘与证书；条件恢复时清掉静默状态。
func (s NotifyService) checkConditions() {
	cfg, err := s.Config()
	if err != nil {
		global.Log.Error("读取告警配置失败", "err", err)
		return
	}
	if !cfg.Enabled {
		return
	}
	s.checkDisk(cfg)
	s.checkCert(cfg)
}

// checkDisk 检查 uploads 所在文件系统的占用。
func (s NotifyService) checkDisk(cfg NotifyConfig) {
	dir := global.Config.Upload.Dir
	used, err := utils.DiskUsagePercent(dir)
	if err != nil || used < 0 {
		return
	}
	if used < cfg.DiskThresholdPercent {
		s.clearAlert(NotifyEventDiskHigh)
		return
	}
	s.alert(notifyEvent{
		Event:   NotifyEventDiskHigh,
		Level:   "error",
		Message: fmt.Sprintf("磁盘占用 %d%%，已达阈值 %d%%（目录 %s）", used, cfg.DiskThresholdPercent, dir),
		Details: map[string]any{
			"usedPercent":      used,
			"thresholdPercent": cfg.DiskThresholdPercent,
			"dir":              dir,
		},
		At: time.Now(),
	})
}

// checkCert 检查宿主证书的剩余天数；未配置 tls.cert_file 时完全跳过。
func (s NotifyService) checkCert(cfg NotifyConfig) {
	path := strings.TrimSpace(global.Config.TLS.CertFile)
	if path == "" {
		return
	}

	expiry, err := certificateExpiry(path)
	if err != nil {
		s.alert(notifyEvent{
			Event:   NotifyEventCertExpiring,
			Level:   "error",
			Message: fmt.Sprintf("读取 TLS 证书失败（%s）：%v", path, err),
			Details: map[string]any{"certFile": path},
			At:      time.Now(),
		})
		return
	}

	days := int(time.Until(expiry).Hours() / 24)
	if days >= cfg.CertExpireDays {
		s.clearAlert(NotifyEventCertExpiring)
		return
	}

	message := fmt.Sprintf("TLS 证书还有 %d 天到期（%s，阈值 %d 天）",
		days, expiry.In(postSiteLocation()).Format("2006-01-02"), cfg.CertExpireDays)
	if days < 0 {
		message = fmt.Sprintf("TLS 证书已过期 %d 天（%s）",
			-days, expiry.In(postSiteLocation()).Format("2006-01-02"))
	}
	s.alert(notifyEvent{
		Event:   NotifyEventCertExpiring,
		Level:   "error",
		Message: message,
		Details: map[string]any{
			"certFile":      path,
			"notAfter":      expiry.UTC().Format(time.RFC3339),
			"daysLeft":      days,
			"thresholdDays": cfg.CertExpireDays,
		},
		At: time.Now(),
	})
}

// alert 按配置与静默期决定是否投递；所有触发点都走它。
func (s NotifyService) alert(event notifyEvent) {
	notifyMu.Lock()
	defer notifyMu.Unlock()

	cfg, err := s.Config()
	if err != nil {
		global.Log.Error("读取告警配置失败", "err", err)
		return
	}
	if !cfg.Enabled || !cfg.eventEnabled(event.Event) {
		return
	}
	s.dispatch(cfg, event)
}

// clearAlert 条件恢复时清掉静默状态，下次再出问题能立刻告警。
func (s NotifyService) clearAlert(event string) {
	notifyMu.Lock()
	defer notifyMu.Unlock()

	state, err := s.state()
	if err != nil {
		global.Log.Warn("读取告警状态失败", "err", err)
		return
	}
	if _, ok := state[event]; !ok {
		return
	}
	delete(state, event)
	if err := s.saveState(state); err != nil {
		global.Log.Warn("清理告警状态失败", "err", err)
	}
}

// dispatch 在已持锁的前提下投递一条告警并记录结果。
func (s NotifyService) dispatch(cfg NotifyConfig, event notifyEvent) {
	state, err := s.state()
	if err != nil {
		global.Log.Warn("读取告警状态失败", "err", err)
		return
	}

	now := time.Now()
	if !notifyShouldSend(state, event.Event, now) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), notifyHTTPTimeout)
	defer cancel()

	entry := notifyStateEntry{At: now.Unix(), OK: true}
	if err := s.deliver(ctx, cfg, event); err != nil {
		global.Log.Warn("告警投递失败", "event", event.Event, "channel", cfg.Channel, "err", err)
		entry.OK = false
	} else {
		global.Log.Info("告警已发送", "event", event.Event, "channel", cfg.Channel)
	}
	state[event.Event] = entry

	if err := s.saveState(state); err != nil {
		global.Log.Warn("保存告警状态失败", "err", err)
	}
}

// notifyShouldSend 判断是否过了静默期：发送成功等 24h，失败等 15 分钟。
func notifyShouldSend(state notifyState, event string, now time.Time) bool {
	entry, ok := state[event]
	if !ok {
		return true
	}
	window := notifyRepeatInterval
	if !entry.OK {
		window = notifyRetryInterval
	}
	return now.Sub(time.Unix(entry.At, 0)) >= window
}

// deliver 按当前渠道投递一条告警。
func (s NotifyService) deliver(ctx context.Context, cfg NotifyConfig, event notifyEvent) error {
	if cfg.Channel == NotifyChannelTelegram {
		return s.deliverTelegram(ctx, cfg, event)
	}
	return s.deliverWebhook(ctx, cfg, event)
}

// deliverWebhook 发一段 JSON；任何 2xx 都算成功。
func (s NotifyService) deliverWebhook(ctx context.Context, cfg NotifyConfig, event notifyEvent) error {
	payload := notifyPayload{
		Source:  "blog",
		Event:   event.Event,
		Level:   event.Level,
		Message: event.Message,
		At:      event.At.In(postSiteLocation()).Format(time.RFC3339),
		Details: event.Details,
	}
	if _, err := postJSON(ctx, cfg.WebhookURL, payload); err != nil {
		return err
	}
	return nil
}

// deliverTelegram 调 Bot API；Telegram 出错时也回 200，所以要额外看报文里的 ok。
func (s NotifyService) deliverTelegram(ctx context.Context, cfg NotifyConfig, event notifyEvent) error {
	endpoint := "https://api.telegram.org/bot" + cfg.TelegramBotToken + "/sendMessage"
	payload := map[string]any{
		"chat_id":                  cfg.TelegramChatID,
		"text":                     event.telegramText(),
		"disable_web_page_preview": true,
	}

	body, err := postJSON(ctx, endpoint, payload)
	if err != nil {
		return err
	}

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析 Telegram 响应失败: %w", err)
	}
	if !result.OK {
		// description 是 Telegram 自己的提示（如 "chat not found"），不含本站内部信息。
		return fmt.Errorf("telegram 拒绝投递: %s", truncateRunes(result.Description, 200))
	}
	return nil
}

// postJSON 发一次 JSON 请求并返回响应体（最多 4KB）；非 2xx 返回 notifyHTTPError。
func postJSON(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化告警报文失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造告警请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "zjut-si-blog-notify/1.0")

	resp, err := notifyHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求告警通道失败: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return data, &notifyHTTPError{Status: resp.StatusCode}
	}
	return data, nil
}

// notifyDeliveryHint 把投递失败归成"哪一类原因"；不把底层错误文本回给前端（§7）。
func notifyDeliveryHint(err error) string {
	var statusErr *notifyHTTPError
	switch {
	case errors.As(err, &statusErr):
		return fmt.Sprintf("通道返回 HTTP %d", statusErr.Status)
	case errors.Is(err, context.DeadlineExceeded), os.IsTimeout(err):
		return "请求超时"
	default:
		return "连不上告警通道（检查地址、DNS 与容器出网）"
	}
}

// telegramText 把一条告警写成纯文本消息（不用 Markdown，省掉转义问题）。
func (event notifyEvent) telegramText() string {
	var builder strings.Builder
	builder.WriteString("[zjut.si 博客] ")
	builder.WriteString(event.levelLabel())
	builder.WriteString("\n")
	builder.WriteString(event.Message)

	if len(event.Details) > 0 {
		keys := make([]string, 0, len(event.Details))
		for key := range event.Details {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&builder, "\n· %s: %v", key, event.Details[key])
		}
	}

	builder.WriteString("\n")
	builder.WriteString(event.At.In(postSiteLocation()).Format("2006-01-02 15:04:05"))
	return builder.String()
}

// levelLabel 把 level 写成中文前缀。
func (event notifyEvent) levelLabel() string {
	if event.Level == "info" {
		return "提示"
	}
	return "告警"
}

// certificateExpiry 读 PEM 证书的 NotAfter；certbot 的 fullchain.pem 第一段就是叶子证书。
func certificateExpiry(path string) (time.Time, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return time.Time{}, errors.New("不是 PEM 格式")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}

// state 读取投递状态；键缺失时返回空表。
func (s NotifyService) state() (notifyState, error) {
	state := notifyState{}
	setting, err := model.GetSetting(global.DB, model.SettingNotifyState)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return state, nil
		}
		return state, fmt.Errorf("读取告警状态失败: %w", err)
	}
	if err := json.Unmarshal([]byte(setting.ValueJSON), &state); err != nil {
		return notifyState{}, fmt.Errorf("解析告警状态失败: %w", err)
	}
	return state, nil
}

// save 写入告警配置。
func (s NotifyService) save(cfg NotifyConfig) error {
	buf, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化告警配置失败: %w", err)
	}
	if err := model.PutSetting(global.DB, model.SettingNotify, string(buf), dbtime.Now()); err != nil {
		return fmt.Errorf("写入告警配置失败: %w", err)
	}
	return nil
}

// saveState 写入投递状态。
func (s NotifyService) saveState(state notifyState) error {
	buf, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("序列化告警状态失败: %w", err)
	}
	if err := model.PutSetting(global.DB, model.SettingNotifyState, string(buf), dbtime.Now()); err != nil {
		return fmt.Errorf("写入告警状态失败: %w", err)
	}
	return nil
}

// validateNotifyConfig 校验合并后的完整配置（runbook §20.2）。
func validateNotifyConfig(cfg NotifyConfig) error {
	fields := make([]request.FieldError, 0, 4)

	switch cfg.Channel {
	case NotifyChannelWebhook, NotifyChannelTelegram:
	default:
		fields = append(fields, request.FieldError{Field: "channel", Message: "只能是 webhook 或 telegram"})
	}

	if cfg.WebhookURL != "" {
		if !isHTTPURL(cfg.WebhookURL) {
			fields = append(fields, request.FieldError{Field: "webhookUrl", Message: "必须是 http:// 或 https:// 开头的地址"})
		} else if utf8.RuneCountInString(cfg.WebhookURL) > notifyURLMaxLen {
			fields = append(fields, request.FieldError{Field: "webhookUrl", Message: "长度不能超过 500 个字符"})
		}
	}

	if cfg.DiskThresholdPercent < 1 || cfg.DiskThresholdPercent > 99 {
		fields = append(fields, request.FieldError{Field: "diskThresholdPercent", Message: "必须在 1–99 之间"})
	}
	if cfg.CertExpireDays < 1 || cfg.CertExpireDays > notifyCertDaysMax {
		fields = append(fields, request.FieldError{Field: "certExpireDays", Message: "必须在 1–3650 之间"})
	}

	if cfg.Enabled {
		switch cfg.Channel {
		case NotifyChannelWebhook:
			if cfg.WebhookURL == "" {
				fields = append(fields, request.FieldError{Field: "webhookUrl", Message: "启用告警前必须填 webhook 地址"})
			}
		case NotifyChannelTelegram:
			if cfg.TelegramBotToken == "" {
				fields = append(fields, request.FieldError{Field: "telegramBotToken", Message: "启用告警前必须填 bot token"})
			}
			if cfg.TelegramChatID == "" {
				fields = append(fields, request.FieldError{Field: "telegramChatId", Message: "启用告警前必须填 chatId"})
			}
		}
	}

	if len(fields) > 0 {
		return contentFieldError(fields)
	}
	return nil
}
