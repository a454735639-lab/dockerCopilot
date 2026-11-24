package utiles

import (
	"context"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/settings"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// 旧镜像清理（默认关闭，也可在界面里手动触发）：
//
//	PRUNE_OLD_IMAGES=1     开启（更新成功后自动触发）
//	PRUNE_KEEP=2           每个仓库保留最近 N 个 tag，默认 2
//	PRUNE_DANGLING=1       顺带清理无标签（<none>）镜像
//	PRUNE_DRY_RUN=1        只记录不删除
//
// 安全约束：任何仍被容器（含已停止）引用的镜像一律跳过；自身镜像不参与。

type PruneItem struct {
	ID   string `json:"id"`
	Repo string `json:"repo"`
	Tag  string `json:"tag"`
	Size int64  `json:"size"`
}

type PruneReport struct {
	Keep        int         `json:"keep"`
	DryRun      bool        `json:"dryRun"`
	Candidates  []PruneItem `json:"candidates"`
	Removed     []PruneItem `json:"removed"`
	SkippedUsed []string    `json:"skippedUsed"`
	FreedBytes  int64       `json:"freedBytes"`
}

func PruneEnabled() bool {
	return envBool("PRUNE_OLD_IMAGES")
}

// PruneOldImages 按环境变量配置执行一次清理（更新成功后自动调用）
func PruneOldImages(serviceContext *svc.ServiceContext) {
	report := PruneImagesByRepo(
		serviceContext,
		envInt("PRUNE_KEEP", 2),
		envBool("PRUNE_DANGLING"),
		envBool("PRUNE_DRY_RUN"),
	)
	logx.Infof("清理旧镜像：候选 %d，删除 %d，跳过占用 %d，释放 %d MB",
		len(report.Candidates), len(report.Removed), len(report.SkippedUsed), report.FreedBytes/1024/1024)
}

// PruneImagesByRepo 按仓库保留最近 keep 个版本，dryRun 时只返回候选不删除
func PruneImagesByRepo(serviceContext *svc.ServiceContext, keep int, pruneDangling bool, dryRun bool) PruneReport {
	if keep < 1 {
		keep = 1
	}
	report := PruneReport{
		Keep:        keep,
		DryRun:      dryRun,
		Candidates:  []PruneItem{},
		Removed:     []PruneItem{},
		SkippedUsed: []string{},
	}
	ctx := context.Background()

	containers, err := serviceContext.DockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		logx.Errorf("清理旧镜像：获取容器列表失败: %v", err)
		return report
	}
	inUse := make(map[string]bool, len(containers))
	keepByRepo := make(map[string]int)
	for _, c := range containers {
		inUse[c.ImageID] = true
		if k := module.LabelInt(c.Labels, module.LabelKeep, 0); k > 0 {
			if repo := imageRepository(c.Image); repo != "" && keepByRepo[repo] < k {
				keepByRepo[repo] = k
			}
		}
	}

	images, err := serviceContext.DockerClient.ImageList(ctx, image.ListOptions{})
	if err != nil {
		logx.Errorf("清理旧镜像：获取镜像列表失败: %v", err)
		return report
	}

	handle := func(img image.Summary, repo, tag string) {
		item := PruneItem{ID: img.ID, Repo: repo, Tag: tag, Size: img.Size}
		if inUse[img.ID] {
			report.SkippedUsed = append(report.SkippedUsed, repo+":"+tag)
			return
		}
		report.Candidates = append(report.Candidates, item)
		if dryRun {
			return
		}
		if _, removeErr := serviceContext.DockerClient.ImageRemove(ctx, img.ID, image.RemoveOptions{PruneChildren: true}); removeErr != nil {
			logx.Errorf("清理旧镜像：删除 %s 失败: %v", item.Repo+":"+item.Tag, removeErr)
			return
		}
		report.Removed = append(report.Removed, item)
		report.FreedBytes += img.Size
	}

	groups := make(map[string][]image.Summary)
	var dangling []image.Summary
	for _, img := range images {
		if len(img.RepoTags) == 0 {
			dangling = append(dangling, img)
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
		for _, img := range list[repoKeep:] {
			tag := "latest"
			if parts := strings.Split(img.RepoTags[0], ":"); len(parts) > 1 {
				tag = parts[len(parts)-1]
			}
			handle(img, repo, tag)
		}
	}

	if pruneDangling {
		for _, img := range dangling {
			handle(img, "<none>", "<none>")
		}
	}

	logx.Infof("清理旧镜像：仓库 %d 组，keep=%d，dryRun=%v", len(groups), keep, dryRun)
	return report
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
	switch strings.ToLower(strings.TrimSpace(settings.Str(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
