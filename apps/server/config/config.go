// Package config 负责加载、补默认值并校验进程配置。
//
// 优先级：环境变量（前缀 BLOG_，层级用下划线，例如 BLOG_DB_PATH）> YAML 文件 > 内置默认值。
// 敏感项（数据库密码等本项目不需要；管理员引导口令）一律只走环境变量，不写入 YAML。
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 是进程的全部配置。
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Web       WebConfig       `mapstructure:"web"`
	DB        DBConfig        `mapstructure:"db"`
	Log       LogConfig       `mapstructure:"log"`
	Auth      AuthConfig      `mapstructure:"auth"`
	Upload    UploadConfig    `mapstructure:"upload"`
	Task      TaskConfig      `mapstructure:"task"`
	Cloudreve CloudreveConfig `mapstructure:"cloudreve"`
}

// ServerConfig 描述两条 listener 的监听与超时参数。
type ServerConfig struct {
	Public                   string   `mapstructure:"public"`
	Admin                    string   `mapstructure:"admin"`
	ReadHeaderTimeoutSeconds int      `mapstructure:"read_header_timeout_seconds"`
	IdleTimeoutSeconds       int      `mapstructure:"idle_timeout_seconds"`
	ShutdownTimeoutSeconds   int      `mapstructure:"shutdown_timeout_seconds"`
	TrustedProxies           []string `mapstructure:"trusted_proxies"`
}

// WebConfig 描述 Next 进程的上游地址。
type WebConfig struct {
	Upstream string `mapstructure:"upstream"`
}

// DBConfig 描述 SQLite 的连接与缓存参数。
type DBConfig struct {
	Path          string `mapstructure:"path"`
	MaxOpenConns  int    `mapstructure:"max_open_conns"`
	CacheSizeKiB  int    `mapstructure:"cache_size_kib"`
	BusyTimeoutMS int    `mapstructure:"busy_timeout_ms"`
	AutoMigrate   bool   `mapstructure:"auto_migrate"`
}

// LogConfig 描述日志级别。
type LogConfig struct {
	Level string `mapstructure:"level"`
}

// AuthConfig 描述会话、CSRF 与登录防护参数。
type AuthConfig struct {
	SessionTTLHours   int    `mapstructure:"session_ttl_hours"`
	CookieName        string `mapstructure:"cookie_name"`
	CookieSecure      bool   `mapstructure:"cookie_secure"`
	LoginPerMinute    int    `mapstructure:"login_per_minute"`
	LockFailures      int    `mapstructure:"lock_failures"`
	LockWindowMinutes int    `mapstructure:"lock_window_minutes"`
	LockMinutes       int    `mapstructure:"lock_minutes"`
}

// UploadConfig 描述上传目录与大小上限。
type UploadConfig struct {
	Dir      string `mapstructure:"dir"`
	MaxBytes int64  `mapstructure:"max_bytes"`
}

// TaskConfig 描述调度器参数。
type TaskConfig struct {
	Enabled         bool `mapstructure:"enabled"`
	IntervalSeconds int  `mapstructure:"interval_seconds"`
}

