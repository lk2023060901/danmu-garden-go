package douyin

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
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

	// LiveMatchUploadRoundUserResultRateLimitPerSecond 表示「上报用户对局数据」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveMatchUploadRoundUserResultRateLimitPerSecond = 100

	// LiveMatchMaxUserResultBatchSize 表示单次上报用户对局数据的最大用户数量。
	// 文档要求单批次最多上报 50 个用户对局数据。
	LiveMatchMaxUserResultBatchSize = 50

	// LiveMatchUploadRoundRankListRateLimitPerSecond 表示「上报对局榜单列表」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveMatchUploadRoundRankListRateLimitPerSecond = 100

	// LiveMatchMaxRoundRankListSize 表示单次上报对局榜单列表的最大长度（Top N 最大 N）。
	// 文档要求最多上传 Top 150。
	LiveMatchMaxRoundRankListSize = 150

	// LiveMatchCompleteUploadUserResultRateLimitPerSecond 表示「完成用户对局数据上报」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveMatchCompleteUploadUserResultRateLimitPerSecond = 100
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

// LiveRoundResult 表示单个用户在本局对局中的整体结果。
//
// 对应文档中的 round_result 枚举：
//
//	0: RoundResultUnknown 状态未知
//	1: RoundResultVictory 胜利
//	2: RoundResultFail    失败
//	3: RoundResultTie     平局
type LiveRoundResult int32

const (
	LiveRoundResultUnknown LiveRoundResult = 0
	LiveRoundResultVictory LiveRoundResult = 1
	LiveRoundResultFail    LiveRoundResult = 2
	LiveRoundResultTie     LiveRoundResult = 3
)

// LiveRoundUserResult 表示参与本局对局的单个用户的战绩数据。
//
// 对应 round/upload_user_result 接口请求体中的 user_list 元素。
type LiveRoundUserResult struct {
	OpenID             string          `json:"open_id"`              // 用户 OpenID
	Rank               int64           `json:"rank"`                 // 用户排名
	RoundResult        LiveRoundResult `json:"round_result"`         // 本局对局结果
	Score              int64           `json:"score"`                // 核心数值，用户排名依据
	WinningPoints      int64           `json:"winning_points"`       // 胜点
	WinningStreakCount int64           `json:"winning_streak_count"` // 连胜次数
}

// LiveRoundRankListItem 表示对局榜单 Top N 列表中的单个用户数据。
//
// 对应 round/upload_rank_list 接口请求体中的 rank_list 元素。
type LiveRoundRankListItem struct {
	OpenID             string          `json:"open_id"`              // 用户 OpenID
	Rank               int64           `json:"rank"`                 // 用户排名
	RoundResult        LiveRoundResult `json:"round_result"`         // 本局对局结果
	Score              int64           `json:"score"`                // 核心数值，用户排名依据
	WinningPoints      int64           `json:"winning_points"`       // 胜点
	WinningStreakCount int64           `json:"winning_streak_count"` // 连胜次数
}

// LiveRoundUploadUserResultRequest 表示「上报用户对局数据」接口的请求体。
//
// 对应文档中的请求参数：
//
//	anchor_open_id : 主播 OpenID
//	app_id         : 玩法 id
//	room_id        : 房间 ID
//	round_id       : 对局 ID
//	user_list      : 参与游戏的观众列表，单批次最多 50 个
type LiveRoundUploadUserResultRequest struct {
	AnchorOpenID string                `json:"anchor_open_id"`
	AppID        string                `json:"app_id"`
	RoomID       string                `json:"room_id"`
	RoundID      int64                 `json:"round_id"`
	UserList     []LiveRoundUserResult `json:"user_list"`
}

// liveRoundUploadUserResultResponse 对应 round/upload_user_result 接口的响应结构。
type liveRoundUploadUserResultResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
}

