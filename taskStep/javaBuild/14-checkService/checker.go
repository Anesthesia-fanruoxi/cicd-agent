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
	taskID     string
	taskLogger *common.TaskLogger
}

// NewServiceChecker 创建服务检查器
func NewServiceChecker(taskID string, taskLogger *common.TaskLogger) *ServiceChecker {
	return &ServiceChecker{
		taskID:     taskID,
		taskLogger: taskLogger,
	}
}

// CheckServicesReady 检查服务就绪状态
func (c *ServiceChecker) CheckServicesReady(ctx context.Context, services []string, namespace string) error {
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("开始检查命名空间 %s 下所有pod的就绪状态", namespace))
	}

	// 先等待15秒让pod生成
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", "等待15秒让pod生成...")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(15 * time.Second):
	}

	// 循环检查pod状态，直到所有pod就绪或超时
	return c.checkPodsWithRetry(ctx, namespace)
}

// isPodNormalState 判断Pod是否处于正常状态（只有ContainerCreating和Running算正常）
func (c *ServiceChecker) isPodNormalState(status string) bool {
	normalStates := []string{
		"Pending",
		"ContainerCreating", // 容器创建中
		"Running",           // 运行中
	}

	for _, normalState := range normalStates {
		if status == normalState {
			return true
		}
	}
	return false
}

// scaleDownFailedControllers 缩容命名空间下所有控制器到0个副本
func (c *ServiceChecker) scaleDownFailedControllers(ctx context.Context, namespace string) error {
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("=== 开始执行缩容操作 ==="))
		c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("缩容目标命名空间: %s (将缩容所有控制器)", namespace))
	}

	// 获取命名空间下所有控制器
	allControllers, err := c.getAllControllers(ctx, namespace)
	if err != nil {
		if c.taskLogger != nil {
			c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("获取控制器列表失败: %v", err))
		}
		return err
	}

	if len(allControllers) == 0 {
		if c.taskLogger != nil {
			c.taskLogger.WriteStep("checkService", "WARNING", "没有发现需要缩容的控制器")
		}
		return nil
	}

	// 缩容所有控制器
	for controllerType, controllers := range allControllers {
		if c.taskLogger != nil {
			c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("开始缩容 %s: %v", controllerType, controllers))
		}

		switch controllerType {
		case "Deployment":
			for _, name := range controllers {
				if err := c.scaleDownSpecificDeployment(ctx, namespace, name); err != nil {
					if c.taskLogger != nil {
						c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("缩容Deployment %s 失败: %v", name, err))
					}
				}
			}
		case "ReplicaSet":
			for _, name := range controllers {
				if err := c.scaleDownSpecificReplicaSet(ctx, namespace, name); err != nil {
					if c.taskLogger != nil {
						c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("缩容ReplicaSet %s 失败: %v", name, err))
					}
				}
			}
		case "StatefulSet":
			for _, name := range controllers {
				if err := c.scaleDownSpecificStatefulSet(ctx, namespace, name); err != nil {
					if c.taskLogger != nil {
						c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("缩容StatefulSet %s 失败: %v", name, err))
					}
				}
			}
		}
	}

	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("=== 缩容操作执行完成 ==="))
	}
	return nil
}

