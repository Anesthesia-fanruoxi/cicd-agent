package checkService

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"cicd-agent/common"
)

// ServiceChecker 服务检查器
type ServiceChecker struct {
	taskID string
}

// NewServiceChecker 创建服务检查器
func NewServiceChecker(taskID string) *ServiceChecker {
	return &ServiceChecker{
		taskID: taskID,
	}
}

// CheckServicesReady 检查服务就绪状态
func (c *ServiceChecker) CheckServicesReady(ctx context.Context, services []string, namespace string) error {
	common.AppLogger.Info(fmt.Sprintf("开始检查命名空间 %s 下所有pod的就绪状态", namespace))

	// 先等待15秒让pod生成
	common.AppLogger.Info("等待15秒让pod生成...")
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(15 * time.Second):
	}

	// 循环检查pod状态，直到所有pod就绪或超时
	return c.checkPodsWithRetry(ctx, namespace)
}

// getAllPods 获取命名空间下所有pod名称
func (c *ServiceChecker) getAllPods(ctx context.Context, namespace string) ([]string, error) {
	// 直接获取命名空间下的所有pod
	cmd := exec.CommandContext(ctx, "kubectl", "get", "pod", "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("获取命名空间 %s 下的pod列表失败: %v, 输出: %s", namespace, err, string(output))
	}

	// 解析pod列表
	allPods := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(allPods) == 0 || (len(allPods) == 1 && allPods[0] == "") {
		return nil, fmt.Errorf("命名空间 %s 下没有找到任何pod", namespace)
	}

	// 过滤掉空字符串
	var validPods []string
	for _, pod := range allPods {
		if strings.TrimSpace(pod) != "" {
			validPods = append(validPods, strings.TrimSpace(pod))
		}
	}

	// common.AppLogger.Info(fmt.Sprintf("命名空间 %s 下找到 %d 个pod", namespace, len(validPods)))
	// for _, pod := range validPods {
	// 	common.AppLogger.Info(fmt.Sprintf("发现pod: %s", pod))
	// }

	return validPods, nil
}

// PodStatus pod状态跟踪
type PodStatus struct {
	Name    string
	Ready   bool
	Checked bool
}

// checkPodsWithRetry 两阶段检查：先等待pod Running，再检查服务健康
func (c *ServiceChecker) checkPodsWithRetry(ctx context.Context, namespace string) error {
	// 第一阶段：等待所有pod状态变为Running
	common.AppLogger.Info("开始第一阶段：等待所有pod状态变为Running")
	if err := c.waitForAllPodsRunning(ctx, namespace); err != nil {
		return fmt.Errorf("第一阶段失败: %v", err)
	}

	// 第二阶段：检查服务健康状态
	common.AppLogger.Info("开始第二阶段：检查服务健康状态")
	if err := c.checkPodsHealthiness(ctx, namespace); err != nil {
		return fmt.Errorf("第二阶段失败: %v", err)
	}

	common.AppLogger.Info("所有pod已就绪，服务检查完成")
	return nil
}

