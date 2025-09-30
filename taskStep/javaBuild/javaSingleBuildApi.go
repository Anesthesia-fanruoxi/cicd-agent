package javaBuild

import (
	"cicd-agent/common"
	tagImage "cicd-agent/taskStep/javaBuild/10-tagImage"
	pushLocal "cicd-agent/taskStep/javaBuild/11-pushLocal"
	checkImage "cicd-agent/taskStep/javaBuild/12-checkImage"
	deployService "cicd-agent/taskStep/javaBuild/13-deployService"
	pullOnline "cicd-agent/taskStep/javaBuild/9-pullOnline"
	"context"
	"fmt"
	"time"
)

// SingleVersionProcessor 单版本部署处理器
type SingleVersionProcessor struct {
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

// NewSingleVersionProcessor 创建单版本部署处理器
func NewSingleVersionProcessor(project, category, tag, description, taskID string, ctx context.Context, opsURL, proURL, createTime string, stepDurations map[string]interface{}) *SingleVersionProcessor {
	return &SingleVersionProcessor{
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

// ProcessSingleVersionDeployment 处理单版本部署请求
func (r *SingleVersionProcessor) ProcessSingleVersionDeployment() error {
	common.AppLogger.Info("开始处理单版本部署请求", fmt.Sprintf("项目=%s, 标签=%s, 分类=%s", r.project, r.tag, r.category))

	// 步骤9：拉取在线镜像
	if err := r.step9PullOnline(); err != nil {
		r.sendFailureNotifications()
		return fmt.Errorf("步骤9拉取在线镜像失败: %v", err)
	}

	// 步骤10：标记镜像
	if err := r.step10TagImages(); err != nil {
		r.sendFailureNotifications()
		return fmt.Errorf("步骤10标记镜像失败: %v", err)
	}

	// 步骤11：推送本地镜像
	if err := r.step11PushLocal(); err != nil {
		r.sendFailureNotifications()
		return fmt.Errorf("步骤11推送本地镜像失败: %v", err)
	}

	// 步骤12：检查镜像
	if err := r.step12CheckImage(); err != nil {
		r.sendFailureNotifications()
		return fmt.Errorf("步骤12检查镜像失败: %v", err)
	}

	// 步骤13：应用服务部署
	if err := r.step13DeployService(); err != nil {
		r.sendFailureNotifications()
		return fmt.Errorf("步骤13应用服务部署失败: %v", err)
	}

	// 单版本部署完成，发送任务完成通知
	common.AppLogger.Info("单版本部署流程完成")
	endTime := time.Now().Format("2006-01-02 15:04:05")

	if err := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "complete", r.opsURL, r.proURL, r.stepDurations); err != nil {
		common.AppLogger.Error("发送任务完成通知失败:", err)
	}

	// 发送飞书卡片通知
	if err := common.SendFeishuCard(r.opsURL, r.project, r.tag, "complete", r.startedAt, endTime, "single", r.category, r.description); err != nil {
		common.AppLogger.Error("发送飞书卡片通知失败:", err)
	}
	common.AppLogger.Info("单版本部署请求处理完成", fmt.Sprintf("项目=%s, 标签=%s, 分类=%s", r.project, r.tag, r.category))
	return nil
}

// step9PullOnline 步骤9：拉取在线镜像
func (r *SingleVersionProcessor) step9PullOnline() error {
	stepName := "拉取在线镜像"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "start", "开始拉取在线镜像", r.project, r.tag)

	common.AppLogger.Info("执行步骤9：拉取在线镜像")

	// 获取需要拉取的镜像列表
	images, err := getOnlineImages(r.project, r.tag)
	if err != nil {
		common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "failed", fmt.Sprintf("获取镜像列表失败: %v", err), r.project, r.tag)
		return err
	}

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "cancel", "取消拉取在线镜像", r.project, r.tag)
		r.sendCancelNotifications()
		return r.ctx.Err()
	default:
	}

	// 使用9-pullOnline模块拉取镜像（可取消）
	puller := pullOnline.NewImagePuller(r.taskID)
	if err := puller.PullImages(r.ctx, images); err != nil {
		// 检查是否是取消操作
		if r.ctx.Err() == context.Canceled {
			common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "cancel", fmt.Sprintf("拉取镜像被取消: %v", err), r.project, r.tag)
		} else {
			common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "failed", fmt.Sprintf("拉取镜像失败: %v", err), r.project, r.tag)
		}
		return err
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "success", "拉取在线镜像完成", r.project, r.tag)
	common.AppLogger.Info("步骤9完成：拉取在线镜像")
	return nil
}

