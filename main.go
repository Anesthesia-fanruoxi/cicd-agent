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

	// 初始化IP白名单
	common.InitWhitelist()

	// 设置路由
	r := router.SetupRouter()

	// 启动服务器
	addr := config.AppConfig.Server.Host + ":" + config.AppConfig.Server.Port
	common.AppLogger.Info("启动CICD代理服务", "地址: "+addr)

	if err := r.Run(addr); err != nil {
		common.AppLogger.Error("启动服务器失败:", err)
		log.Fatalf("启动服务器失败: %v", err)
	}
}