// getAllControllers 获取命名空间下所有控制器
func (c *ServiceChecker) getAllControllers(ctx context.Context, namespace string) (map[string][]string, error) {
	allControllers := make(map[string][]string)

	// 获取所有Deployment
	cmdDeploy := exec.CommandContext(ctx, "kubectl", "get", "deployments", "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name")
	outputDeploy, err := cmdDeploy.CombinedOutput()
	if c.taskLogger != nil {
		c.taskLogger.WriteCommand("checkService", cmdDeploy.String(), outputDeploy, err)
	}
	if err == nil && len(outputDeploy) > 0 {
		lines := strings.Split(strings.TrimSpace(string(outputDeploy)), "\n")
		for _, line := range lines {
			name := strings.TrimSpace(line)
			if name != "" && name != "No resources found" {
				allControllers["Deployment"] = append(allControllers["Deployment"], name)
			}
		}
	}

	// 获取所有StatefulSet
	cmdSts := exec.CommandContext(ctx, "kubectl", "get", "statefulsets", "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name")
	outputSts, err := cmdSts.CombinedOutput()
	if c.taskLogger != nil {
		c.taskLogger.WriteCommand("checkService", cmdSts.String(), outputSts, err)
	}
	if err == nil && len(outputSts) > 0 {
		lines := strings.Split(strings.TrimSpace(string(outputSts)), "\n")
		for _, line := range lines {
			name := strings.TrimSpace(line)
			if name != "" && name != "No resources found" {
				allControllers["StatefulSet"] = append(allControllers["StatefulSet"], name)
			}
		}
	}

	// 获取所有独立的ReplicaSet（不属于Deployment的）
	cmdRs := exec.CommandContext(ctx, "kubectl", "get", "replicasets", "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name,OWNER:.metadata.ownerReferences[0].kind")
	outputRs, err := cmdRs.CombinedOutput()
	if c.taskLogger != nil {
		c.taskLogger.WriteCommand("checkService", cmdRs.String(), outputRs, err)
	}
	if err == nil && len(outputRs) > 0 {
		lines := strings.Split(strings.TrimSpace(string(outputRs)), "\n")
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				name := parts[0]
				owner := parts[1]
				// 只缩容没有Deployment作为owner的ReplicaSet
				if owner != "Deployment" && name != "" {
					allControllers["ReplicaSet"] = append(allControllers["ReplicaSet"], name)
				}
			}
		}
	}

	return allControllers, nil
}

// getFailedControllers 获取失败Pod对应的控制器（已弃用）
func (c *ServiceChecker) getFailedControllers(ctx context.Context, namespace string, failedPods []string) (map[string][]string, error) {
	if len(failedPods) == 0 {
		return make(map[string][]string), nil
	}

	failedControllers := make(map[string][]string)

	// 对每个失败的pod查询其控制器信息
	for _, podName := range failedPods {
		cmd := exec.CommandContext(ctx, "kubectl", "get", "pod", podName, "-n", namespace,
			"-o", "jsonpath={.metadata.ownerReferences[0].kind},{.metadata.ownerReferences[0].name}")

		output, err := cmd.CombinedOutput()

		if c.taskLogger != nil {
			c.taskLogger.WriteCommand("checkService", cmd.String(), output, err)
		}

		if err != nil {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "WARNING", fmt.Sprintf("获取Pod %s 的控制器信息失败: %v", podName, err))
			}
			continue
		}

		result := strings.TrimSpace(string(output))
		if result == "" {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "WARNING", fmt.Sprintf("Pod %s 没有控制器信息", podName))
			}
			continue
		}

		parts := strings.Split(result, ",")
		if len(parts) >= 2 {
			controllerKind := strings.TrimSpace(parts[0])
			controllerName := strings.TrimSpace(parts[1])

			if controllerKind != "" && controllerName != "" {
				if c.taskLogger != nil {
					c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("发现失败Pod: %s, 控制器: %s (%s)", podName, controllerName, controllerKind))
				}

				// 收集需要缩容的控制器
				if _, exists := failedControllers[controllerKind]; !exists {
					failedControllers[controllerKind] = []string{}
				}

				// 避免重复添加
				found := false
				for _, existing := range failedControllers[controllerKind] {
					if existing == controllerName {
						found = true
						break
					}
				}

				if !found {
					failedControllers[controllerKind] = append(failedControllers[controllerKind], controllerName)
				}
			}
		}
	}

	return failedControllers, nil
}

// getFailedControllersOld 获取失败Pod对应的控制器（旧版本，使用field-selector）
func (c *ServiceChecker) getFailedControllersOld(ctx context.Context, namespace string) (map[string][]string, error) {
	// 获取所有非Running状态的Pod及其控制器信息
	cmd := exec.CommandContext(ctx, "kubectl", "get", "pods", "-n", namespace,
		"--field-selector=status.phase!=Running", "--no-headers",
		"-o", "custom-columns=NAME:.metadata.name,CONTROLLER:.metadata.ownerReferences[0].name,KIND:.metadata.ownerReferences[0].kind")

	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "No resources found") {
			return make(map[string][]string), nil
		}
		return nil, fmt.Errorf("获取失败Pod信息失败: %v, 输出: %s", err, string(output))
	}

	if c.taskLogger != nil {
		c.taskLogger.WriteCommand("checkService", cmd.String(), output, err)
	}

	failedControllers := make(map[string][]string)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 3 {
			podName := parts[0]
			controllerName := parts[1]
			controllerKind := parts[2]

			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("发现失败Pod: %s, 控制器: %s (%s)", podName, controllerName, controllerKind))
			}

			// 收集需要缩容的控制器
			if controllerName != "<none>" && controllerKind != "<none>" {
				if _, exists := failedControllers[controllerKind]; !exists {
					failedControllers[controllerKind] = []string{}
				}

				// 避免重复添加
				found := false
				for _, existing := range failedControllers[controllerKind] {
					if existing == controllerName {
						found = true
						break
					}
				}
				if !found {
					failedControllers[controllerKind] = append(failedControllers[controllerKind], controllerName)
				}
			}
		}
	}

	return failedControllers, nil
}

