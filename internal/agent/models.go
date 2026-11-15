package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type Model struct {
	Model       string `json:"model"`
	DisplayName string `json:"displayName"`
	IsDefault   bool   `json:"isDefault"`
}

func (r *Runner) Models(ctx context.Context, binary, workspace string) ([]Model, error) {
	client, err := r.connect(binary, workspace)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := []Model{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := client.request(callCtx, "model/list", params)
		if err != nil {
			return nil, err
		}
		var response struct {
			Data       []Model `json:"data"`
			NextCursor string  `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &response) != nil || response.Data == nil {
			return nil, errors.New("Codex 返回无效模型列表")
		}
		for _, model := range response.Data {
			if model.Model == "" {
				return nil, errors.New("Codex 模型缺少标识")
			}
			result = append(result, model)
		}
		if response.NextCursor == "" {
			return result, nil
		}
		if seen[response.NextCursor] {
			return nil, errors.New("Codex 模型分页游标重复")
		}
		seen[response.NextCursor] = true
		cursor = response.NextCursor
	}
	return nil, errors.New("Codex 模型列表超过分页上限")
}
