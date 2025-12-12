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
)

// 世界榜单（world_rank）相关 API 的协议级硬约束常量。
const (
	// LiveWorldRankUploadRankListRateLimitPerSecond 表示「上传世界榜单列表数据」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveWorldRankUploadRankListRateLimitPerSecond = 5

	// LiveWorldRankUploadUserResultRateLimitPerSecond 表示「上报用户世界榜单的累计战绩」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveWorldRankUploadUserResultRateLimitPerSecond = 100

	// LiveWorldRankSetValidVersionRateLimitPerSecond 表示「设置当前生效的世界榜单版本」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveWorldRankSetValidVersionRateLimitPerSecond = 3

	// LiveWorldRankMaxRankListSize 表示单次上传世界榜单列表的最大长度（Top N 最大 N）。
	LiveWorldRankMaxRankListSize = 150

	// LiveWorldRankMaxUserResultBatchSize 表示单次上报用户世界榜累计战绩的最大用户数量。
	LiveWorldRankMaxUserResultBatchSize = 50

	// LiveWorldRankCompleteUploadUserResultRateLimitPerSecond 表示「完成用户世界榜单的累计战绩上报」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 文档当前仅通过错误码说明存在频控，具体数值以官方文档为准。
	LiveWorldRankCompleteUploadUserResultRateLimitPerSecond = 1
)

// LiveWorldRankListItem 表示世界榜单中的单个用户数据。
//
// 对应文档中的 rank_list 数组元素：
//
//	open_id              : 用户的 OpenID
//	rank                 : 世界榜单排名
//	score                : 当前世界榜单积分
//	winning_points       : 胜点
//	winning_streak_count : 连胜次数
type LiveWorldRankListItem struct {
	OpenID             string `json:"open_id"`
	Rank               int64  `json:"rank"`
	Score              int64  `json:"score"`
	WinningPoints      int64  `json:"winning_points"`
	WinningStreakCount int64  `json:"winning_streak_count"`
}

// LiveWorldRankUserResultItem 表示用户世界榜累计战绩的一条记录。
//
// 字段含义与 LiveWorldRankListItem 一致，但用于「上报用户世界榜单的累计战绩」接口的 user_list。
type LiveWorldRankUserResultItem struct {
	OpenID             string `json:"open_id"`
	Rank               int64  `json:"rank"`
	Score              int64  `json:"score"`
	WinningPoints      int64  `json:"winning_points"`
	WinningStreakCount int64  `json:"winning_streak_count"`
}

// LiveWorldRankUploadRankListRequest 表示「上传世界榜单列表数据」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id            : 玩法 id
//	is_online_version : 是否是线上版本，false 表示测试数据
//	rank_list         : 世界榜单 Top N 列表（最多 150）
//	world_rank_version: 榜单版本号，若不传则上传到当前生效版本空间
type LiveWorldRankUploadRankListRequest struct {
	AppID            string                  `json:"app_id"`
	IsOnlineVersion  bool                    `json:"is_online_version"`
	RankList         []LiveWorldRankListItem `json:"rank_list"`
	WorldRankVersion string                  `json:"world_rank_version,omitempty"`
}

// liveWorldRankUploadRankListResponse 对应 world_rank/upload_rank_list 接口的响应结构。
//
// 文档示例：
//
//	{
//	  "err_msg": "ok",
//	  "err_no": 0
//	}
type liveWorldRankUploadRankListResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
}

// LiveWorldRankUploadUserResultRequest 表示「上报用户世界榜单的累计战绩」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id            : 玩法 id
//	is_online_version : 是否是线上版本，false 表示测试数据
//	user_list         : 用户信息列表，单次最多 50 个
//	world_rank_version: 榜单版本号，若不传则上传到当前生效版本空间
type LiveWorldRankUploadUserResultRequest struct {
	AppID            string                        `json:"app_id"`
	IsOnlineVersion  bool                          `json:"is_online_version"`
	UserList         []LiveWorldRankUserResultItem `json:"user_list"`
	WorldRankVersion string                        `json:"world_rank_version,omitempty"`
}

// liveWorldRankUploadUserResultResponse 对应 world_rank/upload_user_result 接口的响应结构。
//
// 文档示例：
//
//	{
//	  "err_no": 0,
//	  "err_msg": "ok"
//	}
type liveWorldRankUploadUserResultResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
}

