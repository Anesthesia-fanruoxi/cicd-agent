package cleanupOldVersion

import (
	"cicd-agent/common"
	"cicd-agent/taskStep"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// VersionCleaner 版本清理处理器
type VersionCleaner struct {
	targetNamespace     string // 要删除的目标namespace
	targetDeploymentDir string // 要删除的目标部署目录
	taskLogger          *common.TaskLogger
}

// NewVersionCleaner 创建版本清理处理器
func NewVersionCleaner(targetNamespace, targetDeploymentDir string, taskLogger *common.TaskLogger) *VersionCleaner {
	return &VersionCleaner{
		targetNamespace:     targetNamespace,
		targetDeploymentDir: targetDeploymentDir,
		taskLogger:          taskLogger,
	}
}

// Execute 执行版本清理
func (vc *VersionCleaner) Execute(ctx context.Context, step taskStep.Step) error {
	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("开始执行版本清理，目标namespace: %s, 部署目录: %s",
			vc.targetNamespace, vc.targetDeploymentDir))
	}

	// 等待55秒让新版本稳定运行
	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", "等待55秒让新版本稳定运行...")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(55 * time.Second):
		if vc.taskLogger != nil {
			vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", "等待55秒完成，开始清理旧版本")
		}
	}

	// 检查部署目录是否存在
	if !vc.deploymentDirExists(vc.targetDeploymentDir) {
		if vc.taskLogger != nil {
			vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("目标部署目录不存在，无需清理: %s", vc.targetDeploymentDir))
		}
		return nil
	}

	// 缩容旧版本部署到0副本
	if err := vc.scaleDeploymentToZero(ctx); err != nil {
		return fmt.Errorf("缩容旧版本部署失败: %v", err)
	}

	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("成功将旧版本部署缩容到0副本: %s", vc.targetNamespace))
	}
	return nil
}

// deploymentDirExists 检查部署目录是否存在
func (vc *VersionCleaner) deploymentDirExists(dir string) bool {
	cmd := exec.Command("ls", "-d", dir)
	err := cmd.Run()
	return err == nil
}

// scaleDeploymentToZero 将namespace下所有deployment缩容到0副本
func (vc *VersionCleaner) scaleDeploymentToZero(ctx context.Context) error {
	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("开始将namespace %s 下的deployment缩容到0副本", vc.targetNamespace))
	}

	// 获取namespace下所有deployment名称
	deployments, err := vc.getDeploymentsInNamespace(ctx)
	if err != nil {
		return fmt.Errorf("获取deployment列表失败: %v", err)
	}

	if len(deployments) == 0 {
		if vc.taskLogger != nil {
			vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("namespace %s 中没有deployment，无需缩容", vc.targetNamespace))
		}
		return nil
	}

	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("找到 %d 个deployment，开始缩容", len(deployments)))
	}

	// 逐个将deployment缩容到0
	for _, deployment := range deployments {
		if err := vc.scaleDeployment(ctx, deployment, 0); err != nil {
			if vc.taskLogger != nil {
				vc.taskLogger.WriteStep("cleanupOldVersion", "ERROR", fmt.Sprintf("缩容deployment %s 失败: %v", deployment, err))
			}
			return err
		}
	}

	// 等待所有pod完全删除
	return vc.waitForResourcesDeletion(ctx, vc.targetDeploymentDir, 3*time.Minute)
}

// getDeploymentsInNamespace 获取指定namespace下所有deployment名称
func (vc *VersionCleaner) getDeploymentsInNamespace(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "get", "deployment", "-n", vc.targetNamespace, "-o", "jsonpath={.items[*].metadata.name}")
	output, err := cmd.CombinedOutput()

	// 写入命令执行日志
	if vc.taskLogger != nil {
		vc.taskLogger.WriteCommand("cleanupOldVersion", cmd.String(), output, err)
	}

	if err != nil {
		// 如果namespace不存在或没有deployment，返回空列表
		if strings.Contains(string(output), "not found") || strings.Contains(string(output), "No resources found") {
			return []string{}, nil
		}
		return nil, fmt.Errorf("获取deployment列表失败: %v, 输出: %s", err, string(output))
	}

	// 解析deployment名称列表
	deploymentNames := strings.Fields(strings.TrimSpace(string(output)))
	return deploymentNames, nil
}

// scaleDeployment 将指定deployment缩容到指定副本数
func (vc *VersionCleaner) scaleDeployment(ctx context.Context, deploymentName string, replicas int) error {
	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("缩容deployment %s 到 %d 副本", deploymentName, replicas))
	}

	// 执行kubectl scale命令
	cmd := exec.CommandContext(ctx, "kubectl", "scale", "deployment", deploymentName,
		"-n", vc.targetNamespace,
		"--replicas="+fmt.Sprintf("%d", replicas))
	output, err := cmd.CombinedOutput()

	// 写入命令执行日志
	if vc.taskLogger != nil {
		vc.taskLogger.WriteCommand("cleanupOldVersion", cmd.String(), output, err)
	}

	if err != nil {
		return fmt.Errorf("缩容失败: %v, 输出: %s", err, string(output))
	}

	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("deployment %s 缩容命令执行成功: %s", deploymentName, string(output)))
	}

	return nil
}

// waitForResourcesDeletion 等待pod完全删除
func (vc *VersionCleaner) waitForResourcesDeletion(ctx context.Context, deploymentDir string, timeout time.Duration) error {
	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", "等待旧版本pod完全删除")
	}

	deadline := time.Now().Add(timeout)
	checkInterval := 10 * time.Second

	for time.Now().Before(deadline) {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 检查目标namespace中的pod是否还存在
		if !vc.hasPodsInNamespace(ctx, vc.targetNamespace) {
			if vc.taskLogger != nil {
				vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", "旧版本pod已完全删除")
			}
			return nil
		}

		if vc.taskLogger != nil {
			vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", "旧版本pod仍在删除中，继续等待...")
		}

		// 等待下次检查
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(checkInterval):
		}
	}

	return fmt.Errorf("等待pod删除超时")
}

// hasPodsInNamespace 检查指定namespace中是否还有pod
func (vc *VersionCleaner) hasPodsInNamespace(ctx context.Context, namespace string) bool {
	// 构建kubectl命令检查pod
	cmd := exec.CommandContext(ctx, "kubectl", "get", "pods", "-n", namespace, "--no-headers", "-o", "name")
	output, err := cmd.CombinedOutput()

	// 写入命令执行日志
	if vc.taskLogger != nil {
		vc.taskLogger.WriteCommand("cleanupOldVersion", cmd.String(), output, err)
	}

	if err != nil {
		// 如果命令失败，可能是namespace不存在或没有权限，认为pod已删除
		return false
	}

	// 如果输出为空，说明没有pod
	return strings.TrimSpace(string(output)) != ""
}
