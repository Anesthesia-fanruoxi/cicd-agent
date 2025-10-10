package trafficSwitching

import (
	"cicd-agent/common"
	"cicd-agent/config"
	"cicd-agent/taskStep"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// TrafficSwitcher 流量切换处理器
type TrafficSwitcher struct {
	namespace    string
	serviceName  string
	version      string
	nginxConfDir string // nginx配置目录，默认 /etc/nginx/conf.d
	taskLogger   *common.TaskLogger
}

// NewTrafficSwitcher 创建流量切换处理器
func NewTrafficSwitcher(namespace, serviceName, version, nginxConfDir string, taskLogger *common.TaskLogger) *TrafficSwitcher {
	if nginxConfDir == "" {
		nginxConfDir = "/etc/nginx/conf.d"
	}
	return &TrafficSwitcher{
		namespace:    namespace,
		serviceName:  serviceName,
		version:      version,
		nginxConfDir: nginxConfDir,
		taskLogger:   taskLogger,
	}
}

// Execute 执行流量切换
func (ts *TrafficSwitcher) Execute(ctx context.Context, step taskStep.Step) error {
	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("开始执行流量切换，目标版本: %s", ts.version))
	}

	// 判断是否启用流量代理
	if config.AppConfig.GetTrafficProxyEnable() {
		// 使用流量代理方式切换
		if ts.taskLogger != nil {
			ts.taskLogger.WriteStep("trafficSwitching", "INFO", "检测到已启用流量代理，使用代理方式切换流量")
		}
		return ts.executeProxySwitch(ctx)
	}

	// 使用 Nginx Upstream 方式切换
	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", "使用Nginx Upstream方式切换流量")
	}
	return ts.executeNginxSwitch(ctx)
}

// executeProxySwitch 通过流量代理切换
func (ts *TrafficSwitcher) executeProxySwitch(ctx context.Context) error {
	// 创建流量代理切换器
	proxySwitcher := NewProxySwitcher(ts.version, ts.taskLogger)

	// 执行切换
	if err := proxySwitcher.Execute(ctx); err != nil {
		return err
	}

	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", "流量切换完成")
	}
	return nil
}

// executeNginxSwitch 通过修改 Nginx Upstream 切换
func (ts *TrafficSwitcher) executeNginxSwitch(ctx context.Context) error {
	// 1. 获取当前版本的Gateway LoadBalancer地址
	gatewayIP, err := ts.getGatewayLoadBalancerIP(ctx)
	if err != nil {
		return fmt.Errorf("获取Gateway LoadBalancer地址失败: %v", err)
	}

	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("获取到Gateway地址: %s:8080", gatewayIP))
	}

	// 2. 修改所有Nginx配置文件
	if err := ts.updateAllNginxConfigs(gatewayIP); err != nil {
		return fmt.Errorf("更新Nginx配置失败: %v", err)
	}

	// 3. 验证配置是否正确应用
	if err := ts.verifyNginxConfig(gatewayIP); err != nil {
		return fmt.Errorf("验证Nginx配置失败: %v", err)
	}

	// 4. 远程执行nginx重启
	if err := ts.reloadNginxRemotely(ctx); err != nil {
		return fmt.Errorf("远程重启Nginx失败: %v", err)
	}

	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", "流量切换完成")
	}
	return nil
}

// getGatewayLoadBalancerIP 获取Gateway的LoadBalancer IP地址
func (ts *TrafficSwitcher) getGatewayLoadBalancerIP(ctx context.Context) (string, error) {
	// 使用传入的namespace，而不是重新构建
	serviceNamespace := ts.namespace
	gatewayServiceName := fmt.Sprintf("%s-gateway", ts.serviceName)

	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("查找服务: %s/%s", serviceNamespace, gatewayServiceName))
	}

	// 执行kubectl命令获取LoadBalancer的EXTERNAL-IP
	cmdArgs := []string{
		"get", "svc", gatewayServiceName,
		"-n", serviceNamespace,
		"-o", "jsonpath={.status.loadBalancer.ingress[0].ip}",
	}

	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	output, err := cmd.CombinedOutput()

	// 写入命令执行日志
	if ts.taskLogger != nil {
		ts.taskLogger.WriteCommand("trafficSwitching", cmd.String(), output, err)
	}

	if err != nil {
		return "", fmt.Errorf("执行kubectl命令失败: %v", err)
	}

	ip := strings.TrimSpace(string(output))
	if ip == "" {
		return "", fmt.Errorf("未找到LoadBalancer的EXTERNAL-IP")
	}

	return ip, nil
}

