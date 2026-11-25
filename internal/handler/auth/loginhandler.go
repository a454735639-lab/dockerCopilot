package auth

import (
	"github.com/onlyLTY/dockerCopilot/internal/logic/auth"
	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func LoginHandler(ctx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LoginReq
		if err := httpx.Parse(r, &req); err != nil {
			var resp types.Resp
			resp.Code = 400
			resp.Msg = "错误的请求"
			httpx.WriteJson(w, 400, resp)
			return
		}
		l := auth.NewLoginLogic(r.Context(), ctx)
		// 统一用 resp.Code 输出：登录成功 200，限速锁定 429，密钥错误 401。
		// 原来锁定分支 err 为 nil 时会被 OkJson 包成 HTTP 200。
		resp, _ := l.Login(&req, module.ClientIP(r))
		httpx.WriteJson(w, resp.Code, resp)
	}
}
