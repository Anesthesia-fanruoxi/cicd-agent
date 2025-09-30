package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"cicd-agent/config"
)

// UnifiedNotificationData 统一通知数据结构
type UnifiedNotificationData struct {
	// 通用字段
	IsStep bool `json:"isset"` // true=步骤通知, false=任务通知

	// 任务通知字段
	ID            string                 `json:"id"`                       // 任务ID
	Name          string                 `json:"name,omitempty"`           // 项目名称
	Description   string                 `json:"description,omitempty"`    // 项目描述
	GitURL        string                 `json:"git_url,omitempty"`        // Git仓库地址
	OpsURL        string                 `json:"ops_feishu_url,omitempty"` // 运维飞书URL
	FeishuURL     string                 `json:"pro_feishu_url,omitempty"` // 产品飞书URL
	StartedAt     string                 `json:"started_at,omitempty"`     // 开始时间
	Type          string                 `json:"type,omitempty"`           // 任务类型
	FinishedAt    string                 `json:"finished_at"`              // 结束时间
	Status        string                 `json:"status,omitempty"`         // 状态 (running/complete/cancel)
	Remote        string                 `json:"remote,omitempty"`         // 来源（agent/server），此处固定为agent
	StepDurations map[string]interface{} `json:"step_durations,omitempty"` // 任务各步骤耗时（秒）

	// 步骤通知字段
	Step           int     `json:"step,omitempty"`             // 步骤编号
	StepType       string  `json:"step_type,omitempty"`        // 步骤类型
	StepStartedAt  string  `json:"step_started_at,omitempty"`  // 步骤开始时间
	StepFinishedAt string  `json:"step_finished_at,omitempty"` // 步骤完成时间
	StepName       string  `json:"step_name,omitempty"`        // 步骤名称
	StepStatus     string  `json:"step_status,omitempty"`      // 步骤状态 (success/failed/cancel)
	Duration       float64 `json:"duration"`                   // 持续时间(秒，保留2位小数)
	LastDuration   float64 `json:"last_duration"`              // 上一个步骤的耗时(秒，保留2位小数)
	EstimatedEnd   string  `json:"estimated_end,omitempty"`    // 预计结束时间
}

// NotificationResponse 通知响应结构
type NotificationResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

// 步骤开始时间记录
var stepStartTimes = make(map[string]time.Time)

