package taskStep

import "context"

// Step 定义任务步骤接口
type Step interface {
	Execute(ctx context.Context) error
	GetName() string
}

// BaseStep 基础步骤结构
type BaseStep struct {
	Name string
}

// GetName 获取步骤名称
func (s *BaseStep) GetName() string {
	return s.Name
}
