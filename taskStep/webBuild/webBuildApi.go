package webBuild

import (
	"context"
	"fmt"
	"os"
	"time"
	
	"cicd-agent/common"
	"cicd-agent/taskStep/webBuild/10-deployNew"
	"cicd-agent/taskStep/webBuild/7-downProduct"
	"cicd-agent/taskStep/webBuild/8-extractProduct"
	"cicd-agent/taskStep/webBuild/9-backupCurrent"
)

// NoRemoteProcessor 非remote请求处理器
type NoRemoteProcessor struct {
	project string
	tag     string
}

// NewNoRemoteProcessor 创建非remote处理器
func NewNoRemoteProcessor(project, tag string) *NoRemoteProcessor {
	return &NoRemoteProcessor{
		project: project,
		tag:     tag,
	}
}

// ProcessNoRemoteRequest 处理非remote请求
func (n *NoRemoteProcessor) ProcessNoRemoteRequest() error {
	common.AppLogger.Info("开始处理非remote请求", fmt.Sprintf("项目=%s, 标签=%s", n.project, n.tag))

	// 这里实现非remote的处理逻辑
	// 可以根据具体需求添加相应的步骤

	common.AppLogger.Info("非remote请求处理完成", fmt.Sprintf("项目=%s, 标签=%s", n.project, n.tag))
	return nil
}

// RemoteProcessor web构建remote请求处理器
type RemoteProcessor struct {
	project       string
	category      string
	tag           string
	description   string
	taskID        string
	ctx           context.Context
	startedAt     string
	opsURL        string
	proURL        string
	stepDurations map[string]interface{}
}

// NewRemoteProcessor 创建web构建remote处理器
func NewRemoteProcessor(project, category, tag, description, taskID string, ctx context.Context, opsURL, proURL, createTime string, stepDurations map[string]interface{}) *RemoteProcessor {
	return &RemoteProcessor{
		project:       project,
		category:      category,
		tag:           tag,
		description:   description,
		taskID:        taskID,
		ctx:           ctx,
		startedAt:     createTime,
		opsURL:        opsURL,
		proURL:        proURL,
		stepDurations: stepDurations,
	}
}

// ProcessRemoteRequest 处理web构建remote请求
func (r *RemoteProcessor) ProcessRemoteRequest() error {
	common.AppLogger.Info("收到web构建回调", fmt.Sprintf("项目=%s, 分类=%s, 标签=%s, 任务ID=%s", r.project, r.category, r.tag, r.taskID))

	// 1. 下载产物
	common.SendStepNotification(r.taskID, 7, "downProduct", "下载产物", "start", "", r.project, r.tag)
	downProductStep := downProduct.NewDownProductStep(r.project, r.tag, r.category, r.ctx)
	if err := downProductStep.Execute(); err != nil {
		common.AppLogger.Error("下载产物失败:", err)
		// 发送步骤失败通知
		common.SendStepNotification(r.taskID, 7, "downProduct", "下载产物", "failed", err.Error(), r.project, r.tag)
		// 发送任务失败通知
		endTime := time.Now().Format("2006-01-02 15:04:05")
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "failed", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送失败通知失败:", notifyErr)
		}
		// 发送飞书失败通知
		if feishuErr := common.SendFeishuCard(r.opsURL, r.project, r.tag, "failed", r.startedAt, endTime, "single", r.category, r.description); feishuErr != nil {
			common.AppLogger.Error("发送飞书失败通知失败:", feishuErr)
		}
		return fmt.Errorf("下载产物失败: %v", err)
	}
	common.SendStepNotification(r.taskID, 7, "downProduct", "下载产物", "success", "", r.project, r.tag)

	// 2. 解压产物
	common.SendStepNotification(r.taskID, 8, "extractProduct", "解压产物", "start", "", r.project, r.tag)
	extractStep := extractProduct.NewExtractProductStep(r.project, r.tag, r.category, r.ctx, downProductStep.GetLocalFilePath())
	if err := extractStep.Execute(); err != nil {
		common.AppLogger.Error("解压产物失败:", err)
		// 发送步骤失败通知
		common.SendStepNotification(r.taskID, 8, "extractProduct", "解压产物", "failed", err.Error(), r.project, r.tag)
		// 发送任务失败通知
		endTime := time.Now().Format("2006-01-02 15:04:05")
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "failed", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送失败通知失败:", notifyErr)
		}
		// 发送飞书失败通知
		if feishuErr := common.SendFeishuCard(r.opsURL, r.project, r.tag, "failed", r.startedAt, endTime, "single", r.category, r.description); feishuErr != nil {
			common.AppLogger.Error("发送飞书失败通知失败:", feishuErr)
		}
		return fmt.Errorf("解压产物失败: %v", err)
	}
	common.SendStepNotification(r.taskID, 8, "extractProduct", "解压产物", "success", "", r.project, r.tag)

	// 3. 备份当前版本
	common.SendStepNotification(r.taskID, 9, "backupCurrent", "备份当前版本", "start", "", r.project, r.tag)
	backupStep := backupCurrent.NewBackupCurrentStep(r.project, r.tag, r.category, r.ctx)
	if err := backupStep.Execute(); err != nil {
		common.AppLogger.Error("备份当前版本失败:", err)
		// 发送步骤失败通知
		common.SendStepNotification(r.taskID, 9, "backupCurrent", "备份当前版本", "failed", err.Error(), r.project, r.tag)
		// 发送任务失败通知
		endTime := time.Now().Format("2006-01-02 15:04:05")
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "failed", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送失败通知失败:", notifyErr)
		}
		// 发送飞书失败通知
		if feishuErr := common.SendFeishuCard(r.opsURL, r.project, r.tag, "failed", r.startedAt, endTime, "single", r.category, r.description); feishuErr != nil {
			common.AppLogger.Error("发送飞书失败通知失败:", feishuErr)
		}
		return fmt.Errorf("备份当前版本失败: %v", err)
	}
	common.SendStepNotification(r.taskID, 9, "backupCurrent", "备份当前版本", "success", "", r.project, r.tag)

	// 4. 部署新版本
	common.SendStepNotification(r.taskID, 10, "deployNew", "部署新版本", "start", "", r.project, r.tag)
	deployStep := deployNew.NewDeployNewStep(r.project, r.tag, r.category, r.ctx, extractStep.GetDistPath())
	if err := deployStep.Execute(); err != nil {
		common.AppLogger.Error("部署新版本失败:", err)
		// 发送步骤失败通知
		common.SendStepNotification(r.taskID, 10, "deployNew", "部署新版本", "failed", err.Error(), r.project, r.tag)
		// 部署失败时尝试回滚
		if rollbackErr := r.rollbackDeployment(backupStep.GetBackupPath(), deployStep.GetWebPath()); rollbackErr != nil {
			common.AppLogger.Error("回滚部署失败:", rollbackErr)
		}
		// 发送任务失败通知
		endTime := time.Now().Format("2006-01-02 15:04:05")
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "failed", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送失败通知失败:", notifyErr)
		}
		// 发送飞书失败通知
		if feishuErr := common.SendFeishuCard(r.opsURL, r.project, r.tag, "failed", r.startedAt, endTime, "single", r.category, r.description); feishuErr != nil {
			common.AppLogger.Error("发送飞书失败通知失败:", feishuErr)
		}
		return fmt.Errorf("部署新版本失败: %v", err)
	}
	common.SendStepNotification(r.taskID, 10, "deployNew", "部署新版本", "success", "", r.project, r.tag)

	// 5. 清理临时文件
	r.cleanupTempFiles(downProductStep.GetLocalFilePath(), extractStep.GetExtractDir())

	// 发送任务完成通知
	endTime := time.Now().Format("2006-01-02 15:04:05")
	if err := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "complete", r.opsURL, r.proURL, r.stepDurations); err != nil {
		common.AppLogger.Error("发送任务完成通知失败:", err)
	}
	
	// 发送飞书完成通知
	if err := common.SendFeishuCard(r.opsURL, r.project, r.tag, "complete", r.startedAt, endTime, "single", r.category, r.description); err != nil {
		common.AppLogger.Error("发送飞书卡片通知失败:", err)
	}

	common.AppLogger.Info("web构建回调处理完成", fmt.Sprintf("项目=%s, 分类=%s, 标签=%s", r.project, r.category, r.tag))
	return nil
}