// SendStepNotification 发送步骤通知
func SendStepNotification(taskID string, step int, stepType, stepName, status, message, project, tag string) error {
	// 获取通知URL
	notifyURL := getNotifyURL()
	if notifyURL == "" {
		AppLogger.Info("通知功能未启用或URL未配置，跳过通知发送")
		return nil
	}

	// 步骤键值，用于记录开始时间 - 统一使用step_stepType格式
	stepKey := fmt.Sprintf("step_%d_%s", step, stepType)
	currentTime := time.Now()

	// 转换状态格式
	var stepStatus string
	switch status {
	case "start":
		stepStatus = "running"
	case "success":
		stepStatus = "success"
	case "failed":
		stepStatus = "failed"
	case "cancel":
		stepStatus = "cancel"
	default:
		stepStatus = "running"
	}

	// 构建通知数据
	notificationData := UnifiedNotificationData{
		IsStep:     true, // 步骤通知
		ID:         taskID,
		Step:       step,
		StepType:   stepType,
		StepName:   stepName,
		StepStatus: stepStatus,
		Remote:     "agent",
	}

	// 计算 last_duration 和 estimated_end
	notificationData.LastDuration = getLastStepDuration(project, stepKey)
	notificationData.EstimatedEnd = calculateEstimatedEnd(project, stepKey)

	// 调试日志
	AppLogger.Info(fmt.Sprintf("步骤 %s(%s) - 上次耗时: %.2f秒, 预计结束: %s", stepName, stepKey, notificationData.LastDuration, notificationData.EstimatedEnd))

	// 设置步骤开始时间 - 兼容新旧格式
	var startTime time.Time
	var exists bool
	var keyToDelete string

	// 优先使用新格式查找
	if startTime, exists = stepStartTimes[stepKey]; exists {
		keyToDelete = stepKey
	} else {
		// 兼容旧格式：taskID_step_stepType
		oldStepKey := fmt.Sprintf("%s_%d_%s", taskID, step, stepType)
		if startTime, exists = stepStartTimes[oldStepKey]; exists {
			keyToDelete = oldStepKey
			AppLogger.Info(fmt.Sprintf("使用旧格式键值找到开始时间: %s", oldStepKey))
		}
	}

	if exists {
		notificationData.StepStartedAt = startTime.Format("2006-01-02 15:04:05")

		// 如果是完成状态，设置完成时间和持续时间
		if status == "success" || status == "failed" || status == "cancel" {
			notificationData.StepFinishedAt = currentTime.Format("2006-01-02 15:04:05")
			// 计算持续时间并转换为秒数，保留2位小数
			durationMs := currentTime.Sub(startTime).Milliseconds()
			notificationData.Duration = math.Round(float64(durationMs)/1000.0*100) / 100
			// 清理已完成步骤的开始时间记录
			delete(stepStartTimes, keyToDelete)
		}
	} else if status == "start" {
		// 如果是开始状态但没有记录，使用当前时间并记录
		notificationData.StepStartedAt = currentTime.Format("2006-01-02 15:04:05")
		stepStartTimes[stepKey] = currentTime
	}

	// 序列化为JSON
	jsonData, err := json.Marshal(notificationData)
	if err != nil {
		return fmt.Errorf("序列化通知数据失败: %v", err)
	}

	AppLogger.Info(fmt.Sprintf("发送%s通知到: %s", stepType, notifyURL))
	AppLogger.Info(fmt.Sprintf("发送的JSON数据: %s", string(jsonData)))

	// 加密和压缩数据
	encryptedData, err := CompressAndEncrypt(jsonData)
	if err != nil {
		return fmt.Errorf("加密数据失败: %v", err)
	}

	// 构建请求体
	requestBody := map[string]interface{}{
		"code": 200,
		"msg":  "success",
		"data": encryptedData,
	}

	requestJson, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("序列化请求体失败: %v", err)
	}

	// 发送HTTP请求
	AppLogger.Info(fmt.Sprintf("正在发送HTTP请求到: %s", notifyURL))
	resp, err := http.Post(notifyURL, "application/json", bytes.NewReader(requestJson))
	if err != nil {
		AppLogger.Error(fmt.Sprintf("发送通知请求失败: %v", err))
		return fmt.Errorf("发送通知请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("读取响应失败: %v", err))
		return fmt.Errorf("读取响应失败: %v", err)
	}

	AppLogger.Info(fmt.Sprintf("收到响应状态码: %d", resp.StatusCode))
	AppLogger.Info(fmt.Sprintf("响应内容: %s", string(respBody)))

	// 检查响应状态
	if resp.StatusCode != 200 {
		AppLogger.Error(fmt.Sprintf("远程接口返回错误状态码 %d: %s", resp.StatusCode, string(respBody)))
		return fmt.Errorf("远程接口返回错误: %s", string(respBody))
	}

	AppLogger.Info("通知发送成功")

	// 通知发送成功后，如果是完成状态，才更新版本文件中的步骤耗时
	if status == "success" || status == "failed" || status == "cancel" {
		if notificationData.Duration > 0 {
			AppLogger.Info(fmt.Sprintf("开始更新步骤耗时到文件: %s = %.2f秒", stepKey, notificationData.Duration))
			updateStepDurationInFile(project, stepKey, notificationData.Duration)
		} else {
			AppLogger.Warning(fmt.Sprintf("步骤 %s 的耗时为0，跳过文件更新", stepKey))
		}
	} else {
		AppLogger.Info(fmt.Sprintf("步骤 %s 状态为 %s，不需要更新文件", stepKey, status))
	}

	return nil
}

// getLastStepDuration 获取指定步骤的上次耗时（秒数，保留2位小数）
func getLastStepDuration(project, stepName string) float64 {
	// 对于web项目，不需要获取历史耗时信息
	if strings.Contains(project, "-web") {
		return 0.0
	}

	versionInfo, err := GetCurrentVersion(project)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("获取项目版本信息失败: %v", err))
		return 0.0
	}

	if duration, ok := versionInfo.StepDurations[stepName]; ok {
		if d, ok := duration.(float64); ok {
			// 返回秒数，保留2位小数
			return math.Round(d*100) / 100
		}
	}
	return 0.0
}

