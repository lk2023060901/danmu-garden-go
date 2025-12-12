package douyin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/alibabacloud-go/tea/tea"
	credential "github.com/bytedance/douyin-openapi-credential-go/client"
	sdkclient "github.com/bytedance/douyin-openapi-sdk-go/client"

	zlog "github.com/lk2023060901/danmu-garden-go/pkg/log"
)

// Client 封装官方 Douyin OpenAPI SDK 客户端，并提供更易用的接口。
//
// 设计目标：
//   - 按官方示例使用 credential.Config + NewCredential + sdk.NewClient；
//   - 将读取超时、连接超时、重试次数等集中由 Config 管理；
//   - 提供便捷的 token 获取方法，避免业务侧直接依赖 credential 包。
type Client struct {
	cfg     Config
	logger  *zlog.MLogger
	credCfg *credential.Config

	cred credential.Credential
	sdk  *sdkclient.Client

	miniHTTP *http.Client
}

// NewClient 创建一个新的 Douyin SDK 客户端。
//
// 调用方至少需要提供 ClientKey 与 ClientSecret，其余字段可留空使用默认值。
func NewClient(cfg Config, opts ...Option) (*Client, error) {
	if cfg.ClientKey == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("douyin: ClientKey and ClientSecret must not be empty")
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	cfg.fillDefaults()

	logger := cfg.Logger
	if logger == nil {
		logger = &zlog.MLogger{Logger: zlog.L()}
	}

	credCfg := (&credential.Config{}).
		SetClientKey(cfg.ClientKey).
		SetClientSecret(cfg.ClientSecret)

	cred, err := credential.NewCredential(credCfg)
	if err != nil {
		return nil, fmt.Errorf("douyin: init credential failed: %w", err)
	}

	sdkCli, err := sdkclient.NewClient(credCfg)
	if err != nil {
		return nil, fmt.Errorf("douyin: init sdk client failed: %w", err)
	}

	// 根据配置调整 SDK 的运行参数。
	if cfg.ReadTimeout > 0 {
		sdkCli.ReadTimeout = tea.Int(cfg.ReadTimeout)
	}
	if cfg.ConnectTimeout > 0 {
		sdkCli.ConnectTimeout = tea.Int(cfg.ConnectTimeout)
	}
	if cfg.MaxAttempts > 0 {
		sdkCli.MaxAttempts = tea.Int(cfg.MaxAttempts)
	}

	miniHTTP := &http.Client{
		Timeout: time.Duration(cfg.ReadTimeout) * time.Millisecond,
	}

	return &Client{
		cfg:      cfg,
		logger:   logger,
		credCfg:  credCfg,
		cred:     cred,
		sdk:      sdkCli,
		miniHTTP: miniHTTP,
	}, nil
}

// GetOpenAPIClientToken 使用官方 credential 实现获取并返回当前可用的 OpenAPI client_token。
//
// 对应文档中的：
//
//	POST https://open.douyin.com/oauth/client_token/
//
// 注意：官方实现内部已做了缓存与过期刷新逻辑。
func (c *Client) GetOpenAPIClientToken(_ context.Context) (string, error) {
	token, err := c.cred.GetClientToken()
	if err != nil {
		return "", err
	}
	if token == nil || token.AccessToken == nil {
		return "", fmt.Errorf("douyin: empty client token returned")
	}
	return *token.AccessToken, nil
}

// GetMiniAppAccessToken 使用官方 credential 实现获取当前小程序服务端 API 的 access_token。
//
// 对应文档中的：
//
//	POST https://developer.toutiao.com/api/apps/v2/token
//
// 注意：
//   - access_token 为小程序级别凭证，适用于小程序支付等 apps API；
//   - 官方实现内部已做了缓存与过期刷新逻辑。
func (c *Client) GetMiniAppAccessToken(_ context.Context) (string, error) {
	token, err := c.cred.GetAccessToken()
	if err != nil {
		return "", err
	}
	if token == nil || token.AccessToken == nil {
		return "", fmt.Errorf("douyin: empty mini-app access token returned")
	}
	return *token.AccessToken, nil
}

// MiniAppPostJSON 调用字节小程序服务端 POST JSON 接口，并带有简单的重试能力。
//
// 约定：
//   - path 为不包含域名的路径，例如 "/api/apps/xxx/yyy"；
//   - reqBody 将被编码为 JSON 并作为请求体发送；
//   - respBody 为可选的响应体解码目标，为 nil 时忽略响应解码；
//   - 自动在 URL 上附加 "?access_token=xxx"，access_token 由 GetMiniAppAccessToken 提供。
//
// 重试策略：
//   - 网络错误或 5xx 响应：按 Config.MaxAttempts 次数进行有限重试，并采用简单退避；
//   - 200 响应但 err_no != 0：视为业务错误，直接返回 *Error，不做重试；
//   - 非 200 且非 5xx：直接返回错误，不做重试。
func (c *Client) MiniAppPostJSON(
	ctx context.Context,
	path string,
	reqBody any,
	respBody any,
) error {
	if path == "" || path[0] != '/' {
		return fmt.Errorf("douyin: mini-app path must start with '/'")
	}

	token, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.MiniAppBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse mini-app base url failed: %w", err)
	}
	u, err := base.Parse(path)
	if err != nil {
		return fmt.Errorf("douyin: build mini-app url failed: %w", err)
	}

	q := u.Query()
	q.Set("access_token", token)
	u.RawQuery = q.Encode()

	var payload []byte
	if reqBody != nil {
		payload, err = json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("douyin: marshal mini-app request failed: %w", err)
		}
	}

	attempts := c.cfg.MaxAttempts
	if attempts <= 0 {
		attempts = defaultMaxAttempts
	}

	var lastErr error

	for i := 0; i < attempts; i++ {
		if i > 0 {
			backoff := time.Duration(i*i) * 100 * time.Millisecond
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("douyin: build mini-app request failed: %w", err)
		}
		if len(payload) > 0 {
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
		}

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: mini-app http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: mini-app read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: mini-app server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx，直接返回。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: mini-app unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		// 尝试解析业务错误码。
		var br struct {
			ErrNo   int    `json:"err_no"`
			ErrTips string `json:"err_tips"`
		}
		if err := json.Unmarshal(body, &br); err == nil && br.ErrNo != 0 {
			return &Error{
				ErrNo:   br.ErrNo,
				ErrTips: br.ErrTips,
				RawBody: body,
			}
		}

		if respBody != nil {
			if err := json.Unmarshal(body, respBody); err != nil {
				return fmt.Errorf("douyin: mini-app unmarshal response failed: %w", err)
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("douyin: mini-app request failed after %d attempts", attempts)
}
