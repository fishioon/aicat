package extension

import (
	"context"
	"fmt"
)

func (h *Host) ReadData(ctx context.Context, id string, query map[string]any) (map[string]any, error) {
	it := h.find(id)
	if it == nil || it.parts.ReadData == nil {
		return nil, fmt.Errorf("这个扩展没有可读数据：%s", id)
	}
	return it.parts.ReadData(ctx, query)
}