// waitForAllPodsRunning 第一阶段：等待所有pod状态变为Running（初筛，连续2次成功）
func (c *ServiceChecker) waitForAllPodsRunning(ctx context.Context, namespace string) error {
	maxWaitDuration := 5 * time.Minute // 最大等待5分钟
	checkInterval := 10 * time.Second  // 每10秒检查一次

	deadline := time.Now().Add(maxWaitDuration)
	common.AppLogger.Info(fmt.Sprintf("第一阶段初筛：等待所有pod变为Running状态，最大等待时间%d分钟，检查间隔%d秒", int(maxWaitDuration.Minutes()), int(checkInterval.Seconds())))

	consecutiveSuccess := 0 // 连续成功次数
	requiredSuccess := 2    // 需要连续成功2次

	for {
		// 检查是否超时或取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			// 获取当前状态用于错误信息
			podStates, err := c.getAllPodsWithStatus(ctx, namespace)
			if err != nil {
				return fmt.Errorf("等待超时且无法获取pod状态: %v", err)
			}

			var nonRunningPods []string
			for podName, status := range podStates {
				if status != "Running" {
					nonRunningPods = append(nonRunningPods, fmt.Sprintf("%s(%s)", podName, status))
				}
			}
			return fmt.Errorf("等待超时，仍有%d个pod未Running: %s", len(nonRunningPods), strings.Join(nonRunningPods, ", "))
		}

		// 获取所有pod及其状态
		podStates, err := c.getAllPodsWithStatus(ctx, namespace)
		if err != nil {
			return fmt.Errorf("获取pod状态失败: %v", err)
		}

		// 统计各状态数量
		statusCount := make(map[string]int)
		totalPods := len(podStates)
		runningPods := 0

		for _, status := range podStates {
			statusCount[status]++
			if status == "Running" {
				runningPods++
			}
		}

		// 输出状态统计
		var statusParts []string
		for status, count := range statusCount {
			statusParts = append(statusParts, fmt.Sprintf("%s=%d", status, count))
		}
		common.AppLogger.Info(fmt.Sprintf("Pod状态统计 - 总数=%d, %s", totalPods, strings.Join(statusParts, ", ")))

		// 检查是否所有pod都是Running
		if runningPods == totalPods && totalPods > 0 {
			consecutiveSuccess++
			common.AppLogger.Info(fmt.Sprintf("所有pod都是Running状态 - 连续成功次数: %d/%d", consecutiveSuccess, requiredSuccess))

			// 连续成功达到要求次数，通过初筛
			if consecutiveSuccess >= requiredSuccess {
				common.AppLogger.Info("初筛完成：所有pod已连续2次检查都是Running状态")
				return nil
			}
		} else {
			// 重置连续成功计数
			if consecutiveSuccess > 0 {
				common.AppLogger.Info("pod状态不全为Running，重置连续成功计数")
				consecutiveSuccess = 0
			}
		}

		// 等待下一次检查
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(checkInterval):
		}
	}
}

// getAllPodsWithStatus 获取所有pod及其状态
func (c *ServiceChecker) getAllPodsWithStatus(ctx context.Context, namespace string) (map[string]string, error) {
	cmdArgs := []string{"get", "pods", "-n", namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\t\"}{.status.phase}{\"\\n\"}{end}"}
	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("获取pod状态失败: %v, 输出: %s", err, string(output))
	}

	podStates := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 2 {
			podName := strings.TrimSpace(parts[0])
			status := strings.TrimSpace(parts[1])
			podStates[podName] = status
		}
	}

	return podStates, nil
}

