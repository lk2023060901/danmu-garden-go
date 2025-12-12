package douyin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 直播数据相关 API 的协议级硬约束常量。
//
// 说明：
//   - 这些值来自抖音开放平台文档，用于参数校验和文档化提示；
//   - 频率限制目前仅用于注释说明，未在客户端强制限流。
const (
	// LiveAPIRateLimitPerSecond 为单个 app_id 在直播数据相关接口上的调用频率上限（次/秒）。
	LiveAPIRateLimitPerSecond = 10

	// LiveFailDataMaxPageSize 为分页查询失败数据接口的单页最大条数。
	LiveFailDataMaxPageSize = 100

	// LiveFansClubMaxUsersPerRequest 为单次查询粉丝团信息接口支持的最大用户数量。
	LiveFansClubMaxUsersPerRequest = 10
)

// LiveMessage 表示一条直播间开放数据服务推送的原始消息。
//
// 设计目标：
//   - 完整承载平台推送的 JSON，不丢失任何字段；
//   - 为常用元信息（roomid/appid/msg_type/seq_id）提供直接访问字段；
//   - 同时保留原始 Data，便于不同玩法按 msg_type 自行反序列化。
//
// 注意：字段名称和含义应与抖音开放平台文档保持一致，如有差异应以文档为准。
type LiveMessage struct {
	// RoomID 为直播间 ID，例如 "7212345678901234567"。
	RoomID string `json:"roomid"`

	// AppID 为玩法接入使用的应用 ID（client_key / app_id）。
	AppID string `json:"appid"`

	// MsgType 为消息类型，用于区分礼物、弹幕、进房等。
	//
	// 具体取值及含义应参考开放平台文档中的 msg_type 枚举。
	// 典型值：live_comment / live_gift / live_like / live_fansclub 等。
	MsgType string `json:"msg_type"`

	// SeqID 为消息序列号，用于检测丢消息与配合 GetPushFailData 做补数。
	SeqID int64 `json:"seq_id"`

	// Data 为业务字段的原始 JSON 片段。
	//
	// 不同 msg_type 对应的结构体可以在玩法侧自行定义，并通过 UnmarshalData 解码。
	Data json.RawMessage `json:"data"`

	// Raw 保存整条消息的原始 JSON，保证不丢任何字段。
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON 自定义反序列化逻辑，以便同时保留 Raw 与字段结构。
func (m *LiveMessage) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		*m = LiveMessage{}
		return nil
	}

	type alias LiveMessage
	var tmp alias
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}

	*m = LiveMessage(tmp)
	m.Raw = append(m.Raw[:0], b...)
	return nil
}

// UnmarshalData 将 Data 字段反序列化到给定结构体。
//
// 典型用法：
//
//	var gift GiftMessageData
//	if err := msg.UnmarshalData(&gift); err != nil { ... }
func (m *LiveMessage) UnmarshalData(v any) error {
	if len(m.Data) == 0 {
		return fmt.Errorf("douyin: live message data is empty")
	}
	return json.Unmarshal(m.Data, v)
}

// LivePushTaskStartResult 表示启动直播间数据推送任务的结果。
//
// 对应文档：
//
//	POST https://webcast.bytedance.com/api/live_data/task/start
type LivePushTaskStartResult struct {
	// TaskID 为本次任务的唯一标识，每次启动任务都会返回新的任务 ID。
	TaskID string `json:"task_id"`
}

// liveTaskStartResponse 对应 start-task 接口的完整响应结构。
type liveTaskStartResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
	Data   struct {
		TaskID string `json:"task_id"`
	} `json:"data"`
}

// liveTaskStopResponse 对应 stop-task 接口的完整响应结构。
type liveTaskStopResponse struct {
	ErrNo  int      `json:"err_no"`
	ErrMsg string   `json:"err_msg"`
	LogID  string   `json:"logid"`
	Data   struct{} `json:"data"`
}

// LivePushTaskStatus 表示数据推送任务的当前状态。
//
// 对应文档中的 status 字段：
//
//	1: NOT_FOUND  任务不存在
//	2: UN_START   任务未启动
//	3: RUNNING    任务运行中
type LivePushTaskStatus struct {
	Status int `json:"status"`
}

