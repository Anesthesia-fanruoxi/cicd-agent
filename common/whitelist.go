package common

import (
	"cicd-agent/config"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"sync"
	"time"
)

// IPWhitelist IP白名单管理器
type IPWhitelist struct {
	allowedIPs map[string]bool
	mutex      sync.RWMutex
	stopChan   chan struct{}
}

var whitelist *IPWhitelist

// InitWhitelist 初始化IP白名单
func InitWhitelist() {
	whitelist = &IPWhitelist{
		allowedIPs: make(map[string]bool),
		stopChan:   make(chan struct{}),
	}

	// 首次加载IP白名单
	whitelist.updateIPs()

	// 启动定时更新goroutine
	go whitelist.startUpdateRoutine()
}

// updateIPs 更新IP白名单
func (w *IPWhitelist) updateIPs() {
	if config.AppConfig == nil {
		AppLogger.Warning("配置未加载，跳过IP白名单更新")
		return
	}

	ips := config.AppConfig.ResolveWhitelistIPs()

	w.mutex.Lock()
	defer w.mutex.Unlock()

	// 清空旧的IP列表
	w.allowedIPs = make(map[string]bool)

	// 添加新的IP列表
	for _, ip := range ips {
		w.allowedIPs[ip] = true
	}
}

// startUpdateRoutine 启动定时更新routine
func (w *IPWhitelist) startUpdateRoutine() {
	if config.AppConfig == nil {
		return
	}

	ticker := time.NewTicker(config.AppConfig.GetUpdateInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.updateIPs()
		case <-w.stopChan:
			AppLogger.Info("IP白名单更新routine已停止")
			return
		}
	}
}

// isAllowed 检查IP是否在白名单中
func (w *IPWhitelist) isAllowed(ip string) bool {
	w.mutex.RLock()
	defer w.mutex.RUnlock()

	return w.allowedIPs[ip]
}

// Stop 停止IP白名单更新
func (w *IPWhitelist) Stop() {
	close(w.stopChan)
}

// getClientIP 获取客户端真实IP
func getClientIP(c *gin.Context) string {
	// 优先从X-Forwarded-For获取
	forwarded := c.GetHeader("X-Forwarded-For")
	if forwarded != "" {
		// X-Forwarded-For可能包含多个IP，取第一个
		ips := strings.Split(forwarded, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// 从X-Real-IP获取
	realIP := c.GetHeader("X-Real-IP")
	if realIP != "" {
		return strings.TrimSpace(realIP)
	}

	// 最后使用RemoteAddr
	return c.ClientIP()
}

// IPWhitelistMiddleware IP白名单检查中间件
func IPWhitelistMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if whitelist == nil {
			AppLogger.Error("IP白名单未初始化")
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "服务器内部错误",
			})
			c.Abort()
			return
		}

		clientIP := getClientIP(c)

		if !whitelist.isAllowed(clientIP) {
			AppLogger.Warning("未授权的IP访问:", clientIP)
			// 返回404而不是403，隐藏服务存在
			c.JSON(http.StatusNotFound, gin.H{
				"code": 404,
				"msg":  "Not Found",
			})
			c.Abort()
			return
		}

		AppLogger.Info("允许IP访问:", clientIP)
		c.Set("client_ip", clientIP)
		c.Next()
	}
}

// GetWhitelist 获取白名单实例（用于测试或管理）
func GetWhitelist() *IPWhitelist {
	return whitelist
}