// checkPodsHealthiness 第二阶段：检查服务健康状态（每次重新获取pod列表）
func (c *ServiceChecker) checkPodsHealthiness(ctx context.Context, namespace string) error {
	maxDuration := 3 * time.Minute   // 最大检查时间3分钟
	checkInterval := 3 * time.Second // 每3秒检查一轮

	deadline := time.Now().Add(maxDuration)
	common.AppLogger.Info(fmt.Sprintf("第二阶段健康检查：每轮重新获取pod列表，最大检查时间%d分钟，检查间隔%d秒", int(maxDuration.Minutes()), int(checkInterval.Seconds())))

	// 记录已完成健康检查的pod（跨轮次保持）
	completedPods := make(map[string]bool)
	roundCount := 0

	for {
		roundCount++

		// 检查是否超时或取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			// 获取当前pod列表用于错误信息
			currentPods, err := c.getAllPods(ctx, namespace)
			if err != nil {
				return fmt.Errorf("健康检查超时且无法获取pod列表: %v", err)
			}

			var failedPods []string
			for _, podName := range currentPods {
				if !completedPods[podName] {
					failedPods = append(failedPods, podName)
				}
			}
			return fmt.Errorf("健康检查超时，仍有%d个pod未就绪: %s", len(failedPods), strings.Join(failedPods, ", "))
		}

		// 每轮重新获取当前的pod列表
		currentPods, err := c.getAllPods(ctx, namespace)
		if err != nil {
			return fmt.Errorf("获取pod列表失败: %v", err)
		}

		if len(currentPods) == 0 {
			return fmt.Errorf("命名空间 %s 下没有找到任何pod", namespace)
		}

		// 统计当前轮次的状态
		totalPods := len(currentPods)
		readyPods := 0
		pendingPods := 0

		// 分类pod：已完成的和待检查的
		var pendingPodsList []string
		for _, podName := range currentPods {
			if completedPods[podName] {
				readyPods++
			} else {
				pendingPods++
				pendingPodsList = append(pendingPodsList, podName)
			}
		}

		common.AppLogger.Info(fmt.Sprintf("第%d轮检查开始 - 总数=%d, 已完成=%d, 待检查=%d", roundCount, totalPods, readyPods, pendingPods))

		// 如果所有pod都已完成健康检查，结束
		if pendingPods == 0 {
			common.AppLogger.Info(fmt.Sprintf("第%d轮检查完成 - 所有pod健康检查通过", roundCount))
			return nil
		}

		// 并发检查待检查的pod
		newlyCompleted := c.checkPodListHealth(ctx, namespace, pendingPodsList)

		// 更新已完成的pod记录
		for _, podName := range newlyCompleted {
			completedPods[podName] = true
		}

		// 重新统计状态（检查后）
		readyPodsAfter := 0
		pendingPodsAfter := 0
		for _, podName := range currentPods {
			if completedPods[podName] {
				readyPodsAfter++
			} else {
				pendingPodsAfter++
			}
		}

		common.AppLogger.Info(fmt.Sprintf("第%d轮检查结果 - 总数=%d, 已完成=%d, 待检查=%d, 本轮新完成=%d",
			roundCount, totalPods, readyPodsAfter, pendingPodsAfter, len(newlyCompleted)))

		// 如果检查后所有pod都完成，结束
		if pendingPodsAfter == 0 {
			return nil
		}

		// 等待下一轮检查
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(checkInterval):
		}
	}
}

// checkPodListHealth 并发检查pod列表的健康状态，返回通过检查的pod名称列表
func (c *ServiceChecker) checkPodListHealth(ctx context.Context, namespace string, podList []string) []string {
	if len(podList) == 0 {
		return []string{}
	}

	// 计算并发数
	concurrency := c.calculateConcurrency(len(podList))
	semaphore := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var completedPods []string

	// 并发检查每个pod
	for _, podName := range podList {
		wg.Add(1)
		go func(pName string) {
			defer wg.Done()

			// 获取信号量
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// 执行健康检查
			if err := c.checkSinglePodHealth(ctx, namespace, pName); err == nil {
				mu.Lock()
				completedPods = append(completedPods, pName)
				mu.Unlock()
				common.AppLogger.Info(fmt.Sprintf("pod %s 健康检查通过", pName))
			} else {
				// 不记录错误日志，避免日志过多
			}
		}(podName)
	}

	wg.Wait()
	return completedPods
}

// updatePodStatusMap 更新pod状态映射
func (c *ServiceChecker) updatePodStatusMap(podStatusMap map[string]*PodStatus, allPods []string) {
	// 创建当前pod集合
	currentPods := make(map[string]bool)
	for _, podName := range allPods {
		currentPods[podName] = true
	}

	// 移除已删除的pod
	for podName := range podStatusMap {
		if !currentPods[podName] {
			common.AppLogger.Info(fmt.Sprintf("检测到pod已删除，从检查列表中移除: %s", podName))
			delete(podStatusMap, podName)
		}
	}

	// 添加新发现的pod
	for _, podName := range allPods {
		if _, exists := podStatusMap[podName]; !exists {
			common.AppLogger.Info(fmt.Sprintf("检测到新pod，添加到检查列表: %s", podName))
			podStatusMap[podName] = &PodStatus{
				Name:    podName,
				Ready:   false,
				Checked: false,
			}
		}
	}
}

