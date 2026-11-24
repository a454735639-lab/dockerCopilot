package image

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/google/uuid"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/onlyLTY/dockerCopilot/internal/utiles"
	"github.com/zeromicro/go-zero/core/logx"
)

// ---------------- 拉取镜像 ----------------

type PullLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPullLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PullLogic {
	return &PullLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *PullLogic) Pull(req *types.ImagePullReq) (resp *types.Resp, err error) {
	ref := strings.TrimSpace(req.Image)
	if ref == "" {
		return &types.Resp{Code: 400, Msg: "镜像名不能为空", Data: map[string]string{}}, nil
	}
	taskID := uuid.New().String()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				l.Errorf("拉取镜像 panic: %v", r)
				l.svcCtx.UpdateProgress(taskID, svc.TaskProgress{
					TaskID: taskID, Name: ref, Percentage: 100,
					Message: "拉取失败", DetailMsg: "内部错误", IsDone: true,
				})
			}
		}()
		if pullErr := utiles.PullImage(l.svcCtx, ref, taskID); pullErr != nil {
			l.Errorf("拉取镜像失败 %s: %v", ref, pullErr)
		}
	}()
	return &types.Resp{Code: 200, Msg: "success", Data: map[string]string{"taskID": taskID, "image": ref}}, nil
}

// ---------------- 镜像详情 ----------------

type DetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DetailLogic {
	return &DetailLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *DetailLogic) Detail(id string) (resp *types.Resp, err error) {
	if strings.TrimSpace(id) == "" {
		return &types.Resp{Code: 400, Msg: "缺少镜像 ID", Data: map[string]string{}}, nil
	}
	info, _, inspectErr := l.svcCtx.DockerClient.ImageInspectWithRaw(context.Background(), id)
	if inspectErr != nil {
		return &types.Resp{Code: 404, Msg: "镜像不存在或已被删除", Data: map[string]string{}}, nil
	}

	used := make([]map[string]string, 0)
	if list, listErr := l.svcCtx.DockerClient.ContainerList(context.Background(), container.ListOptions{All: true}); listErr == nil {
		for _, c := range list {
			if c.ImageID != info.ID {
				continue
			}
			name := ""
			if len(c.Names) > 0 {
				name = strings.TrimPrefix(c.Names[0], "/")
			}
			used = append(used, map[string]string{"name": name, "state": c.State})
		}
	}

	config := map[string]interface{}{}
	if info.Config != nil {
		config["entrypoint"] = info.Config.Entrypoint
		config["cmd"] = info.Config.Cmd
		config["env"] = info.Config.Env
		config["workingDir"] = info.Config.WorkingDir
		config["user"] = info.Config.User
		config["labels"] = info.Config.Labels
		config["exposedPorts"] = info.Config.ExposedPorts
	}

	return &types.Resp{Code: 200, Msg: "success", Data: map[string]interface{}{
		"id":           info.ID,
		"repoTags":     info.RepoTags,
		"repoDigests":  info.RepoDigests,
		"created":      info.Created,
		"size":         info.Size,
		"architecture": info.Architecture,
		"os":           info.Os,
		"author":       info.Author,
		"layers":       len(info.RootFS.Layers),
		"config":       config,
		"usedBy":       used,
	}}, nil
}

// ---------------- 按仓库清理 ----------------

type PruneLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPruneLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PruneLogic {
	return &PruneLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *PruneLogic) Prune(req *types.ImagePruneReq) (resp *types.Resp, err error) {
	keep := req.Keep
	if keep <= 0 {
		keep = 2
	}
	report := utiles.PruneImagesByRepo(l.svcCtx, keep, req.Dangling, req.DryRun)
	return &types.Resp{Code: 200, Msg: "success", Data: report}, nil
}