// LiveRoundUploadRankListRequest 表示「上报对局榜单列表」接口的请求体。
//
// 对应文档中的请求参数：
//
//	anchor_open_id : 主播 OpenID
//	app_id         : 玩法 id
//	room_id        : 房间 ID
//	round_id       : 对局 ID
//	rank_list      : 对局榜单 Top N 列表（最多 150）
type LiveRoundUploadRankListRequest struct {
	AnchorOpenID string                  `json:"anchor_open_id"`
	AppID        string                  `json:"app_id"`
	RoomID       string                  `json:"room_id"`
	RoundID      int64                   `json:"round_id"`
	RankList     []LiveRoundRankListItem `json:"rank_list"`
}

// liveRoundUploadRankListResponse 对应 round/upload_rank_list 接口的响应结构。
type liveRoundUploadRankListResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
}

// LiveRoundCompleteUploadUserResultRequest 表示「完成用户对局数据上报」接口的请求体。
//
// 对应文档中的请求参数：
//
//	anchor_open_id : 主播 OpenID
//	app_id         : 玩法 id
//	complete_time  : 全部用户对局数据上传完成时间
//	room_id        : 房间 ID
//	round_id       : 对局 ID
type LiveRoundCompleteUploadUserResultRequest struct {
	AnchorOpenID string `json:"anchor_open_id"`
	AppID        string `json:"app_id"`
	CompleteTime int64  `json:"complete_time"`
	RoomID       string `json:"room_id"`
	RoundID      int64  `json:"round_id"`
}

// liveRoundCompleteUploadUserResultResponse 对应 round/complete_upload_user_result 接口的响应结构。
type liveRoundCompleteUploadUserResultResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
}

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