// updateAllNginxConfigs 更新/etc/nginx/conf.d目录下所有配置文件
func (ts *TrafficSwitcher) updateAllNginxConfigs(gatewayIP string) error {
	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("开始更新目录下所有Nginx配置文件: %s", ts.nginxConfDir))
	}

	// 获取目录下所有.conf文件
	confFiles, err := ts.getAllConfFiles()
	if err != nil {
		return fmt.Errorf("获取配置文件列表失败: %v", err)
	}

	if len(confFiles) == 0 {
		return fmt.Errorf("目录 %s 下未找到.conf配置文件", ts.nginxConfDir)
	}

	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("找到%d个配置文件，开始批量更新", len(confFiles)))
	}

	// 逐个处理配置文件
	updatedCount := 0
	for _, confFile := range confFiles {
		if err := ts.updateSingleConfigFile(confFile, gatewayIP); err != nil {
			if ts.taskLogger != nil {
				ts.taskLogger.WriteStep("trafficSwitching", "WARNING", fmt.Sprintf("更新配置文件 %s 失败: %v", confFile, err))
			}
			continue
		}
		updatedCount++
	}

	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("成功更新%d个配置文件，后端地址: %s:8080", updatedCount, gatewayIP))
	}
	return nil
}

// getAllConfFiles 获取nginx配置目录下所有.conf文件
func (ts *TrafficSwitcher) getAllConfFiles() ([]string, error) {
	var confFiles []string

	err := filepath.Walk(ts.nginxConfDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 只处理.conf文件
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".conf") {
			confFiles = append(confFiles, path)
		}

		return nil
	})

	return confFiles, err
}

// updateSingleConfigFile 更新单个配置文件
func (ts *TrafficSwitcher) updateSingleConfigFile(filePath, gatewayIP string) error {
	// 读取配置文件
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("读取文件失败: %v", err)
	}

	originalContent := string(content)

	// 替换IP地址和端口
	newContent, changed := ts.replaceIPAndPort(originalContent, gatewayIP)
	if !changed {
		if ts.taskLogger != nil {
			ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("配置文件 %s 无需更新", filepath.Base(filePath)))
		}
		return nil
	}

	// 写入更新后的内容
	err = os.WriteFile(filePath, []byte(newContent), 0644)
	if err != nil {
		return fmt.Errorf("写入文件失败: %v", err)
	}

	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("已更新配置文件: %s", filepath.Base(filePath)))
	}
	return nil
}

// replaceIPAndPort 替换配置中的IP地址和端口
func (ts *TrafficSwitcher) replaceIPAndPort(content, newIP string) (string, bool) {
	// 匹配多种nginx配置格式中的IP:端口
	patterns := []string{
		`server\s+\d+\.\d+\.\d+\.\d+:8080;`,            // upstream中的server
		`proxy_pass\s+http://\d+\.\d+\.\d+\.\d+:8080;`, // location中的proxy_pass
		`proxy_pass\s+http://\d+\.\d+\.\d+\.\d+:8080/`, // 带路径的proxy_pass
		`\d+\.\d+\.\d+\.\d+:8080`,                      // 通用IP:端口格式
	}

	newTarget := fmt.Sprintf("%s:8080", newIP)
	newContent := content
	changed := false

	for _, pattern := range patterns {
		regex := regexp.MustCompile(pattern)
		if regex.MatchString(newContent) {
			// 根据不同模式进行替换
			if strings.Contains(pattern, "server") {
				newContent = regex.ReplaceAllString(newContent, fmt.Sprintf("server %s;", newTarget))
			} else if strings.Contains(pattern, "proxy_pass") && strings.Contains(pattern, "/") {
				newContent = regex.ReplaceAllString(newContent, fmt.Sprintf("proxy_pass http://%s/", newTarget))
			} else if strings.Contains(pattern, "proxy_pass") {
				newContent = regex.ReplaceAllString(newContent, fmt.Sprintf("proxy_pass http://%s;", newTarget))
			} else {
				newContent = regex.ReplaceAllString(newContent, newTarget)
			}
			changed = true
		}
	}

	return newContent, changed
}

// verifyNginxConfig 验证nginx配置是否正确应用
func (ts *TrafficSwitcher) verifyNginxConfig(expectedIP string) error {
	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", "开始验证nginx配置是否正确应用")
	}

	// 获取所有配置文件
	confFiles, err := ts.getAllConfFiles()
	if err != nil {
		return fmt.Errorf("获取配置文件列表失败: %v", err)
	}

	expectedTarget := fmt.Sprintf("%s:8080", expectedIP)
	var inconsistentFiles []string
	var totalChecked int

	// 检查每个配置文件
	for _, confFile := range confFiles {
		content, err := os.ReadFile(confFile)
		if err != nil {
			if ts.taskLogger != nil {
				ts.taskLogger.WriteStep("trafficSwitching", "WARNING", fmt.Sprintf("读取配置文件 %s 失败: %v", confFile, err))
			}
			continue
		}

		totalChecked++

		// 检查是否包含期望的IP地址
		if !ts.containsExpectedIP(string(content), expectedIP) {
			inconsistentFiles = append(inconsistentFiles, filepath.Base(confFile))
			if ts.taskLogger != nil {
				ts.taskLogger.WriteStep("trafficSwitching", "WARNING", fmt.Sprintf("配置文件 %s 检查失败：未找到期望的后端地址 %s", filepath.Base(confFile), expectedTarget))
			}
		} else {
			if ts.taskLogger != nil {
				ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("配置文件 %s 检查通过：后端地址正确为 %s", filepath.Base(confFile), expectedTarget))
			}
		}
	}

	if len(inconsistentFiles) > 0 {
		return fmt.Errorf("配置验证失败，以下%d个文件中的后端地址与期望的%s不一致: %s",
			len(inconsistentFiles), expectedTarget, strings.Join(inconsistentFiles, ", "))
	}

	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("配置验证成功，共检查%d个文件，后端地址均为: %s", totalChecked, expectedTarget))
	}
	return nil
}

