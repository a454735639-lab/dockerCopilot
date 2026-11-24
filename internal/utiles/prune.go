package utiles

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// 旧镜像清理（默认关闭）：
//
//	PRUNE_OLD_IMAGES=1     开启（更新成功后触发）
//	PRUNE_KEEP=2           每个仓库保留最近 N 个 tag，默认 2
//	PRUNE_DANGLING=1       顺带清理无标签（<none>）镜像
//	PRUNE_DRY_RUN=1        只写日志不删除，先看清楚会删什么
//
// 安全约束：任何仍被容器（含已停止）引用的镜像一律跳过；自身镜像不参与。

func PruneEnabled() bool {
	return envBool("PRUNE_OLD_IMAGES")
}

func PruneOldImages(serviceContext *svc.ServiceContext) {
	keep := envInt("PRUNE_KEEP", 2)
	dryRun := envBool("PRUNE_DRY_RUN")
	pruneDangling := envBool("PRUNE_DANGLING")
	ctx := context.Background()

	containers, err := serviceContext.DockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		logx.Errorf("清理旧镜像：获取容器列表失败: %v", err)
		return
	}
	inUse := make(map[string]bool, len(containers))
	keepByRepo := make(map[string]int)
	for _, c := range containers {
		inUse[c.ImageID] = true
		// 容器标签 dockercopilot.keep=N 可以抬高它所用仓库的保留数量
		if k := module.LabelInt(c.Labels, module.LabelKeep, 0); k > 0 {
			if repo := imageRepository(c.Image); repo != "" && keepByRepo[repo] < k {
				keepByRepo[repo] = k
			}
		}
	}

	images, err := serviceContext.DockerClient.ImageList(ctx, image.ListOptions{})
	if err != nil {
		logx.Errorf("清理旧镜像：获取镜像列表失败: %v", err)
		return
	}

	remove := func(img image.Summary, reason string) {
		short := img.ID
		if len(short) > 19 {
			short = short[:19]
		}
		if inUse[img.ID] {
			return
		}
		if dryRun {
			logx.Infof("清理旧镜像（试运行）：将删除 %s，%s", short, reason)
			return
		}
		if _, removeErr := serviceContext.DockerClient.ImageRemove(ctx, img.ID, image.RemoveOptions{PruneChildren: true}); removeErr != nil {
			logx.Errorf("清理旧镜像：删除 %s 失败: %v", short, removeErr)
			return
		}
		logx.Infof("清理旧镜像：已删除 %s（%s）", short, reason)
	}

	groups := make(map[string][]image.Summary)
	for _, img := range images {
		if len(img.RepoTags) == 0 {
			if pruneDangling {
				remove(img, "无标签镜像")
			}
			continue
		}
		repo := imageRepository(img.RepoTags[0])
		if strings.Contains(strings.ToLower(repo), "dockercopilot") {
			continue
		}
		groups[repo] = append(groups[repo], img)
	}

	for repo, list := range groups {
		repoKeep := keep
		if k := keepByRepo[repo]; k > repoKeep {
			repoKeep = k
		}
		if len(list) <= repoKeep {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Created > list[j].Created })
		reason := "仓库 " + repo + " 仅保留最近 " + strconv.Itoa(repoKeep) + " 个版本"
		for _, img := range list[repoKeep:] {
			remove(img, reason)
		}
	}
}

// imageRepository 从 "host:port/repo:tag" 里取出仓库部分（去掉 tag）
func imageRepository(ref string) string {
	if i := strings.LastIndex(ref, "@"); i > 0 {
		ref = ref[:i]
	}
	slash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > slash {
		return ref[:colon]
	}
	return ref
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
