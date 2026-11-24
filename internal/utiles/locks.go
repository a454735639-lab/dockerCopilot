package utiles

import "sync"

// 同一容器的更新（手动点、自动任务）必须串行：
// 并发执行 stop/rename/create 会留下无主容器或创建冲突。
var updatingContainers sync.Map

// TryLockContainerUpdate 抢到该容器的更新权返回 true；已在更新中返回 false。
func TryLockContainerUpdate(id string) bool {
	_, loading := updatingContainers.LoadOrStore(id, true)
	return !loading
}

// UnlockContainerUpdate 释放该容器的更新权。
func UnlockContainerUpdate(id string) {
	updatingContainers.Delete(id)
}