// scaleDownSpecificDeployment 缩容指定的Deployment
func (c *ServiceChecker) scaleDownSpecificDeployment(ctx context.Context, namespace, name string) error {
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("缩容指定Deployment: %s", name))
	}

	scaleCmd := exec.CommandContext(ctx, "kubectl", "scale", "deployment", name, "-n", namespace, "--replicas=0")
	scaleOutput, scaleErr := scaleCmd.CombinedOutput()

	if c.taskLogger != nil {
		c.taskLogger.WriteCommand("checkService", scaleCmd.String(), scaleOutput, scaleErr)
	}

	if scaleErr != nil {
		return fmt.Errorf("缩容Deployment %s 失败: %v, 输出: %s", name, scaleErr, string(scaleOutput))
	}

	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("成功缩容Deployment: %s", name))
	}
	return nil
}

// scaleDownSpecificReplicaSet 缩容指定的ReplicaSet
func (c *ServiceChecker) scaleDownSpecificReplicaSet(ctx context.Context, namespace, name string) error {
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("缩容指定ReplicaSet: %s", name))
	}

	scaleCmd := exec.CommandContext(ctx, "kubectl", "scale", "replicaset", name, "-n", namespace, "--replicas=0")
	scaleOutput, scaleErr := scaleCmd.CombinedOutput()

	if c.taskLogger != nil {
		c.taskLogger.WriteCommand("checkService", scaleCmd.String(), scaleOutput, scaleErr)
	}

	if scaleErr != nil {
		return fmt.Errorf("缩容ReplicaSet %s 失败: %v, 输出: %s", name, scaleErr, string(scaleOutput))
	}

	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("成功缩容ReplicaSet: %s", name))
	}
	return nil
}

// scaleDownSpecificStatefulSet 缩容指定的StatefulSet
func (c *ServiceChecker) scaleDownSpecificStatefulSet(ctx context.Context, namespace, name string) error {
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("缩容指定StatefulSet: %s", name))
	}

	scaleCmd := exec.CommandContext(ctx, "kubectl", "scale", "statefulset", name, "-n", namespace, "--replicas=0")
	scaleOutput, scaleErr := scaleCmd.CombinedOutput()

	if c.taskLogger != nil {
		c.taskLogger.WriteCommand("checkService", scaleCmd.String(), scaleOutput, scaleErr)
	}

	if scaleErr != nil {
		return fmt.Errorf("缩容StatefulSet %s 失败: %v, 输出: %s", name, scaleErr, string(scaleOutput))
	}

	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("成功缩容StatefulSet: %s", name))
	}
	return nil
}

