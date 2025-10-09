package common

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TaskLogger 任务日志管理器
type TaskLogger struct {
	taskID  string
	logDir  string
	writers map[string]*os.File
	mu      sync.RWMutex
}

// NewTaskLogger 创建任务日志器
func NewTaskLogger(taskID string) *TaskLogger {
	logDir := filepath.Join("logs", taskID)

	// 创建日志目录
	if err := os.MkdirAll(logDir, 0755); err != nil {
		AppLogger.Error("创建任务日志目录失败:", err)
		return nil
	}

	return &TaskLogger{
		taskID:  taskID,
		logDir:  logDir,
		writers: make(map[string]*os.File),
	}
}

// getWriter 获取或创建指定类型的日志文件写入器
func (t *TaskLogger) getWriter(stepType string) (*os.File, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 如果已存在，直接返回
	if writer, exists := t.writers[stepType]; exists {
		return writer, nil
	}

	// 创建新的日志文件
	logFile := filepath.Join(t.logDir, stepType+".log")
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("创建日志文件失败: %v", err)
	}

	t.writers[stepType] = file
	return file, nil
}

// WriteStep 写入步骤日志
func (t *TaskLogger) WriteStep(stepType, level, message string) {
	if t == nil {
		return
	}

	writer, err := t.getWriter(stepType)
	if err != nil {
		AppLogger.Error("获取日志写入器失败:", err)
		return
	}

	timestamp := time.Now().Format("2006/01/02 15:04:05")
	logLine := fmt.Sprintf("%s [%s] %s\n", timestamp, level, message)

	t.mu.RLock()
	defer t.mu.RUnlock()

	if _, err := writer.WriteString(logLine); err != nil {
		AppLogger.Error("写入日志失败:", err)
	}
}

// WriteCommand 写入命令执行日志
func (t *TaskLogger) WriteCommand(stepType, command string, output []byte, err error) {
	if t == nil {
		return
	}

	writer, writeErr := t.getWriter(stepType)
	if writeErr != nil {
		AppLogger.Error("获取日志写入器失败:", writeErr)
		return
	}

	timestamp := time.Now().Format("2006/01/02 15:04:05")

	t.mu.RLock()
	defer t.mu.RUnlock()

	// 写入命令
	commandLine := fmt.Sprintf("%s [COMMAND] %s\n", timestamp, command)
	writer.WriteString(commandLine)

	// 写入输出
	if len(output) > 0 {
		writer.Write(output)
		writer.WriteString("\n")
	}

	// 写入错误
	if err != nil {
		errorLine := fmt.Sprintf("%s [ERROR] Command failed: %v\n", timestamp, err)
		writer.WriteString(errorLine)
	}
}

// GetStepWriter 获取步骤的 io.Writer（用于实时流式输出）
func (t *TaskLogger) GetStepWriter(stepType string) (io.Writer, error) {
	if t == nil {
		return nil, fmt.Errorf("task logger is nil")
	}
	return t.getWriter(stepType)
}

// WriteConsole 写入控制台日志（同时写入console.log文件）
func (t *TaskLogger) WriteConsole(level, message string) {
	if t == nil {
		return
	}

	t.WriteStep("console", level, message)
}

// Close 关闭所有日志文件
func (t *TaskLogger) Close() {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for stepType, writer := range t.writers {
		if err := writer.Close(); err != nil {
			AppLogger.Error(fmt.Sprintf("关闭日志文件失败 [%s]:", stepType), err)
		}
	}

	// 清空map
	t.writers = make(map[string]*os.File)
}

// GetLogDir 获取日志目录路径
func (t *TaskLogger) GetLogDir() string {
	if t == nil {
		return ""
	}
	return t.logDir
}
