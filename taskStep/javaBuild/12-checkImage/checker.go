package checkImage

import (
	"cicd-agent/common"
	"cicd-agent/config"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ImageChecker 镜像检查器
type ImageChecker struct {
	taskID string
}

// NewImageChecker 创建镜像检查器
func NewImageChecker(taskID string) *ImageChecker {
	return &ImageChecker{taskID: taskID}
}

// CheckImageExistsInHarbor 检查镜像在Harbor中是否存在
func (c *ImageChecker) CheckImageExistsInHarbor(ctx context.Context, projectName, imageName, tag string) (bool, error) {
	harborConfig := config.AppConfig.Harbor

	// 构建Harbor API URL
	url := fmt.Sprintf("https://%s/api/v2.0/projects/%s/repositories/%s/artifacts/%s/tags",
		harborConfig.Offline, projectName, imageName, tag)

	common.AppLogger.Info(fmt.Sprintf("检查Harbor镜像: %s", url))

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置基本认证
	req.SetBasicAuth(harborConfig.OfflineUser, harborConfig.OfflinePassword)

	// 创建HTTP客户端
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("请求Harbor失败: %v", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	exists := resp.StatusCode == 200
	common.AppLogger.Info(fmt.Sprintf("镜像 %s/%s:%s 在Harbor中存在状态: %v (状态码: %d)",
		projectName, imageName, tag, exists, resp.StatusCode))

	return exists, nil
}

// CheckImagesExistInHarbor 批量检查镜像在Harbor中是否存在
func (c *ImageChecker) CheckImagesExistInHarbor(ctx context.Context, images []string, projectName, tag string) (map[string]bool, []string, error) {
	result := make(map[string]bool)
	var failedImages []string
	var mu sync.Mutex

	// 先从镜像全名中提取镜像名并去重
	uniqueNames := make(map[string]struct{})
	var imageNames []string
	for _, img := range images {
		name := img
		if strings.Contains(img, "/") {
			parts := strings.Split(img, "/")
			name = parts[len(parts)-1]
		}
		if strings.Contains(name, ":") {
			name = strings.Split(name, ":")[0]
		}
		if _, seen := uniqueNames[name]; !seen {
			uniqueNames[name] = struct{}{}
			imageNames = append(imageNames, name)
		}
	}

	// 计算并发数，最大20个
	maxConcurrency := 20
	if len(imageNames) < maxConcurrency {
		maxConcurrency = len(imageNames)
	}

	common.AppLogger.Info(fmt.Sprintf("检查Harbor镜像: 总数=%d, 并发数=%d", len(imageNames), maxConcurrency))

	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	errChan := make(chan error, len(imageNames))

	for _, imageName := range imageNames {
		wg.Add(1)
		go func(imgName string) {
			defer wg.Done()

			// 获取信号量
			select {
			case <-ctx.Done():
				return
			case semaphore <- struct{}{}:
			}
			defer func() { <-semaphore }()

			// 若已取消，直接返回
			select {
			case <-ctx.Done():
				return
			default:
			}

			exists, err := c.CheckImageExistsInHarbor(ctx, projectName, imgName, tag)

			mu.Lock()
			if err != nil {
				common.AppLogger.Error(fmt.Sprintf("检查镜像 %s 失败: %v", imgName, err))
				result[imgName] = false
				failedImages = append(failedImages, imgName)
			} else {
				result[imgName] = exists
				if !exists {
					failedImages = append(failedImages, imgName)
				}
			}
			mu.Unlock()

			if err != nil {
				errChan <- err
			}
		}(imageName)
	}

	wg.Wait()
	close(errChan)

	// 检查是否有错误
	if len(errChan) > 0 {
		return result, failedImages, <-errChan
	}

	return result, failedImages, nil
}

// CheckImages 检查镜像列表（在Harbor中检查）
func CheckImages(ctx context.Context, images []string, projectName string, tag string, taskID string) error {
	if len(images) == 0 {
		common.AppLogger.Info("没有需要检查的镜像")
		return nil
	}

	checker := NewImageChecker(taskID)

	common.AppLogger.Info(fmt.Sprintf("开始检查Harbor镜像，项目: %s, 标签: %s", projectName, tag))

	// 批量检查镜像
	result, failedImages, err := checker.CheckImagesExistInHarbor(ctx, images, projectName, tag)
	if err != nil {
		return fmt.Errorf("批量检查镜像失败: %v", err)
	}

	// 输出检查结果
	successCount := 0
	for imageName, exists := range result {
		if exists {
			common.AppLogger.Info(fmt.Sprintf("✓ 镜像 %s 在Harbor中存在", imageName))
			successCount++
		} else {
			common.AppLogger.Warning(fmt.Sprintf("✗ 镜像 %s 在Harbor中不存在", imageName))
		}
	}

	common.AppLogger.Info(fmt.Sprintf("镜像检查完成: 总数=%d, 成功=%d, 失败=%d",
		len(images), successCount, len(failedImages)))

	// 如果有失败的镜像，返回错误
	if len(failedImages) > 0 {
		return fmt.Errorf("以下镜像在Harbor中不存在: %v", failedImages)
	}

	return nil
}
