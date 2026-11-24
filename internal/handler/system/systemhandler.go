package system

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/onlyLTY/dockerCopilot/internal/logic/system"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func DFHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := system.NewSystemLogic(r.Context(), svcCtx)
		resp, _ := l.DF()
		httpx.WriteJson(w, resp.Code, resp)
	}
}

func PruneHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.SystemPruneReq
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<14))
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				httpx.WriteJson(w, 400, &types.Resp{Code: 400, Msg: "参数解析失败", Data: map[string]string{}})
				return
			}
		}
		l := system.NewSystemLogic(r.Context(), svcCtx)
		resp, _ := l.Prune(&req)
		httpx.WriteJson(w, resp.Code, resp)
	}
}
