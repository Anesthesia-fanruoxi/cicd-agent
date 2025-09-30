package extractProduct

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cicd-agent/common"
)

// ExtractProductStep 解压产物步骤
type ExtractProductStep struct {
	project     string
	tag         string
	category    string
	ctx         context.Context
	zipFilePath string
}

// NewExtractProductStep 创建解压产物步骤
func NewExtractProductStep(project, tag, category string, ctx context.Context, zipFilePath string) *ExtractProductStep {
	return &ExtractProductStep{
		project:     project,
		tag:         tag,
		category:    category,
		ctx:         ctx,
		zipFilePath: zipFilePath,
	}
}

// Execute 执行解压产物
func (e *ExtractProductStep) Execute() error {
	common.AppLogger.Info(fmt.Sprintf("开始执行解压产物步骤: 项目=%s, 标签=%s, 分类=%s", e.project, e.tag, e.category))

	// 检查zip文件是否存在
	if _, err := os.Stat(e.zipFilePath); os.IsNotExist(err) {
		return fmt.Errorf("zip文件不存在: %s", e.zipFilePath)
	}

	// 创建解压目录
	extractDir := "/tmp/web-extract"
	if err := os.RemoveAll(extractDir); err != nil {
		common.AppLogger.Warning(fmt.Sprintf("清理解压目录失败: %v", err))
	}

	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return fmt.Errorf("创建解压目录失败: %v", err)
	}

	// 解压zip文件
	if err := e.unzipFile(e.zipFilePath, extractDir); err != nil {
		return fmt.Errorf("解压文件失败: %v", err)
	}

	common.AppLogger.Info(fmt.Sprintf("解压产物步骤执行完成: %s", e.zipFilePath))
	return nil
}

// unzipFile 解压zip文件
func (e *ExtractProductStep) unzipFile(src, dest string) error {
	// 打开zip文件
	reader, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("打开zip文件失败: %v", err)
	}
	defer reader.Close()

	// 解压每个文件
	for _, file := range reader.File {
		// 构建目标路径
		path := filepath.Join(dest, file.Name)

		// 安全检查，防止路径遍历攻击
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			common.AppLogger.Warning(fmt.Sprintf("跳过不安全的路径: %s", file.Name))
			continue
		}

		if file.FileInfo().IsDir() {
			// 创建目录
			if err := os.MkdirAll(path, file.FileInfo().Mode()); err != nil {
				return fmt.Errorf("创建目录失败: %v", err)
			}
			continue
		}

		// 创建父目录
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("创建父目录失败: %v", err)
		}

		// 解压文件
		if err := e.extractFile(file, path); err != nil {
			return fmt.Errorf("解压文件 %s 失败: %v", file.Name, err)
		}
	}

	common.AppLogger.Info(fmt.Sprintf("成功解压 %d 个文件到: %s", len(reader.File), dest))

	// 调试：列出解压后的目录结构
	if err := e.listExtractedFiles(dest); err != nil {
		common.AppLogger.Warning(fmt.Sprintf("列出解压文件失败: %v", err))
	}

	return nil
}

// extractFile 解压单个文件
func (e *ExtractProductStep) extractFile(file *zip.File, destPath string) error {
	// 打开zip中的文件
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	// 创建目标文件
	outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.FileInfo().Mode())
	if err != nil {
		return err
	}
	defer outFile.Close()

	// 复制文件内容
	_, err = io.Copy(outFile, rc)
	return err
}

// GetExtractDir 获取解压目录
func (e *ExtractProductStep) GetExtractDir() string {
	return "/tmp/web-extract"
}

// GetDistPath 获取要部署的源目录路径
func (e *ExtractProductStep) GetDistPath() string {
	extractDir := e.GetExtractDir()

	// 首先检查根目录下是否有dist子目录
	distPath := filepath.Join(extractDir, "dist")
	if _, err := os.Stat(distPath); err == nil {
		common.AppLogger.Info(fmt.Sprintf("找到dist子目录: %s", distPath))
		return distPath
	}

	// 如果根目录没有dist，搜索子目录中的dist
	if foundPath := e.findDistDirectory(extractDir); foundPath != "" {
		common.AppLogger.Info(fmt.Sprintf("在子目录中找到dist: %s", foundPath))
		return foundPath
	}

	// 如果找不到dist目录，直接使用解压根目录
	common.AppLogger.Info(fmt.Sprintf("未找到dist目录，使用解压根目录: %s", extractDir))
	return extractDir
}

// findDistDirectory 递归查找dist目录
func (e *ExtractProductStep) findDistDirectory(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		entryPath := filepath.Join(dir, entry.Name())

		// 如果当前目录名是dist，返回路径
		if entry.Name() == "dist" {
			return entryPath
		}

		// 递归搜索子目录（最多2层深度避免无限递归）
		if strings.Count(entryPath, string(os.PathSeparator))-strings.Count(dir, string(os.PathSeparator)) < 2 {
			if found := e.findDistDirectory(entryPath); found != "" {
				return found
			}
		}
	}

	return ""
}

// listExtractedFiles 列出解压后的文件结构（调试用）
func (e *ExtractProductStep) listExtractedFiles(dir string) error {
	common.AppLogger.Info(fmt.Sprintf("解压后的目录结构 (%s):", dir))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			common.AppLogger.Info(fmt.Sprintf("  [目录] %s/", entry.Name()))
			// 递归列出子目录（最多2层）
			subDir := filepath.Join(dir, entry.Name())
			subEntries, err := os.ReadDir(subDir)
			if err == nil && len(subEntries) <= 10 { // 避免输出过多
				for _, subEntry := range subEntries {
					if subEntry.IsDir() {
						common.AppLogger.Info(fmt.Sprintf("    [目录] %s/", subEntry.Name()))
					} else {
						common.AppLogger.Info(fmt.Sprintf("    [文件] %s", subEntry.Name()))
					}
				}
			}
		} else {
			common.AppLogger.Info(fmt.Sprintf("  [文件] %s", entry.Name()))
		}
	}

	return nil
}
