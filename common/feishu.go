package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FeishuCardMessage 飞书卡片消息结构
type FeishuCardMessage struct {
	MsgType string     `json:"msg_type"`
	Card    FeishuCard `json:"card"`
}

// FeishuCard 飞书卡片结构
type FeishuCard struct {
	Config   FeishuCardConfig `json:"config"`
	Elements []FeishuElement  `json:"elements"`
	Header   FeishuCardHeader `json:"header"`
}

// FeishuCardConfig 卡片配置
type FeishuCardConfig struct {
	WideScreenMode bool `json:"wide_screen_mode"`
}

// FeishuCardHeader 卡片头部
type FeishuCardHeader struct {
	Title    FeishuText `json:"title"`
	Template string     `json:"template"`
}

// FeishuElement 卡片元素接口
type FeishuElement interface{}

// FeishuText 文本结构
type FeishuText struct {
	Content string `json:"content"`
	Tag     string `json:"tag"`
}

// FeishuField 字段结构
type FeishuField struct {
	IsShort bool       `json:"is_short"`
	Text    FeishuText `json:"text"`
}

// FeishuFieldSet 字段集合
type FeishuFieldSet struct {
	Tag    string        `json:"tag"`
	Fields []FeishuField `json:"fields"`
}

// FeishuDivider 分割线
type FeishuDivider struct {
	Tag string `json:"tag"`
}

// SendFeishuCard 发送飞书卡片通知
func SendFeishuCard(webhookURL, project, tag, status, startTime, endTime, deployType, category, projectName string) error {
	if webhookURL == "" {
		AppLogger.Info("飞书通知URL为空，跳过发送")
		return nil
	}

	// 构建卡片消息
	card := buildTaskCard(project, tag, status, startTime, endTime, deployType, category, projectName)

	// 序列化为JSON
	jsonData, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("序列化飞书消息失败: %v", err)
	}

	// 发送HTTP请求
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("发送飞书通知失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("飞书通知响应异常，状态码: %d", resp.StatusCode)
	}

	AppLogger.Info(fmt.Sprintf("飞书通知发送成功: 项目=%s, 状态=%s", project, status))
	return nil
}

// buildTaskCard 构建任务卡片
func buildTaskCard(project, tag, status, startTime, endTime, deployType, category, projectName string) FeishuCardMessage {
	// 根据状态设置颜色和标题
	var template, title, statusText string
	switch status {
	case "complete":
		template = "green"
		title = fmt.Sprintf("🎉 【%s】部署成功", projectName)
		statusText = "✅ 部署完成"
	case "failed":
		template = "red"
		title = fmt.Sprintf("❌ 【%s】部署失败", projectName)
		statusText = "❌ 部署失败"
	case "cancel":
		template = "grey"
		title = fmt.Sprintf("⏹️ 【%s】部署取消", projectName)
		statusText = "⏹️ 部署取消"
	default:
		template = "blue"
		title = "📋 部署通知"
		statusText = fmt.Sprintf("📋 %s", status)
	}

	// 计算耗时
	duration := calculateDuration(startTime, endTime)

	// 构建字段列表 - 6个字段，3行2列布局
	var fields []FeishuField

	// 第一行：项目名称、版本标签
	fields = append(fields,
		FeishuField{
			IsShort: true,
			Text: FeishuText{
				Content: fmt.Sprintf("**项目名称**\n%s", project),
				Tag:     "lark_md",
			},
		},
		FeishuField{
			IsShort: true,
			Text: FeishuText{
				Content: fmt.Sprintf("**版本标签**\n%s", tag),
				Tag:     "lark_md",
			},
		},
	)

	// 第二行：部署状态、耗时
	fields = append(fields,
		FeishuField{
			IsShort: true,
			Text: FeishuText{
				Content: fmt.Sprintf("**部署状态**\n%s", statusText),
				Tag:     "lark_md",
			},
		},
		FeishuField{
			IsShort: true,
			Text: FeishuText{
				Content: fmt.Sprintf("**耗时**\n%s", duration),
				Tag:     "lark_md",
			},
		},
	)

	// 第三行：额外参数、当前版本/空白
	// 额外参数字段
	var categoryContent string
	if category != "" {
		categoryContent = fmt.Sprintf("**额外参数**\n%s", category)
	} else {
		categoryContent = "**额外参数**\n无"
	}

	fields = append(fields, FeishuField{
		IsShort: true,
		Text: FeishuText{
			Content: categoryContent,
			Tag:     "lark_md",
		},
	})

	// 根据部署类型添加最后一个字段
	if deployType == "double" {
		// 双副本：显示当前运行版本号
		currentVersion := getCurrentVersion(project)
		fields = append(fields, FeishuField{
			IsShort: true,
			Text: FeishuText{
				Content: fmt.Sprintf("**当前版本**\n%s", currentVersion),
				Tag:     "lark_md",
			},
		})
	} else {
		// 单副本：显示空白
		fields = append(fields, FeishuField{
			IsShort: true,
			Text: FeishuText{
				Content: "** **\n ",
				Tag:     "lark_md",
			},
		})
	}

	return FeishuCardMessage{
		MsgType: "interactive",
		Card: FeishuCard{
			Config: FeishuCardConfig{
				WideScreenMode: true,
			},
			Header: FeishuCardHeader{
				Title: FeishuText{
					Content: title,
					Tag:     "plain_text",
				},
				Template: template,
			},
			Elements: []FeishuElement{
				FeishuFieldSet{
					Tag:    "div",
					Fields: fields,
				},
				FeishuDivider{
					Tag: "hr",
				},
				FeishuFieldSet{
					Tag: "div",
					Fields: []FeishuField{
						{
							IsShort: true,
							Text: FeishuText{
								Content: fmt.Sprintf("**开始时间**\n%s", startTime),
								Tag:     "lark_md",
							},
						},
						{
							IsShort: true,
							Text: FeishuText{
								Content: fmt.Sprintf("**结束时间**\n%s", endTime),
								Tag:     "lark_md",
							},
						},
					},
				},
			},
		},
	}
}

// getCurrentVersion 获取当前运行版本号
func getCurrentVersion(project string) string {
	// 检查项目是否有版本结构
	if !HasVersionStructure(project) {
		return "单版本"
	}

	// 获取当前版本信息
	versionInfo, err := GetCurrentVersion(project)
	if err != nil {
		AppLogger.Warning(fmt.Sprintf("获取项目 %s 当前版本失败: %v", project, err))
		return "未知"
	}

	return versionInfo.CurrentVersion
}

// calculateDuration 计算耗时
func calculateDuration(startTime, endTime string) string {
	if startTime == "" || endTime == "" {
		return "未知"
	}

	layout := "2006-01-02 15:04:05"
	start, err1 := time.Parse(layout, startTime)
	end, err2 := time.Parse(layout, endTime)

	if err1 != nil || err2 != nil {
		return "计算失败"
	}

	duration := end.Sub(start)

	// 格式化耗时显示
	if duration < time.Minute {
		return fmt.Sprintf("%.0f秒", duration.Seconds())
	} else if duration < time.Hour {
		minutes := int(duration.Minutes())
		seconds := int(duration.Seconds()) % 60
		return fmt.Sprintf("%d分%d秒", minutes, seconds)
	} else {
		hours := int(duration.Hours())
		minutes := int(duration.Minutes()) % 60
		seconds := int(duration.Seconds()) % 60
		return fmt.Sprintf("%d小时%d分%d秒", hours, minutes, seconds)
	}
}
