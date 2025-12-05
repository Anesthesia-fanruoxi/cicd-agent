package javaBuild

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cicd-agent/common"
	tagImage "cicd-agent/taskStep/javaBuild/10-tagImage"
	pushLocal "cicd-agent/taskStep/javaBuild/11-pushLocal"
	checkImage "cicd-agent/taskStep/javaBuild/12-checkImage"
	deployService "cicd-agent/taskStep/javaBuild/13-deployService"
	checkService "cicd-agent/taskStep/javaBuild/14-checkService"
	trafficSwitching "cicd-agent/taskStep/javaBuild/15-trafficSwitching"
	cleanupOldVersion "cicd-agent/taskStep/javaBuild/16-cleanupOldVersion"
	pullOnline "cicd-agent/taskStep/javaBuild/9-pullOnline"
)

// DoubleVersionProcessor 双版本部署处理器
type DoubleVersionProcessor struct {
	project       string
	tag           string
	projectName   string
	taskID        string
	ctx           context.Context
	startedAt     string
	opsURL        string
	proURL        string
	stepDurations map[string]interface{}
	taskLogger    *common.TaskLogger // 任务日志器
}

// NewDoubleVersionProcessor 创建双版本部署处理器
func NewDoubleVersionProcessor(project, tag, projectName, taskID string, ctx context.Context, opsURL, proURL, createTime string, stepDurations map[string]interface{}) *DoubleVersionProcessor {
	return &DoubleVersionProcessor{
		project:       project,
		tag:           tag,
		projectName:   projectName,
		taskID:        taskID,
		ctx:           ctx,
		startedAt:     createTime,
		opsURL:        opsURL,
		proURL:        proURL,
		stepDurations: stepDurations,
		taskLogger:    common.NewTaskLogger(taskID), // 创建任务日志器
	}
}

