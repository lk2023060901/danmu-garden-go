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

// 观众一键同玩 / 连麦玩法相关接口的协议级硬约束常量。
const (
	// LiveLinkmicQueryRateLimitPerSecond 表示「查询直播间麦位信息」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveLinkmicQueryRateLimitPerSecond = 100

	// LiveAudienceJoinGameRateLimitPerSecond 表示「嘉宾云启动玩法」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveAudienceJoinGameRateLimitPerSecond = 100

	// LiveAudienceLeaveGameRateLimitPerSecond 表示「嘉宾关闭玩法」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveAudienceLeaveGameRateLimitPerSecond = 100

	// LiveCoGameUploadUserDataRateLimitPerSecond 表示「上报连麦对局内用户积分」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveCoGameUploadUserDataRateLimitPerSecond = 200

	// LiveAnchorLinkmicPrepareRateLimitPerSecond 表示「主播连线前置准备」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveAnchorLinkmicPrepareRateLimitPerSecond = 200

	// LiveAnchorLinkmicCreateRateLimitPerSecond 表示「创建主播连线」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveAnchorLinkmicCreateRateLimitPerSecond = 200

	// LiveAnchorLinkmicReleaseRateLimitPerSecond 表示「结束主播连线」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveAnchorLinkmicReleaseRateLimitPerSecond = 100

	// LiveAnchorLinkmicQueryRateLimitPerSecond 表示「查询主播连麦信息」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveAnchorLinkmicQueryRateLimitPerSecond = 200

	// LiveAnchorLinkmicAnchorLeaveRateLimitPerSecond 表示「中途单主播离线」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveAnchorLinkmicAnchorLeaveRateLimitPerSecond = 200

	// LiveAnchorLinkmicUpdateVoiceRateLimitPerSecond 表示「上报附近语音用户音量」接口在单玩法 app_id 维度的频率上限（次/秒）。
	// 该值来自抖音开放平台文档，目前仅作为注释说明使用。
	LiveAnchorLinkmicUpdateVoiceRateLimitPerSecond = 200
)

// LiveLinkmicBaseInfo 表示直播间麦位的基础信息。
//
// 对应 linkmic/query 接口响应中的 base_info 字段。
type LiveLinkmicBaseInfo struct {
	TotalCount int64  `json:"total_count"` // 总麦位数
	FreeCount  int64  `json:"free_count"`  // 空缺麦位数
	LinkID     string `json:"link_id"`     // 连麦唯一标识
}

// LiveLinkmicUserAppInfo 表示麦上用户在宿主 App 中的玩法能力信息。
//
// 对应 user_list.app_info 字段。
type LiveLinkmicUserAppInfo struct {
	// HostAppStartAppAvailable 表示宿主 APP 是否可启动玩法。
	HostAppStartAppAvailable bool `json:"host_app_start_app_available"`
}

// LiveLinkmicUser 表示麦上单个用户的信息。
//
// 对应 linkmic/query 接口响应中的 user_list 元素。
type LiveLinkmicUser struct {
	AppInfo           LiveLinkmicUserAppInfo `json:"app_info"`
	AvatarURL         string                 `json:"avatar_url"`         // 头像
	CameraState       int64                  `json:"camera_state"`       // 摄像头状态：1 打开；2 关闭（仅对已在麦上的用户有效）
	DisableCamera     int64                  `json:"disable_camera"`     // 是否禁用摄像头：1 未禁用；2 已禁用
	DisableMicrophone int64                  `json:"disable_microphone"` // 是否禁用麦克风：1 未禁用；2 已禁用
	LinkPosition      int64                  `json:"link_position"`      // 麦位位置
	LinkState         int64                  `json:"link_state"`         // 连麦状态：1 已上麦；2 邀请中；3 申请中
	MicrophoneState   int64                  `json:"microphone_state"`   // 麦克风状态：1 打开；2 关闭（仅对已在麦上的用户有效）
	NickName          string                 `json:"nick_name"`          // 昵称
	OpenID            string                 `json:"open_id"`            // 用户 OpenID
	SecAvatarURL      string                 `json:"sec_avatar_url"`     // 加密头像
	SecNickName       string                 `json:"sec_nick_name"`      // 加密昵称
}

// LiveLinkmicInfo 表示查询直播间麦位信息接口返回的整体数据。
type LiveLinkmicInfo struct {
	BaseInfo LiveLinkmicBaseInfo `json:"base_info"`
	UserList []LiveLinkmicUser   `json:"user_list"`
}

// liveLinkmicQueryResponse 对应 linkmic/query 接口的完整响应结构。
//
// 文档示例仅展示了 base_info/user_list 字段；绝大多数 Webcast 接口还会包含 err_no/err_msg/logid 字段，
// 因此这里一并建模，若实际响应中未包含则保持默认零值。
type liveLinkmicQueryResponse struct {
	ErrNo    int                 `json:"err_no"`
	ErrMsg   string              `json:"err_msg"`
	LogID    string              `json:"logid"`
	Base     LiveLinkmicBaseInfo `json:"base_info"`
	UserList []LiveLinkmicUser   `json:"user_list"`
}

