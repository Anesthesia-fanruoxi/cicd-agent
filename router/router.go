package router

import (
	"cicd-agent/common"
	"cicd-agent/taskCenter"
	"github.com/gin-gonic/gin"
)

// SetupRouter 设置路由
func SetupRouter() *gin.Engine {
	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()

	// 添加中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// API路由组
	apiGroup := r.Group("/")
	{
		// /update 接口 - 只需要IP白名单验证
		apiGroup.POST("/update",
			common.IPWhitelistMiddleware(),
			taskCenter.HandleUpdate,
		)

		// /callback 接口 - 只需要IP白名单验证
		apiGroup.POST("/callback",
			common.IPWhitelistMiddleware(),
			taskCenter.HandleCallback,
		)

		// /cancel 接口 - 只需要IP白名单验证
		apiGroup.POST("/api/task/cancel",
			common.IPWhitelistMiddleware(),
			taskCenter.HandleCancel,
		)
	}

	// 健康检查接口（不需要认证）
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
			"msg":    "服务运行正常",
		})
	})

	// WebSocket日志查看接口
	r.GET("/ws/task/logs", common.TaskLogWebSocket)

	return r
}
