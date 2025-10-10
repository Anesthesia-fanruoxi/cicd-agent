package trafficSwitching

import (
	"bytes"
	"cicd-agent/common"
	"cicd-agent/config"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ProxySwitcher 流量代理切换器
type ProxySwitcher struct {
	version    string             // 当前部署的版本 (v1/v2)
	proxyURL   string             // 代理服务地址
	taskLogger *common.TaskLogger // 任务日志器
}

// NewProxySwitcher 创建流量代理切换器
func NewProxySwitcher(version string, taskLogger *common.TaskLogger) *ProxySwitcher {
	return &ProxySwitcher{
		version:    version,
		proxyURL:   config.AppConfig.GetTrafficProxyURL(),
		taskLogger: taskLogger,
	}
}

// SwitchTrafficRequest 流量切换请求
type SwitchTrafficRequest struct {
	Version string `json:"version"`
}

// Execute 执行流量代理切换
func (ps *ProxySwitcher) Execute(ctx context.Context) error {
	if ps.taskLogger != nil {
		ps.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("开始通过流量代理切换流量，目标版本: %s", ps.version))
	}

	// 调用流量代理接口（传入的version就是目标版本）
	if err := ps.callProxySwitch(ctx, ps.version); err != nil {
		if ps.taskLogger != nil {
			ps.taskLogger.WriteStep("trafficSwitching", "ERROR", fmt.Sprintf("流量切换失败: %v", err))
		}
		return fmt.Errorf("流量切换失败: %v", err)
	}

	if ps.taskLogger != nil {
		ps.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("流量切换成功，已切换到版本: %s", ps.version))
	}
	return nil
}

// callProxySwitch 调用流量代理切换接口
func (ps *ProxySwitcher) callProxySwitch(ctx context.Context, targetVersion string) error {
	// 构建请求URL
	switchURL := fmt.Sprintf("%s/switch", ps.proxyURL)

	if ps.taskLogger != nil {
		ps.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("调用流量代理接口: %s", switchURL))
	}

	// 构建请求体
	reqBody := SwitchTrafficRequest{
		Version: targetVersion,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("构建请求体失败: %v", err)
	}

	if ps.taskLogger != nil {
		ps.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("请求参数: %s", string(jsonData)))
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", switchURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建HTTP请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 发送请求（设置超时时间）
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	if ps.taskLogger != nil {
		ps.taskLogger.WriteStep("trafficSwitching", "INFO", "发送流量切换请求...")
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	respBody, _ := io.ReadAll(resp.Body)

	if ps.taskLogger != nil {
		ps.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("响应状态码: %d", resp.StatusCode))
		if len(respBody) > 0 {
			ps.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("响应内容: %s", string(respBody)))
		}
	}

	// 验证响应状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("流量切换失败，后端健康检查未通过，状态码: %d, 响应: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
