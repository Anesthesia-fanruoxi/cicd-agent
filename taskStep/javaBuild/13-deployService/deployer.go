package deployService

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"cicd-agent/common"
	"cicd-agent/config"
)

// ServiceDeployer 服务部署器
type ServiceDeployer struct {
	taskID     string
	taskLogger *common.TaskLogger
}

// NewServiceDeployer 创建服务部署器
func NewServiceDeployer(taskID string, taskLogger *common.TaskLogger) *ServiceDeployer {
	return &ServiceDeployer{
		taskID:     taskID,
		taskLogger: taskLogger,
	}
}

// DeployServices 部署服务（可取消）
func (d *ServiceDeployer) DeployServices(ctx context.Context, deployDir, project, newTag string) error {
	return d.DeployServicesWithCategory(ctx, deployDir, project, newTag, "")
}

// DeployServicesWithCategory 部署服务（支持category，可取消）
func (d *ServiceDeployer) DeployServicesWithCategory(ctx context.Context, deployDir, project, newTag, category string) error {
	// 获取所有YAML文件
	yamlFiles, err := d.getYamlFiles(deployDir)
	if err != nil {
		return fmt.Errorf("获取YAML文件失败: %v", err)
	}

	if len(yamlFiles) == 0 {
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("deployService", "INFO", "没有找到需要部署的YAML文件")
		}
		return nil
	}

	if d.taskLogger != nil {
		d.taskLogger.WriteStep("deployService", "INFO", fmt.Sprintf("找到 %d 个YAML文件需要处理", len(yamlFiles)))
	}

	// 并发处理YAML文件
	var wg sync.WaitGroup
	errChan := make(chan error, len(yamlFiles))
	semaphore := make(chan struct{}, 5) // 限制并发数为5

	for _, yamlFile := range yamlFiles {
		wg.Add(1)
		go func(file string) {
			defer wg.Done()

			// 获取信号量
			select {
			case <-ctx.Done():
				return
			case semaphore <- struct{}{}:
			}
			defer func() { <-semaphore }()

			// 检查取消
			select {
			case <-ctx.Done():
				return
			default:
			}

			if err := d.updateYamlFile(file, project, newTag); err != nil {
				errChan <- fmt.Errorf("更新文件 %s 失败: %v", file, err)
			}
		}(yamlFile)
	}

	wg.Wait()
	close(errChan)

	// 检查是否有错误
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	if d.taskLogger != nil {
		d.taskLogger.WriteStep("deployService", "INFO", "所有YAML文件处理完成")
	}

	// 执行kubectl apply应用所有部署文件
	if err := d.applyDeployments(ctx, deployDir, project, category); err != nil {
		return fmt.Errorf("应用部署文件失败: %v", err)
	}

	return nil
}

// getYamlFiles 获取目录下所有YAML文件
func (d *ServiceDeployer) getYamlFiles(deployDir string) ([]string, error) {
	var yamlFiles []string

	err := filepath.Walk(deployDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && (strings.HasSuffix(info.Name(), ".yaml") || strings.HasSuffix(info.Name(), ".yml")) {
			yamlFiles = append(yamlFiles, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return yamlFiles, nil
}

// updateYamlFile 更新YAML文件中的镜像标签
func (d *ServiceDeployer) updateYamlFile(filePath, project, newTag string) error {
	if d.taskLogger != nil {
		d.taskLogger.WriteStep("deployService", "INFO", fmt.Sprintf("开始处理文件: %s", filePath))
	}

	// 读取文件内容
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %v", err)
	}
	defer file.Close()

	var lines []string
	var updated bool
	scanner := bufio.NewScanner(file)

	// 构建镜像匹配正则表达式
	// 从配置中获取离线Harbor地址并转义特殊字符（如点号）
	escapedHarbor := regexp.QuoteMeta(config.AppConfig.Harbor.Offline)
	// 匹配格式: image: testhub.hzbxhd.com/project/service:tag
	imagePattern := regexp.MustCompile(`^(\s*image:\s*)(` + escapedHarbor + `/` + regexp.QuoteMeta(project) + `/[^:]+):(.+)$`)

	for scanner.Scan() {
		line := scanner.Text()

		// 检查是否匹配项目镜像
		if matches := imagePattern.FindStringSubmatch(line); matches != nil {
			// matches[1]: 前缀部分 "  image: "
			// matches[2]: 镜像名部分 "hub.hzbxhd.com/project/service"
			// matches[3]: 旧标签部分

			oldTag := matches[3]
			newLine := matches[1] + matches[2] + ":" + newTag

			if d.taskLogger != nil {
				d.taskLogger.WriteStep("deployService", "INFO", fmt.Sprintf("文件 %s: 更新镜像标签 %s -> %s",
					filepath.Base(filePath), oldTag, newTag))
			}

			lines = append(lines, newLine)
			updated = true
		} else {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取文件失败: %v", err)
	}

	// 如果有更新，写回文件
	if updated {
		content := strings.Join(lines, "\n")
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("写入文件失败: %v", err)
		}
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("deployService", "INFO", fmt.Sprintf("文件 %s 更新完成", filepath.Base(filePath)))
		}
	} else {
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("deployService", "INFO", fmt.Sprintf("文件 %s 无需更新", filepath.Base(filePath)))
		}
	}

	return nil
}

