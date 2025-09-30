package pushLocal

import (
	"context"
	"fmt"
	"os/exec"
	"sync"

	"cicd-agent/common"
)

// ImagePusher 镜像推送器
type ImagePusher struct {
	taskID string
}

// NewImagePusher 创建镜像推送器
func NewImagePusher(taskID string) *ImagePusher {
	return &ImagePusher{taskID: taskID}
}

// PushImages 并发推送镜像（可取消）
func (p *ImagePusher) PushImages(ctx context.Context, images []string) error {
	if len(images) == 0 {
		return fmt.Errorf("镜像列表为空")
	}

	maxConcurrency := p.calculatePushConcurrency(len(images))
	common.AppLogger.Info(fmt.Sprintf("推送镜像: 总数=%d, 并发数=%d", len(images), maxConcurrency))

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

			if err := p.pushSingleImage(ctx, image); err != nil {
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

	common.AppLogger.Info(fmt.Sprintf("所有镜像推送完成: %d个", len(images)))
	return nil
}

// pushSingleImage 推送单个镜像
func (p *ImagePusher) pushSingleImage(ctx context.Context, image string) error {
	common.AppLogger.Info(fmt.Sprintf("开始推送镜像: %s", image))

	cmd := exec.CommandContext(ctx, "docker", "push", image)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// 检查是否是上下文取消导致的错误
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("推送镜像 %s 被取消", image)
		}
		return fmt.Errorf("推送镜像 %s 失败: %v, 输出: %s", image, err, string(output))
	}

	common.AppLogger.Info(fmt.Sprintf("成功推送镜像: %s", image))
	return nil
}

// calculatePushConcurrency 计算推送并发数
func (p *ImagePusher) calculatePushConcurrency(imageCount int) int {
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

// PushImages 推送镜像列表（包装函数）
func PushImages(ctx context.Context, images []string) error {
	// 使用空的taskID，因为这是包装函数
	pusher := NewImagePusher("")
	return pusher.PushImages(ctx, images)
}