// LiveMatchUploadRoundUserResult 上报用户对局数据。
//
// 说明：
//   - 在对局结束后调用，用于分批上报参与本局对局的观众战绩数据；
//   - 单批次最多上报 LiveMatchMaxUserResultBatchSize 个用户（文档为 50），可多次调用完成整局上报；
//   - 底层以 user_id + room_id + round_id 维度存储，重复上报为覆盖写；
//   - 接口限流：在小玩法 app_id 维度，QPS 限制为 LiveMatchUploadRoundUserResultRateLimitPerSecond 次/秒。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/round/upload_user_result
//	Header:
//	  Content-Type: application/json
//	  X-Token      : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的小程序级 token
//	Body:
//	  anchor_open_id, app_id, room_id, round_id, user_list
func (c *Client) LiveMatchUploadRoundUserResult(
	ctx context.Context,
	reqBody *LiveRoundUploadUserResultRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveMatchUploadRoundUserResult request is nil")
	}
	if reqBody.AnchorOpenID == "" || reqBody.AppID == "" || reqBody.RoomID == "" {
		return fmt.Errorf("douyin: LiveMatchUploadRoundUserResult requires non-empty anchor_open_id/app_id/room_id")
	}
	if reqBody.RoundID == 0 {
		return fmt.Errorf("douyin: LiveMatchUploadRoundUserResult requires non-zero round_id")
	}
	if len(reqBody.UserList) == 0 {
		return fmt.Errorf("douyin: LiveMatchUploadRoundUserResult requires non-empty user_list")
	}
	if len(reqBody.UserList) > LiveMatchMaxUserResultBatchSize {
		return fmt.Errorf(
			"douyin: LiveMatchUploadRoundUserResult user_list size must be <= %d",
			LiveMatchMaxUserResultBatchSize,
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
	u, err := base.Parse("/api/gaming_con/round/upload_user_result")
	if err != nil {
		return fmt.Errorf("douyin: build round upload_user_result url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveMatchUploadRoundUserResult request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveMatchUploadRoundUserResult request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveMatchUploadRoundUserResult http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveMatchUploadRoundUserResult read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveMatchUploadRoundUserResult server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveMatchUploadRoundUserResult unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveRoundUploadUserResultResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveMatchUploadRoundUserResult unmarshal response failed: %w", err)
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
	return fmt.Errorf("douyin: LiveMatchUploadRoundUserResult request failed after %d attempts", attempts)
}

// LiveMatchUploadRoundRankList 上报对局榜单列表。
//
// 说明：
//   - 在对局结束后调用，用于一次性上报本局的 Top N 榜单（最多 150 条）；
//   - 同一 room_id + round_id 维度下重复调用会覆盖之前的榜单数据；
//   - 接口限流：在小玩法 app_id 维度，QPS 限制为 LiveMatchUploadRoundRankListRateLimitPerSecond 次/秒。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/round/upload_rank_list
//	Header:
//	  Content-Type: application/json
//	  X-Token      : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的小程序级 token
//	Body:
//	  anchor_open_id, app_id, room_id, round_id, rank_list
func (c *Client) LiveMatchUploadRoundRankList(
	ctx context.Context,
	reqBody *LiveRoundUploadRankListRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveMatchUploadRoundRankList request is nil")
	}
	if reqBody.AnchorOpenID == "" || reqBody.AppID == "" || reqBody.RoomID == "" {
		return fmt.Errorf("douyin: LiveMatchUploadRoundRankList requires non-empty anchor_open_id/app_id/room_id")
	}
	if reqBody.RoundID == 0 {
		return fmt.Errorf("douyin: LiveMatchUploadRoundRankList requires non-zero round_id")
	}
	if len(reqBody.RankList) == 0 {
		return fmt.Errorf("douyin: LiveMatchUploadRoundRankList requires non-empty rank_list")
	}
	if len(reqBody.RankList) > LiveMatchMaxRoundRankListSize {
		return fmt.Errorf(
			"douyin: LiveMatchUploadRoundRankList rank_list size must be <= %d",
			LiveMatchMaxRoundRankListSize,
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
	u, err := base.Parse("/api/gaming_con/round/upload_rank_list")
	if err != nil {
		return fmt.Errorf("douyin: build round upload_rank_list url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveMatchUploadRoundRankList request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveMatchUploadRoundRankList request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveMatchUploadRoundRankList http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveMatchUploadRoundRankList read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveMatchUploadRoundRankList server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveMatchUploadRoundRankList unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveRoundUploadRankListResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveMatchUploadRoundRankList unmarshal response failed: %w", err)
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
	return fmt.Errorf("douyin: LiveMatchUploadRoundRankList request failed after %d attempts", attempts)
}

// LiveMatchCompleteUploadUserResult 完成用户对局数据上报。
//
// 说明：
//   - 在完成本局所有用户对局数据和对局榜单列表上报后调用；
//   - 标记指定 room_id + round_id 对局数据上报已完成，小摇杆的「本局榜」才会展示本局榜单数据；
//   - 若未调用该接口，本轮对局数据在「本局榜」中不展示。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/round/complete_upload_user_result
//	Header:
//	  Content-Type: application/json
//	  X-Token      : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的小程序级 token
//	Body:
//	  anchor_open_id, app_id, complete_time, room_id, round_id
func (c *Client) LiveMatchCompleteUploadUserResult(
	ctx context.Context,
	reqBody *LiveRoundCompleteUploadUserResultRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveMatchCompleteUploadUserResult request is nil")
	}
	if reqBody.AnchorOpenID == "" || reqBody.AppID == "" || reqBody.RoomID == "" {
		return fmt.Errorf("douyin: LiveMatchCompleteUploadUserResult requires non-empty anchor_open_id/app_id/room_id")
	}
	if reqBody.RoundID == 0 {
		return fmt.Errorf("douyin: LiveMatchCompleteUploadUserResult requires non-zero round_id")
	}
	if reqBody.CompleteTime <= 0 {
		return fmt.Errorf("douyin: LiveMatchCompleteUploadUserResult requires positive complete_time")
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
	u, err := base.Parse("/api/gaming_con/round/complete_upload_user_result")
	if err != nil {
		return fmt.Errorf("douyin: build round complete_upload_user_result url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveMatchCompleteUploadUserResult request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveMatchCompleteUploadUserResult request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveMatchCompleteUploadUserResult http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveMatchCompleteUploadUserResult read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveMatchCompleteUploadUserResult server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveMatchCompleteUploadUserResult unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveRoundCompleteUploadUserResultResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveMatchCompleteUploadUserResult unmarshal response failed: %w", err)
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
	return fmt.Errorf("douyin: LiveMatchCompleteUploadUserResult request failed after %d attempts", attempts)
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

// LiveUserGroupQueryRequest 表示「查询观众阵营数据（开发者提供）」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id : 小玩法 app_id
//	open_id: 用户 open_id
//	room_id: 房间 ID
type LiveUserGroupQueryRequest struct {
	AppID  string `json:"app_id"`
	OpenID string `json:"open_id"`
	RoomID string `json:"room_id"`
}

// LiveUserGroupStatusData 表示用户在当前直播间的最新阵营数据。
//
// 对应文档中的 data 字段：
//
//	round_id         : 当前直播间的对局 id
//	round_status     : 当前直播间的对局状态（1=已开始、2=已结束）
//	user_group_status: 用户是否加入阵营（0=未加入、1=已加入）
//	group_id         : 阵营 id（未加入时为空字符串）
type LiveUserGroupStatusData struct {
	RoundID         int64  `json:"round_id"`
	RoundStatus     int    `json:"round_status"`
	UserGroupStatus int    `json:"user_group_status"`
	GroupID         string `json:"group_id"`
}

// LiveUserGroupQueryResponse 表示查询观众阵营数据接口的响应结构。
//
// 注意：该接口无论成功或失败，HTTP 状态码均为 200，错误信息通过 errcode/errmsg 表达。
type LiveUserGroupQueryResponse struct {
	ErrCode int64                    `json:"errcode"`
	ErrMsg  string                   `json:"errmsg"`
	Data    *LiveUserGroupStatusData `json:"data,omitempty"`
}

// LiveUserGroupError 表示业务侧自定义的错误码与错误信息。
//
// 开发者可在查询逻辑中返回该错误，以精确控制 errcode/errmsg。
type LiveUserGroupError struct {
	Code int64
	Msg  string
}

func (e *LiveUserGroupError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("douyin: live user group error, code=%d", e.Code)
}

// LiveUserGroupHandlerFunc 是业务侧处理查询逻辑的回调函数类型。
//
// 实现方需要根据 app_id/open_id/room_id 查询出当前对局和阵营数据，并返回。
type LiveUserGroupHandlerFunc func(ctx context.Context, req *LiveUserGroupQueryRequest) (*LiveUserGroupStatusData, error)

// liveUserGroupHTTPHandler 实现 net/http 的 Handler，用于快速接入查询观众阵营数据接口。
//
// 使用方式示例：
//
//	mux := http.NewServeMux()
//	mux.Handle("/douyin/live/user_group", douyin.NewLiveUserGroupHTTPHandler(secret, handlerFunc))
//
// 其中 secret 为开发配置中配置的签名密钥。
type liveUserGroupHTTPHandler struct {
	secret  string
	handler LiveUserGroupHandlerFunc
}

// NewLiveUserGroupHTTPHandler 创建一个用于处理「查询观众阵营数据」请求的 http.Handler。
//
// 说明：
//   - secret 为开发配置中的签名密钥，用于校验 x-signature；
//   - handler 为业务回调，用于根据请求参数返回用户阵营数据；
//   - 本 Handler 遵循文档要求，所有响应均返回 HTTP 200，错误通过 errcode/errmsg 表达。
func NewLiveUserGroupHTTPHandler(secret string, handler LiveUserGroupHandlerFunc) http.Handler {
	return &liveUserGroupHTTPHandler{
		secret:  secret,
		handler: handler,
	}
}

func (h *liveUserGroupHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// 文档要求使用 POST 方法。
	if r.Method != http.MethodPost {
		writeLiveUserGroupResponse(w, &LiveUserGroupQueryResponse{
			ErrCode: 40001,
			ErrMsg:  "invalid method, must be POST",
		})
		return
	}

	if h.secret == "" || h.handler == nil {
		writeLiveUserGroupResponse(w, &LiveUserGroupQueryResponse{
			ErrCode: 40001,
			ErrMsg:  "handler not configured",
		})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeLiveUserGroupResponse(w, &LiveUserGroupQueryResponse{
			ErrCode: 40001,
			ErrMsg:  "read body failed",
		})
		return
	}
	_ = r.Body.Close()

	// 验证签名。
	if err := VerifyLiveUserGroupSignature(r.Header, body, h.secret); err != nil {
		writeLiveUserGroupResponse(w, &LiveUserGroupQueryResponse{
			ErrCode: 40004,
			ErrMsg:  "signature error",
		})
		return
	}

	// 解析请求体。
	var reqBody LiveUserGroupQueryRequest
	if err := json.Unmarshal(body, &reqBody); err != nil {
		writeLiveUserGroupResponse(w, &LiveUserGroupQueryResponse{
			ErrCode: 40001,
			ErrMsg:  "invalid json body",
		})
		return
	}

	if strings.TrimSpace(reqBody.AppID) == "" ||
		strings.TrimSpace(reqBody.OpenID) == "" ||
		strings.TrimSpace(reqBody.RoomID) == "" {
		writeLiveUserGroupResponse(w, &LiveUserGroupQueryResponse{
			ErrCode: 40001,
			ErrMsg:  "missing required fields",
		})
		return
	}

	// 调用业务侧回调获取阵营数据。
	data, err := h.handler(r.Context(), &reqBody)
	if err != nil {
		if e, ok := err.(*LiveUserGroupError); ok {
			writeLiveUserGroupResponse(w, &LiveUserGroupQueryResponse{
				ErrCode: e.Code,
				ErrMsg:  e.Msg,
			})
			return
		}

		// 未显式指定错误码时，使用一个通用错误。
		writeLiveUserGroupResponse(w, &LiveUserGroupQueryResponse{
			ErrCode: 1,
			ErrMsg:  err.Error(),
		})
		return
	}

	// 成功返回。
	writeLiveUserGroupResponse(w, &LiveUserGroupQueryResponse{
		ErrCode: 0,
		ErrMsg:  "success",
		Data:    data,
	})
}

// writeLiveUserGroupResponse 将响应结构编码为 JSON 并写入 HTTP 响应。
//
// 注意：不修改 HTTP 状态码，保持为 200。
func writeLiveUserGroupResponse(w http.ResponseWriter, resp *LiveUserGroupQueryResponse) {
	if resp == nil {
		resp = &LiveUserGroupQueryResponse{
			ErrCode: 1,
			ErrMsg:  "empty response",
		}
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// LiveUserGroupSelectRequest 表示「观众选择阵营（开发者提供）」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id    : 小玩法 app_id
//	open_id   : 用户 open_id
//	room_id   : 房间 ID
//	group_id  : 用户在前端选择的阵营 ID（意向阵营）
//	avatar_url: 用户头像
//	nickname  : 用户昵称（未加密）
type LiveUserGroupSelectRequest struct {
	AppID     string `json:"app_id"`
	OpenID    string `json:"open_id"`
	RoomID    string `json:"room_id"`
	GroupID   string `json:"group_id"`
	AvatarURL string `json:"avatar_url"`
	Nickname  string `json:"nickname"`
}

// LiveUserGroupSelectData 表示观众选择阵营操作后，用户最终所属阵营及对局信息。
//
// 对应文档中的 data 字段：
//
//	round_id    : 当前直播间的对局 id
//	round_status: 当前直播间的对局状态（1=已开始、2=已结束）
//	group_id    : 用户最终实际加入的阵营 id（未成功加入时为空字符串）
type LiveUserGroupSelectData struct {
	RoundID     int64  `json:"round_id"`
	RoundStatus int    `json:"round_status"`
	GroupID     string `json:"group_id"`
}

// LiveUserGroupSelectResponse 表示「观众选择阵营」接口的响应结构。
//
// 注意：该接口无论成功或失败，HTTP 状态码均为 200，错误信息通过 errcode/errmsg 表达。
type LiveUserGroupSelectResponse struct {
	ErrCode int64                    `json:"errcode"`
	ErrMsg  string                   `json:"errmsg"`
	Data    *LiveUserGroupSelectData `json:"data,omitempty"`
}

// LiveUserGroupSelectError 表示观众选择阵营接口的业务错误。
//
// 开发者可在处理逻辑中返回该错误，以精确控制 errcode/errmsg。
type LiveUserGroupSelectError struct {
	Code int64
	Msg  string
}

func (e *LiveUserGroupSelectError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("douyin: live user group select error, code=%d", e.Code)
}

// LiveUserGroupSelectHandlerFunc 是业务侧处理观众选择阵营逻辑的回调函数类型。
//
// 实现方需要根据 app_id/open_id/room_id/group_id 等信息，决定用户最终加入的阵营，
// 并返回对应的对局信息和最终阵营 ID。
type LiveUserGroupSelectHandlerFunc func(ctx context.Context, req *LiveUserGroupSelectRequest) (*LiveUserGroupSelectData, error)

// liveUserGroupSelectHTTPHandler 实现 net/http 的 Handler，用于快速接入「观众选择阵营」接口。
//
// 使用方式示例：
//
//	mux := http.NewServeMux()
//	mux.Handle("/douyin/live/user_group/select", douyin.NewLiveUserGroupSelectHTTPHandler(secret, handlerFunc))
//
// 其中 secret 为开发配置中配置的签名密钥。
type liveUserGroupSelectHTTPHandler struct {
	secret  string
	handler LiveUserGroupSelectHandlerFunc
}

// NewLiveUserGroupSelectHTTPHandler 创建一个用于处理「观众选择阵营」请求的 http.Handler。
//
// 说明：
//   - secret 为开发配置中的签名密钥，用于校验 x-signature；
//   - handler 为业务回调，用于根据请求参数决定用户最终所属阵营及对局信息；
//   - 本 Handler 遵循文档要求，所有响应均返回 HTTP 200，错误通过 errcode/errmsg 表达。
func NewLiveUserGroupSelectHTTPHandler(secret string, handler LiveUserGroupSelectHandlerFunc) http.Handler {
	return &liveUserGroupSelectHTTPHandler{
		secret:  secret,
		handler: handler,
	}
}

func (h *liveUserGroupSelectHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// 文档要求使用 POST 方法。
	if r.Method != http.MethodPost {
		writeLiveUserGroupSelectResponse(w, &LiveUserGroupSelectResponse{
			ErrCode: 40001,
			ErrMsg:  "invalid method, must be POST",
		})
		return
	}

	if h.secret == "" || h.handler == nil {
		writeLiveUserGroupSelectResponse(w, &LiveUserGroupSelectResponse{
			ErrCode: 40001,
			ErrMsg:  "handler not configured",
		})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeLiveUserGroupSelectResponse(w, &LiveUserGroupSelectResponse{
			ErrCode: 40001,
			ErrMsg:  "read body failed",
		})
		return
	}
	_ = r.Body.Close()

	// 验证签名。
	if err := VerifyLiveUserGroupSignature(r.Header, body, h.secret); err != nil {
		writeLiveUserGroupSelectResponse(w, &LiveUserGroupSelectResponse{
			ErrCode: 40004,
			ErrMsg:  "signature error",
		})
		return
	}

	// 解析请求体。
	var reqBody LiveUserGroupSelectRequest
	if err := json.Unmarshal(body, &reqBody); err != nil {
		writeLiveUserGroupSelectResponse(w, &LiveUserGroupSelectResponse{
			ErrCode: 40001,
			ErrMsg:  "invalid json body",
		})
		return
	}

	if strings.TrimSpace(reqBody.AppID) == "" ||
		strings.TrimSpace(reqBody.OpenID) == "" ||
		strings.TrimSpace(reqBody.RoomID) == "" ||
		strings.TrimSpace(reqBody.GroupID) == "" {
		writeLiveUserGroupSelectResponse(w, &LiveUserGroupSelectResponse{
			ErrCode: 40001,
			ErrMsg:  "missing required fields",
		})
		return
	}

	// 调用业务侧回调，决定用户最终所属阵营及对局信息。
	data, err := h.handler(r.Context(), &reqBody)
	if err != nil {
		if e, ok := err.(*LiveUserGroupSelectError); ok {
			writeLiveUserGroupSelectResponse(w, &LiveUserGroupSelectResponse{
				ErrCode: e.Code,
				ErrMsg:  e.Msg,
			})
			return
		}

		// 未显式指定错误码时，使用一个通用错误。
		writeLiveUserGroupSelectResponse(w, &LiveUserGroupSelectResponse{
			ErrCode: 1,
			ErrMsg:  err.Error(),
		})
		return
	}

	// 成功返回。
	writeLiveUserGroupSelectResponse(w, &LiveUserGroupSelectResponse{
		ErrCode: 0,
		ErrMsg:  "success",
		Data:    data,
	})
}

// writeLiveUserGroupSelectResponse 将响应结构编码为 JSON 并写入 HTTP 响应。
//
// 注意：不修改 HTTP 状态码，保持为 200。
func writeLiveUserGroupSelectResponse(w http.ResponseWriter, resp *LiveUserGroupSelectResponse) {
	if resp == nil {
		resp = &LiveUserGroupSelectResponse{
			ErrCode: 1,
			ErrMsg:  "empty response",
		}
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// ComputeLiveUserGroupSignature 根据文档规则计算 x-signature。
//
// header 参数应包含文档中要求参与签名的字段（x-nonce-str、x-timestamp、x-roomid、x-msg-type），
// 且 key 使用小写形式；bodyStr 为原始请求体字符串；secret 为开发配置中的签名密钥。
//
// 等价于文档中的 Go 示例：
//
//	rawData := urlParams + bodyStr + secret
//	md5Result := md5.Sum([]byte(rawData))
//	return base64.StdEncoding.EncodeToString(md5Result[:])
func ComputeLiveUserGroupSignature(header map[string]string, bodyStr, secret string) string {
	if len(header) == 0 {
		return ""
	}

	keyList := make([]string, 0, len(header))
	for key := range header {
		keyList = append(keyList, key)
	}
	sort.Slice(keyList, func(i, j int) bool {
		return keyList[i] < keyList[j]
	})

	kvList := make([]string, 0, len(keyList))
	for _, key := range keyList {
		kvList = append(kvList, key+"="+header[key])
	}
	urlParams := strings.Join(kvList, "&")
	rawData := urlParams + bodyStr + secret

	sum := md5.Sum([]byte(rawData))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// VerifyLiveUserGroupSignature 按文档规则校验查询观众阵营数据请求的签名。
//
// 说明：
//   - 从 http.Header 中读取 x-nonce-str/x-timestamp/x-roomid/x-msg-type 参与签名计算；
//   - 忽略 x-signature 与 content-type；
//   - body 为原始请求体字节串（UTF-8 编码）。
func VerifyLiveUserGroupSignature(h http.Header, body []byte, secret string) error {
	if secret == "" {
		return fmt.Errorf("douyin: live user group secret is empty")
	}

	signature := strings.TrimSpace(h.Get("x-signature"))
	if signature == "" {
		return fmt.Errorf("douyin: missing x-signature header")
	}

	headerForSign := map[string]string{
		"x-nonce-str": strings.TrimSpace(h.Get("x-nonce-str")),
		"x-timestamp": strings.TrimSpace(h.Get("x-timestamp")),
		"x-roomid":    strings.TrimSpace(h.Get("x-roomid")),
		"x-msg-type":  strings.TrimSpace(h.Get("x-msg-type")),
	}

	for k, v := range headerForSign {
		if v == "" {
			return fmt.Errorf("douyin: missing header %s", k)
		}
	}

	bodyStr := string(body)
	expected := ComputeLiveUserGroupSignature(headerForSign, bodyStr, secret)
	if expected != signature {
		return fmt.Errorf("douyin: invalid live user group signature")
	}
	return nil
}
