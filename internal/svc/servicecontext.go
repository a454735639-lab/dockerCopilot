package svc

import (
	"github.com/docker/docker/client"
	"github.com/onlyLTY/dockerCopilot/internal/config"
	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"sync"
	"time"
)

type ServiceContext struct {
	Config                     config.Config
	CookieCheckMiddleware      rest.Middleware
	Jwtuuid                    string
	BearerTokenCheckMiddleware rest.Middleware
	JwtSecret                  string
	PortainerJwt               string
	HubImageInfo               *module.ImageUpdateData
	IndexCheckMiddleware       rest.Middleware
	ProgressStore              ProgressStoreType
	DockerClient               *client.Client
	mu                         sync.Mutex
	progressFinished           map[string]time.Time // 已完成任务的完成时刻，用于清理
	lastProgressPurge          time.Time
}

const (
	progressRetention = time.Hour
	progressPurgeGap  = 5 * time.Minute
)

type TaskProgress struct {
	TaskID     string
	Percentage int
	Message    string
	Name       string
	DetailMsg  string
	IsDone     bool
}

type ProgressStoreType map[string]TaskProgress

func NewServiceContext(c config.Config) *ServiceContext {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logx.Errorf("Unable to create docker client: %s", err)
	}
	return &ServiceContext{
		Config:            c,
		HubImageInfo:      module.NewImageCheck(),
		ProgressStore:     make(ProgressStoreType),
		progressFinished:  make(map[string]time.Time),
		lastProgressPurge: time.Now(),
		DockerClient:      cli,
	}
}

func (ctx *ServiceContext) UpdateProgress(taskID string, progress TaskProgress) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.ProgressStore[taskID] = progress
	if ctx.progressFinished == nil {
		ctx.progressFinished = make(map[string]time.Time)
	}
	if progress.IsDone {
		ctx.progressFinished[taskID] = time.Now()
	}
	// 已完成任务保留一段时间后清除，避免长期运行内存无限增长。
	now := time.Now()
	if now.Sub(ctx.lastProgressPurge) >= progressPurgeGap {
		ctx.lastProgressPurge = now
		for id, finishedAt := range ctx.progressFinished {
			if now.Sub(finishedAt) >= progressRetention {
				delete(ctx.ProgressStore, id)
				delete(ctx.progressFinished, id)
			}
		}
	}
}

func (ctx *ServiceContext) GetProgress(taskID string) (TaskProgress, bool) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	progress, ok := ctx.ProgressStore[taskID]
	return progress, ok
}
