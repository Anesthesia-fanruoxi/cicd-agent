package tagImage

import (
	"context"
	"fmt"
	"os/exec"
	"sync"

	"cicd-agent/common"
)

// TagImages 标记镜像（可取消）
func TagImages(ctx context.Context, onlineImages, localImages []string, taskID string, taskLogger *common.TaskLogger) error {
	if len(onlineImages) != len(localImages) {
		return fmt.Errorf("在线镜像和本地镜像数量不匹配")
	}

	if taskLogger != nil {
		taskLogger.WriteStep("tagImages", "INFO", fmt.Sprintf("开始标记镜像，共%d个", len(onlineImages)))
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(onlineImages))

	// 并发标记镜像
	for i, onlineImg := range onlineImages {
		wg.Add(1)
		go func(online, local string) {
			defer wg.Done()
			// 取消检查
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := tagSingleImage(ctx, online, local, taskLogger); err != nil {
				errChan <- fmt.Errorf("标记镜像失败 %s -> %s: %v", online, local, err)
			}
		}(onlineImg, localImages[i])
	}

	wg.Wait()
	close(errChan)

	// 检查是否有错误
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	if taskLogger != nil {
		taskLogger.WriteStep("tagImages", "INFO", "镜像标记完成")
	}
	return nil
}

// tagSingleImage 标记单个镜像
func tagSingleImage(ctx context.Context, onlineImage, localImage string, taskLogger *common.TaskLogger) error {
	if taskLogger != nil {
		taskLogger.WriteStep("tagImages", "INFO", fmt.Sprintf("标记镜像: %s -> %s", onlineImage, localImage))
	}

	cmd := exec.CommandContext(ctx, "docker", "tag", onlineImage, localImage)
	output, err := cmd.CombinedOutput()

	// 写入命令执行日志
	if taskLogger != nil {
		taskLogger.WriteCommand("tagImages", fmt.Sprintf("docker tag %s %s", onlineImage, localImage), output, err)
	}

	if err != nil {
		// 检查是否是上下文取消导致的错误
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("标记镜像 %s 被取消", onlineImage)
		}
		return fmt.Errorf("docker tag命令执行失败: %v", err)
	}

	if taskLogger != nil {
		taskLogger.WriteStep("tagImages", "INFO", fmt.Sprintf("镜像标记成功: %s -> %s", onlineImage, localImage))
	}
	return nil
}
