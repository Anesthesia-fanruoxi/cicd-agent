package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// 允许所有跨域请求
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// taskLogConnection 任务日志WebSocket连接管理
type taskLogConnection struct {
	conn        *websocket.Conn
	taskID      string
	stepType    string
	logFilePath string
	mu          sync.Mutex
	closeChan   chan struct{}
	lastFilePos int64
	logBuffer   []string
	bufferSize  int
	flushTicker *time.Ticker
	maxLines    int
}

// TaskLogWebSocket 任务日志WebSocket处理函数
// 客户端示例：
// const ws = new WebSocket(`ws://agent地址/ws/task/logs?data=加密参数`);
// ws.onmessage = function(event) { console.log(event.data); };
func TaskLogWebSocket(c *gin.Context) {
	// 获取加密的参数
	encryptedData := c.Query("data")
	if encryptedData == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少加密参数"})
		return
	}

	// 解密参数（使用common中的解密方法）
	decryptedData, err := DecryptAndDecompress(encryptedData)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "解密参数失败"})
		return
	}

	// 解析解密后的参数
	var params struct {
		TaskID   string `json:"taskId"`
		StepType string `json:"stepType"`
	}

	if err := json.Unmarshal(decryptedData, &params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "解析参数失败"})
		return
	}

	taskID := params.TaskID
	stepType := params.StepType

	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少任务ID参数"})
		return
	}
	if stepType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少步骤名称参数"})
		return
	}

	// 升级HTTP连接为WebSocket连接
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("升级WebSocket连接失败: %v", err))
		return
	}

	// 构建日志文件路径
	logFilePath := buildLogFilePath(taskID, stepType)

	// 创建连接管理对象
	tc := &taskLogConnection{
		conn:        conn,
		taskID:      taskID,
		stepType:    stepType,
		logFilePath: logFilePath,
		closeChan:   make(chan struct{}),
		lastFilePos: 0,
		logBuffer:   make([]string, 0, 100),
		bufferSize:  0,
		flushTicker: time.NewTicker(200 * time.Millisecond),
		maxLines:    1000,
	}

	// 发送当前日志
	tc.sendCurrentLogs()

	// 启动监听任务日志的goroutine
	go tc.watchTaskLogs()

	// 启动缓冲区刷新goroutine
	go tc.flushBufferRoutine()

	// 处理客户端消息
	go tc.handleMessages()
}

// buildLogFilePath 构建日志文件路径
func buildLogFilePath(taskID, stepType string) string {
	// 日志文件名映射
	var logFileName string
	switch stepType {
	case "console":
		logFileName = "console.log"
	case "pullOnline":
		logFileName = "pullOnline.log"
	case "tagImages":
		logFileName = "tagImages.log"
	case "pushLocal":
		logFileName = "pushLocal.log"
	case "checkImage":
		logFileName = "checkImage.log"
	case "deployService":
		logFileName = "deployService.log"
	case "checkService":
		logFileName = "checkService.log"
	case "trafficSwitching":
		logFileName = "trafficSwitching.log"
	case "cleanupOldVersion":
		logFileName = "cleanupOldVersion.log"
	default:
		logFileName = stepType + ".log"
	}

	// 构建完整的日志文件路径: logs/{任务ID}/{日志文件名}
	return filepath.Join("logs", taskID, logFileName)
}

