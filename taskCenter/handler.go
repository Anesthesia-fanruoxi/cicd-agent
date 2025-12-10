package taskCenter

import (
	"bytes"
	"cicd-agent/common"
	"cicd-agent/config"
	"cicd-agent/taskStep/javaBuild"
	"cicd-agent/taskStep/webBuild"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// HandleUpdate 处理更新请求
func HandleUpdate(c *gin.Context) {
	// 记录原始请求数据
	body, _ := c.GetRawData()
	common.AppLogger.Info("收到更新请求，原始数据:", string(body))

	// 重新设置请求体，因为GetRawData会消耗掉
	c.Request.Body = http.NoBody
	if len(body) > 0 {
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
	}

	// 尝试手动解析JSON来调试
	var rawData map[string]interface{}
	if err := json.Unmarshal(body, &rawData); err == nil {
		//common.AppLogger.Info("手动解析的JSON数据:", fmt.Sprintf("%+v", rawData))
	}

	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.AppLogger.Error("请求参数绑定失败:", err)
		common.AppLogger.Error("期望的结构体:", fmt.Sprintf("%+v", UpdateRequest{}))
		c.JSON(http.StatusBadRequest, Response{Code: 400, Msg: "请求参数错误"})
		return
	}

	//common.AppLogger.Info("收到更新请求:", fmt.Sprintf("项目=%s, 类型=%s, 分类=%s", req.Project, req.Type, req.Category))

	// 验证项目是否有效
	if !config.AppConfig.IsValidProject(req.Project) {
		errMsg := fmt.Sprintf("项目 %s 不在有效项目列表中", req.Project)
		common.AppLogger.Error("项目验证失败:", errMsg)
		c.JSON(http.StatusBadRequest, Response{Code: 400, Msg: errMsg})
		return
	}

	// 验证项目是否配置了部署目录（仅Java项目需要验证，Web项目可以自动创建目录）
	if req.Type != "web" {
		if _, exists := config.AppConfig.GetProjectPath(req.Project); !exists {
			errMsg := fmt.Sprintf("项目 %s 未配置部署目录", req.Project)
			common.AppLogger.Error("配置验证失败:", errMsg)
			c.JSON(http.StatusBadRequest, Response{Code: 400, Msg: errMsg})
			return
		}

		// 如果type为空，说明是后端项目，自动判断是double还是single
		if req.Type == "" {
			if config.AppConfig.IsDoubleProject(req.Project) {
				req.Type = "double"
			} else {
				req.Type = "single"
			}
		}
	}

	// 验证通过，进行远程调用
	if err := callRemoteAPI(req); err != nil {
		common.AppLogger.Error("调用远程API失败:", err)
		c.JSON(http.StatusInternalServerError, Response{Code: 500, Msg: "调用远程API失败"})
		return
	}
	c.JSON(http.StatusOK, Response{Code: 200, Msg: "远程API调用成功"})
}