// containsExpectedIP 检查配置内容是否包含期望的IP地址
func (ts *TrafficSwitcher) containsExpectedIP(content, expectedIP string) bool {
	expectedTarget := fmt.Sprintf("%s:8080", expectedIP)

	// 检查多种可能的配置格式
	patterns := []string{
		fmt.Sprintf("server %s;", expectedTarget),            // upstream中的server
		fmt.Sprintf("proxy_pass http://%s;", expectedTarget), // proxy_pass
		fmt.Sprintf("proxy_pass http://%s/", expectedTarget), // 带路径的proxy_pass
		expectedTarget, // 通用格式
	}

	for _, pattern := range patterns {
		if strings.Contains(content, pattern) {
			return true
		}
	}

	return false
}

// reloadNginxRemotely 通过SSH远程执行nginx重启命令（异步执行）
func (ts *TrafficSwitcher) reloadNginxRemotely(ctx context.Context) error {
	// SSH配置
	sshKeyPath := "/root/.ssh/id_rsa"
	sshUser := "root"

	// 支持多个nginx服务器
	nginxServers := []string{
		"192.168.7.2",
		// 可以添加更多服务器IP
		// "192.168.7.3",
		// "192.168.7.4",
	}

	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("启动异步SSH重启%d个Nginx服务器", len(nginxServers)))
	}

	// 异步执行所有服务器的nginx重启，不阻塞主线程
	go func() {
		// 使用channel收集结果
		type reloadResult struct {
			serverIP string
			success  bool
			error    string
		}

		resultChan := make(chan reloadResult, len(nginxServers))

		// 并发执行所有服务器的nginx重启
		for _, serverIP := range nginxServers {
			go func(ip string) {
				if ts.taskLogger != nil {
					ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("正在重启nginx服务器: %s@%s", sshUser, ip))
				}

				// 构建SSH命令，优化配置避免警告信息
				sshCmd := exec.CommandContext(ctx, "ssh",
					"-i", sshKeyPath,
					"-o", "StrictHostKeyChecking=no",
					"-o", "UserKnownHostsFile=/dev/null",
					"-o", "ConnectTimeout=10",
					"-o", "LogLevel=ERROR", // 减少SSH警告输出
					fmt.Sprintf("%s@%s", sshUser, ip),
					"nginx -s reload")

				// 执行SSH命令
				output, err := sshCmd.CombinedOutput()
				if err != nil {
					errorMsg := fmt.Sprintf("SSH执行失败: %v, 输出: %s", err, string(output))
					resultChan <- reloadResult{serverIP: ip, success: false, error: errorMsg}
				} else {
					if ts.taskLogger != nil {
						ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("服务器%s nginx重启成功", ip))
					}
					resultChan <- reloadResult{serverIP: ip, success: true, error: ""}
				}
			}(serverIP)
		}

		// 收集所有结果
		var errors []string
		successCount := 0

		for i := 0; i < len(nginxServers); i++ {
			result := <-resultChan
			if result.success {
				successCount++
			} else {
				errorMsg := fmt.Sprintf("服务器%s重启失败: %s", result.serverIP, result.error)
				errors = append(errors, errorMsg)
				if ts.taskLogger != nil {
					ts.taskLogger.WriteStep("trafficSwitching", "ERROR", errorMsg)
				}
			}
		}

		// 异步报告最终结果
		if len(errors) > 0 {
			if successCount == 0 {
				if ts.taskLogger != nil {
					ts.taskLogger.WriteStep("trafficSwitching", "ERROR", fmt.Sprintf("所有nginx服务器重启失败: %s", strings.Join(errors, "; ")))
				}
			} else {
				if ts.taskLogger != nil {
					ts.taskLogger.WriteStep("trafficSwitching", "WARNING", fmt.Sprintf("部分nginx服务器重启失败(%d/%d成功): %s",
						successCount, len(nginxServers), strings.Join(errors, "; ")))
				}
			}
		} else {
			if ts.taskLogger != nil {
				ts.taskLogger.WriteStep("trafficSwitching", "INFO", fmt.Sprintf("所有Nginx服务器重启成功(%d/%d)", successCount, len(nginxServers)))
			}
		}
	}()

	// 立即返回，不等待SSH执行完成
	if ts.taskLogger != nil {
		ts.taskLogger.WriteStep("trafficSwitching", "INFO", "Nginx重启任务已启动，正在后台执行...")
	}
	return nil
}