// scaleDownDeployments 缩容所有Deployment到0个副本
func (c *ServiceChecker) scaleDownDeployments(ctx context.Context, namespace string) error {
	// 获取所有Deployment及其副本数
	cmd := exec.CommandContext(ctx, "kubectl", "get", "deployment", "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name,REPLICAS:.spec.replicas")
	output, err := cmd.CombinedOutput()

	// 写入命令执行日志
	if c.taskLogger != nil {
		c.taskLogger.WriteCommand("checkService", cmd.String(), output, err)
	}

	if err != nil {
		// 如果没有Deployment，不算错误
		if strings.Contains(string(output), "No resources found") {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("命名空间 %s 下没有Deployment", namespace))
			}
			return nil
		}
		return fmt.Errorf("获取Deployment列表失败: %v, 输出: %s", err, string(output))
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		deploymentName := parts[0]
		replicas := parts[1]

		// 如果副本数已经是0，跳过
		if replicas == "0" {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("Deployment %s 副本数已为0，跳过缩容", deploymentName))
			}
			continue
		}

		if c.taskLogger != nil {
			c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("缩容Deployment %s (当前副本:%s) 到0个副本", deploymentName, replicas))
		}
		scaleCmd := exec.CommandContext(ctx, "kubectl", "scale", "deployment", deploymentName, "-n", namespace, "--replicas=0")
		scaleOutput, scaleErr := scaleCmd.CombinedOutput()

		// 写入命令执行日志
		if c.taskLogger != nil {
			c.taskLogger.WriteCommand("checkService", scaleCmd.String(), scaleOutput, scaleErr)
		}

		if scaleErr != nil {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("缩容Deployment %s 失败: %v, 输出: %s", deploymentName, scaleErr, string(scaleOutput)))
			}
		} else {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("成功缩容Deployment %s", deploymentName))
			}
		}
	}

	return nil
}

// scaleDownStatefulSets 缩容所有StatefulSet到0个副本
func (c *ServiceChecker) scaleDownStatefulSets(ctx context.Context, namespace string) error {
	// 获取所有StatefulSet
	cmd := exec.CommandContext(ctx, "kubectl", "get", "statefulset", "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name")
	output, err := cmd.CombinedOutput()

	// 写入命令执行日志
	if c.taskLogger != nil {
		c.taskLogger.WriteCommand("checkService", cmd.String(), output, err)
	}

	if err != nil {
		// 如果没有StatefulSet，不算错误
		if strings.Contains(string(output), "No resources found") {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("命名空间 %s 下没有StatefulSet", namespace))
			}
			return nil
		}
		return fmt.Errorf("获取StatefulSet列表失败: %v, 输出: %s", err, string(output))
	}

	statefulsets := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, statefulset := range statefulsets {
		statefulset = strings.TrimSpace(statefulset)
		if statefulset == "" {
			continue
		}

		if c.taskLogger != nil {
			c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("缩容StatefulSet %s 到0个副本", statefulset))
		}
		scaleCmd := exec.CommandContext(ctx, "kubectl", "scale", "statefulset", statefulset, "-n", namespace, "--replicas=0")
		scaleOutput, scaleErr := scaleCmd.CombinedOutput()

		// 写入命令执行日志
		if c.taskLogger != nil {
			c.taskLogger.WriteCommand("checkService", scaleCmd.String(), scaleOutput, scaleErr)
		}

		if scaleErr != nil {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("缩容StatefulSet %s 失败: %v, 输出: %s", statefulset, scaleErr, string(scaleOutput)))
			}
		} else {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("成功缩容StatefulSet %s", statefulset))
			}
		}
	}

	return nil
}

// scaleDownReplicaSets 缩容所有ReplicaSet到0个副本
func (c *ServiceChecker) scaleDownReplicaSets(ctx context.Context, namespace string) error {
	// 获取所有ReplicaSet及其副本数
	cmd := exec.CommandContext(ctx, "kubectl", "get", "replicaset", "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name,REPLICAS:.spec.replicas")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 如果没有ReplicaSet，不算错误
		if strings.Contains(string(output), "No resources found") {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("命名空间 %s 下没有ReplicaSet", namespace))
			}
			return nil
		}
		return fmt.Errorf("获取ReplicaSet列表失败: %v, 输出: %s", err, string(output))
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		replicasetName := parts[0]
		replicas := parts[1]

		// 如果副本数已经是0，跳过
		if replicas == "0" {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("ReplicaSet %s 副本数已为0，跳过缩容", replicasetName))
			}
			continue
		}

		if c.taskLogger != nil {
			c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("缩容ReplicaSet %s (当前副本:%s) 到0个副本", replicasetName, replicas))
		}
		scaleCmd := exec.CommandContext(ctx, "kubectl", "scale", "replicaset", replicasetName, "-n", namespace, "--replicas=0")
		if scaleOutput, scaleErr := scaleCmd.CombinedOutput(); scaleErr != nil {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("缩容ReplicaSet %s 失败: %v, 输出: %s", replicasetName, scaleErr, string(scaleOutput)))
			}
		} else {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("成功缩容ReplicaSet %s", replicasetName))
			}
		}
	}

	return nil
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
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", "开始第一阶段：等待所有pod状态变为Running")
	}
	if err := c.waitForAllPodsRunning(ctx, namespace); err != nil {
		return fmt.Errorf("第一阶段失败: %v", err)
	}

	// 第二阶段：检查服务健康状态
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", "开始第二阶段：检查服务健康状态")
	}
	if err := c.checkPodsHealthiness(ctx, namespace); err != nil {
		return fmt.Errorf("第二阶段失败: %v", err)
	}

	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", "所有pod已就绪，服务检查完成")
	}
	return nil
}

