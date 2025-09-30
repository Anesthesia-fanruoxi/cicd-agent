package pullOnline

import (
	"context"
	"fmt"
	"os/exec"
	"sync"

	"cicd-agent/common"
)

// ImagePuller 镜像拉取器
type ImagePuller struct {
	taskID string
}

// NewImagePuller 创建镜像拉取器
func NewImagePuller(taskID string) *ImagePuller {
	return &ImagePuller{taskID: taskID}
}

// PullImages 并发拉取镜像（可取消）
func (p *ImagePuller) PullImages(ctx context.Context, images []string) error {
	if len(images) == 0 {
		return fmt.Errorf("镜像列表为空")
	}

	maxConcurrency := p.calculatePullConcurrency(len(images))
	common.AppLogger.Info(fmt.Sprintf("拉取镜像: 总数=%d, 并发数=%d", len(images), maxConcurrency))

	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	errChan := make(chan error, len(images))

	for _, img := range images {
		wg.Add(1)
		go func(image string) {
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

			if err := p.pullSingleImage(ctx, image); err != nil {
				errChan <- err
			}
		}(img)
	}

	wg.Wait()
	close(errChan)

	// 检查是否有错误
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	common.AppLogger.Info(fmt.Sprintf("所有镜像拉取完成: %d个", len(images)))
	return nil
}

// pullSingleImage 拉取单个镜像
func (p *ImagePuller) pullSingleImage(ctx context.Context, image string) error {
	common.AppLogger.Info(fmt.Sprintf("开始拉取镜像: %s", image))

	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// 检查是否是上下文取消导致的错误
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("拉取镜像 %s 被取消", image)
		}
		return fmt.Errorf("拉取镜像 %s 失败: %v, 输出: %s", image, err, string(output))
	}

	common.AppLogger.Info(fmt.Sprintf("成功拉取镜像: %s", image))
	return nil
}

// calculatePullConcurrency 计算拉取并发数
func (p *ImagePuller) calculatePullConcurrency(imageCount int) int {
	// 直接根据服务数量设置线程数，最大不超过20个线程
	const maxConcurrency = 20
	const minConcurrency = 1

	// 如果服务数量小于等于最大并发数，使用服务数量作为并发数
	if imageCount <= maxConcurrency {
		if imageCount < minConcurrency {
			return minConcurrency
		}
		return imageCount
	}

	// 如果服务数量超过最大并发数，使用最大并发数
	return maxConcurrency
}

// PullImages 拉取镜像列表（包装函数）
func PullImages(ctx context.Context, images []string) error {
	// 使用空的taskID，因为这是包装函数
	puller := NewImagePuller("")
	return puller.PullImages(ctx, images)
}
