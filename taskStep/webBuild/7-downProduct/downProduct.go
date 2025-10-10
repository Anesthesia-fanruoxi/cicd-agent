package downProduct

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"cicd-agent/common"
	"cicd-agent/config"
)

// DownProductStep 下载产物步骤
type DownProductStep struct {
	project    string
	tag        string
	category   string
	ctx        context.Context
	taskLogger *common.TaskLogger
}

// NewDownProductStep 创建下载产物步骤
func NewDownProductStep(project, tag, category string, ctx context.Context, taskLogger *common.TaskLogger) *DownProductStep {
	return &DownProductStep{
		project:    project,
		tag:        tag,
		category:   category,
		ctx:        ctx,
		taskLogger: taskLogger,
	}
}

// Execute 执行下载产物
func (d *DownProductStep) Execute() error {
	logMsg := fmt.Sprintf("开始执行下载产物步骤: 项目=%s, 标签=%s, 分类=%s", d.project, d.tag, d.category)
	common.AppLogger.Info(logMsg)
	if d.taskLogger != nil {
		d.taskLogger.WriteStep("downProduct", "INFO", logMsg)
	}

	// 构建产物名称: name-tag.zip
	var productName string
	if d.category != "" {
		productName = fmt.Sprintf("%s-%s-%s.zip", d.project, d.category, d.tag)
	} else {
		productName = fmt.Sprintf("%s-%s.zip", d.project, d.tag)
	}

	// 从配置文件获取下载URL
	baseURL := config.AppConfig.GetWebDownloadURL()
	baseDir := config.AppConfig.GetWebDownloadDir()
	downloadURL := fmt.Sprintf("%s/%s/%s", baseURL, baseDir, productName)

	if d.taskLogger != nil {
		d.taskLogger.WriteStep("downProduct", "INFO", fmt.Sprintf("开始下载产物: %s", downloadURL))
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(d.ctx, "GET", downloadURL, nil)
	if err != nil {
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("downProduct", "ERROR", fmt.Sprintf("创建HTTP请求失败: %v", err))
		}
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("downProduct", "ERROR", fmt.Sprintf("HTTP请求失败: %v", err))
		}
	}
	defer resp.Body.Close()

	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("downProduct", "ERROR", fmt.Sprintf("下载失败，HTTP状态码: %d", resp.StatusCode))
		}
	}

	// 创建本地保存目录
	downloadDir := "/tmp/web-products"
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("downProduct", "ERROR", fmt.Sprintf("创建下载目录失败: %v", err))
		}
	}

	// 本地文件路径
	localFilePath := filepath.Join(downloadDir, productName)

	// 创建本地文件
	file, err := os.Create(localFilePath)
	if err != nil {
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("downProduct", "ERROR", fmt.Sprintf("创建本地文件失败: %v", err))
		}
	}
	defer file.Close()

	// 下载文件内容
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("downProduct", "ERROR", fmt.Sprintf("写入文件失败: %v", err))
		}
	}

	// 获取文件大小
	fileInfo, err := file.Stat()
	if err != nil {
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("downProduct", "ERROR", fmt.Sprintf("获取文件信息失败: %v", err))
		}
	} else {
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("downProduct", "INFO", fmt.Sprintf("产物下载成功: %s (大小: %d bytes)", localFilePath, fileInfo.Size()))
		}
	}

	if d.taskLogger != nil {
		d.taskLogger.WriteStep("downProduct", "INFO", fmt.Sprintf("下载产物步骤执行完成: %s", productName))
	}
	return nil
}

// GetLocalFilePath 获取本地文件路径
func (d *DownProductStep) GetLocalFilePath() string {
	var productName string
	if d.category != "" {
		productName = fmt.Sprintf("%s-%s-%s.zip", d.project, d.category, d.tag)
	} else {
		productName = fmt.Sprintf("%s-%s.zip", d.project, d.tag)
	}
	return filepath.Join("/tmp/web-products", productName)
}

// GetTargetWebPath 获取目标web路径
func (d *DownProductStep) GetTargetWebPath() string {
	return config.AppConfig.GetWebPath(d.project)
}