// calculateEstimatedEnd 计算当前步骤的预计结束时间
func calculateEstimatedEnd(project, currentStepName string) string {
	// 获取当前步骤的上次执行耗时
	lastDuration := getLastStepDuration(project, currentStepName)

	// 如果没有历史数据，使用默认估算时间（30秒）
	if lastDuration == 0 {
		lastDuration = 30.0
	}

	// 当前步骤预估结束时间 = 当前时间 + 上次执行耗时
	estimatedTime := time.Now().Add(time.Duration(lastDuration) * time.Second)
	return estimatedTime.Format("2006-01-02 15:04:05")
}

// updateStepDurationInFile 更新版本文件中的步骤耗时（不修改版本信息）
func updateStepDurationInFile(project, stepName string, durationSeconds float64) {
	AppLogger.Info(fmt.Sprintf("正在更新步骤耗时: 项目=%s, 步骤=%s, 耗时=%.2f秒", project, stepName, durationSeconds))

	// 存储为秒数，保留2位小数
	roundedDuration := math.Round(durationSeconds*100) / 100
	AppLogger.Info(fmt.Sprintf("设置步骤耗时: %s = %.2f秒", stepName, roundedDuration))

	// 使用统一的步骤耗时更新方法
	AppLogger.Info(fmt.Sprintf("开始保存步骤耗时到磁盘: 项目=%s", project))
	if err := UpdateStepDuration(project, stepName, roundedDuration); err != nil {
		AppLogger.Error(fmt.Sprintf("保存步骤耗时失败: %v", err))
	} else {
		AppLogger.Info(fmt.Sprintf("成功保存步骤耗时! 项目 %s 步骤 %s: %.2f秒", project, stepName, roundedDuration))
	}
}

// getNotifyURL 获取通知URL
func getNotifyURL() string {
	if !config.AppConfig.Notification.Enable {
		return ""
	}
	return config.AppConfig.Notification.NotifyURL
}

// SendTaskNotification 发送任务级别通知（最终完成/取消/失败）
func SendTaskNotification(taskID, name, startedAt, status string, opsURL, proURL string, stepDurations map[string]interface{}) error {
	// 获取通知URL
	notifyURL := getNotifyURL()
	if notifyURL == "" {
		AppLogger.Info("通知功能未启用或URL未配置，跳过任务通知发送")
		return nil
	}

	// 规范状态
	normStatus := status
	switch status {
	case "complete", "failed", "cancel", "running":
		// ok
	default:
		normStatus = "complete"
	}

	// 构建任务通知数据（IsStep=false）
	notificationData := UnifiedNotificationData{
		IsStep:        false,
		ID:            taskID,
		Name:          name,
		StartedAt:     startedAt,
		FinishedAt:    time.Now().Format("2006-01-02 15:04:05"),
		Status:        normStatus,
		Remote:        "agent",
		OpsURL:        opsURL,
		FeishuURL:     proURL,
		StepDurations: stepDurations,
	}

	// 序列化为JSON
	jsonData, err := json.Marshal(notificationData)
	if err != nil {
		return fmt.Errorf("序列化任务通知数据失败: %v", err)
	}

	AppLogger.Info(fmt.Sprintf("发送的JSON数据: %s", string(jsonData)))

	// 加密和压缩数据
	encryptedData, err := CompressAndEncrypt(jsonData)
	if err != nil {
		return fmt.Errorf("加密任务通知数据失败: %v", err)
	}

	// 构建请求体
	requestBody := map[string]interface{}{
		"code": 200,
		"msg":  "success",
		"data": encryptedData,
	}

	requestJson, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("序列化任务请求体失败: %v", err)
	}

	// 发送HTTP请求
	AppLogger.Info(fmt.Sprintf("正在发送任务通知HTTP请求到: %s", notifyURL))
	resp, err := http.Post(notifyURL, "application/json", bytes.NewReader(requestJson))
	if err != nil {
		AppLogger.Error(fmt.Sprintf("发送任务通知请求失败: %v", err))
		return fmt.Errorf("发送任务通知请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("读取任务通知响应失败: %v", err))
		return fmt.Errorf("读取任务通知响应失败: %v", err)
	}

	AppLogger.Info(fmt.Sprintf("任务通知响应状态码: %d", resp.StatusCode))
	AppLogger.Info(fmt.Sprintf("任务通知响应内容: %s", string(respBody)))

	// 检查响应状态
	if resp.StatusCode != 200 {
		AppLogger.Error(fmt.Sprintf("任务通知远程接口返回错误状态码 %d: %s", resp.StatusCode, string(respBody)))
		return fmt.Errorf("远程接口返回错误: %s", string(respBody))
	}

	AppLogger.Info("任务通知发送成功")
	return nil
}