const (
	LiveTaskStatusNotFound = 1
	LiveTaskStatusUnStart  = 2
	LiveTaskStatusRunning  = 3
)

// liveTaskGetResponse 对应任务状态查询接口的完整响应结构。
type liveTaskGetResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
	Data   struct {
		Status int `json:"status"`
	} `json:"data"`
}

// liveFailDataResponse 对应分页查询推送失败数据接口的完整响应结构。
type liveFailDataResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
	Data   struct {
		PageNum    int `json:"page_num"`
		TotalCount int `json:"total_count"`
		DataList   []struct {
			RoomID  string `json:"roomid"`
			MsgType string `json:"msg_type"`
			Payload string `json:"payload"`
		} `json:"data_list"`
	} `json:"data"`
}

// liveDataAckResponse 对应互动数据履约上报接口的响应结构。
type liveDataAckResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
}

// LiveDataAckType 表示履约上报类型。
//
// 1: 收到推送；2: 完成处理。
type LiveDataAckType int64

const (
	LiveDataAckTypeReceived LiveDataAckType = 1 // 收到推送
	LiveDataAckTypeHandled  LiveDataAckType = 2 // 完成处理
)

// LiveDataAckItem 描述一条需要履约上报的互动消息。
//
// 对应 data 字段中的单个元素：
//
//	{
//	  "msg_id": "xxxx",
//	  "msg_type": "live_gift",
//	  "client_time": 1705989099973
//	}
type LiveDataAckItem struct {
	MsgID      string `json:"msg_id"`
	MsgType    string `json:"msg_type"`
	ClientTime int64  `json:"client_time"` // 客户端毫秒级时间戳
}

// LiveFansClubInfo 表示单个用户的粉丝团信息。
//
// 对应 fans_club_Info map 中 value 部分：
//
//	{
//	  "level_layer": 1,
//	  "participate_time": 1685432708
//	}
type LiveFansClubInfo struct {
	// LevelLayer 为粉丝团 level 层级。
	LevelLayer int64 `json:"level_layer"`

	// ParticipateTime 为加入粉丝团时间，单位为秒级时间戳。
	ParticipateTime int64 `json:"participate_time"`
}

// liveFansClubInfoResponse 对应粉丝团信息查询接口的响应结构。
type liveFansClubInfoResponse struct {
	ErrNo  int    `json:"err_no"`
	ErrMsg string `json:"err_msg"`
	LogID  string `json:"logid"`
	Data   struct {
		// 注意：字段名为 fans_club_Info（I 大写），与文档示例保持一致。
		FansClubInfo map[string]LiveFansClubInfo `json:"fans_club_Info"`
	} `json:"data"`
}

// LiveWebcastAckConfig 表示直播间 ack 上报配置。
//
// 虽然当前主要用于 SDK 内部或预留字段，但这里仍完整建模以便调用方按需使用。
type LiveWebcastAckConfig struct {
	AckType       int64  `json:"ack_type"`       // 上报类型：1 收到推送；2 渲染成功后上报
	BatchInterval int64  `json:"batch_interval"` // 批次间隔（秒）
	BatchMaxNum   int64  `json:"batch_max_num"`  // 批次最大条目数
	MsgType       string `json:"msg_type"`       // 消息类型
}

// LiveWebcastInfo 表示直播间基础信息。
type LiveWebcastInfo struct {
	RoomID              int64   `json:"room_id"`                // 房间 ID
	AnchorOpenID        string  `json:"anchor_open_id"`         // 主播 OpenID
	AvatarURL           string  `json:"avatar_url"`             // 主播头像
	JoinGameUserOpenID  string  `json:"join_game_user_open_id"` // 当前加入游戏的用户 OpenID
	JoinGameUserRole    int64   `json:"join_game_user_role"`    // 当前加入游戏的用户角色
	NickName            string  `json:"nick_name"`              // 主播昵称
	SupportedGameScenes []int64 `json:"supported_game_scenes"`  // 支持的游戏场景
}

