package utiles

import (
	"context"
	"strings"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// PullImage 拉取镜像，进度写入 ProgressStore，前端轮询 /api/progress/:taskid
func PullImage(serviceContext *svc.ServiceContext, ref string, taskID string) error {
	ref = strings.TrimSpace(ref)
	serviceContext.UpdateProgress(taskID, svc.TaskProgress{
		TaskID:     taskID,
		Name:       ref,
		Percentage: 5,
		Message:    "正在拉取镜像",
		DetailMsg:  "连接 registry",
		IsDone:     false,
	})

	reader, err := serviceContext.DockerClient.ImagePull(context.Background(), ref, image.PullOptions{})
	if err != nil {
		serviceContext.UpdateProgress(taskID, svc.TaskProgress{
			TaskID:     taskID,
			Name:       ref,
			Percentage: 100,
			Message:    "拉取失败",
			DetailMsg:  err.Error(),
			IsDone:     true,
		})
		logx.Errorf("拉取镜像失败 %s: %v", ref, err)
		return err
	}

	if err := decodePullResp(reader, serviceContext, taskID); err != nil {
		return err
	}

	serviceContext.UpdateProgress(taskID, svc.TaskProgress{
		TaskID:     taskID,
		Name:       ref,
		Percentage: 100,
		Message:    "拉取完成",
		DetailMsg:  "镜像已就绪",
		IsDone:     true,
	})
	return nil
}

// DiskUsageSummary 汇总 docker system df 的四类占用
func DiskUsageSummary(serviceContext *svc.ServiceContext) (map[string]interface{}, error) {
	usage, err := serviceContext.DockerClient.DiskUsage(context.Background(), dockerTypes.DiskUsageOptions{})
	if err != nil {
		return nil, err
	}

	var imagesSize, containersSize, volumesSize, buildCacheSize int64
	for _, img := range usage.Images {
		imagesSize += img.Size
	}
	for _, c := range usage.Containers {
		containersSize += c.SizeRw
	}
	for _, v := range usage.Volumes {
		volumesSize += v.UsageData.Size
	}
	for _, bc := range usage.BuildCache {
		buildCacheSize += bc.Size
	}

	return map[string]interface{}{
		"layersSize":      usage.LayersSize,
		"imagesSize":      imagesSize,
		"imagesCount":     len(usage.Images),
		"containersSize":  containersSize,
		"containersCount": len(usage.Containers),
		"volumesSize":     volumesSize,
		"volumesCount":    len(usage.Volumes),
		"buildCacheSize":  buildCacheSize,
		"buildCacheCount": len(usage.BuildCache),
	}, nil
}

// PruneSystem 清理构建缓存与悬空镜像，返回释放量
func PruneSystem(serviceContext *svc.ServiceContext, buildCache bool, danglingImages bool) (map[string]interface{}, error) {
	ctx := context.Background()
	var freed int64
	result := map[string]interface{}{"buildCacheFreed": int64(0), "imagesFreed": int64(0)}

	if buildCache {
		report, err := serviceContext.DockerClient.BuildCachePrune(ctx, dockerTypes.BuildCachePruneOptions{All: true})
		if err != nil {
			logx.Errorf("清理构建缓存失败: %v", err)
		} else if report != nil {
			result["buildCacheFreed"] = int64(report.SpaceReclaimed)
			freed += int64(report.SpaceReclaimed)
		}
	}

	if danglingImages {
		report, err := serviceContext.DockerClient.ImagesPrune(ctx, filters.NewArgs(filters.Arg("dangling", "true")))
		if err != nil {
			logx.Errorf("清理悬空镜像失败: %v", err)
		} else {
			result["imagesDeleted"] = len(report.ImagesDeleted)
			result["imagesFreed"] = int64(report.SpaceReclaimed)
			freed += int64(report.SpaceReclaimed)
		}
	}

	result["freedBytes"] = freed
	return result, nil
}
