package auth

import (
	"net/http"

	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// LogoutHandler 把当前 JWT 加入吊销表；前端随后应丢弃本地 token。
func LogoutHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		module.RevokeToken(r)
		httpx.OkJsonCtx(r.Context(), w, types.Resp{
			Code: 200,
			Msg:  "success",
			Data: map[string]string{},
		})
	}
}
