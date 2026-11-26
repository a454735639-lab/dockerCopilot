package settings

import (
	"context"
	"strings"
	"time"

	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/onlyLTY/dockerCopilot/internal/utiles"
	"github.com/zeromicro/go-zero/core/logx"
)

type TestNotifyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTestNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TestNotifyLogic {
	return &TestNotifyLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Test 用请求体里的当前表单值发送一条测试消息（不写数据库）。
func (l *TestNotifyLogic) Test(req *types.SettingsUpdateReq) (resp *types.Resp, err error) {
	c := utiles.NotifyConf{
		URL:        strings.TrimSpace(req.NotifyURL),
		Format:     strings.ToLower(strings.TrimSpace(req.NotifyFormat)),
		CorpID:     strings.TrimSpace(req.WecomCorpID),
		Secret:     strings.TrimSpace(req.WecomSecret),
		AgentID:    strings.TrimSpace(req.WecomAgentID),
		ToUser:     strings.TrimSpace(req.WecomToUser),
		WecomProxy: strings.TrimSpace(req.WecomProxy),
	}

	format := c.Format
	if format == "" {
		format = guessFmt(c.URL)
	}

	// 校验：应用模式要三件套，其它模式要 URL
	switch format {
	case "wecom_app":
		if c.CorpID == "" || c.Secret == "" || c.AgentID == "" {
			return &types.Resp{Code: 400, Msg: "企业微信应用模式缺少 Corp ID / Secret / Agent ID（三项必填）", Data: map[string]string{}}, nil
		}
	default:
		if c.URL == "" {
			return &types.Resp{Code: 400, Msg: "通知地址为空，请填入 webhook 地址或切换为企业微信应用模式", Data: map[string]string{}}, nil
		}
	}

	msg := "Docker Copilot 配置测试\n时间：" + time.Now().Format("2006-01-02 15:04:05")
	payload := utiles.NotifyPayload{
		Container: "-",
		Image:     "-",
		Result:    "test",
		Message:   msg,
	}
	if sendErr := utiles.SendTestMessage(c, payload); sendErr != nil {
		l.Errorf("测试通知失败: %v", sendErr)
		return &types.Resp{
			Code: 500,
			Msg:  "测试失败: " + sendErr.Error(),
			Data: map[string]string{"format": format},
		}, nil
	}

	return &types.Resp{
		Code: 200,
		Msg:  "已发送测试消息，请检查对应应用/群",
		Data: map[string]string{"format": format},
	}, nil
}

func guessFmt(rawURL string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, "api.day.app"):
		return "bark"
	case strings.Contains(lower, "oapi.dingtalk.com"):
		return "dingtalk"
	case strings.Contains(lower, "open.feishu.cn"), strings.Contains(lower, "open.larksuite.com"):
		return "feishu"
	case strings.Contains(lower, "ntfy"):
		return "ntfy"
	case strings.Contains(lower, "qyapi.weixin.qq.com") && strings.Contains(lower, "webhook"):
		return "wecom_webhook"
	default:
		return "generic"
	}
}