// CloudreveConfig 描述原图与变体归档的 WebDAV 目标（§10）。
//
// 口令只走环境变量 BLOG_CLOUDREVE_PASSWORD，不写入 config.yaml。
type CloudreveConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	WebDAVURL      string `mapstructure:"webdav_url"`
	Username       string `mapstructure:"username"`
	Password       string `mapstructure:"password"`
	BasePath       string `mapstructure:"base_path"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
	// PublicBaseURL 是图床的匿名直出前缀（如 https://pan.example.com/public/blog）。
	// 为空表示不产出原图直链，正文里的图片也就不会被包上「查看原图」链接。
	PublicBaseURL string `mapstructure:"public_base_url"`
	// ArchiveVariants 决定四档 WebP 变体是否也推上图床（默认 true）。
	ArchiveVariants bool `mapstructure:"archive_variants"`
}

// Timeout 返回 WebDAV 请求超时。
func (c CloudreveConfig) Timeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// ReadHeaderTimeout 返回读请求头的超时。
func (s ServerConfig) ReadHeaderTimeout() time.Duration {
	return time.Duration(s.ReadHeaderTimeoutSeconds) * time.Second
}

// IdleTimeout 返回 keep-alive 空闲超时。
func (s ServerConfig) IdleTimeout() time.Duration {
	return time.Duration(s.IdleTimeoutSeconds) * time.Second
}

// ShutdownTimeout 返回优雅退出等待时间。
func (s ServerConfig) ShutdownTimeout() time.Duration {
	return time.Duration(s.ShutdownTimeoutSeconds) * time.Second
}

// SessionTTL 返回会话有效期。
func (a AuthConfig) SessionTTL() time.Duration {
	return time.Duration(a.SessionTTLHours) * time.Hour
}

// LockWindow 返回登录失败计数窗口。
func (a AuthConfig) LockWindow() time.Duration {
	return time.Duration(a.LockWindowMinutes) * time.Minute
}

// LockDuration 返回账号锁定时长。
func (a AuthConfig) LockDuration() time.Duration {
	return time.Duration(a.LockMinutes) * time.Minute
}

// Load 从 path 指向的 YAML 读取配置；path 为空时按 config.yaml、当前目录顺序探测。
func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(".")
		v.AddConfigPath("apps/server")
	}
	v.SetEnvPrefix("BLOG")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	applyDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return Config{}, fmt.Errorf("读取配置文件失败: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("解析配置失败: %w", err)
	}
	if err := cfg.normalize(); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyDefaults 为每个配置键登记默认值；viper 只有认识该键时才让环境变量生效。
func applyDefaults(v *viper.Viper) {
	v.SetDefault("server.public", ":8640")
	v.SetDefault("server.admin", ":8641")
	v.SetDefault("server.read_header_timeout_seconds", 10)
	v.SetDefault("server.idle_timeout_seconds", 120)
	v.SetDefault("server.shutdown_timeout_seconds", 15)
	v.SetDefault("server.trusted_proxies", []string{"127.0.0.1", "172.16.0.0/12"})

	v.SetDefault("web.upstream", "http://127.0.0.1:3000")

	v.SetDefault("db.path", "./data/blog.db")
	v.SetDefault("db.max_open_conns", 4)
	v.SetDefault("db.cache_size_kib", 16384)
	v.SetDefault("db.busy_timeout_ms", 5000)
	v.SetDefault("db.auto_migrate", true)

	v.SetDefault("log.level", "info")

	v.SetDefault("auth.session_ttl_hours", 168)
	v.SetDefault("auth.cookie_name", "sid")
	v.SetDefault("auth.cookie_secure", false)
	v.SetDefault("auth.login_per_minute", 5)
	v.SetDefault("auth.lock_failures", 5)
	v.SetDefault("auth.lock_window_minutes", 10)
	v.SetDefault("auth.lock_minutes", 30)

	v.SetDefault("upload.dir", "./data/uploads")
	v.SetDefault("upload.max_bytes", int64(30*1024*1024))

	v.SetDefault("task.enabled", true)
	v.SetDefault("task.interval_seconds", 60)

	v.SetDefault("cloudreve.enabled", false)
	v.SetDefault("cloudreve.webdav_url", "")
	v.SetDefault("cloudreve.username", "")
	v.SetDefault("cloudreve.password", "")
	v.SetDefault("cloudreve.base_path", "/blog")
	v.SetDefault("cloudreve.public_base_url", "")
	v.SetDefault("cloudreve.archive_variants", true)
	v.SetDefault("cloudreve.timeout_seconds", 30)
}

// normalize 做无损归一化：去空白、创建目录。
func (c *Config) normalize() error {
	c.Server.Public = strings.TrimSpace(c.Server.Public)
	c.Server.Admin = strings.TrimSpace(c.Server.Admin)
	c.Web.Upstream = strings.TrimSpace(c.Web.Upstream)
	c.DB.Path = strings.TrimSpace(c.DB.Path)
	c.Log.Level = strings.ToLower(strings.TrimSpace(c.Log.Level))
	c.Auth.CookieName = strings.TrimSpace(c.Auth.CookieName)
	c.Upload.Dir = strings.TrimSpace(c.Upload.Dir)
	c.Cloudreve.WebDAVURL = strings.TrimRight(strings.TrimSpace(c.Cloudreve.WebDAVURL), "/")
	c.Cloudreve.Username = strings.TrimSpace(c.Cloudreve.Username)
	c.Cloudreve.BasePath = strings.TrimSpace(c.Cloudreve.BasePath)
	if c.Cloudreve.BasePath == "" {
		c.Cloudreve.BasePath = "/blog"
	}
	if !strings.HasPrefix(c.Cloudreve.BasePath, "/") {
		c.Cloudreve.BasePath = "/" + c.Cloudreve.BasePath
	}
	c.Cloudreve.BasePath = strings.TrimRight(c.Cloudreve.BasePath, "/")
	c.Cloudreve.PublicBaseURL = strings.TrimRight(strings.TrimSpace(c.Cloudreve.PublicBaseURL), "/")

	if c.Upload.Dir != "" {
		if err := os.MkdirAll(c.Upload.Dir, 0o750); err != nil {
			return fmt.Errorf("创建上传目录失败: %w", err)
		}
	}
	if c.DB.Path != "" {
		dir := filepath.Dir(c.DB.Path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return fmt.Errorf("创建数据库目录失败: %w", err)
			}
		}
	}
	return nil
}

// Validate 做启动前的静态校验；不通过即拒绝启动。
func (c Config) Validate() error {
	var problems []string

	for _, item := range []struct{ name, addr string }{
		{"server.public", c.Server.Public},
		{"server.admin", c.Server.Admin},
	} {
		if item.addr == "" {
			problems = append(problems, item.name+" 不能为空")
			continue
		}
		if _, _, err := net.SplitHostPort(item.addr); err != nil {
			problems = append(problems, fmt.Sprintf("%s 不是合法的监听地址 %q: %v", item.name, item.addr, err))
		}
	}
	if c.Server.Public == c.Server.Admin {
		problems = append(problems, "server.public 与 server.admin 不能相同")
	}
	if c.Web.Upstream == "" {
		problems = append(problems, "web.upstream 不能为空")
	}
	if c.DB.Path == "" {
		problems = append(problems, "db.path 不能为空")
	}
	if c.DB.MaxOpenConns < 1 || c.DB.MaxOpenConns > 8 {
		problems = append(problems, "db.max_open_conns 必须在 1–8 之间")
	}
	if c.DB.BusyTimeoutMS < 1000 {
		problems = append(problems, "db.busy_timeout_ms 不得小于 1000")
	}
	if c.Auth.CookieName == "" {
		problems = append(problems, "auth.cookie_name 不能为空")
	}
	if c.Auth.SessionTTLHours < 1 || c.Auth.SessionTTLHours > 24*30 {
		problems = append(problems, "auth.session_ttl_hours 必须在 1–720 之间")
	}
	if c.Auth.LoginPerMinute < 1 {
		problems = append(problems, "auth.login_per_minute 必须 ≥1")
	}
	if c.Auth.LockFailures < 1 {
		problems = append(problems, "auth.lock_failures 必须 ≥1")
	}
	if c.Upload.MaxBytes < 1024 {
		problems = append(problems, "upload.max_bytes 必须 ≥1024")
	}
	if c.Task.IntervalSeconds < 10 {
		problems = append(problems, "task.interval_seconds 必须 ≥10")
	}
	if c.Cloudreve.Enabled {
		if c.Cloudreve.WebDAVURL == "" {
			problems = append(problems, "cloudreve.webdav_url 在启用归档时不能为空")
		}
		if c.Cloudreve.Username == "" {
			problems = append(problems, "cloudreve.username 在启用归档时不能为空")
		}
	}
	if c.Cloudreve.TimeoutSeconds < 5 || c.Cloudreve.TimeoutSeconds > 300 {
		problems = append(problems, "cloudreve.timeout_seconds 必须在 5–300 之间")
	}
	if base := c.Cloudreve.PublicBaseURL; base != "" &&
		!strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		problems = append(problems, "cloudreve.public_base_url 必须以 http:// 或 https:// 开头")
	}

	if len(problems) > 0 {
		return fmt.Errorf("配置校验失败: %s", strings.Join(problems, "；"))
	}
	return nil
}
