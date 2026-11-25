package settings

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onlyLTY/dockerCopilot/internal/settings"
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

// Test 发送一条测试消息（结果不写数据库），用于确认通知配置是否正确
func (l *TestNotifyLogic) Test() (resp *types.Resp, err error) {
	u := settings.Str("NOTIFY_URL")
	if u == "" {
		return &types.Resp{Code: 400, Msg: "通知地址未设置，请先在\"通知地址\"中填入 webhook 或 bark", Data: map[string]string{}}, nil
	}
	format := settings.Str("NOTIFY_FORMAT")
	msg := "Docker Copilot 配置测试\n时间：" + time.Now().Format("2006-01-02 15:04:05")
	result := ""
	switch format {
	case "bark":
		result = "格式: Bark"
	case "dingtalk":
		result = "格式: 钉钉"
	case "feishu":
		result = "格式: 飞书"
	case "ntfy":
		result = "格式: Ntfy"
	case "wecom_webhook":
		result = "格式: 企业微信群机器人"
	case "wecom_app":
		corpID := strings.TrimSpace(settings.Str("WECOM_CORP_ID"))
		secret := settings.Str("WECOM_SECRET")
		agentID := strings.TrimSpace(settings.Str("WECOM_AGENT_ID"))
		toUser := strings.TrimSpace(settings.Str("WECOM_TOUSER"))
		if corpID == "" || secret == "" || agentID == "" {
			return &types.Resp{Code: 400, Msg: "企业微信应用模式凭证不完整（缺少 Corp ID / Secret / Agent ID）", Data: map[string]string{}}, nil
		}
		if toUser == "" {
			toUser = "@all"
		}
		result = fmt.Sprintf("格式: 企业微信应用(%s)", toUser)
	default:
		result = "格式: generic(webhook)"
	}

	// 实际推送一次测试消息，错误会写日志并作为响应返回
	payload := utiles.NotifyPayload{
		Container: "-",
		Image:     "-",
		Result:    "test",
		Message:   msg,
	}
	if sendErr := utiles.SendTestMessage(u, payload); sendErr != nil {
		l.Errorf("测试通知失败: %v", sendErr)
		return &types.Resp{
			Code: 500,
			Msg:  "测试失败: " + sendErr.Error(),
			Data: map[string]string{"result": result},
		}, nil
	}

	return &types.Resp{
		Code: 200,
		Msg:  "已发送测试消息，请检查对应应用",
		Data: map[string]string{"result": result},
	}, nil
}
