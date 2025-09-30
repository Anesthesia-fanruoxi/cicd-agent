package common

import (
	"context"
	"sync"
)

// 任务取消管理器
var (
	taskCtxMu  sync.Mutex
	taskCtxMap = make(map[string]context.CancelFunc)
)

// CreateTaskContext 为任务创建可取消上下文
func CreateTaskContext(taskID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	taskCtxMu.Lock()
	taskCtxMap[taskID] = cancel
	taskCtxMu.Unlock()
	return ctx, cancel
}

// CancelTask 取消指定任务
func CancelTask(taskID string) bool {
	taskCtxMu.Lock()
	defer taskCtxMu.Unlock()
	if cancel, ok := taskCtxMap[taskID]; ok {
		cancel()
		delete(taskCtxMap, taskID)
		return true
	}
	return false
}

// CleanupTask 在任务完成后清理
func CleanupTask(taskID string) {
	taskCtxMu.Lock()
	delete(taskCtxMap, taskID)
	taskCtxMu.Unlock()
}