// checkPendingPods 并发检查未就绪的pod
func (c *ServiceChecker) checkPendingPods(ctx context.Context, namespace string, podStatusMap map[string]*PodStatus) {
	var wg sync.WaitGroup

	// 收集需要检查的pod
	var pendingPods []*PodStatus
	for _, status := range podStatusMap {
		if !status.Ready {
			pendingPods = append(pendingPods, status)
		}
	}

	// 并发检查每个pod
	for _, podStatus := range pendingPods {
		wg.Add(1)
		go func(ps *PodStatus) {
			defer wg.Done()

			// 检查pod是否运行
			if !c.isPodRunning(ctx, namespace, ps.Name) {
				return
			}

			// 执行健康检查
			if err := c.checkSinglePodHealth(ctx, namespace, ps.Name); err == nil {
				ps.Ready = true
			}
			ps.Checked = true
		}(podStatus)
	}

	wg.Wait()
}

// calculateConcurrency 根据pod数量计算合理的并发数
func (c *ServiceChecker) calculateConcurrency(podCount int) int {
	if podCount <= 20 {
		return podCount // 20个以下：全并发
	} else if podCount <= 100 {
		return 20 // 100个以下：20并发
	} else {
		return 30 // 大规模：30并发
	}
}

// checkPendingPodsWithConcurrency 带并发控制的pod检查
func (c *ServiceChecker) checkPendingPodsWithConcurrency(ctx context.Context, namespace string, podStatusMap map[string]*PodStatus) {
	// 收集需要检查的pod
	var pendingPods []*PodStatus
	for _, status := range podStatusMap {
		if !status.Ready {
			pendingPods = append(pendingPods, status)
		}
	}

	if len(pendingPods) == 0 {
		return
	}

	// 计算并发数
	concurrency := c.calculateConcurrency(len(pendingPods))
	semaphore := make(chan struct{}, concurrency)

	var wg sync.WaitGroup

	// 并发检查每个pod
	for _, podStatus := range pendingPods {
		wg.Add(1)
		go func(ps *PodStatus) {
			defer wg.Done()

			// 获取信号量
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// 执行健康检查
			if err := c.checkSinglePodHealth(ctx, namespace, ps.Name); err == nil {
				ps.Ready = true
			}
			ps.Checked = true
		}(podStatus)
	}

	wg.Wait()
}

// isPodRunning 检查pod是否处于Running状态
func (c *ServiceChecker) isPodRunning(ctx context.Context, namespace, podName string) bool {
	cmdArgs := []string{"get", "pod", "-n", namespace, podName, "-o", "jsonpath={.status.phase}"}
	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "Running"
}

// checkSinglePodHealth 检查单个pod的健康状态
func (c *ServiceChecker) checkSinglePodHealth(ctx context.Context, namespace, podName string) error {
	// 创建2秒超时的上下文
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	cmdArgs := []string{"exec", "-n", namespace, podName, "-c", "filebeat", "--", "curl", "-s", "127.0.0.1:8080/actuator/health"}
	cmd := exec.CommandContext(cmdCtx, "kubectl", cmdArgs...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("健康检查命令执行失败: %v", err)
	}

	// 检查返回内容是否包含UP状态
	if strings.Contains(string(output), "UP") || strings.Contains(string(output), "\"status\":\"UP\"") {
		return nil
	}

	return fmt.Errorf("健康检查返回异常状态: %s", string(output))
}

