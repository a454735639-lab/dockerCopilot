package utiles

import (
	"context"
	"github.com/docker/docker/api/types/container"
	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	MyType "github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
)

func GetContainerList(ctx *svc.ServiceContext) ([]MyType.Container, error) {
	// 获取所有容器（包括停止的容器）
	dockerContainerList, err := ctx.DockerClient.ContainerList(context.Background(), container.ListOptions{
		All: true, // 设置为true来获取所有容器
	})
	if err != nil {
		logx.Errorf("get container list error: %v", err)
		return nil, err
	}
	var containerList []MyType.Container
	for _, dockerContainerInfo := range dockerContainerList {
		containerInfo := MyType.Container{
			Container: dockerContainerInfo,
		}
		containerList = append(containerList, containerInfo)
	}
	return containerList, nil
}

func CheckImageUpdate(ctx *svc.ServiceContext, containerListData []MyType.Container) []MyType.Container {
	for i, v := range containerListData {
		// HubImageInfo 现在是并发安全的读取接口
		info, ok := ctx.HubImageInfo.Get(v.ImageID)
		if !ok || !info.NeedUpdate {
			continue
		}
		// 容器标签 dockercopilot.check=false：这个容器不要更新提示
		if !module.LabelBool(v.Labels, module.LabelCheck, true) {
			continue
		}
		// 容器级忽略名单：镜像有更新，但这个容器用户不想看到/不想动
		ignored := false
		for _, n := range v.Names {
			if module.IgnoredContainer(strings.TrimPrefix(n, "/")) {
				ignored = true
				break
			}
		}
		if !ignored {
			containerListData[i].Update = true
		}
	}
	return containerListData
}
