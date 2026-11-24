package image

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/onlyLTY/dockerCopilot/internal/logic/image"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func PullHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ImagePullReq
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<14))
		if err := json.Unmarshal(body, &req); err != nil {
			httpx.WriteJson(w, 400, &types.Resp{Code: 400, Msg: "参数解析失败：" + err.Error(), Data: map[string]string{}})
			return
		}
		l := image.NewPullLogic(r.Context(), svcCtx)
		resp, _ := l.Pull(&req)
		httpx.WriteJson(w, resp.Code, resp)
	}
}

func DetailHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.IdReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.WriteJson(w, 400, &types.Resp{Code: 400, Msg: "缺少镜像 ID", Data: map[string]string{}})
			return
		}
		l := image.NewDetailLogic(r.Context(), svcCtx)
		resp, _ := l.Detail(req.Id)
		httpx.WriteJson(w, resp.Code, resp)
	}
}

func PruneHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ImagePruneReq
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<14))
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				httpx.WriteJson(w, 400, &types.Resp{Code: 400, Msg: "参数解析失败：" + err.Error(), Data: map[string]string{}})
				return
			}
		}
		l := image.NewPruneLogic(r.Context(), svcCtx)
		resp, _ := l.Prune(&req)
		httpx.WriteJson(w, resp.Code, resp)
	}
}
