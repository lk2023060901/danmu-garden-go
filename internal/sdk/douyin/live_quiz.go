package douyin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// 题库相关接口的协议级硬约束常量。
//
// 说明：
//   - 这些值来自抖音开放平台文档，用于参数校验和注释说明；
//   - 频率限制 LiveAPIRateLimitPerSecond 统一定义在 live.go 中。
const (
	// LiveQuizMaxNum 为单次获取题目数量的上限。
	LiveQuizMaxNum = 100

	// LiveQuizMaxLevel 为题目难度 level 的最大值（1~6）。
	LiveQuizMaxLevel = 6

	// LiveQuizDefaultNum 为未显式指定题目数量时的默认值。
	LiveQuizDefaultNum = 100

	// LiveQuizDefaultType 为未显式指定题目类型时的默认值（1：选择题）。
	LiveQuizDefaultType = 1
)

// LiveQuizQuestion 表示题库中的一道题目。
//
// 对应 quiz/get 接口返回的 data.list 中的单个元素。
type LiveQuizQuestion struct {
	ID          string   `json:"id"`           // 题目 ID
	Title       string   `json:"title"`        // 题干信息
	Options     []string `json:"options"`      // 选项
	AnswerIndex int32    `json:"answer"`       // 答案下标（文档为 Int32）
	ResourceURL string   `json:"resource_url"` // 题目资源（音频、图片等）的资源地址
}

// liveQuizGetResponse 对应题库获取接口的完整响应结构。
type liveQuizGetResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
	Data   struct {
		List []LiveQuizQuestion `json:"list"`
	} `json:"data"`
}

// LiveQuizGet 获取题库题目列表。
//
// 说明：
//   - 使用前需要开通“弹幕题库能力”；
//   - level 取值范围 1~LiveQuizMaxLevel，0 或其它非法值表示随机难度（由平台决定）；
//   - num 最大不超过 LiveQuizMaxNum，<=0 时使用 LiveQuizDefaultNum；
//   - typ 为题目类型：1 选择题，2 判断题，非法值按 LiveQuizDefaultType 处理。
//
// 文档对应：
//
//	GET https://webcast.bytedance.com/api/quiz/get
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Query:
//	  level : 难度（1~6），0 表示随机难度
//	  num   : 题目数量，最高 100，默认 100
//	  type  : 题目类型，1 选择题，2 判断题，默认 1
func (c *Client) LiveQuizGet(
	ctx context.Context,
	level int,
	num int,
	typ int,
) ([]LiveQuizQuestion, error) {
	// level：1~LiveQuizMaxLevel 为合法，<=0 或 >max 则让平台随机。
	if level < 0 {
		return nil, fmt.Errorf("douyin: LiveQuizGet level must be >= 0")
	}
	if level > LiveQuizMaxLevel {
		level = 0
	}

	// num：<=0 使用默认值；>max 则截断到上限。
	if num <= 0 {
		num = LiveQuizDefaultNum
	}
	if num > LiveQuizMaxNum {
		num = LiveQuizMaxNum
	}

	// typ：1 或 2，非法值按默认类型处理（选择题）。
	if typ != 1 && typ != 2 {
		typ = LiveQuizDefaultType
	}

	// 小程序级 access_token，用于填写 x-token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return nil, fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/quiz/get")
	if err != nil {
		return nil, fmt.Errorf("douyin: build quiz-get url failed: %w", err)
	}

	// 构造查询参数。
	q := u.Query()
	if level > 0 {
		q.Set("level", fmt.Sprintf("%d", level))
	}
	q.Set("num", fmt.Sprintf("%d", num))
	q.Set("type", fmt.Sprintf("%d", typ))
	u.RawQuery = q.Encode()

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
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("douyin: build LiveQuizGet request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveQuizGet http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveQuizGet read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveQuizGet server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("douyin: LiveQuizGet unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveQuizGetResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("douyin: LiveQuizGet unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return nil, &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return resp.Data.List, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("douyin: LiveQuizGet request failed after %d attempts", attempts)
}
