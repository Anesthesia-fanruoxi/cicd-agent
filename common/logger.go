package common

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"
)

// Logger 日志配置
type Logger struct {
	*log.Logger
}

var AppLogger *Logger

// InitLogger 初始化日志
func InitLogger() {
	AppLogger = &Logger{
		Logger: log.New(os.Stdout, "", 0), // 不使用默认标志，自定义格式
	}
}

// getCallerInfo 获取调用者信息
func getCallerInfo() string {
	_, file, line, ok := runtime.Caller(3)
	if !ok {
		return "unknown:0"
	}
	// 只显示文件名，不显示完整路径
	parts := strings.Split(file, "/")
	if len(parts) > 0 {
		file = parts[len(parts)-1]
	}
	// Windows路径处理
	parts = strings.Split(file, "\\")
	if len(parts) > 0 {
		file = parts[len(parts)-1]
	}
	return fmt.Sprintf("%s:%d", file, line)
}

// logWithLevel 统一的日志输出方法
func (l *Logger) logWithLevel(level string, v ...interface{}) {
	caller := getCallerInfo()
	timestamp := time.Now().Format("2006/01/02 15:04:05")
	message := fmt.Sprint(v...)

	// 格式：时间 [级别] 文件名:行号 消息
	logMessage := fmt.Sprintf("%s [%s] %s %s", timestamp, level, caller, message)
	l.Println(logMessage)
}

// Info 信息日志
func (l *Logger) Info(v ...interface{}) {
	l.logWithLevel("INFO", v...)
}

// Error 错误日志
func (l *Logger) Error(v ...interface{}) {
	l.logWithLevel("ERROR", v...)
}

// Warning 警告日志
func (l *Logger) Warning(v ...interface{}) {
	l.logWithLevel("WARNING", v...)
}

// Debug 调试日志
func (l *Logger) Debug(v ...interface{}) {
	l.logWithLevel("DEBUG", v...)
}
