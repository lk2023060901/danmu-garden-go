package douyin

import (
	"context"
	"fmt"

	"github.com/alibabacloud-go/tea/tea"
	sdkclient "github.com/bytedance/douyin-openapi-sdk-go/client"
)

// CouponQueryCouponMetaRequest 是优惠券元信息查询接口的请求结构体别名。
//
// 说明：
//   - 为了避免业务侧直接依赖 sdkclient 包，这里通过类型别名暴露请求结构；
//   - 使用方式与官方 SDK 保持一致，例如通过 SetCouponMetaId/SetHeader 等方法设置字段。
type CouponQueryCouponMetaRequest = sdkclient.CouponQueryCouponMetaRequest

// CouponQueryCouponMetaResponse 是优惠券元信息查询接口的响应结构体别名。
type CouponQueryCouponMetaResponse = sdkclient.CouponQueryCouponMetaResponse

// CouponQueryCouponMeta 封装官方 CouponQueryCouponMeta 接口。
//
// 行为：
//   - 若请求未显式设置 AccessToken，则自动使用 GetOpenAPIClientToken 获取并注入；
//   - 然后调用底层 sdkclient.Client 的 CouponQueryCouponMeta 方法。
func (c *Client) CouponQueryCouponMeta(ctx context.Context, req *CouponQueryCouponMetaRequest) (*CouponQueryCouponMetaResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("douyin: CouponQueryCouponMeta request is nil")
	}

	// 如未设置 AccessToken，则自动使用应用级 client_token。
	if req.AccessToken == nil || tea.StringValue(req.AccessToken) == "" {
		token, err := c.GetOpenAPIClientToken(ctx)
		if err != nil {
			return nil, err
		}
		req.SetAccessToken(token)
	}

	resp, err := c.sdk.CouponQueryCouponMeta(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