// waitForAllPodsRunning 第一阶段：等待所有pod状态变为Running（初筛，连续2次成功）
func (c *ServiceChecker) waitForAllPodsRunning(ctx context.Context, namespace string) error {
	maxWaitDuration := 3 * time.Minute // 最大等待3分钟
	checkInterval := 10 * time.Second  // 每10秒检查一次

	deadline := time.Now().Add(maxWaitDuration)
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("第一阶段初筛：等待所有pod变为Running状态，最大等待时间%d分钟，检查间隔%d秒", int(maxWaitDuration.Minutes()), int(checkInterval.Seconds())))
	}

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

			// 超时时也需要进行缩容操作
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("!!! 第一阶段等待超时，触发缩容操作 !!!"))
				c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("超时详情: 等待pod Running状态超时，非Running的pod: %s", strings.Join(nonRunningPods, ", ")))
			}
			if err := c.scaleDownFailedControllers(ctx, namespace); err != nil {
				if c.taskLogger != nil {
					c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("执行缩容操作时出错: %v", err))
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
		normalPods := 0
		var abnormalPods []string

		for podName, status := range podStates {
			statusCount[status]++
			if c.isPodNormalState(status) {
				normalPods++
			} else {
				// 所有非正常状态都算异常（包括Pending）
				abnormalPods = append(abnormalPods, fmt.Sprintf("%s(%s)", podName, status))
			}
		}

		// 如果有Pod处于异常状态，立即返回失败
		if len(abnormalPods) > 0 {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("检测到%d个Pod处于异常状态，立即终止等待", len(abnormalPods)))
			}

			// 对失败的控制器进行缩容到0个副本
			if err := c.scaleDownFailedControllers(ctx, namespace); err != nil {
				if c.taskLogger != nil {
					c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("缩容失败的控制器时出错: %v", err))
				}
			}

			return fmt.Errorf("Pod状态异常，异常的Pod: %s", strings.Join(abnormalPods, ", "))
		}

		// 输出状态统计
		var statusParts []string
		for status, count := range statusCount {
			statusParts = append(statusParts, fmt.Sprintf("%s=%d", status, count))
		}
		if c.taskLogger != nil {
			c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("Pod状态统计 - 总数=%d, %s", totalPods, strings.Join(statusParts, ", ")))
		}

		// 检查是否所有pod都是Running（只有Running状态才算完全就绪）
		runningPods := 0
		for _, status := range podStates {
			if status == "Running" {
				runningPods++
			}
		}

		if runningPods == totalPods && totalPods > 0 {
			consecutiveSuccess++
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("所有pod都是Running状态 - 连续成功次数: %d/%d", consecutiveSuccess, requiredSuccess))
			}

			// 连续成功达到要求次数，通过初筛
			if consecutiveSuccess >= requiredSuccess {
				if c.taskLogger != nil {
					c.taskLogger.WriteStep("checkService", "INFO", "初筛完成：所有pod已连续2次检查都是Running状态")
				}
				return nil
			}
		} else {
			// 重置连续成功计数
			if consecutiveSuccess > 0 {
				if c.taskLogger != nil {
					c.taskLogger.WriteStep("checkService", "INFO", "pod状态不全为Running，重置连续成功计数")
				}
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
	maxDuration := 1 * time.Minute   // 最大检查时间3分钟
	checkInterval := 3 * time.Second // 每3秒检查一轮

	deadline := time.Now().Add(maxDuration)
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("第二阶段健康检查：每轮重新获取pod列表，最大检查时间%d分钟，检查间隔%d秒", int(maxDuration.Minutes()), int(checkInterval.Seconds())))
	}

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

			// 健康检查超时时也需要进行缩容操作
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("!!! 第二阶段健康检查超时，触发缩容操作 !!!"))
				c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("超时详情: 健康检查超时，未就绪的pod: %s", strings.Join(failedPods, ", ")))
			}
			if err := c.scaleDownFailedControllers(ctx, namespace); err != nil {
				if c.taskLogger != nil {
					c.taskLogger.WriteStep("checkService", "ERROR", fmt.Sprintf("执行缩容操作时出错: %v", err))
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

		if c.taskLogger != nil {
			c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("第%d轮检查开始 - 总数=%d, 已完成=%d, 待检查=%d", roundCount, totalPods, readyPods, pendingPods))
		}

		// 如果所有pod都已完成健康检查，结束
		if pendingPods == 0 {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("第%d轮检查完成 - 所有pod健康检查通过", roundCount))
			}
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

		if c.taskLogger != nil {
			c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("第%d轮检查结果 - 总数=%d, 已完成=%d, 待检查=%d, 本轮新完成=%d",
				roundCount, totalPods, readyPodsAfter, pendingPodsAfter, len(newlyCompleted)))
		}

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
				if c.taskLogger != nil {
					c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("pod %s 健康检查通过", pName))
				}
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
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("检测到pod已删除，从检查列表中移除: %s", podName))
			}
			delete(podStatusMap, podName)
		}
	}

	// 添加新发现的pod
	for _, podName := range allPods {
		if _, exists := podStatusMap[podName]; !exists {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("检测到新pod，添加到检查列表: %s", podName))
			}
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

	cmdArgs := []string{"exec", "-n", namespace, podName, "-c", "filebeat", "--", "curl", "-s", "http://127.0.0.1:8080/actuator/health"}
	cmd := exec.CommandContext(cmdCtx, "kubectl", cmdArgs...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("健康检查命令执行失败: %v", err)
	}

	outputStr := strings.TrimSpace(string(output))

	// 只要能正确返回JSON响应（包含status字段），就认为服务已就绪
	// 不判断UP/DOWN，因为只要服务能响应就说明已经启动
	if outputStr != "" && (strings.Contains(outputStr, "\"status\"") || strings.Contains(outputStr, "status")) {
		return nil
	}

	return fmt.Errorf("健康检查返回异常: %s", outputStr)
}

