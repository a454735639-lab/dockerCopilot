package settings

import (
	"context"
	"strings"

	settingsStore "github.com/onlyLTY/dockerCopilot/internal/settings"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/robfig/cron/v3"
	"github.com/zeromicro/go-zero/core/logx"
)

type SettingsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSettingsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SettingsLogic {
	return &SettingsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Get 返回当前设置：文件里的值优先，其次环境变量兜底，并标明来源
func (l *SettingsLogic) Get() (resp *types.Resp, err error) {
	current := settingsStore.Get()
	env := settingsStore.EnvValues()

	values := make(map[string]string, len(env))
	sources := make(map[string]string, len(env))
	for envKey, field := range settingsStore.EnvKeys() {
		if v := settingsStore.FieldValue(current, field); v != "" {
			values[field] = v
			sources[field] = "file"
			continue
		}
		values[field] = env[envKey]
		sources[field] = "env"
	}

	return &types.Resp{
		Code: 200,
		Msg:  "success",
		Data: types.SettingsData{
			Values:  values,
			Sources: sources,
			Env:     env,
		},
	}, nil
}

// Update 保存设置并落盘，保存后立即生效
func (l *SettingsLogic) Update(req *types.SettingsUpdateReq) (resp *types.Resp, err error) {
	if req == nil {
		return &types.Resp{Code: 400, Msg: "参数为空", Data: map[string]string{}}, nil
	}
	// 无效表达式必须在保存时拒绝，否则 UI 显示成功但定时更新被静默关闭。
	if expr := strings.TrimSpace(req.AutoUpdateCron); expr != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, parseErr := parser.Parse(expr); parseErr != nil {
			return &types.Resp{Code: 400, Msg: "自动更新 cron 表达式无效：" + parseErr.Error(), Data: map[string]string{}}, nil
		}
	}
	if saveErr := settingsStore.Update(*req); saveErr != nil {
		l.Errorf("保存设置失败: %v", saveErr)
		return &types.Resp{Code: 500, Msg: "保存设置失败：" + saveErr.Error(), Data: map[string]string{}}, saveErr
	}
	return l.Get()
}
