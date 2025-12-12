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

// 直播对局（live_match）相关 API 的协议级硬约束常量。
const (
	// LiveMatchRoundSyncRateLimitPerSecond 表示「同步对局状态」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveMatchRoundSyncRateLimitPerSecond = 3

	// LiveMatchUploadUserGroupInfoRateLimitPerSecond 表示「上报阵营数据」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveMatchUploadUserGroupInfoRateLimitPerSecond = 1000
)

// LiveRoundStatus 表示当前房间的游戏对局状态。
//
// 对应文档中的 status 枚举：
//
//	0: NotStart 状态未知/未开始
//	1: Start    本局已开始
//	2: End      本局正常结束
type LiveRoundStatus int32

const (
	LiveRoundStatusNotStart LiveRoundStatus = 0
	LiveRoundStatusStart    LiveRoundStatus = 1
	LiveRoundStatusEnd      LiveRoundStatus = 2
)

// LiveRoundGroupResult 表示单个阵型的对局结果。
//
// 对应 group_result_list 中元素：
//
//	group_id: 阵型 ID
//	result  : 阵型结果（0 未知 / 1 胜利 / 2 失败 / 3 平局）
type LiveRoundGroupResult struct {
	GroupID string `json:"group_id"`
	Result  int32  `json:"result"`
}

const (
	LiveRoundGroupResultUnknown int32 = 0
	LiveRoundGroupResultVictory int32 = 1
	LiveRoundGroupResultFail    int32 = 2
	LiveRoundGroupResultTie     int32 = 3
)

// LiveRoundSyncStatusRequest 表示同步对局状态接口的请求体。
//
// 对应文档中的请求参数：
//
//	anchor_open_id : 主播 OpenID
//	app_id         : 玩法 ID
//	room_id        : 房间 ID
//	round_id       : 对局 ID
//	start_time     : 本局开始时间（秒级时间戳）
//	end_time       : 本局结束时间（秒级时间戳）
//	status         : 当前房间的游戏对局状态（0/1/2）
//	group_result_list: 阵型结果列表
type LiveRoundSyncStatusRequest struct {
	AnchorOpenID    string                 `json:"anchor_open_id"`
	AppID           string                 `json:"app_id"`
	RoomID          string                 `json:"room_id"`
	RoundID         int64                  `json:"round_id"`
	StartTime       int64                  `json:"start_time"`
	EndTime         int64                  `json:"end_time"`
	Status          LiveRoundStatus        `json:"status"`
	GroupResultList []LiveRoundGroupResult `json:"group_result_list"`
}

// liveRoundSyncStatusResponse 对应 round/sync_status 接口的响应结构。
type liveRoundSyncStatusResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
}

// LiveUploadUserGroupInfoRequest 表示上报观众阵营数据接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id  : 玩法 id
//	group_id: 阵营 id（来自开发者平台「用户战绩与排行榜」的 GroupID）
//	open_id : 用户 OpenID
//	room_id : 房间 ID
//	round_id: 对局 ID（开发者自定义）
type LiveUploadUserGroupInfoRequest struct {
	AppID   string `json:"app_id"`
	GroupID string `json:"group_id"`
	OpenID  string `json:"open_id"`
	RoomID  string `json:"room_id"`
	RoundID int64  `json:"round_id"`
}

// liveUploadUserGroupInfoResponse 对应 upload_user_group_info 接口的响应结构。
//
// 文档示例：
//
//	{
//	  "errcode": 0,
//	  "errmsg": ""
//	}
type liveUploadUserGroupInfoResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// LiveMatchSyncRoundStatus 调用「同步对局状态」接口。
//
// 说明：
//   - 对局开始时调用一次（status=Start，start_time 填当前局开始时间，end_time 可为 0 或当前值）；
//   - 对局正常结束时再次调用（status=End，end_time 填结束时间）；
//   - 接口频率限制：单玩法 app_id 每秒最多调用 3 次。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/round/sync_status
//	Header:
//	  Content-Type: application/json
//	  X-Token      : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的小程序级 token
//	Body:
//	  anchor_open_id, app_id, room_id, round_id, start_time, end_time, status, group_result_list
func (c *Client) LiveMatchSyncRoundStatus(
	ctx context.Context,
	reqBody *LiveRoundSyncStatusRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveMatchSyncRoundStatus request is nil")
	}
	if reqBody.AnchorOpenID == "" || reqBody.AppID == "" || reqBody.RoomID == "" {
		return fmt.Errorf("douyin: LiveMatchSyncRoundStatus requires non-empty anchor_open_id/app_id/room_id")
	}
	if reqBody.RoundID == 0 {
		return fmt.Errorf("douyin: LiveMatchSyncRoundStatus requires non-zero round_id")
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
	u, err := base.Parse("/api/gaming_con/round/sync_status")
	if err != nil {
		return fmt.Errorf("douyin: build round sync_status url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveMatchSyncRoundStatus request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveMatchSyncRoundStatus request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveMatchSyncRoundStatus http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveMatchSyncRoundStatus read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveMatchSyncRoundStatus server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveMatchSyncRoundStatus unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveRoundSyncStatusResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveMatchSyncRoundStatus unmarshal response failed: %w", err)
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
	return fmt.Errorf("douyin: LiveMatchSyncRoundStatus request failed after %d attempts", attempts)
}

// LiveMatchUploadUserGroupInfo 上报观众阵营数据。
//
// 说明：
//   - 当观众通过评论、点赞、送礼等方式加入玩法时，应调用该接口上报其所属阵营；
//   - 限流：在小玩法 app_id 维度，频率限制为 LiveMatchUploadUserGroupInfoRateLimitPerSecond 次/秒；
//   - 接口仅上报当前观众的阵营归属，不负责推送数据。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/round/upload_user_group_info
//	Header:
//	  Content-Type: application/json
//	  X-Token      : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的小程序级 token
//	Body:
//	  app_id, group_id, open_id, room_id, round_id
func (c *Client) LiveMatchUploadUserGroupInfo(
	ctx context.Context,
	reqBody *LiveUploadUserGroupInfoRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveMatchUploadUserGroupInfo request is nil")
	}
	if reqBody.AppID == "" || reqBody.GroupID == "" || reqBody.OpenID == "" || reqBody.RoomID == "" {
		return fmt.Errorf("douyin: LiveMatchUploadUserGroupInfo requires non-empty app_id/group_id/open_id/room_id")
	}
	if reqBody.RoundID == 0 {
		return fmt.Errorf("douyin: LiveMatchUploadUserGroupInfo requires non-zero round_id")
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
	u, err := base.Parse("/api/gaming_con/round/upload_user_group_info")
	if err != nil {
		return fmt.Errorf("douyin: build upload_user_group_info url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveMatchUploadUserGroupInfo request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveMatchUploadUserGroupInfo request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveMatchUploadUserGroupInfo http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveMatchUploadUserGroupInfo read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveMatchUploadUserGroupInfo server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveMatchUploadUserGroupInfo unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveUploadUserGroupInfoResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveMatchUploadUserGroupInfo unmarshal response failed: %w", err)
		}
		if resp.ErrCode != 0 {
			return &Error{
				ErrNo:   resp.ErrCode,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("douyin: LiveMatchUploadUserGroupInfo request failed after %d attempts", attempts)
}
