package backupCurrent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"cicd-agent/common"
	"cicd-agent/config"
)

// BackupCurrentStep 备份当前版本步骤
type BackupCurrentStep struct {
	project  string
	tag      string
	category string
	ctx      context.Context
}

// NewBackupCurrentStep 创建备份当前版本步骤
func NewBackupCurrentStep(project, tag, category string, ctx context.Context) *BackupCurrentStep {
	return &BackupCurrentStep{
		project:  project,
		tag:      tag,
		category: category,
		ctx:      ctx,
	}
}

// Execute 执行备份当前版本
func (b *BackupCurrentStep) Execute() error {
	common.AppLogger.Info(fmt.Sprintf("开始执行备份当前版本步骤: 项目=%s, 标签=%s, 分类=%s", b.project, b.tag, b.category))

	// 获取web目录和备份目录路径
	webPath := b.getWebPath()
	backupPath := b.getBackupPath()

	common.AppLogger.Info(fmt.Sprintf("Web目录: %s, 备份目录: %s", webPath, backupPath))

	// 删除旧的备份目录
	if err := b.removeOldBackup(backupPath); err != nil {
		common.AppLogger.Warning(fmt.Sprintf("删除旧备份失败: %v", err))
	}

	// 检查web目录是否存在
	if _, err := os.Stat(webPath); os.IsNotExist(err) {
		common.AppLogger.Info("web目录不存在，跳过备份（可能是首次部署）")
		return nil
	}

	// 执行备份
	if err := b.moveDirectory(webPath, backupPath); err != nil {
		return fmt.Errorf("备份web目录失败: %v", err)
	}

	common.AppLogger.Info(fmt.Sprintf("备份当前版本步骤执行完成: %s -> %s", webPath, backupPath))
	return nil
}

// getWebPath 获取web路径
func (b *BackupCurrentStep) getWebPath() string {
	if b.category != "" {
		// 有category: /www/scfq/manager
		basePath := config.AppConfig.GetWebPath(b.project)
		return filepath.Clean(filepath.Dir(basePath) + "/" + b.category)
	} else {
		// 无category: /www/scfq/web
		return config.AppConfig.GetWebPath(b.project)
	}
}

// getBackupPath 获取备份路径
func (b *BackupCurrentStep) getBackupPath() string {
	webPath := b.getWebPath()
	// /www/scfq/web -> /www/scfq/web_backup
	// /www/scfq/manager -> /www/scfq/manager_backup
	return webPath + "_backup"
}

// removeOldBackup 删除旧的备份目录
func (b *BackupCurrentStep) removeOldBackup(backupPath string) error {
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		// 备份目录不存在，无需删除
		return nil
	}

	common.AppLogger.Info(fmt.Sprintf("删除旧备份目录: %s", backupPath))
	return os.RemoveAll(backupPath)
}

// moveDirectory 移动目录
func (b *BackupCurrentStep) moveDirectory(src, dst string) error {
	common.AppLogger.Info(fmt.Sprintf("移动目录: %s -> %s", src, dst))

	// 创建目标目录的父目录
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("创建父目录失败: %v", err)
	}

	// 移动目录
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("移动目录失败: %v", err)
	}

	return nil
}

// GetBackupPath 获取备份路径（公共方法）
func (b *BackupCurrentStep) GetBackupPath() string {
	return b.getBackupPath()
}