// LiveWebcastLinkerInfo 为连屏数据预留信息，当前业务无需感知，但保留结构以便未来使用。
type LiveWebcastLinkerInfo struct {
	ActivityMasterOpenID string   `json:"activity_master_openid"`
	CurReward            string   `json:"cur_reward"`
	LinkerActivityMaster string   `json:"linker_activity_master"`
	LinkerID             int64    `json:"linker_id"`
	LinkerStatus         int64    `json:"linker_status"`
	MasterStatus         int64    `json:"master_status"`
	RewardInfo           []string `json:"reward_info"`
}

// LiveWebcastUserInfo 表示直播间中参与者的基础信息。
type LiveWebcastUserInfo struct {
	AvatarURL string `json:"avatar_url"`
	NickName  string `json:"nick_name"`
	OpenID    string `json:"open_id"`
}

// LiveWebcastMateInfo 表示 webcastmate/info 接口返回的整体数据结构。
type LiveWebcastMateInfo struct {
	AckCfg     []LiveWebcastAckConfig `json:"ack_cfg"`
	Info       LiveWebcastInfo        `json:"info"`
	LinkerInfo LiveWebcastLinkerInfo  `json:"linker_info"`
	UserInfo   []LiveWebcastUserInfo  `json:"user_info"`
}

// liveWebcastInfoResponse 对应 webcastmate/info 接口的完整响应结构。
type liveWebcastInfoResponse struct {
	ErrNo  int                 `json:"err_no"`
	ErrMsg string              `json:"err_msg"`
	LogID  string              `json:"logid"`
	Data   LiveWebcastMateInfo `json:"data"`
}

