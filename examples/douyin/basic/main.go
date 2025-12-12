package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	sdkdouyin "github.com/lk2023060901/danmu-garden-go/internal/sdk/douyin"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	// 为了避免在仓库里硬编码密钥，这里从环境变量读取。
	//
	// 建议启动方式：
	//   DOUYIN_CLIENT_KEY=tt8621f09c192351a210 \
	//   DOUYIN_CLIENT_SECRET=18a5a64bd5dce46ea907e79838758102ce009cb6 \
	//   go run ./examples/douyin/basic
	clientKey := getenv("DOUYIN_CLIENT_KEY", "")
	clientSecret := getenv("DOUYIN_CLIENT_SECRET", "")

	if clientKey == "" || clientSecret == "" {
		log.Fatalf("DOUYIN_CLIENT_KEY / DOUYIN_CLIENT_SECRET must be set via environment variables")
	}

	cfg := sdkdouyin.Config{
		ClientKey:    clientKey,
		ClientSecret: clientSecret,
	}

	client, err := sdkdouyin.NewClient(cfg)
	if err != nil {
		log.Fatalf("init douyin client failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1) 测试 OpenAPI client_token 获取（应用级鉴权），验证我们对 credential 包的封装是否正确。
	token, err := client.GetOpenAPIClientToken(ctx)
	if err != nil {
		log.Fatalf("GetOpenAPIClientToken failed: %v", err)
	}
	fmt.Printf("[douyin-basic] client_token acquired, len=%d prefix=%q...\n", len(token), token[:min(8, len(token))])

	// 2) 示例：使用一个固定的 coupon_meta_id 调用 CouponQueryCouponMeta 接口。
	// 说明：该 ID 仅用于演示，实际使用时请替换为自己应用下创建的优惠券模版 ID。
	const couponMetaID = "7373678331264630820"

	req := &sdkdouyin.CouponQueryCouponMetaRequest{}
	req.SetCouponMetaId(couponMetaID)
	// AccessToken 若未显式设置，内部会自动使用 GetClientToken 注入。

	resp, err := client.CouponQueryCouponMeta(ctx, req)
	if err != nil {
		log.Fatalf("CouponQueryCouponMeta failed: %v", err)
	}
	fmt.Printf("[douyin-basic] CouponQueryCouponMeta success: err_no=%d coupon_meta_id=%s\n",
		resp.ErrNo, resp.CouponMeta.CouponMetaId)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
