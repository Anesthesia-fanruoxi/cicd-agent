package javaBuild

import (
	"cicd-agent/common"
	"cicd-agent/config"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// getNamespace 统一的namespace获取方法
// mode: "now" - 当前运行的namespace（从.current文件读取）, "next" - 下一个要部署的namespace
func getNamespace(project string, mode string) string {
	singleNamespace := fmt.Sprintf("%s-service", project)

	// 检查是否为双副本部署模式
	if !common.HasVersionStructure(project) {
		return singleNamespace
	}

	switch mode {
	case "now":
		// 获取当前运行的namespace（使用统一的版本获取方法）
		version, err := common.GetVersion(project)
		if err != nil {
			common.AppLogger.Error(fmt.Sprintf("获取版本信息失败: %v", err))
			// 获取失败，默认返回v1
			namespace := fmt.Sprintf("%s-service-v1", project)
			return namespace
		}

		// 根据版本信息构建namespace
		namespace := fmt.Sprintf("%s-service-%s", project, version)
		common.AppLogger.Info(fmt.Sprintf("当前运行namespace: %s", namespace))
		return namespace

	case "next":
		// 获取下一个要部署的namespace（蓝绿切换逻辑）
		nowNamespace := getNamespace(project, "now")
		var nextNamespace string
		if strings.Contains(nowNamespace, "-v1") {
			nextNamespace = fmt.Sprintf("%s-service-v2", project)
		} else if strings.Contains(nowNamespace, "-v2") {
			nextNamespace = fmt.Sprintf("%s-service-v1", project)
		} else {
			// 单版本模式或首次部署，默认使用v1
			nextNamespace = fmt.Sprintf("%s-service-v1", project)
		}
		common.AppLogger.Info(fmt.Sprintf("下一个部署namespace: %s", nextNamespace))
		return nextNamespace

	default:
		common.AppLogger.Warning(fmt.Sprintf("未知的namespace模式: %s，使用默认", mode))
		return singleNamespace
	}
}

// getDeploymentPath 统一的部署路径获取方法
// mode: "now" - 当前运行版本的部署路径, "next" - 下一个要部署版本的部署路径
func getDeploymentPath(project string, mode string) string {
	// 获取项目基础目录
	baseDir, exists := config.AppConfig.GetProjectPath(project)
	if !exists {
		common.AppLogger.Error(fmt.Sprintf("项目 %s 的部署目录未配置", project))
		return fmt.Sprintf("/data/project/%s/deployment", project) // 默认路径
	}

	// 检查是否为双副本部署模式
	if !common.HasVersionStructure(project) {
		return fmt.Sprintf("%s/deployment", baseDir)
	}

	switch mode {
	case "now":
		// 获取当前运行版本的部署路径
		version, err := common.GetVersion(project)
		if err != nil {
			common.AppLogger.Error(fmt.Sprintf("获取版本信息失败: %v", err))
			// 获取失败，默认返回v1路径
			return fmt.Sprintf("%s/deployment-v1", baseDir)
		}
		path := fmt.Sprintf("%s/deployment-%s", baseDir, version)
		common.AppLogger.Info(fmt.Sprintf("当前运行部署路径: %s", path))
		return path

	case "next":
		// 获取下一个要部署版本的部署路径（蓝绿切换逻辑）
		nowPath := getDeploymentPath(project, "now")
		var nextPath string
		if strings.Contains(nowPath, "-v1") {
			nextPath = fmt.Sprintf("%s/deployment-v2", baseDir)
		} else if strings.Contains(nowPath, "-v2") {
			nextPath = fmt.Sprintf("%s/deployment-v1", baseDir)
		} else {
			// 单版本模式或首次部署，默认使用v1
			nextPath = fmt.Sprintf("%s/deployment-v1", baseDir)
		}
		common.AppLogger.Info(fmt.Sprintf("下一个部署路径: %s", nextPath))
		return nextPath

	default:
		common.AppLogger.Warning(fmt.Sprintf("未知的路径模式: %s，使用默认", mode))
		return fmt.Sprintf("%s/deployment", baseDir)
	}
}

// namespaceExists 检查namespace是否存在
func namespaceExists(namespace string) bool {
	cmd := exec.Command("kubectl", "get", "namespace", namespace)
	err := cmd.Run()
	return err == nil
}

// getOnlineImages 获取在线镜像列表
func getOnlineImages(project, tag string) ([]string, error) {
	services, err := getServices(project)
	if err != nil {
		return nil, err
	}

	var images []string
	for _, service := range services {
		image := fmt.Sprintf("%s/%s/%s:%s",
			config.AppConfig.Harbor.Online, project, service, tag)
		images = append(images, image)
	}

	return images, nil
}

// getLocalImages 获取本地镜像列表
func getLocalImages(project, tag string) ([]string, error) {
	services, err := getServices(project)
	if err != nil {
		return nil, err
	}

	var images []string
	for _, service := range services {
		image := fmt.Sprintf("%s/%s/%s:%s",
			config.AppConfig.Harbor.Offline, project, service, tag)
		images = append(images, image)
	}

	return images, nil
}

// getAllImages 获取所有镜像列表（在线+本地）
func getAllImages(project, tag string) ([]string, error) {
	onlineImages, err := getOnlineImages(project, tag)
	if err != nil {
		return nil, err
	}

	localImages, err := getLocalImages(project, tag)
	if err != nil {
		return nil, err
	}

	// 合并在线镜像和本地镜像列表
	allImages := make([]string, 0, len(onlineImages)+len(localImages))
	allImages = append(allImages, onlineImages...)
	allImages = append(allImages, localImages...)

	return allImages, nil
}

// getServiceList 获取服务列表
func getServiceList(project string) ([]string, error) {
	// 获取下一个版本的部署目录（统一处理单副本和双副本）
	deployDir, err := common.GetDeploymentPath(project)
	if err != nil {
		return nil, fmt.Errorf("获取部署目录失败: %v", err)
	}

	common.AppLogger.Info(fmt.Sprintf("使用部署目录: %s", deployDir))

	// 扫描部署目录获取服务列表
	entries, err := os.ReadDir(deployDir)
	if err != nil {
		return nil, fmt.Errorf("读取部署目录失败 %s: %v", deployDir, err)
	}

	var services []string
	for _, entry := range entries {
		if entry.IsDir() {
			// 检查是否包含docker-compose.yml或docker-compose.yaml文件
			composePath1 := filepath.Join(deployDir, entry.Name(), "docker-compose.yml")
			composePath2 := filepath.Join(deployDir, entry.Name(), "docker-compose.yaml")
			if _, err := os.Stat(composePath1); err == nil {
				services = append(services, entry.Name())
			} else if _, err := os.Stat(composePath2); err == nil {
				services = append(services, entry.Name())
			}
		} else if strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml") {
			// 如果是直接的yaml文件，提取服务名（去掉扩展名）
			serviceName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			services = append(services, serviceName)
		}
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("在部署目录 %s 中未找到任何服务", deployDir)
	}

	common.AppLogger.Info(fmt.Sprintf("扫描到服务列表: %v", services))
	return services, nil
}

// getServices 获取服务列表（从部署目录读取）
func getServices(project string) ([]string, error) {
	return getServiceList(project)
}

// getNginxConfDir 获取nginx配置目录
func getNginxConfDir() string {
	// 可以从配置文件或环境变量获取
	// 暂时使用默认路径
	return "/etc/nginx/conf.d"
}
