package settings

import (
	"encoding/json"
	"io"
	"net/http"

	settingsLogic "github.com/onlyLTY/dockerCopilot/internal/logic/settings"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func GetHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := settingsLogic.NewSettingsLogic(r.Context(), svcCtx)
		resp, _ := l.Get()
		httpx.WriteJson(w, resp.Code, resp)
	}
}

func TestNotifyHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.SettingsUpdateReq
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil || len(body) == 0 {
			httpx.WriteJson(w, 400, &types.Resp{Code: 400, Msg: "请求体为空", Data: map[string]string{}})
			return
		}
		if err := json.Unmarshal(body, &req); err != nil {
			httpx.WriteJson(w, 400, &types.Resp{Code: 400, Msg: "参数解析失败：" + err.Error(), Data: map[string]string{}})
			return
		}
		l := settingsLogic.NewTestNotifyLogic(r.Context(), svcCtx)
		resp, _ := l.Test(&req)
		httpx.WriteJson(w, resp.Code, resp)
	}
}

func UpdateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.SettingsUpdateReq
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil || len(body) == 0 {
			httpx.WriteJson(w, 400, &types.Resp{Code: 400, Msg: "请求体为空", Data: map[string]string{}})
			return
		}
		if err := json.Unmarshal(body, &req); err != nil {
			httpx.WriteJson(w, 400, &types.Resp{Code: 400, Msg: "参数解析失败：" + err.Error(), Data: map[string]string{}})
			return
		}
		l := settingsLogic.NewSettingsLogic(r.Context(), svcCtx)
		resp, _ := l.Update(&req)
		httpx.WriteJson(w, resp.Code, resp)
	}
}