// ProcessDoubleVersionDeployment 处理双版本部署请求
func (r *DoubleVersionProcessor) ProcessDoubleVersionDeployment() error {
	common.AppLogger.Info("开始处理双版本部署请求", fmt.Sprintf("项目=%s, 标签=%s", r.project, r.tag))

	// 确保日志文件关闭
	defer func() {
		if r.taskLogger != nil {
			r.taskLogger.Close()
		}
	}()

	// 写入任务开始日志到文件
	if r.taskLogger != nil {
		r.taskLogger.WriteConsole("INFO", fmt.Sprintf("开始处理双版本部署请求: 项目=%s, 标签=%s", r.project, r.tag))
	}

	// 步骤9：拉取在线镜像
	if err := r.step9PullOnline(); err != nil {
		if r.ctx.Err() == context.Canceled {
			return fmt.Errorf("步骤9拉取在线镜像被取消: %v", err)
		}
		r.sendFailureNotifications()
		return fmt.Errorf("步骤9拉取在线镜像失败: %v", err)
	}

	// 步骤10：标记镜像
	if err := r.step10TagImages(); err != nil {
		if r.ctx.Err() == context.Canceled {
			return fmt.Errorf("步骤10标记镜像被取消: %v", err)
		}
		r.sendFailureNotifications()
		return fmt.Errorf("步骤10标记镜像失败: %v", err)
	}

	// 步骤11：推送本地镜像
	if err := r.step11PushLocal(); err != nil {
		if r.ctx.Err() == context.Canceled {
			return fmt.Errorf("步骤11推送本地镜像被取消: %v", err)
		}
		r.sendFailureNotifications()
		return fmt.Errorf("步骤11推送本地镜像失败: %v", err)
	}

	// 步骤12：检查镜像
	if err := r.step12CheckImage(); err != nil {
		if r.ctx.Err() == context.Canceled {
			return fmt.Errorf("步骤12检查镜像被取消: %v", err)
		}
		r.sendFailureNotifications()
		return fmt.Errorf("步骤12检查镜像失败: %v", err)
	}

	// 步骤13：应用服务部署
	if err := r.step13DeployService(); err != nil {
		if r.ctx.Err() == context.Canceled {
			return fmt.Errorf("步骤13应用服务部署被取消: %v", err)
		}
		r.sendFailureNotifications()
		return fmt.Errorf("步骤13应用服务部署失败: %v", err)
	}

	// 检查是否为双版本部署模式，非双版本项目不应该使用此处理器
	if !common.HasVersionStructure(r.project) {
		common.AppLogger.Warning("警告：单版本项目不应使用双版本处理器，建议使用SingleVersionProcessor")
		common.AppLogger.Info("项目使用单版本结构，部署流程在步骤13完成")
		// 发送任务完成通知（任务级别）
		endTime := time.Now().Format("2006-01-02 15:04:05")
		if err := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "complete", r.opsURL, r.proURL, r.stepDurations); err != nil {
			common.AppLogger.Error("发送任务完成通知失败:", err)
		}
		// 发送飞书完成通知
		if err := common.SendFeishuCard(r.opsURL, r.project, r.tag, "complete", r.startedAt, endTime, "double", "", r.projectName); err != nil {
			common.AppLogger.Error("发送飞书卡片通知失败:", err)
		}
		common.AppLogger.Info("双版本部署请求处理完成", fmt.Sprintf("项目=%s, 标签=%s", r.project, r.tag))
	}

	// 以下步骤仅适用于双版本部署模式
	// 步骤14：检查服务就绪状态
	if err := r.step14CheckServiceReady(); err != nil {
		if r.ctx.Err() == context.Canceled {
			return fmt.Errorf("步骤14检查服务就绪状态被取消: %v", err)
		}
		r.sendFailureNotifications()
		return fmt.Errorf("步骤14检查服务就绪状态失败: %v", err)
	}

	// 步骤15：流量切换
	if err := r.step15TrafficSwitching(); err != nil {
		if r.ctx.Err() == context.Canceled {
			return fmt.Errorf("步骤15流量切换被取消: %v", err)
		}
		r.sendFailureNotifications()
		return fmt.Errorf("步骤15流量切换失败: %v", err)
	}

	// 步骤16：清理旧版本
	if err := r.step16CleanupOldVersion(); err != nil {
		if r.ctx.Err() == context.Canceled {
			return fmt.Errorf("步骤16清理旧版本被取消: %v", err)
		}
		r.sendFailureNotifications()
		return fmt.Errorf("步骤16清理旧版本失败: %v", err)
	}

	// 发送任务完成通知（任务级别）
	endTime := time.Now().Format("2006-01-02 15:04:05")
	if err := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "complete", r.opsURL, r.proURL, r.stepDurations); err != nil {
		common.AppLogger.Error("发送任务完成通知失败:", err)
	}
	// 发送飞书完成通知
	if err := common.SendFeishuCard(r.opsURL, r.project, r.tag, "complete", r.startedAt, endTime, "double", "", r.projectName); err != nil {
		common.AppLogger.Error("发送飞书卡片通知失败:", err)
	}
	common.AppLogger.Info("双版本部署请求处理完成", fmt.Sprintf("项目=%s, 标签=%s", r.project, r.tag))
	return nil
}

// step9PullOnline 步骤9：拉取在线镜像
func (r *DoubleVersionProcessor) step9PullOnline() error {
	stepName := "拉取在线镜像"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "start", "开始拉取在线镜像", r.project, r.tag)

	common.AppLogger.Info("执行步骤9：拉取在线镜像")

	// 获取需要拉取的镜像列表
	images, err := getOnlineImages(r.project, r.tag, r.taskLogger, "pullOnline")
	if err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("pullOnline", "ERROR", fmt.Sprintf("获取镜像列表失败: %v", err))
		}
		common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "failed", fmt.Sprintf("获取镜像列表失败: %v", err), r.project, r.tag)
		return err
	}

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "cancel", "取消拉取在线镜像", r.project, r.tag)
		// 任务级取消通知
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送任务取消通知失败:", notifyErr)
		}
		return r.ctx.Err()
	default:
	}

	// 使用9-pullOnline模块拉取镜像（可取消）
	puller := pullOnline.NewImagePuller(r.taskID, r.taskLogger)

	// 清理旧镜像
	if err := puller.CleanProjectImages(r.ctx, r.project); err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("pullOnline", "WARNING", fmt.Sprintf("清理旧镜像失败: %v", err))
		}
		// 清理失败不中断流程，继续拉取
	}

	if err := puller.PullImages(r.ctx, images); err != nil {
		// 检查是否是取消操作
		if r.ctx.Err() == context.Canceled {
			common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "cancel", fmt.Sprintf("拉取镜像被取消: %v", err), r.project, r.tag)
			r.sendCancelNotifications()
			return r.ctx.Err()
		} else {
			if r.taskLogger != nil {
				r.taskLogger.WriteStep("pullOnline", "ERROR", fmt.Sprintf("拉取镜像失败: %v", err))
			}
			common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "failed", fmt.Sprintf("拉取镜像失败: %v", err), r.project, r.tag)
			return err
		}
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 9, "pullOnline", stepName, "success", "拉取在线镜像完成", r.project, r.tag)
	common.AppLogger.Info("步骤9完成：拉取在线镜像")
	return nil
}

