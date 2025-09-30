package taskCenter

import (
	"bytes"
	"cicd-agent/common"
	"cicd-agent/config"
	"cicd-agent/taskStep/javaBuild"
	"cicd-agent/taskStep/webBuild"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strings"
	"time"
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
		common.AppLogger.Info("手动解析的JSON数据:", fmt.Sprintf("%+v", rawData))
	}

	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.AppLogger.Error("请求参数绑定失败:", err)
		common.AppLogger.Error("期望的结构体:", fmt.Sprintf("%+v", UpdateRequest{}))
		c.JSON(http.StatusBadRequest, Response{Code: 400, Msg: "请求参数错误"})
		return
	}

	common.AppLogger.Info("收到更新请求:", fmt.Sprintf("项目=%s, 类型=%s, 分类=%s", req.Project, req.Type, req.Category))

	// 所有请求都进行远程调用
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
	common.AppLogger.Info("收到回调请求，原始数据:", string(body))

	// 重新设置请求体
	c.Request.Body = http.NoBody
	if len(body) > 0 {
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
	}

	var req CallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.AppLogger.Error("回调参数绑定失败:", err)
		c.JSON(http.StatusBadRequest, Response{
			Code: 400,
			Msg:  fmt.Sprintf("请求参数错误: %v", err),
		})
		return
	}

	common.AppLogger.Info("解析后的回调参数:", fmt.Sprintf("%+v", req))

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

		// 根据项目名称判断构建类型
		// 如果项目名称包含"-web"，则使用webBuildApi，否则使用javaBuildApi
		if strings.Contains(req.Project, "-web") {
			// Web项目构建
			processor := webBuild.NewRemoteProcessor(
				req.Project,
				req.Category,
				req.Tag,
				req.Description,
				taskID,
				ctx,
				req.OpsFeishuURL,
				req.ProFeishuURL,
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
		} else {
			// Java项目构建 - 根据项目版本结构选择处理器
			if common.HasVersionStructure(req.Project) {
				// 双版本部署
				processor := javaBuild.NewDoubleVersionProcessor(
					req.Project,
					req.Tag,
					req.Description,
					taskID,
					ctx,
					req.OpsFeishuURL,
					req.ProFeishuURL,
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
				// 单版本部署
				processor := javaBuild.NewSingleVersionProcessor(
					req.Project,
					req.Category,
					req.Tag,
					req.Description,
					taskID,
					ctx,
					req.OpsFeishuURL,
					req.ProFeishuURL,
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
	var req CancelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{Code: 400, Msg: "请求参数错误: " + err.Error()})
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

	common.AppLogger.Info("构建的回调URL:", callbackURL)

	// 构建远程调用请求
	remoteReq := RemoteCallRequest{
		Project:     req.Project,
		CallbackURL: callbackURL,
		Category:    req.Category,
	}

	// 序列化请求
	jsonData, err := json.Marshal(remoteReq)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	common.AppLogger.Info("发送到远程服务的URL:", config.AppConfig.Remote.UpdateURL)
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
	common.AppLogger.Info("远程服务响应状态:", resp.StatusCode)
	common.AppLogger.Info("远程服务响应内容:", string(respBody))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("远程服务返回错误状态: %d, 响应内容: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
