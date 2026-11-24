package utiles

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/settings"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// 定时自动更新（默认关闭）：
//
//	AUTO_UPDATE=1                        开启
//	AUTO_UPDATE_CRON="0 4 * * *"         执行时刻（默认每天 04:00）
//	AUTO_UPDATE_INCLUDE=db,redis-*       非空时只更新容器名命中的
//	AUTO_UPDATE_MAX=5                    每轮最多更新几个，默认 5
//
// IGNORE_IMAGES / IGNORE_CONTAINERS 对自动更新同样生效。

func AutoUpdateEnabled() bool {
	switch strings.ToLower(settings.Str("AUTO_UPDATE")) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func AutoUpdateCron() string {
	if v := strings.TrimSpace(settings.Str("AUTO_UPDATE_CRON")); v != "" {
		return v
	}
	return "0 4 * * *"
}

// AutoUpdateOnce 扫描一轮，按更新标记逐个更新命中的容器。
func AutoUpdateOnce(ctx *svc.ServiceContext) {
	list, err := GetContainerList(ctx)
	if err != nil {
		logx.Errorf("自动更新：获取容器列表失败: %v", err)
		return
	}
	list = CheckImageUpdate(ctx, list)

	include := module.EnvList("AUTO_UPDATE_INCLUDE")
	maxUpdates := envInt("AUTO_UPDATE_MAX", 5)
	updated := 0

	for _, v := range list {
		if !v.Update {
			continue
		}
		name := ""
		if len(v.Names) > 0 {
			name = strings.TrimPrefix(v.Names[0], "/")
		}
		if name == "" {
			continue
		}
		// 自己永远不自动更新：更新过程中会把自己的进程杀掉
		if strings.Contains(strings.ToLower(name), "dockercopilot") ||
			strings.Contains(strings.ToLower(v.Image), "dockercopilot") {
			continue
		}
		if !module.LabelBool(v.Labels, module.LabelAutoUpdate, true) {
			logx.Infof("自动更新：%s 打了 auto-update=false，跳过", name)
			continue
		}
		if len(include) > 0 && !module.MatchAny(include, name) {
			continue
		}
		if updated >= maxUpdates {
			logx.Infof("自动更新：本轮已达上限 %d，剩余容器留到下一次", maxUpdates)
			break
		}
		if !TryLockContainerUpdate(v.ID) {
			logx.Infof("自动更新：%s 正在更新中，跳过", name)
			continue
		}
		logx.Infof("自动更新：开始更新 %s（%s）", name, v.Image)
		if updateErr := UpdateContainer(ctx, v.ID, name, v.Image, true, uuid.New().String()); updateErr != nil {
			logx.Errorf("自动更新：%s 失败: %v", name, updateErr)
		}
		UnlockContainerUpdate(v.ID)
		updated++
	}
	if updated == 0 {
		logx.Info("自动更新：本轮没有需要更新的容器")
	}
}

func envInt(key string, def int) int {
	raw := strings.TrimSpace(settings.Str(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
