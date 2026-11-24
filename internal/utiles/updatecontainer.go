package utiles

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerMsgType "github.com/docker/docker/pkg/jsonmessage"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
	"io"
	"strings"
	"time"
)

func UpdateContainer(serviceContext *svc.ServiceContext, id string, name string, imageNameAndTag string, delOldContainer bool, taskID string) (err error) {
	ctx := context.Background()
	startedAt := time.Now()
	notifyResult := "success"
	notifyDetail := "更新完成"
	// 通知最后执行（defer 后进先出），能拿到回滚或跳过之后的真实结果
	defer func() {
		if err != nil && notifyResult == "success" {
			notifyResult = "failed"
			notifyDetail = err.Error()
		}
		NotifyUpdate(NotifyPayload{
			Container:  name,
			Image:      imageNameAndTag,
			Result:     notifyResult,
			Message:    notifyDetail,
			DurationMS: time.Since(startedAt).Milliseconds(),
		})
	}()
	serviceContext.UpdateProgress(taskID, svc.TaskProgress{
		TaskID:     taskID,
		Percentage: 0,
		Name:       name,
		Message:    "正在连接Docker",
		DetailMsg:  "正在连接Docker",
		IsDone:     false,
	})
	var oldTaskProgress, result = serviceContext.GetProgress(taskID)
	if !result {
		oldTaskProgress = svc.TaskProgress{
			Percentage: 0,
			Name:       "",
			Message:    "",
			DetailMsg:  "",
			IsDone:     false,
		}
	}
	timeout := 10
	signal := "SIGINT"

	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	oldTaskProgress.Message = "正在拉取新镜像"
	oldTaskProgress.Percentage = 10
	oldTaskProgress.DetailMsg = "正在拉取新镜像"
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	serviceContext.DockerClient.NegotiateAPIVersion(ctx)

	// 记录现场：旧容器的镜像 ID 与真实名字，用于「镜像没变就不重建」和失败回滚
	inspectBefore, err := serviceContext.DockerClient.ContainerInspect(ctx, id)
	if err != nil {
		oldTaskProgress.Message = "获取容器信息失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		return err
	}
	origName := strings.TrimPrefix(inspectBefore.Name, "/")
	oldImageID := inspectBefore.Image
	renamedTo := ""
	newCreated := false
	rollbackNeeded := false

	// 失败回滚：把旧容器改名改回来并重新启动，避免「更新失败 = 服务停着」
	defer func() {
		if err == nil || !rollbackNeeded {
			return
		}
		if newCreated {
			if e := serviceContext.DockerClient.ContainerRemove(ctx, origName, container.RemoveOptions{Force: true}); e != nil {
				logx.Errorf("回滚：删除新容器失败: %v", e)
			}
		}
		if renamedTo != "" {
			if e := serviceContext.DockerClient.ContainerRename(ctx, id, origName); e != nil {
				logx.Errorf("回滚：容器名恢复失败: %v", e)
			}
		}
		if e := serviceContext.DockerClient.ContainerStart(ctx, id, container.StartOptions{}); e != nil {
			logx.Errorf("回滚：启动旧容器失败: %v", e)
		}
		notifyResult = "rollback"
		notifyDetail = fmt.Sprintf("%v；已回滚到旧容器", err)
		serviceContext.UpdateProgress(taskID, svc.TaskProgress{
			TaskID:     taskID,
			Percentage: 100,
			Name:       name,
			Message:    "更新失败，已回滚",
			DetailMsg:  fmt.Sprintf("%v；旧容器已恢复运行", err),
			IsDone:     true,
		})
	}()

	reader, err := serviceContext.DockerClient.ImagePull(ctx, imageNameAndTag, image.PullOptions{})
	if err != nil {
		oldTaskProgress.Message = "拉取镜像失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		logx.Errorf("Failed to pull image: %s", err)
		return err
	}
	err = decodePullResp(reader, serviceContext, taskID)
	if err != nil {
		oldTaskProgress.Message = "拉取镜像失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		logx.Errorf("Failed to pull image: %s", err)
		return err
	}
	oldTaskProgress, result = serviceContext.GetProgress(taskID)
	if !result {
		oldTaskProgress = svc.TaskProgress{
			Percentage: 0,
			Name:       "",
			Message:    "",
			DetailMsg:  "",
			IsDone:     false,
		}
	}
	oldTaskProgress.Message = "拉取镜像成功"
	oldTaskProgress.DetailMsg = "拉取镜像成功"

	// 镜像没变就不要白停一次服务：比对拉取后的镜像 ID 与容器当前镜像 ID
	if imgInspect, _, inspectErr := serviceContext.DockerClient.ImageInspectWithRaw(ctx, imageNameAndTag); inspectErr == nil && imgInspect.ID == oldImageID {
		notifyResult = "skipped"
		notifyDetail = "远端镜像与本地一致，未重建容器"
		serviceContext.UpdateProgress(taskID, svc.TaskProgress{
			TaskID:     taskID,
			Percentage: 100,
			Name:       name,
			Message:    "已是最新镜像，无需重建容器",
			DetailMsg:  "远端镜像与本地一致（image id 相同）",
			IsDone:     true,
		})
		return nil
	}

	oldTaskProgress.Percentage = 30
	oldTaskProgress.Message = "正在停止容器"
	oldTaskProgress.DetailMsg = "正在停止容器"
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	rollbackNeeded = true
	stopOptions := container.StopOptions{
		Signal:  signal,
		Timeout: &timeout,
	}
	err = serviceContext.DockerClient.ContainerStop(context.Background(), id, stopOptions)
	if err != nil {
		oldTaskProgress.Message = "停止容器失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		return err
	}
	oldTaskProgress.Message = "容器停止成功"
	oldTaskProgress.DetailMsg = "容器停止成功"

	oldTaskProgress.Percentage = 40
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	oldTaskProgress.Message = "正在重命名旧容器"
	oldTaskProgress.DetailMsg = "正在重命名旧容器"
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	currentDate := time.Now().Format("2006-01-02-15-04-05")
	renamedTarget := origName + "-" + currentDate
	err = serviceContext.DockerClient.ContainerRename(context.Background(), id, renamedTarget)
	if err != nil {
		oldTaskProgress.Message = "重命名旧容器失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		return err
	}
	renamedTo = renamedTarget
	oldTaskProgress.Message = "重命名旧容器成功"
	oldTaskProgress.DetailMsg = "重命名旧容器成功"
	oldTaskProgress.Percentage = 60
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	oldTaskProgress.Message = "正在创建新容器"
	oldTaskProgress.DetailMsg = "正在创建新容器"
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	inspectedContainer, err := serviceContext.DockerClient.ContainerInspect(ctx, id)
	if err != nil {
		oldTaskProgress.Message = "获取容器信息失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		logx.Error("获取容器信息失败" + err.Error())
		return err
	}
	inspectedContainer.Config.Hostname = ""
	inspectedContainer.Config.Image = imageNameAndTag
	inspectedContainer.Image = imageNameAndTag
	config := inspectedContainer.Config
	hostConfig := inspectedContainer.HostConfig
	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: inspectedContainer.NetworkSettings.Networks,
	}
	containerName := origName
	_, err = serviceContext.DockerClient.ContainerCreate(ctx, config, hostConfig, networkingConfig, nil, containerName)
	if err != nil {
		oldTaskProgress.Message = "创建新容器失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		return err
	}
	newCreated = true
	oldTaskProgress.Message = "创建新容器成功"
	oldTaskProgress.DetailMsg = "创建新容器成功"
	oldTaskProgress.Percentage = 80
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	oldTaskProgress.Message = "正在启动新容器以及删除旧容器(如果不保留旧容器)"
	oldTaskProgress.DetailMsg = "正在启动新容器以及删除旧容器(如果不保留旧容器)"
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	err = serviceContext.DockerClient.ContainerStart(context.Background(), containerName, container.StartOptions{
		CheckpointID:  "",
		CheckpointDir: "",
	})
	if err != nil {
		oldTaskProgress.Message = "启动新容器失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		return err
	}
	// 新容器已经在跑，旧容器删不掉只影响磁盘，不能让整次更新报失败
	rollbackNeeded = false
	if delOldContainer {
		if removeErr := serviceContext.DockerClient.ContainerRemove(context.Background(), id, container.RemoveOptions{}); removeErr != nil {
			logx.Errorf("删除旧容器失败（新容器已正常运行）: %v", removeErr)
			notifyDetail = "更新成功，但旧容器删除失败：" + removeErr.Error()
			oldTaskProgress.DetailMsg = notifyDetail
		}
	}
	oldTaskProgress.Message = "更新成功"
	oldTaskProgress.DetailMsg = "更新成功"
	oldTaskProgress.Percentage = 100
	oldTaskProgress.IsDone = true
	serviceContext.UpdateProgress(taskID, oldTaskProgress)

	// 修复：更新容器成功后刷新 HubImageInfo，避免旧的 NeedUpdate 状态残留
	// 之前需要等到每小时的定时检查才能更新，导致用户看到"已更新但仍提示有更新"
	go func() {
		list, err := GetImagesList(serviceContext)
		if err != nil {
			logx.Errorf("刷新镜像列表出错: %v", err)
			return
		}
		serviceContext.HubImageInfo.CheckUpdate(list)
	}()

	if PruneEnabled() {
		go PruneOldImages(serviceContext)
	}

	return nil
}

