package container

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/onlyLTY/dockerCopilot/internal/utiles"
	"path/filepath"

	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type RestoreLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRestoreLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RestoreLogic {
	return &RestoreLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RestoreLogic) Restore(req *types.ContainerRestoreReq) (resp *types.Resp, err error) {
	resp = &types.Resp{}
	taskID := uuid.New().String()
	fileName := req.Filename
	fullPath, pathErr := utiles.ResolveBackupPath(fileName, ".json")
	if pathErr != nil {
		resp.Code = 400
		resp.Msg = pathErr.Error()
		resp.Data = map[string]interface{}{}
		return resp, pathErr
	}
	fileName = filepath.Base(fullPath)
	if filepath.Ext(fileName) != ".json" {
		err = fmt.Errorf("目前仅支持config备份恢复")
		resp.Code = 400
		resp.Msg = err.Error()
		resp.Data = map[string]interface{}{}
		return resp, err
	}
	go func() {
		// panic 也必须让进度结束，否则前端会永远轮询一个未完成的任务。
		defer func() {
			if r := recover(); r != nil {
				l.Errorf("Recovered from panic in restoreContainer: %v", r)
				l.svcCtx.UpdateProgress(taskID, svc.TaskProgress{
					TaskID:     taskID,
					Percentage: 100,
					Message:    "恢复失败（内部错误）",
					DetailMsg:  fmt.Sprintf("%v", r),
					Name:       "恢复容器",
					IsDone:     true,
				})
			}
		}()
		err := utiles.RestoreContainer(l.svcCtx, fileName, taskID)
		if err != nil {
			l.Errorf("Error in restoreContainer: %v", err)
		}
	}()
	resp.Code = 200
	resp.Msg = "success"
	resp.Data = map[string]interface{}{
		"taskID": taskID,
	}
	return resp, nil
}