// HandleCallback 处理回调请求
func HandleCallback(c *gin.Context) {
	// 记录原始回调数据
	body, _ := c.GetRawData()
	// common.AppLogger.Info("收到回调请求，原始数据:", string(body))

	// 重新设置请求体
	c.Request.Body = http.NoBody
	if len(body) > 0 {
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
	}

	// 直接解析明文回调请求
	var req CallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.AppLogger.Error("请求参数绑定失败:", err)
		c.JSON(http.StatusBadRequest, Response{
			Code: 400,
			Msg:  fmt.Sprintf("请求参数错误: %v", err),
		})
		return
	}

	// common.AppLogger.Info("解析后的回调参数:", fmt.Sprintf("%+v", req))

	// 只处理成功状态的回调
	if req.Status != "success" {
		common.AppLogger.Info("非成功状态的回调，跳过处理:", req.Status)
		c.JSON(http.StatusOK, Response{
			Code: 200,
			Msg:  "回调处理完成（非成功状态）",
		})
		return
	}

	// 记录成功构建任务
	common.AppLogger.Info("构建成功回调:", fmt.Sprintf("项目=%s, 标签=%s, 任务ID=%s, 完成时间=%s",
		req.Project, req.Tag, req.TaskID, req.FinishedAt))

	// 异步处理镜像拉取和推送，根据项目名称后缀判断构建类型
	go func() {
		// 使用任务ID或生成一个临时ID
		taskID := req.TaskID
		if taskID == "" {
			taskID = fmt.Sprintf("%s-%s-%d", req.Project, req.Tag, time.Now().Unix())
		}

		// 为任务创建可取消的上下文（供外部取消接口使用）
		ctx, _ := common.CreateTaskContext(taskID)

		// 根据type字段判断构建类型: web/double/single
		if req.Type == "web" {
			// Web项目构建
			processor := webBuild.NewRemoteProcessor(
				req.Project,
				req.Category,
				req.Tag,
				req.ProjectName,
				taskID,
				req.Type,
				ctx,
				req.UpdateFeishuURL,
				req.NotifyFeishuURL,
				req.CreateTime,
				req.StepDurations,
			)
			if err := processor.ProcessRemoteRequest(); err != nil {
				common.AppLogger.Error("web构建处理失败:", fmt.Sprintf("项目=%s, 标签=%s, 错误=%v",
					req.Project, req.Tag, err))
			} else {
				common.AppLogger.Info("web构建处理成功:", fmt.Sprintf("项目=%s, 标签=%s",
					req.Project, req.Tag))
			}
		} else if req.Type == "double" {
			// Java双版本部署
			processor := javaBuild.NewDoubleVersionProcessor(
				req.Project,
				req.Tag,
				req.ProjectName,
				taskID,
				req.Type,
				ctx,
				req.UpdateFeishuURL,
				req.NotifyFeishuURL,
				req.CreateTime,
				req.StepDurations,
			)
			if err := processor.ProcessDoubleVersionDeployment(); err != nil {
				common.AppLogger.Error("双版本java构建处理失败:", fmt.Sprintf("项目=%s, 标签=%s, 错误=%v",
					req.Project, req.Tag, err))
			} else {
				common.AppLogger.Info("双版本java构建处理成功:", fmt.Sprintf("项目=%s, 标签=%s",
					req.Project, req.Tag))
			}
		} else {
			// Java单版本部署 (type == "single" 或其他)
			processor := javaBuild.NewSingleVersionProcessor(
				req.Project,
				req.Category,
				req.Tag,
				req.ProjectName,
				taskID,
				req.Type,
				ctx,
				req.UpdateFeishuURL,
				req.NotifyFeishuURL,
				req.CreateTime,
				req.StepDurations,
			)
			if err := processor.ProcessSingleVersionDeployment(); err != nil {
				common.AppLogger.Error("单版本java构建处理失败:", fmt.Sprintf("项目=%s, 标签=%s, 错误=%v",
					req.Project, req.Tag, err))
			} else {
				common.AppLogger.Info("单版本java构建处理成功:", fmt.Sprintf("项目=%s, 标签=%s",
					req.Project, req.Tag))
			}
		}

		// 清理任务上下文
		common.CleanupTask(taskID)
	}()

	c.JSON(http.StatusOK, Response{
		Code: 200,
		Msg:  "回调处理成功",
	})
}

// HandleCancel 取消正在执行的任务
func HandleCancel(c *gin.Context) {
	// 直接解析明文取消请求
	var req CancelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.AppLogger.Error("取消请求参数绑定失败:", err)
		c.JSON(http.StatusBadRequest, Response{
			Code: 400,
			Msg:  fmt.Sprintf("请求参数错误: %v", err),
		})
		return
	}

	if ok := common.CancelTask(req.ID); ok {
		common.AppLogger.Info("收到取消任务请求:", req.ID)
		c.JSON(http.StatusOK, Response{Code: 200, Msg: "任务取消信号已发送"})
		return
	}

	c.JSON(http.StatusNotFound, Response{Code: 404, Msg: "未找到对应的任务或任务已结束"})
}

// callRemoteAPI 调用远程API
func callRemoteAPI(req UpdateRequest) error {
	// 构建回调URL
	callbackURL := config.AppConfig.GetCallbackURL()

	//common.AppLogger.Info("构建的回调URL:", callbackURL)

	// 构建远程调用请求
	remoteReq := RemoteCallRequest{
		Project:     req.Project,
		CallbackURL: callbackURL,
		Type:        req.Type,
		Category:    req.Category,
	}

	// 序列化请求
	jsonData, err := json.Marshal(remoteReq)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	//common.AppLogger.Info("发送到远程服务的URL:", config.AppConfig.Remote.UpdateURL)
	common.AppLogger.Info("发送到远程服务的数据:", string(jsonData))

	// 发送HTTP请求
	resp, err := http.Post(
		config.AppConfig.Remote.UpdateURL,
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return fmt.Errorf("发送请求失败: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(resp.Body)

	// 读取响应内容
	respBody, _ := io.ReadAll(resp.Body)
	//common.AppLogger.Info("远程服务响应状态:", resp.StatusCode)
	//common.AppLogger.Info("远程服务响应内容:", string(respBody))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("远程服务返回错误状态: %d, 响应内容: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