// checkPodReady 检查单个pod的就绪状态
func (c *ServiceChecker) checkPodReady(ctx context.Context, namespace, podName string) error {
	common.AppLogger.Info(fmt.Sprintf("检查pod %s 就绪状态", podName))

	// 检查pod是否处于Running状态
	if err := c.checkPodStatus(ctx, namespace, podName); err != nil {
		return fmt.Errorf("pod状态检查失败: %v", err)
	}

	// 使用统一的健康检查方法
	if err := c.checkSinglePodHealth(ctx, namespace, podName); err != nil {
		return fmt.Errorf("健康检查失败: %v", err)
	}

	common.AppLogger.Info(fmt.Sprintf("pod %s 健康检查通过", podName))
	return nil
}

// checkSingleServiceReady 检查单个服务的就绪状态（保留兼容性）
func (c *ServiceChecker) checkSingleServiceReady(ctx context.Context, namespace, serviceName string) error {
	common.AppLogger.Info(fmt.Sprintf("检查服务 %s 就绪状态", serviceName))

	// 首先获取pod名称
	podName, err := c.getPodName(ctx, namespace, serviceName)
	if err != nil {
		return fmt.Errorf("获取pod名称失败: %v", err)
	}

	// 检查pod是否处于Running状态
	if err := c.checkPodStatus(ctx, namespace, podName); err != nil {
		return fmt.Errorf("pod状态检查失败: %v", err)
	}

	// 执行健康检查
	if err := c.checkSinglePodHealth(ctx, namespace, podName); err != nil {
		return fmt.Errorf("健康检查失败: %v", err)
	}

	common.AppLogger.Info(fmt.Sprintf("服务 %s 健康检查通过", serviceName))
	return nil
}

// checkPodStatus 检查pod状态
func (c *ServiceChecker) checkPodStatus(ctx context.Context, namespace, podName string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "get", "pod", "-n", namespace, podName, "-o", "jsonpath={.status.phase}")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("获取pod状态失败: %v, 输出: %s", err, string(output))
	}

	status := strings.TrimSpace(string(output))
	if status != "Running" {
		return fmt.Errorf("pod状态不是Running，当前状态: %s", status)
	}

	common.AppLogger.Info(fmt.Sprintf("pod %s 状态正常: %s", podName, status))
	return nil
}

// getPodName 获取服务对应的pod名称，支持重试
func (c *ServiceChecker) getPodName(ctx context.Context, namespace, serviceName string) (string, error) {
	maxRetries := 30                  // 最多重试30次
	retryInterval := 10 * time.Second // 每次重试间隔10秒

	for i := 0; i < maxRetries; i++ {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		// 尝试多种标签选择器
		selectors := []string{
			fmt.Sprintf("app=%s", serviceName),
			fmt.Sprintf("app.kubernetes.io/name=%s", serviceName),
			fmt.Sprintf("k8s-app=%s", serviceName),
		}

		for _, selector := range selectors {
			cmd := exec.CommandContext(ctx, "kubectl", "get", "pods", "-n", namespace, "-l", selector, "-o", "jsonpath={.items[0].metadata.name}")

			output, err := cmd.CombinedOutput()
			if err == nil {
				podName := strings.TrimSpace(string(output))
				if podName != "" {
					common.AppLogger.Info(fmt.Sprintf("找到服务 %s 对应的pod: %s (使用选择器: %s)", serviceName, podName, selector))
					return podName, nil
				}
			}
		}

		if i < maxRetries-1 {
			common.AppLogger.Info(fmt.Sprintf("第%d次尝试未找到服务 %s 的pod，%d秒后重试", i+1, serviceName, int(retryInterval.Seconds())))
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(retryInterval):
			}
		}
	}

	return "", fmt.Errorf("重试%d次后仍未找到服务 %s 对应的pod", maxRetries, serviceName)
}

// CheckServices 检查服务列表（包装函数）
func CheckServices(ctx context.Context, services []string, namespace string) error {
	// 使用空的taskID，因为这是包装函数
	checker := NewServiceChecker("")
	return checker.CheckServicesReady(ctx, services, namespace)
}
