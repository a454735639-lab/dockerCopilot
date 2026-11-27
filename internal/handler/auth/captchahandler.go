package auth

import (
	"net/http"
	"os"

	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// CaptchaHandler GET /api/auth/captcha：生成图形验证码（公开接口）。
// 设 CAPTCHA_DEBUG=1 时响应里带明文 code，仅用于联调验证，生产不要开。
func CaptchaHandler(ctx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !module.AllowCaptcha(module.ClientIP(r)) {
			httpx.WriteJson(w, http.StatusTooManyRequests, &types.Resp{
				Code: http.StatusTooManyRequests,
				Msg:  "验证码请求过于频繁，请稍后再试",
				Data: map[string]string{},
			})
			return
		}
		id, svg, code := module.GenCaptcha()
		data := map[string]string{"captchaId": id, "svg": svg}
		if os.Getenv("CAPTCHA_DEBUG") == "1" {
			data["code"] = code
		}
		httpx.OkJson(w, &types.Resp{Code: 200, Msg: "success", Data: data})
	}
}
