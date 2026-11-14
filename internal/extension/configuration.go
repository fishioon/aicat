package extension

import "fmt"

func (h *Host) ReadConfig(id string) (map[string]any, error) {
	it := h.find(id)
	if it == nil || it.parts.Configuration == nil {
		return nil, fmt.Errorf("扩展没有配置：%s", id)
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.parts.Configuration.ReadConfig()
}

func (h *Host) UpdateConfig(id string, patch map[string]any) error {
	it := h.find(id)
	if it == nil || it.parts.Configuration == nil {
		return fmt.Errorf("扩展没有配置：%s", id)
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.on.Load() {
		return fmt.Errorf("请先停用扩展再修改配置")
	}
	if it.parts.Messages != nil && !it.parts.Messages.Installed() {
		return fmt.Errorf("请先安装渠道插件")
	}
	return it.parts.Configuration.UpdateConfig(patch)
}
