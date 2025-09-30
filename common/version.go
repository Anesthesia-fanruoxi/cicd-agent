package common

import (
	"cicd-agent/config"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

// VersionInfo 版本信息结构
type VersionInfo struct {
	CurrentVersion string                 `json:"current_version"` // v1 或 v2
	LastUpdated    string                 `json:"last_updated"`    // 最后更新时间
	StepDurations  map[string]interface{} `json:"step_durations"`  // 上次各步骤执行时间
}

// GetCurrentVersion 读取版本文件，如果不存在则创建默认文件
func GetCurrentVersion(project string) (*VersionInfo, error) {
	// 获取项目部署目录
	deployDir, exists := config.AppConfig.GetProjectPath(project)
	if !exists {
		return nil, fmt.Errorf("项目 %s 的部署目录未配置", project)
	}

	currentFile := filepath.Join(deployDir, ".current")

	// 检查文件是否存在
	if _, err := os.Stat(currentFile); os.IsNotExist(err) {
		// 文件不存在，创建默认文件
		return createDefaultVersionFile(project, currentFile)
	}

	// 文件存在，读取并解析
	return readVersionFile(currentFile)
}

// createDefaultVersionFile 创建默认版本文件
func createDefaultVersionFile(project, filePath string) (*VersionInfo, error) {
	defaultVersion := &VersionInfo{
		CurrentVersion: "v1",
		LastUpdated:    time.Now().Format("2006-01-02 15:04:05"),
		StepDurations:  make(map[string]interface{}),
	}

	// 序列化为JSON
	data, err := json.MarshalIndent(defaultVersion, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化默认版本信息失败: %v", err)
	}

	// 写入文件
	if err := ioutil.WriteFile(filePath, data, 0644); err != nil {
		return nil, fmt.Errorf("创建默认版本文件失败: %v", err)
	}

	AppLogger.Info(fmt.Sprintf("已为项目 %s 创建默认版本文件", project))
	return defaultVersion, nil
}

// readVersionFile 读取并解析版本文件
func readVersionFile(filePath string) (*VersionInfo, error) {
	// 读取文件内容
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取版本文件失败: %v", err)
	}

	// 解析JSON
	var versionInfo VersionInfo
	if err := json.Unmarshal(data, &versionInfo); err != nil {
		return nil, fmt.Errorf("解析版本文件失败: %v", err)
	}

	return &versionInfo, nil
}

// GetVersion 获取当前版本号
func GetVersion(project string) (string, error) {
	versionInfo, err := GetCurrentVersion(project)
	if err != nil {
		return "", err
	}
	return versionInfo.CurrentVersion, nil
}

// UpdateVersion 更新版本字段
func UpdateVersion(project, newVersion string) error {
	// 读取当前版本信息
	versionInfo, err := GetCurrentVersion(project)
	if err != nil {
		return fmt.Errorf("读取版本信息失败: %v", err)
	}

	// 更新版本字段
	versionInfo.CurrentVersion = newVersion
	versionInfo.LastUpdated = time.Now().Format("2006-01-02 15:04:05")

	// 保存到文件
	return saveVersionFile(project, versionInfo)
}

// saveVersionFile 保存版本信息到文件
func saveVersionFile(project string, versionInfo *VersionInfo) error {
	// 获取项目部署目录
	deployDir, exists := config.AppConfig.GetProjectPath(project)
	if !exists {
		return fmt.Errorf("项目 %s 的部署目录未配置", project)
	}

	currentFile := filepath.Join(deployDir, ".current")

	// 序列化为JSON
	data, err := json.MarshalIndent(versionInfo, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化版本信息失败: %v", err)
	}

	// 写入文件
	if err := ioutil.WriteFile(currentFile, data, 0644); err != nil {
		return fmt.Errorf("写入版本文件失败: %v", err)
	}

	AppLogger.Info(fmt.Sprintf("已更新项目 %s 的版本: %s", project, versionInfo.CurrentVersion))
	return nil
}

// UpdateStepDuration 更新步骤耗时信息
func UpdateStepDuration(project, stepName string, duration interface{}) error {
	// 读取当前版本信息
	versionInfo, err := GetCurrentVersion(project)
	if err != nil {
		return fmt.Errorf("读取版本信息失败: %v", err)
	}

	// 更新步骤耗时
	versionInfo.StepDurations[stepName] = duration
	versionInfo.LastUpdated = time.Now().Format("2006-01-02 15:04:05")

	// 保存到文件
	return saveVersionFile(project, versionInfo)
}

// HasVersionStructure 检查项目是否有v1/v2版本结构（基于配置）
func HasVersionStructure(project string) bool {
	return config.AppConfig.IsDoubleProject(project)
}

// GetDeploymentPath 获取部署路径（默认获取下一个版本的路径）
func GetDeploymentPath(project string) (string, error) {
	// 获取项目基础目录
	baseDir, exists := config.AppConfig.GetProjectPath(project)
	if !exists {
		return "", fmt.Errorf("项目 %s 的部署目录未配置", project)
	}

	// 检查是否为双副本模式
	if !HasVersionStructure(project) {
		// 单副本模式，返回 deployment 目录
		deployPath := filepath.Join(baseDir, "deployment")
		return deployPath, nil
	}

	// 双副本模式，根据当前版本生成下一个版本的路径
	version, err := GetVersion(project)
	if err != nil {
		return "", fmt.Errorf("获取版本信息失败: %v", err)
	}

	// 获取下一个版本
	var nextVersion string
	if version == "v1" {
		nextVersion = "v2"
	} else if version == "v2" {
		nextVersion = "v1"
	} else {
		nextVersion = "v1" // 默认
	}

	deployPath := filepath.Join(baseDir, fmt.Sprintf("deployment-%s", nextVersion))
	return deployPath, nil
}
