package douyin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	zlog "github.com/lk2023060901/danmu-garden-go/pkg/log"
	"go.uber.org/zap"
)

const (
	headerSignature = "X-Douyin-Signature"
	headerTimestamp = "X-Douyin-Timestamp"
	headerNonce     = "X-Douyin-Nonce"
)

// Event 表示抖音推送的一条通用事件。
//
// 实际业务事件可以在 Raw 字段基础上进行二次反序列化。
type Event struct {
	EventType string
	Raw       json.RawMessage
}

// CallbackConfig 描述回调验签配置。
type CallbackConfig struct {
	// SigningSecret 为回调验签使用的密钥，需与开放平台配置保持一致。
	SigningSecret string

	// MaxClockSkew 为允许的时间偏移量，用于防止重放攻击。
	MaxClockSkew time.Duration
}

// Verifier 负责验证抖音回调请求的签名并解析事件。
type Verifier struct {
	cfg    CallbackConfig
	logger *zlog.MLogger
}

// NewVerifier 创建一个新的回调验签器。
func NewVerifier(cfg CallbackConfig, logger *zlog.MLogger) *Verifier {
	if cfg.MaxClockSkew <= 0 {
		cfg.MaxClockSkew = 5 * time.Minute
	}
	if logger == nil {
		logger = &zlog.MLogger{Logger: zlog.L()}
	}
	return &Verifier{
		cfg:    cfg,
		logger: logger,
	}
}

// VerifyAndParse 验证回调请求的签名并解析事件。
//
// 注意：签名算法应以抖音官方文档为准。
// 当前实现采用一种通用的 HMAC-SHA256(timestamp + ":" + body) 方案，
// 仅作为结构示例，后续可根据正式文档进行调整。
func (v *Verifier) VerifyAndParse(
	method string,
	path string,
	headers map[string]string,
	body []byte,
) (*Event, error) {
	if v.cfg.SigningSecret == "" {
		return nil, fmt.Errorf("douyin: callback signing secret is empty")
	}

	sig := headers[headerSignature]
	timestampStr := headers[headerTimestamp]

	if sig == "" || timestampStr == "" {
		return nil, fmt.Errorf("douyin: missing signature or timestamp header")
	}

	ts, err := parseUnixTimestamp(timestampStr)
	if err != nil {
		return nil, fmt.Errorf("douyin: invalid timestamp header: %w", err)
	}

	now := time.Now()
	if now.Sub(ts) > v.cfg.MaxClockSkew || ts.Sub(now) > v.cfg.MaxClockSkew {
		return nil, fmt.Errorf("douyin: callback timestamp out of allowed skew")
	}

	expectedSig := computeHMAC(v.cfg.SigningSecret, timestampStr+":"+string(body))
	if !hmacEqual(expectedSig, sig) {
		return nil, fmt.Errorf("douyin: invalid callback signature")
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("douyin: unmarshal callback body failed: %w", err)
	}

	evtType, _ := raw["event_type"].(string)

	return &Event{
		EventType: evtType,
		Raw:       body,
	}, nil
}

// Handler 是一个可选的 http.Handler 适配器，便于在 net/http 中直接挂载回调入口。
type Handler struct {
	Verifier   *Verifier
	HandleFunc func(ctx context.Context, evt *Event) error
}

// ServeHTTP 实现 http.Handler。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Verifier == nil || h.HandleFunc == nil {
		http.Error(w, "callback handler not configured", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	headers := make(map[string]string, len(r.Header))
	for k, vals := range r.Header {
		if len(vals) == 0 {
			continue
		}
		// 使用首个值即可。
		headers[k] = vals[0]
	}

	evt, err := h.Verifier.VerifyAndParse(r.Method, r.URL.Path, headers, body)
	if err != nil {
		h.Verifier.logger.Warn("douyin callback verify failed", zap.Error(err))
		http.Error(w, "signature verification failed", http.StatusForbidden)
		return
	}

	if err := h.HandleFunc(ctx, evt); err != nil {
		h.Verifier.logger.Error("douyin callback handle failed", zap.Error(err))
		http.Error(w, "handler error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func parseUnixTimestamp(s string) (time.Time, error) {
	// 支持秒级或毫秒级时间戳。
	if len(s) == 0 {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}

	// 简单按长度判断，后续可根据官方文档调整。
	if len(s) <= 10 {
		sec, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(sec, 0), nil
	}

	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, ms*int64(time.Millisecond)), nil
}

func computeHMAC(secret, data string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func hmacEqual(a, b string) bool {
	return hmac.Equal([]byte(strings.ToLower(a)), []byte(strings.ToLower(b)))
}
