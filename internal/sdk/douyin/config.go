package douyin

import (
	"time"

	zlog "github.com/lk2023060901/danmu-garden-go/pkg/log"
)

const (
	// 默认网络超时时间等配置，主要用于包装官方 SDK 的 runtime 参数。
	defaultReadTimeoutMS    = 5000 // 与官方 SDK 默认一致
	defaultConnectTimeoutMS = 1000
	defaultMaxAttempts      = 3
)

// Config 描述 Douyin SDK 客户端的基础配置。
//
// 说明：
//   - ClientKey/ClientSecret 为抖音开放平台应用的凭证；
//   - ReadTimeout/ConnectTimeout/MaxAttempts 会下发到官方 SDK 的 runtime 配置（单位：毫秒/次数）；
//   - Logger 仅用于本地封装层的日志记录。
type Config struct {
	ClientKey    string
	ClientSecret string

	ReadTimeout    int // 毫秒
	ConnectTimeout int // 毫秒
	MaxAttempts    int

	// Logger 允许调用方注入自定义日志实例；为空时使用全局日志。
	Logger *zlog.MLogger
}

// Option 为 Config 的可选配置项。
type Option func(*Config)

// WithBaseURL 为保持向后兼容保留，但对官方 SDK 无实际影响。
func WithBaseURL(baseURL string) Option {
	return func(c *Config) {}
}

// WithHTTPTimeout 保留旧接口以兼容调用方，内部映射为 ReadTimeout。
func WithHTTPTimeout(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.ReadTimeout = int(d.Milliseconds())
		}
	}
}

// WithMaxRetries 设置可重试错误的最大重试次数（不含首次调用）。
func WithMaxRetries(n int) Option {
	return func(c *Config) {
		if n >= 0 {
			// 官方 SDK 使用 MaxAttempts 表示总尝试次数。
			c.MaxAttempts = n + 1
		}
	}
}

// WithLogger 注入具名日志实例。
func WithLogger(l *zlog.MLogger) Option {
	return func(c *Config) {
		if l != nil {
			c.Logger = l
		}
	}
}

func (c *Config) fillDefaults() {
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = defaultReadTimeoutMS
	}
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = defaultConnectTimeoutMS
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = defaultMaxAttempts
	}
}
