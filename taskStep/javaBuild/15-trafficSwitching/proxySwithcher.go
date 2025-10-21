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
	"sync"
	"time"
)

// ProxySwitcher 流量代理切换器
type ProxySwitcher struct {
	version     string             // 当前部署的版本 (v1/v2)
	projectName string             // 项目名称
	proxyURLs   []string           // 代理服务地址列表
	taskLogger  *common.TaskLogger // 任务日志器
}

// NewProxySwitcher 创建流量代理切换器
func NewProxySwitcher(version string, projectName string, taskLogger *common.TaskLogger) *ProxySwitcher {
	return &ProxySwitcher{
		version:     version,
		projectName: projectName,
		proxyURLs:   config.AppConfig.GetTrafficProxyURLs(projectName),
		taskLogger:  taskLogger,
	}
}

// SwitchTrafficRequest 流量切换请求
type SwitchTrafficRequest struct {
	Version string `json:"version"`
}

// Execute 执行流量代理切换
func (ps *ProxySwitcher) Execute(ctx context.Context) error {
	if ps.taskLogger != nil {
		ps.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("开始通过流量代理切换流量，项目: %s, 目标版本: %s", ps.projectName, ps.version))
	}

	// 检查是否有代理地址
	if len(ps.proxyURLs) == 0 {
		if ps.taskLogger != nil {
			ps.taskLogger.WriteStep("trafficSwitching", "WARN", fmt.Sprintf("项目 %s 没有配置流量代理地址，跳过流量切换", ps.projectName))
		}
		return nil
	}

	if ps.taskLogger != nil {
		ps.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("找到 %d 个代理地址，将并发进行流量切换", len(ps.proxyURLs)))
	}

	// 并发调用所有代理地址
	if err := ps.switchAllProxies(ctx); err != nil {
		if ps.taskLogger != nil {
			ps.taskLogger.WriteStep("trafficSwitching", "ERROR", fmt.Sprintf("流量切换失败: %v", err))
		}
		return fmt.Errorf("流量切换失败: %v", err)
	}

	if ps.taskLogger != nil {
		ps.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("所有代理地址流量切换成功，已切换到版本: %s", ps.version))
	}
	return nil
}

// switchAllProxies 并发切换所有代理地址
func (ps *ProxySwitcher) switchAllProxies(ctx context.Context) error {
	var wg sync.WaitGroup
	errorChan := make(chan error, len(ps.proxyURLs))

	// 并发调用所有代理地址
	for _, proxyURL := range ps.proxyURLs {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			if err := ps.callProxySwitch(ctx, url, ps.version); err != nil {
				errorChan <- fmt.Errorf("代理 %s 切换失败: %v", url, err)
			}
		}(proxyURL)
	}

	// 等待所有请求完成
	wg.Wait()
	close(errorChan)

	// 收集错误
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	// 如果有错误，返回第一个错误
	if len(errors) > 0 {
		if ps.taskLogger != nil {
			for _, err := range errors {
				ps.taskLogger.WriteStep("trafficSwitching", "ERROR", err.Error())
			}
		}
		return fmt.Errorf("有 %d 个代理地址切换失败", len(errors))
	}

	return nil
}

// callProxySwitch 调用流量代理切换接口
func (ps *ProxySwitcher) callProxySwitch(ctx context.Context, proxyURL string, targetVersion string) error {
	// 构建请求URL
	switchURL := fmt.Sprintf("%s/switch", proxyURL)

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

	if ps.taskLogger != nil {
		ps.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("代理 %s 流量切换成功", proxyURL))
	}

	return nil
}