// step10TagImages 步骤10：标记镜像
func (r *DoubleVersionProcessor) step10TagImages() error {
	stepName := "标记镜像"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "start", "开始标记镜像", r.project, r.tag)

	common.AppLogger.Info("执行步骤10：标记镜像")

	// 获取在线镜像和本地镜像列表
	onlineImages, err := getOnlineImages(r.project, r.tag, r.taskLogger, "tagImages")
	if err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("tagImages", "ERROR", fmt.Sprintf("获取在线镜像列表失败: %v", err))
		}
		common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "failed", fmt.Sprintf("获取在线镜像列表失败: %v", err), r.project, r.tag)
		return err
	}

	localImages, err := getLocalImages(r.project, r.tag, r.taskLogger, "tagImages")
	if err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("tagImages", "ERROR", fmt.Sprintf("获取本地镜像列表失败: %v", err))
		}
		common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "failed", fmt.Sprintf("获取本地镜像列表失败: %v", err), r.project, r.tag)
		return err
	}

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "cancel", "取消标记镜像", r.project, r.tag)
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送任务取消通知失败:", notifyErr)
		}
		return r.ctx.Err()
	default:
	}

	// 使用10-tagImage模块标记镜像（可取消）
	if err := tagImage.TagImages(r.ctx, onlineImages, localImages, r.taskID, r.taskLogger); err != nil {
		// 检查是否是取消操作
		if r.ctx.Err() == context.Canceled {
			common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "cancel", fmt.Sprintf("标记镜像被取消: %v", err), r.project, r.tag)
			r.sendCancelNotifications()
			return r.ctx.Err()
		} else {
			if r.taskLogger != nil {
				r.taskLogger.WriteStep("tagImages", "ERROR", fmt.Sprintf("标记镜像失败: %v", err))
			}
			common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "failed", fmt.Sprintf("标记镜像失败: %v", err), r.project, r.tag)
			return err
		}
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 10, "tagImages", stepName, "success", "标记镜像完成", r.project, r.tag)
	common.AppLogger.Info("步骤10完成：标记镜像")
	return nil
}

// step11PushLocal 步骤11：推送本地镜像
func (r *DoubleVersionProcessor) step11PushLocal() error {
	stepName := "推送本地镜像"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "start", "开始推送本地镜像", r.project, r.tag)

	common.AppLogger.Info("执行步骤11：推送本地镜像")

	// 获取需要推送的镜像列表
	images, err := getLocalImages(r.project, r.tag, r.taskLogger, "pushLocal")
	if err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("pushLocal", "ERROR", fmt.Sprintf("获取本地镜像列表失败: %v", err))
		}
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
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送任务取消通知失败:", notifyErr)
		}
		return r.ctx.Err()
	default:
	}

	// 使用11-pushLocal模块推送镜像（可取消）
	pusher := pushLocal.NewImagePusher(r.taskID, r.taskLogger)
	if err := pusher.PushImages(r.ctx, images); err != nil {
		// 检查是否是取消操作
		if r.ctx.Err() == context.Canceled {
			common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "cancel", fmt.Sprintf("推送镜像被取消: %v", err), r.project, r.tag)
			r.sendCancelNotifications()
			return r.ctx.Err()
		} else {
			if r.taskLogger != nil {
				r.taskLogger.WriteStep("pushLocal", "ERROR", fmt.Sprintf("推送镜像失败: %v", err))
			}
			common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "failed", fmt.Sprintf("推送镜像失败: %v", err), r.project, r.tag)
			return err
		}
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 11, "pushLocal", stepName, "success", "推送本地镜像完成", r.project, r.tag)
	common.AppLogger.Info("步骤11完成：推送本地镜像")
	return nil
}