func decodePullResp(reader io.Reader, ctx *svc.ServiceContext, taskID string) (err error) {
	decoder := json.NewDecoder(reader)
	var oldTaskProgress, result = ctx.GetProgress(taskID)
	if !result {
		oldTaskProgress = svc.TaskProgress{
			Percentage: 0,
			Name:       "",
			Message:    "",
			DetailMsg:  "",
			IsDone:     false,
		}
	}
	for {
		var msg dockerMsgType.JSONMessage
		if err = decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				return nil
			}
			oldTaskProgress.Message = "拉取镜像失败"
			oldTaskProgress.DetailMsg = err.Error()
			oldTaskProgress.Percentage = 25
			oldTaskProgress.IsDone = true
			ctx.UpdateProgress(taskID, oldTaskProgress)
			logx.Errorf("Failed to decode pull image response: %s", err)
			return fmt.Errorf("拉取镜像失败: %w", err)
		}
		// Print the progress or error information from the response
		if msg.Error != nil {
			oldTaskProgress.Message = "拉取镜像失败"
			oldTaskProgress.DetailMsg = msg.Error.Error()
			oldTaskProgress.Percentage = 25
			oldTaskProgress.IsDone = true
			ctx.UpdateProgress(taskID, oldTaskProgress)
			logx.Errorf("Error: %s", msg.Error)
			return fmt.Errorf("拉取镜像失败: %w", msg.Error)
		} else {
			var formattedMsg string
			if msg.Progress != nil {
				formattedMsg = fmt.Sprintf("进度%s: %s", msg.Status, msg.Progress.String())
			} else {
				formattedMsg = fmt.Sprintf("进度%s", msg.Status)
			}
			oldTaskProgress.DetailMsg = formattedMsg
			oldTaskProgress.Percentage = 25
			ctx.UpdateProgress(taskID, oldTaskProgress)
			logx.Infof("拉取镜像进度\t %s: %s\n", msg.Status, msg.Progress)
		}
	}
}
