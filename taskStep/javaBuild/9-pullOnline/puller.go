package pullOnline

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"cicd-agent/common"
)

// ImagePuller 镜像拉取器
type ImagePuller struct {
	taskID     string
	taskLogger *common.TaskLogger
}

// NewImagePuller 创建镜像拉取器
func NewImagePuller(taskID string, taskLogger *common.TaskLogger) *ImagePuller {
	return &ImagePuller{
		taskID:     taskID,
		taskLogger: taskLogger,
	}
}

// CleanProjectImages 清理指定项目的所有旧镜像（包括online和local harbor）
func (p *ImagePuller) CleanProjectImages(ctx context.Context, projectName string) error {
	if projectName == "" {
		return fmt.Errorf("项目名称为空")
	}

	if p.taskLogger != nil {
		p.taskLogger.WriteStep("pullOnline", "INFO", fmt.Sprintf("开始清理项目 %s 的旧镜像", projectName))
	}

	// 获取所有本地镜像
	cmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{.Repository}}:{{.Tag}}")
	output, err := cmd.Output()
	if err != nil {
		if p.taskLogger != nil {
			p.taskLogger.WriteStep("pullOnline", "ERROR", fmt.Sprintf("获取镜像列表失败: %v", err))
		}
		return fmt.Errorf("获取镜像列表失败: %v", err)
	}

	// 解析镜像列表，筛选出需要删除的镜像
	var imagesToDelete []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		image := strings.TrimSpace(scanner.Text())
		if image == "" || image == "<none>:<none>" {
			continue
		}

		// 检查镜像是否属于当前项目（精准匹配 /项目名/）
		if strings.Contains(image, "/"+projectName+"/") {
			imagesToDelete = append(imagesToDelete, image)
		}
	}

	if len(imagesToDelete) == 0 {
		if p.taskLogger != nil {
			p.taskLogger.WriteStep("pullOnline", "INFO", "没有需要清理的旧镜像")
		}
		return nil
	}

	if p.taskLogger != nil {
		p.taskLogger.WriteStep("pullOnline", "INFO", fmt.Sprintf("找到 %d 个需要清理的镜像", len(imagesToDelete)))
	}

	// 并发删除镜像
	return p.deleteImages(ctx, imagesToDelete)
}

// deleteImages 并发删除镜像
func (p *ImagePuller) deleteImages(ctx context.Context, images []string) error {
	maxConcurrency := p.calculatePullConcurrency(len(images))

	if p.taskLogger != nil {
		p.taskLogger.WriteStep("pullOnline", "INFO", fmt.Sprintf("删除镜像: 总数=%d, 并发数=%d", len(images), maxConcurrency))
	}

	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var deletedCount int
	var mu sync.Mutex

	for _, img := range images {
		wg.Add(1)
		go func(image string) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			case semaphore <- struct{}{}:
			}
			defer func() { <-semaphore }()

			select {
			case <-ctx.Done():
				return
			default:
			}

			cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", image)
			output, err := cmd.CombinedOutput()

			if p.taskLogger != nil {
				p.taskLogger.WriteCommand("pullOnline", "docker rmi -f "+image, output, err)
			}

			if err == nil {
				mu.Lock()
				deletedCount++
				mu.Unlock()
				if p.taskLogger != nil {
					p.taskLogger.WriteStep("pullOnline", "INFO", fmt.Sprintf("成功删除镜像: %s", image))
				}
			} else {
				// 删除失败只记录警告，不中断流程
				if p.taskLogger != nil {
					p.taskLogger.WriteStep("pullOnline", "WARNING", fmt.Sprintf("删除镜像失败: %s, 错误: %v", image, err))
				}
			}
		}(img)
	}

	wg.Wait()

	if p.taskLogger != nil {
		p.taskLogger.WriteStep("pullOnline", "INFO", fmt.Sprintf("镜像清理完成: 成功删除 %d 个", deletedCount))
	}

	return nil
}

// PullImages 并发拉取镜像（可取消）
func (p *ImagePuller) PullImages(ctx context.Context, images []string) error {
	if len(images) == 0 {
		return fmt.Errorf("镜像列表为空")
	}

	maxConcurrency := p.calculatePullConcurrency(len(images))
	logMsg := fmt.Sprintf("拉取镜像: 总数=%d, 并发数=%d", len(images), maxConcurrency)

	if p.taskLogger != nil {
		p.taskLogger.WriteStep("pullOnline", "INFO", logMsg)
	}

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

	successMsg := fmt.Sprintf("所有镜像拉取完成: %d个", len(images))
	if p.taskLogger != nil {
		p.taskLogger.WriteStep("pullOnline", "INFO", successMsg)
	}
	return nil
}

// pullSingleImage 拉取单个镜像
func (p *ImagePuller) pullSingleImage(ctx context.Context, image string) error {
	if p.taskLogger != nil {
		p.taskLogger.WriteStep("pullOnline", "INFO", fmt.Sprintf("开始拉取镜像: %s", image))
	}

	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	output, err := cmd.CombinedOutput()

	// 写入命令执行日志
	if p.taskLogger != nil {
		p.taskLogger.WriteCommand("pullOnline", "docker pull "+image, output, err)
	}

	if err != nil {
		// 检查是否是上下文取消导致的错误
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("拉取镜像 %s 被取消", image)
		}
		return fmt.Errorf("拉取镜像 %s 失败: %v", image, err)
	}

	if p.taskLogger != nil {
		p.taskLogger.WriteStep("pullOnline", "INFO", fmt.Sprintf("成功拉取镜像: %s", image))
	}
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

// PullImages 拉取镜像列表（包装函数，无日志记录）
func PullImages(ctx context.Context, images []string) error {
	// 使用空的taskID和nil logger，因为这是包装函数
	puller := NewImagePuller("", nil)
	return puller.PullImages(ctx, images)
}

// CleanProjectImages 清理项目旧镜像（包装函数）
func CleanProjectImages(ctx context.Context, projectName string) error {
	puller := NewImagePuller("", nil)
	return puller.CleanProjectImages(ctx, projectName)
}