// step12CheckImage 步骤12：检查镜像
func (r *DoubleVersionProcessor) step12CheckImage() error {
	stepName := "检查镜像"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 12, "checkImage", stepName, "start", "开始检查镜像", r.project, r.tag)

	common.AppLogger.Info("执行步骤12：检查镜像")

	// 获取需要检查的镜像列表（仅检查离线仓库Harbor中的镜像）
	images, err := getLocalImages(r.project, r.tag, r.taskLogger, "checkImage")
	if err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("checkImage", "ERROR", fmt.Sprintf("获取镜像列表失败: %v", err))
		}
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
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送任务取消通知失败:", notifyErr)
		}
		return r.ctx.Err()
	default:
	}

	// 使用12-checkImage模块检查镜像（显式传入项目与标签，可取消）
	if err := checkImage.CheckImages(r.ctx, images, r.project, r.tag, r.taskID, r.taskLogger); err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("checkImage", "ERROR", fmt.Sprintf("检查镜像失败: %v", err))
		}
		common.SendStepNotification(r.taskID, 12, "checkImage", stepName, "failed", fmt.Sprintf("检查镜像失败: %v", err), r.project, r.tag)
		return err
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 12, "checkImage", stepName, "success", "检查镜像完成", r.project, r.tag)
	common.AppLogger.Info("步骤12完成：检查镜像")
	return nil
}

// step13DeployService 步骤13：应用服务部署
func (r *DoubleVersionProcessor) step13DeployService() error {
	stepName := "应用服务部署"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 13, "deployService", stepName, "start", "开始应用服务部署", r.project, r.tag)

	common.AppLogger.Info("执行步骤13：应用服务部署")

	// 获取下一个版本的部署目录（统一处理单副本和双副本）
	deployDir, err := common.GetDeploymentPath(r.project)
	if err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("deployService", "ERROR", fmt.Sprintf("获取部署目录失败: %v", err))
		}
		common.SendStepNotification(r.taskID, 13, "deployService", stepName, "failed", fmt.Sprintf("获取部署目录失败: %v", err), r.project, r.tag)
		return err
	}

	if r.taskLogger != nil {
		r.taskLogger.WriteStep("deployService", "INFO", fmt.Sprintf("使用部署目录: %s", deployDir))
	}

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 13, "deployService", stepName, "cancel", "取消应用服务部署", r.project, r.tag)
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送任务取消通知失败:", notifyErr)
		}
		return r.ctx.Err()
	default:
	}

	// 使用13-deployService模块部署服务（可取消）
	deployer := deployService.NewServiceDeployer(r.taskID, r.taskLogger)
	if err := deployer.DeployServices(r.ctx, deployDir, r.project, r.tag); err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("deployService", "ERROR", fmt.Sprintf("应用服务部署失败: %v", err))
		}
		common.SendStepNotification(r.taskID, 13, "deployService", stepName, "failed", fmt.Sprintf("应用服务部署失败: %v", err), r.project, r.tag)
		return err
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 13, "deployService", stepName, "success", "应用服务部署完成", r.project, r.tag)
	common.AppLogger.Info("步骤13完成：应用服务部署")
	return nil
}

