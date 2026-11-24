package container

import (
	"context"
	"github.com/google/uuid"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/onlyLTY/dockerCopilot/internal/utiles"
	"github.com/zeromicro/go-zero/core/logx"
	"os"
	"sync"
)

// 同一容器的更新必须串行：连点两下会并发执行 stop/rename/create，最后留下无主容器或创建冲突
var updatingContainers sync.Map

type UpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateLogic {
	return &UpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateLogic) Update(req *types.ContainerUpdateReq) (resp *types.Resp, err error) {
	resp = &types.Resp{}
	if _, loading := updatingContainers.LoadOrStore(req.Id, true); loading {
		resp.Code = 409
		resp.Msg = "该容器正在更新中，请等待当前任务完成"
		return resp, nil
	}
	taskID := uuid.New().String()
	go func() {
		defer updatingContainers.Delete(req.Id)
		// Catch any panic and log the error
		defer func() {
			if r := recover(); r != nil {
				l.Errorf("Recovered from panic in UpdateContainer: %v", r)
			}
		}()
		imageNameAndTag := req.ImageNameAndTag
		delOldContainer := os.Getenv("DelOldContainer") != "false"
		err := utiles.UpdateContainer(l.svcCtx, req.Id, req.ContainerName, imageNameAndTag, delOldContainer, taskID)
		if err != nil {
			l.Errorf("Error in UpdateContainer: %v", err)
		}
	}()
	resp.Code = 200
	resp.Msg = "success"
	resp.Data = map[string]string{"taskID": taskID}
	return resp, nil
}
