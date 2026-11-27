package utiles

import (
	"context"
	"encoding/json"
	"fmt"
	dockerBackend "github.com/docker/docker/api/types/backend"
	"github.com/docker/docker/api/types/image"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
	"os"
	"strconv"
	"strings"
)

func RestoreContainer(ctx *svc.ServiceContext, filename string, taskID string) error {
	var failList []string
	var successList []string
	oldProgress := svc.TaskProgress{
		TaskID:     taskID,
		Percentage: 0,
		Message:    "",
		Name:       "",
		DetailMsg:  "",
		IsDone:     false,
	}
	oldProgress.Name = "恢复容器"
	fullPath, err := ResolveBackupPath(filename, ".json")
	if err != nil {
		logx.Errorf("Failed to resolve backup path: %v", err)
		oldProgress.Message = "非法文件名"
		oldProgress.DetailMsg = err.Error()
		oldProgress.IsDone = true
		ctx.UpdateProgress(taskID, oldProgress)
		return err
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		logx.Errorf("Failed to read file: %v", err)
		oldProgress.Percentage = 0
		oldProgress.Message = "读取文件失败或者未找到文件。请确认文件名仅由大小写字母、数字和短横线组成"
		oldProgress.DetailMsg = err.Error()
		oldProgress.IsDone = true
		ctx.UpdateProgress(taskID, oldProgress)
		return err
	}
	var configList []dockerBackend.ContainerCreateConfig
	err = json.Unmarshal(content, &configList)
	if err != nil {
		logx.Errorf("Failed to parse json: %v", err)
		oldProgress.Percentage = 0
		oldProgress.Message = "解析文件失败"
		oldProgress.DetailMsg = err.Error()
		oldProgress.IsDone = true
		ctx.UpdateProgress(taskID, oldProgress)
		return err
	}
	if len(configList) == 0 {
		oldProgress.Percentage = 100
		oldProgress.Message = "备份文件为空"
		oldProgress.DetailMsg = "备份文件中没有容器配置"
		oldProgress.IsDone = true
		ctx.UpdateProgress(taskID, oldProgress)
		return fmt.Errorf("备份文件为空")
	}

	for i, containerInfo := range configList {
		info := "正在恢复第" + strconv.Itoa(i+1) + "个容器"
		oldProgress.Percentage = int(float64(i) / float64(len(configList)) * 100)
		oldProgress.Message = info
		oldProgress.DetailMsg = info
		ctx.UpdateProgress(taskID, oldProgress)
		ctx.DockerClient.NegotiateAPIVersion(context.TODO())

		// 畸形备份不能让整个恢复 goroutine panic，更不能让进度永远卡住。
		if containerInfo.Config == nil || containerInfo.Config.Image == "" {
			detail := fmt.Sprintf("第 %d 个容器配置无效：缺少镜像信息", i+1)
			logx.Error(detail)
			failList = append(failList, detail)
			continue
		}
		imageRef := containerInfo.Config.Image
		// 本地已有镜像时不要拉取：本地 build 的镜像/私有镜像/离线环境都会因拉取失败而漏恢复。
		if _, _, inspectErr := ctx.DockerClient.ImageInspectWithRaw(context.TODO(), imageRef); inspectErr != nil {
			if pullErr := pullImageForRestore(ctx, imageRef, taskID); pullErr != nil {
				detail := imageRef + " 拉取镜像出现错误: " + pullErr.Error()
				logx.Error(detail)
				failList = append(failList, detail)
				continue
			}
		}
		_, err = ctx.DockerClient.ContainerCreate(context.TODO(), containerInfo.Config, containerInfo.HostConfig, containerInfo.NetworkingConfig, nil, containerInfo.Name)
		if err != nil {
			detail := containerInfo.Name + " 恢复失败: " + err.Error()
			logx.Error(detail)
			failList = append(failList, detail)
			continue
		}
		successList = append(successList, containerInfo.Name+" 恢复成功")
	}

	result := append(successList, failList...)
	oldProgress.Percentage = 100
	oldProgress.DetailMsg = strings.Join(result, ",\n")
	oldProgress.IsDone = true
	if len(failList) > 0 {
		oldProgress.Message = fmt.Sprintf("恢复完成，%d 个容器失败", len(failList))
		ctx.UpdateProgress(taskID, oldProgress)
		return fmt.Errorf("恢复完成，但 %d 个容器失败: %s", len(failList), strings.Join(failList, "; "))
	}
	oldProgress.Message = "恢复完成"
	ctx.UpdateProgress(taskID, oldProgress)
	return nil
}

// pullImageForRestore 拉取镜像并确保关闭响应体，避免连接/goroutine 泄漏。
func pullImageForRestore(ctx *svc.ServiceContext, ref string, taskID string) error {
	reader, err := ctx.DockerClient.ImagePull(context.TODO(), ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	return decodePullResp(reader, ctx, taskID)
}