// step10TagImages 步骤10：标记镜像
func (r *SingleVersionProcessor) step10TagImages() error {
	stepName := "标记镜像"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "start", "开始标记镜像", r.project, r.tag)

	common.AppLogger.Info("执行步骤10：标记镜像")

	// 获取在线镜像和本地镜像列表
	onlineImages, err := getOnlineImages(r.project, r.tag)
	if err != nil {
		common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "failed", fmt.Sprintf("获取在线镜像列表失败: %v", err), r.project, r.tag)
		return err
	}

	localImages, err := getLocalImages(r.project, r.tag)
	if err != nil {
		common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "failed", fmt.Sprintf("获取本地镜像列表失败: %v", err), r.project, r.tag)
		return err
	}

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "cancel", "取消标记镜像", r.project, r.tag)
		r.sendCancelNotifications()
		return r.ctx.Err()
	default:
	}

	// 使用10-tagImage模块标记镜像（可取消）
	if err := tagImage.TagImages(r.ctx, onlineImages, localImages, r.taskID); err != nil {
		// 检查是否是取消操作
		if r.ctx.Err() == context.Canceled {
			common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "cancel", fmt.Sprintf("标记镜像被取消: %v", err), r.project, r.tag)
		} else {
			common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "failed", fmt.Sprintf("标记镜像失败: %v", err), r.project, r.tag)
		}
		return err
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "success", "标记镜像完成", r.project, r.tag)
	common.AppLogger.Info("步骤10完成：标记镜像")
	return nil
}

// step11PushLocal 步骤11：推送本地镜像
func (r *SingleVersionProcessor) step11PushLocal() error {
	stepName := "推送本地镜像"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "start", "开始推送本地镜像", r.project, r.tag)

	common.AppLogger.Info("执行步骤11：推送本地镜像")

	// 获取需要推送的镜像列表
	images, err := getLocalImages(r.project, r.tag)
	if err != nil {
		common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "failed", fmt.Sprintf("获取本地镜像列表失败: %v", err), r.project, r.tag)
		return err
	}

	if len(images) == 0 {
		common.AppLogger.Info("没有需要推送的本地镜像")
		common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "success", "没有需要推送的镜像", r.project, r.tag)
		return nil
	}

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "cancel", "取消推送本地镜像", r.project, r.tag)
		r.sendCancelNotifications()
		return r.ctx.Err()
	default:
	}

	// 使用11-pushLocal模块推送镜像（可取消）
	pusher := pushLocal.NewImagePusher(r.taskID)
	if err := pusher.PushImages(r.ctx, images); err != nil {
		// 检查是否是取消操作
		if r.ctx.Err() == context.Canceled {
			common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "cancel", fmt.Sprintf("推送镜像被取消: %v", err), r.project, r.tag)
		} else {
			common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "failed", fmt.Sprintf("推送镜像失败: %v", err), r.project, r.tag)
		}
		return err
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "success", "推送本地镜像完成", r.project, r.tag)
	common.AppLogger.Info("步骤11完成：推送本地镜像")
	return nil
}

