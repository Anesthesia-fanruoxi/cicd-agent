package taskCenter

// UpdateRequest 更新请求结构
type UpdateRequest struct {
	Project  string `json:"project" binding:"required"`
	Type     string `json:"type"`
	Category string `json:"category,omitempty"`
}

// CallbackRequest 回调请求结构
type CallbackRequest struct {
	Project       string                 `json:"project" binding:"required"`
	Category      string                 `json:"category"`
	Status        string                 `json:"status" binding:"required"`
	Tag           string                 `json:"tag" binding:"required"`
	TaskID        string                 `json:"task_id"`
	CreateTime    string                 `json:"create_time"`
	Description   string                 `json:"description"`
	FinishedAt    string                 `json:"finished_at"`
	ProFeishuURL  string                 `json:"pro_feishu_url"`
	OpsFeishuURL  string                 `json:"ops_feishu_url"`
	StepDurations map[string]interface{} `json:"step_durations"`
}

// RemoteCallRequest 远程调用请求结构
type RemoteCallRequest struct {
	Project     string `json:"project"`
	CallbackURL string `json:"callback_url"`
	Category    string `json:"category,omitempty"`
}

// CancelRequest 取消任务请求结构
type CancelRequest struct {
	ID string `json:"id" binding:"required"`
}

// EncryptedRequest 加密请求结构
type EncryptedRequest struct {
	Data string `json:"data" binding:"required"`
}

// Response 统一响应结构
type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}