// sendCurrentLogs 发送当前日志
func (tc *taskLogConnection) sendCurrentLogs() {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// 检查日志文件是否存在
	if _, err := os.Stat(tc.logFilePath); os.IsNotExist(err) {
		err := tc.conn.WriteMessage(websocket.TextMessage, []byte("日志文件不存在或尚未生成"))
		if err != nil {
			AppLogger.Error(fmt.Sprintf("发送消息失败: %v", err))
		}
		return
	}

	// 读取日志文件内容
	content, err := os.ReadFile(tc.logFilePath)
	if err != nil {
		AppLogger.Warning(fmt.Sprintf("读取日志文件失败: %v", err))
		return
	}

	// 发送日志内容（限制行数）
	if len(content) > 0 {
		// 按行分割内容
		lines := splitLines(string(content))

		// 如果行数超过限制，只取最后maxLines行
		if len(lines) > tc.maxLines {
			sendLines := lines[len(lines)-tc.maxLines:]
			// 添加提示信息
			prefixMsg := fmt.Sprintf("[日志过长，仅显示最后%d行，总共%d行]\n", tc.maxLines, len(lines))
			sendContent := prefixMsg + strings.Join(sendLines, "\n")

			err := tc.conn.WriteMessage(websocket.TextMessage, []byte(sendContent))
			if err != nil {
				AppLogger.Error(fmt.Sprintf("发送日志失败: %v", err))
				return
			}
		} else {
			// 发送全部内容
			err := tc.conn.WriteMessage(websocket.TextMessage, content)
			if err != nil {
				AppLogger.Error(fmt.Sprintf("发送日志失败: %v", err))
				return
			}
		}
		// 设置文件位置为实际文件大小
		tc.lastFilePos = int64(len(content))
	}
}

// watchTaskLogs 监听任务日志更新
func (tc *taskLogConnection) watchTaskLogs() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-tc.closeChan:
			return
		case <-ticker.C:
			// 检查日志文件是否有更新
			fileInfo, err := os.Stat(tc.logFilePath)
			if err != nil {
				// 日志文件不存在时静默等待
				continue
			}

			// 如果文件大小有变化，读取新增内容
			if fileInfo.Size() > tc.lastFilePos {
				file, err := os.Open(tc.logFilePath)
				if err != nil {
					AppLogger.Error(fmt.Sprintf("打开日志文件失败: %v", err))
					continue
				}

				// 从上次位置开始读取
				file.Seek(tc.lastFilePos, 0)
				buffer := make([]byte, fileInfo.Size()-tc.lastFilePos)
				n, err := file.Read(buffer)
				file.Close()

				if err != nil {
					AppLogger.Error(fmt.Sprintf("读取日志文件失败: %v", err))
					continue
				}

				if n > 0 {
					// 解析新增日志
					newContent := string(buffer[:n])
					newLogs := splitLines(newContent)

					// 添加到缓冲区
					tc.mu.Lock()
					for _, log := range newLogs {
						if log == "" {
							continue
						}
						tc.logBuffer = append(tc.logBuffer, log)
						tc.bufferSize++
					}
					tc.mu.Unlock()
				}

				// 更新文件位置
				tc.lastFilePos = fileInfo.Size()
			}
		}
	}
}

// flushBufferRoutine 定期刷新缓冲区
func (tc *taskLogConnection) flushBufferRoutine() {
	defer tc.flushTicker.Stop()

	for {
		select {
		case <-tc.closeChan:
			return
		case <-tc.flushTicker.C:
			tc.flushBuffer()
		}
	}
}

// flushBuffer 刷新缓冲区，发送积累的日志
func (tc *taskLogConnection) flushBuffer() {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.bufferSize == 0 {
		return
	}

	// 构建批量消息
	var buffer bytes.Buffer
	for _, log := range tc.logBuffer {
		buffer.WriteString(log + "\n")
	}

	// 发送批量消息
	err := tc.conn.WriteMessage(websocket.TextMessage, buffer.Bytes())
	if err != nil {
		AppLogger.Error(fmt.Sprintf("批量发送日志失败: %v", err))
		return
	}

	// 清空缓冲区
	tc.logBuffer = tc.logBuffer[:0]
	tc.bufferSize = 0
}

// handleMessages 处理客户端消息
func (tc *taskLogConnection) handleMessages() {
	defer tc.close()

	for {
		// 读取客户端消息
		_, _, err := tc.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				AppLogger.Error(fmt.Sprintf("WebSocket读取错误: %v", err))
			}
			break
		}
		// 目前不处理客户端发送的消息
	}
}

// close 关闭连接
func (tc *taskLogConnection) close() {
	select {
	case <-tc.closeChan:
		// 已经关闭
		return
	default:
		// 关闭前发送剩余的日志
		tc.flushBuffer()

		close(tc.closeChan)
		tc.conn.Close()
	}
}

// splitLines 按行分割字符串
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}