// LiveLinkmicQuery 查询直播间麦位信息。
//
// 说明：
//   - 玩法过程中，平台不会主动推送麦位变化，需由玩法服务端定时轮询本接口；
//   - 接口返回当前房间的麦位基础信息以及麦上用户列表，可用于驱动一键同玩/嘉宾玩法的动态调整；
//   - 接口频率限制：在小玩法 app_id 维度，QPS 不超过 LiveLinkmicQueryRateLimitPerSecond 次/秒。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/linkmic/query
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  app_id      : 开发者应用 ID
//	  room_id     : 直播间 ID
func (c *Client) LiveLinkmicQuery(
	ctx context.Context,
	appID string,
	roomID string,
) (*LiveLinkmicInfo, error) {
	if appID == "" || roomID == "" {
		return nil, fmt.Errorf("douyin: LiveLinkmicQuery requires non-empty appID/roomID")
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
	u, err := base.Parse("/api/linkmic/query")
	if err != nil {
		return nil, fmt.Errorf("douyin: build linkmic query url failed: %w", err)
	}

	bodyObj := struct {
		AppID  string `json:"app_id"`
		RoomID string `json:"room_id"`
	}{
		AppID:  appID,
		RoomID: roomID,
	}

	payload, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("douyin: marshal LiveLinkmicQuery request failed: %w", err)
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
			return nil, fmt.Errorf("douyin: build LiveLinkmicQuery request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveLinkmicQuery http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveLinkmicQuery read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveLinkmicQuery server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("douyin: LiveLinkmicQuery unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveLinkmicQueryResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("douyin: LiveLinkmicQuery unmarshal response failed: %w", err)
		}
		if resp.ErrNo != 0 {
			return nil, &Error{
				ErrNo:   resp.ErrNo,
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		info := &LiveLinkmicInfo{
			BaseInfo: resp.Base,
			UserList: resp.UserList,
		}
		return info, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("douyin: LiveLinkmicQuery request failed after %d attempts", attempts)
}

// LiveAudienceJoinGameRequest 表示「嘉宾云启动玩法」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id : 小玩法 app_id
//	open_id: 嘉宾用户 open_id
//	room_id: 房间 ID
type LiveAudienceJoinGameRequest struct {
	AppID  string `json:"app_id"`
	OpenID string `json:"open_id"`
	RoomID int64  `json:"room_id"`
}

// liveAudienceJoinGameResponse 对应 audience/join_game 接口的响应结构。
//
// 文档当前仅展示了 data 字段，实际返回通常还会包含 err_no/err_msg/logid 等通用字段。
type liveAudienceJoinGameResponse struct {
	ErrNo  int             `json:"err_no"`
	ErrMsg string          `json:"err_msg"`
	LogID  string          `json:"logid"`
	Data   json.RawMessage `json:"data"`
}

// LiveAudienceLeaveGameRequest 表示「嘉宾关闭玩法」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id : 小玩法 app_id
//	open_id: 嘉宾用户 open_id
//	room_id: 房间 ID
type LiveAudienceLeaveGameRequest struct {
	AppID  string `json:"app_id"`
	OpenID string `json:"open_id"`
	RoomID int64  `json:"room_id"`
}

// liveAudienceLeaveGameResponse 对应 audience/leave_game 接口的响应结构。
//
// 文档当前仅展示了 data 字段，实际返回通常还会包含 err_no/err_msg/logid 等通用字段。
type liveAudienceLeaveGameResponse struct {
	ErrNo  int             `json:"err_no"`
	ErrMsg string          `json:"err_msg"`
	LogID  string          `json:"logid"`
	Data   json.RawMessage `json:"data"`
}

// LiveCoGameUserScore 表示连麦对局内单个用户的积分数据。
//
// 对应 co_game_upload_user_data 接口请求体中 anchor_infos.user_list 元素。
type LiveCoGameUserScore struct {
	OpenID string `json:"open_id"`
	Score  int64  `json:"score"`
}

// LiveCoGameRoundStatus 表示连麦对局状态。
//
// 对应文档中的 round_status 枚举：
//
//	1: 开始；2: 结束；3: 进行中
type LiveCoGameRoundStatus int64

const (
	LiveCoGameRoundStatusStart   LiveCoGameRoundStatus = 1
	LiveCoGameRoundStatusEnd     LiveCoGameRoundStatus = 2
	LiveCoGameRoundStatusRunning LiveCoGameRoundStatus = 3
)

// LiveCoGameAnchorInfo 描述与玩法对局关联的直播房间信息。
//
// 对应 co_game_upload_user_data 接口请求体中的 anchor_infos 元素。
type LiveCoGameAnchorInfo struct {
	AnchorOpenID string                `json:"anchor_open_id"` // 主播 open_id
	RoomID       string                `json:"room_id"`        // 房间 room_id
	AppID        string                `json:"app_id"`         // 小玩法 app_id
	RoundID      int64                 `json:"round_id"`       // 一轮游戏的唯一标识
	RoundStatus  LiveCoGameRoundStatus `json:"round_status"`   // 对局状态：1 开始；2 结束；3 进行中
	UserList     []LiveCoGameUserScore `json:"user_list"`      // 参与游戏的用户列表
}

// LiveCoGameUploadUserDataRequest 表示「上报连麦对局内用户积分」接口的请求体。
//
// 对应文档中的请求参数：
//
//	anchor_infos: 对局关联的直播房间列表，目前仅支持传入 1 个。
type LiveCoGameUploadUserDataRequest struct {
	AnchorInfos []LiveCoGameAnchorInfo `json:"anchor_infos"`
}

// LiveCoGameUploadFailRoom 描述 co_game_upload_user_data 接口中单个失败房间的信息。
type LiveCoGameUploadFailRoom struct {
	ErrCode   int64  `json:"errcode"`
	ErrMsg    string `json:"errmsg"`
	RoomIDStr string `json:"room_id_str"`
	RoomID    int64  `json:"room_id"`
}

// liveCoGameUploadUserDataResponse 对应 co_game_upload_user_data 接口的响应结构。
type liveCoGameUploadUserDataResponse struct {
	ErrCode      int64                      `json:"err_code"`
	ErrMsg       string                     `json:"err_msg"`
	FailRoomList []LiveCoGameUploadFailRoom `json:"fail_room_list"`
}

// LiveAnchorLinkmicPrepareRequest 表示「主播连线前置准备」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id     : 玩法 APPID
//	room_id    : 房间 ID
//	room_id_str: 房间 ID 字符串（可选）
type LiveAnchorLinkmicPrepareRequest struct {
	AppID     string `json:"app_id"`
	RoomID    int64  `json:"room_id"`
	RoomIDStr string `json:"room_id_str,omitempty"`
}

// liveAnchorLinkmicPrepareResponse 对应 anchor_linkmic/prepare 接口的响应结构。
type liveAnchorLinkmicPrepareResponse struct {
	ErrCode int64           `json:"errcode"`
	ErrMsg  string          `json:"errmsg"`
	Data    json.RawMessage `json:"data"`
}

// LiveAnchorLinkmicCreateRoom 描述创建主播连线时单个加入房间的信息。
//
// 对应 anchor_linkmic/create 接口请求体中的 rooms 元素。
type LiveAnchorLinkmicCreateRoom struct {
	RoomID          int64    `json:"room_id"`                     // 房间 ID
	RoomIDStr       string   `json:"room_id_str,omitempty"`       // 房间 ID 字符串
	AudienceOpenIDs []string `json:"audience_open_ids,omitempty"` // 进入连线的嘉宾 openID 列表
}

// LiveAnchorLinkmicCreateRequest 表示「创建主播连线」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id               : 玩法 ID
//	max_link_num_per_room: 单直播间最大连线人数（可选，不填则不限制）
//	room_id              : 发起开启连线的房间 ID
//	room_id_str          : 发起房间的字符串 ID（可选）
//	rooms                : 要加入主主连线的直播间列表
type LiveAnchorLinkmicCreateRequest struct {
	AppID             string                        `json:"app_id"`
	MaxLinkNumPerRoom int64                         `json:"max_link_num_per_room,omitempty"`
	RoomID            int64                         `json:"room_id"`
	RoomIDStr         string                        `json:"room_id_str,omitempty"`
	Rooms             []LiveAnchorLinkmicCreateRoom `json:"rooms"`
}

// LiveAnchorLinkmicCreateFilterAudience 描述被过滤的嘉宾信息。
type LiveAnchorLinkmicCreateFilterAudience struct {
	OpenID       string `json:"open_id"`
	FilterReason int64  `json:"filterReason"`
}

// LiveAnchorLinkmicCreateFilterRoom 描述被过滤的房间信息。
type LiveAnchorLinkmicCreateFilterRoom struct {
	IsFilter           bool                                    `json:"is_filter"`
	FilterReason       int64                                   `json:"filterReason"`
	RoomID             int64                                   `json:"room_id"`
	RoomIDStr          string                                  `json:"room_id_str"`
	FilterAudienceList []LiveAnchorLinkmicCreateFilterAudience `json:"filter_audience_list"`
}

// LiveAnchorLinkmicCreateData 表示创建主播连线成功后的返回数据。
type LiveAnchorLinkmicCreateData struct {
	LinkIDStr               string                              `json:"link_id_str"`
	LinkID                  int64                               `json:"link_id"`
	FilterRoomList          []LiveAnchorLinkmicCreateFilterRoom `json:"filter_room_list"`
	JustFilterAudienceRooms []LiveAnchorLinkmicCreateFilterRoom `json:"just_filter_audience_room_list"`
}

// liveAnchorLinkmicCreateResponse 对应 anchor_linkmic/create 接口的响应结构。
type liveAnchorLinkmicCreateResponse struct {
	ErrCode int64                        `json:"errcode"`
	ErrMsg  string                       `json:"errmsg"`
	Data    *LiveAnchorLinkmicCreateData `json:"data"`
}

// LiveAnchorLinkmicReleaseRequest 表示「结束主播连线」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id : 玩法 ID
//	link_id: 需要结束的连线 ID
//	room_id: 发起结束整场连线的房间 ID（任意连线主播的房间）
type LiveAnchorLinkmicReleaseRequest struct {
	AppID  string `json:"app_id"`
	LinkID int64  `json:"link_id"`
	RoomID int64  `json:"room_id"`
}

// liveAnchorLinkmicReleaseResponse 对应 anchor_linkmic/release 接口的响应结构。
type liveAnchorLinkmicReleaseResponse struct {
	ErrCode int64           `json:"errcode"`
	ErrMsg  string          `json:"errmsg"`
	Data    json.RawMessage `json:"data"`
}

// LiveAnchorLinkmicQueryRequest 表示「查询主播连麦信息」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id : 玩法 APPID
//	link_id: 主主连线 ID
//	room_id: 发起查询的房间 ID
type LiveAnchorLinkmicQueryRequest struct {
	AppID  string `json:"app_id"`
	LinkID int64  `json:"link_id"`
	RoomID int64  `json:"room_id"`
}

// LiveAnchorLinkmicRoomAnchor 表示连线中单个房间的主播信息。
type LiveAnchorLinkmicRoomAnchor struct {
	AvatarURL string `json:"avatar_url"`
	NickName  string `json:"nick_name"`
	OpenID    string `json:"open_id"`
}

// LiveAnchorLinkmicRoomAudience 表示连线中单个房间的已连线嘉宾信息。
type LiveAnchorLinkmicRoomAudience struct {
	AvatarURL string `json:"avatar_url"`
	NickName  string `json:"nick_name"`
	OpenID    string `json:"open_id"`
}

// LiveAnchorLinkmicRoomInfo 表示查询主播连麦信息接口中单个房间的连线信息。
type LiveAnchorLinkmicRoomInfo struct {
	RoomID             int64                           `json:"room_id"`
	RoomIDStr          string                          `json:"room_id_str"`
	Anchor             LiveAnchorLinkmicRoomAnchor     `json:"anchor"`
	LinkedAudienceList []LiveAnchorLinkmicRoomAudience `json:"linked_audience_list"`
}

// LiveAnchorLinkmicQueryData 表示查询主播连麦信息接口的返回数据。
type LiveAnchorLinkmicQueryData struct {
	Rooms []LiveAnchorLinkmicRoomInfo `json:"rooms"`
}

// liveAnchorLinkmicQueryResponse 对应 anchor_linkmic/query 接口的响应结构。
type liveAnchorLinkmicQueryResponse struct {
	ErrCode int64                       `json:"errcode"`
	ErrMsg  string                      `json:"errmsg"`
	Data    *LiveAnchorLinkmicQueryData `json:"data"`
}

// LiveAnchorLinkmicAnchorLeaveRequest 表示「中途单主播离线」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id : 玩法 ID
//	link_id: 要离开的连线 ID
//	room_id: 离开连线的房间 ID
type LiveAnchorLinkmicAnchorLeaveRequest struct {
	AppID  string `json:"app_id"`
	LinkID int64  `json:"link_id"`
	RoomID int64  `json:"room_id"`
}

// liveAnchorLinkmicAnchorLeaveResponse 对应 anchor_linkmic/anchor_leave 接口的响应结构。
type liveAnchorLinkmicAnchorLeaveResponse struct {
	ErrCode int64           `json:"errcode"`
	ErrMsg  string          `json:"errmsg"`
	Data    json.RawMessage `json:"data"`
}

// LiveAnchorLinkmicUpdateVoiceNeighbor 表示主态用户可听见的某个附近用户及其音量。
//
// 对应 update_voice 接口请求体中 user_infos.voice_infos 元素。
type LiveAnchorLinkmicUpdateVoiceNeighbor struct {
	NeighborOpenID string `json:"neighbor_open_id"`
	Voice          int64  `json:"voice"`
}

// LiveAnchorLinkmicUpdateVoiceUserInfo 表示一个主态用户及其可听见的附近用户音量配置。
//
// 对应 update_voice 接口请求体中 user_infos 元素。
type LiveAnchorLinkmicUpdateVoiceUserInfo struct {
	OpenID     string                                 `json:"open_id"`
	VoiceInfos []LiveAnchorLinkmicUpdateVoiceNeighbor `json:"voice_infos"`
}

// LiveAnchorLinkmicUpdateVoiceRequest 表示「上报附近语音用户音量」接口的请求体。
//
// 对应文档中的请求参数：
//
//	app_id    : 玩法 ID
//	link_id   : 连线 ID
//	link_id_str: 连线 ID 字符串（可选，当整型有精度丢失时使用）
//	user_infos: 主态用户及其可听见的附近用户音量列表
type LiveAnchorLinkmicUpdateVoiceRequest struct {
	AppID     string                                 `json:"app_id"`
	LinkID    int64                                  `json:"link_id"`
	LinkIDStr string                                 `json:"link_id_str,omitempty"`
	UserInfos []LiveAnchorLinkmicUpdateVoiceUserInfo `json:"user_infos"`
}

// liveAnchorLinkmicUpdateVoiceResponse 对应 anchor_linkmic/update_voice 接口的响应结构。
type liveAnchorLinkmicUpdateVoiceResponse struct {
	ErrCode int64           `json:"errcode"`
	ErrMsg  string          `json:"errmsg"`
	Data    json.RawMessage `json:"data"`
}

// LiveAudienceJoinGame 调用「嘉宾云启动玩法」接口。
//
// 说明：
//   - 用于指定某个嘉宾在当前直播间云启动玩法；
//   - 启动成功的前置条件包括：玩法支持云启动、用户已在麦上、宿主 App 及版本支持云启动等；
//   - 若条件不满足或频率超限，平台会返回相应错误码（如 50041/50042/50047/50048 等）。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/audience/join_game
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  app_id, open_id, room_id
func (c *Client) LiveAudienceJoinGame(
	ctx context.Context,
	reqBody *LiveAudienceJoinGameRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveAudienceJoinGame request is nil")
	}
	if reqBody.AppID == "" || reqBody.OpenID == "" {
		return fmt.Errorf("douyin: LiveAudienceJoinGame requires non-empty app_id/open_id")
	}
	if reqBody.RoomID == 0 {
		return fmt.Errorf("douyin: LiveAudienceJoinGame requires non-zero room_id")
	}

	// 小程序级 access_token，用于填写 x-token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/audience/join_game")
	if err != nil {
		return fmt.Errorf("douyin: build audience join_game url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveAudienceJoinGame request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveAudienceJoinGame request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveAudienceJoinGame http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveAudienceJoinGame read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveAudienceJoinGame server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveAudienceJoinGame unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveAudienceJoinGameResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveAudienceJoinGame unmarshal response failed: %w", err)
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
	return fmt.Errorf("douyin: LiveAudienceJoinGame request failed after %d attempts", attempts)
}

// LiveAudienceLeaveGame 调用「嘉宾关闭玩法」接口。
//
// 说明：
//   - 用于指定某个嘉宾在当前直播间关闭玩法；
//   - 通常在嘉宾退出玩法或玩法结束时调用；
//   - 若条件不满足或频率超限，平台会返回相应错误码（如 50041/50042/50047/50048 等）。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/audience/leave_game
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  app_id, open_id, room_id
func (c *Client) LiveAudienceLeaveGame(
	ctx context.Context,
	reqBody *LiveAudienceLeaveGameRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveAudienceLeaveGame request is nil")
	}
	if reqBody.AppID == "" || reqBody.OpenID == "" {
		return fmt.Errorf("douyin: LiveAudienceLeaveGame requires non-empty app_id/open_id")
	}
	if reqBody.RoomID == 0 {
		return fmt.Errorf("douyin: LiveAudienceLeaveGame requires non-zero room_id")
	}

	// 小程序级 access_token，用于填写 x-token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/audience/leave_game")
	if err != nil {
		return fmt.Errorf("douyin: build audience leave_game url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveAudienceLeaveGame request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveAudienceLeaveGame request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveAudienceLeaveGame http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveAudienceLeaveGame read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveAudienceLeaveGame server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveAudienceLeaveGame unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveAudienceLeaveGameResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveAudienceLeaveGame unmarshal response failed: %w", err)
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
	return fmt.Errorf("douyin: LiveAudienceLeaveGame request failed after %d attempts", attempts)
}

// LiveCoGameUploadUserData 调用「上报连麦对局内用户积分」接口。
//
// 说明：
//   - 用于在观众一键同玩 / 嘉宾玩法场景下，上报连麦对局内各用户的积分（score）；
//   - 当前 anchor_infos 仅支持传入一个房间信息，但请求体仍为数组形式，便于未来扩展；
//   - 接口频率限制：小玩法 app_id 维度 QPS 不超过 LiveCoGameUploadUserDataRateLimitPerSecond 次/秒。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/round/co_game_upload_user_data
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  anchor_infos: 对局关联的直播房间列表（目前仅支持 1 个）
func (c *Client) LiveCoGameUploadUserData(
	ctx context.Context,
	reqBody *LiveCoGameUploadUserDataRequest,
) ([]LiveCoGameUploadFailRoom, error) {
	if reqBody == nil {
		return nil, fmt.Errorf("douyin: LiveCoGameUploadUserData request is nil")
	}
	if len(reqBody.AnchorInfos) == 0 {
		return nil, fmt.Errorf("douyin: LiveCoGameUploadUserData requires non-empty anchor_infos")
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
	u, err := base.Parse("/api/gaming_con/round/co_game_upload_user_data")
	if err != nil {
		return nil, fmt.Errorf("douyin: build co_game_upload_user_data url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("douyin: marshal LiveCoGameUploadUserData request failed: %w", err)
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
			return nil, fmt.Errorf("douyin: build LiveCoGameUploadUserData request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveCoGameUploadUserData http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveCoGameUploadUserData read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveCoGameUploadUserData server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("douyin: LiveCoGameUploadUserData unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveCoGameUploadUserDataResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("douyin: LiveCoGameUploadUserData unmarshal response failed: %w", err)
		}
		if resp.ErrCode != 0 {
			return nil, &Error{
				ErrNo:   int(resp.ErrCode),
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		// 即便整体 err_code 为 0，仍可能存在 fail_room_list 记录部分失败的房间。
		return resp.FailRoomList, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("douyin: LiveCoGameUploadUserData request failed after %d attempts", attempts)
}

// LiveAnchorLinkmicPrepare 调用「主播连线前置准备」接口。
//
// 说明：
//   - 用于在创建多主播连线前，检查目标主播所在房间是否满足连线条件，并为其开启聊天室模式；
//   - 前置条件包括：主播已挂载当前玩法、宿主版本满足要求、直播间类型/分成比符合规则等；
//   - 建议在创建主播连线前 2~3 秒，对每个将要参与连线的直播间调用一次。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/anchor_linkmic/prepare
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  app_id, room_id, room_id_str
func (c *Client) LiveAnchorLinkmicPrepare(
	ctx context.Context,
	reqBody *LiveAnchorLinkmicPrepareRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveAnchorLinkmicPrepare request is nil")
	}
	if reqBody.AppID == "" {
		return fmt.Errorf("douyin: LiveAnchorLinkmicPrepare requires non-empty app_id")
	}
	if reqBody.RoomID == 0 {
		return fmt.Errorf("douyin: LiveAnchorLinkmicPrepare requires non-zero room_id")
	}

	// 小程序级 access_token，用于填写 x-token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/gaming_con/anchor_linkmic/prepare")
	if err != nil {
		return fmt.Errorf("douyin: build anchor_linkmic prepare url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveAnchorLinkmicPrepare request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveAnchorLinkmicPrepare request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicPrepare http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicPrepare read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicPrepare server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveAnchorLinkmicPrepare unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveAnchorLinkmicPrepareResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveAnchorLinkmicPrepare unmarshal response failed: %w", err)
		}
		if resp.ErrCode != 0 {
			return &Error{
				ErrNo:   int(resp.ErrCode),
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("douyin: LiveAnchorLinkmicPrepare request failed after %d attempts", attempts)
}

// LiveAnchorLinkmicCreate 调用「创建主播连线」接口。
//
// 说明：
//   - 用于在多主播同局玩法中，创建一条包含多个主播直播间及其嘉宾的连线，返回连线 ID；
//   - 接口会根据主播/嘉宾的玩法挂载情况、版本、分成比、直播间公开性等条件过滤不符合要求的房间或嘉宾；
//   - 当过滤后人数不在 [2,9] 区间内时，接口会返回错误码提示创建失败。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/anchor_linkmic/create
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  app_id, max_link_num_per_room, room_id, room_id_str, rooms
func (c *Client) LiveAnchorLinkmicCreate(
	ctx context.Context,
	reqBody *LiveAnchorLinkmicCreateRequest,
) (*LiveAnchorLinkmicCreateData, error) {
	if reqBody == nil {
		return nil, fmt.Errorf("douyin: LiveAnchorLinkmicCreate request is nil")
	}
	if reqBody.AppID == "" {
		return nil, fmt.Errorf("douyin: LiveAnchorLinkmicCreate requires non-empty app_id")
	}
	if reqBody.RoomID == 0 {
		return nil, fmt.Errorf("douyin: LiveAnchorLinkmicCreate requires non-zero room_id")
	}
	if len(reqBody.Rooms) == 0 {
		return nil, fmt.Errorf("douyin: LiveAnchorLinkmicCreate requires non-empty rooms")
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
	u, err := base.Parse("/api/gaming_con/anchor_linkmic/create")
	if err != nil {
		return nil, fmt.Errorf("douyin: build anchor_linkmic create url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("douyin: marshal LiveAnchorLinkmicCreate request failed: %w", err)
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
			return nil, fmt.Errorf("douyin: build LiveAnchorLinkmicCreate request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicCreate http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicCreate read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicCreate server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("douyin: LiveAnchorLinkmicCreate unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveAnchorLinkmicCreateResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("douyin: LiveAnchorLinkmicCreate unmarshal response failed: %w", err)
		}
		if resp.ErrCode != 0 {
			return nil, &Error{
				ErrNo:   int(resp.ErrCode),
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return resp.Data, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("douyin: LiveAnchorLinkmicCreate request failed after %d attempts", attempts)
}

// LiveAnchorLinkmicRelease 调用「结束主播连线」接口。
//
// 说明：
//   - 用于结束指定连线 ID 的多主播连线；
//   - room_id 需填写任一参与连线的主播直播间 ID，用于发起结束动作。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/anchor_linkmic/release
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  app_id, link_id, room_id
func (c *Client) LiveAnchorLinkmicRelease(
	ctx context.Context,
	reqBody *LiveAnchorLinkmicReleaseRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveAnchorLinkmicRelease request is nil")
	}
	if reqBody.AppID == "" {
		return fmt.Errorf("douyin: LiveAnchorLinkmicRelease requires non-empty app_id")
	}
	if reqBody.LinkID == 0 {
		return fmt.Errorf("douyin: LiveAnchorLinkmicRelease requires non-zero link_id")
	}
	if reqBody.RoomID == 0 {
		return fmt.Errorf("douyin: LiveAnchorLinkmicRelease requires non-zero room_id")
	}

	// 小程序级 access_token，用于填写 x-token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/gaming_con/anchor_linkmic/release")
	if err != nil {
		return fmt.Errorf("douyin: build anchor_linkmic release url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveAnchorLinkmicRelease request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveAnchorLinkmicRelease request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicRelease http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicRelease read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicRelease server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveAnchorLinkmicRelease unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveAnchorLinkmicReleaseResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveAnchorLinkmicRelease unmarshal response failed: %w", err)
		}
		if resp.ErrCode != 0 {
			return &Error{
				ErrNo:   int(resp.ErrCode),
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("douyin: LiveAnchorLinkmicRelease request failed after %d attempts", attempts)
}

// LiveAnchorLinkmicQuery 调用「查询主播连麦信息」接口。
//
// 说明：
//   - 用于根据连线 ID 查询当前多主播连线中的各个房间及麦位上的主播、嘉宾 open_id；
//   - 返回的数据可用于玩法内感知连线结构、同步参与者列表等。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/anchor_linkmic/query
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  app_id, link_id, room_id
func (c *Client) LiveAnchorLinkmicQuery(
	ctx context.Context,
	reqBody *LiveAnchorLinkmicQueryRequest,
) (*LiveAnchorLinkmicQueryData, error) {
	if reqBody == nil {
		return nil, fmt.Errorf("douyin: LiveAnchorLinkmicQuery request is nil")
	}
	if reqBody.AppID == "" {
		return nil, fmt.Errorf("douyin: LiveAnchorLinkmicQuery requires non-empty app_id")
	}
	if reqBody.LinkID == 0 {
		return nil, fmt.Errorf("douyin: LiveAnchorLinkmicQuery requires non-zero link_id")
	}
	if reqBody.RoomID == 0 {
		return nil, fmt.Errorf("douyin: LiveAnchorLinkmicQuery requires non-zero room_id")
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
	u, err := base.Parse("/api/gaming_con/anchor_linkmic/query")
	if err != nil {
		return nil, fmt.Errorf("douyin: build anchor_linkmic query url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("douyin: marshal LiveAnchorLinkmicQuery request failed: %w", err)
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
			return nil, fmt.Errorf("douyin: build LiveAnchorLinkmicQuery request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicQuery http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicQuery read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicQuery server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("douyin: LiveAnchorLinkmicQuery unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveAnchorLinkmicQueryResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("douyin: LiveAnchorLinkmicQuery unmarshal response failed: %w", err)
		}
		if resp.ErrCode != 0 {
			return nil, &Error{
				ErrNo:   int(resp.ErrCode),
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return resp.Data, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("douyin: LiveAnchorLinkmicQuery request failed after %d attempts", attempts)
}

// LiveAnchorLinkmicAnchorLeave 调用「中途单主播离线」接口。
//
// 说明：
//   - 用于让指定房间的主播从当前连线中中途离开；
//   - 若该房间中仍有其他参与主播连线的嘉宾，则这些嘉宾也会一同离线。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/anchor_linkmic/anchor_leave
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  app_id, link_id, room_id
func (c *Client) LiveAnchorLinkmicAnchorLeave(
	ctx context.Context,
	reqBody *LiveAnchorLinkmicAnchorLeaveRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveAnchorLinkmicAnchorLeave request is nil")
	}
	if reqBody.AppID == "" {
		return fmt.Errorf("douyin: LiveAnchorLinkmicAnchorLeave requires non-empty app_id")
	}
	if reqBody.LinkID == 0 {
		return fmt.Errorf("douyin: LiveAnchorLinkmicAnchorLeave requires non-zero link_id")
	}
	if reqBody.RoomID == 0 {
		return fmt.Errorf("douyin: LiveAnchorLinkmicAnchorLeave requires non-zero room_id")
	}

	// 小程序级 access_token，用于填写 x-token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/gaming_con/anchor_linkmic/anchor_leave")
	if err != nil {
		return fmt.Errorf("douyin: build anchor_linkmic anchor_leave url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveAnchorLinkmicAnchorLeave request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveAnchorLinkmicAnchorLeave request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicAnchorLeave http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicAnchorLeave read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicAnchorLeave server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveAnchorLinkmicAnchorLeave unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveAnchorLinkmicAnchorLeaveResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveAnchorLinkmicAnchorLeave unmarshal response failed: %w", err)
		}
		if resp.ErrCode != 0 {
			return &Error{
				ErrNo:   int(resp.ErrCode),
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("douyin: LiveAnchorLinkmicAnchorLeave request failed after %d attempts", attempts)
}

// LiveAnchorLinkmicUpdateVoice 调用「上报附近语音用户音量」接口。
//
// 说明：
//   - 用于在多主播连线场景下，为主态用户配置其可听见的附近用户音量，实现“附近语音”效果；
//   - 初始默认所有连线内用户互相可听见，音量为 100，该接口可对单个或多个用户的听音范围与音量做精细调整；
//   - 建议在对局开始时、中途有人上下麦时，以及玩法中用户“靠近/远离”时按需调用。
//
// 文档对应：
//
//	POST https://webcast.bytedance.com/api/gaming_con/anchor_linkmic/update_voice
//	Header:
//	  content-type: application/json
//	  x-token     : 通过 https://developer.toutiao.com/api/apps/v2/token 生成的 token
//	Body:
//	  app_id, link_id, link_id_str, user_infos
func (c *Client) LiveAnchorLinkmicUpdateVoice(
	ctx context.Context,
	reqBody *LiveAnchorLinkmicUpdateVoiceRequest,
) error {
	if reqBody == nil {
		return fmt.Errorf("douyin: LiveAnchorLinkmicUpdateVoice request is nil")
	}
	if reqBody.AppID == "" {
		return fmt.Errorf("douyin: LiveAnchorLinkmicUpdateVoice requires non-empty app_id")
	}
	if reqBody.LinkID == 0 && reqBody.LinkIDStr == "" {
		return fmt.Errorf("douyin: LiveAnchorLinkmicUpdateVoice requires non-zero link_id or non-empty link_id_str")
	}
	if len(reqBody.UserInfos) == 0 {
		return fmt.Errorf("douyin: LiveAnchorLinkmicUpdateVoice requires non-empty user_infos")
	}

	// 小程序级 access_token，用于填写 x-token。
	xToken, err := c.GetMiniAppAccessToken(ctx)
	if err != nil {
		return err
	}

	base, err := url.Parse(c.cfg.WebcastBaseURL)
	if err != nil {
		return fmt.Errorf("douyin: parse webcast base url failed: %w", err)
	}
	u, err := base.Parse("/api/gaming_con/anchor_linkmic/update_voice")
	if err != nil {
		return fmt.Errorf("douyin: build anchor_linkmic update_voice url failed: %w", err)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("douyin: marshal LiveAnchorLinkmicUpdateVoice request failed: %w", err)
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
			return fmt.Errorf("douyin: build LiveAnchorLinkmicUpdateVoice request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("x-token", xToken)

		res, err := c.miniHTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicUpdateVoice http request failed: %w", err)
			continue
		}

		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicUpdateVoice read response failed: %w", readErr)
			continue
		}

		// 5xx 视为可重试错误。
		if res.StatusCode >= http.StatusInternalServerError && res.StatusCode <= http.StatusNetworkAuthenticationRequired {
			lastErr = fmt.Errorf("douyin: LiveAnchorLinkmicUpdateVoice server error status=%d body=%s", res.StatusCode, string(body))
			continue
		}
		// 非 200 且非 5xx：直接返回，不重试。
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("douyin: LiveAnchorLinkmicUpdateVoice unexpected status=%d body=%s", res.StatusCode, string(body))
		}

		var resp liveAnchorLinkmicUpdateVoiceResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("douyin: LiveAnchorLinkmicUpdateVoice unmarshal response failed: %w", err)
		}
		if resp.ErrCode != 0 {
			return &Error{
				ErrNo:   int(resp.ErrCode),
				ErrTips: resp.ErrMsg,
				RawBody: body,
			}
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("douyin: LiveAnchorLinkmicUpdateVoice request failed after %d attempts", attempts)
}