// rollbackDeployment 回滚部署
func (r *RemoteProcessor) rollbackDeployment(backupPath, webPath string) error {
	common.AppLogger.Info(fmt.Sprintf("开始回滚部署: %s -> %s", backupPath, webPath))

	// 检查备份是否存在
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("备份目录不存在，无法回滚: %s", backupPath)
	}

	// 删除失败的部署
	if err := os.RemoveAll(webPath); err != nil {
		common.AppLogger.Warning(fmt.Sprintf("删除失败部署目录失败: %v", err))
	}

	// 恢复备份
	if err := os.Rename(backupPath, webPath); err != nil {
		return fmt.Errorf("恢复备份失败: %v", err)
	}

	common.AppLogger.Info("部署回滚成功")
	return nil
}

// cleanupTempFiles 清理临时文件
func (r *RemoteProcessor) cleanupTempFiles(zipFilePath, extractDir string) {
	common.AppLogger.Info("开始清理临时文件")

	// 删除下载的zip文件
	if err := os.Remove(zipFilePath); err != nil {
		common.AppLogger.Warning(fmt.Sprintf("删除zip文件失败: %v", err))
	} else {
		common.AppLogger.Info(fmt.Sprintf("已删除zip文件: %s", zipFilePath))
	}

	// 删除解压目录
	if err := os.RemoveAll(extractDir); err != nil {
		common.AppLogger.Warning(fmt.Sprintf("删除解压目录失败: %v", err))
	} else {
		common.AppLogger.Info(fmt.Sprintf("已删除解压目录: %s", extractDir))
	}
}

// ProcessCancelRequest 处理取消请求
func (r *RemoteProcessor) ProcessCancelRequest() error {
	common.AppLogger.Info("收到web构建取消请求", fmt.Sprintf("项目=%s, 分类=%s, 标签=%s, 任务ID=%s", r.project, r.category, r.tag, r.taskID))

	// 发送取消通知
	if err := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); err != nil {
		common.AppLogger.Error("发送取消通知失败:", err)
		return fmt.Errorf("发送取消通知失败: %v", err)
	}

	common.AppLogger.Info("web构建取消处理完成", fmt.Sprintf("项目=%s, 分类=%s, 标签=%s", r.project, r.category, r.tag))
	return nil
}