// applyDeployments 执行kubectl apply应用部署文件
func (d *ServiceDeployer) applyDeployments(ctx context.Context, deployDir, project, category string) error {
	if d.taskLogger != nil {
		d.taskLogger.WriteStep("deployService", "INFO", fmt.Sprintf("开始应用部署文件，目录: %s, 项目: %s, 分类: %s", deployDir, project, category))
	}

	var cmd *exec.Cmd

	// 检查是否为风控项目且有category
	if strings.Contains(project, "risk") && category != "" {
		// 根据category拼接具体的服务文件名：bxhd-risk-{category}.yaml
		serviceFile := fmt.Sprintf("bxhd-risk-%s.yaml", category)
		serviceFilePath := filepath.Join(deployDir, serviceFile)
		if _, err := os.Stat(serviceFilePath); os.IsNotExist(err) {
			return fmt.Errorf("指定的服务文件不存在: %s", serviceFilePath)
		}
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("deployService", "INFO", fmt.Sprintf("风控项目 - 应用服务文件: %s", serviceFile))
		}
		cmd = exec.CommandContext(ctx, "kubectl", "apply", "-f", serviceFile)
	} else {
		// 非风控项目或无category，应用所有文件
		if d.taskLogger != nil {
			d.taskLogger.WriteStep("deployService", "INFO", "非风控项目或无分类 - 应用所有YAML文件")
		}
		cmd = exec.CommandContext(ctx, "kubectl", "apply", "-f", ".")
	}

	cmd.Dir = deployDir // 设置工作目录

	output, err := cmd.CombinedOutput()

	// 写入命令执行日志
	if d.taskLogger != nil {
		d.taskLogger.WriteCommand("deployService", cmd.String(), output, err)
	}

	if err != nil {
		// 检查是否是上下文取消导致的错误
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("kubectl apply被取消")
		}
		return fmt.Errorf("kubectl apply执行失败: %v", err)
	}

	if d.taskLogger != nil {
		d.taskLogger.WriteStep("deployService", "INFO", "kubectl apply执行成功")
	}
	return nil
}

// DeployServices 部署服务列表（包装函数，无日志记录）
func DeployServices(ctx context.Context, deployDir, project, newTag string) error {
	// 使用空的taskID和nil logger，因为这是包装函数
	deployer := NewServiceDeployer("", nil)
	return deployer.DeployServices(ctx, deployDir, project, newTag)
}

// DeployServicesWithCategory 部署服务列表（支持category的包装函数，无日志记录）
func DeployServicesWithCategory(ctx context.Context, deployDir, project, newTag, category string) error {
	// 使用空的taskID和nil logger，因为这是包装函数
	deployer := NewServiceDeployer("", nil)
	return deployer.DeployServicesWithCategory(ctx, deployDir, project, newTag, category)
}