// step14CheckServiceReady 步骤14：检查服务就绪状态
func (r *DoubleVersionProcessor) step14CheckServiceReady() error {
	stepName := "检查服务就绪"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 14, "checkService", stepName, "start", "开始检查服务就绪状态", r.project, r.tag)

	common.AppLogger.Info("执行步骤14：检查服务就绪状态")

	// 检查是否为双副本部署模式
	if !common.HasVersionStructure(r.project) {
		common.AppLogger.Info("项目使用单版本结构，跳过服务就绪检查")
		common.SendStepNotification(r.taskID, 14, "checkService", stepName, "success", "单版本结构，跳过服务就绪检查", r.project, r.tag)
		return nil
	}

	// 获取服务列表
	services, err := getServices(r.project, r.taskLogger, "checkService")
	if err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("获取服务列表失败: %v", err))
		}
		common.SendStepNotification(r.taskID, 14, "checkService", stepName, "failed", fmt.Sprintf("获取服务列表失败: %v", err), r.project, r.tag)
		return err
	}

	if len(services) == 0 {
		common.AppLogger.Info("没有需要检查的服务")
		common.SendStepNotification(r.taskID, 14, "checkService", stepName, "success", "没有需要检查的服务", r.project, r.tag)
		return nil
	}

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 14, "checkService", stepName, "cancel", "取消检查服务就绪", r.project, r.tag)
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送任务取消通知失败:", notifyErr)
		}
		return r.ctx.Err()
	default:
	}

	// 生成正确的namespace（检查刚刚部署的服务）
	namespace := getNamespace(r.project, "next", r.taskLogger, "checkService")

	// 使用14-checkService模块检查服务就绪状态（可取消）
	checker := checkService.NewServiceChecker(r.taskID, r.taskLogger)
	if err := checker.CheckServicesReady(r.ctx, services, namespace); err != nil {
		// 检查是否是取消操作
		if r.ctx.Err() == context.Canceled {
			common.SendStepNotification(r.taskID, 14, "checkService", stepName, "cancel", fmt.Sprintf("检查服务就绪被取消: %v", err), r.project, r.tag)
			r.sendCancelNotifications()
			return r.ctx.Err()
		} else {
			if r.taskLogger != nil {
				r.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("检查服务就绪失败: %v", err))
			}
			common.SendStepNotification(r.taskID, 14, "checkService", stepName, "failed", fmt.Sprintf("检查服务就绪失败: %v", err), r.project, r.tag)
			return err
		}
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 14, "checkService", stepName, "success", "检查服务就绪完成", r.project, r.tag)
	common.AppLogger.Info("步骤14完成：检查服务就绪状态")
	return nil
}

// step15TrafficSwitching 步骤15：流量切换
func (r *DoubleVersionProcessor) step15TrafficSwitching() error {
	stepName := "流量切换"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 15, "trafficSwitching", stepName, "start", "开始执行流量切换", r.project, r.tag)
	common.AppLogger.Info("执行步骤15：流量切换")

	// 检查是否为双副本部署模式
	if !common.HasVersionStructure(r.project) {
		common.AppLogger.Info("项目使用单版本结构，跳过流量切换")
		common.SendStepNotification(r.taskID, 15, "trafficSwitching", stepName, "success", "单版本结构，跳过流量切换", r.project, r.tag)
		return nil
	}

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 15, "trafficSwitching", stepName, "cancel", "取消流量切换", r.project, r.tag)
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送任务取消通知失败:", notifyErr)
		}
		return r.ctx.Err()
	default:
	}

	// 获取新部署的namespace
	namespace := getNamespace(r.project, "next", r.taskLogger, "trafficSwitching")

	// 从namespace直接提取版本
	var version string
	if strings.Contains(namespace, "-v1") {
		version = "v1"
	} else if strings.Contains(namespace, "-v2") {
		version = "v2"
	} else {
		version = "v1" // 默认版本
	}

	// 获取nginx配置目录（可以从配置文件或环境变量获取）
	nginxConfDir := getNginxConfDir()

	// 创建流量切换器
	switcher := trafficSwitching.NewTrafficSwitcher(namespace, r.project, version, nginxConfDir, r.taskLogger)

	// 执行流量切换
	if err := switcher.Execute(r.ctx, nil); err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("trafficSwitching", "ERROR", fmt.Sprintf("流量切换失败: %v", err))
		}
		common.SendStepNotification(r.taskID, 15, "trafficSwitching", stepName, "failed", fmt.Sprintf("流量切换失败: %v", err), r.project, r.tag)
		return err
	}

	// 更新当前版本信息
	if err := common.UpdateVersion(r.project, version); err != nil {
		common.AppLogger.Error("更新版本信息失败:", err)
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 15, "trafficSwitching", stepName, "success", "流量切换完成", r.project, r.tag)
	common.AppLogger.Info("步骤15完成：流量切换")
	return nil
}