// LiveStartPushTask 启动直播间数据推送任务。
//
// 说明：
//   - 不同 msg_type 需要单独启动任务，例如评论、礼物、点赞等各一个任务；
//   - 启动成功后，直播间数据才会同步推送到开发者服务器。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/live_data/task/start
//	Header:
//	  access-token: 通过 https://developer.toutiao.com/api/apps/v2/token 获取
//	  content-type: application/json
//	Body:
//	  appid    : 直播玩法 id
//	  msg_type : live_comment / live_gift / live_like / live_fansclub
//	  roomid   : 直播间 id
func (c *Client) LiveStartPushTask(
	ctx context.Context,
	roomID string,
	appID string,
	msgType string,
) (*LivePushTaskStartResult, error) {
	if roomID == "" || appID == "" || msgType == "" {
		return nil, fmt.Errorf("douyin: LiveStartPushTask requires non-empty roomID/appID/msgType")
	}

	// 小程序级 access_token，用于 Live Data Push 能力。
	token, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return nil, fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/live_data/task/start")
	if err != nil {
		return nil, fmt.Errorf("douyin: build start-task url failed: %w", err)
	}

	bodyObj := struct {
		AppID   string `json:"appid"`
		MsgType string `json:"msg_type"`
		RoomID  string `json:"roomid"`
	}{
		AppID:   appID,
		MsgType: msgType,
		RoomID:  roomID,
	}

	payload, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("douyin: marshal start-task request failed: %w", err)
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
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("douyin: build start-task request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("access-token", token)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: start-task http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: start-task read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: start-task server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("douyin: start-task unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveTaskStartResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("douyin: start-task unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return nil, &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return &LivePushTaskStartResult{
			TaskID: resp.Data.TaskID,
		}, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("douyin: start-task request failed after %d attempts", attempts)
}

// LiveStopPushTask 停止直播间数据推送任务。
//
// 说明：
//   - 停止成功后，平台会先将停止前产生的存量数据推送完成，然后停止推送新的数据。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/live_data/task/stop
//	Header:
//	  access-token: 通过 https://developer.toutiao.com/api/apps/v2/token 获取
//	  content-type: application/json
//	Body:
//	  appid    : 直播玩法 id
//	  msg_type : live_comment / live_gift / live_like / live_fansclub
//	  roomid   : 直播间 id
func (c *Client) LiveStopPushTask(
	ctx context.Context,
	roomID string,
	appID string,
	msgType string,
) error {
	if roomID == "" || appID == "" || msgType == "" {
		return fmt.Errorf("douyin: LiveStopPushTask requires non-empty roomID/appID/msgType")
	}

	token, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/live_data/task/stop")
	if err != nil {
		return fmt.Errorf("douyin: build stop-task url failed: %w", err)
	}

	bodyObj := struct {
		AppID   string `json:"appid"`
		MsgType string `json:"msg_type"`
		RoomID  string `json:"roomid"`
	}{
		AppID:   appID,
		MsgType: msgType,
		RoomID:  roomID,
	}

	payload, err := json.Marshal(bodyObj)
	if err != nil {
		return fmt.Errorf("douyin: marshal stop-task request failed: %w", err)
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
			return fmt.Errorf("douyin: build stop-task request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("access-token", token)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: stop-task http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: stop-task read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: stop-task server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: stop-task unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveTaskStopResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: stop-task unmarshal response failed: %w", err)
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
	return fmt.Errorf("douyin: stop-task request failed after %d attempts", attempts)
}

// LiveGetPushTaskStatus 查询直播间数据推送任务状态。
//
// 说明：
//   - 仅当返回状态为 RUNNING 时，任务才被视为正常运行；
//   - 状态含义：
//     1: NOT_FOUND  任务不存在（可能是主播取消挂载或已关播）；
//     2: UN_START   任务未启动，需要开发者调用启动任务接口；
//     3: RUNNING    任务运行中。
//
// 文档对应：
//
//	GET https://webcast.bytedance.com/api/live_data/task/get
//	Header:
//	  access-token: 通过 https://developer.toutiao.com/api/apps/v2/token 获取
//	  content-type: application/json
//	Query:
//	  appid    : 直播玩法 id
//	  msg_type : live_comment / live_gift / live_like / live_fansclub
//	  roomid   : 直播间 id
func (c *Client) LiveGetPushTaskStatus(
	ctx context.Context,
	roomID string,
	appID string,
	msgType string,
) (*LivePushTaskStatus, error) {
	if roomID == "" || appID == "" || msgType == "" {
		return nil, fmt.Errorf("douyin: LiveGetPushTaskStatus requires non-empty roomID/appID/msgType")
	}

	token, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return nil, fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/live_data/task/get")
	if err != nil {
		return nil, fmt.Errorf("douyin: build get-task url failed: %w", err)
	}

	// 构造查询参数。
	q := u.Query()
	q.Set("appid", appID)
	q.Set("msg_type", msgType)
	q.Set("roomid", roomID)
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
			return nil, fmt.Errorf("douyin: build get-task request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("access-token", token)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: get-task http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: get-task read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: get-task server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("douyin: get-task unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveTaskGetResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("douyin: get-task unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return nil, &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return &LivePushTaskStatus{Status: resp.Data.Status}, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("douyin: get-task request failed after %d attempts", attempts)
}

// LiveGetPushFailData 分页查询推送失败的数据（仅支持礼物、粉丝团）。
//
// 说明：
//   - 补偿数据查询没有 ack 机制，需要开发者自行维护补偿进度（当前补到第几页、下次从第几页开始等）；
//   - 当返回的 data_list 为空时，代表所有数据已成功推送；
//   - 仅支持 msg_type 为 live_gift 或 live_fansclub。
//
// 文档对应：
//
//	GET https://webcast.bytedance.com/api/live_data/task/fail_data/get
//	Header:
//	  access-token: 通过 https://developer.toutiao.com/api/apps/v2/token 获取
//	  content-type: application/json
//	Query:
//	  appid     : 小玩法 id
//	  msg_type  : live_gift / live_fansclub
//	  page_num  : 页码，从 1 开始
//	  page_size : 每页条数，最大不超过 100
//	  roomid    : 直播间 id
func (c *Client) LiveGetPushFailData(
	ctx context.Context,
	roomID string,
	appID string,
	msgType string,
	pageNum int,
	pageSize int,
) (messages []LiveMessage, currentPage int, totalCount int, err error) {
	if roomID == "" || appID == "" || msgType == "" {
		return nil, 0, 0, fmt.Errorf("douyin: LiveGetPushFailData requires non-empty roomID/appID/msgType")
	}
	if pageNum <= 0 {
		return nil, 0, 0, fmt.Errorf("douyin: LiveGetPushFailData requires pageNum >= 1")
	}
	if pageSize <= 0 || pageSize > LiveFailDataMaxPageSize {
		return nil, 0, 0, fmt.Errorf("douyin: LiveGetPushFailData pageSize must be in (0,%d]", LiveFailDataMaxPageSize)
	}

	token, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return nil, 0, 0, err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/live_data/task/fail_data/get")
	if err != nil {
		return nil, 0, 0, fmt.Errorf("douyin: build fail-data url failed: %w", err)
	}

	// 构造查询参数。
	q := u.Query()
	q.Set("appid", appID)
	q.Set("msg_type", msgType)
	q.Set("roomid", roomID)
	q.Set("page_num", fmt.Sprintf("%d", pageNum))
	q.Set("page_size", fmt.Sprintf("%d", pageSize))
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
				return nil, 0, 0, ctx.Err()
			case <-timer.C:
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("douyin: build fail-data request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("access-token", token)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: fail-data http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: fail-data read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: fail-data server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return nil, 0, 0, fmt.Errorf("douyin: fail-data unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveFailDataResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, 0, 0, fmt.Errorf("douyin: fail-data unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return nil, 0, 0, &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		// 将返回的 data_list 转为 []LiveMessage。
		msgs := make([]LiveMessage, 0, len(resp.Data.DataList))
		for _, item := range resp.Data.DataList {
			var data json.RawMessage
			if item.Payload != "" {
				data = json.RawMessage(item.Payload)
			}
			// Raw 中保留单条记录的原始 JSON，便于调试。
			entryRaw, _ := json.Marshal(item)

			msgs = append(msgs, LiveMessage{
				RoomID:  item.RoomID,
				AppID:   appID,
				MsgType: item.MsgType,
				Data:    data,
				Raw:     entryRaw,
			})
		}

		return msgs, resp.Data.PageNum, resp.Data.TotalCount, nil
	}

	if lastErr != nil {
		return nil, 0, 0, lastErr
	}
	return nil, 0, 0, fmt.Errorf("douyin: fail-data request failed after %d attempts", attempts)
}

// LiveDataAck 互动数据履约上报接口。
//
// 说明：
//   - 在收到平台推送数据时，应上报一次 ack（ack_type=1），确认“已收到推送数据”；
//   - 在玩法客户端完成该条数据的处理后，应再次上报（ack_type=2），确认“已完成处理”；
//   - 平台将基于这些上报统计处理情况、超时情况，并在异常时告警。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/live_data/ack
//	Header:
//	  access-token: 通过 https://developer.toutiao.com/api/apps/v2/token 获取
//	  content-type: application/json
//	Body:
//	  ack_type : 1/2
//	  app_id  : 玩法 id
//	  room_id : 直播间 id
//	  data    : 上报数据 JSON 字符串（数组），每个元素为 {msg_id,msg_type,client_time}
func (c *Client) LiveDataAck(
	ctx context.Context,
	ackType LiveDataAckType,
	appID string,
	roomID string,
	items []LiveDataAckItem,
) error {
	if ackType != LiveDataAckTypeReceived && ackType != LiveDataAckTypeHandled {
		return fmt.Errorf("douyin: LiveDataAck invalid ackType=%d", ackType)
	}
	if appID == "" || roomID == "" {
		return fmt.Errorf("douyin: LiveDataAck requires non-empty appID/roomID")
	}
	if len(items) == 0 {
		return fmt.Errorf("douyin: LiveDataAck requires at least one item")
	}

	token, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/live_data/ack")
	if err != nil {
		return fmt.Errorf("douyin: build live_data ack url failed: %w", err)
	}

	// data 字段需要是一个 JSON 字符串，其中内容为数组。
	dataBytes, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveDataAck items failed: %w", err)
	}

	bodyObj := struct {
		AckType int64  `json:"ack_type"`
		AppID   string `json:"app_id"`
		Data    string `json:"data"`
		RoomID  string `json:"room_id"`
	}{
		AckType: int64(ackType),
		AppID:   appID,
		Data:    string(dataBytes),
		RoomID:  roomID,
	}

	payload, err := json.Marshal(bodyObj)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveDataAck request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveDataAck request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("access-token", token)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveDataAck http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveDataAck read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveDataAck server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveDataAck unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveDataAckResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveDataAck unmarshal response failed: %w", err)
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
	return fmt.Errorf("douyin: LiveDataAck request failed after %d attempts", attempts)
}

// LiveGetFansClubInfo 批量获取粉丝团信息。
//
// 说明：
//   - 单次查询的 userOpenIDs 数量不能超过 LiveFansClubMaxUsersPerRequest 个；
//   - 仅在已开通“直播间粉丝团互动数据能力”且已启动粉丝团推送任务的前提下有效。
//
// 文档对应：
//
//	GET https://webcast.bytedance.com/api/live_data/fans_club/get_info
//	Header:
//	  access-token: 通过 https://developer.toutiao.com/api/apps/v2/token 获取
//	  content-type: application/json
//	Query:
//	  anchor_openid : 主播 OpenID
//	  roomid        : 直播间 ID
//	  user_openids  : 需要查询的用户 OpenID 列表（逗号分隔，最多 10 个）
func (c *Client) LiveGetFansClubInfo(
	ctx context.Context,
	anchorOpenID string,
	roomID string,
	userOpenIDs []string,
) (map[string]LiveFansClubInfo, error) {
	if anchorOpenID == "" || roomID == "" {
		return nil, fmt.Errorf("douyin: LiveGetFansClubInfo requires non-empty anchorOpenID/roomID")
	}
	if len(userOpenIDs) == 0 {
		return nil, fmt.Errorf("douyin: LiveGetFansClubInfo requires at least one userOpenID")
	}
	if len(userOpenIDs) > LiveFansClubMaxUsersPerRequest {
		return nil, fmt.Errorf("douyin: LiveGetFansClubInfo supports at most %d userOpenIDs per request", LiveFansClubMaxUsersPerRequest)
	}

	token, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return nil, fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/live_data/fans_club/get_info")
	if err != nil {
		return nil, fmt.Errorf("douyin: build fans-club get_info url failed: %w", err)
	}

	// 构造查询参数。
	q := u.Query()
	q.Set("anchor_openid", anchorOpenID)
	q.Set("roomid", roomID)
	q.Set("user_openids", strings.Join(userOpenIDs, ","))
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
			return nil, fmt.Errorf("douyin: build fans-club get_info request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("access-token", token)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: fans-club get_info http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: fans-club get_info read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: fans-club get_info server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("douyin: fans-club get_info unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveFansClubInfoResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("douyin: fans-club get_info unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return nil, &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return resp.Data.FansClubInfo, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("douyin: fans-club get_info request failed after %d attempts", attempts)
}

// LiveGetWebcastMateInfo 使用主播从直播伴侣/移动端云启动玩法获得的 token，获取直播间信息。
//
// 说明：
//   - 主播挂载玩法后，客户端会拿到一个短期有效的 token（有效期 30 分钟）；
//   - 客户端将该 token 传递给玩法服务端，服务端通过本接口查询直播间相关信息；
//   - 请求头中的 x-token 为通过 /api/apps/v2/token 获取的小程序级 token。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/webcastmate/info
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  token       : 客户端传入的直播 token，30 分钟有效
func (c *Client) LiveGetWebcastMateInfo(
	ctx context.Context,
	tokenFromClient string,
) (*LiveWebcastMateInfo, error) {
	if tokenFromClient == "" {
		return nil, fmt.Errorf("douyin: LiveGetWebcastMateInfo requires non-empty token")
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
	u, err := base.Parse("/api/webcastmate/info")
	if err != nil {
		return nil, fmt.Errorf("douyin: build webcastmate info url failed: %w", err)
	}

	bodyObj := struct {
		Token string `json:"token"`
	}{
		Token: tokenFromClient,
	}

	payload, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("douyin: marshal LiveGetWebcastMateInfo request failed: %w", err)
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
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("douyin: build LiveGetWebcastMateInfo request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveGetWebcastMateInfo http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveGetWebcastMateInfo read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveGetWebcastMateInfo server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("douyin: LiveGetWebcastMateInfo unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveWebcastInfoResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("douyin: LiveGetWebcastMateInfo unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return nil, &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return &resp.Data, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("douyin: LiveGetWebcastMateInfo request failed after %d attempts", attempts)
}