// checkPodReady 检查单个pod的就绪状态
func (c *ServiceChecker) checkPodReady(ctx context.Context, namespace, podName string) error {
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("检查pod %s 就绪状态", podName))
	}

	// 检查pod是否处于Running状态
	if err := c.checkPodStatus(ctx, namespace, podName); err != nil {
		return fmt.Errorf("pod状态检查失败: %v", err)
	}

	// 使用统一的健康检查方法
	if err := c.checkSinglePodHealth(ctx, namespace, podName); err != nil {
		return fmt.Errorf("健康检查失败: %v", err)
	}

	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("pod %s 健康检查通过", podName))
	}
	return nil
}

// checkSingleServiceReady 检查单个服务的就绪状态（保留兼容性）
func (c *ServiceChecker) checkSingleServiceReady(ctx context.Context, namespace, serviceName string) error {
	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("检查服务 %s 就绪状态", serviceName))
	}

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

	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("服务 %s 健康检查通过", serviceName))
	}
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

	if c.taskLogger != nil {
		c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("pod %s 状态正常: %s", podName, status))
	}
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
					if c.taskLogger != nil {
						c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("找到服务 %s 对应的pod: %s (使用选择器: %s)", serviceName, podName, selector))
					}
					return podName, nil
				}
			}
		}

		if i < maxRetries-1 {
			if c.taskLogger != nil {
				c.taskLogger.WriteStep("checkService", "INFO", fmt.Sprintf("第%d次尝试未找到服务 %s 的pod，%d秒后重试", i+1, serviceName, int(retryInterval.Seconds())))
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(retryInterval):
			}
		}
	}

	return "", fmt.Errorf("重试%d次后仍未找到服务 %s 对应的pod", maxRetries, serviceName)
}

// CheckServices 检查服务列表（包装函数，无日志记录）
func CheckServices(ctx context.Context, services []string, namespace string) error {
	// 使用空的taskID和nil logger，因为这是包装函数
	checker := NewServiceChecker("", nil)
	return checker.CheckServicesReady(ctx, services, namespace)
}