// step16CleanupOldVersion 步骤16：清理旧版本
func (r *DoubleVersionProcessor) step16CleanupOldVersion() error {
	stepName := "清理旧版本"

	// 发送步骤开始通知
	common.SendStepNotification(r.taskID, 16, "cleanupOldVersion", stepName, "start", "开始清理旧版本", r.project, r.tag)
	common.AppLogger.Info("执行步骤16：清理旧版本")

	// 检查是否为双副本部署模式
	if !common.HasVersionStructure(r.project) {
		common.AppLogger.Info("项目使用单版本结构，跳过旧版本清理")
		common.SendStepNotification(r.taskID, 16, "cleanupOldVersion", stepName, "success", "单版本结构，跳过旧版本清理", r.project, r.tag)
		return nil
	}

	// 取消检查
	select {
	case <-r.ctx.Done():
		common.SendStepNotification(r.taskID, 16, "cleanupOldVersion", stepName, "cancel", "取消清理旧版本", r.project, r.tag)
		if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
			common.AppLogger.Error("发送任务取消通知失败:", notifyErr)
		}
		return r.ctx.Err()
	default:
	}

	// 获取要清理的旧版本信息
	// 由于第15步已经切换了流量，所以这里应该获取"next"（之前运行的旧版本），而不是"now"（当前运行的新版本）
	oldNamespace := getNamespace(r.project, "next", r.taskLogger, "cleanupOldVersion")
	oldPath := getDeploymentPath(r.project, "next", r.taskLogger, "cleanupOldVersion")

	if r.taskLogger != nil {
		r.taskLogger.WriteStep("cleanupOldVersion", "INFO", fmt.Sprintf("当前版本: %s, 将清理旧版本: %s (路径: %s)",
			getNamespace(r.project, "now", r.taskLogger, "cleanupOldVersion"), oldNamespace, oldPath))
	}

	// 创建版本清理器，直接传入要删除的目标
	cleaner := cleanupOldVersion.NewVersionCleaner(oldNamespace, oldPath, r.taskLogger)

	// 执行清理
	if err := cleaner.Execute(r.ctx, nil); err != nil {
		if r.taskLogger != nil {
			r.taskLogger.WriteStep("cleanupOldVersion", "ERROR", fmt.Sprintf("清理旧版本失败: %v", err))
		}
		common.SendStepNotification(r.taskID, 16, "cleanupOldVersion", stepName, "failed", fmt.Sprintf("清理旧版本失败: %v", err), r.project, r.tag)
		return err
	}

	// 发送步骤完成通知
	common.SendStepNotification(r.taskID, 16, "cleanupOldVersion", stepName, "success", "清理旧版本完成", r.project, r.tag)
	common.AppLogger.Info("步骤16完成：清理旧版本")
	return nil
}

// sendFailureNotifications 发送失败通知（包括任务通知和飞书通知）
func (r *DoubleVersionProcessor) sendFailureNotifications() {
	endTime := time.Now().Format("2006-01-02 15:04:05")

	// 发送任务失败通知
	if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "failed", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
		common.AppLogger.Error("发送任务失败通知失败:", notifyErr)
	}

	// 发送飞书失败通知
	if feishuErr := common.SendFeishuCard(r.opsURL, r.project, r.tag, "failed", r.startedAt, endTime, "double", "", r.projectName); feishuErr != nil {
		common.AppLogger.Error("发送飞书失败通知失败:", feishuErr)
	}
}

// sendCancelNotifications 发送取消通知（包括任务通知和飞书通知）
func (r *DoubleVersionProcessor) sendCancelNotifications() {
	endTime := time.Now().Format("2006-01-02 15:04:05")

	// 发送任务取消通知
	if notifyErr := common.SendTaskNotification(r.taskID, r.project, r.startedAt, "cancel", r.opsURL, r.proURL, r.stepDurations); notifyErr != nil {
		common.AppLogger.Error("发送任务取消通知失败:", notifyErr)
	}

	// 发送飞书取消通知
	if feishuErr := common.SendFeishuCard(r.opsURL, r.project, r.tag, "cancel", r.startedAt, endTime, "double", "", r.projectName); feishuErr != nil {
		common.AppLogger.Error("发送飞书取消通知失败:", feishuErr)
	}
}