// LiveWorldRankCompleteUploadUserResultRequest 表示「完成用户世界榜单的累计战绩上报」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id            : 玩法 id
//	complete_time     : 上传完成时间（时间戳，单位由文档定义）
//	is_online_version : 是否是线上版本，false 表示测试数据
//	world_rank_version: 榜单版本号，若不传则自动使用当前生效版本空间
type LiveWorldRankCompleteUploadUserResultRequest struct {
	AppID            string `json:"app_id"`
	CompleteTime     int64  `json:"complete_time"`
	IsOnlineVersion  bool   `json:"is_online_version"`
	WorldRankVersion string `json:"world_rank_version,omitempty"`
}

// liveWorldRankCompleteUploadUserResultResponse 对应 world_rank/complete_upload_user_result 接口的响应结构。
//
// 文档示例：
//
//	{
//	  "err_no": 0,
//	  "err_msg": "ok"
//	}
type liveWorldRankCompleteUploadUserResultResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
}

// LiveWorldRankSetValidVersionRequest 表示「设置当前生效的世界榜单版本」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id            : 玩法 id
//	is_online_version : 是否是线上版本，false 表示测试数据
//	world_rank_version: 当前榜单的生效版本（自定义），每个玩法只有一个生效版本
type LiveWorldRankSetValidVersionRequest struct {
	AppID            string `json:"app_id"`
	IsOnlineVersion  bool   `json:"is_online_version"`
	WorldRankVersion string `json:"world_rank_version"`
}

// liveWorldRankSetValidVersionResponse 对应 world_rank/set_valid_version 接口的响应结构。
//
// 文档示例：
//
//	{
//	  "err_no": 0,
//	  "err_msg": "ok"
//	}
type liveWorldRankSetValidVersionResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
}

