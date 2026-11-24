package system

import (
	"context"

	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/onlyLTY/dockerCopilot/internal/utiles"
	"github.com/zeromicro/go-zero/core/logx"
)

type SystemLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSystemLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SystemLogic {
	return &SystemLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// DF 汇总镜像/容器/卷/构建缓存占用
func (l *SystemLogic) DF() (resp *types.Resp, err error) {
	data, err := utiles.DiskUsageSummary(l.svcCtx)
	if err != nil {
		l.Errorf("获取磁盘占用失败: %v", err)
		return &types.Resp{Code: 500, Msg: "获取磁盘占用失败：" + err.Error(), Data: map[string]string{}}, nil
	}
	return &types.Resp{Code: 200, Msg: "success", Data: data}, nil
}

// Prune 清理构建缓存 / 悬空镜像
func (l *SystemLogic) Prune(req *types.SystemPruneReq) (resp *types.Resp, err error) {
	if !req.BuildCache && !req.DanglingImages {
		return &types.Resp{Code: 400, Msg: "没有选择要清理的内容", Data: map[string]string{}}, nil
	}
	data, err := utiles.PruneSystem(l.svcCtx, req.BuildCache, req.DanglingImages)
	if err != nil {
		return &types.Resp{Code: 500, Msg: "清理失败：" + err.Error(), Data: map[string]string{}}, nil
	}
	return &types.Resp{Code: 200, Msg: "success", Data: data}, nil
}