// step12CheckImage 步骤12：检查镜像
func (r *SingleVersionProcessor) step12CheckImage() error {
	stepName := "检查镜像"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 12, "checkImage", stepName, "start", "开始检查镜像", r.project, r.tag)

	common.AppLogger.Info("执行步骤12：检查镜像")

	// 获取需要检查的镜像列表（仅检查离线仓库Harbor中的镜像）
	images, err := getLocalImages(r.project, r.tag)
	if err != nil {
		common.SendStepNotification(r.taskID, 12, "checkImage", stepName, "failed", fmt.Sprintf("获取镜像列表失败: %v", err), r.project, r.tag)
		return err
	}

	if len(images) == 0 {
		common.AppLogger.Info("没有需要检查的镜像")
		common.SendStepNotification(r.taskID, 12, "checkImage", stepName, "success", "没有需要检查的镜像", r.project, r.tag)
		return nil
	}

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 12, "checkImage", stepName, "cancel", "取消检查镜像", r.project, r.tag)
		r.sendCancelNotifications()
		return r.ctx.Err()
	default:
	}

	// 使用12-checkImage模块检查镜像（显式传入项目与标签，可取消）
	if err := checkImage.CheckImages(r.ctx, images, r.project, r.tag, r.taskID); err != nil {
		common.SendStepNotification(r.taskID, 12, "checkImage", stepName, "failed", fmt.Sprintf("检查镜像失败: %v", err), r.project, r.tag)
		return err
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 12, "checkImage", stepName, "success", "检查镜像完成", r.project, r.tag)
	common.AppLogger.Info("步骤12完成：检查镜像")
	return nil
}

// step13DeployService 步骤13：应用服务部署
func (r *SingleVersionProcessor) step13DeployService() error {
	stepName := "应用服务部署"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 13, "deployService", stepName, "start", "开始应用服务部署", r.project, r.tag)

	common.AppLogger.Info("执行步骤13：应用服务部署")

	// 获取单版本部署目录
	deployDir, err := common.GetDeploymentPath(r.project)
	if err != nil {
		common.SendStepNotification(r.taskID, 13, "deployService", stepName, "failed", fmt.Sprintf("获取部署目录失败: %v", err), r.project, r.tag)
		return err
	}

	common.AppLogger.Info(fmt.Sprintf("使用部署目录: %s", deployDir))

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 13, "deployService", stepName, "cancel", "取消应用服务部署", r.project, r.tag)
		r.sendCancelNotifications()
		return r.ctx.Err()
	default:
	}

	// 使用13-deployService模块部署服务（可取消）
	deployer := deployService.NewServiceDeployer(r.taskID)
	if err := deployer.DeployServicesWithCategory(r.ctx, deployDir, r.project, r.tag, r.category); err != nil {
		common.SendStepNotification(r.taskID, 13, "deployService", stepName, "failed", fmt.Sprintf("应用服务部署失败: %v", err), r.project, r.tag)
		return err
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 13, "deployService", stepName, "success", "应用服务部署完成", r.project, r.tag)
	common.AppLogger.Info("步骤13完成：应用服务部署")
	return nil
}

// sendFailureNotifications 发送失败通知（包括任务通知和飞书通知）
func (r *SingleVersionProcessor) sendFailureNotifications() {
	endTime := time.Now().Format("2006-01-02 15:04:05")

	// 发送任务失败通知
	if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "failed", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
		common.AppLogger.Error("发送任务失败通知失败:", notifyErr)
	}

	// 发送飞书失败通知
	if feishuErr := common.SendFeishuCard(r.opsURL, r.project, r.tag, "failed", r.startedAt, endTime, "single", r.category, r.description); feishuErr != nil {
		common.AppLogger.Error("发送飞书失败通知失败:", feishuErr)
	}
}

// sendCancelNotifications 发送取消通知（包括任务通知和飞书通知）
func (r *SingleVersionProcessor) sendCancelNotifications() {
	endTime := time.Now().Format("2006-01-02 15:04:05")

	// 发送任务取消通知
	if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
		common.AppLogger.Error("发送任务取消通知失败:", notifyErr)
	}

	// 发送飞书取消通知
	if feishuErr := common.SendFeishuCard(r.opsURL, r.project, r.tag, "cancel", r.startedAt, endTime, "single", r.category, r.description); feishuErr != nil {
		common.AppLogger.Error("发送飞书取消通知失败:", feishuErr)
	}
}
