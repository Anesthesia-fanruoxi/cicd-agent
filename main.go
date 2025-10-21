package main

import (
	"log"

	"cicd-agent/common"
	"cicd-agent/config"
	"cicd-agent/router"
)

func main() {
	// 初始化配置
	if _, err := config.LoadConfig(""); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化日志
	common.InitLogger()

	// 启动日志清理定时任务（保留7天）
	common.StartLogCleanupRoutine(7)

	// 初始化IP白名单
	common.InitWhitelist()

	// 设置路由
	r := router.SetupRouter()

	// 输出配置信息
	printConfigInfo()

	// 启动服务器
	addr := config.AppConfig.Server.Host + ":" + config.AppConfig.Server.Port
	common.AppLogger.Info("启动CICD代理服务", "地址: "+addr)

	if err := r.Run(addr); err != nil {
		common.AppLogger.Error("启动服务器失败:", err)
		log.Fatalf("启动服务器失败: %v", err)
	}
}

// printConfigInfo 输出配置信息
func printConfigInfo() {
	log.Println("========================================")
	log.Println("配置信息:")
	log.Println("========================================")

	// 输出双副本项目配置信息
	log.Println("双副本项目配置:")
	if len(config.AppConfig.Deployment.Double) == 0 {
		log.Println("  无")
	} else {
		for projectName, path := range config.AppConfig.Deployment.Double {
			// 获取该项目的流量代理配置
			proxyURLs := config.AppConfig.GetTrafficProxyURLs(projectName)

			if config.AppConfig.TrafficProxy.Enable && len(proxyURLs) > 0 {
				log.Printf("  获取到双副本配置项目%s，已开启流量代理，代理地址为%v (部署目录: %s)",
					projectName, proxyURLs, path)
			} else if config.AppConfig.TrafficProxy.Enable {
				log.Printf("  获取到双副本配置项目%s，已开启流量代理，但未配置代理地址 (部署目录: %s)",
					projectName, path)
			} else {
				log.Printf("  获取到双副本配置项目%s，未开启流量代理 (部署目录: %s)",
					projectName, path)
			}
		}
	}

	log.Println("========================================")
}
