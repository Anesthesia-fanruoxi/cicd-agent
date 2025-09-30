package deployNew

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cicd-agent/common"
	"cicd-agent/config"
)

// DeployNewStep 部署新版本步骤
type DeployNewStep struct {
	project  string
	tag      string
	category string
	ctx      context.Context
	distPath string
}

// NewDeployNewStep 创建部署新版本步骤
func NewDeployNewStep(project, tag, category string, ctx context.Context, distPath string) *DeployNewStep {
	return &DeployNewStep{
		project:  project,
		tag:      tag,
		category: category,
		ctx:      ctx,
		distPath: distPath,
	}
}

// Execute 执行部署新版本
func (d *DeployNewStep) Execute() error {
	common.AppLogger.Info(fmt.Sprintf("开始执行部署新版本步骤: 项目=%s, 标签=%s, 分类=%s", d.project, d.tag, d.category))

	// 获取目标web路径
	webPath := d.getWebPath()

	// 检查dist目录是否存在
	if _, err := os.Stat(d.distPath); os.IsNotExist(err) {
		return fmt.Errorf("dist目录不存在: %s", d.distPath)
	}

	common.AppLogger.Info(fmt.Sprintf("部署路径: %s -> %s", d.distPath, webPath))

	// 创建目标目录的父目录
	if err := os.MkdirAll(filepath.Dir(webPath), 0755); err != nil {
		return fmt.Errorf("创建父目录失败: %v", err)
	}

	// 移动dist目录到目标位置
	if err := d.moveDirectory(d.distPath, webPath); err != nil {
		return fmt.Errorf("部署新版本失败: %v", err)
	}

	// 验证部署结果
	if err := d.verifyDeployment(webPath); err != nil {
		return fmt.Errorf("部署验证失败: %v", err)
	}

	common.AppLogger.Info(fmt.Sprintf("部署新版本步骤执行完成: %s", webPath))
	return nil
}

// moveDirectory 移动目录
func (d *DeployNewStep) moveDirectory(src, dst string) error {
	common.AppLogger.Info(fmt.Sprintf("移动目录: %s -> %s", src, dst))

	// 如果目标目录已存在，先删除
	if _, err := os.Stat(dst); err == nil {
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("删除目标目录失败: %v", err)
		}
	}

	// 移动目录
	if err := os.Rename(src, dst); err != nil {
		// 如果跨文件系统移动失败，则使用复制+删除的方式
		if err := d.copyDirectory(src, dst); err != nil {
			return fmt.Errorf("复制目录失败: %v", err)
		}

		// 删除源目录
		if err := os.RemoveAll(src); err != nil {
			common.AppLogger.Warning(fmt.Sprintf("删除源目录失败: %v", err))
		}
	}

	return nil
}

// copyDirectory 复制目录
func (d *DeployNewStep) copyDirectory(src, dst string) error {
	// 获取源目录信息
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// 创建目标目录
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	// 读取源目录内容
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	// 复制每个文件/目录
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			// 递归复制子目录
			if err := d.copyDirectory(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// 复制文件
			if err := d.copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile 复制文件
func (d *DeployNewStep) copyFile(src, dst string) error {
	// 打开源文件
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// 获取源文件信息
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	// 创建目标文件
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// 复制文件内容
	_, err = io.Copy(dstFile, srcFile)
	return err
}

// verifyDeployment 验证部署结果
func (d *DeployNewStep) verifyDeployment(webPath string) error {
	// 检查web目录是否存在
	if _, err := os.Stat(webPath); os.IsNotExist(err) {
		return fmt.Errorf("部署后web目录不存在: %s", webPath)
	}

	// 检查目录是否为空
	entries, err := os.ReadDir(webPath)
	if err != nil {
		return fmt.Errorf("读取web目录失败: %v", err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("部署后web目录为空: %s", webPath)
	}

	common.AppLogger.Info(fmt.Sprintf("部署验证成功，web目录包含 %d 个文件/目录", len(entries)))
	return nil
}

// getWebPath 获取web路径
func (d *DeployNewStep) getWebPath() string {
	if d.category != "" {
		// 有category: /www/scfq/manager
		basePath := config.AppConfig.GetWebPath(d.project)
		return filepath.Clean(filepath.Dir(basePath) + "/" + d.category)
	} else {
		// 无category: /www/scfq/web
		return config.AppConfig.GetWebPath(d.project)
	}
}

// GetWebPath 获取web路径（公共方法）
func (d *DeployNewStep) GetWebPath() string {
	return d.getWebPath()
}