// LiveWorldRankUploadRankList 调用「上传世界榜单列表数据」接口。
//
// 说明：
//   - 用于设置正式环境/测试环境的世界榜单 Top 150 列表；
//   - 当到达定期更新世界榜单列表的时机时，调用一次，一次性上报最新 Top N（N<=150）；
//   - 同一 world_rank_version 下重复上报会覆盖旧数据；
//   - 接口频率限制：单玩法 app_id 维度 QPS 不超过 LiveWorldRankUploadRankListRateLimitPerSecond。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/world_rank/upload_rank_list
//	Header:
//	  Content-Type: application/json
//	  X-Token      : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的小程序级 token
//	Body:
//	  app_id, is_online_version, rank_list, world_rank_version
func (c *Client) LiveWorldRankUploadRankList(
	ctx context.Context,
	reqBody *LiveWorldRankUploadRankListRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveWorldRankUploadRankList request is nil")
	}
	if reqBody.AppID == "" {
		return fmt.Errorf("douyin: LiveWorldRankUploadRankList requires non-empty app_id")
	}
	if len(reqBody.RankList) == 0 {
		return fmt.Errorf("douyin: LiveWorldRankUploadRankList requires non-empty rank_list")
	}
	if len(reqBody.RankList) > LiveWorldRankMaxRankListSize {
		return fmt.Errorf(
			"douyin: LiveWorldRankUploadRankList rank_list size must be <= %d",
			LiveWorldRankMaxRankListSize,
		)
	}

	// 小程序级 access_token，用于填写 X-Token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/gaming_con/world_rank/upload_rank_list")
	if err != nil {
		return fmt.Errorf("douyin: build world_rank upload_rank_list url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveWorldRankUploadRankList request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveWorldRankUploadRankList request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveWorldRankUploadRankList http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveWorldRankUploadRankList read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveWorldRankUploadRankList server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveWorldRankUploadRankList unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveWorldRankUploadRankListResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveWorldRankUploadRankList unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("douyin: LiveWorldRankUploadRankList request failed after %d attempts", attempts)
}

// LiveWorldRankUploadUserResult 调用「上报用户世界榜单的累计战绩」接口。
//
// 说明：
//   - 用于批量上报参与玩法且有战绩的用户世界榜累计数据；
//   - 单次调用最多上报 LiveWorldRankMaxUserResultBatchSize 个用户（文档为 50）；
//   - 底层以 user_id + world_rank_version 维度存储，重复上报为覆盖写；
//   - 即便已通过 LiveWorldRankUploadRankList 上报了 Top150，本接口仍需上报这些用户的个人战绩。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/world_rank/upload_user_result
//	Header:
//	  Content-Type: application/json
//	  X-Token      : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的小程序级 token
//	Body:
//	  app_id, is_online_version, user_list, world_rank_version
func (c *Client) LiveWorldRankUploadUserResult(
	ctx context.Context,
	reqBody *LiveWorldRankUploadUserResultRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveWorldRankUploadUserResult request is nil")
	}
	if reqBody.AppID == "" {
		return fmt.Errorf("douyin: LiveWorldRankUploadUserResult requires non-empty app_id")
	}
	if len(reqBody.UserList) == 0 {
		return fmt.Errorf("douyin: LiveWorldRankUploadUserResult requires non-empty user_list")
	}
	if len(reqBody.UserList) > LiveWorldRankMaxUserResultBatchSize {
		return fmt.Errorf(
			"douyin: LiveWorldRankUploadUserResult user_list size must be <= %d",
			LiveWorldRankMaxUserResultBatchSize,
		)
	}

	// 小程序级 access_token，用于填写 X-Token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/gaming_con/world_rank/upload_user_result")
	if err != nil {
		return fmt.Errorf("douyin: build world_rank upload_user_result url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveWorldRankUploadUserResult request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveWorldRankUploadUserResult request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveWorldRankUploadUserResult http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveWorldRankUploadUserResult read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveWorldRankUploadUserResult server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveWorldRankUploadUserResult unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveWorldRankUploadUserResultResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveWorldRankUploadUserResult unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("douyin: LiveWorldRankUploadUserResult request failed after %d attempts", attempts)
}

// LiveWorldRankCompleteUploadUserResult 调用「完成用户世界榜单的累计战绩上报」接口。
//
// 说明：
//   - 在到达截榜时间且当前存在生效的 world_rank_version 时调用；
//   - 用于标记指定版本的用户世界榜累计战绩已全部上报完成；
//   - world_rank_version 为空时，由平台按当前生效版本处理。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/world_rank/complete_upload_user_result
//	Header:
//	  Content-Type: application/json
//	  X-Token      : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的小程序级 token
//	Body:
//	  app_id, complete_time, is_online_version, world_rank_version
func (c *Client) LiveWorldRankCompleteUploadUserResult(
	ctx context.Context,
	reqBody *LiveWorldRankCompleteUploadUserResultRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveWorldRankCompleteUploadUserResult request is nil")
	}
	if reqBody.AppID == "" {
		return fmt.Errorf("douyin: LiveWorldRankCompleteUploadUserResult requires non-empty app_id")
	}
	if reqBody.CompleteTime <= 0 {
		return fmt.Errorf("douyin: LiveWorldRankCompleteUploadUserResult requires positive complete_time")
	}

	// 小程序级 access_token，用于填写 X-Token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/gaming_con/world_rank/complete_upload_user_result")
	if err != nil {
		return fmt.Errorf("douyin: build world_rank complete_upload_user_result url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveWorldRankCompleteUploadUserResult request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveWorldRankCompleteUploadUserResult request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveWorldRankCompleteUploadUserResult http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveWorldRankCompleteUploadUserResult read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveWorldRankCompleteUploadUserResult server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveWorldRankCompleteUploadUserResult unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveWorldRankCompleteUploadUserResultResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveWorldRankCompleteUploadUserResult unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("douyin: LiveWorldRankCompleteUploadUserResult request failed after %d attempts", attempts)
}

// LiveWorldRankSetValidVersion 调用「设置当前生效的世界榜单版本」接口。
//
// 说明：
//   - 用于设置当前玩法在正式/测试环境下生效的世界榜单版本；
//   - 同一玩法在任一时刻仅有一个生效的 world_rank_version；
//   - 接口频率限制：单玩法 app_id 维度 QPS 不超过 LiveWorldRankSetValidVersionRateLimitPerSecond。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/world_rank/set_valid_version
//	Header:
//	  Content-Type: application/json
//	  X-Token      : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的小程序级 token
//	Body:
//	  app_id, is_online_version, world_rank_version
func (c *Client) LiveWorldRankSetValidVersion(
	ctx context.Context,
	reqBody *LiveWorldRankSetValidVersionRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveWorldRankSetValidVersion request is nil")
	}
	if reqBody.AppID == "" {
		return fmt.Errorf("douyin: LiveWorldRankSetValidVersion requires non-empty app_id")
	}
	if reqBody.WorldRankVersion == "" {
		return fmt.Errorf("douyin: LiveWorldRankSetValidVersion requires non-empty world_rank_version")
	}

	// 小程序级 access_token，用于填写 X-Token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/gaming_con/world_rank/set_valid_version")
	if err != nil {
		return fmt.Errorf("douyin: build world_rank set_valid_version url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveWorldRankSetValidVersion request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveWorldRankSetValidVersion request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveWorldRankSetValidVersion http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveWorldRankSetValidVersion read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveWorldRankSetValidVersion server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveWorldRankSetValidVersion unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveWorldRankSetValidVersionResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveWorldRankSetValidVersion unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("douyin: LiveWorldRankSetValidVersion request failed after %d attempts", attempts)
}
