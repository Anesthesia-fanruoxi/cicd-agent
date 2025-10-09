package common

import (
	"os"
	"path/filepath"
	"time"
)

// LogRetentionConfig 日志保留配置
type LogRetentionConfig struct {
	MaxDays int // 保留天数
}

// DefaultLogRetention 默认日志保留配置
var DefaultLogRetention = LogRetentionConfig{
	MaxDays: 7, // 默认保留7天
}

// CleanupOldLogs 清理过期的日志目录
func CleanupOldLogs(maxDays int) error {
	logsDir := "logs"

	// 检查logs目录是否存在
	if _, err := os.Stat(logsDir); os.IsNotExist(err) {
		return nil
	}

	cutoffTime := time.Now().AddDate(0, 0, -maxDays)
	AppLogger.Info("开始清理日志，保留天数:", maxDays)

	// 遍历logs目录
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return err
	}

	deletedCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(logsDir, entry.Name())

		// 获取目录信息
		info, err := entry.Info()
		if err != nil {
			AppLogger.Warning("获取目录信息失败:", dirPath, err)
			continue
		}

		// 检查目录修改时间
		if info.ModTime().Before(cutoffTime) {
			if err := os.RemoveAll(dirPath); err != nil {
				AppLogger.Error("删除日志目录失败:", dirPath, err)
			} else {
				deletedCount++
				AppLogger.Debug("删除过期日志目录:", dirPath)
			}
		}
	}

	if deletedCount > 0 {
		AppLogger.Info("日志清理完成，删除目录数:", deletedCount)
	}

	return nil
}

// StartLogCleanupRoutine 启动日志清理定时任务
func StartLogCleanupRoutine(maxDays int) {
	// 启动时清理一次
	go func() {
		if err := CleanupOldLogs(maxDays); err != nil {
			AppLogger.Error("日志清理失败:", err)
		}
	}()

	// 每天凌晨2点清理
	go func() {
		for {
			now := time.Now()
			// 计算下次凌晨2点的时间
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 2, 0, 0, 0, now.Location())
			duration := next.Sub(now)

			time.Sleep(duration)

			if err := CleanupOldLogs(maxDays); err != nil {
				AppLogger.Error("定时日志清理失败:", err)
			}
		}
	}()

	AppLogger.Info("日志清理定时任务已启动")
}
