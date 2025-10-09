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

	// 等待30秒让新版本稳定运行
	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", "等待30秒让新版本稳定运行...")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		if vc.taskLogger != nil {
			vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", "等待30秒完成，开始清理旧版本")
		}
	}

	// 检查部署目录是否存在
	if !vc.deploymentDirExists(vc.targetDeploymentDir) {
		if vc.taskLogger != nil {
			vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("目标部署目录不存在，无需清理: %s", vc.targetDeploymentDir))
		}
		return nil
	}

	// 删除旧版本部署
	if err := vc.deleteDeployment(ctx, vc.targetDeploymentDir); err != nil {
		return fmt.Errorf("删除旧版本部署失败: %v", err)
	}

	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("成功删除旧版本部署: %s", vc.targetDeploymentDir))
	}
	return nil
}

// deploymentDirExists 检查部署目录是否存在
func (vc *VersionCleaner) deploymentDirExists(dir string) bool {
	cmd := exec.Command("ls", "-d", dir)
	err := cmd.Run()
	return err == nil
}

// deleteDeployment 删除指定的部署目录
func (vc *VersionCleaner) deleteDeployment(ctx context.Context, deploymentDir string) error {
	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("开始删除部署: %s", deploymentDir))
	}

	// 执行kubectl delete -f 命令
	cmd := exec.CommandContext(ctx, "kubectl", "delete", "-f", deploymentDir, "--timeout=300s")
	output, err := cmd.CombinedOutput()

	// 写入命令执行日志
	if vc.taskLogger != nil {
		vc.taskLogger.WriteCommand("cleanupOldVersion", cmd.String(), output, err)
	}

	if err != nil {
		// 检查是否是因为资源不存在而失败
		if strings.Contains(string(output), "not found") || strings.Contains(string(output), "No resources found") {
			if vc.taskLogger != nil {
				vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("部署资源 %s 不存在，无需删除", deploymentDir))
			}
			return nil
		}
		errMsg := fmt.Sprintf("删除部署失败: %v, 输出: %s", err, string(output))
		if vc.taskLogger != nil {
			vc.taskLogger.WriteStep("cleanupOldVersion", "ERROR", errMsg)
		}
		return fmt.Errorf(errMsg)
	}

	if vc.taskLogger != nil {
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("部署 %s 删除命令执行成功", deploymentDir))
		vc.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("删除输出: %s", string(output)))
	}

	// 等待资源完全删除
	return vc.waitForResourcesDeletion(ctx, deploymentDir, 3*time.Minute)
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
